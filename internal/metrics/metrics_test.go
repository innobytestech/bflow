package metrics

import (
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

var t0 = time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func e(min int, event string, from, to flow.Phase, opts ...func(*store.Entry)) store.Entry {
	en := store.Entry{TS: at(min), ID: "T-1", Event: event, From: from, To: to}
	for _, o := range opts {
		o(&en)
	}
	return en
}

func gateOpened(g string) func(*store.Entry) {
	return func(en *store.Entry) { en.Data = map[string]any{"gate_opened": g} }
}
func gate(g string) func(*store.Entry) { return func(en *store.Entry) { en.Gate = g } }
func verdict(a, v string) func(*store.Entry) {
	return func(en *store.Entry) { en.Agent, en.Verdict = a, v }
}
func round(n int) func(*store.Entry) { return func(en *store.Entry) { en.Round = n } }
func tokens(p flow.Phase, in, out, cr, cw int) store.Entry {
	return store.Entry{TS: at(0), ID: "T-1", Event: "tokens", Data: map[string]any{
		"phase": string(p), "input": float64(in), "output": float64(out), "cache_read": float64(cr), "cache_write": float64(cw)}}
}

// Una feature light, en minutos desde t0:
//
//	0   start → spec (spec-author trabaja)
//	30  spec-author READY → abre gate spec
//	50  reject (humano esperó 20) → spec-author otra vez
//	70  READY → gate spec
//	80  approve (esperó 10) → implementing
//	100 NEEDS_DECISION → gate decision
//	115 approve decision (esperó 15)
//	180 DONE → quality
//	200 reviewer APPROVED
//	210 security REJECTED → implementing, ronda 1
//	240 block
//	300 unblock (60 bloqueada)
//	330 DONE → quality
//	350 dos APPROVED → documenting
//	360 DONE → walkthrough (gate walkthrough)
//	400 approve (esperó 40) → in_review
func featureLog() []store.Entry {
	return []store.Entry{
		e(0, "start", flow.Backlog, flow.Spec),
		e(30, "report", flow.Spec, flow.Spec, verdict("spec-author", "READY"), gateOpened("spec")),
		e(50, "reject", flow.Spec, flow.Spec, gate("spec")),
		e(70, "report", flow.Spec, flow.Spec, verdict("spec-author", "READY"), gateOpened("spec")),
		e(80, "approve", flow.Spec, flow.Implementing, gate("spec")),
		e(100, "report", flow.Implementing, flow.Implementing, verdict("implementer", "NEEDS_DECISION"), gateOpened("decision")),
		e(115, "approve", flow.Implementing, flow.Implementing, gate("decision")),
		e(180, "report", flow.Implementing, flow.Quality, verdict("implementer", "DONE")),
		e(200, "report", flow.Quality, flow.Quality, verdict("reviewer", "APPROVED")),
		e(210, "report", flow.Quality, flow.Implementing, verdict("security-auditor", "REJECTED"), round(1)),
		e(240, "block", flow.Implementing, flow.Blocked),
		e(300, "unblock", flow.Blocked, flow.Implementing),
		e(330, "report", flow.Implementing, flow.Quality, verdict("implementer", "DONE"), round(1)),
		e(345, "report", flow.Quality, flow.Quality, verdict("reviewer", "APPROVED"), round(1)),
		e(350, "report", flow.Quality, flow.Documenting, verdict("security-auditor", "APPROVED"), round(1)),
		e(360, "report", flow.Documenting, flow.Walkthrough, verdict("documenter", "DONE"), gateOpened("walkthrough")),
		e(400, "approve", flow.Walkthrough, flow.InReview, gate("walkthrough")),
		tokens(flow.Spec, 1000, 200, 5000, 300),
		tokens(flow.Implementing, 3000, 900, 40000, 1200),
		tokens(flow.Implementing, 500, 100, 2000, 0),
	}
}

func TestComputeFeature(t *testing.T) {
	st := Compute("T-1", featureLog(), at(430))
	byPhase := map[flow.Phase]PhaseStats{}
	for _, p := range st.Phases {
		byPhase[p.Phase] = p
	}
	min := func(d time.Duration) int { return int(d / time.Minute) }
	cases := []struct {
		phase                 flow.Phase
		agent, human, blocked int
	}{
		{flow.Spec, 50, 30, 0},          // 0-80: gates 30-50 y 70-80
		{flow.Implementing, 145, 15, 0}, // 80-180 (gate 100-115) + 210-240 + 300-330
		{flow.Quality, 50, 0, 0},        // 180-210 + 330-350
		{flow.Documenting, 10, 0, 0},    // 350-360
		{flow.Walkthrough, 0, 40, 0},    // 360-400, todo gate
		{flow.InReview, 0, 30, 0},       // 400-430 esperando merge (abierta)
		{flow.Blocked, 0, 0, 60},
	}
	for _, c := range cases {
		p := byPhase[c.phase]
		if min(p.Agent) != c.agent || min(p.Human) != c.human || min(p.Blocked) != c.blocked {
			t.Errorf("%s: agente %d · humano %d · bloqueada %d, want %d · %d · %d", c.phase, min(p.Agent), min(p.Human), min(p.Blocked), c.agent, c.human, c.blocked)
		}
	}
	if min(st.Agent) != 255 || min(st.Human) != 115 || min(st.Blocked) != 60 || min(st.Total) != 430 {
		t.Errorf("totales: agente %d humano %d bloqueada %d total %d", min(st.Agent), min(st.Human), min(st.Blocked), min(st.Total))
	}
	if st.Rejections != 1 || st.Rounds != 1 || st.Decisions != 1 || st.Phase != flow.InReview {
		t.Errorf("iteraciones: %+v", st)
	}
	if !st.TokensAvailable || st.Tokens.Total() != 54200 || byPhase[flow.Implementing].Tokens.Output != 1000 {
		t.Errorf("tokens: %+v · impl %+v", st.Tokens, byPhase[flow.Implementing].Tokens)
	}
}

func TestQualityFrictionAndModels(t *testing.T) {
	log := featureLog()
	log[len(log)-1].Data["model"] = "claude-haiku-4-5" // las otras dos entradas son previas: sin modelo
	log = append(log,
		e(410, "reject", flow.Walkthrough, flow.Implementing, gate("walkthrough")),
		store.Entry{TS: at(20), ID: "T-1", Event: "refused", Data: map[string]any{"code": "gate_pending"}},
		store.Entry{TS: at(21), ID: "T-1", Event: "guard", Data: map[string]any{"rule": "frozen_test"}},
		store.Entry{TS: at(22), ID: "T-1", Event: "guard", Data: map[string]any{"rule": "force_push"}},
		store.Entry{TS: at(500), ID: "T-9", Event: "start", From: flow.Backlog, To: flow.Implementing, Data: map[string]any{"lane": "hotfix", "fixes": "T-1"}},
		store.Entry{TS: at(501), ID: "T-8", Event: "start", From: flow.Backlog, To: flow.Implementing, Data: map[string]any{"lane": "hotfix", "fixes": "OTRA-1"}},
	)
	st := Compute("T-1", log, at(430))
	if st.Rejections != 2 || st.RejectionsByGate["spec"] != 1 || st.RejectionsByGate["walkthrough"] != 1 {
		t.Errorf("rechazos por gate: %d %v", st.Rejections, st.RejectionsByGate)
	}
	if st.Refused != 1 || st.Guarded != 2 {
		t.Errorf("fricción: refused %d guarded %d", st.Refused, st.Guarded)
	}
	if len(st.Hotfixes) != 1 || st.Hotfixes[0] != "T-9" {
		t.Errorf("hotfixes: %v", st.Hotfixes)
	}
	if st.Models["claude-haiku-4-5"].Total() != 2600 || st.Models[""].Total() != 51600 || st.Tokens.Total() != 54200 {
		t.Errorf("tokens por modelo: %+v", st.Models)
	}
	if st.Phase != flow.Implementing {
		t.Errorf("refused y guard no mueven la fase: %s", st.Phase)
	}
}

func TestNoTokens(t *testing.T) {
	log := featureLog()[:5]
	st := Compute("T-1", log, at(100))
	if st.TokensAvailable {
		t.Error("sin entradas de tokens no deben reportarse disponibles")
	}
}

func TestOtherTasksIgnored(t *testing.T) {
	log := append(featureLog(), store.Entry{TS: at(10), ID: "OTRA-1", Event: "start", From: flow.Backlog, To: flow.Spec})
	if st := Compute("T-1", log, at(430)); min2(st.Total) != 430 {
		t.Errorf("total %v", st.Total)
	}
}

func min2(d time.Duration) int { return int(d / time.Minute) }

func TestUsageTotal(t *testing.T) {
	u := Usage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}
	if u.Total() != 10 {
		t.Error("total")
	}
	if Human(184_300) != "184k" || Human(1_250_000) != "1.2M" || Human(900) != "900" {
		t.Errorf("formato: %s %s %s", Human(184_300), Human(1_250_000), Human(900))
	}
}

func TestTokensNewVersusCache(t *testing.T) {
	// Cifras de la piloto: casi todo es caché leída.
	u := Usage{Input: 100, Output: 27_000, CacheRead: 2_900_000, CacheWrite: 118_000}
	if u.New() != 145_100 {
		t.Errorf("nuevos: %d", u.New())
	}
	if s := Tokens(u.New(), u.CacheRead); s != "145k nuevos · 2.9M caché" {
		t.Errorf("formato: %q", s)
	}
	if s := Tokens(900, 0); s != "900 nuevos" {
		t.Errorf("sin caché: %q", s)
	}
}

// La caché leída se entiende con las llamadas: cada una vuelve a leer el
// contexto. Los datos viejos, sin llamadas, se muestran como antes.
func TestSummaryAndDetailExplainCache(t *testing.T) {
	var impl Usage
	impl.Add(Usage{Output: 4_000, CacheRead: 60_000, CacheWrite: 2_000, Calls: 1, MaxContext: 62_000})
	impl.Add(Usage{Output: 5_000, CacheRead: 130_000, CacheWrite: 3_000, Calls: 1, MaxContext: 140_000})
	if impl.Calls != 2 || impl.MaxContext != 140_000 {
		t.Fatalf("Add suma llamadas y se queda con el contexto mayor: %+v", impl)
	}
	if s := Detail(impl); s != "14k nuevos · 2 llamadas de hasta 140k" {
		t.Errorf("agente: %q", s)
	}
	if s := Summary(impl); s != "14k nuevos · 190k releídos de caché en 2 llamadas" {
		t.Errorf("total: %q", s)
	}
	old := Usage{Output: 1_000, CacheRead: 50_000}
	if Detail(old) != "1k nuevos · 50k caché" || Summary(old) != "1k nuevos · 50k caché" {
		t.Errorf("sin llamadas: %q %q", Detail(old), Summary(old))
	}
}

// Una tarea light como la piloto: el implementer reporta DONE y la tarea pasa
// a quality antes de que termine su hook.
func pilotLog() []store.Entry {
	return []store.Entry{
		e(0, "start", flow.Backlog, flow.Spec),
		e(1, "report", flow.Spec, flow.Spec, verdict("spec-author", "READY"), gateOpened("spec")),
		e(2, "approve", flow.Spec, flow.Implementing, gate("spec")),
		e(9, "report", flow.Implementing, flow.Quality, verdict("implementer", "DONE")),
		e(10, "report", flow.Quality, flow.Quality, verdict("reviewer", "APPROVED")),
		e(11, "report", flow.Quality, flow.Documenting, verdict("security-auditor", "APPROVED")),
	}
}

func sample(min int, model string, out int) Sample {
	return Sample{TS: at(min), Model: model, Usage: Usage{Output: int64(out)}}
}

func TestAllot(t *testing.T) {
	log := pilotLog()
	// El implementer trabajó de 2 a 9 y su última respuesta llega después del
	// reporte: todo va a implementing, no a quality.
	got := Allot(log, "T-1", "implementer", []Sample{sample(3, "opus", 100), sample(8, "opus", 50), sample(9, "opus", 5)})
	if len(got) != 1 || got[0].Phase != flow.Implementing || got[0].Output != 155 {
		t.Errorf("implementer: %+v", got)
	}
	// Un agente que no reportó (p. ej. Explore) va a la fase en que empezó.
	got = Allot(log, "T-1", "Explore", []Sample{sample(4, "haiku", 10), sample(12, "haiku", 10)})
	if len(got) != 1 || got[0].Phase != flow.Implementing || got[0].Output != 20 {
		t.Errorf("agente sin reporte: %+v", got)
	}
	// La sesión principal se reparte por la hora de cada respuesta; lo previo
	// al start es de la primera fase.
	got = Allot(log, "T-1", "", []Sample{sample(-1, "opus", 1), sample(1, "opus", 2), sample(2, "opus", 4),
		sample(5, "sonnet", 8), sample(10, "opus", 16), sample(30, "opus", 32)})
	want := []Share{
		{flow.Documenting, "opus", Usage{Output: 32}},
		{flow.Implementing, "opus", Usage{Output: 4}},
		{flow.Implementing, "sonnet", Usage{Output: 8}},
		{flow.Quality, "opus", Usage{Output: 16}},
		{flow.Spec, "opus", Usage{Output: 3}},
	}
	if len(got) != len(want) {
		t.Fatalf("sesión principal: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sesión principal [%d]: %+v, want %+v", i, got[i], want[i])
		}
	}
	if Allot(log, "T-1", "", nil) != nil || PhaseAt(log, "OTRA-1", at(5)) != "" {
		t.Error("sin muestras o sin historial no hay reparto")
	}
}

func TestComputeByAgent(t *testing.T) {
	tok := func(agent string, p flow.Phase, out, cr int) store.Entry {
		return store.Entry{TS: at(20), ID: "T-1", Event: "tokens", Agent: agent, Data: map[string]any{
			"phase": string(p), "output": float64(out), "cache_read": float64(cr)}}
	}
	log := append(pilotLog(), tok("implementer", flow.Implementing, 100, 1000), tok(MainSession, flow.Implementing, 10, 500),
		tok(MainSession, flow.Quality, 5, 200), tok("reviewer", flow.Quality, 40, 0), tok("", flow.Spec, 7, 0))
	st := Compute("T-1", log, at(20))
	if st.Agents["implementer"].Output != 100 || st.Agents[MainSession].Output != 15 || st.Agents[""].Output != 7 || len(st.Agents) != 4 {
		t.Errorf("por agente: %+v", st.Agents)
	}
	for _, p := range st.Phases {
		if p.Phase == flow.Implementing && (len(p.Agents) != 2 || p.Agents[MainSession].CacheRead != 500) {
			t.Errorf("implementing por agente: %+v", p.Agents)
		}
	}
}

func TestClosedOutsideNotCountedAsWork(t *testing.T) {
	reason := func(r string) func(*store.Entry) {
		return func(en *store.Entry) { en.Data = map[string]any{"reason": r} }
	}
	st := Compute("T-1", []store.Entry{
		e(0, "start", flow.Backlog, flow.Implementing),
		e(60, "report", flow.Implementing, flow.Implementing, verdict("implementer", "NEEDS_DECISION"), gateOpened("decision")),
		// días después, otra máquina la terminó
		e(5000, "closed_outside", flow.Implementing, flow.Done, reason("pr")),
	}, at(6000))
	if st.ClosedOutside != "pr" || st.Phase != flow.Done {
		t.Errorf("motivo/fase: %+v", st)
	}
	if st.Agent != 60*time.Minute || st.Human != 0 || st.Blocked != 0 {
		t.Errorf("el tramo previo al cierre no es trabajo: agent=%v human=%v blocked=%v", st.Agent, st.Human, st.Blocked)
	}
}

func coverage(min int, id string, data map[string]any) store.Entry {
	return store.Entry{TS: at(min), ID: id, Event: "review_coverage", Agent: "reviewer", Data: data}
}

// R14: Review es la última cobertura de la tarea; sin evento, nil.
func TestComputeReviewCoverage(t *testing.T) {
	log := append(featureLog(),
		coverage(201, "T-1", map[string]any{"measured": true, "total": float64(5), "read": float64(3), "round": float64(0), "unread": []any{"a.go", "b.go"}}),
		coverage(346, "T-1", map[string]any{"measured": true, "total": float64(5), "read": float64(4), "round": float64(1), "unread": []any{"b.go"}, "red_outside": []any{"gone.go"}}),
		coverage(347, "OTRA-1", map[string]any{"measured": true, "total": float64(9), "read": float64(9)}),
	)
	st := Compute("T-1", log, at(430))
	if st.Review == nil {
		t.Fatal("Review es la última cobertura")
	}
	r := st.Review
	if !r.Measured || r.Total != 5 || r.Read != 4 || len(r.Unread) != 1 || r.Unread[0] != "b.go" || len(r.RedOutside) != 1 {
		t.Errorf("la última, no la primera ni la de otra tarea: %+v", r)
	}
	// El evento no altera el tiempo por fase ni la fricción.
	base := Compute("T-1", featureLog(), at(430))
	st.Review = nil
	if st.Total != base.Total || st.Agent != base.Agent || st.Human != base.Human || len(st.Phases) != len(base.Phases) || st.Refused != base.Refused {
		t.Errorf("review_coverage no cuenta como trabajo: %+v vs %+v", st, base)
	}
	if base.Review != nil {
		t.Errorf("sin evento Review es nil: %+v", base.Review)
	}
}
