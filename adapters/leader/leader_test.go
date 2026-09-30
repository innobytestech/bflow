package leader

import (
	"os"
	"strings"
	"testing"
)

func TestLeaderRender(t *testing.T) {
	head := []byte("---\ndescription: prueba\n---\n\n")
	raw, err := os.ReadFile("bflow.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "{{ask_tool}}") {
		t.Fatal("el cuerpo común nombra la herramienta de preguntas con {{ask_tool}}")
	}

	got, err := Render(head, map[string]string{"ask_tool": "la herramienta X"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := string(head) + strings.ReplaceAll(string(raw), "{{ask_tool}}", "la herramienta X")
	if string(got) != want {
		t.Errorf("head + cuerpo con el marcador reemplazado:\n%s", got)
	}
	if strings.Contains(string(got), "{{") {
		t.Error("no queda ningún marcador")
	}

	if _, err := Render(head, nil); err == nil || !strings.Contains(err.Error(), "ask_tool") {
		t.Errorf("un marcador sin valor falla y lo nombra: %v", err)
	}
	if _, err := Render(head, map[string]string{"ask_tool": "x", "sobra": "y"}); err == nil || !strings.Contains(err.Error(), "sobra") {
		t.Errorf("una variable que el cuerpo no usa falla y la nombra: %v", err)
	}
	if _, err := Render([]byte("{{otro}}\n"), map[string]string{"ask_tool": "x", "otro": "y"}); err != nil {
		t.Errorf("los marcadores también se reemplazan en head: %v", err)
	}
}
