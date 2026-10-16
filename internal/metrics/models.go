package metrics

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// ModelSet es agente → modelo normalizado.
type ModelSet map[string]string

// NormalizeModel: minúsculas, sin prefijo proveedor/, y la familia opus, sonnet
// o haiku si el nombre la contiene; "" se muestra como "heredado".
func NormalizeModel(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if m == "" {
		return "heredado"
	}
	for _, f := range []string{"opus", "sonnet", "haiku"} {
		if strings.Contains(m, f) {
			return f
		}
	}
	return m
}

// modelsOfStart lee data.models de un start; ok=false si no es un mapa de strings.
func modelsOfStart(e store.Entry) (ModelSet, bool) {
	raw, ok := e.Data["models"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil, false
	}
	out := ModelSet{}
	for a, v := range raw {
		str, ok := v.(string)
		if !ok {
			return nil, false
		}
		out[a] = NormalizeModel(str)
	}
	return out, true
}

// TaskModels da los modelos de la tarea y su origen: "start" | "tokens" | "".
func TaskModels(id string, entries []store.Entry) (ModelSet, string) {
	var fromStart ModelSet
	type tally struct {
		n    map[string]int
		last map[string]int
	}
	toks := map[string]*tally{}
	for i, e := range entries {
		if e.ID != id {
			continue
		}
		switch e.Event {
		case "start":
			if ms, ok := modelsOfStart(e); ok {
				fromStart = ms
			}
		case "tokens":
			if e.Agent == "" || e.Agent == MainSession {
				continue
			}
			m, _ := e.Data["model"].(string)
			if strings.TrimSpace(m) == "" {
				continue
			}
			t := toks[e.Agent]
			if t == nil {
				t = &tally{n: map[string]int{}, last: map[string]int{}}
				toks[e.Agent] = t
			}
			m = NormalizeModel(m)
			t.n[m]++
			t.last[m] = i
		}
	}
	if len(toks) == 0 {
		if fromStart != nil {
			return fromStart, "start"
		}
		return nil, ""
	}
	out := ModelSet{}
	for a, m := range fromStart {
		out[a] = m
	}
	for a, t := range toks {
		best := ""
		for m, n := range t.n {
			if best == "" || n > t.n[best] || (n == t.n[best] && t.last[m] > t.last[best]) {
				best = m
			}
		}
		out[a] = best
	}
	if fromStart != nil {
		return out, "start+tokens"
	}
	return out, "tokens"
}

// Matches (R11): comparten al menos un agente y en los compartidos el modelo es igual.
func (a ModelSet) Matches(b ModelSet) bool {
	shared := false
	for ag, m := range a {
		if o, ok := b[ag]; ok {
			if o != m {
				return false
			}
			shared = true
		}
	}
	return shared
}

// String es "agente=modelo,..." ordenado por agente.
func (a ModelSet) String() string {
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + a[k]
	}
	return strings.Join(parts, ",")
}

// ModelGroup es un grupo de tareas con la misma configuración de modelos.
type ModelGroup struct {
	Models     ModelSet           `json:"models"`
	Tasks      []string           `json:"tasks"` // IDs, más reciente primero, máximo ModelWindow
	Rejections map[string]float64 `json:"rejections"`
	Rounds     float64            `json:"rounds"`
	Hotfixes   float64            `json:"hotfixes"`
}

// ModelChange es un agente que cambió de modelo.
type ModelChange struct {
	Agent string `json:"agent"`
	From  string `json:"from"` // anterior: al que volver
	To    string `json:"to"`   // vigente
}

// ModelVerdict compara la configuración vigente con la anterior.
type ModelVerdict struct {
	Verdict  string        `json:"verdict"` // "sube" | "sin_regresion" | "sin_veredicto" | "sin_datos"
	Current  ModelGroup    `json:"current"`
	Previous *ModelGroup   `json:"previous,omitempty"`
	Changes  []ModelChange `json:"changes,omitempty"`
	Rose     []string      `json:"rose,omitempty"` // "rondas 1.7 > 1.0", "rechazos spec 0.7 > 0.3"
}

// ModelWindow es cuántas tareas por grupo exige la comparación.
const ModelWindow = 3

// modelTask es una tarea elegible para la comparación.
type modelTask struct {
	id         string
	start      time.Time
	idx        int
	models     ModelSet
	rejections map[string]int
	rounds     int
	hotfixes   int
}

// CompareModels aplica R8-R14 sobre el log.
func CompareModels(entries []store.Entry, now time.Time) ModelVerdict {
	byID := map[string][]store.Entry{}
	var order []string
	fixes := map[string]int{}
	var cur ModelSet
	var curID string
	var curTS time.Time
	for _, e := range entries {
		if _, seen := byID[e.ID]; !seen {
			order = append(order, e.ID)
		}
		byID[e.ID] = append(byID[e.ID], e)
		if e.Event != "start" {
			continue
		}
		if f, _ := e.Data["fixes"].(string); f != "" {
			fixes[f]++
		}
		if ms, ok := modelsOfStart(e); ok && (cur == nil || !e.TS.Before(curTS)) {
			cur, curID, curTS = ms, e.ID, e.TS
		}
	}
	if cur == nil {
		return ModelVerdict{Verdict: "sin_datos"}
	}
	cur, _ = TaskModels(curID, byID[curID])

	var tasks []modelTask
	for i, id := range order {
		es := byID[id]
		start := es[0].TS
		lane := ""
		for _, e := range es {
			if e.Event == "start" {
				start = e.TS
				lane, _ = e.Data["lane"].(string)
			}
		}
		if lane != string(flow.Full) && lane != string(flow.Light) {
			continue
		}
		st := Compute(id, es, now)
		if st.Phase != flow.Done || st.ClosedOutside != "" {
			continue
		}
		ms, src := TaskModels(id, es)
		if src == "" {
			continue
		}
		tasks = append(tasks, modelTask{id: id, start: start, idx: i, models: ms,
			rejections: st.RejectionsByGate, rounds: st.Rounds, hotfixes: fixes[id]})
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if !tasks[i].start.Equal(tasks[j].start) {
			return tasks[i].start.After(tasks[j].start)
		}
		return tasks[i].idx > tasks[j].idx
	})

	// "después": las que coinciden con la vigente hasta la primera que no.
	n := 0
	for n < len(tasks) && tasks[n].models.Matches(cur) {
		n++
	}
	after := tasks[:n]
	v := ModelVerdict{Current: groupOf(cur, after)}
	if n < len(tasks) {
		prev := tasks[n].models
		m := n
		for m < len(tasks) && tasks[m].models.Matches(prev) {
			m++
		}
		pg := groupOf(prev, tasks[n:m])
		v.Previous = &pg
		for _, a := range sortedKeys(cur) {
			if p, ok := prev[a]; ok && p != cur[a] {
				v.Changes = append(v.Changes, ModelChange{Agent: a, From: p, To: cur[a]})
			}
		}
	}
	if v.Previous == nil || len(v.Current.Tasks) < ModelWindow || len(v.Previous.Tasks) < ModelWindow {
		v.Verdict = "sin_veredicto"
		return v
	}
	v.Verdict = "sin_regresion"
	gates := map[string]bool{}
	for g := range v.Current.Rejections {
		gates[g] = true
	}
	for g := range v.Previous.Rejections {
		gates[g] = true
	}
	gl := make([]string, 0, len(gates))
	for g := range gates {
		gl = append(gl, g)
	}
	sort.Strings(gl)
	over := func(label string, a, b float64) {
		if a > b+1e-9 {
			v.Rose = append(v.Rose, fmt.Sprintf("%s %.1f > %.1f", label, a, b))
		}
	}
	for _, g := range gl {
		over("rechazos "+g, v.Current.Rejections[g], v.Previous.Rejections[g])
	}
	over("rondas", v.Current.Rounds, v.Previous.Rounds)
	over("hotfixes", v.Current.Hotfixes, v.Previous.Hotfixes)
	if len(v.Rose) > 0 {
		v.Verdict = "sube"
	}
	return v
}

func sortedKeys(m ModelSet) []string {
	k := make([]string, 0, len(m))
	for a := range m {
		k = append(k, a)
	}
	slices.Sort(k)
	return k
}

// groupOf arma el grupo con las ModelWindow tareas más recientes de ts.
func groupOf(models ModelSet, ts []modelTask) ModelGroup {
	if len(ts) > ModelWindow {
		ts = ts[:ModelWindow]
	}
	g := ModelGroup{Models: models, Tasks: []string{}, Rejections: map[string]float64{}}
	if len(ts) == 0 {
		return g
	}
	n := float64(len(ts))
	for _, t := range ts {
		g.Tasks = append(g.Tasks, t.id)
		for gate, c := range t.rejections {
			g.Rejections[gate] += float64(c) / n
		}
		g.Rounds += float64(t.rounds) / n
		g.Hotfixes += float64(t.hotfixes) / n
	}
	for gate, x := range g.Rejections {
		g.Rejections[gate] = math.Round(x*1e9) / 1e9
	}
	return g
}
