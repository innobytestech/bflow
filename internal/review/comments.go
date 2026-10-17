package review

import (
	"fmt"
	"regexp"
	"strings"

	"innobytes.tech/bflow/internal/vcs"
)

// DefaultCommentPatterns son los patrones que delatan un comentario que narra
// la tarea. Sin \b al final de según y decisión: \b de Go solo reconoce ASCII.
var DefaultCommentPatterns = []string{`\bR\d+\b`, `\bT\d+\b`, `(?i)\bspec\b`, `(?i)\bsegún`, `(?i)\bdecisi[oó]n`, `(?i)\bwalkthrough\b`}

// IsComment dice si la línea es un comentario por su prefijo (sin parser por
// lenguaje) y devuelve solo el texto del comentario. Un "//" final con espacio
// antes cuenta, y devuelve desde el "//".
func IsComment(text string) (comment string, ok bool) {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "//"), strings.HasPrefix(t, "#"), strings.HasPrefix(t, "/*"),
		strings.HasPrefix(t, "<!--"), strings.HasPrefix(t, "-- "), t == "--":
		return t, true
	case strings.HasPrefix(t, "*"):
		rest := t[1:]
		if rest == "" || rest[0] == ' ' || rest[0] == '/' {
			return t, true
		}
		return "", false
	}
	if i := strings.Index(t, " //"); i >= 0 {
		return t[i+1:], true
	}
	return "", false
}

// NarrativeComments devuelve "ruta:línea: texto" por cada línea agregada que es
// comentario y cumple algún patrón o contiene el ID de la tarea (sin distinguir
// mayúsculas). Se saltan los .md. Un patrón inválido es error.
func NarrativeComments(lines []vcs.AddedLine, taskID string, patterns []string) ([]string, error) {
	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("patrón de comentario %q inválido: %w", p, err)
		}
		res = append(res, re)
	}
	id := strings.ToLower(taskID)
	var out []string
	for _, l := range lines {
		if strings.HasSuffix(strings.ToLower(l.Path), ".md") {
			continue
		}
		c, ok := IsComment(l.Text)
		if !ok {
			continue
		}
		hit := id != "" && strings.Contains(strings.ToLower(c), id)
		for _, re := range res {
			if hit {
				break
			}
			hit = re.MatchString(c)
		}
		if hit {
			out = append(out, fmt.Sprintf("%s:%d: %s", l.Path, l.Line, c))
		}
	}
	return out, nil
}
