package flow

import "testing"

func TestPauseWhileBlocked(t *testing.T) {
	cfg := testConfig()
	for _, p := range []Phase{Discovery, Spec, Contract, Implementing} {
		s := apply(t, cfg, stateFor(row{lane: Full, from: p}), Event{Kind: EvBlock, Note: "espera"})
		s = apply(t, cfg, s, Event{Kind: EvPause})
		if s.Phase != Blocked || !s.PauseRequested {
			t.Errorf("%s: bloqueada debe aceptar pausa: %s marca=%v", p, s.Phase, s.PauseRequested)
		}
	}
	s := apply(t, cfg, stateFor(row{lane: Full, from: Quality}), Event{Kind: EvBlock, Note: "espera"})
	if got := rejectionCode(t, cfg, s, Event{Kind: EvPause}); got != "pause_late" {
		t.Errorf("bloqueada en quality: %q", got)
	}
}
