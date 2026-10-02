package config

import (
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

// R18: el scout va encendido salvo flow.scout: false.
func TestFlowScoutDefaultOnAndOff(t *testing.T) {
	for _, c := range []struct {
		yaml string
		want string
	}{
		{"stack: go\n", flow.ScoutAgent},
		{"stack: go\nflow: { scout: true }\n", flow.ScoutAgent},
		{"stack: go\nflow: { scout: false }\n", ""},
	} {
		cfg, err := Load(fixture(t, "", c.yaml))
		if err != nil {
			t.Fatalf("%q: %v", c.yaml, err)
		}
		if got := cfg.Flow.Core().Scout; got != c.want {
			t.Errorf("%q: Scout=%q, want %q", c.yaml, got, c.want)
		}
	}
}

// R19: agents.scout vale mientras el scout está activo; apagado, avisa.
func TestAgentsScoutNoWarning(t *testing.T) {
	const agents = "agents:\n  scout:\n    model: sonnet\n"
	if _, err := Load(fixture(t, "", "stack: go\n"+agents)); err != nil {
		t.Errorf("con el scout activo: %v", err)
	}
	_, err := Load(fixture(t, "", "stack: go\nflow: { scout: false }\n"+agents))
	if err == nil || !strings.Contains(err.Error(), "agents.scout no trabaja en ninguna fase") {
		t.Errorf("con el scout apagado: %v", err)
	}
}

// R20: el scout es reservado; no se pone en flow.agents.
func TestFlowAgentsScoutReserved(t *testing.T) {
	_, err := Load(fixture(t, "", "stack: go\nflow:\n  agents:\n    spec: [scout, spec-author]\n"))
	want := "flow.agents: scout es un agente reservado; se apaga con flow.scout: false"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error: %v", err)
	}
}
