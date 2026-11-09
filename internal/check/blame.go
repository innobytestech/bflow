package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/config"
)

// Blame dice a qué archivos apunta la salida de un paso previo a las pruebas
// que falló. Se calcula sobre la salida completa del paso (Tail es un extracto;
// con baseline new-only, también lo que el filtro descarta) y no cambia su resultado:
// la usa el contrato para no culpar a la tarea de un rojo que ya estaba en la base.
type Blame struct {
	// Findings son las rutas con forma de hallazgo: primer elemento de la línea
	// o seguidas de un número de línea.
	Findings []string `json:"findings,omitempty"`
	// Mentions son las demás rutas que nombra la salida (eco del comando, ayuda,
	// un import sin extensión).
	Mentions []string `json:"mentions,omitempty"`
	// Opaque dice por qué el rojo no se puede atribuir a archivos: el paso
	// compila, corre pruebas, busca vulnerabilidades o no llegó a revisar el código.
	Opaque string `json:"opaque,omitempty"`
	// More: la salida nombra más rutas de las que se guardan.
	More bool `json:"more,omitempty"`
}

const maxBlamePaths = 200

var (
	blamePathRe = regexp.MustCompile(`(?:[A-Za-z]:/)?(?:[\w.@~+%-]+/)*[\w.@~+%-]*\.[A-Za-z][A-Za-z0-9]{0,9}\b`)
	slashRe     = regexp.MustCompile(`(?:[\w.@~+%-]+/)+[\w.@~+%-]+`) // rutas sin extensión, como un import
	lineNumRe   = regexp.MustCompile(`^(?::\d|\(\d)`)
	escRe       = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`) // colores de la terminal
	// buildLikeRe: pasos cuyo rojo dice que el código no compila; en ellos un
	// archivo intacto puede romperse por un cambio en otro.
	buildLikeRe = regexp.MustCompile(`(?i)\b(build|compile|tsc|vue-tsc|typecheck|type-check|types|vet|staticcheck|mypy|pyright|clippy|svelte-check|astro|phpstan|psalm)\b`)
	// testLikeRe: pasos que corren pruebas; una prueba intacta falla por el
	// código que la rama cambió.
	testLikeRe = regexp.MustCompile(`(?i)\b(test|tests|spec|specs|e2e|vitest|jest|karma|pytest|mocha|playwright|cypress)\b`)
	// brokenRe: líneas que dicen que el paso no terminó de revisar el código o
	// que encontró un error de tipos o de compilación.
	brokenRe = regexp.MustCompile(`\[(build|setup) failed\]|^# \S+$|^panic: |^fatal error: |Traceback \(most recent call last\)|` +
		`^\s*(Uncaught )?\w*(Error|Exception)( \[\w+\])?: |^\s+at .*:\d+:\d+\)?$|(?i:\berror ts\(?\d+)|\((typecheck|compile)\)|\[ERROR\]|` +
		`^error\[E\d+\]|\berror CS\d+|: error: .*\[[\w-]+\]$|out of memory|signal: killed|^Killed$|^exec: `)
)

// blame resume la salida out de un paso que falló con exit.
func (r *Runner) blame(st config.Step, out string, exit int, runErr error) *Blame {
	b := &Blame{}
	lines := strings.Split(escRe.ReplaceAllString(strings.ReplaceAll(out, "\r\n", "\n"), ""), "\n")
	switch {
	case runErr != nil:
		b.Opaque = "el paso no se pudo ejecutar: " + runErr.Error()
	case exit < 0 || exit >= 128:
		b.Opaque = fmt.Sprintf("el paso no terminó solo: lo cortó una señal o un fallo del proceso (código %d)", exit)
	case isVulnStep(st):
		b.Opaque = "el paso busca vulnerabilidades en dependencias"
	case buildLikeRe.MatchString(st.Name + " " + st.Run):
		b.Opaque = "el paso compila o revisa tipos: un archivo intacto puede romperse por un cambio en otro"
	case testLikeRe.MatchString(st.Name + " " + st.Run):
		b.Opaque = "el paso corre pruebas: una prueba intacta puede fallar por el código que cambió la rama"
	}
	if b.Opaque == "" {
		if i := slices.IndexFunc(lines, brokenRe.MatchString); i >= 0 {
			b.Opaque = "la salida indica que el paso no terminó de revisar el código: " + clip(strings.TrimSpace(lines[i]), 160)
		}
	}
	root := strings.ToLower(filepath.ToSlash(filepath.Clean(r.Root))) + "/"
	for _, l := range lines {
		l = stripPrefixFold(strings.ReplaceAll(l, `\`, "/"), root)
		first := len(l) - len(strings.TrimLeft(l, " \t"))
		files := blamePathRe.FindAllStringIndex(l, -1)
		for _, m := range files {
			if strings.HasSuffix(l[:m[0]], "://") {
				continue
			}
			p := l[m[0]:m[1]]
			for strings.HasPrefix(p, "./") {
				p = p[2:]
			}
			if m[0] == first || lineNumRe.MatchString(l[m[1]:]) {
				b.Findings = b.add(b.Findings, p)
			} else {
				b.Mentions = b.add(b.Mentions, p)
			}
		}
		for _, m := range slashRe.FindAllStringIndex(l, -1) {
			inFile := slices.ContainsFunc(files, func(f []int) bool { return m[0] < f[1] && f[0] < m[1] })
			if !inFile && !strings.HasSuffix(l[:m[0]], "://") {
				b.Mentions = b.add(b.Mentions, l[m[0]:m[1]])
			}
		}
	}
	b.Mentions = slices.DeleteFunc(b.Mentions, func(p string) bool { return slices.Contains(b.Findings, p) })
	return b
}

func (b *Blame) add(list []string, p string) []string {
	if slices.Contains(list, p) {
		return list
	}
	if len(b.Findings)+len(b.Mentions) >= maxBlamePaths {
		b.More = true
		return list
	}
	return append(list, p)
}

// stripPrefixFold quita de l toda aparición de prefix, sin distinguir mayúsculas
// (prefix ya en minúsculas).
func stripPrefixFold(l, prefix string) string {
	low := strings.ToLower(l)
	if len(low) != len(l) {
		return l
	}
	var b strings.Builder
	for {
		i := strings.Index(low, prefix)
		if i < 0 {
			b.WriteString(l)
			return b.String()
		}
		b.WriteString(l[:i])
		l, low = l[i+len(prefix):], low[i+len(prefix):]
	}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
