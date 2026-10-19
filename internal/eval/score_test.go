package eval

import (
	"fmt"
	"runtime"
	"slices"
	"sort"
	"testing"
)

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
	got := keys(Findings("internal/n.go:5-9 en la nota", map[string]string{"review-map.md": doc}, "/wt"))
	want := []string{
		"internal/a.go:10-10", "internal/b.go:20-25", "internal/c.go:30-30",
		"internal/d.go:40-40", "internal/e.go:50-50", "internal/n.go:5-9",
	}
	if !slices.Equal(got, want) {
		t.Errorf("hallazgos:\n got %v\nwant %v", got, want)
	}

	// Sin extensión o sin número no es una referencia.
	if fs := Findings("Makefile:3 y a.go: y a.go:x", nil, "/wt"); len(fs) != 0 {
		t.Errorf("no son referencias: %v", fs)
	}
	// Cada .md de out cuenta, y la nota también.
	two := Findings("", map[string]string{"review-map.md": "a.go:1\n", "security.md": "b.go:2\n"}, "/wt")
	if len(two) != 2 {
		t.Errorf("todos los .md cuentan: %v", two)
	}
}

func TestFindingsTrimsWorktreePrefix(t *testing.T) {
	note := "/tmp/w/wt/internal/a.go:10 y ./internal/b.go:2-3 y internal/c.go:4 y /etc/hosts.conf:9 y /tmp/w/wt2/internal/d.go:1\n" +
		"`C:\\Users\\x\\wt\\internal\\e.go:7-8` y C:\\otro\\f.go:1 y (../fuera.go:1)"
	got := keys(Findings(note, nil, "/tmp/w/wt"))
	want := []string{"internal/a.go:10-10", "internal/b.go:2-3", "internal/c.go:4-4"}
	if !slices.Equal(got, want) {
		t.Errorf("Unix:\n got %v\nwant %v", got, want)
	}

	got = keys(Findings(note, nil, `C:\Users\x\wt`))
	want = []string{"internal/b.go:2-3", "internal/c.go:4-4", "internal/e.go:7-8"}
	if !slices.Equal(got, want) {
		t.Errorf("Windows (rutas con \\):\n got %v\nwant %v", got, want)
	}

	if runtime.GOOS == "windows" {
		got = keys(Findings(`c:\users\X\WT\internal\g.go:1`, nil, `C:\Users\x\wt`))
		if !slices.Equal(got, []string{"internal/g.go:1-1"}) {
			t.Errorf("en Windows el prefijo no distingue mayúsculas: %v", got)
		}
	}
	for _, f := range Findings(note, nil, "/tmp/w/wt") {
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
