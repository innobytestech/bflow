package review

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/guard"
)

var now = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// R4: todas las formas de escribir una ruta caen en la misma ruta relativa.
func TestNormalize(t *testing.T) {
	cases := []struct {
		root, cwd, in string
		want          string
		ok            bool
	}{
		{"/repo", "/repo", "internal/a.go", "internal/a.go", true},
		{"/repo", "/repo/internal", "a.go", "internal/a.go", true},
		{"/repo", "/repo/internal", "./a.go", "internal/a.go", true},
		{"/repo", "/repo/internal", "../x/b.go", "x/b.go", true},
		{"/repo", "/", "/repo/x/y.go", "x/y.go", true},
		{"/repo", "/repo", "/etc/passwd", "", false},
		{"/repo", "/repo/internal", "../../outside.go", "", false},
		{"/repo", "/repo", "/repository/a.go", "", false}, // prefijo de texto, no carpeta
		{`C:\repo`, `C:\repo`, `internal\a.go`, "internal/a.go", true},
		{`C:\repo`, `C:\repo\internal`, `a.go`, "internal/a.go", true},
		{`C:\repo`, `C:\repo`, `C:\repo\x\y.go`, "x/y.go", true},
		{`C:\repo`, `C:\repo`, `c:\repo\x\y.go`, "x/y.go", true},
		{`C:\repo`, `C:\repo`, `C:/repo/x/y.go`, "x/y.go", true},
		{`C:\repo`, `C:\repo`, `/c/repo/x/y.go`, "x/y.go", true},
		{`C:\repo`, `C:\repo`, `D:\otro\a.go`, "", false},
		{`C:\repo`, `C:\repo`, `C:\Users\x\.claude\f.md`, "", false},
	}
	for _, c := range cases {
		got, ok := Normalize(c.root, c.cwd, c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("Normalize(%q, %q, %q) = %q, %v; quiero %q, %v", c.root, c.cwd, c.in, got, ok, c.want, c.ok)
		}
	}
}

// repoWith crea archivos pequeños y devuelve la raíz.
func repoWith(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// R1, R2: un Read se guarda con su ruta relativa y de qué hook vino.
func TestFromActionRead(t *testing.T) {
	root := repoWith(t, "internal/a.go")
	abs := filepath.Join(root, "internal", "a.go")
	got := FromAction(guard.Action{Tool: guard.Read, Path: abs, Agent: "bflow-reviewer", Subagent: true}, root, root, true, now)
	if got.Tool != guard.Read || !slices.Equal(got.Paths, []string{"internal/a.go"}) || !got.ReadHook || got.Whole || !got.TS.Equal(now) {
		t.Errorf("Read: %+v", got)
	}
	if r := FromAction(guard.Action{Tool: guard.Read, Path: abs}, root, root, false, now); r.ReadHook {
		t.Errorf("sin --reads, ReadHook es false: %+v", r)
	}
	// Una lectura fuera del repo se guarda igual (R1: aunque no lea nada), sin ruta.
	out := FromAction(guard.Action{Tool: guard.Read, Path: filepath.Join(t.TempDir(), "otro.txt")}, root, root, true, now)
	if out.Tool != guard.Read || len(out.Paths) != 0 || !out.ReadHook {
		t.Errorf("fuera del repo: %+v", out)
	}
}

func TestFromActionBash(t *testing.T) {
	root := repoWith(t, "internal/a.go", "internal/b.go", "docs/guia.md")
	cases := []struct {
		cmd  string
		want []string // rutas leídas, ordenadas
	}{
		{"git diff main...HEAD -- internal/a.go", []string{"internal/a.go"}},
		{"git diff -- internal", []string{"internal"}},
		{"git diff -- docs internal/a.go", []string{"docs", "internal/a.go"}},
		{`git diff -- "internal/b.go"`, []string{"internal/b.go"}},
		{"git -C . --no-pager diff -- internal/a.go", []string{"internal/a.go"}},
		{"git -c core.pager=cat diff -- internal/a.go", []string{"internal/a.go"}},
		{"git show HEAD:internal/a.go", []string{"internal/a.go"}},
		{"git show HEAD -- internal/b.go", []string{"internal/b.go"}},
		{"git diff -- no/existe.go", nil},
		{"git diff --stat -- internal/a.go", nil},
		{"git diff --numstat -- internal/a.go", nil},
		{"git diff --shortstat -- internal/a.go", nil},
		{"git diff --name-only -- internal/a.go", nil},
		{"git diff --name-status -- internal/a.go", nil},
		{"git diff --dirstat -- internal", nil},
		{"git show HEAD", nil},
		{"cat internal/a.go", []string{"internal/a.go"}},
		{`type internal\a.go`, []string{"internal/a.go"}},
		{"Get-Content internal/a.go", []string{"internal/a.go"}},
		{"head -n 20 internal/a.go", []string{"internal/a.go"}},
		{"tail -20 internal/b.go", []string{"internal/b.go"}},
		{"sed -n 1,20p internal/a.go", []string{"internal/a.go"}},
		{"less internal/a.go", []string{"internal/a.go"}},
		{"more internal/a.go", []string{"internal/a.go"}},
		{"bat internal/a.go", []string{"internal/a.go"}},
		{"nl internal/a.go", []string{"internal/a.go"}},
		{"cat internal/a.go && git diff -- internal/b.go", []string{"internal/a.go", "internal/b.go"}},
		{"cat internal/a.go | head -5", []string{"internal/a.go"}},
		{"cat no/existe.go", nil},
		{"ls internal", nil},
		{"grep -n x internal/a.go", nil},
		{"go test ./...", nil},
	}
	for _, c := range cases {
		r := FromAction(guard.Action{Tool: guard.Bash, Command: c.cmd, Agent: "bflow-reviewer", Subagent: true}, root, root, true, now)
		got := slices.Clone(r.Paths)
		slices.Sort(got)
		if !slices.Equal(got, c.want) || r.Whole || r.Tool != guard.Bash || !r.ReadHook {
			t.Errorf("%q: paths %v whole=%v tool=%s; quiero %v", c.cmd, got, r.Whole, r.Tool, c.want)
		}
	}
}

// R3: solo un git diff (no git show) sin ruta cuenta como diff entero.
func TestFromActionGitWhole(t *testing.T) {
	root := repoWith(t, "internal/a.go")
	cases := map[string]bool{
		"git diff":                                true,
		"git diff main...HEAD":                    true,
		"git diff origin/main...HEAD --":          true,
		"git --no-pager diff":                     true,
		"git -C . diff HEAD~1":                    true,
		"git diff --stat":                         false,
		"git diff --name-only main...HEAD":        false,
		"git diff HEAD~1 --numstat":               false,
		"git diff -- internal/a.go":               false,
		"git show HEAD":                           false,
		"git show --stat HEAD":                    false,
		"git log -p":                              false,
		"go vet ./... && git diff main...HEAD":    true,
		"echo git diff":                           false,
		"git status":                              false,
		"git diff --shortstat && git diff --stat": false,
	}
	for cmd, whole := range cases {
		r := FromAction(guard.Action{Tool: guard.Bash, Command: cmd}, root, root, true, now)
		if r.Whole != whole {
			t.Errorf("%q: Whole = %v, quiero %v", cmd, r.Whole, whole)
		}
	}
}

func TestParseReadsSkipsBadLines(t *testing.T) {
	in := `{"ts":"2026-10-01T09:00:00Z","tool":"read","paths":["internal/a.go"],"read_hook":true}
esto no es json
{"ts":"2026-10-01T09:01:00Z","tool":"bash","whole":true,"read_hook":true}

{"ts":
{"ts":"2026-10-01T09:02:00Z","tool":"bash","paths":["docs"],"read_hook":false}
`
	got := ParseReads(strings.NewReader(in))
	if len(got) != 3 {
		t.Fatalf("tres líneas válidas, hay %d: %+v", len(got), got)
	}
	if got[0].Tool != guard.Read || !slices.Equal(got[0].Paths, []string{"internal/a.go"}) || !got[0].ReadHook {
		t.Errorf("primera: %+v", got[0])
	}
	if !got[1].Whole || got[2].ReadHook || !slices.Equal(got[2].Paths, []string{"docs"}) {
		t.Errorf("resto: %+v", got[1:])
	}
	if len(ParseReads(strings.NewReader(""))) != 0 {
		t.Error("vacío no da lecturas")
	}
}

func TestIsDoc(t *testing.T) {
	for p, want := range map[string]bool{"README.md": true, "a/b.mdx": true, "x.rst": true, "n.txt": true, "g.adoc": true,
		"docs/diagrama.png": true, "docs/a/b.go": true, "internal/a.go": false, "adapters/docs.js": false, "mydocs/a.go": false} {
		if IsDoc(p) != want {
			t.Errorf("IsDoc(%q) = %v", p, !want)
		}
	}
}

const reviewMap = "# Review-map\n\n" +
	"## 🔴 Mirar primero\n" +
	"- `internal/a.go:12`, `internal/b.go#L3-L9`: razón uno\n" +
	"- `internal/a.go` otra vez, sin duplicar\n" +
	"* `internal/c.go` texto\n" +
	"1. `d/e.go`: numerada\n" +
	"- `p/x.go` `p/y.go`: dos rutas separadas por espacio\n" +
	"- Esta viñeta no empieza con una ruta y es larga larga larga larga larga larga larga larga larga larga larga larga fin\n" +
	"  - `internal/sub.go` anidada: no cuenta\n" +
	"### Detalle\n" +
	"- `z/dentro.go`: un subencabezado sigue dentro de la sección\n" +
	"## 🟡 Revisar\n" +
	"- `x/amarillo.go`\n" +
	"## docs\n" +
	"- `README.md`: tabla de comandos\n" +
	"- `docs/guia.md`\n" +
	"## Otra\n" +
	"- `no/cuenta.md`\n"

func TestParseMap(t *testing.T) {
	m := ParseMap(reviewMap)
	wantRed := []string{"internal/a.go", "internal/b.go", "internal/c.go", "d/e.go", "p/x.go", "p/y.go", "z/dentro.go"}
	if !slices.Equal(m.Red, wantRed) {
		t.Errorf("Red = %v, quiero %v", m.Red, wantRed)
	}
	if len(m.RedNoPath) != 1 || !strings.HasPrefix(m.RedNoPath[0], "Esta viñeta no empieza") || len([]rune(m.RedNoPath[0])) != 80 {
		t.Errorf("RedNoPath = %q (80 runas)", m.RedNoPath)
	}
	if !m.HasDocs || !slices.Equal(m.Docs, []string{"README.md", "docs/guia.md"}) {
		t.Errorf("Docs = %v has=%v", m.Docs, m.HasDocs)
	}

	none := ParseMap("# Review-map\n## 🔴 Primero\n- `a.go`\n## 🟡 Luego\n- `b.go`\n")
	if none.HasDocs || len(none.Docs) != 0 || !slices.Equal(none.Red, []string{"a.go"}) {
		t.Errorf("sin Docs: %+v", none)
	}
	empty := ParseMap("## Docs\n\nsolo texto, sin viñetas\n")
	if !empty.HasDocs || len(empty.Docs) != 0 {
		t.Errorf("Docs vacía existe pero sin rutas: %+v", empty)
	}
	if m := ParseMap(""); m.HasDocs || len(m.Red) != 0 || len(m.RedNoPath) != 0 {
		t.Errorf("vacío: %+v", m)
	}
}

func isTest(p string) bool { return strings.HasSuffix(p, "_test.go") }

func rd(paths ...string) Read { return Read{TS: now, Tool: guard.Read, Paths: paths, ReadHook: true} }

func TestMeasure(t *testing.T) {
	diff := []string{"internal/a.go", "internal/b.go", "internal/c_test.go", "docs/g.md"}
	reads := []Read{rd("internal/a.go"), {TS: now, Tool: guard.Bash, Paths: []string{"docs"}, ReadHook: true}}
	c := Measure(diff, 300, reads, []string{"internal/a.go", "internal/b.go", "gone/x.go"}, isTest)
	if !c.Measured || c.Total != 4 || c.Read != 2 || c.Whole {
		t.Errorf("conteo: %+v", c)
	}
	if !slices.Equal(c.Unread, []string{"internal/b.go"}) || c.UnreadOther != 1 {
		t.Errorf("Unread = %v, UnreadOther = %d (la prueba sin leer no se lista)", c.Unread, c.UnreadOther)
	}
	if !slices.Equal(c.RedMissing, []string{"internal/b.go"}) || !slices.Equal(c.RedOutside, []string{"gone/x.go"}) {
		t.Errorf("rojas: missing %v outside %v", c.RedMissing, c.RedOutside)
	}
	lines := c.Lines()
	if len(lines) == 0 || lines[0] != "el reviewer leyó 2 de 4 archivos del diff" {
		t.Fatalf("Lines[0]: %q", lines)
	}
	all := strings.Join(lines, "\n")
	if !strings.Contains(all, "internal/b.go") || !strings.Contains(all, "gone/x.go") || !strings.Contains(all, "fuera del diff") || strings.Contains(all, "c_test.go") {
		t.Errorf("Lines: %q", all)
	}

	// Una carpeta cubre solo lo que contiene: "internal/a" no es "internal/ab.go".
	if c := Measure([]string{"internal/ab.go"}, 10, []Read{rd("internal/a")}, nil, isTest); c.Read != 0 {
		t.Errorf("prefijo de texto no es carpeta: %+v", c)
	}

	// Lines lista como máximo 20 no leídos y dice cuántos más hay.
	var many []string
	for i := 0; i < 25; i++ {
		many = append(many, fmt.Sprintf("pkg/f%02d.go", i))
	}
	big := Measure(many, 100, []Read{rd("otra/cosa.go")}, nil, isTest)
	if len(big.Unread) != 25 {
		t.Errorf("Unread guarda todos: %d", len(big.Unread))
	}
	text := strings.Join(big.Lines(), "\n")
	if !strings.Contains(text, "pkg/f19.go") || strings.Contains(text, "pkg/f20.go") || !strings.Contains(text, "y 5 más") {
		t.Errorf("tope de 20 y 'y 5 más':\n%s", text)
	}
}

func TestMeasureWholeDiffLimit(t *testing.T) {
	diff := []string{"a.go", "b.go", "c.go"}
	whole := []Read{{TS: now, Tool: guard.Bash, Whole: true, ReadHook: true}}
	c := Measure(diff, WholeDiffMax-1, whole, []string{"a.go"}, isTest)
	if !c.Measured || c.Read != 3 || !c.Whole || len(c.Unread) != 0 || len(c.RedMissing) != 0 {
		t.Errorf("con %d líneas un diff entero cubre todo: %+v", WholeDiffMax-1, c)
	}
	c = Measure(diff, WholeDiffMax, whole, []string{"a.go"}, isTest)
	if !c.Measured || c.Read != 0 || c.Whole || len(c.Unread) != 3 || !slices.Equal(c.RedMissing, []string{"a.go"}) {
		t.Errorf("con %d líneas ya no cubre nada: %+v", WholeDiffMax, c)
	}
}

func TestMeasureUnmeasured(t *testing.T) {
	diff := []string{"a.go", "b.go"}
	for name, reads := range map[string][]Read{
		"sin lecturas": nil,
		"sin hook":     {{TS: now, Tool: guard.Bash, Paths: []string{"a.go"}, ReadHook: false}},
	} {
		c := Measure(diff, 10, reads, []string{"a.go", "gone.go"}, isTest)
		if c.Measured || c.Read != 0 || len(c.RedMissing) != 0 {
			t.Errorf("%s: %+v", name, c)
		}
		if l := c.Lines(); len(l) != 1 || l[0] != "cobertura del reviewer: no medida" {
			t.Errorf("%s: Lines = %q", name, l)
		}
	}
}

func TestDocsPending(t *testing.T) {
	listed := []string{"README.md", "docs/guia.md", "CHANGELOG.md"}
	diff := []string{"README.md", "internal/a.go"}
	report := "# Docs\n" +
		"- `docs/guia.md`: sin cambio: la guía no habla de esto\n" +
		"- `CHANGELOG.md`: sin cambio:   \n"
	if got := DocsPending(listed, diff, report); !slices.Equal(got, []string{"CHANGELOG.md"}) {
		t.Errorf("motivo vacío no vale: %v", got)
	}
	if got := DocsPending(listed, diff, ""); !slices.Equal(got, []string{"docs/guia.md", "CHANGELOG.md"}) {
		t.Errorf("sin reports/docs.md faltan las que no cambiaron: %v", got)
	}
	ok := report + "- `CHANGELOG.md`: sin cambio: no hay API nueva\n"
	if got := DocsPending(listed, diff, ok); len(got) != 0 {
		t.Errorf("todo cumplido: %v", got)
	}
	if got := DocsPending(nil, diff, ""); len(got) != 0 {
		t.Errorf("sin rutas listadas no aplica: %v", got)
	}
	if got := DocsPending([]string{"docs/guia.md"}, diff, "- `docs/otra.md`: sin cambio: x\n"); !slices.Equal(got, []string{"docs/guia.md"}) {
		t.Errorf("el motivo es de otra ruta: %v", got)
	}
}
