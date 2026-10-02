package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/testutil"
)

// sh corre git en dir y falla la prueba si no funciona.
func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture: un remoto bare con rama dev y un clon de trabajo.
func fixture(t *testing.T) (work, remote string) {
	t.Helper()
	root := testutil.TempDir(t)
	remote = filepath.Join(root, "remote.git")
	seed := filepath.Join(root, "seed")
	work = filepath.Join(root, "work")
	sh(t, root, "init", "--bare", "-b", "main", remote)
	sh(t, root, "init", "-b", "main", seed)
	write(t, seed, "internal/a.go", "package a\n")
	write(t, seed, "README.md", "hola\n")
	sh(t, seed, "add", ".")
	sh(t, seed, "commit", "-m", "inicio")
	sh(t, seed, "checkout", "-b", "dev")
	sh(t, seed, "remote", "add", "origin", remote)
	sh(t, seed, "push", "origin", "main", "dev")
	sh(t, root, "clone", remote, work)
	sh(t, work, "config", "user.name", "Ana Dev")
	sh(t, work, "config", "user.email", "a@a")
	return work, remote
}

func TestEnsureBranchCreatesFromBase(t *testing.T) {
	work, _ := fixture(t)
	g := New(work)
	ctx := context.Background()
	how, err := g.EnsureBranch(ctx, "feature/X-1-demo", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if how != "created" {
		t.Errorf("how=%s", how)
	}
	if br, _ := g.CurrentBranch(ctx); br != "feature/X-1-demo" {
		t.Errorf("rama actual %s", br)
	}
	if base := sh(t, work, "merge-base", "HEAD", "origin/dev"); base != sh(t, work, "rev-parse", "origin/dev") {
		t.Error("la rama debe salir de dev")
	}
}

func TestEnsureBranchResumesAndTracks(t *testing.T) {
	work, remote := fixture(t)
	g := New(work)
	ctx := context.Background()
	if _, err := g.EnsureBranch(ctx, "feature/X-1-demo", "dev"); err != nil {
		t.Fatal(err)
	}
	sh(t, work, "checkout", "dev")
	how, err := g.EnsureBranch(ctx, "feature/X-1-demo", "dev")
	if err != nil || how != "resumed" {
		t.Errorf("retomar: %s %v", how, err)
	}

	// Existe solo en el remoto (otro clon la empujó).
	other := filepath.Join(filepath.Dir(work), "other")
	sh(t, filepath.Dir(work), "clone", remote, other)
	sh(t, other, "checkout", "-b", "feature/X-2-otra", "origin/dev")
	sh(t, other, "push", "origin", "feature/X-2-otra")
	how, err = g.EnsureBranch(ctx, "feature/X-2-otra", "dev")
	if err != nil || how != "tracked" {
		t.Errorf("trackear: %s %v", how, err)
	}
}

func TestEnsureBranchWithoutRemote(t *testing.T) {
	dir := testutil.TempDir(t)
	sh(t, dir, "init", "-b", "main")
	write(t, dir, "a.txt", "x")
	sh(t, dir, "add", ".")
	sh(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "x")
	g := New(dir)
	how, err := g.EnsureBranch(context.Background(), "feature/L-1", "")
	if err != nil || how != "created" {
		t.Errorf("sin remoto ni base: %s %v", how, err)
	}
}

func TestDirtyOnlyInPaths(t *testing.T) {
	work, _ := fixture(t)
	g := New(work)
	ctx := context.Background()
	write(t, work, "README.md", "cambio fuera del código\n")
	d, err := g.Dirty(ctx, []string{"internal", "cmd"})
	if err != nil || len(d) != 0 {
		t.Errorf("cambios fuera de code_paths no cuentan: %v %v", d, err)
	}
	write(t, work, "internal/a.go", "package a // cambio\n")
	write(t, work, "internal/nuevo.go", "package a\n")
	d, _ = g.Dirty(ctx, []string{"internal", "cmd"})
	slices.Sort(d)
	if strings.Join(d, ",") != "internal/a.go,internal/nuevo.go" {
		t.Errorf("dirty: %v", d)
	}
}

func TestShaDiffAndPush(t *testing.T) {
	work, remote := fixture(t)
	g := New(work)
	ctx := context.Background()
	if _, err := g.EnsureBranch(ctx, "feature/X-1", "dev"); err != nil {
		t.Fatal(err)
	}
	sha, err := g.HeadSHA(ctx)
	if err != nil || len(sha) != 40 {
		t.Fatalf("sha %q %v", sha, err)
	}
	write(t, work, "internal/b.go", "package a\n")
	write(t, work, "docs/x.md", "x\n")
	sh(t, work, "add", ".")
	sh(t, work, "commit", "-m", "b")
	changed, err := g.ChangedSince(ctx, sha, []string{"internal"})
	if err != nil || strings.Join(changed, ",") != "internal/b.go" {
		t.Errorf("changedSince: %v %v", changed, err)
	}
	names, err := g.DiffNames(ctx, "origin/dev")
	slices.Sort(names)
	if err != nil || strings.Join(names, ",") != "docs/x.md,internal/b.go" {
		t.Errorf("diffNames: %v %v", names, err)
	}
	if err := g.Push(ctx, "feature/X-1"); err != nil {
		t.Fatal(err)
	}
	if out := sh(t, remote, "branch", "--list", "feature/X-1"); !strings.Contains(out, "feature/X-1") {
		t.Errorf("push no llegó al remoto: %q", out)
	}
	url, err := g.RemoteURL(ctx)
	if err != nil || url == "" {
		t.Errorf("remote url: %q %v", url, err)
	}
	if u := g.UserName(ctx); u != "Ana Dev" {
		t.Errorf("usuario: %q", u)
	}
}

func TestProtectedPush(t *testing.T) {
	work, _ := fixture(t)
	g := New(work)
	g.Protected = []string{"main", "master", "dev"}
	if err := g.Push(context.Background(), "dev"); err == nil {
		t.Error("bflow no empuja ramas protegidas")
	}
}

func TestParseRepo(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/acme-api.git": "github.com acme/acme-api",
		"git@github.com:acme/acme-web.git":     "github.com acme/acme-web",
		"ssh://git@github.com/innobytes/bflow": "github.com innobytes/bflow",
		"https://gitea.acme.io/team/app":       "gitea.acme.io team/app",
	}
	for in, want := range cases {
		host, repo, ok := ParseRemote(in)
		if !ok || host+" "+repo != want {
			t.Errorf("ParseRemote(%q) = %q %q %v", in, host, repo, ok)
		}
	}
	if _, _, ok := ParseRemote("/tmp/remote.git"); ok {
		t.Error("una ruta local no es un host remoto")
	}
}

// Commit toma solo las rutas pedidas, aunque haya otras cosas en stage.
func TestCommitOnlyPaths(t *testing.T) {
	work, _ := fixture(t)
	g := New(work)
	ctx := context.Background()
	write(t, work, "specs/API-1-x/spec.md", "# spec\n")
	write(t, work, "internal/a.go", "package a // del implementer\n")
	sh(t, work, "add", "internal/a.go")

	done, err := g.Commit(ctx, []string{"specs/API-1-x/spec.md"}, "docs: API-1 spec")
	if err != nil || !done {
		t.Fatalf("commit: %v %v", done, err)
	}
	if files := sh(t, work, "show", "--name-only", "--format=%s", "HEAD"); files != "docs: API-1 spec\n\nspecs/API-1-x/spec.md" {
		t.Errorf("el commit trae otras cosas:\n%s", files)
	}
	if st := sh(t, work, "status", "--porcelain"); st != "M  internal/a.go" {
		t.Errorf("lo que estaba en stage sigue ahí: %q", st)
	}
	if done, err := g.Commit(ctx, []string{"specs/API-1-x/spec.md"}, "docs: otra vez"); done || err != nil {
		t.Errorf("sin cambios no hay commit: %v %v", done, err)
	}
}

// R6: DiffLines suma agregadas y borradas de base...HEAD; un binario cuenta 0.
func TestDiffLines(t *testing.T) {
	work, _ := fixture(t)
	g := New(work)
	ctx := context.Background()
	if _, err := g.EnsureBranch(ctx, "feature/X-2", "dev"); err != nil {
		t.Fatal(err)
	}
	if n, err := g.DiffLines(ctx, "origin/dev"); err != nil || n != 0 {
		t.Fatalf("sin cambios: %d %v", n, err)
	}
	write(t, work, "internal/b.go", "package a\nvar B = 1\n") // +2
	write(t, work, "README.md", "hola\nmás\n")                // +1
	sh(t, work, "rm", "internal/a.go")                        // -1
	write(t, work, "img.bin", "\x00\x01\x02\x00")             // binario: 0
	sh(t, work, "add", ".")
	sh(t, work, "commit", "-m", "cambios")
	n, err := g.DiffLines(ctx, "origin/dev")
	if err != nil || n != 4 {
		t.Errorf("DiffLines = %d, %v; quiero 4 (2 + 1 + 1, binario 0)", n, err)
	}
	if _, err := g.DiffLines(ctx, "origin/no-existe"); err == nil {
		t.Error("una base que no existe es un error")
	}
}
