package engine

import (
	"strings"
	"testing"
)

func TestProductQuestionsHideAnswers(t *testing.T) {
	v := newEnv(t, "")
	v.e.Store.WriteFile("T-1", "reports/review-map.md", []byte(`# Review map

## 🔴 Decisión
- internal/x.go: valida el RFC

## Preguntas de producto
1. ¿Qué pasa si el RFC es genérico?
   - Lo acepta como cualquier otro
   - Lo rechaza con 422
   el código responde: lo rechaza (internal/x.go:12)
2. ¿Y si viene vacío?
   El código responde: error 400 (internal/x.go:20)

## 🟢 Mecánico
3 archivos
`))
	q, err := v.e.productQuestions("T-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "1. ¿Qué pasa si el RFC es genérico?\n   - Lo acepta como cualquier otro\n   - Lo rechaza con 422\n2. ¿Y si viene vacío?"
	if q != want {
		t.Errorf("preguntas:\n%s\nwant:\n%s", q, want)
	}
	v.e.Store.WriteFile("T-2", "reports/review-map.md", []byte("## 🔴 Decisión\n- algo\n"))
	if q, _ := v.e.productQuestions("T-2"); !strings.Contains(q, "no trae preguntas") {
		t.Errorf("sin sección: %q", q)
	}
}

func TestClipDisplay(t *testing.T) {
	long := strings.Repeat("línea\n", maxDisplayLines+10)
	got := clip(strings.TrimSpace(long), "bflow show T-1 review-map")
	if n := strings.Count(got, "\n"); n != maxDisplayLines || !strings.HasSuffix(got, "(10 líneas más: bflow show T-1 review-map)") {
		t.Errorf("recorte: %d saltos\n%s", n, got[len(got)-60:])
	}
	if clip("corto", "x") != "corto" {
		t.Error("lo corto no se toca")
	}
}
