// Package metrics calcula, a partir de log.jsonl, cuánto tiempo pasó cada
// tarea en cada fase (trabajando el agente, esperando al humano o bloqueada),
// cuántas iteraciones hubo y cuántos tokens se gastaron.
package metrics

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// Usage son tokens de un modelo.
type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Total suma todo lo que se procesó.
func (u Usage) Total() int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

// Add acumula.
func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheWrite += o.CacheWrite
}

// PhaseStats son los tiempos de una fase.
type PhaseStats struct {
	Phase   flow.Phase    `json:"phase"`
	Agent   time.Duration `json:"agent_ns"`
	Human   time.Duration `json:"human_ns"`
	Blocked time.Duration `json:"blocked_ns"`
	Tokens  Usage         `json:"tokens"`
}

// TaskStats son las métricas de una tarea.
type TaskStats struct {
	ID              string        `json:"id"`
	Phase           flow.Phase    `json:"phase"`
	Phases          []PhaseStats  `json:"phases"`
	Agent           time.Duration `json:"agent_ns"`
	Human           time.Duration `json:"human_ns"`
	Blocked         time.Duration `json:"blocked_ns"`
	Total           time.Duration `json:"total_ns"`
	Rejections      int           `json:"rejections"`
	Rounds          int           `json:"rounds"`
	Decisions       int           `json:"decisions"`
	Splits          int           `json:"splits"`
	Tokens          Usage         `json:"tokens"`
	TokensAvailable bool          `json:"tokens_available"`

	// Calidad: rechazos humanos por gate y hotfixes que la corrigen (--fixes).
	RejectionsByGate map[string]int `json:"rejections_by_gate,omitempty"`
	Hotfixes         []string       `json:"hotfixes,omitempty"`
	// Fricción: pedidos que el flujo rechazó (exit 2) y acciones que bloqueó guard.
	Refused int `json:"refused"`
	Guarded int `json:"guarded"`
	// Tokens por modelo ("" = registrados antes de guardar el modelo).
	Models map[string]Usage `json:"models,omitempty"`
}

// Compute calcula las métricas de una tarea. now cierra el último intervalo
// si la tarea sigue abierta.
func Compute(id string, entries []store.Entry, now time.Time) TaskStats {
	st := TaskStats{ID: id}
	var evs []store.Entry
	per := map[flow.Phase]*PhaseStats{}
	get := func(p flow.Phase) *PhaseStats {
		if per[p] == nil {
			per[p] = &PhaseStats{Phase: p}
		}
		return per[p]
	}
	for _, e := range entries {
		if e.Event == "start" && e.ID != id && e.Data["fixes"] == id {
			st.Hotfixes = append(st.Hotfixes, e.ID)
		}
		if e.ID != id {
			continue
		}
		switch e.Event {
		case "tokens":
			u := usageOf(e.Data)
			p := flow.Phase(fmt.Sprint(e.Data["phase"]))
			get(p).Tokens.Add(u)
			st.Tokens.Add(u)
			st.TokensAvailable = true
			model, _ := e.Data["model"].(string)
			if st.Models == nil {
				st.Models = map[string]Usage{}
			}
			m := st.Models[model]
			m.Add(u)
			st.Models[model] = m
			continue
		case "refused":
			st.Refused++
			continue
		case "guard":
			st.Guarded++
			continue
		}
		if e.To != "" {
			evs = append(evs, e)
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].TS.Before(evs[j].TS) })

	var (
		cur       flow.Phase
		last      time.Time
		gateOpen  bool
		savedGate bool // gate abierto antes de bloquear
		started   bool
	)
	add := func(to time.Time) {
		if !started || cur == flow.Done || cur == flow.Backlog || !to.After(last) {
			return
		}
		d := to.Sub(last)
		ps := get(cur)
		switch {
		case cur == flow.Blocked:
			ps.Blocked += d
			st.Blocked += d
		case gateOpen || cur == flow.InReview:
			ps.Human += d
			st.Human += d
		default:
			ps.Agent += d
			st.Agent += d
		}
	}
	for _, e := range evs {
		add(e.TS)
		opened := e.Data["gate_opened"] != nil
		switch e.Event {
		case "reject":
			st.Rejections++
			if e.Gate != "" {
				if st.RejectionsByGate == nil {
					st.RejectionsByGate = map[string]int{}
				}
				st.RejectionsByGate[e.Gate]++
			}
			// Rechazar el discovery lo deja abierto.
			gateOpen = opened || (e.From == flow.Discovery && e.To == flow.Discovery)
		case "approve":
			gateOpen = opened
		case "block":
			savedGate, gateOpen = gateOpen, false
		case "unblock":
			gateOpen = savedGate
		default:
			if opened {
				gateOpen = true
			} else if e.From != e.To {
				gateOpen = false
			}
		}
		switch e.Verdict {
		case string(flow.NeedsDecision):
			st.Decisions++
		case string(flow.Split):
			st.Splits++
		}
		if e.Round > st.Rounds {
			st.Rounds = e.Round
		}
		cur, last, started = e.To, e.TS, true
	}
	add(now)
	st.Phase = cur
	st.Total = st.Agent + st.Human + st.Blocked

	order := append(append([]flow.Phase{flow.Backlog}, flow.Order...), flow.Blocked)
	for _, p := range order {
		if ps, ok := per[p]; ok {
			st.Phases = append(st.Phases, *ps)
		}
	}
	for p, ps := range per { // fases desconocidas (tokens sin fase) al final
		if !slices.Contains(order, p) {
			st.Phases = append(st.Phases, *ps)
		}
	}
	return st
}

func usageOf(d map[string]any) Usage {
	return Usage{Input: num(d["input"]), Output: num(d["output"]), CacheRead: num(d["cache_read"]), CacheWrite: num(d["cache_write"])}
}

func num(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	}
	return 0
}

// Human formatea tokens: 900, 184k, 1.2M.
func Human(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

// Duration formatea una duración corta: 45m, 1h42m, 3d4h.
func Duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
}

// Cursor recuerda hasta dónde se leyó cada transcript y qué mensajes ya se
// contaron, para sumar solo lo nuevo en cada lectura.
type Cursor struct {
	Offsets map[string]int64 `json:"offsets"`
	Seen    map[string]Usage `json:"seen"` // id de mensaje → uso ya contado
}
