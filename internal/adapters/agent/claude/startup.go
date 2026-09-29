package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"innobytes.tech/bflow/internal/agents"
)

// Lo que Claude Code carga al iniciar cada sesión, según su documentación
// (code.claude.com/docs/en/memory, skills y sub-agents): los CLAUDE.md del
// usuario, del proyecto y de las carpetas superiores, con sus @imports; las
// reglas de .claude/rules sin `paths:`; las primeras 200 líneas o 25 KB del
// MEMORY.md, y el nombre y la descripción de cada skill y agente. Se cobra en
// cada llamada de la sesión principal.

const (
	memoryMaxLines = 200
	memoryMaxBytes = 25 << 10
	importDepth    = 4
)

// StartupContext lista lo que se carga al iniciar una sesión en root.
func (a Agent) StartupContext(root string) []agents.ContextSource {
	home, _ := os.UserHomeDir()
	return startupContext(root, home, a.Skills(root))
}

func startupContext(root, home string, skills []agents.Skill) []agents.ContextSource {
	var out []agents.ContextSource
	seen := map[string]bool{}
	var addMD func(label, path string, depth int)
	addMD = func(label, path string, depth int) {
		abs, _ := filepath.Abs(path)
		key := strings.ToLower(abs)
		if seen[key] {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		seen[key] = true
		out = append(out, agents.ContextSource{Label: label, Bytes: len(b)})
		if depth >= importDepth {
			return
		}
		for _, imp := range imports(b) {
			p := imp
			if rest, ok := strings.CutPrefix(p, "~/"); ok && home != "" {
				p = filepath.Join(home, rest)
			} else if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(path), p)
			}
			addMD(label+" → @"+imp, p, depth+1)
		}
	}

	if home != "" {
		addMD("~/.claude/CLAUDE.md", filepath.Join(home, ".claude", "CLAUDE.md"), 0)
	}
	// El proyecto y sus carpetas superiores, de la más cercana a la raíz.
	for dir, first := root, true; ; first = false {
		rel := func(name string) string {
			if first {
				return name
			}
			return filepath.ToSlash(filepath.Join(dir, name))
		}
		addMD(rel("CLAUDE.md"), filepath.Join(dir, "CLAUDE.md"), 0)
		if first {
			addMD(".claude/CLAUDE.md", filepath.Join(dir, ".claude", "CLAUDE.md"), 0)
			addMD("CLAUDE.local.md", filepath.Join(dir, "CLAUDE.local.md"), 0)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, d := range []struct{ label, dir string }{{".claude/rules/", filepath.Join(root, ".claude", "rules")}, {"~/.claude/rules/", filepath.Join(home, ".claude", "rules")}} {
		if home == "" && strings.HasPrefix(d.label, "~") {
			continue
		}
		_ = filepath.WalkDir(d.dir, func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || filepath.Ext(p) != ".md" {
				return nil
			}
			if b, err := os.ReadFile(p); err == nil && !pathScoped(b) {
				rel, _ := filepath.Rel(d.dir, p)
				addMD(d.label+filepath.ToSlash(rel), p, 0)
			}
			return nil
		})
	}
	if m := memoryPath(root, home); m != "" {
		if b, err := os.ReadFile(m); err == nil {
			loaded, cut := memoryLoaded(b)
			src := agents.ContextSource{Label: "MEMORY.md (memoria automática)", Bytes: loaded}
			if cut {
				src.Note = "se corta: pasa de 200 líneas o 25 KB y lo que sigue no se carga"
			}
			out = append(out, src)
		}
	}
	var n int
	for _, s := range skills {
		n += len(s.Name) + len(s.Description)
	}
	for _, a := range agentDescriptions(root, home) {
		n += len(a)
	}
	if n > 0 {
		out = append(out, agents.ContextSource{Label: "descripciones de skills y agentes", Bytes: n})
	}
	return out
}

// imports saca las rutas @ de un CLAUDE.md, fuera de bloques y spans de código.
var importRe = regexp.MustCompile(`(?:^|\s)@([~./\w-][^\s` + "`" + `]*)`)

func imports(b []byte) []string {
	var out []string
	fenced := false
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		parts := strings.Split(l, "`") // los pares son texto; los impares, código
		for i := 0; i < len(parts); i += 2 {
			for _, m := range importRe.FindAllStringSubmatch(parts[i], -1) {
				p := strings.TrimRight(m[1], ".,;:)") // puntuación de la oración
				if strings.Contains(p, "/") || strings.Contains(p, ".") {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// pathScoped dice si una regla tiene `paths:` en su frontmatter: esas se
// cargan solo al leer archivos que coinciden.
func pathScoped(b []byte) bool {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return false
	}
	fm, _, ok := strings.Cut(s[4:], "\n---")
	return ok && regexp.MustCompile(`(?m)^paths\s*:`).MatchString(fm)
}

// memoryPath es el MEMORY.md del proyecto: Claude Code nombra la carpeta con
// la ruta del repo, cambiando por "-" todo lo que no es letra ni número.
func memoryPath(root, home string) string {
	if home == "" {
		return ""
	}
	want := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(root, "-")
	projects := filepath.Join(home, ".claude", "projects")
	entries, _ := os.ReadDir(projects)
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), want) {
			return filepath.Join(projects, e.Name(), "memory", "MEMORY.md")
		}
	}
	return ""
}

// memoryLoaded es cuánto de MEMORY.md se carga y si se corta.
func memoryLoaded(b []byte) (int, bool) {
	n, lines := 0, 0
	for len(b) > 0 && lines < memoryMaxLines {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			i = len(b) - 1
		}
		if n+i+1 > memoryMaxBytes {
			return n, true
		}
		n += i + 1
		b = b[i+1:]
		lines++
	}
	return n, len(b) > 0
}

// agentDescriptions son las descripciones de los agentes del proyecto y del
// usuario, que la sesión principal ve para saber a quién delegar.
func agentDescriptions(root, home string) []string {
	dirs := []string{filepath.Join(root, ".claude", "agents")}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".claude", "agents"))
	}
	var out []string
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, "*.md"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(l), "description:"); ok {
					out = append(out, filepath.Base(f)+strings.TrimSpace(v))
					break
				}
			}
		}
	}
	return out
}

// CoauthorOff dice si Claude Code tiene apagado el trailer Co-Authored-By en
// sus commits: attribution.commit en false o "" (o el viejo
// includeCoAuthoredBy: false). Manda el archivo más específico: local,
// proyecto, usuario.
func (Agent) CoauthorOff(root string) bool {
	home, _ := os.UserHomeDir()
	return coauthorOff(root, home)
}

func coauthorOff(root, home string) bool {
	files := []string{filepath.Join(root, ".claude", "settings.local.json"), filepath.Join(root, ".claude", "settings.json")}
	if home != "" {
		files = append(files, filepath.Join(home, ".claude", "settings.json"))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			Attribution *struct {
				Commit any `json:"commit"`
			} `json:"attribution"`
			IncludeCoAuthoredBy *bool `json:"includeCoAuthoredBy"`
		}
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		if s.Attribution != nil && s.Attribution.Commit != nil {
			return s.Attribution.Commit == false || s.Attribution.Commit == ""
		}
		if s.IncludeCoAuthoredBy != nil {
			return !*s.IncludeCoAuthoredBy
		}
	}
	return false
}
