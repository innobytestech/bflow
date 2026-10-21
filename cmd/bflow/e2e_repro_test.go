package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const reproYAML = "stack: go\ncheck:\n  steps:\n    - { name: test, run: \"go test -count=1 ./...\" }\n"

// reproRepo deja un repo con un defecto: A() devuelve 0 y debería devolver 1.
// Con baseBranch, la rama del hotfix sale de origin/dev; sin él, el rango de
// commits arranca en el HEAD guardado al crear la rama.
func reproRepo(t *testing.T, baseBranch bool) (*repo, string) {
	t.Helper()
	yaml := reproYAML
	if baseBranch {
		yaml += "vcs: { base_branch: dev }\n"
	}
	r, _ := gitRepo(t, yaml)
	writeFixture(t, filepath.Join(r.dir, "go.mod"), "module demo\n\ngo 1.21\n")
	writeFixture(t, filepath.Join(r.dir, "internal", "a.go"), "package a\n\nfunc A() int { return 0 }\n")
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "base")
	if baseBranch {
		git(t, r.dir, "push", "origin", "dev")
	}
	id := r.ok("task", "add", "Demo").Data["id"].(string)
	r.ok("start", id, "--lane", "hotfix")
	return r, id
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitFile(t *testing.T, r *repo, rel, content, msg string) {
	t.Helper()
	writeFixture(t, filepath.Join(r.dir, filepath.FromSlash(rel)), content)
	git(t, r.dir, "add", rel)
	git(t, r.dir, "commit", "-m", msg)
}

const (
	reproTest = "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"rojo\")\n\t}\n}\n"
	fixedA    = "package a\n\nfunc A() int { return 1 }\n"
)

func TestHotfixDoneWithoutReproRejected(t *testing.T) {
	r, _ := reproRepo(t, true)
	commitFile(t, r, "internal/a.go", fixedA, "fix: sin prueba")
	r.ok("check")
	env := r.run("report", "--agent", "implementer", "--verdict", "DONE")
	if env.exit != 2 || env.Code != "repro_test_missing" {
		t.Fatalf("sin commit de prueba previo: exit %d %s %v", env.exit, env.Code, env.Data)
	}
	if reason := env.Data["reason"].(string); !strings.Contains(reason, "no hay commit de solo pruebas") {
		t.Errorf("el reason dice qué falta: %s", reason)
	}
}

func TestHotfixReproFailThenPassAccepted(t *testing.T) {
	r, id := reproRepo(t, false)
	commitFile(t, r, "internal/a_test.go", reproTest, "test: reproduce el defecto")
	testSHA := git(t, r.dir, "rev-parse", "HEAD")

	env := r.run("check")
	if env.exit != 1 || env.Data["result"] != "FAIL" {
		t.Fatalf("la prueba nueva debe fallar: %d %v", env.exit, env.Data)
	}
	saved := filepath.Join(r.dir, ".bflow", "tasks", id, "checks", testSHA+".json")
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("el check del commit de prueba queda en el historial: %v", err)
	}

	commitFile(t, r, "internal/a.go", fixedA, "fix: corrige A")
	if env = r.ok("check"); env.Data["result"] != "PASS" {
		t.Fatalf("check en HEAD: %v", env.Data)
	}
	env = r.ok("report", "--agent", "implementer", "--verdict", "DONE")
	if env.Data["phase"] != "quality" {
		t.Fatalf("con prueba que falla y luego pasa, avanza: %v", env.Data)
	}
}

func TestHotfixNoReproOpensHumanGate(t *testing.T) {
	r, _ := reproRepo(t, true)
	commitFile(t, r, "internal/a.go", fixedA, "fix: sin prueba")
	r.ok("check")

	env := r.ok("report", "--agent", "implementer", "--verdict", "DONE", "--no-repro", "el defecto vive en la config")
	if env.Data["phase"] != "implementing" || env.Data["gate"] != "repro" {
		t.Fatalf("--no-repro abre el gate y no avanza: %v", env.Data)
	}
	if env.Next.Action != "ask" || env.Next.Gate != "repro" {
		t.Fatalf("next pregunta al humano: %+v", env.Next)
	}

	env = r.ok("reject", "--gate", "repro", "--note", "escribe la prueba")
	if env.Data["phase"] != "implementing" || env.Data["gate"] != nil && env.Data["gate"] != "" {
		t.Fatalf("rechazar vuelve al implementer sin gate: %v", env.Data)
	}

	env = r.run("report", "--agent", "implementer", "--verdict", "DONE", "--no-repro", "el defecto vive en la config")
	if env.exit != 2 || env.Code != "no_repro_rejected" {
		t.Fatalf("tras el rechazo, --no-repro no se reabre: exit %d %s", env.exit, env.Code)
	}
}

func TestHotfixNoReproApprovedRecordsDecision(t *testing.T) {
	r, id := reproRepo(t, true)
	commitFile(t, r, "internal/a.go", fixedA, "fix: sin prueba")
	r.ok("check")
	r.ok("report", "--agent", "implementer", "--verdict", "DONE", "--no-repro", "el defecto vive en la config")
	env := r.ok("approve", "--gate", "repro")
	if env.Data["phase"] != "quality" {
		t.Fatalf("aprobar avanza a quality: %v", env.Data)
	}
	dec, err := os.ReadFile(filepath.Join(r.dir, ".bflow", "tasks", id, "decisions.md"))
	if err != nil || !strings.Contains(string(dec), "sin prueba de reproducción: el defecto vive en la config") {
		t.Errorf("decisions.md: %v\n%s", err, dec)
	}
	st := r.ok("stats", id).Data["stats"].(map[string]any)
	if st["no_repro"] != "el defecto vive en la config" {
		t.Errorf("stats --json: %v", st["no_repro"])
	}
}

func TestHotfixReproCommitBeforeStartAccepted(t *testing.T) {
	// Con base_branch explícito el merge-base no depende de que git cree origin/HEAD al hacer fetch.
	r, _ := gitRepo(t, reproYAML+"vcs: { base_branch: dev }\n")
	writeFixture(t, filepath.Join(r.dir, "go.mod"), "module demo\n\ngo 1.21\n")
	writeFixture(t, filepath.Join(r.dir, "internal", "a.go"), "package a\n\nfunc A() int { return 0 }\n")
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "base")
	main := git(t, r.dir, "rev-parse", "--abbrev-ref", "HEAD")
	id := r.ok("task", "add", "Demo").Data["id"].(string)

	branch := "hotfix/" + id + "-demo"
	git(t, r.dir, "checkout", "-b", branch)
	commitFile(t, r, "internal/a_test.go", reproTest, "test: reproduce el defecto")
	testSHA := git(t, r.dir, "rev-parse", "HEAD")
	git(t, r.dir, "checkout", main)

	if got := r.ok("start", id, "--lane", "hotfix").Data["branch"]; got != branch {
		t.Fatalf("la rama existente se retoma: %v, want %s", got, branch)
	}
	git(t, r.dir, "checkout", "--detach", testSHA)
	if env := r.run("check"); env.exit != 1 {
		t.Fatalf("la prueba debe fallar: %d %v", env.exit, env.Data)
	}
	git(t, r.dir, "checkout", branch)
	commitFile(t, r, "internal/a.go", fixedA, "fix: corrige A")
	r.ok("check")
	if env := r.ok("report", "--agent", "implementer", "--verdict", "DONE"); env.Data["phase"] != "quality" {
		t.Fatalf("la prueba hecha antes del start cuenta: %v", env.Data)
	}
}
