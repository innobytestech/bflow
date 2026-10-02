package flow

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const scoutName = "scout"

func scoutConfig() Config {
	c := testConfig()
	c.Scout = ScoutAgent
	return c
}

func scoutDone() Event { return Event{Kind: EvReport, Agent: scoutName, Verdict: DoneV} }

func rejCode(err error) string {
	var rj *Rejection
	if errors.As(err, &rj) {
		return rj.Code
	}
	return ""
}

func started(t *testing.T, cfg Config, lane Lane) Result {
	t.Helper()
	res, err := Apply(cfg, New("T-1", "demo"), start(lane))
	if err != nil {
		t.Fatalf("start %s: %v", lane, err)
	}
	return res
}

// R1: al empezar, en cualquier carril, el scout queda pendiente.
func TestScoutPendingOnStart(t *testing.T) {
	for _, lane := range []Lane{Full, Light, Hotfix} {
		res := started(t, scoutConfig(), lane)
		if res.State.Scout != ScoutPending {
			t.Errorf("%s: scout=%q, want pending", lane, res.State.Scout)
		}
	}
}

// R2: con el scout pendiente, discovery no abre su gate.
func TestScoutDefersDiscoveryGate(t *testing.T) {
	res := started(t, scoutConfig(), Full)
	if res.State.Phase != Discovery || res.State.Gate != nil {
		t.Fatalf("estado: phase=%s gate=%v", res.State.Phase, res.State.Gate)
	}
}

// R3: mientras está pendiente, el siguiente paso es lanzar solo al scout.
func TestScoutNextSpawn(t *testing.T) {
	cfg := scoutConfig()
	res := started(t, cfg, Full)
	n := res.Next
	if n.Action != "spawn" || n.Parallel || len(n.Agents) != 1 {
		t.Fatalf("next: %+v", n)
	}
	a := n.Agents[0]
	if a.Agent != "scout" || a.Subagent != "bflow-scout" {
		t.Errorf("agente: %+v", a)
	}
	want := map[string]string{"id": "T-1", "lane": "full", "phase": "discovery"}
	for k, v := range want {
		if a.Args[k] != v {
			t.Errorf("args[%s]=%q, want %q", k, a.Args[k], v)
		}
	}
	if a.Report != "bflow report T-1 --agent scout --verdict DONE --stdin" {
		t.Errorf("report: %q", a.Report)
	}
	// Una tarea bloqueada no lanza al scout.
	b, err := Apply(cfg, res.State, Event{Kind: EvBlock, Note: "espera"})
	if err != nil || b.Next.Action == "spawn" {
		t.Errorf("bloqueada: %+v %v", b.Next, err)
	}
}

// R4: el scout en discovery abre el gate; la fase y el tracker no cambian.
func TestScoutReportOpensDiscoveryGate(t *testing.T) {
	cfg := scoutConfig()
	res, err := Apply(cfg, started(t, cfg, Full).State, scoutDone())
	if err != nil {
		t.Fatal(err)
	}
	s := res.State
	if s.Scout != ScoutDone || s.Phase != Discovery || s.Gate == nil || s.Gate.Name != GateDiscovery {
		t.Errorf("estado: %+v", s)
	}
	if len(res.Effects) != 0 {
		t.Errorf("efectos: %v", kinds(res.Effects))
	}
	if res.Next.Action != "ask" || res.Next.Gate != "discovery" {
		t.Errorf("next: %+v", res.Next)
	}
}

// R4: en light, tras el scout viene el spec-author, y su reporte cierra la fase
// sin que el scout cuente.
func TestScoutReportLightThenSpecAuthor(t *testing.T) {
	cfg := scoutConfig()
	res, err := Apply(cfg, started(t, cfg, Light).State, scoutDone())
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Spec || res.State.Gate != nil || res.State.Scout != ScoutDone {
		t.Fatalf("estado: %+v", res.State)
	}
	if n := res.Next; n.Action != "spawn" || len(n.Agents) != 1 || n.Agents[0].Agent != sa {
		t.Fatalf("next: %+v", n)
	}
	res, err = Apply(cfg, res.State, rep(sa, Ready))
	if err != nil || res.State.Gate == nil || res.State.Gate.Name != GateSpec {
		t.Errorf("el spec-author solo cierra la spec: %+v %v", res.State.Gate, err)
	}
}

// R4: hotfix no tiene discovery ni spec; el scout corre antes del implementer.
func TestScoutHotfixBeforeImplementer(t *testing.T) {
	cfg := scoutConfig()
	res := started(t, cfg, Hotfix)
	if res.State.Phase != Implementing || res.Next.Agents[0].Agent != "scout" {
		t.Fatalf("start: %s %+v", res.State.Phase, res.Next)
	}
	res, err := Apply(cfg, res.State, scoutDone())
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Implementing || res.State.Gate != nil {
		t.Fatalf("estado: %+v", res.State)
	}
	if n := res.Next; n.Action != "spawn" || n.Agents[0].Agent != imp {
		t.Errorf("next: %+v", n)
	}
}

// R5-R8: rechazos del scout y de los demás agentes.
func TestScoutRejections(t *testing.T) {
	cfg := scoutConfig()
	pending := started(t, cfg, Light).State
	done := pending
	done.Scout = ScoutDone
	none := pending
	none.Scout = ""
	for _, c := range []struct {
		name string
		s    State
		ev   Event
		code string
	}{
		{"ya reportó", done, scoutDone(), "already_reported"},
		{"sin scout", none, scoutDone(), "scout_not_due"},
		{"otro veredicto", pending, rep(scoutName, Ready), "unexpected_verdict"},
		{"otro agente primero", pending, rep(sa, Ready), "scout_pending"},
	} {
		res, err := Apply(cfg, c.s, c.ev)
		if got := rejCode(err); got != c.code {
			t.Errorf("%s: código %q (%v), want %q", c.name, got, err, c.code)
		}
		if err != nil && res.State.Scout != c.s.Scout {
			t.Errorf("%s: el estado cambió", c.name)
		}
	}
	if _, err := Apply(cfg, pending, rep(sa, Ready)); err == nil || !strings.Contains(err.Error(), "scout") {
		t.Errorf("el mensaje debe decir que primero corre el scout: %v", err)
	}
	// En discovery el rechazo es scout_pending, no gate_pending.
	d := started(t, cfg, Full).State
	if _, err := Apply(cfg, d, rep(sa, Ready)); rejCode(err) != "scout_pending" {
		t.Errorf("discovery: %v", err)
	}
}

// R9: el scout no es un agente de fase.
func TestScoutNotInAgentNames(t *testing.T) {
	cfg := scoutConfig()
	if slices.Contains(cfg.AgentNames(), scoutName) || len(cfg.PhasesOf(scoutName)) != 0 {
		t.Errorf("agentes: %v fases: %v", cfg.AgentNames(), cfg.PhasesOf(scoutName))
	}
	res, err := Apply(cfg, started(t, cfg, Light).State, scoutDone())
	if err != nil || res.State.Scout != ScoutDone || len(res.State.Reports) != 0 {
		t.Errorf("el scout no entra en Reports: %+v %v", res.State, err)
	}
}

// R10: volver a la primera fase por un rechazo no vuelve a pedir el scout.
func TestScoutOncePerTask(t *testing.T) {
	cfg := scoutConfig()
	s := stateFor(row{lane: Light, from: Walkthrough, gate: GateWalkthrough})
	s.Scout = ScoutDone
	res, err := Apply(cfg, s, rejTo(Spec))
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Spec || res.State.Scout != ScoutDone {
		t.Fatalf("estado: %+v", res.State)
	}
	if n := res.Next; n.Action != "spawn" || n.Agents[0].Agent != sa {
		t.Errorf("no debe volver a pedir el scout: %+v", n)
	}
	// Y en full, rechazar el discovery mantiene el gate sin scout.
	d, err := Apply(cfg, started(t, cfg, Full).State, scoutDone())
	if err != nil {
		t.Fatal(err)
	}
	res, err = Apply(cfg, d.State, rej("más detalle"))
	if err != nil || res.State.Gate == nil || res.State.Gate.Name != GateDiscovery || res.State.Scout != ScoutDone {
		t.Errorf("rechazo del discovery: %+v %v", res.State, err)
	}
}

// R6, R18: con el scout apagado nada cambia.
func TestScoutOffSkips(t *testing.T) {
	cfg := testConfig()
	res := started(t, cfg, Full)
	if res.State.Scout != "" || res.State.Gate == nil || res.State.Gate.Name != GateDiscovery {
		t.Errorf("estado: %+v", res.State)
	}
	if _, err := Apply(cfg, res.State, scoutDone()); rejCode(err) != "scout_not_due" {
		t.Errorf("scout sin tarea que lo pida: %v", err)
	}
}

// R11: Upcoming dice lo que sigue al scout.
func TestUpcomingWhileScoutPending(t *testing.T) {
	cfg := scoutConfig()
	for _, c := range []struct {
		lane Lane
		want Step
	}{
		{Full, Step{Phase: Discovery, Gate: GateDiscovery}},
		{Light, Step{Phase: Spec, Agents: []string{sa}}},
		{Hotfix, Step{Phase: Implementing, Agents: []string{imp}}},
	} {
		got := Upcoming(cfg, started(t, cfg, c.lane).State)
		if got.Phase != c.want.Phase || got.Gate != c.want.Gate || !slices.Equal(got.Agents, c.want.Agents) {
			t.Errorf("%s: %+v, want %+v", c.lane, got, c.want)
		}
	}
}
