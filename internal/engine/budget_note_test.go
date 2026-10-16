package engine

import (
	"context"
	"errors"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

// R9: rechazar la pregunta de presupuesto sin --note se rechaza con
// note_required y no bloquea la tarea (GH-16, ajuste tras revisión).
func TestRejectBudgetEmptyNote(t *testing.T) {
	v := newEnv(t, budgetYAML)
	ctx := context.Background()
	id := startImplementing(t, v)
	addTokens(t, v, id, flow.Implementing, 412_000)

	_, err := v.e.Reject(ctx, id, RejectOpts{Gate: "budget", Note: "   "})
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "note_required" {
		t.Fatalf("rechazo de presupuesto sin nota debía dar note_required: %v", err)
	}

	view, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Phase == flow.Blocked {
		t.Errorf("sin nota no debía bloquear la tarea: %+v", view)
	}
}
