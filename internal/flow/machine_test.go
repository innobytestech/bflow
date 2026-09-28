package flow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

const (
	cs = FxCreateBranch
	pr = FxOpenPR
	ts = FxTrackerState
	ss = FxStampStart
	cm = FxComment
)

type row struct {
	lane    Lane
	from    Phase
	gate    Gate
	reports map[string]Verdict
	round   int
	ev      Event

	want      Phase
	wantGate  Gate
	wantRound int
	fx        []EffectKind
	err       string // código de Rejection esperado; si no vacío, no se evalúa lo demás
}

func (r row) name() string {
	g := ""
	if r.gate != "" {
		g = "[" + string(r.gate) + "]"
	}
	ev := string(r.ev.Kind)
	if r.ev.Verdict != "" {
		ev += ":" + r.ev.Agent + "=" + string(r.ev.Verdict)
	}
	if r.ev.To != "" {
		ev += "→" + string(r.ev.To)
	}
	return fmt.Sprintf("%s/%s%s/%s", r.lane, r.from, g, ev)
}

// Eventos abreviados.
func start(l Lane) Event           { return Event{Kind: EvStart, Lane: l} }
func approve() Event               { return Event{Kind: EvApprove} }
func approveChoice(n int) Event    { return Event{Kind: EvApprove, Choice: n} }
func approveWith(att string) Event { return Event{Kind: EvApprove, Attachment: att} }
func rej(note string) Event        { return Event{Kind: EvReject, Note: note} }
func rejTo(to Phase) Event         { return Event{Kind: EvReject, To: to, Note: "motivo"} }
func rep(agent string, v Verdict) Event {
	e := Event{Kind: EvReport, Agent: agent, Verdict: v, Note: "detalle"}
	if v == NeedsDecision {
		e.Options = []string{"opción A", "opción B"}
	}
	if v == DoneV {
		e.CheckOK = true
	}
	return e
}
func merged() Event { return Event{Kind: EvMerged} }

var (
	sa  = "spec-author"
	imp = "implementer"
	rv  = "reviewer"
	sec = "security-auditor"
	doc = "documenter"
	ok  = map[string]Verdict{rv: Approved}
	ko  = map[string]Verdict{rv: RejectedV}
)

// commonRows son las filas que valen igual para cualquier carril que tenga la fase.
func commonRows(l Lane) []row {
	rs := []row{
		// spec
		{lane: l, from: Spec, ev: rep(sa, Ready), want: Spec, wantGate: GateSpec},
		{lane: l, from: Spec, ev: rep(sa, Split), want: Spec, wantGate: GateSplit},
		{lane: l, from: Spec, ev: rep(sa, NeedsDecision), want: Spec, wantGate: GateDecision},
		{lane: l, from: Spec, gate: GateDecision, ev: approveChoice(1), want: Spec},
		{lane: l, from: Spec, gate: GateSplit, ev: approve(), want: Blocked, fx: []EffectKind{ts, cm}},
		{lane: l, from: Spec, gate: GateSplit, ev: rej("hazla completa"), want: Spec, fx: []EffectKind{cm}},
		{lane: l, from: Spec, gate: GateSpec, ev: rej("falta X"), want: Spec, fx: []EffectKind{cm}},
		// implementing
		{lane: l, from: Implementing, ev: rep(imp, NeedsDecision), want: Implementing, wantGate: GateDecision},
		{lane: l, from: Implementing, ev: rep(imp, BlockedV), want: Blocked, fx: []EffectKind{ts, cm}},
		{lane: l, from: Implementing, gate: GateDecision, ev: approveChoice(2), want: Implementing},
		// quality: primer reporte, el otro sigue pendiente
		{lane: l, from: Quality, ev: rep(rv, Approved), want: Quality},
		{lane: l, from: Quality, ev: rep(rv, RejectedV), want: Quality},
		// quality: último reporte
		{lane: l, from: Quality, reports: ok, ev: rep(sec, Approved), want: Documenting, fx: []EffectKind{ts}},
		{lane: l, from: Quality, reports: ko, ev: rep(sec, Approved), want: Implementing, wantRound: 1, fx: []EffectKind{ts, cm}},
		{lane: l, from: Quality, reports: ok, ev: rep(sec, RejectedV), want: Implementing, wantRound: 1, fx: []EffectKind{ts, cm}},
		{lane: l, from: Quality, reports: ko, round: 1, ev: rep(sec, Approved), want: Implementing, wantGate: GateRounds, wantRound: 2, fx: []EffectKind{ts, cm}},
		{lane: l, from: Quality, reports: ok, round: 1, ev: rep(sec, RejectedV), want: Implementing, wantGate: GateRounds, wantRound: 2, fx: []EffectKind{ts, cm}},
		// cortacircuito
		{lane: l, from: Implementing, gate: GateRounds, round: 2, ev: approve(), want: Implementing, wantRound: 2},
		// documenting
		{lane: l, from: Documenting, ev: rep(doc, DoneV), want: Walkthrough, wantGate: GateWalkthrough, fx: []EffectKind{ts}},
		{lane: l, from: Documenting, ev: rep(doc, BlockedV), want: Blocked, fx: []EffectKind{ts, cm}},
		// walkthrough
		{lane: l, from: Walkthrough, gate: GateWalkthrough, ev: approve(), want: InReview, fx: []EffectKind{pr, ts}},
		{lane: l, from: Walkthrough, gate: GateWalkthrough, ev: rej("no me convence"), want: Implementing, fx: []EffectKind{ts, cm}},
		// in_review
		{lane: l, from: InReview, ev: merged(), want: Done, fx: []EffectKind{ts}},
	}
	var out []row
	for _, r := range rs {
		if DefaultConfig().has(l, r.from) {
			out = append(out, r)
		}
	}
	return out
}

func table() []row {
	rows := []row{
		// ---------- full ----------
		{lane: Full, from: Backlog, ev: start(Full), want: Discovery, wantGate: GateDiscovery, fx: []EffectKind{ts}},
		{lane: Full, from: Discovery, gate: GateDiscovery, ev: approveWith("discovery completo"), want: Spec, fx: []EffectKind{ts, cm}},
		{lane: Full, from: Discovery, gate: GateDiscovery, ev: rej("sigue"), want: Discovery, wantGate: GateDiscovery, fx: []EffectKind{cm}},
		{lane: Full, from: Spec, gate: GateSpec, ev: approve(), want: Contract, fx: []EffectKind{cs, ts}},
		{lane: Full, from: Contract, ev: rep(imp, ContractReady), want: Contract, wantGate: GateContract},
		{lane: Full, from: Contract, ev: rep(imp, NeedsDecision), want: Contract, wantGate: GateDecision},
		{lane: Full, from: Contract, ev: rep(imp, BlockedV), want: Blocked, fx: []EffectKind{ts, cm}},
		{lane: Full, from: Contract, gate: GateDecision, ev: approveChoice(1), want: Contract},
		{lane: Full, from: Contract, gate: GateContract, ev: approve(), want: Implementing, fx: []EffectKind{ts, ss}},
		{lane: Full, from: Contract, gate: GateContract, ev: rej("ajusta firmas"), want: Contract, fx: []EffectKind{cm}},
		{lane: Full, from: Contract, gate: GateContract, ev: rejTo(Spec), want: Spec, fx: []EffectKind{ts, cm}},
		{lane: Full, from: Implementing, ev: rep(imp, DoneV), want: Paused, wantGate: GatePause, fx: []EffectKind{ts}},
		{lane: Full, from: Paused, gate: GatePause, ev: approve(), want: Quality, fx: []EffectKind{ts}},
		{lane: Full, from: Paused, gate: GatePause, ev: rej("probé y falla X"), want: Implementing, fx: []EffectKind{ts, cm}},
		{lane: Full, from: Implementing, gate: GateRounds, round: 2, ev: rej("volver"), want: Spec, wantRound: 0, fx: []EffectKind{ts, cm}},
		{lane: Full, from: Walkthrough, gate: GateWalkthrough, ev: rejTo(Spec), want: Spec, fx: []EffectKind{ts, cm}},

		// ---------- light ----------
		{lane: Light, from: Backlog, ev: start(Light), want: Spec, fx: []EffectKind{ts}},
		{lane: Light, from: Spec, gate: GateSpec, ev: approve(), want: Implementing, fx: []EffectKind{cs, ts, ss}},
		{lane: Light, from: Implementing, ev: rep(imp, DoneV), want: Quality, fx: []EffectKind{ts}},
		{lane: Light, from: Implementing, gate: GateRounds, round: 2, ev: rej("volver"), want: Spec, wantRound: 0, fx: []EffectKind{ts, cm}},
		{lane: Light, from: Walkthrough, gate: GateWalkthrough, ev: rejTo(Spec), want: Spec, fx: []EffectKind{ts, cm}},

		// ---------- hotfix ----------
		{lane: Hotfix, from: Backlog, ev: start(Hotfix), want: Implementing, fx: []EffectKind{cs, ts, ss}},
		{lane: Hotfix, from: Backlog, ev: Event{Kind: EvStart, Lane: Hotfix, Fixes: "T-0"}, want: Implementing, fx: []EffectKind{cs, ts, ss}},
		{lane: Hotfix, from: Implementing, ev: rep(imp, DoneV), want: Quality, fx: []EffectKind{ts}},

		// ---------- errores ----------
		{lane: Full, from: Spec, gate: GateSpec, ev: Event{Kind: EvApprove, Gate: GateWalkthrough}, err: "gate_mismatch"},
		{lane: Full, from: Spec, ev: approve(), err: "no_gate"},
		{lane: Full, from: Spec, gate: GateSpec, ev: rep(sa, Ready), err: "gate_pending"},
		{lane: Full, from: Implementing, ev: rep(rv, Approved), err: "unexpected_agent"},
		{lane: Full, from: Implementing, ev: rep(imp, Ready), err: "unexpected_verdict"},
		{lane: Full, from: Implementing, ev: Event{Kind: EvReport, Agent: imp, Verdict: DoneV}, err: "check_required"},
		{lane: Full, from: Contract, ev: rep(imp, DoneV), err: "unexpected_verdict"},
		{lane: Full, from: Quality, reports: ok, ev: rep(rv, Approved), err: "already_reported"},
		{lane: Hotfix, from: Implementing, gate: GateRounds, round: 2, ev: rej("volver"), err: "invalid_target"},
		{lane: Hotfix, from: Walkthrough, gate: GateWalkthrough, ev: rejTo(Spec), err: "invalid_target"},
		{lane: Full, from: Walkthrough, gate: GateWalkthrough, ev: rejTo(Done), err: "invalid_target"},
		{lane: Full, from: Implementing, gate: GateDecision, ev: approveChoice(3), err: "bad_choice"},
		{lane: Full, from: Implementing, gate: GateDecision, ev: rej("no"), err: "not_rejectable"},
		{lane: Full, from: Implementing, ev: merged(), err: "not_in_review"},
		{lane: Full, from: Spec, ev: start(Light), err: "already_started"},
		{lane: Full, from: Backlog, ev: start("turbo"), err: "unknown_lane"},
		{lane: Light, from: Backlog, ev: Event{Kind: EvStart, Lane: Light, Fixes: "T-0"}, err: "fixes_needs_hotfix"},
		{lane: Hotfix, from: Backlog, ev: Event{Kind: EvStart, Lane: Hotfix, Fixes: "T-1"}, err: "fixes_self"},
		{lane: Full, from: Done, ev: Event{Kind: EvBlock, Note: "x"}, err: "task_done"},
		{lane: Full, from: Spec, ev: Event{Kind: EvUnblock}, err: "not_blocked"},
		{lane: Full, from: Spec, gate: GateSpec, ev: Event{Kind: EvReject}, err: "note_required"},
	}
	for _, l := range []Lane{Full, Light, Hotfix} {
		rows = append(rows, commonRows(l)...)
	}
	return rows
}

// stateFor construye el estado inicial de una fila.
func stateFor(r row) State {
	s := New("T-1", "demo")
	if r.from != Backlog {
		s.Lane = r.lane
	}
	s.Phase = r.from
	s.Round = r.round
	s.BranchCreated = !slices.Contains([]Phase{Backlog, Discovery, Spec}, r.from)
	s.Started = s.BranchCreated && r.from != Contract
	if r.reports != nil {
		s.Reports = map[string]Verdict{}
		for k, v := range r.reports {
			s.Reports[k] = v
		}
	}
	if r.gate != "" {
		g := &PendingGate{Name: r.gate}
		if r.gate == GateDecision {
			g.Agent = imp
			if r.from == Spec {
				g.Agent = sa
			}
			g.Options = []string{"opción A", "opción B"}
		}
		if r.gate == GateSplit {
			g.Agent = sa
		}
		s.Gate = g
	}
	if r.from == Blocked {
		s.Block = &BlockInfo{Reason: "x", From: Implementing}
	}
	return s
}

func TestTransitionTable(t *testing.T) {
	cfg := DefaultConfig()
	for _, r := range table() {
		t.Run(r.name(), func(t *testing.T) {
			res, err := Apply(cfg, stateFor(r), r.ev)
			if r.err != "" {
				var rj *Rejection
				if !errors.As(err, &rj) {
					t.Fatalf("esperaba Rejection %q, got err=%v", r.err, err)
				}
				if rj.Code != r.err {
					t.Fatalf("código %q, want %q (%s)", rj.Code, r.err, rj.Reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("error inesperado: %v", err)
			}
			s := res.State
			if s.Phase != r.want {
				t.Errorf("fase %s, want %s", s.Phase, r.want)
			}
			if got := gateName(s); got != r.wantGate {
				t.Errorf("gate %q, want %q", got, r.wantGate)
			}
			if s.Round != r.wantRound {
				t.Errorf("ronda %d, want %d", s.Round, r.wantRound)
			}
			if got := kinds(res.Effects); !slices.Equal(got, r.fx) {
				t.Errorf("efectos %v, want %v", got, r.fx)
			}
			for _, fx := range res.Effects {
				if fx.Kind == FxTrackerState && fx.Phase != s.Phase {
					t.Errorf("tracker_state a %s, pero la fase es %s", fx.Phase, s.Phase)
				}
				if fx.Kind == FxComment && strings.TrimSpace(fx.Body) == "" {
					t.Error("comentario vacío")
				}
			}
			if res.Next.Action == "" {
				t.Error("Next sin acción")
			}
		})
	}
}

func gateName(s State) Gate {
	if s.Gate == nil {
		return ""
	}
	return s.Gate.Name
}

func kinds(fx []Effect) []EffectKind {
	var out []EffectKind
	for _, f := range fx {
		out = append(out, f.Kind)
	}
	return out
}

// ---------- cobertura exhaustiva ----------

type transitionKey string

func keyOf(lane Lane, from State, ev Event, to State) transitionKey {
	return transitionKey(fmt.Sprintf("%s|%s[%s]|%s:%s|→%s[%s]", lane, from.Phase, gateName(from), ev.Kind, ev.Verdict, to.Phase, gateName(to)))
}

// candidates genera todos los eventos que podrían ocurrir en un estado.
func candidates(cfg Config, s State) []Event {
	var evs []Event
	if s.Phase == Backlog {
		for _, l := range []Lane{Full, Light, Hotfix} {
			evs = append(evs, start(l))
		}
		return evs
	}
	evs = append(evs, approve(), approveChoice(1), approveWith("adjunto"), rej("nota"), merged())
	for _, p := range Order {
		evs = append(evs, rejTo(p))
	}
	agents := map[string]bool{}
	for _, as := range cfg.Agents {
		for _, a := range as {
			agents[a] = true
		}
	}
	for a := range agents {
		for _, v := range []Verdict{Ready, Split, NeedsDecision, ContractReady, DoneV, BlockedV, Approved, RejectedV} {
			evs = append(evs, rep(a, v))
		}
	}
	return evs
}

func canon(s State) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestTableCoversEveryReachableTransition recorre todos los estados alcanzables
// desde backlog y exige que cada transición válida tenga una fila en la tabla.
// Si alguien agrega una transición al núcleo sin probarla, esta prueba falla.
func TestTableCoversEveryReachableTransition(t *testing.T) {
	cfg := DefaultConfig()
	covered := map[transitionKey]bool{}
	for _, r := range table() {
		if r.err != "" {
			continue
		}
		res, err := Apply(cfg, stateFor(r), r.ev)
		if err != nil {
			continue // ya lo reporta TestTransitionTable
		}
		covered[keyOf(r.lane, stateFor(r), r.ev, res.State)] = true
	}

	found := map[transitionKey]bool{}
	seen := map[string]bool{}
	queue := []State{New("T-1", "demo")}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if seen[canon(s)] || s.Round > cfg.MaxQualityRounds+1 {
			continue
		}
		seen[canon(s)] = true
		for _, ev := range candidates(cfg, s) {
			res, err := Apply(cfg, s, ev)
			if err != nil {
				var rj *Rejection
				if !errors.As(err, &rj) {
					t.Fatalf("error que no es Rejection en %s con %+v: %v", canon(s), ev, err)
				}
				continue
			}
			lane := res.State.Lane
			found[keyOf(lane, s, ev, res.State)] = true
			queue = append(queue, res.State)
		}
	}
	var missing []string
	for k := range found {
		if !covered[k] {
			missing = append(missing, string(k))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d transiciones alcanzables sin fila en la tabla:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	t.Logf("estados visitados: %d · transiciones distintas: %d · cubiertas por la tabla: %d", len(seen), len(found), len(covered))
	if len(seen) < 30 {
		t.Errorf("el recorrido solo visitó %d estados; algo no avanza", len(seen))
	}
}

// ---------- propiedades ----------

func TestBlockAndUnblockReturnToSamePhase(t *testing.T) {
	cfg := DefaultConfig()
	for _, r := range table() {
		if r.err != "" || r.from == Backlog {
			continue
		}
		s := stateFor(r)
		b, err := Apply(cfg, s, Event{Kind: EvBlock, Note: "falta acceso a la BD"})
		if err != nil {
			t.Fatalf("%s: block: %v", r.name(), err)
		}
		if b.State.Phase != Blocked || b.State.Block.From != s.Phase {
			t.Fatalf("%s: block dejó %+v", r.name(), b.State)
		}
		if got := kinds(b.Effects); !slices.Equal(got, []EffectKind{ts, cm}) {
			t.Errorf("%s: efectos de block %v", r.name(), got)
		}
		u, err := Apply(cfg, b.State, Event{Kind: EvUnblock})
		if err != nil {
			t.Fatalf("%s: unblock: %v", r.name(), err)
		}
		if u.State.Phase != s.Phase || gateName(u.State) != gateName(s) || u.State.Block != nil {
			t.Errorf("%s: unblock volvió a %s[%s], want %s[%s]", r.name(), u.State.Phase, gateName(u.State), s.Phase, gateName(s))
		}
		if _, err := Apply(cfg, b.State, Event{Kind: EvBlock, Note: "otra vez"}); err == nil {
			t.Errorf("%s: bloquear una tarea bloqueada debe fallar", r.name())
		}
	}
}

func TestPhaseAlwaysInLane(t *testing.T) {
	cfg := DefaultConfig()
	for _, r := range table() {
		if r.err != "" {
			continue
		}
		res, err := Apply(cfg, stateFor(r), r.ev)
		if err != nil {
			continue
		}
		p := res.State.Phase
		if p != Blocked && !cfg.has(res.State.Lane, p) {
			t.Errorf("%s: terminó en %s, que no está en el carril %s", r.name(), p, res.State.Lane)
		}
	}
}

func TestBranchCreatedOnce(t *testing.T) {
	cfg := DefaultConfig()
	s := New("T-1", "demo")
	steps := []Event{start(Full), approveWith("d"), rep(sa, Ready), approve(), rep(imp, ContractReady), rejTo(Spec), rep(sa, Ready), approve()}
	branches := 0
	for _, ev := range steps {
		res, err := Apply(cfg, s, ev)
		if err != nil {
			t.Fatalf("%+v: %v", ev, err)
		}
		for _, fx := range res.Effects {
			if fx.Kind == FxCreateBranch {
				branches++
			}
		}
		s = res.State
	}
	if branches != 1 {
		t.Errorf("create_branch se emitió %d veces, want 1", branches)
	}
}

func TestHotfixBranchPrefix(t *testing.T) {
	res, err := Apply(DefaultConfig(), New("T-9", "arregla"), start(Hotfix))
	if err != nil {
		t.Fatal(err)
	}
	if res.Effects[0].Kind != FxCreateBranch || !res.Effects[0].Hotfix {
		t.Errorf("hotfix debe crear rama de hotfix: %+v", res.Effects)
	}
}

func TestStartStampedOnce(t *testing.T) {
	cfg := DefaultConfig()
	s := stateFor(row{lane: Full, from: Walkthrough, gate: GateWalkthrough})
	res, err := Apply(cfg, s, rej("vuelve"))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(kinds(res.Effects), FxStampStart) {
		t.Error("no se debe volver a sellar el inicio al regresar a implementing")
	}
}

func TestDecisionCarriesChoiceAndResume(t *testing.T) {
	s := stateFor(row{lane: Full, from: Implementing, gate: GateDecision})
	res, err := Apply(DefaultConfig(), s, approveChoice(2))
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Decision != "opción B" || !res.State.Resume {
		t.Errorf("decision=%q resume=%v", res.State.Decision, res.State.Resume)
	}
	free, err := Apply(DefaultConfig(), s, Event{Kind: EvApprove, Note: "haz C"})
	if err != nil {
		t.Fatal(err)
	}
	if free.State.Decision != "haz C" {
		t.Errorf("decisión libre: %q", free.State.Decision)
	}
}

func TestQualityReportsResetOnPhaseChange(t *testing.T) {
	s := stateFor(row{lane: Full, from: Quality, reports: ko})
	res, err := Apply(DefaultConfig(), s, rep(sec, Approved))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.State.Reports) != 0 {
		t.Errorf("reportes deben limpiarse al salir de quality: %v", res.State.Reports)
	}
	if !strings.Contains(res.State.Note, rv) {
		t.Errorf("la nota para el implementer debe decir quién rechazó: %q", res.State.Note)
	}
}

func TestConfigValidation(t *testing.T) {
	bad := []func(c *Config){
		func(c *Config) { c.Lanes[Hotfix] = []Phase{Implementing, Documenting, Walkthrough, InReview, Done} },  // sin quality
		func(c *Config) { c.Lanes[Light] = []Phase{Implementing, Spec, Quality, Walkthrough, InReview, Done} }, // desordenado
		func(c *Config) {
			c.Lanes[Light] = []Phase{Contract, Implementing, Quality, Walkthrough, InReview, Done}
		}, // contract sin spec
		func(c *Config) { c.MaxQualityRounds = 0 },
		func(c *Config) { c.Agents[Quality] = nil },
	}
	for i, mut := range bad {
		c := DefaultConfig()
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("caso %d: esperaba error de validación", i)
		}
	}
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("la config por defecto debe ser válida: %v", err)
	}
}

func TestPausedConfigurablePerLane(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Lanes[Light] = []Phase{Spec, Implementing, Paused, Quality, Documenting, Walkthrough, InReview, Done}
	s := stateFor(row{lane: Light, from: Implementing})
	res, err := Apply(cfg, s, rep(imp, DoneV))
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Paused || gateName(res.State) != GatePause {
		t.Errorf("con paused en light: %s[%s]", res.State.Phase, gateName(res.State))
	}
}

// Una nota de rechazo es para el agente de esa fase: no debe llegar al de la
// fase siguiente.
func TestNoteDoesNotLeakToNextPhase(t *testing.T) {
	cfg := DefaultConfig()
	s := stateFor(row{lane: Full, from: Spec, gate: GateSpec})
	res, err := Apply(cfg, s, rej("falta el caso de concurrencia"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Next.Agents[0].Args["note"] != "falta el caso de concurrencia" {
		t.Fatalf("la nota debe llegar al spec-author: %v", res.Next.Agents[0].Args)
	}
	res, _ = Apply(cfg, res.State, rep(sa, Ready))
	res, err = Apply(cfg, res.State, approve())
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := res.Next.Agents[0].Args["note"]; ok {
		t.Errorf("la nota del spec llegó al implementer: %q", n)
	}
}
