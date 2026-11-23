package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/testutil"
)

const navSpec = "src/app/core/navbar/navbar.component.spec.ts"

// vitestOut arma una salida del reporter por defecto de vitest con las pruebas
// en rojo fails («archivo > prueba») de total archivos de prueba.
func vitestOut(total int, fails ...string) string {
	var b strings.Builder
	b.WriteString(" ✓ |ms-sys-frontend| src/app/shared/ok.spec.ts (3 tests) 12ms\n")
	var files []string
	for _, f := range fails {
		file, _, _ := strings.Cut(f, " > ")
		if !slices.Contains(files, file) {
			files = append(files, file)
		}
	}
	for _, f := range files {
		fmt.Fprintf(&b, " ❯ |ms-sys-frontend| %s (20 tests | 1 failed) 300ms\n", f)
	}
	if len(fails) > 0 {
		fmt.Fprintf(&b, "\n⎯⎯⎯⎯⎯⎯⎯ Failed Tests %d ⎯⎯⎯⎯⎯⎯⎯\n\n", len(fails))
	}
	for i, f := range fails {
		file, _, _ := strings.Cut(f, " > ")
		fmt.Fprintf(&b, " FAIL   ms-sys-frontend  %s\nAssertionError: expected 1 to be 2\n ❯ %s:%d:5\n\n⎯⎯⎯[%d/%d]⎯\n\n", f, file, 10+i, i+1, len(fails))
	}
	passed := max(total-len(files), 0)
	if len(files) > 0 {
		fmt.Fprintf(&b, " Test Files  %d failed | %d passed (%d)\n", len(files), passed, total)
		fmt.Fprintf(&b, "      Tests  %d failed | 100 passed (%d)\n", len(fails), len(fails)+100)
	} else {
		fmt.Fprintf(&b, " Test Files  %d passed (%d)\n      Tests  100 passed (100)\n", total, total)
	}
	b.WriteString("   Start at  07:00:18\n   Duration  10.5s\n")
	return b.String()
}

type baseGit struct {
	fakeGit
	t         *testing.T
	removed   []string
	untracked []string
	mergeBase string
	baseFiles map[string]string
	addErr    error
	added     []string
	dropped   []string
	shared    []string // rutas que git no ignora
}

func (g *baseGit) DiffRemoved(context.Context, string) ([]string, error) { return g.removed, nil }
func (g *baseGit) Tracked(_ context.Context, paths []string) ([]string, error) {
	return slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return slices.Contains(g.untracked, p) }), nil
}
func (g *baseGit) MergeBase(context.Context, string) (string, error) { return g.mergeBase, nil }
func (g *baseGit) IsIgnored(_ context.Context, p string) (bool, error) {
	return !slices.Contains(g.shared, p), nil
}
func (g *baseGit) Worktrees(context.Context) ([]string, error) {
	return slices.DeleteFunc(slices.Clone(g.added), func(d string) bool { return slices.Contains(g.dropped, d) }), nil
}
func (g *baseGit) WorktreeAddLocked(_ context.Context, dir, sha, reason, hooks string) error {
	if reason == "" {
		g.t.Error("el worktree temporal debe ir bloqueado")
	}
	if es, err := os.ReadDir(hooks); err != nil || len(es) != 0 {
		g.t.Errorf("los ganchos deben apuntar a una carpeta vacía: %v %v", es, err)
	}
	g.added = append(g.added, dir)
	if g.addErr != nil {
		return g.addErr
	}
	for f, c := range g.baseFiles {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			return err
		}
	}
	return nil
}
func (g *baseGit) WorktreeForget(_ context.Context, dir string) error {
	g.dropped = append(g.dropped, dir)
	if _, err := os.Lstat(dir); err == nil {
		g.t.Errorf("se soltó el worktree en git con la carpeta todavía en disco: %s", dir)
		return errors.New("sigue en disco")
	}
	return nil
}

// isLink: un enlace simbólico o un junction (Go reporta este como irregular).
func isLink(fi os.FileInfo) bool { return fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 }

// rerunFixture arma un repo con el spec de la navbar y un Exec que responde
// según corra en la raíz (la rama) o en el worktree de la base.
type rerunFixture struct {
	r        *Runner
	g        *baseGit
	tmp      string
	head     string
	headExit int
	base     string
	baseExit int
	baseErr  error
	onBase   func(dir string, argv []string, shell string)
	baseRuns int
}

func newRerun(t *testing.T) *rerunFixture {
	t.Helper()
	root := testutil.TempDir(t)
	tmp := testutil.TempDir(t)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMPDIR", tmp)
	for _, f := range []string{navSpec, "src/app/core/navbar/navbar.component.ts", "src/app/features/emp/emp.ts"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fx := &rerunFixture{tmp: tmp, headExit: 1, baseExit: 1}
	fx.g = &baseGit{t: t, fakeGit: fakeGit{sha: "abc123", diff: []string{"src/app/features/emp/emp.ts"}}, mergeBase: "base0123456789",
		baseFiles: map[string]string{navSpec: "x"}}
	fx.head = vitestOut(900, navSpec+" > NavbarComponent > campana > con una notificación", navSpec+" > NavbarComponent > campana > con varias")
	fx.base = vitestOut(1, navSpec+" > NavbarComponent > campana > con una notificación", navSpec+" > NavbarComponent > campana > con varias")
	exec := func(_ context.Context, dir string, argv []string, shell string) (string, int, error) {
		if dir == root {
			return fx.head, fx.headExit, nil
		}
		fx.baseRuns++
		if fx.onBase != nil {
			fx.onBase(dir, argv, shell)
		}
		return fx.base, fx.baseExit, fx.baseErr
	}
	fx.r = &Runner{Root: root, Base: "origin/dev", Git: fx.g, Exec: exec,
		Cfg: config.Check{Steps: []config.Step{{Name: "test", Run: "npm test -- --watch=false",
			Rerun: &config.Rerun{Run: "npm test -- --watch=false {files}", Each: "--include={file}"}}}}}
	return fx
}

func (fx *rerunFixture) run(t *testing.T) (Result, StepResult) {
	t.Helper()
	res, err := fx.r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res, res.Steps[0]
}

// leftovers son las carpetas que la corrida dejó en el directorio temporal.
func (fx *rerunFixture) leftovers(t *testing.T) []string {
	t.Helper()
	es, err := os.ReadDir(fx.tmp)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestRerunIgnoresRedAlreadyInBase(t *testing.T) {
	fx := newRerun(t)
	var gotArgv []string
	var gotDir string
	fx.onBase = func(dir string, argv []string, _ string) { gotDir, gotArgv = dir, argv }
	res, s := fx.run(t)
	if res.Result != "PASS" || s.Status != "pass" {
		t.Fatalf("el rojo que ya estaba en la base no debe tumbar el check: %s / %s (%+v)", res.Result, s.Status, s.Rerun)
	}
	if s.Rerun == nil || !s.Rerun.Accepted() || len(s.Rerun.Ignored) != 2 || s.Rerun.Base != "base0123456789" {
		t.Fatalf("rerun = %+v", s.Rerun)
	}
	want := []string{"npm", "test", "--", "--watch=false", "--include=" + navSpec}
	if !slices.Equal(gotArgv, want) {
		t.Fatalf("comando en la base = %q, quiero %q", gotArgv, want)
	}
	tmp, _ := filepath.EvalSymlinks(fx.tmp)
	if filepath.Base(gotDir) != "wt" || !strings.HasPrefix(gotDir, tmp) {
		t.Fatalf("la base no corrió en el worktree temporal: %s", gotDir)
	}
	if len(res.Degraded) != 1 || !strings.Contains(res.Degraded[0], "2 prueba(s) que ya fallaban en la base base0123") || !strings.Contains(res.Degraded[0], "con una notificación") {
		t.Fatalf("lo ignorado debe quedar a la vista: %q", res.Degraded)
	}
	if len(fx.g.dropped) != 1 || len(fx.leftovers(t)) != 0 {
		t.Fatalf("el worktree temporal debe quitarse: dropped=%v quedan=%v", fx.g.dropped, fx.leftovers(t))
	}
	md := res.Markdown()
	if !strings.Contains(md, "rojo que ya estaba en la base base0123 (ignorado)") || !strings.Contains(md, "- "+navSpec+" > NavbarComponent > campana > con varias") {
		t.Fatalf("check.md debe listar lo ignorado:\n%s", md)
	}
	if ig := res.Ignored(); len(ig) != 1 || ig["test"] == nil {
		t.Fatalf("Ignored = %v", ig)
	}
}

func TestRerunRejects(t *testing.T) {
	other := "src/app/shared/other.spec.ts"
	cases := []struct {
		name     string
		setup    func(fx *rerunFixture)
		why      string
		baseRuns int // -1: no importa
	}{
		{"la prueba en rojo está en un archivo tocado", func(fx *rerunFixture) { fx.g.diff = append(fx.g.diff, navSpec) }, "que esta rama tocó", 0},
		{"el archivo en rojo se renombró", func(fx *rerunFixture) { fx.g.removed = []string{navSpec} }, "que esta rama tocó", 0},
		{"el archivo en rojo está sin commitear", func(fx *rerunFixture) { fx.g.dirty = []string{navSpec} }, "que esta rama tocó", 0},
		{"la rama tocó un vecino", func(fx *rerunFixture) { fx.g.diff = append(fx.g.diff, "src/app/core/navbar/navbar.component.html") }, "junto a", 0},
		{"la rama tocó una carpeta que contiene la prueba", func(fx *rerunFixture) { fx.g.diff = []string{"src/app/core"} }, "que esta rama tocó", 0},
		{"la rama tocó el manifiesto de dependencias", func(fx *rerunFixture) { fx.g.diff = append(fx.g.diff, "package.json") }, "package.json", 0},
		{"la rama tocó la config de vitest", func(fx *rerunFixture) { fx.g.diff = append(fx.g.diff, "vitest.config.ts") }, "vitest.config.ts", 0},
		{"la rama tocó un archivo de la raíz", func(fx *rerunFixture) { fx.g.diff = append(fx.g.diff, "angular.json") }, "angular.json", 0},
		{"la traza de la falla nombra un archivo tocado", func(fx *rerunFixture) {
			fx.head = strings.Replace(fx.head, " ❯ "+navSpec+":10:5", " ❯ src/app/features/emp/emp.ts:10:5", 1)
		}, "nombra src/app/features/emp/emp.ts", 0},
		{"una falla está en un archivo tocado y otra no", func(fx *rerunFixture) {
			fx.g.diff = append(fx.g.diff, "src/app/features/emp/emp.spec.ts")
			fx.head = vitestOut(900, navSpec+" > a", "src/app/features/emp/emp.spec.ts > b")
		}, "que esta rama tocó", 0},
		{"una prueba en rojo pasa en la base", func(fx *rerunFixture) {
			fx.base = vitestOut(1, navSpec+" > NavbarComponent > campana > con una notificación")
		}, "falla en la rama y no en la base", 1},
		{"en la base pasa todo", func(fx *rerunFixture) { fx.base, fx.baseExit = vitestOut(1), 0 }, "pasan en la base", 1},
		{"en la base la prueba se llama distinto", func(fx *rerunFixture) {
			fx.base = vitestOut(1, navSpec+" > NavbarComponent > campana > con una notificacion", navSpec+" > NavbarComponent > campana > con varias")
		}, "falla en la rama y no en la base", 1},
		{"la corrida en la base no se filtró", func(fx *rerunFixture) {
			fx.base = vitestOut(900, navSpec+" > NavbarComponent > campana > con una notificación", navSpec+" > NavbarComponent > campana > con varias")
		}, "no quedó filtrada", 1},
		{"la base no trae resumen", func(fx *rerunFixture) { fx.base = "✘ [ERROR] TS2307: Cannot find module '@env/version'\n" }, "en la base", 1},
		{"la base trae errores fuera de las pruebas", func(fx *rerunFixture) { fx.base += "     Errors  1 error\n" }, "en la base", 1},
		{"la base no se pudo correr", func(fx *rerunFixture) { fx.baseErr = errors.New("npm no está") }, "no se pudo correr el comando en la base", 1},
		{"la base cortada por una señal", func(fx *rerunFixture) { fx.baseExit = 137 }, "no terminó sola", 1},
		{"no se pudo crear el worktree", func(fx *rerunFixture) { fx.g.addErr = errors.New("ya existe") }, "no se pudo crear el worktree", 0},
		{"el archivo no está en la base", func(fx *rerunFixture) { fx.g.baseFiles = nil }, "no está en la base", 0},
		{"una falla de archivo completo", func(fx *rerunFixture) {
			fx.head = strings.Replace(fx.head, " FAIL   ms-sys-frontend  "+navSpec+" > NavbarComponent > campana > con varias", " FAIL  "+navSpec+" [ "+navSpec+" ]", 1)
		}, "no es de una prueba con nombre", 0},
		{"el resumen cuenta más fallas de las que nombra", func(fx *rerunFixture) {
			fx.head = strings.Replace(fx.head, "Tests  2 failed", "Tests  3 failed", 1)
		}, "no se pueden atribuir", 0},
		{"el resumen cuenta más archivos en rojo", func(fx *rerunFixture) {
			fx.head = strings.Replace(fx.head, "Test Files  1 failed | 899 passed", "Test Files  2 failed | 898 passed", 1)
		}, "no se pueden atribuir", 0},
		{"sin resumen en la rama", func(fx *rerunFixture) { fx.head = " FAIL  " + navSpec + " > a\n" }, "resumen", 0},
		{"errores no atrapados en la rama", func(fx *rerunFixture) { fx.head += "⎯⎯⎯⎯ Unhandled Rejection ⎯⎯⎯⎯\n" }, "no terminó limpia", 0},
		{"la rama no compiló", func(fx *rerunFixture) { fx.head = "X [ERROR] TS2345: error TS2345 algo\n" + fx.head }, "no terminó limpia", 0},
		{"la rama cortada por una señal", func(fx *rerunFixture) { fx.headExit = 137 }, "no terminó solo", 0},
		{"el paso falló sin pruebas en rojo", func(fx *rerunFixture) { fx.head = vitestOut(900) }, "sin ninguna prueba en rojo", 0},
		{"el archivo en rojo no lo rastrea git", func(fx *rerunFixture) { fx.g.untracked = []string{navSpec} }, "git no rastrea", 0},
		{"el archivo en rojo no existe en el repo", func(fx *rerunFixture) {
			fx.head = vitestOut(900, other+" > x")
		}, "no es de un archivo del repo", 0},
		{"una ruta que no se puede pasar al comando", func(fx *rerunFixture) {
			fx.head = vitestOut(900, "src/app/a&b.spec.ts > x")
		}, "no se puede pasar", 0},
		{"una ruta fuera del repo", func(fx *rerunFixture) {
			fx.head = vitestOut(900, "../otro/x.spec.ts > x")
		}, "no se puede pasar", 0},
		{"sin merge-base", func(fx *rerunFixture) { fx.g.mergeBase = "" }, "merge-base", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newRerun(t)
			c.setup(fx)
			res, s := fx.run(t)
			if res.Result != "FAIL" || s.Status != "fail" {
				t.Fatalf("debió rechazar: %s / %s (%+v)", res.Result, s.Status, s.Rerun)
			}
			if s.Rerun == nil || !strings.Contains(s.Rerun.Why, c.why) {
				t.Fatalf("why = %+v, quiero que diga %q", s.Rerun, c.why)
			}
			if c.baseRuns >= 0 && fx.baseRuns != c.baseRuns {
				t.Fatalf("corridas en la base = %d, quiero %d", fx.baseRuns, c.baseRuns)
			}
			if len(fx.g.added) != len(fx.g.dropped) || len(fx.leftovers(t)) != 0 {
				t.Fatalf("el worktree temporal debe quitarse siempre: added=%v dropped=%v quedan=%v", fx.g.added, fx.g.dropped, fx.leftovers(t))
			}
			if res.SHA == "DIRTY" {
				return
			}
			ok, detail := Verify(context.Background(), &fx.g.fakeGit, nil, &res)
			if ok || !strings.Contains(detail, "no cuenta como un rojo que ya estaba en la base: "+s.Rerun.Why) {
				t.Fatalf("el rechazo del DONE debe decir por qué: %v %q", ok, detail)
			}
			if !strings.Contains(res.Markdown(), "No cuenta como un rojo que ya estaba en la base: "+s.Rerun.Why) {
				t.Fatalf("check.md debe decir por qué:\n%s", res.Markdown())
			}
		})
	}
}

func TestRerunOffWithoutKey(t *testing.T) {
	fx := newRerun(t)
	fx.r.Cfg.Steps[0].Rerun = nil
	res, s := fx.run(t)
	if res.Result != "FAIL" || s.Rerun != nil || fx.baseRuns != 0 || len(fx.g.added) != 0 {
		t.Fatalf("sin rerun el paso falla como siempre: %s %+v base=%d", res.Result, s.Rerun, fx.baseRuns)
	}
}

func TestRerunOnlyOnTestStep(t *testing.T) {
	fx := newRerun(t)
	fx.r.Cfg.Steps[0].Name = "unit"
	res, s := fx.run(t)
	if res.Result != "FAIL" || s.Rerun != nil || fx.baseRuns != 0 {
		t.Fatalf("rerun solo cuenta en check.test_step: %s %+v", res.Result, s.Rerun)
	}
}

func TestRerunNeedsWorktreeGit(t *testing.T) {
	fx := newRerun(t)
	fx.r.Git = &fakeGit{sha: "abc123"}
	res, s := fx.run(t)
	if res.Result != "FAIL" || s.Rerun == nil || !strings.Contains(s.Rerun.Why, "git sin worktrees") {
		t.Fatalf("sin worktrees no se ignora nada: %s %+v", res.Result, s.Rerun)
	}
}

func TestRerunSharesNodeModulesByLinkAndRemovesLinkFirst(t *testing.T) {
	fx := newRerun(t)
	marker := filepath.Join(fx.r.Root, "node_modules", "pkg", "index.js")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("module.exports = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.r.Root, "version.ts"), []byte("export const v = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	fx.r.Cfg.Steps[0].Rerun.Share = []string{"node_modules", "version.ts", "no-existe"}
	fx.onBase = func(dir string, _ []string, _ string) {
		if fi, err := os.Lstat(filepath.Join(dir, "node_modules")); err != nil || !isLink(fi) {
			t.Errorf("node_modules en la base debe ser un enlace: %v %v", fi, err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "node_modules", "pkg", "index.js"))
		if err != nil || string(b) != "module.exports = 1" {
			t.Errorf("la base debe ver node_modules del repo: %q %v", b, err)
		}
		if b, err := os.ReadFile(filepath.Join(dir, "version.ts")); err != nil || string(b) != "export const v = 1" {
			t.Errorf("la base debe recibir la copia de version.ts: %q %v", b, err)
		}
	}
	res, s := fx.run(t)
	if res.Result != "PASS" || !s.Rerun.Accepted() || s.Rerun.Cleanup != "" {
		t.Fatalf("%s %+v", res.Result, s.Rerun)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "module.exports = 1" {
		t.Fatalf("node_modules del repo debe quedar intacto: %q %v", b, err)
	}
	if len(fx.leftovers(t)) != 0 {
		t.Fatalf("quedaron restos: %v", fx.leftovers(t))
	}
}

func TestRerunRefusesToShadowTrackedPath(t *testing.T) {
	fx := newRerun(t)
	if err := os.MkdirAll(filepath.Join(fx.r.Root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	fx.g.baseFiles["node_modules/x.js"] = "rastreado"
	res, s := fx.run(t)
	if res.Result != "FAIL" || !strings.Contains(s.Rerun.Why, "existe en la base") || fx.baseRuns != 0 {
		t.Fatalf("%s %+v", res.Result, s.Rerun)
	}
	if len(fx.leftovers(t)) != 0 {
		t.Fatalf("quedaron restos: %v", fx.leftovers(t))
	}
}

func TestRerunCommand(t *testing.T) {
	files := []string{"a/x.spec.ts", "b/y.spec.ts"}
	argv := rerunCommand(&config.Rerun{Run: `npx vitest run {files} --reporter=default`}, files)
	if !slices.Equal(argv, []string{"npx", "vitest", "run", "a/x.spec.ts", "b/y.spec.ts", "--reporter=default"}) {
		t.Fatalf("%q", argv)
	}
	argv = rerunCommand(&config.Rerun{Run: `ng test {files}`, Each: "--include {file}"}, files)
	if !slices.Equal(argv, []string{"ng", "test", "--include", "a/x.spec.ts", "--include", "b/y.spec.ts"}) {
		t.Fatalf("%q", argv)
	}
}

func TestParseVitestRealFormat(t *testing.T) {
	out := "\x1b[31m FAIL \x1b[39m  ms-sys-frontend  src/app/core/layout/components/navbar/navbar.component.spec.ts > NavbarComponent · atención de la campana (sr-only) > gate del stream: notifications.stream, no notifications.read > con read y stream, carga la lista y conecta el sondeo\r\n" +
		"AssertionError: expected false to be true // Object.is equality\r\n" +
		" ❯ src/app/core/layout/components/navbar/navbar.component.spec.ts:170:21\r\n" +
		" ❯ ../../../../Carlos%20Solutions/ms-sys-frontend/node_modules/@vitest/runner/dist/chunk-artifact.js:302:11\r\n" +
		"⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯\r\n\r\n" +
		" Test Files  1 failed | 946 passed (947)\r\n      Tests  1 failed | 9321 passed | 2 skipped (9324)\r\n   Start at  07:00:18\r\n"
	run, why := parseVitest(out, "")
	if why != "" {
		t.Fatal(why)
	}
	want := "src/app/core/layout/components/navbar/navbar.component.spec.ts > NavbarComponent · atención de la campana (sr-only) > gate del stream: notifications.stream, no notifications.read > con read y stream, carga la lista y conecta el sondeo"
	if len(run.tests) != 1 || run.tests[0] != want || run.total != 947 || !strings.Contains(run.section, "chunk-artifact.js") ||
		!slices.Equal(run.errs[want], []string{"AssertionError: expected false to be true // Object.is equality"}) {
		t.Fatalf("%+v", run)
	}
}

func TestVerifyMentionsIgnoredWhenAnotherStepFails(t *testing.T) {
	res := Result{SHA: "abc", Result: "FAIL", Steps: []StepResult{
		{Name: "test", Status: "pass", Rerun: &Rerun{Base: "base0123456789", Files: []string{navSpec}, Ignored: []string{navSpec + " > a"}}},
		{Name: "build", Status: "fail"},
	}}
	ok, detail := Verify(context.Background(), &fakeGit{}, nil, &res)
	if ok || !strings.Contains(detail, "1 prueba(s) que ya fallaban en la base base0123") {
		t.Fatalf("%v %q", ok, detail)
	}
}

// Hallazgos de la revisión: cada caso tiene que seguir rechazando.
func TestRerunRejectsReviewHoles(t *testing.T) {
	dup := navSpec + " > Navbar > x"
	cases := []struct {
		name  string
		setup func(fx *rerunFixture)
		why   string
	}{
		{"dos pruebas con el mismo nombre: una ya fallaba, la rama rompe la otra", func(fx *rerunFixture) {
			fx.head, fx.base = vitestOut(900, dup, dup), vitestOut(1, dup)
		}, "mismo nombre"},
		{"mismo nombre, otro error", func(fx *rerunFixture) {
			fx.base = strings.ReplaceAll(fx.base, "expected 1 to be 2", "Test timed out in 5000ms.")
		}, "con un error que no tiene en la base"},
		{"umbral de cobertura roto", func(fx *rerunFixture) {
			fx.head += "ERROR: Coverage for lines (50%) does not meet global threshold (80%)\n"
		}, "no terminó limpia"},
		{"fugas", func(fx *rerunFixture) {
			fx.head = strings.Replace(fx.head, "   Start at", "      Leaks  1 leak\n   Start at", 1)
		}, "no terminó limpia"},
		{"más salida después del resumen", func(fx *rerunFixture) { fx.head += "algo más que falló\n" }, "después del resumen"},
		{"comando encadenado", func(fx *rerunFixture) {
			fx.r.Cfg.Steps[0].Run = "npm test -- --watch=false && npm run lint"
		}, "encadena otros comandos"},
		{"share que git no ignora", func(fx *rerunFixture) {
			if err := os.MkdirAll(filepath.Join(fx.r.Root, "node_modules"), 0o755); err != nil {
				t.Fatal(err)
			}
			fx.g.shared = []string{"node_modules"}
		}, "que git no ignora"},
		{"share tocado por la rama", func(fx *rerunFixture) {
			if err := os.MkdirAll(filepath.Join(fx.r.Root, "gen"), 0o755); err != nil {
				t.Fatal(err)
			}
			fx.r.Cfg.Steps[0].Rerun.Share = []string{"gen"}
			fx.g.diff = append(fx.g.diff, "gen/a.ts")
		}, "que esta rama tocó"},
		{"bail: archivos que no corrieron", func(fx *rerunFixture) {
			fx.head = strings.Replace(fx.head, "Test Files  1 failed | 899 passed (900)", "Test Files  1 failed | 10 passed | 889 skipped (900)", 1)
		}, "no fallaron ni pasaron"},
		{"workspaces de npm", func(fx *rerunFixture) {
			if err := os.WriteFile(filepath.Join(fx.r.Root, "package.json"), []byte(`{"workspaces": ["packages/*"]}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "workspaces"},
		{"workspaces de pnpm", func(fx *rerunFixture) {
			if err := os.WriteFile(filepath.Join(fx.r.Root, "pnpm-workspace.yaml"), []byte("packages: []\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "workspaces"},
		{"aviso de npm o de node después del resumen no basta para aceptar otra salida", func(fx *rerunFixture) {
			fx.head += "npm notice New minor version of npm available\n(node:123) Warning: algo\notra cosa\n"
		}, "después del resumen"},
		{"share anidado", func(fx *rerunFixture) {
			fx.r.Cfg.Steps[0].Rerun.Share = []string{"node_modules", "node_modules/x"}
		}, "repite o anida"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newRerun(t)
			c.setup(fx)
			res, s := fx.run(t)
			if res.Result != "FAIL" || s.Rerun == nil || !strings.Contains(s.Rerun.Why, c.why) {
				t.Fatalf("debió rechazar con %q: %s %+v", c.why, res.Result, s.Rerun)
			}
			if len(fx.leftovers(t)) != 0 {
				t.Fatalf("quedaron restos: %v", fx.leftovers(t))
			}
		})
	}
}

// Una prueba con varios errores sale con varias líneas FAIL: no es ambigua.
func TestRerunToleratesSeveralErrorsOfOneTest(t *testing.T) {
	fx := newRerun(t)
	one := navSpec + " > Navbar > a"
	twice := func(total int) string {
		out := vitestOut(total, one, one)
		return strings.Replace(out, "Tests  2 failed", "Tests  1 failed", 1)
	}
	fx.head, fx.base = twice(900), twice(1)
	res, s := fx.run(t)
	if res.Result != "PASS" || !s.Rerun.Accepted() || !slices.Equal(s.Rerun.Ignored, []string{one}) {
		t.Fatalf("%s %+v", res.Result, s.Rerun)
	}
}

// Un enlace que deja el comando de pruebas dentro del worktree se quita sin
// borrar lo que apunta, igual que el de node_modules.
func TestRerunCleanupNeverDeletesThroughStrayLink(t *testing.T) {
	fx := newRerun(t)
	victim := filepath.Join(fx.r.Root, "victim") // fuera de la carpeta temporal que se revisa
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(victim, "victim.txt"), []byte("vivo"), 0o644); err != nil {
		t.Fatal(err)
	}
	fx.onBase = func(dir string, _ []string, _ string) {
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
			t.Error(err)
		}
		if err := linkDir(victim, filepath.Join(dir, "src", "cache")); err != nil {
			t.Error(err)
		}
	}
	res, s := fx.run(t)
	if res.Result != "PASS" || s.Rerun.Cleanup != "" {
		t.Fatalf("%s %+v", res.Result, s.Rerun)
	}
	if b, err := os.ReadFile(filepath.Join(victim, "victim.txt")); err != nil || string(b) != "vivo" {
		t.Fatalf("la limpieza borró a través del enlace: %q %v", b, err)
	}
	if len(fx.leftovers(t)) != 0 {
		t.Fatalf("quedaron restos: %v", fx.leftovers(t))
	}
}

// Los restos de una corrida a la que mataron (su proceso ya no existe) se
// limpian en la siguiente sin tocar lo que apunta su enlace.
func TestRerunSweepsStaleRunOfDeadProcess(t *testing.T) {
	fx := newRerun(t)
	target := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(target, "index.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(fx.tmp, baseDirPrefix+"viejo")
	wt := filepath.Join(stale, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(target, filepath.Join(wt, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "pid"), []byte("999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	alive := filepath.Join(fx.tmp, baseDirPrefix+"vivo")
	if err := os.MkdirAll(filepath.Join(alive, "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := os.Create(filepath.Join(alive, "pid")) // una corrida viva lo tiene abierto
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if _, err := fmt.Fprint(held, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	fx.g.added = []string{filepath.ToSlash(wt), filepath.ToSlash(filepath.Join(alive, "wt"))}
	res, _ := fx.run(t)
	if res.Result != "PASS" {
		t.Fatalf("%s", res.Result)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("el resto de la corrida muerta sigue: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(target, "index.js")); err != nil || string(b) != "1" {
		t.Fatalf("la limpieza borró a través del enlace: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(alive, "wt")); err != nil {
		t.Fatalf("no debe tocar la corrida de un proceso vivo: %v", err)
	}
}

// El error se compara sin la carpeta donde corrió ni la línea del archivo,
// pero sí con los números del mensaje.
func TestNormalizeErr(t *testing.T) {
	root := `C:\Users\Carlos Solutions\repo`
	a := normalizeErr(`TypeError: x is undefined at C:/Users/Carlos%20Solutions/repo/src/a.ts:12:5`, root)
	b := normalizeErr(`TypeError: x is undefined at src/a.ts:40:1`, "")
	if a != b || a != "TypeError: x is undefined at src/a.ts" {
		t.Fatalf("%q / %q", a, b)
	}
	if normalizeErr("expected '12:30' to be '12:31'", "") == normalizeErr("expected '12:30' to be '12:30'", "") {
		t.Fatal("los números del mensaje cuentan")
	}
}

// Npm o Node pueden avisar después del resumen sin que eso cambie el resultado.
func TestParseVitestToleratesNpmNoticeAfterSummary(t *testing.T) {
	out := vitestOut(900, navSpec+" > a") + "npm notice New minor version of npm available\n(node:123) Warning: algo\nnpm error code 1\n"
	if _, why := parseVitest(out, ""); why != "" {
		t.Fatal(why)
	}
}
