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
		{lane: l, from: Spec, gate: GateSplit, ev: approve(), err: "split_children"},
		{lane: l, from: Spec, gate: GateSplit, ev: Event{Kind: EvApprove, Children: []string{"T-2 · a", "T-3 · b"}}, want: Dropped, fx: []EffectKind{ts, cm}},
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
		// regreso humano sin gate
		{lane: l, from: Quality, ev: rej("falta X"), want: Implementing, fx: []EffectKind{ts, cm}},
		{lane: l, from: Quality, reports: ko, round: 1, ev: rej("falta X"), want: Implementing, wantRound: 1, fx: []EffectKind{ts, cm}},
		{lane: l, from: Documenting, ev: rej("defecto"), want: Implementing, fx: []EffectKind{ts, cm}},
		{lane: l, from: Documenting, ev: rejTo(Implementing), want: Implementing, fx: []EffectKind{ts, cm}},
		// cortacircuito
		{lane: l, from: Implementing, gate: GateRounds, round: 2, ev: approve(), want: Implementing, wantRound: 2},
		// documenting
		{lane: l, from: Documenting, ev: rep(doc, DoneV), want: Walkthrough, wantGate: GateQuestions, fx: []EffectKind{ts}},
		{lane: l, from: Documenting, ev: rep(doc, BlockedV), want: Blocked, fx: []EffectKind{ts, cm}},
		// walkthrough: primero las preguntas de producto, después el recorrido
		{lane: l, from: Walkthrough, gate: GateQuestions, ev: Event{Kind: EvApprove, Note: "que rechace el RFC genérico"}, want: Walkthrough, wantGate: GateWalkthrough},
		{lane: l, from: Walkthrough, gate: GateQuestions, ev: rej("no"), err: "not_rejectable"},
		{lane: l, from: Walkthrough, gate: GateWalkthrough, ev: approve(), want: InReview, fx: []EffectKind{pr, ts}},
		{lane: l, from: Walkthrough, gate: GateWalkthrough, ev: rej("no me convence"), want: Implementing, fx: []EffectKind{ts, cm}},
		// in_review
		{lane: l, from: InReview, ev: merged(), want: Done, fx: []EffectKind{ts}},
	}
	var out []row
	for _, r := range rs {
		if testConfig().has(l, r.from) {
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
		{lane: Full, from: Implementing, ev: rep(imp, DoneV), want: Quality, fx: []EffectKind{ts}},
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
		// decisión: se puede volver a contract o spec si van antes en el carril
		{lane: Full, from: Implementing, gate: GateDecision, ev: rej("contrato vacío"), want: Contract, fx: []EffectKind{ts, cm}},
		{lane: Full, from: Implementing, gate: GateDecision, ev: rejTo(Spec), want: Spec, fx: []EffectKind{ts, cm}},
		{lane: Full, from: Contract, gate: GateDecision, ev: rej("la spec no alcanza"), want: Spec, fx: []EffectKind{ts, cm}},
		{lane: Light, from: Implementing, gate: GateDecision, ev: rej("la spec no alcanza"), want: Spec, fx: []EffectKind{ts, cm}},
		{lane: Light, from: Implementing, gate: GateDecision, ev: rejTo(Contract), err: "invalid_target"},
		{lane: Full, from: Implementing, gate: GateDecision, ev: rejTo(Quality), err: "invalid_target"},
		{lane: Hotfix, from: Implementing, gate: GateDecision, ev: rej("no"), err: "not_rejectable"},
		{lane: Full, from: Spec, gate: GateDecision, ev: rej("no"), err: "not_rejectable"},
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
	cfg := testConfig()
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
	evs = append(evs, approve(), Event{Kind: EvApprove, Children: []string{"T-2 · a"}}, approveChoice(1), approveWith("adjunto"), rej("nota"), merged())
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
	cfg := testConfig()
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
	cfg := testConfig()
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
	cfg := testConfig()
	for _, r := range table() {
		if r.err != "" {
			continue
		}
		res, err := Apply(cfg, stateFor(r), r.ev)
		if err != nil {
			continue
		}
		p := res.State.Phase
		if p != Blocked && p != Dropped && !cfg.has(res.State.Lane, p) {
			t.Errorf("%s: terminó en %s, que no está en el carril %s", r.name(), p, res.State.Lane)
		}
	}
}

func TestBranchCreatedOnce(t *testing.T) {
	cfg := testConfig()
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
	res, err := Apply(testConfig(), New("T-9", "arregla"), start(Hotfix))
	if err != nil {
		t.Fatal(err)
	}
	if res.Effects[0].Kind != FxCreateBranch || !res.Effects[0].Hotfix {
		t.Errorf("hotfix debe crear rama de hotfix: %+v", res.Effects)
	}
}

func TestStartStampedOnce(t *testing.T) {
	cfg := testConfig()
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
	res, err := Apply(testConfig(), s, approveChoice(2))
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Decision != "opción B" || !res.State.Resume {
		t.Errorf("decision=%q resume=%v", res.State.Decision, res.State.Resume)
	}
	free, err := Apply(testConfig(), s, Event{Kind: EvApprove, Note: "haz C"})
	if err != nil {
		t.Fatal(err)
	}
	if free.State.Decision != "haz C" {
		t.Errorf("decisión libre: %q", free.State.Decision)
	}
}

func TestQualityReportsResetOnPhaseChange(t *testing.T) {
	s := stateFor(row{lane: Full, from: Quality, reports: ko})
	res, err := Apply(testConfig(), s, rep(sec, Approved))
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

// TestQualityRejectedWithoutNoteFallsBackToReports cubre el caso del
// review-map "sin prueba": un rechazo de calidad sin --note no debe dejar al
// implementer sin nada que leer, sino apuntarlo a reports/ como respaldo.
func TestQualityRejectedWithoutNoteFallsBackToReports(t *testing.T) {
	s := stateFor(row{lane: Full, from: Quality, reports: ko}) // ko: RejectedV sin RejectNotes
	res, err := Apply(testConfig(), s, rep(sec, Approved))
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Implementing {
		t.Fatalf("fase: %v", res.State.Phase)
	}
	want := TaskDir(s.ID) + "/reports/"
	if !strings.Contains(res.State.Note, want) {
		t.Errorf("sin nota de rechazo, la nota del implementer debe apuntar a %q: %q", want, res.State.Note)
	}
	if !strings.Contains(res.State.Note, rv) {
		t.Errorf("la nota debe seguir diciendo quién rechazó: %q", res.State.Note)
	}
}

// TestRejectHeadClearedOnEnter cubre que RejectHead no se quede con un valor
// viejo: enter() lo limpia en cualquier transición de fase, no solo en el
// rechazo de calidad que lo generó.
func TestRejectHeadClearedOnEnter(t *testing.T) {
	s := stateFor(row{lane: Full, from: Quality, reports: ok})
	s.RejectHead = "sha-vieja-de-una-ronda-anterior"
	res, err := Apply(testConfig(), s, rep(sec, Approved))
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Documenting {
		t.Fatalf("fase: %v", res.State.Phase)
	}
	if res.State.RejectHead != "" {
		t.Errorf("RejectHead debe limpiarse al salir de implementing/quality: %q", res.State.RejectHead)
	}

	// También se limpia cuando un rechazo de walkthrough devuelve la tarea a
	// implementing (enter() corre igual sin pasar por la rama de calidad).
	s2 := stateFor(row{lane: Full, from: Documenting})
	s2.RejectHead = "sha-vieja"
	res2, err := Apply(testConfig(), s2, rej("corrige X"))
	if err != nil {
		t.Fatal(err)
	}
	if res2.State.Phase != Implementing {
		t.Fatalf("fase: %v", res2.State.Phase)
	}
	if res2.State.RejectHead != "" {
		t.Errorf("RejectHead debe limpiarse al reentrar a implementing desde un rechazo fuera de quality: %q", res2.State.RejectHead)
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
		c := testConfig()
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
	cfg := testConfig()
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
	cfg := testConfig()
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

// Las respuestas a las preguntas de producto se guardan para el recorrido.
func TestQuestionsKeepAnswers(t *testing.T) {
	s := stateFor(row{lane: Light, from: Walkthrough, gate: GateQuestions})
	res, err := Apply(testConfig(), s, Event{Kind: EvApprove, Note: " rechaza el RFC genérico "})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Note != "rechaza el RFC genérico" || res.State.Gate.Name != GateWalkthrough {
		t.Errorf("estado: %+v", res.State)
	}
}

// testConfig es el flujo por defecto con dos agentes de calidad, para probar
// los reportes en paralelo (con security_audit: true el repo queda así).
func testConfig() Config {
	c := DefaultConfig()
	c.Agents[Quality] = []string{rv, sec}
	return c
}

func TestDefaultQualityIsReviewerAlone(t *testing.T) {
	cfg := DefaultConfig()
	if q := cfg.Agents[Quality]; len(q) != 1 || q[0] != rv {
		t.Fatalf("quality por defecto: %v", q)
	}
	s := stateFor(row{lane: Light, from: Quality})
	if n := NextFor(cfg, s); n.Parallel || len(n.Agents) != 1 || n.Agents[0].Agent != rv {
		t.Errorf("next: %+v", n)
	}
	res, err := Apply(cfg, s, rep(rv, Approved))
	if err != nil || res.State.Phase != Documenting {
		t.Errorf("el reviewer solo cierra quality: %v %v", res.State.Phase, err)
	}
	res, err = Apply(cfg, s, rep(rv, RejectedV))
	if err != nil || res.State.Phase != Implementing || res.State.Round != 1 {
		t.Errorf("rechazo del reviewer: %v ronda %d %v", res.State.Phase, res.State.Round, err)
	}
}

func closedOutside(reason string) Event { return Event{Kind: EvClosedOutside, Reason: reason} }

func TestClosedOutsideFromAnyPhase(t *testing.T) {
	cfg := testConfig()
	for _, r := range table() {
		if r.err != "" || r.from == Backlog || r.from == Done {
			continue
		}
		s := stateFor(r)
		res, err := Apply(cfg, s, closedOutside("pr"))
		if err != nil {
			t.Fatalf("%s: %v", r.name(), err)
		}
		if res.State.Phase != Done || res.State.Gate != nil || res.State.Block != nil {
			t.Errorf("%s: quedó %+v", r.name(), res.State)
		}
		// Bloqueada (con o sin gate previo) también cierra.
		b, err := Apply(cfg, s, Event{Kind: EvBlock, Note: "falta acceso"})
		if err != nil {
			continue
		}
		res, err = Apply(cfg, b.State, closedOutside("tracker"))
		if err != nil || res.State.Phase != Done || res.State.Block != nil || res.State.Gate != nil {
			t.Errorf("%s: bloqueada no cerró: %+v %v", r.name(), res.State, err)
		}
	}
	s := State{ID: "T-1", Slug: "x", Lane: Light, Phase: Implementing, Gate: &PendingGate{Name: GateDecision, Options: []string{"a", "b"}}}
	res, err := Apply(cfg, s, closedOutside("tracker"))
	if err != nil || res.State.Phase != Done || res.State.Gate != nil {
		t.Errorf("con gate decision abierto: %+v %v", res.State, err)
	}
}

func TestClosedOutsideTrackerEmitsNoEffect(t *testing.T) {
	cfg := testConfig()
	s := State{ID: "T-1", Slug: "x", Lane: Light, Phase: Implementing}
	res, err := Apply(cfg, s, closedOutside("tracker"))
	if err != nil || len(res.Effects) != 0 {
		t.Errorf("tracker: efectos %v %v", res.Effects, err)
	}
	res, err = Apply(cfg, s, closedOutside("pr"))
	if err != nil || len(res.Effects) != 1 || res.Effects[0].Kind != FxTrackerState || res.Effects[0].Phase != Done {
		t.Errorf("pr: efectos %v %v", res.Effects, err)
	}
}

func TestClosedOutsideRejectsBacklogAndDone(t *testing.T) {
	cfg := testConfig()
	for phase, code := range map[Phase]string{Backlog: "not_started", Done: "task_done"} {
		_, err := Apply(cfg, State{ID: "T-1", Slug: "x", Lane: Light, Phase: phase}, closedOutside("pr"))
		var rj *Rejection
		if !errors.As(err, &rj) || rj.Code != code {
			t.Errorf("%s: %v, want %s", phase, err, code)
		}
	}
}

func TestRejectBackToImplementingFromDocumenting(t *testing.T) {
	s := stateFor(row{lane: Light, from: Documenting})
	res, err := Apply(testConfig(), s, Event{Kind: EvReject, To: Implementing, Note: "defecto en X"})
	if err != nil {
		t.Fatal(err)
	}
	st := res.State
	if st.Phase != Implementing || !st.Resume || st.Note != "defecto en X" || st.Gate != nil {
		t.Errorf("estado: %+v", st)
	}
	k := kinds(res.Effects)
	if slices.Contains(k, cs) || slices.Contains(k, ss) || !slices.Contains(k, ts) {
		t.Errorf("efectos: %v", k)
	}
	found := false
	for _, e := range res.Effects {
		if e.Kind == cm && strings.Contains(e.Body, "**Regresada a implementing desde documenting:** defecto en X") {
			found = true
		}
	}
	if !found {
		t.Errorf("falta comentario: %+v", res.Effects)
	}
}

func TestRejectBackToImplementingFromQuality(t *testing.T) {
	s := stateFor(row{lane: Full, from: Quality, reports: ko, round: 1})
	res, err := Apply(testConfig(), s, rej("volver"))
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Implementing || len(res.State.Reports) != 0 || res.State.Round != 1 || !res.State.Resume {
		t.Errorf("estado: %+v", res.State)
	}
}

func TestRejectBackNeedsNote(t *testing.T) {
	_, err := Apply(testConfig(), stateFor(row{lane: Full, from: Documenting}), Event{Kind: EvReject})
	var rj *Rejection
	if !errors.As(err, &rj) || rj.Code != "note_required" {
		t.Errorf("err=%v", err)
	}
}

func TestRejectBackInvalidTarget(t *testing.T) {
	_, err := Apply(testConfig(), stateFor(row{lane: Full, from: Documenting}), rejTo(Spec))
	var rj *Rejection
	if !errors.As(err, &rj) || rj.Code != "invalid_target" {
		t.Errorf("err=%v", err)
	}
}

func TestRejectWithoutGateElsewhereIsNoGate(t *testing.T) {
	check := func(r row, ev Event) {
		_, err := Apply(testConfig(), stateFor(r), ev)
		var rj *Rejection
		if !errors.As(err, &rj) || rj.Code != "no_gate" {
			t.Errorf("%s %+v: err=%v", r.from, ev, err)
		}
	}
	for _, p := range []Phase{Spec, Contract, Implementing} {
		check(row{lane: Full, from: p}, rej("x"))
	}
	check(row{lane: Full, from: Documenting}, Event{Kind: EvReject, Gate: GateWalkthrough, Note: "x"})
}

// R5, R8: BudgetDue solo da ok cuando la fase está presupuestada, no es
// terminal, k ≥ 1 y k > el último múltiplo respondido.
func TestBudgetDue(t *testing.T) {
	cfg := testConfig()
	cfg.Budget = map[Phase]int64{Implementing: 200_000}
	s := stateFor(row{lane: Full, from: Implementing})

	if k, ok := BudgetDue(cfg, s, 150_000); ok {
		t.Errorf("sin pasar el presupuesto no debía tocar: k=%d", k)
	}
	k, ok := BudgetDue(cfg, s, 412_000)
	if !ok || k != 2 {
		t.Errorf("412k/200k debía dar k=2 ok=true: k=%d ok=%v", k, ok)
	}
	s.BudgetAck = map[Phase]int{Implementing: 2}
	if _, ok := BudgetDue(cfg, s, 412_000); ok {
		t.Error("k ya respondido no debía volver a preguntar")
	}
	if k, ok := BudgetDue(cfg, s, 650_000); !ok || k != 3 {
		t.Errorf("al llegar a 3x debía preguntar otra vez: k=%d ok=%v", k, ok)
	}
	// Fase sin presupuesto, o terminal (Blocked): nunca toca.
	s2 := stateFor(row{lane: Full, from: Quality})
	if _, ok := BudgetDue(cfg, s2, 999_000); ok {
		t.Error("quality no está presupuestada")
	}
	blocked := stateFor(row{lane: Full, from: Implementing})
	blocked.Phase = Blocked
	if _, ok := BudgetDue(cfg, blocked, 999_000); ok {
		t.Error("una tarea bloqueada no pregunta por presupuesto")
	}
}

// R7, R11: approve --gate budget marca el múltiplo como respondido y no
// cambia ni la fase ni el gate pendiente.
func TestApproveBudgetKeepsGate(t *testing.T) {
	cfg := testConfig()
	s := stateFor(row{lane: Full, from: Implementing})
	s.BudgetAck = map[Phase]int{Implementing: 1}

	res, err := Apply(cfg, s, Event{Kind: EvApprove, Gate: GateBudget, Choice: 2})
	if err != nil {
		t.Fatalf("approve budget: %v", err)
	}
	if res.State.Phase != Implementing || res.State.Gate != nil {
		t.Errorf("approve budget no debía mover la tarea: %+v", res.State)
	}
	if res.State.BudgetAck[Implementing] != 2 {
		t.Errorf("BudgetAck no se actualizó: %v", res.State.BudgetAck)
	}

	// R11: sin pregunta vigente (choice ≤ BudgetAck) se rechaza.
	_, err = Apply(cfg, res.State, Event{Kind: EvApprove, Gate: GateBudget, Choice: 2})
	var rj *Rejection
	if !errors.As(err, &rj) || rj.Code != "no_budget_gate" {
		t.Errorf("approve budget repetido debía rechazarse: %v", err)
	}
}

// R9, R10: reject --gate budget bloquea la tarea y marca el múltiplo como
// respondido (no se vuelve a preguntar hasta el siguiente múltiplo).
func TestRejectBudgetBlocks(t *testing.T) {
	cfg := testConfig()
	s := stateFor(row{lane: Full, from: Implementing})

	res, err := Apply(cfg, s, Event{Kind: EvReject, Gate: GateBudget, Choice: 2, Note: "presupuesto de implementing superado (412k / 200k nuevos): a revisar"})
	if err != nil {
		t.Fatalf("reject budget: %v", err)
	}
	if res.State.Phase != Blocked || res.State.Block == nil {
		t.Errorf("reject budget debía bloquear: %+v", res.State)
	}
	if res.State.BudgetAck[Implementing] != 2 {
		t.Errorf("BudgetAck no se actualizó al rechazar: %v", res.State.BudgetAck)
	}
}

// R11: approve/reject --gate budget sin pregunta vigente (choice < 1 o ya
// respondido) se rechaza con no_budget_gate.
func TestBudgetGateWithoutDue(t *testing.T) {
	cfg := testConfig()
	s := stateFor(row{lane: Full, from: Implementing})

	_, err := Apply(cfg, s, Event{Kind: EvApprove, Gate: GateBudget, Choice: 0})
	var rj *Rejection
	if !errors.As(err, &rj) || rj.Code != "no_budget_gate" {
		t.Errorf("choice=0 debía rechazarse: %v", err)
	}
	_, err = Apply(cfg, s, Event{Kind: EvReject, Gate: GateBudget, Choice: 0, Note: "x"})
	if !errors.As(err, &rj) || rj.Code != "no_budget_gate" {
		t.Errorf("choice=0 en reject debía rechazarse: %v", err)
	}
}

func hotfixDone(noRepro string) Event {
	ev := rep(imp, DoneV)
	ev.NoRepro = noRepro
	return ev
}

func reproGateState() State {
	s := stateFor(row{lane: Hotfix, from: Implementing})
	s.Gate = &PendingGate{Name: GateRepro, Agent: imp, Note: "solo cambia un texto de la guía"}
	return s
}

func TestNoReproOpensGate(t *testing.T) {
	cfg := testConfig()
	res, err := Apply(cfg, stateFor(row{lane: Hotfix, from: Implementing}), hotfixDone("defecto de docs"))
	if err != nil {
		t.Fatal(err)
	}
	g := res.State.Gate
	if res.State.Phase != Implementing || g == nil || g.Name != GateRepro || g.Agent != imp || g.Note != "defecto de docs" {
		t.Fatalf("debe abrir el gate repro sin cambiar de fase: fase %s gate %+v", res.State.Phase, g)
	}
	if res.State.NoRepro != "" {
		t.Errorf("el motivo se guarda al aprobar, no al pedirlo: %q", res.State.NoRepro)
	}
	if _, err := Apply(cfg, stateFor(row{lane: Hotfix, from: Implementing}), func() Event { e := hotfixDone("x"); e.CheckOK = false; return e }()); err == nil {
		t.Error("sin check vigente el escape no abre el gate")
	}
}

func TestApproveReproAdvances(t *testing.T) {
	res, err := Apply(testConfig(), reproGateState(), Event{Kind: EvApprove})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Phase != Quality || res.State.Gate != nil {
		t.Fatalf("aprobar avanza a quality: %s %+v", res.State.Phase, res.State.Gate)
	}
	if res.State.NoRepro != "solo cambia un texto de la guía" {
		t.Errorf("el motivo queda en el estado: %q", res.State.NoRepro)
	}
	var body string
	for _, fx := range res.Effects {
		if fx.Kind == FxComment {
			body = fx.Body
		}
	}
	if !strings.Contains(body, "Hotfix sin prueba de reproducción (aprobado):") || !strings.Contains(body, "solo cambia un texto de la guía") {
		t.Errorf("comentario al tracker: %q", body)
	}
	// El motivo sobrevive a las rondas de corrección (enter no lo limpia).
	back, err := Apply(testConfig(), res.State, Event{Kind: EvReject, Note: "falta X"})
	if err != nil || back.State.Phase != Implementing || back.State.NoRepro == "" {
		t.Errorf("la ronda de corrección conserva el motivo: %v %+v", err, back.State)
	}
}

func TestRejectReproBackToImplementing(t *testing.T) {
	res, err := Apply(testConfig(), reproGateState(), Event{Kind: EvReject, Note: "escribe la prueba"})
	if err != nil {
		t.Fatal(err)
	}
	s := res.State
	if s.Phase != Implementing || s.Gate != nil || !s.Resume || s.Note != "escribe la prueba" || s.NoRepro != "" {
		t.Fatalf("rechazar cierra el gate y reanuda al implementer: %+v", s)
	}
	if _, err := Apply(testConfig(), reproGateState(), Event{Kind: EvReject}); err == nil {
		t.Error("rechazar exige --note")
	}
}

func TestNoReproInvalid(t *testing.T) {
	cfg := testConfig()
	cases := []struct {
		name string
		s    State
		ev   Event
	}{
		{"carril full", stateFor(row{lane: Full, from: Implementing}), hotfixDone("x")},
		{"carril light", stateFor(row{lane: Light, from: Implementing}), hotfixDone("x")},
		{"otra fase", stateFor(row{lane: Hotfix, from: Quality}), func() Event { e := rep(rv, Approved); e.NoRepro = "x"; return e }()},
		{"otro veredicto", stateFor(row{lane: Hotfix, from: Implementing}), func() Event { e := rep(imp, NeedsDecision); e.NoRepro = "x"; return e }()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Apply(cfg, c.s, c.ev)
			var rj *Rejection
			if !errors.As(err, &rj) || rj.Code != "no_repro_invalid" {
				t.Fatalf("esperaba no_repro_invalid, got %v", err)
			}
		})
	}
}
