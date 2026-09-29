package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/agents"
	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
)

func TestRenderWatch(t *testing.T) {
	now := time.Date(2026, 9, 28, 17, 42, 0, 0, time.Local)
	at := func(h, m int) time.Time { return time.Date(2026, 9, 28, h, m, 0, 0, time.Local) }
	phaseSince := at(17, 40)
	v := engine.View{ID: "API-7", Title: "Alta de clientes", Lane: flow.Light, Phase: flow.Quality, Round: 1, Since: &phaseSince,
		Phases:   flow.DefaultLanes()[flow.Light],
		Upcoming: flow.Step{Phase: flow.Documenting, Agents: []string{"documenter"}}, Reported: []string{"reviewer"},
		Next: output.Next{Action: output.ActionSpawn, Agents: []output.AgentCall{{Agent: "security-auditor"}}}}
	d := watchData{Repo: "api", Active: &v,
		Stats: metrics.TaskStats{Total: 10 * time.Minute, Agent: 8 * time.Minute, Human: 2 * time.Minute, Guarded: 1, TokensAvailable: true,
			Tokens: metrics.Usage{Output: 40_000, CacheWrite: 60_000, CacheRead: 1_500_000},
			Phases: []metrics.PhaseStats{{Phase: flow.Spec, Human: 2 * time.Minute}, {Phase: flow.Implementing, Agent: 6 * time.Minute}},
			Agents: map[string]metrics.Usage{metrics.MainSession: {Output: 10_000, CacheRead: 900_000}, "implementer": {Output: 30_000, CacheWrite: 60_000}}},
		Events: []store.Entry{
			{TS: at(17, 32), Event: "start", From: flow.Backlog, To: flow.Spec, Data: map[string]any{"lane": "light"}},
			{TS: at(17, 35), Event: "guard", Data: map[string]any{"rule": "frozen_test"}},
			{TS: at(17, 40), Event: "report", Agent: "implementer", Verdict: "DONE", From: flow.Implementing, To: flow.Quality},
			{TS: at(17, 41), Event: "report", Agent: "reviewer", Verdict: "APPROVED", From: flow.Quality, To: flow.Quality},
		},
		Others: []engine.View{{ID: "API-9", Phase: flow.Spec, Gate: "spec"}},
	}
	out := renderWatch(bannerPlain, "dev", d, now, 0)
	for _, want := range []string{
		"bflow by innobytes.tech · dev · api · 17:42:00",
		"API-7 · carril light · ronda 1",
		"spec > implementing > [quality] > documenting > walkthrough > in_review",
		"AHORA    AGENTE  security-auditor audita la seguridad del diff · ya reportó reviewer · hace 2m",
		"DESPUÉS  AGENTE  documenter documenta el cambio y escribe el walkthrough",
		"10m · agentes 8m · tú 2m",
		"spec 2m · implementing 6m",
		"100k nuevos · 1.5M caché",
		"implementer        90k nuevos",
		"sesión principal   10k nuevos · 900k caché",
		"0 rechazo(s) del flujo · 1 bloqueo(s) de guard",
		"17:41 reviewer APPROVED\n  17:40 implementer DONE → quality\n  17:35 guard bloqueó: frozen_test\n  17:32 inicio en carril light → spec",
		"otras     API-9 · spec · decidir spec",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en:\n%s", want, out)
		}
	}
	if strings.Index(out, "implementer        90k") > strings.Index(out, "sesión principal") {
		t.Error("los agentes van de mayor a menor gasto nuevo")
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("sin color no hay secuencias de escape")
	}

	// Gate: le toca a la persona, en amarillo, desde que se abrió la gate.
	v.Gate, v.GateSince, v.Next = "walkthrough", at(17, 30), output.Next{Action: output.ActionAsk}
	v.Phase, v.Upcoming = flow.Walkthrough, flow.Step{Phase: flow.InReview, Merge: true}
	out = renderWatch(bannerColor, "dev", d, now, 0)
	for _, want := range []string{
		"\x1b[1;33mTÚ    \x1b[0m  \x1b[1;33mrecorrer el cambio y aprobar el PR\x1b[0m\x1b[2m · esperando hace 12m\x1b[0m",
		"\x1b[1;33mTÚ    \x1b[0m  revisar y hacer merge del PR",
		"\x1b[32mspec\x1b[0m", // lo hecho en verde
		"\x1b[1;33mwalkthrough\x1b[0m",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("gate: falta %q en:\n%s", want, out)
		}
	}
	v.Gate, v.Blocked, v.Phase, v.Upcoming = "", "falta acceso a la BD", flow.Blocked, flow.Step{}
	if out := renderWatch(bannerOff, "dev", d, now, 0); !strings.Contains(out, "AHORA    TÚ      desbloquear la tarea: falta acceso a la BD") || strings.Contains(out, "DESPUÉS") {
		t.Errorf("bloqueada:\n%s", out)
	}

	// Sin tarea activa: lo dice y lista las demás.
	d.Active = nil
	if out := renderWatch(bannerOff, "dev", d, now, 0); !strings.Contains(out, "Ninguna tarea en curso") || !strings.Contains(out, "API-9") {
		t.Errorf("sin tarea:\n%s", out)
	}
}

func TestProgressWraps(t *testing.T) {
	v := engine.View{Phase: flow.Implementing, Phases: flow.DefaultLanes()[flow.Full]}
	lines := progress(bannerPlain, v, 60)
	if len(lines) != 2 || !strings.Contains(lines[0], "[implementing]") || strings.Contains(strings.Join(lines, ""), "done") {
		t.Errorf("%q", lines)
	}
	for _, l := range lines {
		if len([]rune(l)) > 62 {
			t.Errorf("línea de %d columnas: %q", len([]rune(l)), l)
		}
	}
}

func TestEventTimeOtherDay(t *testing.T) {
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.Local)
	if got := eventTime(time.Date(2026, 9, 28, 17, 44, 0, 0, time.Local), now); got != "28/09 17:44" {
		t.Errorf("%q", got)
	}
	if got := describeEvent(store.Entry{Event: "reject", Gate: "spec", Note: "falta\nel caso de 409", From: flow.Spec, To: flow.Spec}); got != "rechazada gate spec: falta el caso de 409" {
		t.Errorf("%q", got)
	}
}

func TestRenderWatchFitsRows(t *testing.T) {
	now := time.Date(2026, 9, 28, 17, 42, 0, 0, time.Local)
	since := now.Add(-time.Minute)
	v := engine.View{ID: "API-7", Phase: flow.Implementing, Since: &since, Next: output.Next{Action: output.ActionSpawn, Agents: []output.AgentCall{{Agent: "implementer"}}}}
	d := watchData{Repo: "api", Active: &v}
	for i := range 12 {
		d.Events = append(d.Events, store.Entry{TS: now.Add(time.Duration(i-12) * time.Minute), Event: "guard", Data: map[string]any{"rule": fmt.Sprint(i)}})
	}
	full := renderWatch(bannerPlain, "dev", d, now, 0)
	if strings.Count(full, "guard bloqueó") != watchEvents {
		t.Fatalf("sin límite, %d eventos:\n%s", watchEvents, full)
	}
	short := renderWatch(bannerPlain, "dev", d, now, 14)
	if n := strings.Count(short, "\n") + 1; n > 14 {
		t.Errorf("%d líneas en una terminal de 14:\n%s", n, short)
	}
	if !strings.Contains(short, "AHORA") || !strings.Contains(short, "guard bloqueó: 11") || strings.Contains(short, "guard bloqueó: 4") {
		t.Errorf("se quitan los eventos más viejos, nunca AHORA:\n%s", short)
	}
}

func TestStartupCheck(t *testing.T) {
	st, d := startupCheck([]agents.ContextSource{{Label: "CLAUDE.md", Bytes: 1500}, {Label: "MEMORY.md", Bytes: 3000}})
	if st != "ok" || !strings.Contains(d, "≈1k tokens") || !strings.Contains(d, "MEMORY.md ≈1k, CLAUDE.md ≈500") {
		t.Errorf("chico: %s %s", st, d)
	}
	st, d = startupCheck([]agents.ContextSource{{Label: "CLAUDE.md", Bytes: 1500}, {Label: "MEMORY.md", Bytes: 19048}})
	if st != "warn" || !strings.Contains(d, "MEMORY.md ≈6k tokens, más de 4k en un solo archivo") {
		t.Errorf("MEMORY.md de ms-sys: %s %s", st, d)
	}
	st, d = startupCheck([]agents.ContextSource{{Label: "a", Bytes: 11000}, {Label: "b", Bytes: 11000}, {Label: "c", Bytes: 11000}})
	if st != "warn" || !strings.Contains(d, "el total pasa de 10k") {
		t.Errorf("total: %s %s", st, d)
	}
	if st, d := startupCheck([]agents.ContextSource{{Label: "MEMORY.md", Bytes: 900, Note: "se corta"}}); st != "warn" || !strings.Contains(d, "MEMORY.md se corta") {
		t.Errorf("nota: %s %s", st, d)
	}
}
