package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/check"
	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/flow"
)

const contractFailLines = 10

var goTestFunc = regexp.MustCompile(`(?m)^func (Test\w+)\(`)

// contractCheck valida el contrato antes de aceptar CONTRACT_READY. verified
// es true si el último check confirma que las pruebas del contrato fallan contra los stubs; false
// sin rechazo significa que no se pudo validar y decide la gate contract. ignored son los pasos
// previos en rojo que no se le cobran a la tarea porque el rojo ya estaba en la base.
func (e *Engine) contractCheck(ctx context.Context, id string) (verified bool, ignored []baseRed, rej *flow.Rejection) {
	files := e.taskTests(ctx)
	if hollow := hollowTests(e.Cfg.Root, files); len(hollow) > 0 {
		return false, nil, &flow.Rejection{Code: "contract_hollow", Reason: "el contrato tiene pruebas que se saltan siempre o no tienen cuerpo; al aprobarlo se congelan y ya no se pueden completar:" + "\n" +
			strings.Join(hollow, "\n") + "\n" + "Escribe su cuerpo real (preparar, actuar, verificar contra las firmas) para que fallen contra los stubs, y reporta otra vez."}
	}
	testStep := e.Cfg.Check.TestStep
	if testStep == "" {
		testStep = "test"
	}
	if !e.contractValidable() {
		return false, nil, nil
	}
	res, fresh, detail, err := e.Contract.Last(ctx, id)
	if err != nil {
		detail = err.Error()
		fresh = false
	}
	if !fresh {
		return false, nil, &flow.Rejection{Code: "contract_stale", Reason: "CONTRACT_READY exige el contrato commiteado y un check vigente sobre él: " + detail +
			"\nHaz commit del contrato, corre `bflow check " + id + "` (las T1 deben fallar) y reporta otra vez."}
	}
	var touched []string
	var touchedErr error
	loaded := false
	for _, s := range res.Steps {
		if s.Name == testStep {
			break
		}
		if s.Status != "fail" {
			continue
		}
		if !loaded {
			touched, touchedErr = e.branchFiles(ctx)
			loaded = true
		}
		var why string
		if touchedErr != nil {
			why = "no se pudo calcular qué archivos tocó la rama: " + touchedErr.Error()
		} else {
			var found []string
			if found, why = e.redFromBase(ctx, s, touched); why == "" {
				ignored = append(ignored, baseRed{Step: s.Name, Files: found})
				continue
			}
		}
		return false, ignored, &flow.Rejection{Code: "contract_broken", Reason: fmt.Sprintf("el paso %q del check falló antes de las pruebas: el contrato debe compilar y pasar lo previo, y fallar solo en las T1.\n%s\nNo cuenta como un rojo que ya estaba en la base: %s.%s\nCorrígelo, haz commit, corre `bflow check %s` y reporta otra vez.",
			s.Name, strings.Join(failLines(s.Fails, s.Tail, contractFailLines), "\n"), why, ignoredNote(ignored), id)}
	}
	t1 := contractTestNames(e.Cfg.Root, files)
	names := t1.names()
	for _, s := range res.Steps {
		if s.Name != testStep || s.Status != "fail" {
			continue
		}
		lines := failLines(s.Fails, s.Tail, -1)
		if buildFailed(lines) {
			return false, ignored, nil
		}
		if slices.ContainsFunc(lines, t1.failed) {
			return true, ignored, nil
		}
	}
	return false, ignored, &flow.Rejection{Code: "contract_green", Reason: fmt.Sprintf("el paso %q no falla por ninguna prueba T1 (%s): las T1 deben fallar contra los stubs; si pasan no verifican nada.%s\nAjusta las pruebas, haz commit, corre `bflow check %s` y reporta otra vez.",
		testStep, strings.Join(firstN(names, 5), ", "), ignoredNote(ignored), id)}
}

// baseRed es un paso previo a las pruebas que falló solo por archivos que la
// rama no tocó: el rojo ya estaba en la base y el contrato no se lo cobra a la tarea.
type baseRed struct {
	Step  string
	Files []string
}

func (b baseRed) String() string {
	files := strings.Join(firstN(b.Files, 10), ", ")
	if n := len(b.Files) - 10; n > 0 {
		files += fmt.Sprintf(" y %d más", n)
	}
	return fmt.Sprintf("se ignoró el paso %q del check: falla solo en archivos que esta rama no tocó (el rojo ya estaba en la base): %s", b.Step, files)
}

func ignoredNote(ignored []baseRed) string {
	var b strings.Builder
	for _, r := range ignored {
		b.WriteString("\n" + r.String() + ".")
	}
	return b.String()
}

// branchFiles son los archivos que tocó la rama: el diff contra la base más lo
// que está sin commitear, como el baseline new-only del check, y además la ruta
// vieja de lo que borró o renombró.
func (e *Engine) branchFiles(ctx context.Context) ([]string, error) {
	if e.Git == nil {
		return nil, errors.New("el repo no tiene git")
	}
	return check.BranchFiles(ctx, e.Git, e.diffBase())
}

// redFromBase dice si el rojo del paso s ya estaba en la base: tiene hallazgos,
// todos en archivos que git rastrea, su salida no nombra ningún archivo que la
// rama tocó y la rama no tocó lo que configura al paso. Si no, why dice por
// qué. Ante la duda, no.
func (e *Engine) redFromBase(ctx context.Context, s check.StepResult, touched []string) (files []string, why string) {
	b := s.Blame
	switch {
	case b != nil && b.Opaque != "":
		return nil, b.Opaque
	case b == nil || len(b.Findings) == 0:
		return nil, "la salida no tiene hallazgos atribuibles a archivos del repo (o el check es de una versión de bflow que no los registraba)"
	case b.More:
		return nil, "la salida nombra más rutas de las que bflow guarda"
	}
	for i, p := range slices.Concat(b.Findings, b.Mentions) {
		if t := check.TouchedBy(p, touched, i < len(b.Findings)); t != "" {
			return nil, "la salida nombra " + t + ", que esta rama tocó"
		}
	}
	for _, f := range b.Findings {
		dir := strings.ToLower(path.Dir(f))
		for _, m := range b.Mentions {
			if strings.HasPrefix(m, "./") || strings.HasPrefix(m, "../") {
				if t := check.TouchedBy(path.Join(dir, m), touched, false); t != "" {
					return nil, "la salida nombra " + t + ", que esta rama tocó"
				}
			}
		}
		if t := check.SameDir(f, touched); t != "" {
			return nil, "la rama tocó " + t + ", junto a " + f + ": un archivo vecino puede cambiar lo que se revisa"
		}
	}
	if t := check.ToolingTouched(e.Cfg.Root, []string{s.Cmd}, b.Mentions, touched); t != "" {
		return nil, "la rama tocó " + t + ", que puede cambiar lo que revisa el paso"
	}
	for _, f := range b.Findings {
		if !check.RepoFile(e.Cfg.Root, f) {
			return nil, "el hallazgo en " + f + " no es de un archivo del repo"
		}
	}
	tracked, err := e.Git.Tracked(ctx, b.Findings)
	if err != nil {
		return nil, "no se pudo saber qué archivos rastrea git: " + err.Error()
	}
	for _, f := range b.Findings {
		if !slices.Contains(tracked, f) {
			return nil, "el hallazgo en " + f + " es de un archivo que git no rastrea (generado o ignorado)"
		}
	}
	return b.Findings, ""
}

// failLines son las líneas de fallo del paso: Fails, o las del Tail si no hay.
func failLines(fails []string, tail string, max int) []string {
	out := fails
	if len(out) == 0 {
		for _, l := range strings.Split(tail, "\n") {
			if strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
	}
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// contractT1 son los nombres con que una línea de fallo nombra una prueba del contrato.
type contractT1 struct {
	Funcs []string // funciones Test* de los archivos .go del contrato
	Paths []string // rutas (con /) de los archivos del contrato que no son .go
}

// contractTestNames separa los archivos del contrato en funciones Test* (Go) y rutas.
func contractTestNames(root string, files []string) contractT1 {
	var out contractT1
	for _, f := range files {
		f = strings.ReplaceAll(f, `\`, "/")
		if !strings.HasSuffix(f, ".go") {
			if !slices.Contains(out.Paths, f) {
				out.Paths = append(out.Paths, f)
			}
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		for _, m := range goTestFunc.FindAllSubmatch(b, -1) {
			if n := string(m[1]); !slices.Contains(out.Funcs, n) {
				out.Funcs = append(out.Funcs, n)
			}
		}
	}
	return out
}

// names son Funcs y luego Paths, para el mensaje contract_green.
func (c contractT1) names() []string {
	return append(slices.Clone(c.Funcs), c.Paths...)
}

// failed dice si la línea de fallo nombra con exactitud una prueba del contrato.
func (c contractT1) failed(line string) bool {
	t := strings.TrimSpace(line)
	for _, f := range c.Funcs {
		if rest, ok := strings.CutPrefix(t, "--- FAIL: "+f); ok && (rest == "" || rest[0] == ' ' || rest[0] == '/') {
			return true
		}
	}
	n := strings.ReplaceAll(line, `\`, "/")
	for _, p := range c.Paths {
		for from := 0; from <= len(n); {
			i := strings.Index(n[from:], p)
			if i < 0 {
				break
			}
			i += from
			end := i + len(p)
			if (i == 0 || !pathByte(n[i-1])) && (end >= len(n) || !pathByte(n[end])) {
				return true
			}
			from = i + 1
		}
	}
	return false
}

func pathByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-'
}

// buildFailed dice si alguna línea indica que la prueba no llegó a correr.
func buildFailed(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, "[build failed]") || strings.Contains(l, "[setup failed]") {
			return true
		}
	}
	return false
}

// contractValidable dice si el repo puede validar el contrato con el último check.
func (e *Engine) contractValidable() bool {
	if e.Contract == nil {
		return false
	}
	testStep := e.Cfg.Check.TestStep
	if testStep == "" {
		testStep = "test"
	}
	return slices.ContainsFunc(e.Cfg.Check.Steps, func(s config.Step) bool { return s.Name == testStep })
}

// Pause marca la tarea para parar antes de quality.
func (e *Engine) Pause(ctx context.Context, id string) (Outcome, error) {
	return e.apply(ctx, id, flow.Event{Kind: flow.EvPause}, "")
}
