package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
)

// SplitPart es una hija propuesta en la `### División` del Brief.
type SplitPart struct{ Title, Scope string }

const (
	maxSplitParts = 6
	maxSplitTitle = 120
)

var splitBullet = regexp.MustCompile(`^- \*\*(.*?)\*\*:(.*)$`)

func splitInvalid(format string, a ...any) error {
	return &flow.Rejection{Code: "split_invalid", Reason: "la división del Brief no es válida: " + fmt.Sprintf(format, a...) +
		". Escribe en el Brief una `### División` con 2 a 6 viñetas `- **<título>**: <alcance>` (títulos distintos de 120 caracteres como máximo) y reporta otra vez."}
}

func isDivisionHeading(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "###") || strings.HasPrefix(t, "####") {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(strings.TrimLeft(t, "#")))
	return name == "división" || name == "division"
}

// ParseSplit lee la `### División` del Brief. Cualquier violación de las
// reglas (2 a 6 hijas, título y alcance no vacíos, título único y de hasta
// 120 runas) devuelve *flow.Rejection con código split_invalid.
func ParseSplit(brief string) ([]SplitPart, error) {
	lines := strings.Split(strings.ReplaceAll(brief, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if isDivisionHeading(l) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, splitInvalid("falta la sección `### División`")
	}
	var parts []SplitPart
	seen := map[string]bool{}
	var scope []string
	flush := func(line int) error {
		if len(parts) == 0 {
			return nil
		}
		p := &parts[len(parts)-1]
		p.Scope = strings.Join(scope, "\n")
		if strings.TrimSpace(p.Scope) == "" {
			return splitInvalid("la hija %q no tiene alcance (línea %d)", p.Title, line)
		}
		return nil
	}
	for i := start; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], " \t")
		n := i + 1
		if strings.HasPrefix(strings.TrimSpace(l), "#") && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
			break
		}
		if strings.TrimSpace(l) == "" {
			continue
		}
		if l[0] == ' ' || l[0] == '\t' {
			if len(parts) == 0 {
				return nil, splitInvalid("línea %d con sangría antes de la primera viñeta", n)
			}
			scope = append(scope, strings.TrimSpace(l))
			continue
		}
		m := splitBullet.FindStringSubmatch(l)
		if m == nil {
			return nil, splitInvalid("la línea %d no tiene el formato `- **<título>**: <alcance>`", n)
		}
		if err := flush(n); err != nil {
			return nil, err
		}
		title := strings.TrimSpace(m[1])
		switch {
		case title == "":
			return nil, splitInvalid("la viñeta de la línea %d no tiene título", n)
		case utf8.RuneCountInString(title) > maxSplitTitle:
			return nil, splitInvalid("el título %q (línea %d) pasa de %d caracteres", title, n, maxSplitTitle)
		case seen[title]:
			return nil, splitInvalid("el título %q (línea %d) está repetido", title, n)
		}
		seen[title] = true
		parts = append(parts, SplitPart{Title: title})
		scope = scope[:0]
		if s := strings.TrimSpace(m[2]); s != "" {
			scope = append(scope, s)
		}
	}
	if err := flush(len(lines)); err != nil {
		return nil, err
	}
	if len(parts) < 2 || len(parts) > maxSplitParts {
		return nil, splitInvalid("hay %d hijas y deben ser de 2 a %d", len(parts), maxSplitParts)
	}
	return parts, nil
}

// createChildren crea con el tracker las hijas del split que faltan, guarda
// cada una en cuanto existe y devuelve las líneas "<ID> · <título>" en el
// orden del Brief. Si falla una, el error lista lo ya creado y cómo reintentar.
func (e *Engine) createChildren(ctx context.Context, id string) ([]string, error) {
	cr, ok := e.Tracker.(tracker.Creator)
	if !ok {
		return nil, &flow.Rejection{Code: "no_creator", Reason: fmt.Sprintf("el tracker %s no permite crear tareas desde bflow; créalas a mano y retira %s con bflow drop", e.Tracker.Name(), id)}
	}
	rec, err := e.Store.Load(id)
	if err != nil {
		return nil, err
	}
	brief, err := e.specSection(rec, "brief")
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer el Brief de %s: %w", id, err)
	}
	parts, err := ParseSplit(brief)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, c := range rec.Split {
		ids[c.Title] = c.ID
	}
	for _, p := range parts {
		if _, done := ids[p.Title]; done {
			continue
		}
		task, err := cr.Create(ctx, p.Title, p.Scope+"\n\nParte de "+id+" · "+rec.Title)
		if err != nil {
			return nil, fmt.Errorf("no se pudo crear la hija %q de %s: %w; hijas ya creadas: %s; reintenta con: bflow approve %s --gate split",
				p.Title, id, err, createdList(ids), id)
		}
		ids[p.Title] = task.ID
		if _, err := e.Store.Update(id, func(r *store.Record, _ bool) error {
			r.Split = append(r.Split, store.SplitChild{Title: p.Title, ID: task.ID})
			return nil
		}); err != nil {
			return nil, fmt.Errorf("la hija %q se creó como %s pero no se pudo guardar: %w; hijas ya creadas: %s", p.Title, task.ID, err, createdList(ids))
		}
	}
	lines := make([]string, 0, len(parts))
	for _, p := range parts {
		lines = append(lines, ids[p.Title]+" · "+p.Title)
	}
	return lines, nil
}

func createdList(ids map[string]string) string {
	if len(ids) == 0 {
		return "ninguna"
	}
	var out []string
	for t, i := range ids {
		out = append(out, i+" · "+t)
	}
	return strings.Join(out, ", ")
}
