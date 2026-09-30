package tracker

import (
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

func TestStateTablePhaseOf(t *testing.T) {
	tab := NewStateTable(nil)
	cases := map[string]flow.Phase{
		"Todo":             flow.Backlog,
		"Backlog":          flow.Backlog,
		"In Progress":      flow.Implementing, // aparece antes en su lista que en la de contract
		"in progress":      flow.Implementing,
		"Done":             flow.Done,
		"discovery":        flow.Discovery,
		"Bloqueado":        flow.Blocked,
		"Spec por aprobar": flow.Spec,
		"Cancelled":        "",
	}
	for name, want := range cases {
		if got := tab.PhaseOf(name); got != want {
			t.Errorf("PhaseOf(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestStateTableOverrides(t *testing.T) {
	tab := NewStateTable(map[string][]string{"quality": {"QA"}})
	if got := tab.PhaseOf("QA"); got != flow.Quality {
		t.Errorf("PhaseOf(QA) = %q, want quality", got)
	}
	if got := tab.PhaseOf("En revisión"); got != "" {
		t.Errorf("el override reemplaza los nombres por defecto: En revisión = %q", got)
	}
	if n, ok := tab.Write([]string{"Backlog", "qa"}, flow.Quality); !ok || n != "qa" {
		t.Errorf("Write devuelve el nombre existente: %q %v", n, ok)
	}
	if _, ok := tab.Write([]string{"Backlog"}, flow.Quality); ok {
		t.Error("sin estado existente no hay dónde escribir")
	}
	if n, _ := tab.Write([]string{"Implementado", "In Progress"}, flow.Contract); n != "In Progress" {
		t.Errorf("primer nombre existente en orden de preferencia: %q", n)
	}
	if got := NewStateTable(nil)[flow.Quality].Names[0]; got != "En revisión" {
		t.Errorf("los overrides no tocan DefaultStates: %q", got)
	}
}
