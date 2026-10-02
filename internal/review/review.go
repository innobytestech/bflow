// Package review mide cuánto del diff leyó el reviewer y lee del review-map lo
// que bflow exige. Es lógica pura: el git y el disco los pone el engine.
package review

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/guard"
)

const (
	// ReadsFile es el archivo de la tarea con las lecturas del reviewer.
	ReadsFile = "reads.jsonl"
	// WholeDiffMax son las líneas bajo las cuales un git diff sin ruta cubre todo.
	WholeDiffMax = 1500
	// maxListed son los archivos que Lines nombra antes de decir "y K más".
	maxListed = 20
)

// Read es una herramienta que usó el reviewer, tal como se guarda.
type Read struct {
	TS       time.Time `json:"ts"`
	Tool     string    `json:"tool"`            // guard.Read | guard.Bash | guard.Edit | guard.Write
	Paths    []string  `json:"paths,omitempty"` // relativas al repo, con /; pueden ser carpetas (pathspec de git)
	Whole    bool      `json:"whole,omitempty"` // git diff sin ruta
	ReadHook bool      `json:"read_hook"`       // vino de guard --reads
}

var (
	driveRe   = regexp.MustCompile(`^[A-Za-z]:(/|$)`)
	gitBashRe = regexp.MustCompile(`^/([A-Za-z])(/|$)`)
)

// absParts separa una ruta en unidad (en minúscula, "" si no hay) y resto con
// "/". abs dice si es absoluta. Con winRoot, "/c/..." de Git Bash es la unidad c.
func absParts(p string, winRoot bool) (drive, rest string, abs bool) {
	p = strings.ReplaceAll(p, `\`, "/")
	if driveRe.MatchString(p) {
		return strings.ToLower(p[:1]), p[2:], true
	}
	if winRoot {
		if m := gitBashRe.FindStringSubmatch(p); m != nil {
			return strings.ToLower(m[1]), p[2:], true
		}
	}
	return "", p, strings.HasPrefix(p, "/")
}

// Normalize deja p como ruta relativa al repo con "/" (R4). ok es false si cae
// fuera del repo. Acepta absolutas, relativas a cwd, con "\" o "/", "/c/..." de
// Git Bash y la unidad de Windows en cualquier caja. La raíz misma es ".".
func Normalize(root, cwd, p string) (string, bool) {
	rd, rr, _ := absParts(root, driveRe.MatchString(strings.ReplaceAll(root, `\`, "/")))
	winRoot := rd != ""
	if cwd == "" {
		cwd = root
	}
	d, r, isAbs := absParts(p, winRoot)
	if !isAbs {
		d, r, _ = absParts(cwd, winRoot)
		_, rel, _ := absParts(p, false)
		r = r + "/" + rel
	}
	r = path.Join("/", r)
	rr = path.Join("/", rr)
	if d != rd {
		return "", false
	}
	cmpR, cmpRoot := r, rr
	if winRoot {
		cmpR, cmpRoot = strings.ToLower(r), strings.ToLower(rr)
	}
	switch {
	case cmpR == cmpRoot:
		return ".", true
	case cmpRoot == "/":
		return strings.TrimPrefix(r, "/"), true
	case strings.HasPrefix(cmpR, cmpRoot+"/"):
		return r[len(rr)+1:], true
	}
	return "", false
}

var (
	statFlags = []string{"--stat", "--numstat", "--shortstat", "--name-only", "--name-status", "--dirstat"}
	readers   = []string{"cat", "type", "get-content", "head", "tail", "sed", "less", "more", "bat", "nl"}
)

// FromAction traduce una acción del reviewer a lo que se guarda (R2, R3).
func FromAction(a guard.Action, root, cwd string, readHook bool, now time.Time) Read {
	r := Read{TS: now, Tool: a.Tool, ReadHook: readHook}
	add := func(p string) {
		if n, ok := Normalize(root, cwd, p); ok && !slices.Contains(r.Paths, n) {
			r.Paths = append(r.Paths, n)
		}
	}
	// exists devuelve la ruta normalizada si existe en el repo.
	exists := func(p string, wantFile bool) (string, bool) {
		n, ok := Normalize(root, cwd, p)
		if !ok {
			return "", false
		}
		fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(n)))
		if err != nil || (wantFile && fi.IsDir()) {
			return "", false
		}
		return n, true
	}
	switch a.Tool {
	case guard.Read:
		add(a.Path)
	case guard.Bash:
		for _, seg := range guard.Segments(a.Command) {
			toks := strings.Fields(seg)
			for i := range toks {
				toks[i] = strings.Trim(toks[i], `"'`)
			}
			if len(toks) == 0 {
				continue
			}
			name := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(toks[0], `\`, "/"))), ".exe")
			switch {
			case name == "git":
				fromGit(toks[1:], &r, add, exists)
			case slices.Contains(readers, name):
				for _, t := range toks[1:] {
					if strings.HasPrefix(t, "-") {
						continue
					}
					if n, ok := exists(t, true); ok {
						add(n)
					}
				}
			}
		}
	}
	return r
}

// fromGit lee los argumentos de un `git` (sin el nombre) y anota lo que
// diff o show leen.
func fromGit(args []string, r *Read, add func(string), exists func(string, bool) (string, bool)) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if args[0] == "-C" || args[0] == "-c" {
			args = args[min(2, len(args)):]
			continue
		}
		args = args[1:]
	}
	if len(args) == 0 || (args[0] != "diff" && args[0] != "show") {
		return
	}
	sub, args := args[0], args[1:]
	var before, after []string
	dashes := false
	for i, t := range args {
		if t == "--" {
			dashes, after = true, args[i+1:]
			break
		}
		before = append(before, t)
	}
	for _, t := range before {
		for _, f := range statFlags {
			if t == f || strings.HasPrefix(t, f+"=") {
				return
			}
		}
	}
	found := len(after) > 0
	for _, t := range after {
		if n, ok := exists(t, false); ok {
			add(n)
		}
	}
	if !dashes {
		for _, t := range before {
			if strings.HasPrefix(t, "-") {
				continue
			}
			if i := strings.Index(t, ":"); sub == "show" && i > 0 && !driveRe.MatchString(t) {
				found = true
				add(t[i+1:])
			} else if n, ok := exists(t, false); ok {
				found = true
				add(n)
			}
		}
	}
	if sub == "diff" && !found {
		r.Whole = true
	}
}

// ParseReads lee reads.jsonl y salta las líneas inválidas.
func ParseReads(rd io.Reader) []Read {
	var out []Read
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Read
		if json.Unmarshal([]byte(line), &r) == nil && r.Tool != "" {
			out = append(out, r)
		}
	}
	return out
}

// IsDoc dice si la ruta es documentación: .md .mdx .rst .txt .adoc o bajo docs/.
func IsDoc(p string) bool {
	p = strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(p, "docs/") {
		return true
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".mdx", ".rst", ".txt", ".adoc":
		return true
	}
	return false
}

// Map es lo que bflow lee del review-map.
type Map struct {
	Red       []string // rutas de las viñetas 🔴, normalizadas (sin :línea ni #L), sin duplicados
	RedNoPath []string // viñetas 🔴 sin ruta inicial, recortadas a 80 runas
	Docs      []string // rutas de ## Docs
	HasDocs   bool
}

var (
	headingRe = regexp.MustCompile(`^(#+)\s+(.*)$`)
	bulletRe  = regexp.MustCompile(`^(?:[-*]|\d+\.)\s+(.*)$`)
	lineSufRe = regexp.MustCompile(`:\d+(-\d+)?$`)
)

// ParseMap lee las secciones 🔴 y Docs del review-map.
func ParseMap(md string) Map {
	var m Map
	redLvl, docsLvl := 0, 0
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if h := headingRe.FindStringSubmatch(line); h != nil {
			lvl := len(h[1])
			if redLvl > 0 && lvl <= redLvl {
				redLvl = 0
			}
			if docsLvl > 0 && lvl <= docsLvl {
				docsLvl = 0
			}
			if strings.Contains(h[2], "🔴") {
				redLvl = lvl
			}
			if strings.EqualFold(strings.TrimSpace(h[2]), "docs") {
				docsLvl, m.HasDocs = lvl, true
			}
			continue
		}
		b := bulletRe.FindStringSubmatch(line)
		if b == nil || (redLvl == 0 && docsLvl == 0) {
			continue
		}
		paths := leadingSpans(b[1])
		if redLvl > 0 {
			if len(paths) == 0 {
				r := []rune(strings.TrimSpace(b[1]))
				if len(r) > 80 {
					r = r[:80]
				}
				m.RedNoPath = append(m.RedNoPath, string(r))
			}
			for _, p := range paths {
				if !slices.Contains(m.Red, p) {
					m.Red = append(m.Red, p)
				}
			}
		}
		if docsLvl > 0 {
			for _, p := range paths {
				if !slices.Contains(m.Docs, p) {
					m.Docs = append(m.Docs, p)
				}
			}
		}
	}
	return m
}

// leadingSpans toma los code spans del inicio de la viñeta, separados por ", "
// o espacio, y los deja como rutas.
func leadingSpans(s string) []string {
	var out []string
	for strings.HasPrefix(s, "`") {
		end := strings.Index(s[1:], "`")
		if end < 0 {
			break
		}
		span := s[1 : 1+end]
		s = strings.TrimLeft(s[end+2:], ", ")
		if i := strings.Index(span, "#"); i >= 0 {
			span = span[:i]
		}
		span = lineSufRe.ReplaceAllString(strings.ReplaceAll(span, `\`, "/"), "")
		if span = strings.TrimPrefix(strings.TrimSpace(span), "./"); span != "" {
			out = append(out, span)
		}
	}
	return out
}

// Coverage es la cobertura de una corrida del reviewer.
type Coverage struct {
	Measured    bool     `json:"measured"`
	Total       int      `json:"total"` // archivos del diff
	Read        int      `json:"read"`
	Whole       bool     `json:"whole,omitempty"`  // un diff entero cubrió todo
	Unread      []string `json:"unread,omitempty"` // no leídos que no son pruebas ni docs
	UnreadOther int      `json:"unread_other,omitempty"`
	RedMissing  []string `json:"red_missing,omitempty"`
	RedOutside  []string `json:"red_outside,omitempty"`
}

// Measure calcula la cobertura de reads contra los archivos del diff (R6, R10, R11).
func Measure(diff []string, diffLines int, reads []Read, red []string, isTest func(string) bool) Coverage {
	c := Coverage{Total: len(diff)}
	var hooked []Read
	for _, r := range reads {
		if r.ReadHook {
			hooked = append(hooked, r)
		}
	}
	if len(hooked) == 0 {
		return c
	}
	c.Measured = true
	for _, r := range hooked {
		if r.Whole && diffLines < WholeDiffMax {
			c.Whole = true
		}
	}
	covered := func(f string) bool {
		if c.Whole {
			return true
		}
		for _, r := range hooked {
			for _, p := range r.Paths {
				if p == f || p == "." || strings.HasPrefix(f, p+"/") {
					return true
				}
			}
		}
		return false
	}
	for _, f := range diff {
		switch {
		case covered(f):
			c.Read++
		case (isTest != nil && isTest(f)) || IsDoc(f):
			c.UnreadOther++
		default:
			c.Unread = append(c.Unread, f)
		}
	}
	for _, p := range red {
		switch {
		case !slices.Contains(diff, p):
			c.RedOutside = append(c.RedOutside, p)
		case !covered(p):
			c.RedMissing = append(c.RedMissing, p)
		}
	}
	return c
}

// Lines es el texto de la cobertura para el display y stats, sin emojis (R13).
func (c Coverage) Lines() []string {
	if !c.Measured {
		return []string{"cobertura del reviewer: no medida"}
	}
	out := []string{fmt.Sprintf("el reviewer leyó %d de %d archivos del diff", c.Read, c.Total)}
	if len(c.Unread) > 0 {
		out = append(out, "sin leer (no son pruebas ni docs): "+list(c.Unread))
	}
	if c.UnreadOther > 0 {
		out = append(out, fmt.Sprintf("pruebas y docs sin leer: %d", c.UnreadOther))
	}
	if len(c.RedMissing) > 0 {
		out = append(out, "rutas rojas sin leer: "+list(c.RedMissing))
	}
	if len(c.RedOutside) > 0 {
		out = append(out, "rutas rojas fuera del diff: "+list(c.RedOutside))
	}
	return out
}

func list(ps []string) string {
	if len(ps) > maxListed {
		return strings.Join(ps[:maxListed], ", ") + fmt.Sprintf(", y %d más", len(ps)-maxListed)
	}
	return strings.Join(ps, ", ")
}

var sinCambioRe = regexp.MustCompile("^\\s*[-*]\\s+`([^`]+)`\\s*:\\s*sin cambio\\s*:\\s*(\\S.*)$")

// DocsPending devuelve las rutas de listed que no están en diff ni constan en
// docsReport como "- `ruta`: sin cambio: <motivo>" con motivo (R15).
func DocsPending(listed, diff []string, docsReport string) []string {
	clean := func(p string) string {
		return strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p), `\`, "/"), "./")
	}
	justified := map[string]bool{}
	for _, line := range strings.Split(docsReport, "\n") {
		if m := sinCambioRe.FindStringSubmatch(strings.TrimRight(line, " \t\r")); m != nil {
			justified[clean(m[1])] = true
		}
	}
	var out []string
	for _, p := range listed {
		p = clean(p)
		if !slices.Contains(diff, p) && !justified[p] && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}
