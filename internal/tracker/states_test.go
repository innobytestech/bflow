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
		"Cancelled":        flow.Dropped,
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

// R6, R8: dropped tiene estado propio (grupo cancelled) y entra en PhaseOrder.
func TestStatesDropped(t *testing.T) {
	d, ok := DefaultStates[flow.Dropped]
	if !ok {
		t.Fatal("DefaultStates no tiene dropped")
	}
	if d.Group != "cancelled" || len(d.Names) != 4 || d.Names[0] != "Cancelled" || d.Names[1] != "Cancelado" || d.Names[2] != "Descartado" || d.Names[3] != "Canceled" {
		t.Errorf("dropped: %+v", d)
	}
	var in []flow.Phase
	for _, p := range PhaseOrder {
		if p == flow.Dropped {
			in = append(in, p)
		}
	}
	if len(in) != 1 || PhaseOrder[len(PhaseOrder)-1] != flow.Dropped {
		t.Errorf("PhaseOrder debe terminar en dropped, una vez: %v", PhaseOrder)
	}
	for _, p := range flow.Order {
		if p == flow.Dropped {
			t.Error("dropped no entra en flow.Order")
		}
	}
	tab := NewStateTable(nil)
	if got := tab.PhaseOf("Cancelado"); got != flow.Dropped {
		t.Errorf("PhaseOf(Cancelado) = %q", got)
	}
	if n, ok := tab.Write([]string{"Backlog", "Cancelled"}, flow.Dropped); !ok || n != "Cancelled" {
		t.Errorf("Write(dropped): %q %v", n, ok)
	}
}
