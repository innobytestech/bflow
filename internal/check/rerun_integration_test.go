package check

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitad "innobytes.tech/bflow/internal/adapters/vcs/git"
	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/testutil"
)

// gitRepo arma un repo con origin/dev en el commit base y una rama que toca
// touch; node_modules queda ignorado, como en un proyecto de Node.
func gitRepo(t *testing.T, touch string) string {
	t.Helper()
	dir := testutil.TempDir(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(f, c string) {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "dev")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("config", "core.autocrlf", "false")
	write(".gitignore", "node_modules/\n")
	write(navSpec, "spec")
	write("src/app/core/navbar/navbar.component.ts", "comp")
	write("src/app/features/emp/emp.ts", "v1")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("update-ref", "refs/remotes/origin/dev", "HEAD")
	run("checkout", "-q", "-b", "feat")
	write(touch, "v2")
	run("add", ".")
	run("commit", "-q", "-m", "rama")
	write("node_modules/pkg/index.js", "module.exports = 1")
	return dir
}

func realRerunner(t *testing.T, dir string, onBase func(wt string)) *Runner {
	t.Helper()
	fails := []string{navSpec + " > Navbar > a", navSpec + " > Navbar > b"}
	exec := func(_ context.Context, d string, argv []string, _ string) (string, int, error) {
		if d == dir {
			return vitestOut(900, fails...), 1, nil
		}
		if onBase != nil {
			onBase(d)
		}
		return vitestOut(1, fails...), 1, nil
	}
	g := gitad.New(dir)
	return &Runner{Root: dir, Base: "origin/dev", Git: g, Exec: exec,
		Cfg: config.Check{Steps: []config.Step{{Name: "test", Run: "npm test -- --watch=false",
			Rerun: &config.Rerun{Run: "npm test -- --watch=false {files}", Each: "--include={file}"}}}}}
}

func worktrees(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRerunRealGitWorktreeAndJunction(t *testing.T) {
	if testing.Short() {
		t.Skip("usa git de verdad")
	}
	dir := gitRepo(t, "src/app/features/emp/emp.ts")
	var wt string
	r := realRerunner(t, dir, func(d string) {
		wt = d
		if l := worktrees(t, dir); !strings.Contains(l, "locked "+lockReason) {
			t.Errorf("el worktree temporal debe estar bloqueado mientras corre:\n%s", l)
		}
		if b, err := os.ReadFile(filepath.Join(d, filepath.FromSlash(navSpec))); err != nil || string(b) != "spec" {
			t.Errorf("la base debe traer el spec: %q %v", b, err)
		}
		if b, err := os.ReadFile(filepath.Join(d, "src", "app", "features", "emp", "emp.ts")); err != nil || string(b) != "v1" {
			t.Errorf("la base debe traer el código de la base, no el de la rama: %q %v", b, err)
		}
		if b, err := os.ReadFile(filepath.Join(d, "node_modules", "pkg", "index.js")); err != nil || string(b) != "module.exports = 1" {
			t.Errorf("la base debe ver node_modules por el enlace: %q %v", b, err)
		}
	})
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := res.Steps[0]
	if res.Result != "PASS" || !s.Rerun.Accepted() || s.Rerun.Cleanup != "" {
		t.Fatalf("%s %+v", res.Result, s.Rerun)
	}
	if wt == "" {
		t.Fatal("no corrió en la base")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("el worktree temporal sigue en disco: %v", err)
	}
	if strings.Count(worktrees(t, dir), "worktree ") != 1 {
		t.Fatalf("git sigue registrando el worktree temporal:\n%s", worktrees(t, dir))
	}
	if b, err := os.ReadFile(filepath.Join(dir, "node_modules", "pkg", "index.js")); err != nil || string(b) != "module.exports = 1" {
		t.Fatalf("node_modules del repo debe quedar intacto: %q %v", b, err)
	}
}

// Soltar el worktree temporal no poda los demás worktrees del repo, aunque su
// carpeta no esté a la mano (otra unidad, una carpeta movida).
func TestRerunRealGitKeepsOtherWorktrees(t *testing.T) {
	if testing.Short() {
		t.Skip("usa git de verdad")
	}
	dir := gitRepo(t, "src/app/features/emp/emp.ts")
	other := filepath.Join(testutil.TempDir(t), "otro")
	cmd := exec.Command("git", "worktree", "add", "--detach", other, "HEAD")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	res, err := realRerunner(t, dir, nil).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Result != "PASS" || res.Steps[0].Rerun.Cleanup != "" {
		t.Fatalf("%s %+v", res.Result, res.Steps[0].Rerun)
	}
	if l := worktrees(t, dir); strings.Count(l, "worktree ") != 2 || strings.Contains(l, baseDirPrefix) {
		t.Fatalf("debe quedar solo el otro worktree, sin podar:\n%s", l)
	}
}

func TestRerunRealGitRejectsNeighbourWithoutWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("usa git de verdad")
	}
	dir := gitRepo(t, "src/app/core/navbar/navbar.component.ts")
	ran := false
	r := realRerunner(t, dir, func(string) { ran = true })
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := res.Steps[0]
	if res.Result != "FAIL" || s.Rerun == nil || !strings.Contains(s.Rerun.Why, "navbar.component.ts") || ran {
		t.Fatalf("%s %+v ran=%v", res.Result, s.Rerun, ran)
	}
	if strings.Count(worktrees(t, dir), "worktree ") != 1 {
		t.Fatalf("no debió crear worktree:\n%s", worktrees(t, dir))
	}
}
