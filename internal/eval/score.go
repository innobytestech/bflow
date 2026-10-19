package eval

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// refRe es una referencia ruta:N o ruta:N-M; la ruta lleva extensión.
var refRe = regexp.MustCompile("(?:^|[\\s(`\"'])((?:[A-Za-z]:)?[\\w./\\\\-]+\\.\\w+):(\\d+)(?:-(\\d+))?")

var driveRe = regexp.MustCompile(`^[A-Za-z]:/`)

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// excludedLine son las líneas que citan código sin acusarlo.
func excludedLine(l string) bool {
	l = strings.ToLower(l)
	return strings.Contains(l, "caminos:") || strings.Contains(l, "cubre:") || strings.Contains(l, "el código responde")
}

func excludedHeading(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	return strings.HasPrefix(t, "docs") || strings.HasPrefix(t, "pruebas sin ejecutar")
}

// relToWorktree normaliza p a relativa al worktree con "/"; ok=false si cae fuera.
func relToWorktree(p, worktree string) (string, bool) {
	p = strings.ReplaceAll(p, `\`, "/")
	wt := strings.TrimRight(strings.ReplaceAll(worktree, `\`, "/"), "/")
	if strings.HasPrefix(p, "/") || driveRe.MatchString(p) {
		pre := wt + "/"
		if wt == "" || len(p) <= len(pre) {
			return "", false
		}
		same := p[:len(pre)] == pre
		if !same && runtime.GOOS == "windows" {
			same = strings.EqualFold(p[:len(pre)], pre)
		}
		if !same {
			return "", false
		}
		p = p[len(pre):]
	}
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	if p == "" || strings.HasPrefix(p, "/") || driveRe.MatchString(p) {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	return p, true
}

// stripWorktree quita el prefijo del worktree antes de buscar referencias: la
// ruta del equipo puede llevar espacios o ~ que la expresión no admite.
func stripWorktree(text, worktree string) string {
	wt := strings.TrimRight(worktree, `/\`)
	if wt == "" {
		return text
	}
	flag := ""
	if runtime.GOOS == "windows" {
		flag = "(?i)"
	}
	for _, v := range []string{strings.ReplaceAll(wt, `\`, "/"), strings.ReplaceAll(wt, "/", `\`)} {
		text = regexp.MustCompile(flag+regexp.QuoteMeta(v)+`[/\\]`).ReplaceAllString(text, "")
	}
	return text
}

func findIn(text, worktree string) []Finding {
	var out []Finding
	text = stripWorktree(text, worktree)
	skipLevel := 0 // nivel del encabezado excluido en curso; 0 = ninguno
	for _, line := range strings.Split(text, "\n") {
		if m := headingRe.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			lvl := len(m[1])
			if skipLevel > 0 && lvl <= skipLevel {
				skipLevel = 0
			}
			if skipLevel == 0 && excludedHeading(m[2]) {
				skipLevel = lvl
			}
			continue
		}
		if skipLevel > 0 || excludedLine(line) {
			continue
		}
		for _, m := range refRe.FindAllStringSubmatch(line, -1) {
			file, ok := relToWorktree(m[1], worktree)
			if !ok {
				continue
			}
			from, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			to := from
			if m[3] != "" {
				if to, err = strconv.Atoi(m[3]); err != nil {
					continue
				}
			}
			if to < from {
				from, to = to, from
			}
			out = append(out, Finding{File: file, From: from, To: to})
		}
	}
	return out
}

// Findings saca los hallazgos del note y de cada .md de docs (nombre → contenido).
func Findings(note string, docs map[string]string, worktree string) []Finding {
	out := findIn(note, worktree)
	names := make([]string, 0, len(docs))
	for n := range docs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, findIn(docs[n], worktree)...)
	}
	return out
}

func hits(f Finding, file string, from, to int) bool {
	return f.File == file && f.From <= to+Tolerance && f.To >= from-Tolerance
}

// ScoreCase marca cada defecto como atrapado o no, y lista los hallazgos que
// no coinciden con ninguno.
func ScoreCase(defects []Defect, found []Finding) Score {
	s := Score{Caught: []Defect{}, Missed: []Defect{}, FalsePositives: []string{}}
	type rng struct {
		file     string
		from, to int
	}
	var ranges []rng
	for _, d := range defects {
		from, to, err := ParseLines(d.Lines)
		caught := false
		if err == nil {
			ranges = append(ranges, rng{d.File, from, to})
			for _, f := range found {
				if hits(f, d.File, from, to) {
					caught = true
					break
				}
			}
		}
		if caught {
			s.Caught = append(s.Caught, d)
		} else {
			s.Missed = append(s.Missed, d)
		}
	}
	seen := map[string]bool{}
	for _, f := range found {
		matched := false
		for _, r := range ranges {
			if hits(f, r.file, r.from, r.to) {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		key := fmt.Sprintf("%s:%d", f.File, f.From)
		if f.To != f.From {
			key += fmt.Sprintf("-%d", f.To)
		}
		if !seen[key] {
			seen[key] = true
			s.FalsePositives = append(s.FalsePositives, key)
		}
	}
	return s
}
