package check

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/config"
)

func failingRun(t *testing.T, st config.Step, out string, exit int) StepResult {
	t.Helper()
	fx := &fakeExec{results: map[string]struct {
		out  string
		exit int
	}{st.Run: {out, exit}}}
	r := runner(t, []config.Step{st}, fx, &fakeGit{sha: "a"})
	r.Root = filepath.Join(r.Root, "Carlos Solutions") // una raíz con espacio, como en Windows
	out = strings.ReplaceAll(out, "{root}", filepath.ToSlash(r.Root))
	out = strings.ReplaceAll(out, "{winroot}", filepath.FromSlash(r.Root))
	fx.results[st.Run] = struct {
		out  string
		exit int
	}{out, exit}
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res.Steps[0]
}

// Salida de un check del ERP: hallazgos al inicio de la línea o con número de
// línea, más rutas que solo se mencionan (el eco de npm, un doc de ayuda).
func TestBlameFindingsAndMentions(t *testing.T) {
	out := "\n> app@1.0.0 check:theme\n> node scripts/check-theme.js\n\n" +
		"theme check: 2 violation(s):\n" +
		"  src/app/modal/modal.component.css:26 - radio fuera de escala\n" +
		"  {winroot}\\src\\app\\modal\\modal.component.html - <button>: 2 sitio(s)\n" +
		"\x1b[31m./src/app/otro/otro.ts\x1b[0m(12,3): borde\n" +
		"Ver `docs/ux-patterns.md §7` y https://example.com/docs/guia.html\n"
	sr := failingRun(t, config.Step{Name: "check:theme", Run: "npm run check:theme"}, out, 1)
	b := sr.Blame
	if b == nil || b.Opaque != "" || b.More {
		t.Fatalf("blame: %+v", b)
	}
	want := []string{"src/app/modal/modal.component.css", "src/app/modal/modal.component.html", "src/app/otro/otro.ts"}
	if !slices.Equal(b.Findings, want) {
		t.Errorf("findings: %v", b.Findings)
	}
	if !slices.Contains(b.Mentions, "scripts/check-theme.js") || !slices.Contains(b.Mentions, "docs/ux-patterns.md") {
		t.Errorf("mentions: %v", b.Mentions)
	}
	if slices.ContainsFunc(b.Mentions, func(m string) bool { return strings.Contains(m, "example.com") }) {
		t.Errorf("una URL no es una ruta del repo: %v", b.Mentions)
	}
	if sr.Status != "fail" || !strings.Contains(sr.Tail, "violation") {
		t.Errorf("el paso sigue igual: %+v", sr)
	}
}

func TestBlameOpaque(t *testing.T) {
	cases := []struct {
		name string
		st   config.Step
		out  string
		exit int
	}{
		{"paso de compilación", config.Step{Name: "build", Run: "npm run build"}, "src/app/a.ts:1:1 - algo", 1},
		{"vet compila", config.Step{Name: "estatico", Run: "go vet ./..."}, "internal/a/a.go:3:2: algo", 1},
		{"vulnerabilidades", config.Step{Name: "vulns", Run: "govulncheck ./..."}, "Vulnerability #1: GO-2024-0001\n  internal/a/a.go:3:2: llama", 3},
		{"go no compila", config.Step{Name: "calidad", Run: "go run ./cmd/q"}, "# m/internal/a\ninternal/a/a.go:3:2: undefined: X", 1},
		{"pánico", config.Step{Name: "calidad", Run: "go run ./cmd/q"}, "internal/a/a.go:3 algo\npanic: nil map", 2},
		{"excepción de node", config.Step{Name: "ui", Run: "npm run check:ui"}, "src/app/a.html:3 algo\nTypeError: x is undefined\n    at main (scripts/check-ui.js:10:5)", 1},
		{"error de TypeScript", config.Step{Name: "ui", Run: "npm run check:ui"}, "src/app/a.ts(3,1): error TS2304: Cannot find name 'x'.", 1},
		{"se cortó", config.Step{Name: "ui", Run: "npm run check:ui"}, "src/app/a.html:3 algo", -1},
		{"lo mató el sistema", config.Step{Name: "lint", Run: "npm run lint"}, "src/app/a.ts:3:1 algo\nKilled", 137},
		{"fallo del proceso en Windows", config.Step{Name: "lint", Run: "npm run lint"}, "src/app/a.ts:3:1 algo", 0xC0000005},
		{"error de tipos de astro", config.Step{Name: "check", Run: "npm run check"}, "src/pages/a.astro:3:5 - error ts(2304): Cannot find name 'x'.", 1},
		{"staticcheck compila", config.Step{Name: "lint", Run: "golangci-lint run"}, "b.go:3:2: undefined: a.Foo (compile)", 1},
		{"mypy", config.Step{Name: "lint", Run: "make lint"}, "b.py:3: error: Argument 1 has incompatible type  [arg-type]", 1},
		{"rustc", config.Step{Name: "lint", Run: "make lint"}, "error[E0425]: cannot find value `x`\n --> src/b.rs:3:5", 1},
		{"C#", config.Step{Name: "lint", Run: "make lint"}, "src/B.cs(3,5): error CS0103: The name 'x' does not exist", 1},
		{"pruebas antes del paso de pruebas", config.Step{Name: "e2e", Run: "npx playwright run"}, "src/app/a.html:3 algo", 1},
		{"paso de tipos", config.Step{Name: "check:types", Run: "npm run check:types"}, "src/app/a.ts:3:1 algo", 1},
	}
	for _, c := range cases {
		sr := failingRun(t, c.st, c.out, c.exit)
		if sr.Blame == nil || sr.Blame.Opaque == "" {
			t.Errorf("%s: debe quedar opaco: %+v", c.name, sr.Blame)
		}
	}
}

func TestBlameOnlyOnFailAndCapped(t *testing.T) {
	sr := failingRun(t, config.Step{Name: "ui", Run: "npm run check:ui"}, "src/app/a.html:3 algo", 0)
	if sr.Blame != nil {
		t.Errorf("un paso en verde no lleva blame: %+v", sr.Blame)
	}
	var b strings.Builder
	for i := range maxBlamePaths + 1 {
		fmt.Fprintf(&b, "src/app/f%d.html:1 algo\n", i)
	}
	sr = failingRun(t, config.Step{Name: "ui", Run: "npm run check:ui"}, b.String(), 1)
	if sr.Blame == nil || !sr.Blame.More || len(sr.Blame.Findings) > maxBlamePaths {
		t.Errorf("más rutas que el tope: %+v", sr.Blame)
	}
}

// Una ruta sin extensión (un import roto) queda como mención.
func TestBlameExtensionlessMention(t *testing.T) {
	out := "src/app/modal/modal.ts:3:1 Unable to resolve path to module '../cart/cart'\n"
	sr := failingRun(t, config.Step{Name: "lint", Run: "npm run lint"}, out, 1)
	if sr.Blame == nil || !slices.Contains(sr.Blame.Mentions, "../cart/cart") || !slices.Equal(sr.Blame.Findings, []string{"src/app/modal/modal.ts"}) {
		t.Errorf("blame: %+v", sr.Blame)
	}
}

// Solo los pasos previos a las pruebas guardan blame; quick nunca.
func TestBlameOnlyBeforeTestStep(t *testing.T) {
	fx := &fakeExec{results: map[string]struct {
		out  string
		exit int
	}{"npm": {"src/app/a.html:3 algo", 1}}}
	steps := []config.Step{{Name: "ui", Run: "npm run check:ui"}, {Name: "test", Run: "npm test"}, {Name: "after", Run: "npm run after"}}
	r := runner(t, steps, fx, &fakeGit{sha: "a"})
	r.Cfg.Quick = []string{"npm run quick {pkg}"}
	res, _ := r.Run(context.Background())
	if res.Steps[0].Blame == nil || res.Steps[1].Blame != nil || res.Steps[2].Blame != nil {
		t.Errorf("blame por paso: %+v", res.Steps)
	}
	q, _ := r.Quick(context.Background(), "./x")
	if q.Steps[0].Status != "fail" || q.Steps[0].Blame != nil {
		t.Errorf("quick: %+v", q.Steps[0])
	}
}
