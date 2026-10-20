package engine

import (
	"context"
	"testing"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

func TestEnginePauseGuards(t *testing.T) {
	ctx := context.Background()
	v := newEnv(t, "")
	id := v.task(t, "Demo")
	if _, err := v.e.Pause(ctx, id); rejectionOf(t, err).Code != "not_started" {
		t.Errorf("sin empezar: %v", err)
	}
	if _, err := v.e.Store.Update(id, func(r *store.Record, _ bool) error {
		r.Flow = flow.State{ID: id, Lane: flow.Full, Phase: flow.Implementing, BranchCreated: true, Started: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // repetirlo no es error
		if _, err := v.e.Pause(ctx, id); err != nil {
			t.Fatalf("pausa %d: %v", i, err)
		}
	}
	rec, _ := v.e.Store.Load(id)
	if !rec.Flow.PauseRequested {
		t.Error("la marca debe quedar guardada")
	}
	o := mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV}))
	if o.To != flow.Paused || o.Gate != string(flow.GatePause) {
		t.Fatalf("DONE con marca: %+v", o)
	}
	if _, err := v.e.Pause(ctx, id); rejectionOf(t, err).Code != "pause_late" {
		t.Errorf("en paused: %v", err)
	}
}
