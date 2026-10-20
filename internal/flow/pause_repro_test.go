package flow

import "testing"

func TestPauseHonoredByNoReproApproval(t *testing.T) {
	cfg := testConfig()
	s := stateFor(row{lane: Hotfix, from: Implementing})
	s = apply(t, cfg, s, Event{Kind: EvPause})
	s = apply(t, cfg, s, hotfixDone("solo texto"))
	s = apply(t, cfg, s, approve())
	if s.Phase != Paused || s.PauseRequested {
		t.Errorf("aprobar repro con marca: %s marca=%v, want paused sin marca", s.Phase, s.PauseRequested)
	}
}
