package review

import (
	"slices"
	"testing"
)

func TestParseMapQuestionTells(t *testing.T) {
	md := "## Preguntas de producto\n" +
		"- ¿Qué pasa con tareas anteriores a GH-29?\n" +
		"  - Muestra \"sin detalle por llamada\" (lo que hace el código)\n" +
		"  - Reconstruir desde transcripts\n" +
		"1. ¿Y si el RFC es genérico?\n" +
		"   - Lo rechaza con 422\n" +
		"   - Error con código 422\n" +
		"   - Lo acepta (actual)\n" +
		"- ¿Y vacío?\n" +
		"  * ✓ Devuelve 400\n" +
		"  * Es un no-op\n" +
		"  el código responde: 400 (x.go:1)\n"
	got := ParseMap(md).QuestionTells
	want := []string{
		"Muestra \"sin detalle por llamada\" (lo que hace el código)",
		"Lo acepta (actual)",
		"✓ Devuelve 400",
	}
	if !slices.Equal(got, want) {
		t.Errorf("tells = %q, want %q", got, want)
	}
}

func TestParseMapQuestionTellsIgnoresOtherSections(t *testing.T) {
	md := "## 🔴 Decisión\n  - `a.go`: es el actual\n## Docs\n- `x.md` actual\n" +
		"## Preguntas de producto\n- ¿Algo?\n  - Opción A\n  - Opción B\n  el código responde: A (a.go:1)\n  - el código responde: A\n" +
		"## 🟢 Mecánico\n  - lo que hace el código, hoy\n"
	if got := ParseMap(md).QuestionTells; len(got) != 0 {
		t.Errorf("tells = %q, want vacío", got)
	}
}
