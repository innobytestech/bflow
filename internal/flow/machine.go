package flow

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/output"
)

// Result es el resultado de aplicar un evento.
type Result struct {
	State   State
	Effects []Effect
	Next    output.Next
}

// Apply aplica ev sobre s. Si las reglas no lo permiten devuelve *Rejection y
// el estado no cambia.
func Apply(cfg Config, s State, ev Event) (Result, error) {
	t := &tx{cfg: cfg, s: clone(s)}
	if err := t.apply(ev); err != nil {
		return Result{State: s}, err
	}
	return Result{State: t.s, Effects: t.effects(), Next: NextFor(cfg, t.s)}, nil
}

// tx acumula los cambios de una transición.
type tx struct {
	cfg      Config
	s        State
	moved    bool
	branch   bool
	stamp    bool
	openPR   bool
	comments []string
}

// effects ordena los efectos: primero lo que puede fallar sin dejar rastro
// (rama, PR), luego el tracker.
func (t *tx) effects() []Effect {
	var fx []Effect
	if t.branch {
		fx = append(fx, Effect{Kind: FxCreateBranch, Hotfix: t.s.Lane == Hotfix})
	}
	if t.openPR {
		fx = append(fx, Effect{Kind: FxOpenPR})
	}
	if t.moved {
		fx = append(fx, Effect{Kind: FxTrackerState, Phase: t.s.Phase})
	}
	if t.stamp {
		fx = append(fx, Effect{Kind: FxStampStart})
	}
	for _, c := range t.comments {
		fx = append(fx, Effect{Kind: FxComment, Body: c})
	}
	return fx
}

func (t *tx) apply(ev Event) error {
	s := &t.s
	if s.Phase == Done {
		return reject("task_done", "la tarea %s ya está cerrada", s.ID)
	}
	if s.Phase == Dropped {
		return reject("task_dropped", "la tarea %s ya se retiró", s.ID)
	}
	if ev.Kind == EvDrop {
		if s.Phase == Backlog {
			return reject("not_started", "la tarea %s no ha empezado", s.ID)
		}
		return t.drop(ev.Note)
	}
	if ev.Kind == EvClosedOutside {
		if s.Phase == Backlog {
			return reject("not_started", "la tarea %s no ha empezado", s.ID)
		}
		s.Block = nil
		t.enter(Done)
		if ev.Reason == "tracker" {
			t.moved = false // el tracker ya la tiene cerrada
		}
		return nil
	}
	if s.Phase == Blocked && ev.Kind != EvUnblock {
		return reject("blocked", "la tarea %s está bloqueada (%s); desbloquéala primero", s.ID, s.Block.Reason)
	}
	switch ev.Kind {
	case EvStart:
		return t.start(ev)
	case EvApprove:
		return t.approve(ev)
	case EvReject:
		return t.reject(ev)
	case EvReport:
		return t.report(ev)
	case EvPause:
		return t.pause()
	case EvBlock:
		return t.block(ev.Note)
	case EvUnblock:
		return t.unblock(ev.Note)
	case EvMerged:
		if s.Phase != InReview {
			return reject("not_in_review", "solo se cierra por merge una tarea en in_review (está en %s)", s.Phase)
		}
		t.enter(Done)
		return nil
	}
	return reject("unknown_event", "evento desconocido %q", ev.Kind)
}

// enter mueve la tarea a p y prepara lo que la fase necesita al entrar.
func (t *tx) enter(p Phase) {
	s := &t.s
	s.Phase = p
	s.Gate = nil
	s.Reports = nil
	s.RejectNotes = nil
	s.Resume = false
	s.Decision = ""
	s.Note = ""
	s.RejectHead = ""
	t.moved = true
	if (p == Contract || p == Implementing) && !s.BranchCreated {
		s.BranchCreated = true
		t.branch = true
	}
	if p == Implementing && !s.Started {
		s.Started = true
		t.stamp = true
	}
	switch p {
	case Discovery:
		if s.Scout != ScoutPending {
			s.Gate = &PendingGate{Name: GateDiscovery}
		}
	case Spec:
		s.Round = 0
	case Paused:
		s.Gate = &PendingGate{Name: GatePause}
	case Walkthrough:
		// Primero el humano contesta qué espera del producto, sin ver el código;
		// después recorre el diff. Si se pregunta todo junto, se aprueba sin mirar.
		s.Gate = &PendingGate{Name: GateQuestions}
	}
}

// pause marca la tarea para parar al terminar implementing.
func (t *tx) pause() error {
	s := &t.s
	switch s.Phase {
	case Backlog:
		return reject("not_started", "la tarea %s no ha empezado", s.ID)
	case Discovery, Spec, Contract, Implementing:
		s.PauseRequested = true
		return nil
	}
	return reject("pause_late", "la tarea %s ya pasó implementing (fase %s): no hay nada que pausar", s.ID, s.Phase)
}

// afterImplementing entra a la fase que sigue al DONE de implementing: paused
// si el carril la tiene o una persona pidió pausa.
func (t *tx) afterImplementing() {
	next := t.cfg.after(t.s.Lane, Implementing)
	if t.s.PauseRequested {
		next = Paused
	}
	t.enter(next)
	t.s.PauseRequested = false
}

func (t *tx) start(ev Event) error {
	s := &t.s
	if s.Phase != Backlog {
		return reject("already_started", "la tarea %s ya empezó (fase %s)", s.ID, s.Phase)
	}
	phases, ok := t.cfg.Lanes[ev.Lane]
	if !ok {
		return reject("unknown_lane", "carril desconocido %q (disponibles: %s)", ev.Lane, laneNames(t.cfg))
	}
	if ev.Fixes != "" && ev.Lane != Hotfix {
		return reject("fixes_needs_hotfix", "--fixes solo aplica al carril hotfix (se pidió %s)", ev.Lane)
	}
	if ev.Fixes == s.ID {
		return reject("fixes_self", "%s no puede corregirse a sí misma", s.ID)
	}
	s.Lane = ev.Lane
	if t.cfg.Scout != "" {
		s.Scout = ScoutPending
	}
	t.enter(phases[0])
	return nil
}

func (t *tx) pendingGate(ev Event) (*PendingGate, error) {
	s := &t.s
	if s.Gate == nil {
		return nil, reject("no_gate", "no hay ninguna decisión pendiente en %s (fase %s)", s.ID, s.Phase)
	}
	if ev.Gate != "" && ev.Gate != s.Gate.Name {
		return nil, reject("gate_mismatch", "el gate pendiente es %q, no %q", s.Gate.Name, ev.Gate)
	}
	return s.Gate, nil
}

// approveBudget registra que la persona vio la pregunta de presupuesto y
// sigue: no toca la fase ni el gate pendiente (R7, R11).
func (t *tx) approveBudget(ev Event) error {
	s := &t.s
	if ev.Choice < 1 || ev.Choice <= s.BudgetAck[s.Phase] {
		return reject("no_budget_gate", "no hay pregunta de presupuesto vigente en %s", s.Phase)
	}
	if s.BudgetAck == nil {
		s.BudgetAck = map[Phase]int{}
	}
	s.BudgetAck[s.Phase] = ev.Choice
	return nil
}

// rejectBudget bloquea la tarea con el motivo que el engine ya armó (gasto +
// nota) y marca el múltiplo como respondido (R9, R10).
func (t *tx) rejectBudget(ev Event) error {
	s := &t.s
	if ev.Choice < 1 || ev.Choice <= s.BudgetAck[s.Phase] {
		return reject("no_budget_gate", "no hay pregunta de presupuesto vigente en %s", s.Phase)
	}
	if ev.To != "" {
		return reject("bad_target", "un rechazo de presupuesto no admite --to")
	}
	note := strings.TrimSpace(ev.Note)
	if note == "" {
		return reject("note_required", "un rechazo de presupuesto necesita --note con el motivo")
	}
	if s.BudgetAck == nil {
		s.BudgetAck = map[Phase]int{}
	}
	s.BudgetAck[s.Phase] = ev.Choice
	return t.block(note)
}

func (t *tx) approve(ev Event) error {
	s := &t.s
	if ev.Gate == GateBudget {
		return t.approveBudget(ev)
	}
	g, err := t.pendingGate(ev)
	if err != nil {
		return err
	}
	if ev.Manual && g.Name != GateDecision {
		return reject("bad_choice", "--manual solo aplica al gate decision")
	}
	if ev.Choice != 0 && g.Name != GateDecision {
		return reject("bad_choice", "--choice solo aplica al gate decision")
	}
	switch g.Name {
	case GateDiscovery:
		if strings.TrimSpace(ev.Attachment) != "" {
			t.comments = append(t.comments, "## Discovery\n\n"+strings.TrimSpace(ev.Attachment))
		}
		t.enter(t.cfg.after(s.Lane, Discovery))
	case GateSpec:
		t.enter(t.cfg.after(s.Lane, Spec))
	case GateSplit:
		if len(ev.Children) == 0 {
			return reject("split_children", "aprobar el split necesita las hijas creadas")
		}
		t.comments = append(t.comments, "**Dividida en:**\n- "+strings.Join(ev.Children, "\n- "))
		s.Block = nil
		t.enter(Dropped)
	case GateContract:
		t.enter(t.cfg.after(s.Lane, Contract))
		s.Resume = true
	case GateDecision:
		switch {
		case ev.Choice > 0 && ev.Choice <= len(g.Options):
			s.Decision = g.Options[ev.Choice-1]
		case ev.Choice != 0:
			return reject("bad_choice", "opción %d fuera de rango (1..%d)", ev.Choice, len(g.Options))
		case strings.TrimSpace(ev.Note) != "":
			s.Decision = strings.TrimSpace(ev.Note)
		default:
			return reject("bad_choice", "elige una opción (--choice) o escribe la decisión (--note)")
		}
		s.Gate = nil
		s.Resume = true
		if ev.Manual {
			// La opción exige trabajo que un agente no puede hacer (lanzar otro
			// agente, verificar a mano): se bloquea hasta que la sesión principal
			// lo haga y corra unblock --note con el resultado.
			if err := t.block("la sesión principal debe hacer esto antes de seguir: " + s.Decision); err != nil {
				return err
			}
			s.Block.Manual = true
			return nil
		}
	case GatePause:
		t.enter(t.cfg.afterPause(s.Lane))
	case GateRounds:
		s.Gate = nil
		s.Resume = true
	case GateQuestions:
		s.Note = strings.TrimSpace(ev.Note) // sus respuestas, para compararlas en el recorrido
		s.Gate = &PendingGate{Name: GateWalkthrough}
	case GateRepro:
		s.NoRepro = g.Note
		t.comments = append(t.comments, "**Hotfix sin prueba de reproducción (aprobado):** "+g.Note)
		t.enter(t.cfg.after(s.Lane, Implementing))
	case GateWalkthrough:
		t.openPR = true
		t.enter(t.cfg.after(s.Lane, Walkthrough))
	default:
		return reject("unknown_gate", "gate desconocido %q", g.Name)
	}
	return nil
}

// rejectTargets: destino por defecto y destinos permitidos de cada gate.
var rejectTargets = map[Gate][]Phase{
	GateDiscovery:   {Discovery},
	GateSpec:        {Spec},
	GateSplit:       {Spec},
	GateContract:    {Contract, Spec},
	GatePause:       {Implementing},
	GateRounds:      {Spec},
	GateWalkthrough: {Implementing, Spec},
	GateRepro:       {Implementing},
}

// RejectTargets devuelve los destinos válidos de un rechazo en el carril.
func RejectTargets(cfg Config, lane Lane, g Gate) []Phase {
	var out []Phase
	for _, p := range rejectTargets[g] {
		if cfg.has(lane, p) {
			out = append(out, p)
		}
	}
	return out
}

// decisionTargets son las fases a las que se puede volver desde una decisión:
// spec y contract, si el carril las tiene y van antes de la fase actual (la
// más cercana primero). Sirve cuando el agente descubre que el contrato o la
// spec están mal: en la piloto, un contrato de pruebas vacías congeladas.
func decisionTargets(cfg Config, lane Lane, phase Phase) []Phase {
	phases := cfg.Lanes[lane]
	cur := slices.Index(phases, phase)
	var out []Phase
	for _, p := range []Phase{Contract, Spec} {
		if i := slices.Index(phases, p); i >= 0 && i < cur {
			out = append(out, p)
		}
	}
	return out
}

func (t *tx) reject(ev Event) error {
	s := &t.s
	if ev.Gate == GateBudget {
		return t.rejectBudget(ev)
	}
	if s.Gate == nil && ev.Gate == "" && len(backTargets[s.Phase]) > 0 {
		return t.back(ev)
	}
	g, err := t.pendingGate(ev)
	if err != nil {
		return err
	}
	allowed, ok := rejectTargets[g.Name]
	if g.Name == GateDecision {
		allowed = decisionTargets(t.cfg, s.Lane, s.Phase)
		ok = len(allowed) > 0
	}
	if !ok {
		return reject("not_rejectable", "el gate %q no se rechaza: aprueba una opción o escribe la decisión", g.Name)
	}
	note := strings.TrimSpace(ev.Note)
	if note == "" {
		return reject("note_required", "un rechazo necesita --note con el motivo")
	}
	to := allowed[0]
	if ev.To != "" {
		to = ev.To
	}
	if !slices.Contains(allowed, to) || !t.cfg.has(s.Lane, to) {
		valid := RejectTargets(t.cfg, s.Lane, g.Name)
		if g.Name == GateDecision {
			valid = allowed
		}
		if len(valid) == 0 {
			return reject("invalid_target", "el gate %q no tiene destino de rechazo en el carril %s", g.Name, s.Lane)
		}
		return reject("invalid_target", "un rechazo de %q no puede ir a %s en el carril %s (válidos: %s)", g.Name, to, s.Lane, joinPhases(valid))
	}
	t.comments = append(t.comments, fmt.Sprintf("**Rechazado en %s:** %s", g.Name, note))
	if g.Name == GateRepro {
		s.ReproRejected = true
	}
	switch {
	case to == s.Phase && g.Name == GateDiscovery:
		// el discovery sigue abierto; el gate se mantiene
	case to == s.Phase:
		s.Gate = nil
		s.Resume = true
	default:
		t.enter(to)
		s.Resume = to == Implementing
	}
	s.Note = note
	return nil
}

// backTargets: fases sin gate desde las que un humano puede regresar a implementing.
var backTargets = map[Phase][]Phase{Quality: {Implementing}, Documenting: {Implementing}}

// back regresa a implementing desde quality o documenting sin gate pendiente.
// La ronda de calidad no cambia: no es un rechazo de la compuerta.
func (t *tx) back(ev Event) error {
	s := &t.s
	note := strings.TrimSpace(ev.Note)
	if note == "" {
		return reject("note_required", "regresar a implementing necesita --note con el motivo")
	}
	to := Implementing
	if ev.To != "" {
		to = ev.To
	}
	if !slices.Contains(backTargets[s.Phase], to) || !t.cfg.has(s.Lane, to) {
		return reject("invalid_target", "desde %s sin gate solo se regresa a %s (carril %s)", s.Phase, joinPhases(backTargets[s.Phase]), s.Lane)
	}
	from := s.Phase
	round := s.Round
	t.enter(Implementing)
	s.Round = round
	s.Resume = true
	s.Note = note
	t.comments = append(t.comments, fmt.Sprintf("**Regresada a implementing desde %s:** %s", from, note))
	return nil
}

// verdictsFor: veredictos válidos por fase.
var verdictsFor = map[Phase][]Verdict{
	Spec:         {Ready, Split, NeedsDecision},
	Contract:     {ContractReady, NeedsDecision, BlockedV},
	Implementing: {DoneV, NeedsDecision, BlockedV},
	Quality:      {Approved, RejectedV},
	Documenting:  {DoneV, BlockedV},
}

// ScoutVerdicts son los veredictos que el scout puede reportar.
var ScoutVerdicts = []Verdict{DoneV}

// Verdicts devuelve los veredictos que un agente puede reportar en la fase.
func Verdicts(p Phase) []Verdict { return verdictsFor[p] }

// scoutReport registra el reporte del scout: corre una vez por tarea y no
// cuenta como reporte de fase.
func (t *tx) scoutReport(ev Event) error {
	s := &t.s
	switch s.Scout {
	case ScoutDone:
		return reject("already_reported", "el scout ya reportó en %s", s.ID)
	case ScoutPending:
	default:
		return reject("scout_not_due", "%s no lleva scout", s.ID)
	}
	if !slices.Contains(ScoutVerdicts, ev.Verdict) {
		return reject("unexpected_verdict", "el scout no puede reportar %s (válidos: %s)", ev.Verdict, joinVerdicts(ScoutVerdicts))
	}
	s.Scout = ScoutDone
	if s.Phase == Discovery {
		s.Gate = &PendingGate{Name: GateDiscovery}
	}
	return nil
}

func (t *tx) report(ev Event) error {
	s := &t.s
	if ev.Agent == ScoutAgent {
		return t.scoutReport(ev)
	}
	if s.Scout == ScoutPending {
		return reject("scout_pending", "primero corre el scout (bflow-scout) y reporta; %s espera su reporte", s.ID)
	}
	if s.Gate != nil {
		return reject("gate_pending", "hay una decisión pendiente (%s); resuélvela antes de aceptar reportes", s.Gate.Name)
	}
	agents := t.cfg.Agents[s.Phase]
	if !slices.Contains(agents, ev.Agent) {
		if len(agents) == 0 {
			return reject("unexpected_agent", "en la fase %s no trabaja ningún agente", s.Phase)
		}
		return reject("unexpected_agent", "%q no trabaja en la fase %s (esperados: %s)", ev.Agent, s.Phase, strings.Join(agents, ", "))
	}
	if _, done := s.Reports[ev.Agent]; done {
		return reject("already_reported", "%s ya reportó en esta fase", ev.Agent)
	}
	valid := verdictsFor[s.Phase]
	if !slices.Contains(valid, ev.Verdict) {
		return reject("unexpected_verdict", "%s no puede reportar %s en %s (válidos: %s)", ev.Agent, ev.Verdict, s.Phase, joinVerdicts(valid))
	}

	if strings.TrimSpace(ev.NoRepro) != "" && (ev.Verdict != DoneV || s.Phase != Implementing || s.Lane != Hotfix) {
		return reject("no_repro_invalid", "--no-repro solo aplica a un DONE del implementer en el carril hotfix")
	}
	if strings.TrimSpace(ev.NoRepro) != "" && s.ReproRejected {
		return reject("no_repro_rejected", "un humano ya rechazó --no-repro en esta tarea: agrega un commit de solo pruebas que reproduzca el defecto, corre `bflow check %s` (debe fallar) y luego la corrección", s.ID)
	}

	switch ev.Verdict {
	case NeedsDecision:
		s.Gate = &PendingGate{Name: GateDecision, Agent: ev.Agent, Options: ev.Options, Note: ev.Note}
		return nil
	case Split:
		s.Gate = &PendingGate{Name: GateSplit, Agent: ev.Agent, Note: ev.Note}
		return nil
	case BlockedV:
		return t.block(fmt.Sprintf("%s: %s", ev.Agent, strings.TrimSpace(ev.Note)))
	}

	switch s.Phase {
	case Spec:
		t.record(ev)
		if t.allReported() {
			s.Reports = nil
			s.Gate = &PendingGate{Name: GateSpec}
		}
	case Contract:
		if ev.ContractOK {
			t.enter(t.cfg.after(s.Lane, Contract))
			s.Resume = true
			return nil
		}
		s.Gate = &PendingGate{Name: GateContract}
	case Implementing:
		if !ev.CheckOK {
			return reject("check_required", "DONE requiere un check verde sobre el código actual (bflow check %s)", s.ID)
		}
		if ev.NoRepro != "" {
			s.Gate = &PendingGate{Name: GateRepro, Agent: ev.Agent, Note: ev.NoRepro}
			return nil
		}
		t.afterImplementing()
	case Quality:
		t.record(ev)
		if !t.allReported() {
			return nil
		}
		var rejected []string
		for _, a := range t.cfg.Agents[Quality] {
			if s.Reports[a] == RejectedV {
				rejected = append(rejected, a)
			}
		}
		if len(rejected) == 0 {
			t.enter(t.cfg.after(s.Lane, Quality))
			return nil
		}
		round := s.Round + 1
		notes := s.RejectNotes
		t.enter(Implementing)
		s.Round = round
		s.Resume = true
		s.Note = "Compuerta de calidad rechazada por: " + strings.Join(rejected, ", ") + ". Corrige lo siguiente y haz commit antes de reportar DONE:"
		for _, a := range rejected {
			if n := notes[a]; n != "" {
				s.Note += "\n- " + a + ": " + n
			}
		}
		s.Note += "\nLee sus reportes completos en " + TaskDir(s.ID) + "/reports/."
		t.comments = append(t.comments, fmt.Sprintf("**Compuerta de calidad rechazada (ronda %d):** %s", round, strings.Join(rejected, ", ")))
		if round >= t.cfg.MaxQualityRounds {
			s.Gate = &PendingGate{Name: GateRounds}
		}
	case Documenting:
		t.enter(t.cfg.after(s.Lane, Documenting))
	}
	return nil
}

func (t *tx) record(ev Event) {
	if t.s.Reports == nil {
		t.s.Reports = map[string]Verdict{}
	}
	t.s.Reports[ev.Agent] = ev.Verdict
	if ev.Verdict == RejectedV && strings.TrimSpace(ev.Note) != "" {
		if t.s.RejectNotes == nil {
			t.s.RejectNotes = map[string]string{}
		}
		t.s.RejectNotes[ev.Agent] = strings.TrimSpace(ev.Note)
	}
}

func (t *tx) allReported() bool {
	for _, a := range t.cfg.Agents[t.s.Phase] {
		if _, ok := t.s.Reports[a]; !ok {
			return false
		}
	}
	return true
}

func (t *tx) block(reason string) error {
	s := &t.s
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reject("note_required", "bloquear necesita un motivo (--reason)")
	}
	s.Block = &BlockInfo{Reason: reason, From: s.Phase}
	s.Phase = Blocked
	t.moved = true
	t.comments = append(t.comments, "**Bloqueada:** "+reason)
	return nil
}

func (t *tx) drop(note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return reject("note_required", "retirar necesita --note con el motivo")
	}
	t.s.Block = nil
	t.enter(Dropped)
	t.comments = append(t.comments, "**Retirada:** "+note)
	return nil
}

func (t *tx) unblock(note string) error {
	s := &t.s
	if s.Phase != Blocked || s.Block == nil {
		return reject("not_blocked", "la tarea %s no está bloqueada", s.ID)
	}
	s.Phase = s.Block.From
	s.Block = nil
	if note = strings.TrimSpace(note); note != "" {
		s.Note = note // llega en los args del agente que retoma
		s.Resume = true
	}
	t.moved = true
	return nil
}

func clone(s State) State {
	c := s
	if s.Reports != nil {
		c.Reports = maps.Clone(s.Reports)
	}
	if s.RejectNotes != nil {
		c.RejectNotes = maps.Clone(s.RejectNotes)
	}
	if s.Gate != nil {
		g := *s.Gate
		g.Options = slices.Clone(s.Gate.Options)
		c.Gate = &g
	}
	if s.Block != nil {
		b := *s.Block
		c.Block = &b
	}
	if s.BudgetAck != nil {
		c.BudgetAck = maps.Clone(s.BudgetAck)
	}
	return c
}

func laneNames(cfg Config) string {
	var ls []string
	for l := range cfg.Lanes {
		ls = append(ls, string(l))
	}
	slices.Sort(ls)
	return strings.Join(ls, ", ")
}

func joinPhases(ps []Phase) string {
	var out []string
	for _, p := range ps {
		out = append(out, string(p))
	}
	return strings.Join(out, ", ")
}

func joinVerdicts(vs []Verdict) string {
	var out []string
	for _, v := range vs {
		out = append(out, string(v))
	}
	return strings.Join(out, "|")
}
