package check

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/testutil"
)

type realGit struct{ dir string }

func (g realGit) git(args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
func (g realGit) HeadSHA(context.Context) (string, error) { return g.git("rev-parse", "HEAD"), nil }
func (g realGit) Dirty(_ context.Context, p []string) ([]string, error) {
	out := g.git(append([]string{"status", "--porcelain", "--"}, p...)...)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
func (g realGit) DiffNames(context.Context, string) ([]string, error) { return nil, nil }
func (g realGit) ChangedSince(_ context.Context, sha string, p []string) ([]string, error) {
	out := g.git(append([]string{"diff", "--name-only", sha, "HEAD", "--"}, p...)...)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// TestRealGoModule corre go vet y go test de verdad sobre un módulo con un
// paquete que usa "BD" (por la regex) y una prueba que falla.
func TestRealGoModule(t *testing.T) {
	if testing.Short() {
		t.Skip("compila código Go real")
	}
	dir := testutil.TempDir(t)
	files := map[string]string{
		"go.mod":                 "module demo\n\ngo 1.21\n",
		"internal/a/a.go":        "package a\n\nfunc Suma(x, y int) int { return x + y }\n",
		"internal/a/a_test.go":   "package a\n\nimport \"testing\"\n\nfunc TestSuma(t *testing.T) {\n\tif Suma(1, 1) != 3 {\n\t\tt.Fatal(\"esperaba 3\")\n\t}\n}\n",
		"internal/db/db.go":      "package db\n",
		"internal/db/db_test.go": "package db\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestDSN(t *testing.T) { _ = os.Getenv(\"TEST_POSTGRES_DSN\") }\n",
	}
	for f, c := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(c), 0o644)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	r := &Runner{Root: dir, Base: "HEAD", Git: realGit{dir}, Exec: OSExec, LookPath: exec.LookPath, CGO: func() bool { return false },
		Cfg: config.Check{CodePaths: []string{"internal", "go.mod"}, Steps: []config.Step{
			{Name: "vet", Run: "go vet ./..."},
			{Name: "test-nodb", Run: "go test -count=1 {race} {pkgs_nodb}"},
			{Name: "test-db", Run: "go test -p 1 -count=1 {pkgs_db}"},
		}},
		Packages: func(ctx context.Context) ([]GoPackage, error) {
			return GoPackages(ctx, dir, `TEST_POSTGRES_DSN`, []string{"/internal/dbtest"})
		},
	}
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]StepResult{}
	for _, s := range res.Steps {
		st[s.Name] = s
	}
	if st["vet"].Status != "pass" || st["test-db"].Status != "pass" || st["test-nodb"].Status != "fail" {
		t.Fatalf("estados: %+v", res.Steps)
	}
	if !strings.Contains(st["test-nodb"].Tail, "--- FAIL: TestSuma") || !strings.Contains(st["test-nodb"].Tail, "esperaba 3") {
		t.Errorf("extracto:\n%s", st["test-nodb"].Tail)
	}
	pkgs, _ := r.Packages(context.Background())
	class := map[string]bool{}
	for _, p := range pkgs {
		class[p.ImportPath] = p.DB
	}
	if class["demo/internal/db"] != true || class["demo/internal/a"] != false {
		t.Errorf("clasificación con/sin BD: %v", class)
	}
	if ok, _ := Verify(context.Background(), realGit{dir}, r.Cfg.CodePaths, &res); ok {
		t.Error("un check FAIL no verifica")
	}
}
