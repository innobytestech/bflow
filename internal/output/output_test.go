package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestExitCodes(t *testing.T) {
	cases := []struct {
		name string
		env  Envelope
		want int
	}{
		{"ok", OK("advanced", nil, nil), ExitOK},
		{"error", Fail("config_invalid", errors.New("x")), ExitError},
		{"rejected", Rejected("transition_invalid", "no se puede"), ExitRejected},
	}
	for _, c := range cases {
		if got := c.env.ExitCode(); got != c.want {
			t.Errorf("%s: exit=%d, want %d", c.name, got, c.want)
		}
	}
}

func TestJSONShape(t *testing.T) {
	env := OK("advanced", map[string]any{"id": "LOCAL-1"}, &Next{
		Action:   ActionAsk,
		Gate:     "spec",
		Question: "¿Apruebas el spec?",
		Options:  []Option{{ID: "approve", Label: "Aprobar"}, {ID: "reject", Label: "Rechazar"}},
	})
	var buf bytes.Buffer
	if err := Write(&buf, env, true); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("salida no es JSON: %v\n%s", err, buf.String())
	}
	for _, k := range []string{"ok", "code", "data", "next"} {
		if _, ok := got[k]; !ok {
			t.Errorf("falta la clave %q en %s", k, buf.String())
		}
	}
	next := got["next"].(map[string]any)
	if next["action"] != "ask" || next["gate"] != "spec" {
		t.Errorf("next inesperado: %v", next)
	}
}

func TestJSONAlwaysHasDataAndNext(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Fail("boom", errors.New("detalle")), true); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != false || got["code"] != "boom" {
		t.Errorf("envelope de error inesperado: %v", got)
	}
	if got["data"] == nil {
		t.Error("data debe ser un objeto, no null")
	}
	if n := got["next"].(map[string]any); n["action"] != "done" {
		t.Errorf("next por defecto debe ser done, got %v", n)
	}
}

func TestTextMode(t *testing.T) {
	var buf bytes.Buffer
	env := Rejected("transition_invalid", "contract → done no es válida")
	env.Text = "RECHAZADO: contract → done no es válida"
	if err := Write(&buf, env, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "RECHAZADO") {
		t.Errorf("modo texto: %q", buf.String())
	}
}
