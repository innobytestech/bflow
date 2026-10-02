package metrics

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// CallsFile es el archivo de la tarea con una línea por muestra del modelo.
const CallsFile = "calls.jsonl"

// Call es una muestra de una llamada del modelo tal como se guarda: puede ser
// un delta de una respuesta que otra pasada del hook ya había leído en parte.
type Call struct {
	TS         time.Time  `json:"ts"`
	Run        string     `json:"run"`   // RunKey
	Tool       string     `json:"tool"`  // claude | opencode
	Agent      string     `json:"agent"` // MainSession o nombre sin prefijo
	Phase      flow.Phase `json:"phase"`
	Model      string     `json:"model,omitempty"`
	Msg        string     `json:"msg"`
	Input      int64      `json:"input"`
	CacheWrite int64      `json:"cache_write"`
	CacheRead  int64      `json:"cache_read"`
	Output     int64      `json:"output"`
}

// Context es lo que la llamada leyó como contexto: Input + CacheRead + CacheWrite.
func (c Call) Context() int64 { return c.Input + c.CacheRead + c.CacheWrite }

// New es igual que Usage.New: Input + CacheWrite + Output.
func (c Call) New() int64 { return c.Input + c.CacheWrite + c.Output }

// CallRow es una llamada ya fusionada, con su lugar en la corrida.
type CallRow struct {
	N int `json:"n"`
	Call
	Context int64 `json:"context"`
	AccRead int64 `json:"acc_read"`
	AccNew  int64 `json:"acc_new"`
}

// Run es una corrida: un transcript de un agente o de la sesión principal.
type Run struct {
	Run          string     `json:"run"`
	Agent        string     `json:"agent"`
	Name         string     `json:"name"`  // "sesión principal" | agente (R3)
	Stage        string     `json:"stage"` // "contract", "discovery → spec", "spec #2" (R1, R2)
	Label        string     `json:"label"` // Name + " · " + Stage (R3)
	Phase        flow.Phase `json:"phase"` // de la primera fila
	Rows         []CallRow  `json:"rows"`
	Total        Usage      `json:"total"` // Calls = len(Rows), MaxContext = máximo
	FinalContext int64      `json:"final_context"`
	Last         time.Time  `json:"last"`
}

// RunKey identifica la corrida de un transcript: tool + ":" + base sin extensión.
func RunKey(tool, path string) string {
	// Las rutas de Windows y de Unix pueden llegar en cualquier sistema.
	path = strings.ReplaceAll(path, "\\", "/")
	base := filepath.Base(filepath.ToSlash(path))
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return tool + ":" + base
}

// ParseCalls lee calls.jsonl y salta las líneas malas (R6).
func ParseCalls(r io.Reader) []Call {
	var out []Call
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var c Call
			if json.Unmarshal(line, &c) == nil && c.Run != "" && c.Msg != "" {
				out = append(out, c)
			}
		}
		if err != nil {
			return out
		}
	}
}

// stageOf: fase única, "primera → última" o "sin fase" (R1).
func stageOf(rows []CallRow) string {
	if len(rows) == 0 || rows[0].Phase == "" {
		return "sin fase"
	}
	first, last := rows[0].Phase, rows[len(rows)-1].Phase
	if last == "" || first == last {
		return string(first)
	}
	return string(first) + " → " + string(last)
}

// Runs fusiona (R3), agrupa y acumula (R4); orden por primera llamada.
func Runs(calls []Call) []Run {
	type mk struct{ run, msg string }
	merged := map[mk]*Call{}
	byRun := map[string][]*Call{}
	var order []string
	for _, c := range calls {
		k := mk{c.Run, c.Msg}
		if m := merged[k]; m != nil {
			m.Input += c.Input
			m.CacheWrite += c.CacheWrite
			m.CacheRead += c.CacheRead
			m.Output += c.Output
			continue
		}
		cc := c
		merged[k] = &cc
		if byRun[c.Run] == nil {
			order = append(order, c.Run)
		}
		byRun[c.Run] = append(byRun[c.Run], &cc)
	}
	var runs []Run
	for _, key := range order {
		cs := byRun[key]
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].TS.Before(cs[j].TS) })
		r := Run{Run: key, Agent: cs[0].Agent, Phase: cs[0].Phase}
		var accRead, accNew int64
		for i, c := range cs {
			accRead += c.CacheRead
			accNew += c.New()
			r.Rows = append(r.Rows, CallRow{N: i + 1, Call: *c, Context: c.Context(), AccRead: accRead, AccNew: accNew})
			r.Total.Add(Usage{Input: c.Input, Output: c.Output, CacheRead: c.CacheRead, CacheWrite: c.CacheWrite,
				Calls: 1, MaxContext: c.Context()})
			r.FinalContext = c.Context()
			r.Last = c.TS
		}
		runs = append(runs, r)
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].Rows[0].TS.Before(runs[j].Rows[0].TS) })
	for i := range runs {
		name := runs[i].Agent
		if name == MainSession {
			name = "sesión principal"
		}
		runs[i].Name = name
		runs[i].Stage = stageOf(runs[i].Rows)
	}
	count := map[string]int{}
	for _, r := range runs {
		count[r.Name+"|"+r.Stage]++
	}
	seen := map[string]int{}
	for i := range runs {
		k := runs[i].Name + "|" + runs[i].Stage
		seen[k]++
		if count[k] > 1 {
			runs[i].Stage = fmt.Sprintf("%s #%d", runs[i].Stage, seen[k])
		}
		runs[i].Label = runs[i].Name + " · " + runs[i].Stage
	}
	return runs
}

// Exact da una cifra con separador de miles: 17711 -> "17,711".
func Exact(n int64) string {
	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b = append(b, ',')
		}
		b = append(b, s[i])
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// Phases da una fase por muestra, con la regla de Allot (R2).
func Phases(entries []store.Entry, id, agent string, samples []Sample) []flow.Phase {
	if len(samples) == 0 {
		return nil
	}
	var fixed flow.Phase
	if agent != "" {
		for _, e := range entries {
			if e.ID == id && e.Event == string(flow.EvReport) && e.Agent == agent {
				fixed = e.From
			}
		}
		if fixed == "" {
			fixed = PhaseAt(entries, id, samples[0].TS)
		}
	}
	out := make([]flow.Phase, len(samples))
	for i, s := range samples {
		out[i] = fixed
		if fixed == "" {
			out[i] = PhaseAt(entries, id, s.TS)
		}
	}
	return out
}
