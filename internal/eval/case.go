package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// EvalsDir es donde viven los casos y los resultados, relativa a la raíz.
const EvalsDir = ".bflow/evals"

// CasesDir son los casos del reviewer, relativa a la raíz.
const CasesDir = EvalsDir + "/reviewer"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func validName(n string) bool { return nameRe.MatchString(n) && !strings.Contains(n, "..") }

var linesRe = regexp.MustCompile(`^(\d+)(?:-(\d+))?$`)

// ParseLines lee "N" o "N-M" con 1 ≤ N ≤ M.
func ParseLines(s string) (from, to int, err error) {
	m := linesRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, fmt.Errorf("rango %q: use N o N-M", s)
	}
	from, err = strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, fmt.Errorf("rango %q: número demasiado grande", s)
	}
	to = from
	if m[2] != "" {
		if to, err = strconv.Atoi(m[2]); err != nil {
			return 0, 0, fmt.Errorf("rango %q: número demasiado grande", s)
		}
	}
	if from < 1 || to < from {
		return 0, 0, fmt.Errorf("rango %q: debe cumplir 1 <= N <= M", s)
	}
	return from, to, nil
}

// LoadCases lee los casos de CasesDir (todos, o solo names). Los borradores se
// omiten y se listan en skipped; si no queda ninguno para correr, rechaza con no_cases.
func LoadCases(root string, names []string) (cases []Case, skipped []string, err error) {
	dir := filepath.Join(root, filepath.FromSlash(CasesDir))
	ents, _ := os.ReadDir(dir)
	have := map[string]bool{}
	for _, e := range ents {
		if e.IsDir() && validName(e.Name()) {
			have[e.Name()] = true
		}
	}
	want := names
	if len(want) == 0 {
		for _, e := range ents {
			if have[e.Name()] {
				want = append(want, e.Name())
			}
		}
	}
	for _, n := range want {
		if !validName(n) || !have[n] {
			return nil, nil, &Rejection{"no_cases", fmt.Sprintf("el caso %q no existe en %s", n, CasesDir)}
		}
	}
	seen := map[string]bool{}
	for _, n := range want {
		if seen[n] {
			continue
		}
		seen[n] = true
		c := Case{Name: n}
		raw, rerr := os.ReadFile(filepath.Join(dir, n, "case.yaml"))
		if rerr == nil {
			rerr = yaml.Unmarshal(raw, &c)
		}
		c.Name = n
		c.parseErr = rerr
		if c.Draft {
			skipped = append(skipped, n)
			continue
		}
		cases = append(cases, c)
	}
	if len(cases) == 0 {
		return nil, skipped, &Rejection{"no_cases", "no hay casos para correr en " + CasesDir + " (los borradores con draft: true se omiten)"}
	}
	return cases, skipped, nil
}

// badRel dice por qué una ruta relativa al worktree no sirve ("" si sirve).
func badRel(p string) string {
	switch {
	case strings.TrimSpace(p) == "":
		return "ruta vacía"
	case strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || strings.Contains(p, ":"):
		return "la ruta debe ser relativa"
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return "la ruta no puede tener .."
		}
	}
	return ""
}

// Problems da una línea "<caso>: <problema>" por cada problema del caso.
func (c Case) Problems(ctx context.Context, g Git) []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, c.Name+": "+fmt.Sprintf(format, a...)) }
	if c.parseErr != nil {
		add("case.yaml ilegible: %v", c.parseErr)
		return out
	}
	for _, ref := range []struct{ field, val string }{{"base", c.Base}, {"head", c.Head}} {
		switch {
		case strings.TrimSpace(ref.val) == "":
			add("falta %s", ref.field)
		case strings.HasPrefix(ref.val, "-"):
			add("%s %q no es un commit", ref.field, ref.val)
		default:
			if _, err := g.ResolveCommit(ctx, ref.val); err != nil {
				add("%s %q no resuelve a un commit", ref.field, ref.val)
			}
		}
	}
	if c.Spec != "" {
		if why := badRel(c.Spec); why != "" {
			add("spec: %s", why)
		}
	}
	if len(c.Defects) == 0 {
		add("no tiene defectos esperados")
	}
	for i, d := range c.Defects {
		if why := badRel(d.File); why != "" {
			add("defecto %d: file: %s", i+1, why)
		}
		if _, _, err := ParseLines(d.Lines); err != nil {
			add("defecto %d: lines: %v", i+1, err)
		}
	}
	return out
}
