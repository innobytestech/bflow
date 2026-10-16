package metrics

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

func asAny(m map[string]string) map[string]any {
	o := map[string]any{}
	for k, v := range m {
		o[k] = v
	}
	return o
}

// mt describe una tarea del log de modelos.
type mt struct {
	id      string
	day     int               // el start ocurre en el día `day`
	lane    string            // "" = light
	models  map[string]string // data.models del start
	infer   map[string]string // tokens por agente → modelo
	rej     map[string]int    // rechazos por gate
	rounds  int
	fixes   string     // ID que corrige (hotfix)
	end     flow.Phase // "" = done
	outside bool
}

func (m mt) entries() []store.Entry {
	ts := func(min int) time.Time {
		return t0.Add(time.Duration(m.day)*24*time.Hour + time.Duration(min)*time.Minute)
	}
	lane := m.lane
	if lane == "" {
		lane = "light"
	}
	data := map[string]any{"lane": lane}
	if m.models != nil {
		data["models"] = asAny(m.models)
	}
	if m.fixes != "" {
		data["fixes"] = m.fixes
	}
	out := []store.Entry{{TS: ts(0), ID: m.id, Event: "start", To: flow.Spec, Data: data}}
	min := 1
	for _, g := range []string{"spec", "contract", "quality"} {
		for i := 0; i < m.rej[g]; i++ {
			out = append(out, store.Entry{TS: ts(min), ID: m.id, Event: "reject", Gate: g, From: flow.Spec, To: flow.Spec})
			min++
		}
	}
	if m.rounds > 0 {
		out = append(out, store.Entry{TS: ts(min), ID: m.id, Event: "report", Agent: "reviewer", From: flow.Quality, To: flow.Implementing, Round: m.rounds})
		min++
	}
	for a, mod := range m.infer {
		out = append(out, store.Entry{TS: ts(min), ID: m.id, Event: "tokens", Agent: a,
			Data: map[string]any{"phase": "implementing", "model": mod, "input": float64(1)}})
	}
	end := m.end
	if end == "" {
		end = flow.Done
	}
	switch {
	case m.outside:
		out = append(out, store.Entry{TS: ts(100), ID: m.id, Event: string(flow.EvClosedOutside), From: flow.Spec, To: flow.Done, Data: map[string]any{"reason": "pr"}})
	case end != flow.Implementing:
		out = append(out, store.Entry{TS: ts(100), ID: m.id, Event: "approve", From: flow.InReview, To: end})
	}
	return out
}

func logOf(ts ...mt) []store.Entry {
	var all []store.Entry
	for _, m := range ts {
		all = append(all, m.entries()...)
	}
	return all
}

var (
	cfgA = map[string]string{"reviewer": "sonnet", "implementer": "sonnet"}
	cfgB = map[string]string{"reviewer": "opus", "implementer": "sonnet"}
	cfgC = map[string]string{"reviewer": "haiku", "implementer": "haiku"}
)

func TestNormalizeModel(t *testing.T) {
	for in, want := range map[string]string{
		"anthropic/Claude-Opus-4-5":    "opus",
		"claude-3-5-haiku-20241022":    "haiku",
		"SONNET":                       "sonnet",
		"amazon-bedrock/claude-sonnet": "sonnet",
		"openai/GPT-5":                 "gpt-5",
		"":                             "heredado",
	} {
		if got := NormalizeModel(in); got != want {
			t.Errorf("NormalizeModel(%q) = %q, quiero %q", in, got, want)
		}
	}
}

func TestTaskModelsFromStart(t *testing.T) {
	log := logOf(mt{id: "T-1", models: map[string]string{"reviewer": "anthropic/Claude-Opus-4", "implementer": "", "scout": "haiku"},
		infer: map[string]string{"scout": "sonnet"}})
	got, src := TaskModels("T-1", log)
	want := ModelSet{"reviewer": "opus", "implementer": "heredado", "scout": "sonnet"}
	if src != "start+tokens" || !reflect.DeepEqual(got, want) {
		t.Errorf("TaskModels = %v (%s), quiero %v (start+tokens)", got, src, want)
	}
	// Un data.models que no es un mapa de strings se ignora y cae a tokens.
	bad := logOf(mt{id: "T-2", infer: map[string]string{"reviewer": "opus"}})
	bad[0].Data["models"] = "opus"
	got, src = TaskModels("T-2", bad)
	if src != "tokens" || got["reviewer"] != "opus" {
		t.Errorf("models inválido debe caer a tokens: %v (%s)", got, src)
	}
}

func TestTaskModelsInferredFromTokens(t *testing.T) {
	tok := func(min int, agent, model string) store.Entry {
		return store.Entry{TS: at(min), ID: "T-1", Event: "tokens", Agent: agent, Data: map[string]any{"phase": "implementing", "model": model, "input": float64(1)}}
	}
	log := []store.Entry{
		{TS: at(0), ID: "T-1", Event: "start", To: flow.Spec, Data: map[string]any{"lane": "light"}},
		tok(1, "reviewer", "claude-opus-4"), tok(2, "reviewer", "claude-sonnet-4"), tok(3, "reviewer", "claude-opus-4"), // más llamadas: opus
		tok(4, "implementer", "claude-sonnet-4"), tok(5, "implementer", "claude-haiku-4"), // empate: el último
		tok(6, MainSession, "claude-opus-4"), tok(7, "", "claude-opus-4"), // no cuentan
		{TS: at(8), ID: "T-9", Event: "tokens", Agent: "documenter", Data: map[string]any{"model": "haiku"}}, // otra tarea
	}
	got, src := TaskModels("T-1", log)
	want := ModelSet{"reviewer": "opus", "implementer": "haiku"}
	if src != "tokens" || !reflect.DeepEqual(got, want) {
		t.Errorf("TaskModels = %v (%s), quiero %v (tokens)", got, src, want)
	}
}

func TestTaskModelsWithoutData(t *testing.T) {
	log := []store.Entry{
		{TS: at(0), ID: "T-1", Event: "start", To: flow.Spec, Data: map[string]any{"lane": "light"}},
		{TS: at(1), ID: "T-1", Event: "tokens", Agent: MainSession, Data: map[string]any{"model": "opus"}},
		{TS: at(2), ID: "T-1", Event: "tokens", Agent: "", Data: map[string]any{"model": "opus"}},
	}
	got, src := TaskModels("T-1", log)
	if len(got) != 0 || src != "" {
		t.Errorf("sin datos: %v (%q)", got, src)
	}
}

func TestModelSetMatches(t *testing.T) {
	a := ModelSet{"reviewer": "opus", "implementer": "sonnet"}
	for name, c := range map[string]struct {
		b    ModelSet
		want bool
	}{
		"iguales":               {ModelSet{"reviewer": "opus", "implementer": "sonnet"}, true},
		"un compartido difiere": {ModelSet{"reviewer": "sonnet", "implementer": "sonnet"}, false},
		"extra en el otro":      {ModelSet{"reviewer": "opus", "scout": "haiku"}, true},
		"sin agentes en común":  {ModelSet{"scout": "haiku"}, false},
		"vacío":                 {ModelSet{}, false},
		"heredado no es igual":  {ModelSet{"reviewer": "heredado"}, false},
	} {
		if got := a.Matches(c.b); got != c.want {
			t.Errorf("%s: Matches = %v, quiero %v", name, got, c.want)
		}
		if got := c.b.Matches(a); got != c.want {
			t.Errorf("%s (al revés): Matches = %v, quiero %v", name, got, c.want)
		}
	}
	if got := (ModelSet{"reviewer": "opus", "documenter": "haiku"}).String(); got != "documenter=haiku,reviewer=opus" {
		t.Errorf("String = %q", got)
	}
}

// groupLog junta las tareas de antes y de después, en ese orden en el tiempo.
func groupLog(antes, despues []mt) []store.Entry {
	return logOf(append(slices.Clone(antes), despues...)...)
}

func nTasks(prefix string, firstDay int, models map[string]string, n int, f func(i int, m *mt)) []mt {
	var out []mt
	for i := 0; i < n; i++ {
		m := mt{id: fmt.Sprintf("%s-%d", prefix, i+1), day: firstDay + i, models: models}
		if f != nil {
			f(i, &m)
		}
		out = append(out, m)
	}
	return out
}

func TestCompareModelsNoVerdictUnderWindow(t *testing.T) {
	log := groupLog(nTasks("A", 0, cfgA, 3, nil), nTasks("B", 10, cfgB, 2, func(i int, m *mt) { m.rounds = 5 }))
	v := CompareModels(log, at(0))
	if v.Verdict != "sin_veredicto" {
		t.Fatalf("con 2 tareas después: %q", v.Verdict)
	}
	if !reflect.DeepEqual(v.Current.Tasks, []string{"B-2", "B-1"}) || v.Previous == nil || len(v.Previous.Tasks) != 3 {
		t.Errorf("cuentas: después %v, antes %+v", v.Current.Tasks, v.Previous)
	}
	if len(v.Rose) != 0 {
		t.Errorf("sin veredicto no calcula si sube: %v", v.Rose)
	}
	// Sin configuración anterior tampoco hay veredicto.
	v = CompareModels(logOf(nTasks("B", 10, cfgB, 3, nil)...), at(0))
	if v.Verdict != "sin_veredicto" || v.Previous != nil || len(v.Current.Tasks) != 3 {
		t.Errorf("sin anterior: %+v", v)
	}
}

func TestCompareModelsRises(t *testing.T) {
	antes := nTasks("A", 1, cfgA, 4, func(i int, m *mt) {
		m.rounds = 1
		if i == 1 {
			m.rej = map[string]int{"spec": 1} // 1 de las 3 recientes: 0.3
		}
		if i == 0 {
			m.rounds, m.rej = 9, map[string]int{"spec": 9} // fuera de la ventana de 3
		}
		if i == 3 { // sin data.models: sale de tokens
			m.models, m.infer = nil, map[string]string{"reviewer": "sonnet", "implementer": "sonnet"}
		}
	})
	despues := nTasks("B", 10, cfgB, 3, func(i int, m *mt) {
		m.rounds = []int{2, 2, 1}[i]
		m.rej = map[string]int{"spec": []int{1, 1, 0}[i]}
	})
	v := CompareModels(groupLog(antes, despues), at(0))
	if v.Verdict != "sube" {
		t.Fatalf("veredicto %q: %+v", v.Verdict, v)
	}
	if !reflect.DeepEqual(v.Previous.Tasks, []string{"A-4", "A-3", "A-2"}) {
		t.Errorf("antes (3 más recientes, la de tokens cuenta): %v", v.Previous.Tasks)
	}
	if !reflect.DeepEqual(v.Changes, []ModelChange{{Agent: "reviewer", From: "sonnet", To: "opus"}}) {
		t.Errorf("cambios: %+v", v.Changes)
	}
	all := strings.Join(v.Rose, "|")
	for _, want := range []string{"rondas 1.7 > 1.0", "rechazos spec 0.7 > 0.3"} {
		if !strings.Contains(all, want) {
			t.Errorf("Rose no dice %q: %v", want, v.Rose)
		}
	}

	t.Run("hotfixes", func(t *testing.T) {
		antes := nTasks("A", 1, cfgA, 3, nil)
		despues := nTasks("B", 10, cfgB, 3, nil)
		hot := []mt{{id: "H-1", day: 20, lane: "hotfix", fixes: "B-3", models: cfgB}}
		v := CompareModels(append(groupLog(antes, despues), logOf(hot...)...), at(0))
		if v.Verdict != "sube" || len(v.Rose) != 1 || !strings.Contains(v.Rose[0], "hotfixes 0.3 > 0.0") {
			t.Errorf("un hotfix en después: %q %v", v.Verdict, v.Rose)
		}
	})
	t.Run("gate solo en después cuenta 0 en antes", func(t *testing.T) {
		antes := nTasks("A", 1, cfgA, 3, nil)
		despues := nTasks("B", 10, cfgB, 3, func(i int, m *mt) { m.rej = map[string]int{"contract": 1} })
		v := CompareModels(groupLog(antes, despues), at(0))
		if v.Verdict != "sube" || !strings.Contains(strings.Join(v.Rose, "|"), "rechazos contract 1.0 > 0.0") {
			t.Errorf("%q %v", v.Verdict, v.Rose)
		}
	})
}

func TestCompareModelsNoRegression(t *testing.T) {
	antes := nTasks("A", 1, cfgA, 3, func(i int, m *mt) {
		m.rounds = 2
		m.rej = map[string]int{"spec": 1, "contract": 1} // gate que después ya no tiene: cuenta 0
	})
	despues := nTasks("B", 10, cfgB, 3, func(i int, m *mt) {
		m.rounds = []int{2, 2, 0}[i]      // promedio 1.3 < 2.0
		m.rej = map[string]int{"spec": 1} // igual que antes: no es "por encima"
	})
	v := CompareModels(groupLog(antes, despues), at(0))
	if v.Verdict != "sin_regresion" || len(v.Rose) != 0 {
		t.Errorf("veredicto %q, rose %v", v.Verdict, v.Rose)
	}
	if v.Current.Rounds < 1.33 || v.Current.Rounds > 1.34 || v.Previous.Rounds != 2 || v.Current.Rejections["spec"] != 1 {
		t.Errorf("promedios: %+v %+v", v.Current, v.Previous)
	}
}

func TestCompareModelsSkipsHotfixDroppedAndOutside(t *testing.T) {
	// Entre y dentro de los grupos hay tareas que no cuentan ni cortan: si
	// contaran, su configuración C o sus cifras cambiarían los grupos.
	antes := nTasks("A", 1, cfgA, 3, nil)
	despues := nTasks("B", 20, cfgB, 3, nil)
	junk := []mt{
		{id: "HF-1", day: 21, lane: "hotfix", models: cfgC, rounds: 9},          // carril hotfix
		{id: "DR-1", day: 21, models: cfgC, end: flow.Dropped, rounds: 9},       // descartada
		{id: "OUT-1", day: 12, models: cfgC, outside: true, rounds: 9},          // cerrada fuera
		{id: "WIP-1", day: 13, models: cfgC, end: flow.Implementing, rounds: 9}, // sin terminar
		{id: "NODATA-1", day: 21}, // done sin models ni tokens (R8)
		{id: "NODATA-2", day: 5},
	}
	v := CompareModels(append(groupLog(antes, despues), logOf(junk...)...), at(0))
	if !reflect.DeepEqual(v.Current.Tasks, []string{"B-3", "B-2", "B-1"}) {
		t.Errorf("después: %v", v.Current.Tasks)
	}
	if v.Previous == nil || !reflect.DeepEqual(v.Previous.Tasks, []string{"A-3", "A-2", "A-1"}) {
		t.Errorf("antes: %+v", v.Previous)
	}
	if v.Verdict != "sin_regresion" {
		t.Errorf("las cifras de las que no cuentan no deben entrar: %q %v", v.Verdict, v.Rose)
	}
}

func TestCompareModelsNoData(t *testing.T) {
	if v := CompareModels(nil, at(0)); v.Verdict != "sin_datos" {
		t.Errorf("log vacío: %q", v.Verdict)
	}
	noModels := nTasks("A", 1, nil, 4, func(i int, m *mt) { m.infer = map[string]string{"reviewer": "opus"} })
	if v := CompareModels(logOf(noModels...), at(0)); v.Verdict != "sin_datos" {
		t.Errorf("ningún start trae data.models: %q", v.Verdict)
	}
}

func TestTaskModelsTokensOverrideStart(t *testing.T) {
	log := logOf(mt{id: "T-1", models: map[string]string{"reviewer": "sonnet", "implementer": "heredado"},
		infer: map[string]string{"reviewer": "anthropic/claude-opus-4"}})
	got, src := TaskModels("T-1", log)
	want := ModelSet{"reviewer": "opus", "implementer": "heredado"}
	if src != "start+tokens" || !reflect.DeepEqual(got, want) {
		t.Errorf("TaskModels = %v (%s), quiero %v (start+tokens)", got, src, want)
	}
	only := logOf(mt{id: "T-2", infer: map[string]string{"reviewer": "opus"}})
	if _, src := TaskModels("T-2", only); src != "tokens" {
		t.Errorf("sin start con modelos el origen es tokens, no %q", src)
	}
}

func TestTaskModelsStartFallbackPerAgent(t *testing.T) {
	log := logOf(mt{id: "T-1", models: map[string]string{"reviewer": "sonnet", "scout": "haiku"},
		infer: map[string]string{"implementer": "opus"}})
	log = append(log, store.Entry{TS: at(5), ID: "T-1", Event: "tokens", Agent: MainSession, Data: map[string]any{"model": "opus"}})
	got, src := TaskModels("T-1", log)
	want := ModelSet{"reviewer": "sonnet", "scout": "haiku", "implementer": "opus"}
	if src != "start+tokens" || !reflect.DeepEqual(got, want) {
		t.Errorf("TaskModels = %v (%s), quiero %v", got, src, want)
	}
	// Solo la sesión principal: no hay agente de tokens, queda el start.
	main := logOf(mt{id: "T-3", models: map[string]string{"reviewer": "sonnet"}})
	main = append(main, store.Entry{TS: at(5), ID: "T-3", Event: "tokens", Agent: MainSession, Data: map[string]any{"model": "opus"}})
	if got, src := TaskModels("T-3", main); src != "start" || got["reviewer"] != "sonnet" {
		t.Errorf("solo main: %v (%s), quiero start", got, src)
	}
}

func TestCompareModelsCurrentUsesEffectiveSet(t *testing.T) {
	// El start dice sonnet, pero lo que corrió fue opus: las tareas con opus
	// real son "después" y la anterior (sonnet real) es "antes".
	antes := nTasks("A", 0, cfgA, 3, nil)
	despues := nTasks("D", 10, cfgA, 3, func(i int, m *mt) { m.infer = map[string]string{"reviewer": "claude-opus-4"} })
	v := CompareModels(groupLog(antes, despues), t0.Add(60*24*time.Hour))
	if v.Current.Models["reviewer"] != "opus" {
		t.Errorf("la vigente debe ser el conjunto efectivo, no el start: %v", v.Current.Models)
	}
	if len(v.Current.Tasks) != 3 || v.Previous == nil || v.Previous.Models["reviewer"] != "sonnet" {
		t.Errorf("después/antes mal partidos: %+v", v)
	}
}
