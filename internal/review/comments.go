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
// antes cuenta, y devuelve desde el "//", si queda fuera de comillas dobles,
// simples y backticks de la misma línea. Una directiva del preprocesador
// pegada al "#" (#include, #define...) no es comentario "#".
func IsComment(text string) (comment string, ok bool) {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(t, "#") && !isDirective(t[1:]):
		return t, true
	case strings.HasPrefix(t, "//"), strings.HasPrefix(t, "/*"),
		strings.HasPrefix(t, "<!--"), strings.HasPrefix(t, "-- "), t == "--":
		return t, true
	case strings.HasPrefix(t, "*"):
		rest := t[1:]
		if rest == "" || rest[0] == ' ' || rest[0] == '/' {
			return t, true
		}
		return "", false
	}
	if i := trailingSlashes(t); i >= 0 {
		return t[i:], true
	}
	return "", false
}

var directives = []string{"include_next", "include", "import", "define", "undef", "ifdef", "ifndef", "if",
	"elifdef", "elifndef", "elif", "else", "endif", "pragma", "error", "warning", "line", "endregion", "region"}

// isDirective dice si rest (lo que sigue al "#") empieza con una directiva
// completa: la palabra no puede continuar con letra, dígito o "_".
func isDirective(rest string) bool {
	for _, d := range directives {
		if r, ok := strings.CutPrefix(rest, d); ok {
			if r == "" {
				return true
			}
			c := r[0]
			if !(c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80) {
				return true
			}
		}
	}
	return false
}

// trailingSlashes devuelve el índice del primer "//" fuera de comillas que
// tenga espacio o tab justo antes, o -1. "\" escapa dentro de " y ', no
// dentro de backticks.
func trailingSlashes(t string) int {
	var q byte
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case q != 0:
			if c == '\\' && q != '`' {
				i++
			} else if c == q {
				q = 0
			}
		case c == '"' || c == '\'' || c == '`':
			q = c
		case c == '/' && i > 0 && i+1 < len(t) && t[i+1] == '/' && (t[i-1] == ' ' || t[i-1] == '	'):
			return i
		}
	}
	return -1
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
