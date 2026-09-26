package check

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/testutil"
)

// fakeExec registra lo que se ejecuta y responde según el comando.
type fakeExec struct {
	calls   [][]string
	shells  []string
	results map[string]struct {
		out  string
		exit int
	}
}

func (f *fakeExec) run(_ context.Context, _ string, argv []string, shell string) (string, int, error) {
	key := shell
	if argv != nil {
		f.calls = append(f.calls, argv)
		key = strings.Join(argv, " ")
	} else {
		f.shells = append(f.shells, shell)
	}
	for prefix, r := range f.results {
		if strings.HasPrefix(key, prefix) {
			return r.out, r.exit, nil
		}
	}
	return "ok", 0, nil
}

type fakeGit struct {
	sha     string
	dirty   []string
	changed []string
	diff    []string
}

func (g *fakeGit) HeadSHA(context.Context) (string, error)             { return g.sha, nil }
func (g *fakeGit) Dirty(context.Context, []string) ([]string, error)   { return g.dirty, nil }
func (g *fakeGit) DiffNames(context.Context, string) ([]string, error) { return g.diff, nil }
func (g *fakeGit) ChangedSince(context.Context, string, []string) ([]string, error) {
	return g.changed, nil
}

func runner(t *testing.T, steps []config.Step, fx *fakeExec, g *fakeGit) *Runner {
	t.Helper()
	root := testutil.TempDir(t)
	return &Runner{
		Root: root, Base: "origin/dev", Git: g, Exec: fx.run,
		Cfg:      config.Check{Steps: steps, CodePaths: []string{"cmd", "internal"}},
		LookPath: func(name string) (string, error) { return "", errors.New("no está") },
		CGO:      func() bool { return false },
		Now:      func() time.Time { return time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC) },
		Packages: func(context.Context) ([]GoPackage, error) {
			return []GoPackage{{ImportPath: "m/internal/a"}, {ImportPath: "m/internal/db", DB: true}}, nil
		},
	}
}

func TestRunPassAndFailWithTail(t *testing.T) {
	out := "ok  m/internal/a\n--- FAIL: TestCAS (0.01s)\n    cas_test.go:42: esperaba conflicto\nFAIL\nFAIL m/internal/db\ncoverage: 80%"
	fx := &fakeExec{results: map[string]struct {
		out  string
		exit int
	}{"go test": {out, 1}}}
	r := runner(t, []config.Step{{Name: "vet", Run: "go vet ./..."}, {Name: "test", Run: "go test ./..."}}, fx, &fakeGit{sha: "abc123"})
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Result != "FAIL" || res.Steps[0].Status != "pass" || res.Steps[1].Status != "fail" || res.Steps[1].Exit != 1 {
		t.Fatalf("resultado: %+v", res)
	}
	tail := res.Steps[1].Tail
	if !strings.HasPrefix(tail, "--- FAIL: TestCAS") || !strings.Contains(tail, "cas_test.go:42") || !strings.Contains(tail, "coverage: 80%") {
		t.Errorf("el extracto debe empezar por las líneas de fallo y terminar con el final:\n%s", tail)
	}
	if res.SHA != "abc123" {
		t.Errorf("sha %s", res.SHA)
	}
}

func TestDirtyMarksSHA(t *testing.T) {
	r := runner(t, []config.Step{{Name: "vet", Run: "go vet ./..."}}, &fakeExec{}, &fakeGit{sha: "abc", dirty: []string{"internal/x.go"}})
	res, _ := r.Run(context.Background())
	if res.SHA != "DIRTY" {
		t.Errorf("con cambios sin commitear el sha es DIRTY: %s", res.SHA)
	}
}

func TestPlaceholdersAndPackageSplit(t *testing.T) {
	fx := &fakeExec{}
	r := runner(t, []config.Step{
		{Name: "test-nodb", Run: "go test -p 6 {race} -count=1 {pkgs_nodb}"},
		{Name: "test-db", Run: "go test -p 1 {race} -count=1 {pkgs_db}"},
		{Name: "lint", Run: "golangci-lint run --new-from-rev={base}"},
		{Name: "build", Run: "go build -o {devnull} ./cmd/api"},
	}, fx, &fakeGit{sha: "a"})
	res, _ := r.Run(context.Background())
	got := []string{}
	for _, c := range fx.calls {
		got = append(got, strings.Join(c, " "))
	}
	want := []string{"go test -p 6 -count=1 m/internal/a", "go test -p 1 -count=1 m/internal/db", "golangci-lint run --new-from-rev=origin/dev"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("comando %d: %q, want %q", i, got[i], w)
		}
	}
	if !strings.Contains(got[3], "go build -o ") || strings.Contains(got[3], "{devnull}") {
		t.Errorf("devnull: %q", got[3])
	}
	if len(res.Degraded) != 1 || !strings.Contains(res.Degraded[0], "-race") {
		t.Errorf("sin cgo se degrada una sola vez: %v", res.Degraded)
	}
	if res.Result != "PASS" {
		t.Errorf("result %s", res.Result)
	}
}

func TestEmptyPackageListSkips(t *testing.T) {
	fx := &fakeExec{}
	r := runner(t, []config.Step{{Name: "test-db", Run: "go test -p 1 {pkgs_db}"}}, fx, &fakeGit{sha: "a"})
	r.Packages = func(context.Context) ([]GoPackage, error) { return []GoPackage{{ImportPath: "m/a"}}, nil }
	res, _ := r.Run(context.Background())
	if res.Steps[0].Status != "skip" || len(fx.calls) != 0 {
		t.Errorf("sin paquetes con BD no se corre go test sin argumentos (probaría el directorio actual): %+v %v", res.Steps[0], fx.calls)
	}
}

func TestOptionalMissingToolDegrades(t *testing.T) {
	r := runner(t, []config.Step{
		{Name: "secrets", Run: "gitleaks detect --no-banner --redact", Needs: "gitleaks", Optional: true},
		{Name: "lint", Run: "golangci-lint run", Needs: "golangci-lint"},
	}, &fakeExec{}, &fakeGit{sha: "a"})
	res, _ := r.Run(context.Background())
	if res.Steps[0].Status != "skip" || !strings.Contains(strings.Join(res.Degraded, ";"), "gitleaks") {
		t.Errorf("opcional ausente: %+v %v", res.Steps[0], res.Degraded)
	}
	if res.Steps[1].Status != "fail" || !strings.Contains(res.Steps[1].Tail, "golangci-lint") || res.Result != "FAIL" {
		t.Errorf("obligatorio ausente falla: %+v", res.Steps[1])
	}
}

func TestShellOnlyWithOperators(t *testing.T) {
	fx := &fakeExec{}
	r := runner(t, []config.Step{{Name: "a", Run: "npm run check:architecture"}, {Name: "b", Run: "npm test && npm run build"}}, fx, &fakeGit{sha: "a"})
	r.Run(context.Background())
	if len(fx.calls) != 1 || len(fx.shells) != 1 || fx.shells[0] != "npm test && npm run build" {
		t.Errorf("directo: %v · shell: %v", fx.calls, fx.shells)
	}
}

func TestAcceptedVulns(t *testing.T) {
	vulnOut := "Vulnerability #1: GO-2026-6452\n    excelize\nVulnerability #2: GO-2026-7000\n"
	cases := []struct {
		name, accepted, status string
		degraded               bool
	}{
		{"todas aceptadas y vigentes", `{"accepted":[{"id":"GO-2026-6452","revisar":"2026-12-17","seguimiento":"API-162"},{"id":"GO-2026-7000","revisar":"2027-01-01"}]}`, "pass", true},
		{"una sin aceptar", `{"accepted":[{"id":"GO-2026-6452","revisar":"2026-12-17"}]}`, "fail", false},
		{"una vencida", `{"accepted":[{"id":"GO-2026-6452","revisar":"2026-09-01"},{"id":"GO-2026-7000","revisar":"2027-01-01"}]}`, "fail", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := &fakeExec{results: map[string]struct {
				out  string
				exit int
			}{"govulncheck": {vulnOut, 3}}}
			r := runner(t, []config.Step{{Name: "vulns", Run: "govulncheck ./...", Accept: "accepted-vulns.json"}}, fx, &fakeGit{sha: "a"})
			os.WriteFile(filepath.Join(r.Root, "accepted-vulns.json"), []byte(c.accepted), 0o644)
			res, _ := r.Run(context.Background())
			if res.Steps[0].Status != c.status {
				t.Errorf("status %s, want %s (tail %q)", res.Steps[0].Status, c.status, res.Steps[0].Tail)
			}
			if (len(res.Degraded) > 0) != c.degraded {
				t.Errorf("degradado: %v", res.Degraded)
			}
			if c.status == "fail" && !strings.Contains(res.Steps[0].Tail, "GO-2026-") {
				t.Errorf("el motivo debe nombrar la vulnerabilidad: %q", res.Steps[0].Tail)
			}
		})
	}
}

func TestBaselineNewOnly(t *testing.T) {
	out := "src/app/old.component.html:10 control nativo\nsrc/app/features/x/nuevo.component.html:3 control nativo\n"
	fx := &fakeExec{results: map[string]struct {
		out  string
		exit int
	}{"npm": {out, 1}}}
	g := &fakeGit{sha: "a", diff: []string{"src/app/features/x/nuevo.component.html"}}
	r := runner(t, []config.Step{{Name: "ui", Run: "npm run check:ui:full", Baseline: "new-only"}}, fx, g)
	res, _ := r.Run(context.Background())
	if res.Steps[0].Status != "fail" || !strings.Contains(res.Steps[0].Tail, "nuevo.component.html") || strings.Contains(res.Steps[0].Tail, "old.component") {
		t.Errorf("solo cuentan hallazgos en archivos tocados: %+v", res.Steps[0])
	}
	g.diff = []string{"src/otro.ts"}
	res, _ = r.Run(context.Background())
	if res.Steps[0].Status != "pass" {
		t.Errorf("hallazgos solo en archivos no tocados: %+v", res.Steps[0])
	}
}

func TestQuick(t *testing.T) {
	fx := &fakeExec{}
	r := runner(t, nil, fx, &fakeGit{sha: "a"})
	r.Cfg.Quick = []string{"go vet {pkg}", "go test -count=1 {pkg}"}
	res, _ := r.Quick(context.Background(), "./internal/a/...")
	if len(fx.calls) != 2 || strings.Join(fx.calls[1], " ") != "go test -count=1 ./internal/a/..." || res.Result != "PASS" {
		t.Errorf("quick: %v %+v", fx.calls, res)
	}
}

func TestVerify(t *testing.T) {
	g := &fakeGit{sha: "abc"}
	ctx := context.Background()
	paths := []string{"internal"}
	cases := []struct {
		name string
		res  *Result
		git  fakeGit
		ok   bool
		want string
	}{
		{"sin check", nil, fakeGit{}, false, "no hay check"},
		{"corrió sucio", &Result{SHA: "DIRTY", Result: "PASS"}, fakeGit{}, false, "sin commitear"},
		{"falló", &Result{SHA: "abc", Result: "FAIL"}, fakeGit{}, false, "no pasó"},
		{"cambios después", &Result{SHA: "abc", Result: "PASS"}, fakeGit{changed: []string{"internal/x.go"}}, false, "internal/x.go"},
		{"sucio ahora", &Result{SHA: "abc", Result: "PASS"}, fakeGit{dirty: []string{"internal/y.go"}}, false, "internal/y.go"},
		{"ok", &Result{SHA: "abc", Result: "PASS"}, fakeGit{}, true, "abc"},
	}
	for _, c := range cases {
		*g = c.git
		ok, detail := Verify(ctx, g, paths, c.res)
		if ok != c.ok || !strings.Contains(detail, c.want) {
			t.Errorf("%s: ok=%v detail=%q", c.name, ok, detail)
		}
	}
}

func TestReportMarkdown(t *testing.T) {
	res := Result{SHA: "abc", Result: "FAIL", Date: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC), Degraded: []string{"sin cgo: -race omitido"},
		Steps: []StepResult{{Name: "vet", Status: "pass", Secs: 1.2}, {Name: "test", Status: "fail", Exit: 1, Secs: 3, Cmd: "go test ./...", Tail: "--- FAIL: X"}}}
	md := res.Markdown()
	for _, w := range []string{"sha: abc", "result: FAIL (DEGRADADO)", "| vet | pass | 1.2 |", "## ❌ test", "--- FAIL: X", "sin cgo"} {
		if !strings.Contains(md, w) {
			t.Errorf("falta %q en:\n%s", w, md)
		}
	}
	if s := res.Summary(); !strings.HasPrefix(s, "check FAIL") || !strings.Contains(s, "test") {
		t.Errorf("resumen: %s", s)
	}
}
