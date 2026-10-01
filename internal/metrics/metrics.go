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
	"innobytes.tech/bflow/internal/review"
	"innobytes.tech/bflow/internal/store"
)

// Usage son tokens de un modelo.
type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	// Calls son las respuestas del modelo. Cada una vuelve a leer el contexto
	// entero, así que CacheRead ≈ Calls × contexto promedio: los millones de
	// caché no son un contexto de millones.
	Calls int64 `json:"calls,omitempty"`
	// MaxContext es el contexto más grande de una sola llamada (entrada y caché).
	MaxContext int64 `json:"max_context,omitempty"`
}

// Total suma todo lo que se procesó.
func (u Usage) Total() int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

// New es lo que el modelo procesó por primera vez: entrada sin caché, salida y
// caché escrita. La caché leída cuesta ~10% de la entrada y va aparte.
func (u Usage) New() int64 { return u.Input + u.Output + u.CacheWrite }

// Sample es el uso de una respuesta del modelo y cuándo ocurrió.
type Sample struct {
	TS    time.Time
	Model string
	Msg   string // id de mensaje: claude e.Message.ID; opencode "opencode:"+l.ID (la misma llave de cur.Seen)
	Usage
}

// TokenSource es un archivo de uso y de quién es ("" = sesión principal).
type TokenSource struct{ Path, Agent string }

// MainSession es el nombre con el que se registran los tokens de la sesión
// principal, para separarlos de los de los agentes.
const MainSession = "main"

// Add acumula.
func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheWrite += o.CacheWrite
	u.Calls += o.Calls
	u.MaxContext = max(u.MaxContext, o.MaxContext)
}

// PhaseStats son los tiempos de una fase.
type PhaseStats struct {
	Phase   flow.Phase    `json:"phase"`
	Agent   time.Duration `json:"agent_ns"`
	Human   time.Duration `json:"human_ns"`
	Blocked time.Duration `json:"blocked_ns"`
	Tokens  Usage         `json:"tokens"`
	// Tokens por agente (MainSession = sesión principal; "" = registrados
	// antes de separarlos).
	Agents map[string]Usage `json:"agents,omitempty"`
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
	// ClosedOutside es el motivo ("tracker" | "pr") si la tarea se cerró
	// porque se terminó fuera de esta copia.
	ClosedOutside string `json:"closed_outside,omitempty"`

	// Calidad: rechazos humanos por gate y hotfixes que la corrigen (--fixes).
	RejectionsByGate map[string]int `json:"rejections_by_gate,omitempty"`
	Hotfixes         []string       `json:"hotfixes,omitempty"`
	// Fricción: pedidos que el flujo rechazó (exit 2), acciones que bloqueó
	// guard y veces que un agente terminó sin reportar.
	Refused int `json:"refused"`
	Guarded int `json:"guarded"`
	Nudged  int `json:"nudged"`
	// Tokens por modelo ("" = registrados antes de guardar el modelo).
	Models map[string]Usage `json:"models,omitempty"`
	// Tokens por agente, como en PhaseStats.
	Agents map[string]Usage `json:"agents,omitempty"`
	// Review es la última cobertura del reviewer (evento review_coverage).
	Review *review.Coverage `json:"review,omitempty"`
}

func addTo(m *map[string]Usage, k string, u Usage) {
	if *m == nil {
		*m = map[string]Usage{}
	}
	x := (*m)[k]
	x.Add(u)
	(*m)[k] = x
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
			ps := get(flow.Phase(fmt.Sprint(e.Data["phase"])))
			ps.Tokens.Add(u)
			addTo(&ps.Agents, e.Agent, u)
			st.Tokens.Add(u)
			st.TokensAvailable = true
			model, _ := e.Data["model"].(string)
			addTo(&st.Models, model, u)
			addTo(&st.Agents, e.Agent, u)
			continue
		case "refused":
			st.Refused++
			continue
		case "guard":
			st.Guarded++
			continue
		case "nudge":
			st.Nudged++
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
		if e.Event == string(flow.EvClosedOutside) {
			// Terminada fuera de esta copia: el tramo previo no fue trabajo
			// de agente ni espera humana medible.
			st.ClosedOutside, _ = e.Data["reason"].(string)
			if st.ClosedOutside == "" {
				st.ClosedOutside = "unknown"
			}
			cur, last, started = e.To, e.TS, true
			continue
		}
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

// PhaseAt dice en qué fase estaba la tarea en el instante ts, según las
// transiciones del log. Antes de la primera transición devuelve la primera
// fase (lo que se habló justo antes de empezar ya es parte de la tarea); sin
// historial, "".
func PhaseAt(entries []store.Entry, id string, ts time.Time) flow.Phase {
	var (
		cur, first flow.Phase
		at         time.Time
	)
	for _, e := range entries {
		if e.ID != id || e.To == "" {
			continue
		}
		if first == "" {
			first = e.To
		}
		if !e.TS.After(ts) && !e.TS.Before(at) {
			cur, at = e.To, e.TS
		}
	}
	if cur == "" {
		return first
	}
	return cur
}

// Share es la parte de un transcript que va a una fase y un modelo.
type Share struct {
	Phase flow.Phase
	Model string
	Usage
}

// Allot reparte las respuestas de un transcript entre las fases de la tarea.
// Las de un agente van completas a la fase de su último reporte: el hook corre
// cuando el agente termina, y para entonces su reporte ya movió la tarea. Sin
// reporte, a la fase en que empezó. Las de la sesión principal (agent vacío),
// cada una a la fase en que ocurrió.
func Allot(entries []store.Entry, id, agent string, samples []Sample) []Share {
	if len(samples) == 0 {
		return nil
	}
	phases := Phases(entries, id, agent, samples)
	type key struct {
		p flow.Phase
		m string
	}
	sum := map[key]Usage{}
	for i, s := range samples {
		k := key{phases[i], s.Model}
		u := sum[k]
		u.Add(s.Usage)
		sum[k] = u
	}
	out := make([]Share, 0, len(sum))
	for k, u := range sum {
		out = append(out, Share{Phase: k.p, Model: k.m, Usage: u})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Phase != out[j].Phase {
			return out[i].Phase < out[j].Phase
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func usageOf(d map[string]any) Usage {
	return Usage{Input: num(d["input"]), Output: num(d["output"]), CacheRead: num(d["cache_read"]), CacheWrite: num(d["cache_write"]),
		Calls: num(d["calls"]), MaxContext: num(d["max_context"])}
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

// Summary es el total de una tarea: "954k nuevos · 28.6M releídos de caché en
// 410 llamadas". Sin llamadas registradas (datos viejos), como Tokens.
func Summary(u Usage) string {
	if u.Calls == 0 {
		return Tokens(u.New(), u.CacheRead)
	}
	s := Human(u.New()) + " nuevos"
	if u.CacheRead > 0 {
		s += " · " + Human(u.CacheRead) + " releídos de caché"
	}
	return s + fmt.Sprintf(" en %d llamadas", u.Calls)
}

// Detail es el gasto de un agente: "508k nuevos · 160 llamadas de hasta 140k".
// Las llamadas y el contexto máximo explican su caché leída. Sin llamadas
// registradas (datos viejos), como Tokens.
func Detail(u Usage) string {
	if u.Calls == 0 {
		return Tokens(u.New(), u.CacheRead)
	}
	return fmt.Sprintf("%s nuevos · %d llamadas de hasta %s", Human(u.New()), u.Calls, Human(u.MaxContext))
}

// Tokens separa lo nuevo de lo leído de caché: "145k nuevos · 2.9M caché".
// Un total que mezcla ambos exagera el costo.
func Tokens(fresh, cached int64) string {
	s := Human(fresh) + " nuevos"
	if cached > 0 {
		s += " · " + Human(cached) + " caché"
	}
	return s
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
