package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
)

// worktreeWith crea un worktree temporal con los archivos dados (vacíos).
func worktreeWith(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func keys(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s:%d-%d", f.File, f.From, f.To))
	}
	sort.Strings(out)
	return out
}

var defA = Defect{File: "internal/a.go", Lines: "100-110", What: "a"}

func TestScoreCatchesWithinTolerance(t *testing.T) {
	for _, f := range []Finding{
		{"internal/a.go", 95, 95},   // desde-5
		{"internal/a.go", 115, 115}, // hasta+5
		{"internal/a.go", 105, 105},
		{"internal/a.go", 80, 95},
		{"internal/a.go", 115, 200},
		{"internal/a.go", 1, 500},
	} {
		s := ScoreCase([]Defect{defA}, []Finding{f})
		if len(s.Caught) != 1 || len(s.Missed) != 0 || len(s.FalsePositives) != 0 {
			t.Errorf("%+v debe atrapar: %+v", f, s)
		}
	}
	one := Defect{File: "internal/b.go", Lines: "50", What: "b"}
	if s := ScoreCase([]Defect{one}, []Finding{{"internal/b.go", 45, 45}}); len(s.Caught) != 1 {
		t.Errorf("un defecto de una línea también tiene holgura: %+v", s)
	}
	two := ScoreCase([]Defect{defA, one}, []Finding{{"internal/b.go", 55, 55}})
	if len(two.Caught) != 1 || two.Caught[0] != one || len(two.Missed) != 1 || two.Missed[0] != defA {
		t.Errorf("cada defecto se califica aparte: %+v", two)
	}
}

func TestScoreMissesOutsideTolerance(t *testing.T) {
	for _, f := range []Finding{
		{"internal/a.go", 94, 94},
		{"internal/a.go", 116, 116},
		{"internal/a.go", 1, 94},
		{"internal/b.go", 105, 105},   // otro archivo
		{"internal/A.go", 105, 105},   // la ruta es exacta
		{"x/internal/a.go", 105, 105}, // no basta el sufijo
	} {
		s := ScoreCase([]Defect{defA}, []Finding{f})
		if len(s.Caught) != 0 || len(s.Missed) != 1 || len(s.FalsePositives) != 1 {
			t.Errorf("%+v no debe atrapar: %+v", f, s)
		}
	}
	if s := ScoreCase([]Defect{defA}, nil); len(s.Missed) != 1 || len(s.Caught) != 0 || len(s.FalsePositives) != 0 {
		t.Errorf("sin hallazgos: %+v", s)
	}
}

func TestFindingsSkipsExcluded(t *testing.T) {
	doc := "# Review-map\n" +
		"- internal/a.go:10 falta validar\n" +
		"- `internal/b.go:20-25` rango en backticks\n" +
		"- (internal/c.go:30) entre paréntesis\n" +
		"- caminos: internal/skip1.go:1 cubre\n" +
		"- cubre: internal/skip2.go:2\n" +
		"- el código responde internal/skip3.go:3\n" +
		"\n## Docs\n" +
		"- internal/skip4.go:4\n" +
		"### Detalle\n" +
		"- internal/skip5.go:5\n" +
		"## Hallazgos\n" +
		"- internal/d.go:40\n" +
		"## Pruebas sin ejecutar\n" +
		"- internal/skip6.go:6\n" +
		"# Otro\n" +
		"- internal/e.go:50\n"
	wt := worktreeWith(t, "internal/a.go", "internal/b.go", "internal/c.go", "internal/d.go", "internal/e.go", "internal/n.go",
		"internal/skip1.go", "internal/skip2.go", "internal/skip3.go", "internal/skip4.go", "internal/skip5.go", "internal/skip6.go", "a.go", "b.go")
	got := keys(Findings("internal/n.go:5-9 en la nota", map[string]string{"review-map.md": doc}, wt))
	want := []string{
		"internal/a.go:10-10", "internal/b.go:20-25", "internal/c.go:30-30",
		"internal/d.go:40-40", "internal/e.go:50-50", "internal/n.go:5-9",
	}
	if !slices.Equal(got, want) {
		t.Errorf("hallazgos:\n got %v\nwant %v", got, want)
	}

	// Sin número no es una referencia.
	if fs := Findings("localhost:8080 paso:2 y a.go: y a.go:x", nil, wt); len(fs) != 0 {
		t.Errorf("no son referencias: %v", fs)
	}
	// Cada .md de out cuenta, y la nota también.
	two := Findings("", map[string]string{"review-map.md": "a.go:1\n", "security.md": "b.go:2\n"}, wt)
	if len(two) != 2 {
		t.Errorf("todos los .md cuentan: %v", two)
	}
}

func TestFindingsTrimsWorktreePrefix(t *testing.T) {
	wt := worktreeWith(t, "internal/a.go", "internal/b.go", "internal/c.go", "internal/d.go", "internal/e.go", "internal/g.go")
	slash := filepath.ToSlash(wt)
	back := strings.ReplaceAll(slash, "/", `\`)
	note := slash + "/internal/a.go:10 y ./internal/b.go:2-3 y internal/c.go:4 y /etc/hosts.conf:9 y " + slash + "2/internal/d.go:1\n" +
		"`" + back + `\internal\e.go:7-8` + "` y C:" + `\otro\f.go:1` + " y (../fuera.go:1)"
	got := keys(Findings(note, nil, wt))
	want := []string{"internal/a.go:10-10", "internal/b.go:2-3", "internal/c.go:4-4", "internal/e.go:7-8"}
	if !slices.Equal(got, want) {
		t.Errorf("hallazgos:\n got %v\nwant %v", got, want)
	}

	if runtime.GOOS == "windows" {
		got = keys(Findings(strings.ToUpper(wt)+`\internal\g.go:1`, nil, wt))
		if !slices.Equal(got, []string{"internal/g.go:1-1"}) {
			t.Errorf("en Windows el prefijo no distingue mayúsculas: %v", got)
		}
	}
	for _, f := range Findings(note, nil, wt) {
		if f.File != "" && (f.File[0] == '/' || f.File[0] == '.') {
			t.Errorf("la ruta del hallazgo es relativa al worktree: %q", f.File)
		}
	}
}

func TestScoreFalsePositivesDeduped(t *testing.T) {
	found := []Finding{
		{"internal/a.go", 105, 105}, // atrapa
		{"x.go", 7, 7},
		{"y.go", 3, 4},
		{"x.go", 7, 7},
		{"y.go", 3, 4},
		{"x.go", 7, 8}, // otro rango: otra clave
		{"z.go", 9, 9},
	}
	s := ScoreCase([]Defect{defA}, found)
	want := []string{"x.go:7", "y.go:3-4", "x.go:7-8", "z.go:9"}
	if !slices.Equal(s.FalsePositives, want) {
		t.Errorf("falsos positivos (orden de primera aparición, N sin rango):\n got %v\nwant %v", s.FalsePositives, want)
	}
	if len(s.Caught) != 1 {
		t.Errorf("el defecto se atrapó: %+v", s)
	}
}

func TestFindingsExtensionlessPaths(t *testing.T) {
	wt := worktreeWith(t, "Makefile", "Dockerfile", "Brewfile", "cmd/tool")
	got := keys(Findings("Makefile:3 y Dockerfile:12-14 y Brewfile:1 y cmd/tool:4 y "+filepath.ToSlash(wt)+"/Makefile:3", nil, wt))
	want := []string{"Brewfile:1-1", "Dockerfile:12-14", "Makefile:3-3", "Makefile:3-3", "cmd/tool:4-4"}
	if !slices.Equal(got, want) {
		t.Errorf("hallazgos:\n got %v\nwant %v", got, want)
	}
	if fs := Findings("localhost:8080 y paso:2 y MiMakefile:3 y 10:30", nil, wt); len(fs) != 0 {
		t.Errorf("no son referencias: %v", fs)
	}
}

func TestFindingsSkipsMissingFiles(t *testing.T) {
	wt := worktreeWith(t, "internal/a.go")
	got := keys(Findings("2026/10:3 y internal/nada.go:4 y internal/a.go:1", nil, wt))
	if !slices.Equal(got, []string{"internal/a.go:1-1"}) {
		t.Errorf("solo cuenta el archivo que existe: %v", got)
	}
	if fs := Findings("internal/a.go:1", nil, ""); len(fs) != 0 {
		t.Errorf("sin worktree no hay hallazgos: %v", fs)
	}
}

func TestFindingsSkipsDirectories(t *testing.T) {
	wt := worktreeWith(t, "internal/a.go")
	if fs := Findings("internal:3", nil, wt); len(fs) != 0 {
		t.Errorf("un directorio no es una referencia: %v", fs)
	}
}
