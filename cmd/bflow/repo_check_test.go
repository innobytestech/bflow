package main

import (
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/config"
)

// El repo exige que los agentes renderizados coincidan con las plantillas (GH-128).
func TestRepoCheckRunsRender(t *testing.T) {
	cfg, err := config.Load(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range cfg.Check.Steps {
		if strings.Contains(s.Run, "cmd/bflow render --check") {
			return
		}
	}
	t.Fatal("bflow.yaml no tiene un paso de check.steps con 'cmd/bflow render --check'")
}
