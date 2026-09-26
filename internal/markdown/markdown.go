// Package markdown convierte el Markdown que produce bflow (comentarios,
// reportes) a HTML para trackers que lo piden, y HTML de vuelta a texto.
// Cubre el subconjunto que bflow escribe; no es un parser CommonMark completo.
package markdown

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	boldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italRe   = regexp.MustCompile(`(^|[\s(])\*([^*\s][^*]*?)\*`)
	codeRe   = regexp.MustCompile("`([^`]+)`")
	linkRe   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	olRe     = regexp.MustCompile(`^\d+\.\s+`)
	headRe   = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	tableSep = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$`)
)

// inline convierte el formato en línea de un texto ya sin escapar.
func inline(s string) string {
	// El código se protege antes de escapar para no tocar su contenido.
	var codes []string
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, html.EscapeString(codeRe.FindStringSubmatch(m)[1]))
		return mark(len(codes) - 1)
	})
	s = html.EscapeString(s)
	s = linkRe.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = boldRe.ReplaceAllString(s, "<strong>$1</strong>")
	s = italRe.ReplaceAllString(s, "$1<em>$2</em>")
	for i, c := range codes {
		s = strings.Replace(s, mark(i), "<code>"+c+"</code>", 1)
	}
	return s
}

func cells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(strings.TrimSuffix(line, "|"), "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = inline(strings.TrimSpace(parts[i]))
	}
	return parts
}

// ToHTML convierte Markdown a HTML.
func ToHTML(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var b strings.Builder
	var para []string
	flush := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + strings.Join(para, "<br>") + "</p>")
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			flush()
		case strings.HasPrefix(t, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			b.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>")
		case headRe.MatchString(t):
			flush()
			m := headRe.FindStringSubmatch(t)
			n := string(rune('0' + len(m[1])))
			b.WriteString("<h" + n + ">" + inline(m[2]) + "</h" + n + ">")
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			flush()
			b.WriteString("<ul>")
			for ; i < len(lines); i++ {
				x := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(x, "- ") && !strings.HasPrefix(x, "* ") {
					break
				}
				b.WriteString("<li>" + inline(x[2:]) + "</li>")
			}
			i--
			b.WriteString("</ul>")
		case olRe.MatchString(t):
			flush()
			b.WriteString("<ol>")
			for ; i < len(lines) && olRe.MatchString(strings.TrimSpace(lines[i])); i++ {
				b.WriteString("<li>" + inline(olRe.ReplaceAllString(strings.TrimSpace(lines[i]), "")) + "</li>")
			}
			i--
			b.WriteString("</ol>")
		case strings.HasPrefix(t, "|") && i+1 < len(lines) && tableSep.MatchString(strings.TrimSpace(lines[i+1])):
			flush()
			b.WriteString("<table><thead><tr>")
			for _, c := range cells(t) {
				b.WriteString("<th>" + c + "</th>")
			}
			b.WriteString("</tr></thead><tbody>")
			for i += 2; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				b.WriteString("<tr>")
				for _, c := range cells(lines[i]) {
					b.WriteString("<td>" + c + "</td>")
				}
				b.WriteString("</tr>")
			}
			i--
			b.WriteString("</tbody></table>")
		default:
			para = append(para, inline(t))
		}
	}
	flush()
	return b.String()
}

var (
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	blankRe = regexp.MustCompile(`\n{2,}`)
)

// ToText convierte HTML a texto plano legible (para leer comentarios).
func ToText(h string) string {
	r := strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n", "</li>", "\n", "<li>", "- ",
		"</h1>", "\n", "</h2>", "\n", "</h3>", "\n", "</tr>", "\n", "</pre>", "\n")
	s := tagRe.ReplaceAllString(r.Replace(h), "")
	s = html.UnescapeString(s)
	s = blankRe.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

// mark es el marcador temporal de un fragmento de código en línea. Usa
// caracteres de control que no aparecen en texto normal ni cambian al escapar.
func mark(i int) string { return "\x02" + strconv.Itoa(i) + "\x03" }
