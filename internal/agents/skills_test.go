package agents

import "testing"

func TestProcessTerms(t *testing.T) {
	process := []string{
		"Aprobación ligera de una spec en estado specReady. Úsalo cuando una feature llegue a specReady o el humano quiera revisar o aprobar una spec.",
		"Discovery de una feature SDD en estado pending o discovery.",
		"Walkthrough narrado previo al PR para una feature en estado documented.",
		`Orquesta el flujo SDD del repo. Úsalo siempre que el usuario pida avanzar, retomar o arrancar una feature o hotfix, pregunte "qué sigue", mencione un ID de PLANE.`,
		"Creates a branch and opens a pull request for the current ticket.",
	}
	for _, d := range process {
		if ProcessTerms(d) == nil {
			t.Errorf("debería avisar: %q", d)
		}
	}
	other := []string{
		"Interview the user relentlessly about a plan or design until reaching shared understanding, resolving each branch of the decision tree.",
		"Use the codebase knowledge graph for structural code queries.",
		"Convenciones de handlers HTTP en Go: errores, validación y pruebas de tabla.",
		"",
	}
	for _, d := range other {
		if got := ProcessTerms(d); got != nil {
			t.Errorf("no debería avisar %q: %v", d, got)
		}
	}
}
