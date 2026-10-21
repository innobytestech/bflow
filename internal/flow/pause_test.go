package flow

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func apply(t *testing.T, cfg Config, s State, ev Event) State {
	t.Helper()
	res, err := Apply(cfg, s, ev)
	if err != nil {
		t.Fatalf("%s: %v", ev.Kind, err)
	}
	return res.State
}

func rejectionCode(t *testing.T, cfg Config, s State, ev Event) string {
	t.Helper()
	_, err := Apply(cfg, s, ev)
	var rj *Rejection
	if !errors.As(err, &rj) {
		t.Fatalf("esperaba Rejection, got %v", err)
	}
	return rj.Code
}

func implementing(lane Lane) State { return stateFor(row{lane: lane, from: Implementing}) }

func TestFullLaneNoPause(t *testing.T) {
	want := []Phase{Discovery, Spec, Contract, Implementing, Quality, Documenting, Walkthrough, InReview, Done}
	if got := DefaultLanes()[Full]; !slices.Equal(got, want) {
		t.Errorf("carril full por defecto: %v, want %v", got, want)
	}
}

func TestImplementingDoneGoesToQuality(t *testing.T) {
	s := apply(t, testConfig(), implementing(Full), rep(imp, DoneV))
	if s.Phase != Quality || s.Gate != nil {
		t.Errorf("DONE en full sin pausa: %s gate=%v", s.Phase, s.Gate)
	}
}

func TestContractOKAdvancesWithoutGate(t *testing.T) {
	ev := rep(imp, ContractReady)
	ev.ContractOK = true
	s := apply(t, testConfig(), stateFor(row{lane: Full, from: Contract}), ev)
	if s.Phase != Implementing || s.Gate != nil || !s.Resume {
		t.Errorf("contrato validado: %s gate=%v resume=%v", s.Phase, s.Gate, s.Resume)
	}
	s = apply(t, testConfig(), stateFor(row{lane: Full, from: Contract}), rep(imp, ContractReady))
	if s.Phase != Contract || gateName(s) != GateContract {
		t.Errorf("sin validar abre la gate: %s[%s]", s.Phase, gateName(s))
	}
}

func TestPauseRequestStopsAtGate(t *testing.T) {
	cfg := testConfig()
	s := apply(t, cfg, implementing(Full), Event{Kind: EvPause})
	if !s.PauseRequested || s.Phase != Implementing || s.Gate != nil {
		t.Fatalf("pedir pausa no cambia fase ni gate: %+v", s)
	}
	s = apply(t, cfg, s, Event{Kind: EvPause}) // repetir no es error
	if !s.PauseRequested {
		t.Fatal("la marca debe seguir")
	}
	s = apply(t, cfg, s, rep(imp, DoneV))
	if s.Phase != Paused || gateName(s) != GatePause || s.PauseRequested {
		t.Errorf("DONE con marca: %s[%s] marca=%v", s.Phase, gateName(s), s.PauseRequested)
	}

	// Con la marca puesta antes de implementing también cuenta.
	pre := stateFor(row{lane: Full, from: Contract})
	pre = apply(t, cfg, pre, Event{Kind: EvPause})
	if !pre.PauseRequested {
		t.Error("pausar desde contract debe marcar")
	}
}

func TestPauseLate(t *testing.T) {
	cfg := testConfig()
	for _, p := range []Phase{Quality, Documenting, Walkthrough, InReview, Paused} {
		s := stateFor(row{lane: Full, from: p})
		if got := rejectionCode(t, cfg, s, Event{Kind: EvPause}); got != "pause_late" {
			t.Errorf("%s: %q, want pause_late", p, got)
		}
	}
	// Una tarea retirada ya no se puede pausar.
	d := stateFor(row{lane: Full, from: Implementing})
	d = apply(t, cfg, d, Event{Kind: EvDrop, Note: "ya no"})
	if got := rejectionCode(t, cfg, d, Event{Kind: EvPause}); got != "task_dropped" {
		t.Errorf("retirada: %q", got)
	}
}

// Paused abierta por la marca en un carril que no tiene la fase: al aprobar se
// sigue con lo que va tras implementing.
func TestPauseApproveWithoutPausedPhase(t *testing.T) {
	cfg := testConfig()
	s := apply(t, cfg, apply(t, cfg, implementing(Full), Event{Kind: EvPause}), rep(imp, DoneV))
	if got := cfg.afterPause(Full); got != Quality {
		t.Errorf("afterPause sin paused: %s", got)
	}
	s = apply(t, cfg, s, approve())
	if s.Phase != Quality {
		t.Errorf("aprobar pausa: %s, want quality", s.Phase)
	}
	// Rechazar vuelve a implementing aunque el carril no tenga paused.
	s2 := apply(t, cfg, apply(t, cfg, apply(t, cfg, implementing(Full), Event{Kind: EvPause}), rep(imp, DoneV)), rej("falla X"))
	if s2.Phase != Implementing {
		t.Errorf("rechazar pausa: %s", s2.Phase)
	}
	// Con paused en el carril, afterPause es la siguiente a paused.
	cfg.Lanes[Light] = []Phase{Spec, Implementing, Paused, Documenting, Walkthrough, InReview, Done}
	if got := cfg.afterPause(Light); got != Documenting {
		t.Errorf("afterPause con paused: %s", got)
	}
}

// Las tareas que ya esperan en las gates viejas siguen resolviéndose igual.
func TestLegacyContractAndPauseGates(t *testing.T) {
	cfg := testConfig()
	c := stateFor(row{lane: Full, from: Contract, gate: GateContract})
	if s := apply(t, cfg, c, approve()); s.Phase != Implementing || !s.Resume {
		t.Errorf("aprobar contract: %s resume=%v", s.Phase, s.Resume)
	}
	if s := apply(t, cfg, c, rejTo(Spec)); s.Phase != Spec {
		t.Errorf("rechazar contract a spec: %s", s.Phase)
	}
	p := stateFor(row{lane: Full, from: Paused, gate: GatePause})
	if s := apply(t, cfg, p, approve()); s.Phase != Quality {
		t.Errorf("aprobar pause (carril viejo con paused): %s", s.Phase)
	}
	if s := apply(t, cfg, p, rej("falla")); s.Phase != Implementing {
		t.Errorf("rechazar pause: %s", s.Phase)
	}
	// Una gate pause en vuelo en un carril sin paused sigue al que va tras implementing.
	l := stateFor(row{lane: Light, from: Implementing, gate: GatePause})
	if s := apply(t, cfg, l, approve()); s.Phase != Quality {
		t.Errorf("aprobar pause en carril sin paused: %s", s.Phase)
	}
}

func TestUpcomingPauseRequested(t *testing.T) {
	cfg := DefaultConfig()
	s := implementing(Full)
	if u := Upcoming(cfg, s); u.Phase != Quality || u.Gate != "" {
		t.Errorf("sin marca: %+v", u)
	}
	s.PauseRequested = true
	if u := Upcoming(cfg, s); u.Phase != Paused || u.Gate != GatePause {
		t.Errorf("con marca: %+v", u)
	}
	c := stateFor(row{lane: Full, from: Contract})
	if u := Upcoming(cfg, c); u.Phase != Implementing || u.Gate != "" || !slices.Equal(u.Agents, []string{"implementer"}) {
		t.Errorf("contract sin gate: %+v", u)
	}
	g := stateFor(row{lane: Full, from: Paused, gate: GatePause})
	if u := Upcoming(cfg, g); u.Phase != Quality || !strings.Contains(strings.Join(u.Agents, ","), "reviewer") {
		t.Errorf("tras la pausa: %+v", u)
	}
}

func TestPauseLateBlockedPhase(t *testing.T) {
	cfg := testConfig()
	s := stateFor(row{lane: Full, from: Quality})
	s.Block = &BlockInfo{Reason: "x", From: Quality}
	s.Phase = Blocked
	_, err := Apply(cfg, s, Event{Kind: EvPause})
	var rj *Rejection
	if !errors.As(err, &rj) || rj.Code != "pause_late" {
		t.Fatalf("esperaba pause_late: %v", err)
	}
	if !strings.Contains(rj.Reason, "fase quality") || strings.Contains(rj.Reason, "fase blocked") {
		t.Errorf("debe nombrar la fase real: %q", rj.Reason)
	}
}

func TestUpcomingContractGate(t *testing.T) {
	cfg := testConfig()
	s := stateFor(row{lane: Full, from: Contract})
	if u := Upcoming(cfg, s); u.Phase != Implementing || u.Gate != "" {
		t.Errorf("sin ContractGate: %+v", u)
	}
	cfg.ContractGate = true
	if u := Upcoming(cfg, s); u.Phase != Contract || u.Gate != GateContract {
		t.Errorf("con ContractGate: %+v", u)
	}
}
