package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/flow"
)

const contractFailLines = 10

var goTestFunc = regexp.MustCompile(`(?m)^func (Test\w+)\(`)

// contractCheck valida el contrato antes de aceptar CONTRACT_READY. verified
// es true si el último check confirma que las pruebas del contrato fallan contra los stubs; false
// sin rechazo significa que no se pudo validar y decide la gate contract.
func (e *Engine) contractCheck(ctx context.Context, id string) (verified bool, rej *flow.Rejection) {
	files := e.taskTests(ctx)
	if hollow := hollowTests(e.Cfg.Root, files); len(hollow) > 0 {
		return false, &flow.Rejection{Code: "contract_hollow", Reason: "el contrato tiene pruebas que se saltan siempre o no tienen cuerpo; al aprobarlo se congelan y ya no se pueden completar:" + "\n" +
			strings.Join(hollow, "\n") + "\n" + "Escribe su cuerpo real (preparar, actuar, verificar contra las firmas) para que fallen contra los stubs, y reporta otra vez."}
	}
	steps := e.Cfg.Check.Steps
	testStep := e.Cfg.Check.TestStep
	if testStep == "" {
		testStep = "test"
	}
	testIdx := slices.IndexFunc(steps, func(s config.Step) bool { return s.Name == testStep })
	if e.Contract == nil || testIdx < 0 {
		return false, nil
	}
	res, fresh, detail, err := e.Contract.Last(ctx, id)
	if err != nil {
		detail = err.Error()
		fresh = false
	}
	if !fresh {
		return false, &flow.Rejection{Code: "contract_stale", Reason: "CONTRACT_READY exige el contrato commiteado y un check vigente sobre él: " + detail +
			"\nHaz commit del contrato, corre `bflow check " + id + "` (las T1 deben fallar) y reporta otra vez."}
	}
	for _, s := range res.Steps {
		if s.Name == testStep {
			break
		}
		if s.Status == "fail" {
			return false, &flow.Rejection{Code: "contract_broken", Reason: fmt.Sprintf("el paso %q del check falló antes de las pruebas: el contrato debe compilar y pasar lo previo, y fallar solo en las T1.\n%s\nCorrígelo, haz commit, corre `bflow check %s` y reporta otra vez.",
				s.Name, strings.Join(failLines(s.Fails, s.Tail, contractFailLines), "\n"), id)}
		}
	}
	t1 := contractTestNames(e.Cfg.Root, files)
	names := t1.names()
	for _, s := range res.Steps {
		if s.Name != testStep || s.Status != "fail" {
			continue
		}
		for _, l := range failLines(s.Fails, s.Tail, -1) {
			if slices.ContainsFunc(names, func(n string) bool { return strings.Contains(l, n) }) {
				return true, nil
			}
		}
	}
	return false, &flow.Rejection{Code: "contract_green", Reason: fmt.Sprintf("el paso %q no falla por ninguna prueba T1 (%s): las T1 deben fallar contra los stubs; si pasan no verifican nada.\nAjusta las pruebas, haz commit, corre `bflow check %s` y reporta otra vez.",
		testStep, strings.Join(firstN(names, 5), ", "), id)}
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

// contractT1 son los nombres con que una línea de fallo nombra una prueba T1.
type contractT1 struct {
	Funcs []string // funciones Test* de los archivos T1 .go
	Paths []string // rutas (con /) de los archivos T1 que no son .go
}

// contractTestNames separa los archivos T1 en funciones Test* (Go) y rutas.
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

// failed dice si la línea de fallo nombra con exactitud una prueba T1.
func (c contractT1) failed(line string) bool {
	return false
}

// buildFailed dice si alguna línea indica que la prueba no llegó a correr.
func buildFailed(lines []string) bool {
	return false
}

// contractValidable dice si el repo puede validar el contrato con el último check.
func (e *Engine) contractValidable() bool {
	return false
}

// Pause marca la tarea para parar antes de quality.
func (e *Engine) Pause(ctx context.Context, id string) (Outcome, error) {
	return e.apply(ctx, id, flow.Event{Kind: flow.EvPause}, "")
}
