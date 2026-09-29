package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"innobytes.tech/bflow/internal/testutil"
)

// ghFake es un GitHub mínimo en memoria para las pruebas de extremo a extremo.
type ghFake struct {
	mu   sync.Mutex
	prs  map[int]map[string]any
	next int
}

func (g *ghFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
		return
	}
	const base = "/repos/acme/app/pulls"
	switch {
	case r.Method == "POST" && r.URL.Path == base:
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		g.next++
		n := 40 + g.next
		pr := map[string]any{"number": n, "html_url": fmt.Sprintf("https://github.com/acme/app/pull/%d", n), "state": "open", "merged": false, "head": in["head"], "body": in["body"]}
		g.prs[n] = pr
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(pr)
	case r.Method == "GET" && r.URL.Path == base:
		out := []map[string]any{}
		for _, pr := range g.prs {
			if "acme:"+pr["head"].(string) == r.URL.Query().Get("head") && pr["state"] == "open" {
				out = append(out, pr)
			}
		}
		json.NewEncoder(w).Encode(out)
	case strings.HasPrefix(r.URL.Path, base+"/"):
		var n int
		fmt.Sscan(strings.TrimPrefix(r.URL.Path, base+"/"), &n)
		pr, ok := g.prs[n]
		if !ok {
			w.WriteHeader(404)
			return
		}
		if r.Method == "PATCH" {
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			pr["body"] = in["body"]
		}
		json.NewEncoder(w).Encode(pr)
	default:
		w.WriteHeader(404)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitRepo crea un repo de trabajo con remoto bare y rama dev.
func gitRepo(t *testing.T, bflowYAML string) (*repo, string) {
	r := newRepo(t)
	root := testutil.TempDir(t)
	remote := filepath.Join(root, "remote.git")
	git(t, root, "init", "--bare", "-b", "dev", remote)
	git(t, r.dir, "init", "-b", "dev")
	// bflow hace commits (la spec) con la identidad del repo: en CI no hay una global.
	git(t, r.dir, "config", "user.name", "t")
	git(t, r.dir, "config", "user.email", "t@t")
	os.MkdirAll(filepath.Join(r.dir, "internal"), 0o755)
	os.WriteFile(filepath.Join(r.dir, "internal", "a.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(r.dir, "bflow.yaml"), []byte(bflowYAML), 0o644)
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "inicio")
	git(t, r.dir, "remote", "add", "origin", remote)
	git(t, r.dir, "push", "-u", "origin", "dev")
	t.Setenv("BFLOW_NO_KEYRING", "1")
	return r, remote
}

func TestGitAndGitHubEndToEnd(t *testing.T) {
	gh := &ghFake{prs: map[int]map[string]any{}}
	srv := httptest.NewServer(gh)
	defer srv.Close()
	t.Setenv("GH_TOKEN", "tok")
	r, remote := gitRepo(t, "stack: go\nvcs: { host: github, repo: acme/app, base_branch: dev, api_url: "+srv.URL+" }\n")

	id := r.ok("task", "add", "Validación por campo").Data["id"].(string)
	r.ok("start", id, "--lane", "light")
	r.ok("report", id, "--agent", "spec-author", "--verdict", "READY")

	// Con cambios sin commitear en el código, aprobar el spec no crea la rama.
	os.WriteFile(filepath.Join(r.dir, "internal", "a.go"), []byte("package a // sucio\n"), 0o644)
	env := r.run("approve", id)
	if env.exit != 2 || env.Code != "dirty_worktree" {
		t.Fatalf("árbol sucio: exit %d %s %v", env.exit, env.Code, env.Data)
	}
	git(t, r.dir, "checkout", "--", "internal/a.go")

	env = r.ok("approve", id)
	branch := "feature/" + id + "-validacion-por-campo"
	if cur := git(t, r.dir, "rev-parse", "--abbrev-ref", "HEAD"); cur != branch {
		t.Fatalf("rama actual %q, want %q", cur, branch)
	}
	// La spec entra a la rama con su propio commit.
	spec := "specs/" + id + "-validacion-por-campo/spec.md"
	if last := git(t, r.dir, "log", "-1", "--format=%s", "--name-only"); last != "docs: "+id+" spec\n\n"+spec {
		t.Errorf("commit de la spec:\n%s", last)
	}

	os.WriteFile(filepath.Join(r.dir, "internal", "b.go"), []byte("package a\n"), 0o644)
	git(t, r.dir, "add", "internal")
	git(t, r.dir, "commit", "-m", "feat: validación")

	// DONE exige las tareas marcadas.
	specAbs := filepath.Join(r.dir, filepath.FromSlash(spec))
	b, _ := os.ReadFile(specAbs)
	doc := strings.Replace(string(b), "## Tasks\n", "## Tasks\n\n- [x] T1 validar RFC\n- [ ] T2 mensaje de error\n", 1)
	os.WriteFile(specAbs, []byte(doc), 0o644)
	env = r.run("report", "--agent", "implementer", "--verdict", "DONE")
	if env.exit != 2 || env.Code != "tasks_open" || !strings.Contains(env.Data["reason"].(string), "T2 mensaje de error") {
		t.Fatalf("DONE con tareas abiertas: exit %d %s %v", env.exit, env.Code, env.Data)
	}
	os.WriteFile(specAbs, []byte(strings.Replace(doc, "- [ ] T2", "- [x] T2", 1)), 0o644)
	r.ok("report", "--agent", "implementer", "--verdict", "DONE") // tarea activa por rama
	r.ok("report", "--agent", "reviewer", "--verdict", "APPROVED")
	os.MkdirAll(filepath.Join(r.dir, ".bflow", "tasks", id, "reports"), 0o755)
	os.WriteFile(filepath.Join(r.dir, ".bflow", "tasks", id, "reports", "review-map.md"), []byte("🔴 internal/b.go: validación nueva\n"), 0o644)
	r.ok("report", "--agent", "documenter", "--verdict", "DONE")
	r.ok("approve", "--gate", "questions") // saltar las preguntas de producto
	env = r.ok("approve", "--gate", "walkthrough")
	pr, _ := env.Data["pr"].(map[string]any)
	if pr == nil || pr["number"].(float64) != 41 {
		t.Fatalf("PR: %v", env.Data)
	}
	if !strings.Contains(git(t, remote, "branch", "--list", branch), branch) {
		t.Error("la rama no llegó al remoto")
	}
	// Las tareas marcadas (la spec sin commitear) también llegan al PR.
	if log := git(t, remote, "log", branch, "--format=%s"); !strings.Contains(log, "docs: "+id+" actualiza la spec") {
		t.Errorf("commits en el remoto:\n%s", log)
	}
	if body := gh.get(41, "body").(string); !strings.Contains(body, "validación nueva") || !strings.Contains(body, "Spec: `specs/"+id) {
		t.Errorf("cuerpo del PR:\n%s", body)
	}

	// bflow pr no abre otro.
	r.ok("pr", id)
	if n := gh.count(); n != 1 {
		t.Errorf("pr duplicó el PR: %d", n)
	}

	// Merge en GitHub → panel cierra la tarea.
	gh.mu.Lock()
	gh.prs[41]["state"], gh.prs[41]["merged"] = "closed", true
	gh.mu.Unlock()
	env = r.ok("panel")
	if closed, _ := env.Data["closed"].([]any); len(closed) != 1 || closed[0] != id {
		t.Fatalf("panel: %v", env.Data)
	}
	if ph := r.ok("status", id).Data["task"].(map[string]any)["phase"]; ph != "done" {
		t.Errorf("fase %v", ph)
	}
}

func TestPRDegradesWithoutToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	r, _ := gitRepo(t, "vcs: { host: github, repo: acme/app, base_branch: dev }\n")
	id := r.ok("task", "add", "Hotfix").Data["id"].(string)
	r.ok("start", id, "--lane", "hotfix")
	r.ok("report", "--agent", "implementer", "--verdict", "DONE")
	r.ok("report", "--agent", "reviewer", "--verdict", "APPROVED")
	r.ok("report", "--agent", "documenter", "--verdict", "DONE")
	r.ok("approve", "--gate", "questions")
	env := r.ok("approve", "--gate", "walkthrough")
	pr, _ := env.Data["pr"].(map[string]any)
	want := "https://github.com/acme/app/compare/dev...hotfix/" + id + "-hotfix?expand=1"
	if pr == nil || pr["url"] != want {
		t.Errorf("sin token debe dar la URL de compare %s: %v", want, env.Data)
	}
	if _, err := os.Stat(filepath.Join(r.dir, ".bflow", "tasks", id, "pr-body.md")); err != nil {
		t.Error("falta pr-body.md")
	}
}

// get y count leen bajo el mismo lock que usa el servidor falso.
func (g *ghFake) get(n int, key string) any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.prs[n][key]
}

func (g *ghFake) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.prs)
}
