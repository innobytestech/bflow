package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
		Upcoming: "documenting", Reported: []string{"reviewer"},
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
		"bflow by innobytes.tech",
		"API-7 · light · ronda 1",
		"quality · trabajando: security-auditor · hace 2m · listos: reviewer",
		"sigue     documenting",
		"10m · agente 8m · humano 2m",
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

	// Gate: espera al humano desde que se abrió, resaltado en color.
	v.Gate, v.GateSince, v.Next = "walkthrough", at(17, 30), output.Next{Action: output.ActionAsk}
	v.Phase = flow.Walkthrough
	if out := renderWatch(bannerColor, "dev", d, now, 0); !strings.Contains(out, "\x1b[1;33mwalkthrough · esperando tu decisión: gate walkthrough · hace 12m\x1b[0m") {
		t.Errorf("gate:\n%s", out)
	}
	v.Gate, v.Blocked, v.Phase = "", "falta acceso a la BD", flow.Blocked
	if out := renderWatch(bannerOff, "dev", d, now, 0); !strings.Contains(out, "ahora     bloqueada: falta acceso a la BD") || strings.Contains(out, "innobytes") {
		t.Errorf("bloqueada y sin banner:\n%s", out)
	}

	// Sin tarea activa: lo dice y lista las demás.
	d.Active = nil
	if out := renderWatch(bannerOff, "dev", d, now, 0); !strings.Contains(out, "ninguna en curso") || !strings.Contains(out, "API-9") {
		t.Errorf("sin tarea:\n%s", out)
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
	if !strings.Contains(full, "████") || strings.Count(full, "guard bloqueó") != watchEvents {
		t.Fatalf("sin límite: banner completo y %d eventos:\n%s", watchEvents, full)
	}
	short := renderWatch(bannerPlain, "dev", d, now, 14)
	if n := strings.Count(short, "\n") + 1; n > 14 {
		t.Errorf("%d líneas en una terminal de 14:\n%s", n, short)
	}
	if strings.Contains(short, "████") || !strings.Contains(short, "bflow by innobytes.tech · dev") {
		t.Errorf("sin espacio el banner pasa a una línea:\n%s", short)
	}
	if !strings.Contains(short, "guard bloqueó: 11") || strings.Contains(short, "guard bloqueó: 4") {
		t.Errorf("se quitan los eventos más viejos:\n%s", short)
	}
}
