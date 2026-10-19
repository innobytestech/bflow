package eval

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestParseLines(t *testing.T) {
	good := []struct {
		in       string
		from, to int
	}{{"7", 7, 7}, {"3-9", 3, 9}, {"4-4", 4, 4}, {"120-128", 120, 128}}
	for _, g := range good {
		from, to, err := ParseLines(g.in)
		if err != nil || from != g.from || to != g.to {
			t.Errorf("ParseLines(%q) = %d,%d,%v; quería %d,%d", g.in, from, to, err, g.from, g.to)
		}
	}
	for _, bad := range []string{"", "0", "0-4", "5-3", "a", "1-", "-2", "1-2-3", " 3", "3 - 4", "1.5", "-1"} {
		if _, _, err := ParseLines(bad); err == nil {
			t.Errorf("ParseLines(%q) debe fallar", bad)
		}
	}
}

func TestLoadCasesRejectsMalformed(t *testing.T) {
	const ok = "defects:\n  - { file: internal/a.go, lines: \"3\", what: x }\n"
	cases := []struct {
		name, yaml string
		problems   int
	}{
		{"bueno", "base: base1\nhead: head1\n" + ok, 0},
		{"bueno-con-spec", "base: base1\nhead: head1\nspec: specs/x/spec.md\n" + ok, 0},
		{"sin-base", "head: head1\n" + ok, 1},
		{"sin-head", "base: base1\n" + ok, 1},
		{"head-desconocido", "base: base1\nhead: nope\n" + ok, 1},
		{"base-desconocida", "base: nope\nhead: head1\n" + ok, 1},
		{"sin-defectos", "base: base1\nhead: head1\ndefects: []\n", 1},
		{"ruta-vacia", "base: base1\nhead: head1\ndefects:\n  - { file: \"\", lines: \"3\", what: x }\n", 1},
		{"ruta-absoluta", "base: base1\nhead: head1\ndefects:\n  - { file: /etc/passwd, lines: \"3\", what: x }\n", 1},
		{"ruta-unidad", "base: base1\nhead: head1\ndefects:\n  - { file: \"C:/x/a.go\", lines: \"3\", what: x }\n", 1},
		{"ruta-con-puntos", "base: base1\nhead: head1\ndefects:\n  - { file: ../a.go, lines: \"3\", what: x }\n", 1},
		{"ruta-con-puntos-medio", "base: base1\nhead: head1\ndefects:\n  - { file: internal/../../a.go, lines: \"3\", what: x }\n", 1},
		{"rango-al-reves", "base: base1\nhead: head1\ndefects:\n  - { file: a.go, lines: \"9-3\", what: x }\n", 1},
		{"rango-cero", "base: base1\nhead: head1\ndefects:\n  - { file: a.go, lines: \"0\", what: x }\n", 1},
		{"rango-basura", "base: base1\nhead: head1\ndefects:\n  - { file: a.go, lines: \"x-y\", what: x }\n", 1},
		{"spec-absoluta", "base: base1\nhead: head1\nspec: /abs/spec.md\n" + ok, 1},
		{"spec-con-puntos", "base: base1\nhead: head1\nspec: ../spec.md\n" + ok, 1},
		{"dos-defectos-malos", "base: base1\nhead: head1\ndefects:\n  - { file: ../a.go, lines: \"3\", what: x }\n  - { file: b.go, lines: \"4-2\", what: y }\n", 2},
		{"todo-mal", "defects: []\n", 3},
	}
	h := newHarness(t)
	for _, c := range cases {
		h.caseYAML(c.name, c.yaml)
	}
	loaded, skipped, err := LoadCases(h.root, nil)
	if err != nil || len(skipped) != 0 || len(loaded) != len(cases) {
		t.Fatalf("LoadCases: %d casos, skipped %v, err %v", len(loaded), skipped, err)
	}
	byName := map[string]Case{}
	for _, c := range loaded {
		byName[c.Name] = c
	}
	for _, c := range cases {
		got := byName[c.name].Problems(context.Background(), h.g)
		if len(got) != c.problems {
			t.Errorf("%s: %d problemas, quería %d: %v", c.name, len(got), c.problems, got)
		}
		for _, p := range got {
			if !strings.HasPrefix(p, c.name+": ") || len(p) <= len(c.name)+2 {
				t.Errorf("%s: la línea debe ser `<caso>: <problema>`: %q", c.name, p)
			}
		}
	}
}

func TestLoadCasesSkipsDrafts(t *testing.T) {
	h := newHarness(t)
	h.validCase("alfa", "")
	h.caseYAML("borrador", "draft: true\nsource: GH-1\nbase: \"\"\nhead: \"\"\ndefects: []\n")
	h.validCase("beta", "")

	cases, skipped, err := LoadCases(h.root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cases {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"alfa", "beta"}) || !slices.Equal(skipped, []string{"borrador"}) {
		t.Errorf("casos %v, omitidos %v", names, skipped)
	}
	if cases[0].Base != "base1" || cases[0].Source != "GH-1" || len(cases[0].Defects) != 1 || cases[0].Defects[0].Lines != "10-12" {
		t.Errorf("el caso se lee tal cual: %+v", cases[0])
	}

	only, _, err := LoadCases(h.root, []string{"beta"})
	if err != nil || len(only) != 1 || only[0].Name != "beta" {
		t.Errorf("--case beta: %v %v", only, err)
	}
}

func TestLoadCasesNoCases(t *testing.T) {
	h := newHarness(t)
	if _, _, err := LoadCases(h.root, nil); err == nil {
		t.Fatal("sin carpeta de casos debe rechazar")
	} else {
		_ = rejected(t, err, "no_cases")
	}

	h.caseYAML("borrador", "draft: true\ndefects: []\n")
	_, _, err := LoadCases(h.root, nil)
	_ = rejected(t, err, "no_cases")

	h.validCase("alfa", "")
	for _, names := range [][]string{{"inexistente"}, {"alfa", "inexistente"}, {"borrador"}, {"../alfa"}, {"ALFA"}, {"-x"}, {"a/b"}} {
		if _, _, err := LoadCases(h.root, names); err == nil {
			t.Errorf("--case %v debe rechazar", names)
		} else {
			_ = rejected(t, err, "no_cases")
		}
	}
}
