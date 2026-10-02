package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/store"
)

const (
	legacyNote  = "sin detalle por llamada (registrado antes de GH-29)"
	partialNote = "parte del gasto se registró antes de GH-29"
	noTokens    = "tokens: no disponibles"
)

// claudeLine es una línea de transcript de Claude Code.
func claudeLine(id string, ts time.Time, in, out, cr, cw int) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": ts.UTC().Format(time.RFC3339Nano),
		"message": map[string]any{"id": id, "model": "claude-opus-5-5",
			"usage": map[string]any{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cr, "cache_creation_input_tokens": cw}}})
	return string(b) + "\n"
}

func appendText(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// rawCalls lee las líneas de calls.jsonl de la tarea tal como las dejó el hook.
func rawCalls(t *testing.T, e *engine.Engine, id string) []metrics.Call {
	t.Helper()
	b, err := e.Store.ReadFile(id, metrics.CallsFile)
	if err != nil {
		t.Fatalf("falta calls.jsonl: %v", err)
	}
	var out []metrics.Call
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var c metrics.Call
		if err := json.Unmarshal([]byte(l), &c); err != nil {
			t.Fatalf("línea que no es JSON: %q", l)
		}
		out = append(out, c)
	}
	return out
}

func claudeHook(t *testing.T, env *Env, payload map[string]string) {
	t.Helper()
	b, _ := json.Marshal(payload)
	if code, out, errOut := hookRun(t, env, string(b), "hook", "tokens"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("el hook es silencioso: %d %q %q", code, out, errOut)
	}
}

func TestHookTokensWritesCalls(t *testing.T) {
	countNotify(t)
	env, e, id, cache := tokensEnv(t, true)
	now := time.Now()

	// Claude, sesión principal: m1 se lee en dos pasadas.
	main := filepath.Join(t.TempDir(), "sess-1.jsonl")
	appendText(t, main, claudeLine("m1", now, 2, 10, 1000, 300))
	claudeHook(t, env, map[string]string{"transcript_path": main})
	appendText(t, main, claudeLine("m1", now, 2, 40, 1000, 300)+claudeLine("m2", now.Add(time.Second), 1, 5, 2000, 0))
	claudeHook(t, env, map[string]string{"transcript_path": main})

	raw := rawCalls(t, e, id)
	if len(raw) != 3 {
		t.Fatalf("una línea por muestra: m1, el delta de m1 y m2: %+v", raw)
	}
	for _, c := range raw {
		if c.Run != "claude:sess-1" || c.Tool != "claude" || c.Agent != metrics.MainSession || c.Phase == "" || c.Model != "claude-opus-5-5" || c.TS.IsZero() {
			t.Errorf("run, tool, agente, fase, modelo y hora en cada línea: %+v", c)
		}
	}
	if raw[0].Msg != "m1" || raw[1].Msg != "m1" || raw[2].Msg != "m2" || raw[1].Output != 30 || raw[1].CacheRead != 0 {
		t.Errorf("la segunda pasada deja solo el delta de m1: %+v", raw)
	}
	runs := metrics.Runs(raw)
	if len(runs) != 1 || len(runs[0].Rows) != 2 || runs[0].Rows[0].Output != 40 || runs[0].Rows[0].CacheRead != 1000 {
		t.Errorf("un mensaje leído en dos pasadas deja una fila: %+v", runs)
	}

	// Claude, subagente: el agente sin el prefijo y su propia corrida.
	sub := filepath.Join(t.TempDir(), "agent-abc.jsonl")
	appendText(t, sub, claudeLine("s1", now, 3, 7, 500, 100))
	claudeHook(t, env, map[string]string{"transcript_path": main, "agent_transcript_path": sub, "agent_type": "bflow-implementer", "agent_id": "abc"})
	var sc []metrics.Call
	for _, c := range rawCalls(t, e, id) {
		if c.Run == "claude:agent-abc" {
			sc = append(sc, c)
		}
	}
	if len(sc) != 1 || sc[0].Agent != "implementer" || sc[0].Msg != "s1" || sc[0].CacheRead != 500 || sc[0].CacheWrite != 100 {
		t.Errorf("subagente: %+v", sc)
	}

	// OpenCode: la llave de mensaje es la de cur.Seen y la corrida el id de sesión.
	putFile(t, cache, "ses_main.jsonl", usageEntry("o1", "ses_main", "", "build", 100, 10)+usageEntry("o2", "ses_main", "", "build", 1, 1), 0)
	putFile(t, cache, "ses_kid.jsonl", usageEntry("k1", "ses_kid", "ses_main", "bflow-implementer", 200, 20), 0)
	idle := `{"sessionID":"ses_main","parentID":"","cwd":"` + strings.ReplaceAll(env.Dir, `\`, `\\`) + `"}`
	hookRun(t, env, idle, "hook", "tokens", "--tool", "opencode")
	got := map[string]string{}
	for _, c := range rawCalls(t, e, id) {
		if c.Tool == "opencode" {
			got[c.Msg] = c.Run + "|" + c.Agent
		}
	}
	want := map[string]string{"opencode:o1": "opencode:ses_main|main", "opencode:o2": "opencode:ses_main|main", "opencode:k1": "opencode:ses_kid|implementer"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("opencode: %v, want %v", got, want)
	}

	// Una pasada sin nada nuevo no agrega líneas.
	n := len(rawCalls(t, e, id))
	hookRun(t, env, idle, "hook", "tokens", "--tool", "opencode")
	if len(rawCalls(t, e, id)) != n {
		t.Error("sin muestras nuevas no se escribe nada")
	}
}

func TestHookTokensNeverFailsWritingCalls(t *testing.T) {
	countNotify(t)
	env, e, id, _ := tokensEnv(t, true)
	// calls.jsonl es una carpeta: no se puede escribir.
	p, err := e.Store.Path(id, metrics.CallsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(t.TempDir(), "sess-1.jsonl")
	appendText(t, main, claudeLine("m1", time.Now(), 2, 10, 1000, 300))
	claudeHook(t, env, map[string]string{"transcript_path": main})
	if by, _ := tokensByAgent(t, e, id); by["main"] != 2 {
		t.Errorf("los eventos tokens se registran aunque calls.jsonl falle: %v", by)
	}
}

func TestCallsMatchTokenEvents(t *testing.T) {
	countNotify(t)
	env, e, id, cache := tokensEnv(t, true)
	now := time.Now()
	main := filepath.Join(t.TempDir(), "sess-1.jsonl")
	appendText(t, main, claudeLine("m1", now, 2, 10, 1000, 300))
	claudeHook(t, env, map[string]string{"transcript_path": main})
	appendText(t, main, claudeLine("m1", now, 2, 40, 1000, 300)+claudeLine("m2", now.Add(time.Second), 1, 5, 2000, 0))
	claudeHook(t, env, map[string]string{"transcript_path": main})
	sub := filepath.Join(t.TempDir(), "agent-abc.jsonl")
	appendText(t, sub, claudeLine("s1", now, 3, 7, 500, 100)+claudeLine("s2", now.Add(time.Second), 1, 9, 900, 0))
	claudeHook(t, env, map[string]string{"transcript_path": main, "agent_transcript_path": sub, "agent_type": "bflow-implementer"})
	putFile(t, cache, "ses_main.jsonl", usageEntry("o1", "ses_main", "", "build", 100, 10), 0)
	hookRun(t, env, `{"sessionID":"ses_main","parentID":"","cwd":"x"}`, "hook", "tokens", "--tool", "opencode")

	log, err := e.Store.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	var fromLog metrics.Usage
	for _, en := range log {
		if en.Event == "tokens" {
			fromLog.Add(metrics.Usage{Input: num(en.Data["input"]), Output: num(en.Data["output"]),
				CacheRead: num(en.Data["cache_read"]), CacheWrite: num(en.Data["cache_write"])})
		}
	}
	var fromCalls metrics.Usage
	runs := metrics.Runs(rawCalls(t, e, id))
	for _, r := range runs {
		fromCalls.Add(metrics.Usage{Input: r.Total.Input, Output: r.Total.Output, CacheRead: r.Total.CacheRead, CacheWrite: r.Total.CacheWrite})
	}
	if len(runs) != 3 || fromLog.Total() == 0 || fromCalls != fromLog {
		t.Errorf("la suma de las corridas (%d) debe ser la de los eventos tokens: calls %+v, log %+v", len(runs), fromCalls, fromLog)
	}
}

// ---- stats --calls ----

var (
	t1 = time.Date(2026, 9, 30, 10, 0, 1, 0, time.UTC)
	t2 = t1.Add(time.Minute)
	t3 = t1.Add(2 * time.Minute)
	t4 = t1.Add(3 * time.Minute)
)

func callLine(run, agent, msg, model string, ts time.Time, in, cw, cr, out int64) string {
	b, _ := json.Marshal(metrics.Call{TS: ts, Run: run, Tool: "claude", Agent: agent, Phase: flow.Implementing, Model: model, Msg: msg,
		Input: in, CacheWrite: cw, CacheRead: cr, Output: out})
	return string(b) + "\n"
}

// callsFixture es una tarea con dos corridas escritas en calls.jsonl. Totales:
// entrada 14, caché escrita 350, releído 14,000, salida 165 (nuevo 529).
func callsFixture(t *testing.T, withCalls bool) (*Env, *engine.Engine, string) {
	t.Helper()
	env, e, id, _ := tokensEnv(t, true)
	if withCalls {
		body := callLine("claude:agent-a", "implementer", "m1", "opus", t1, 2, 300, 1000, 40) +
			callLine("claude:agent-a", "implementer", "m2", "opus", t2, 1, 0, 2000, 5) +
			callLine("claude:sess-1", "main", "m3", "opus", t3, 10, 0, 5000, 100) +
			callLine("claude:sess-1", "main", "m4", "opus", t4, 1, 50, 6000, 20)
		p, err := e.Store.Path(id, metrics.CallsFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return env, e, id
}

func addTokenEntry(t *testing.T, e *engine.Engine, id string, in, out, cr, cw int64) {
	t.Helper()
	err := e.Store.Append(store.Entry{TS: time.Now(), ID: id, Event: "tokens", Agent: "main", By: "dev",
		Data: map[string]any{"phase": "implementing", "tool": "claude", "input": in, "output": out, "cache_read": cr, "cache_write": cw}})
	if err != nil {
		t.Fatal(err)
	}
}

func textOf(t *testing.T, env *Env, args ...string) (int, string) {
	t.Helper()
	env.Stdout = &bytes.Buffer{}
	code := Run(args, env)
	return code, env.Stdout.(*bytes.Buffer).String()
}

func TestStatsCallsText(t *testing.T) {
	env, e, id := callsFixture(t, true)
	addTokenEntry(t, e, id, 14, 165, 14000, 350)
	code, out := textOf(t, env, "stats", id, "--calls")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	_, plain := textOf(t, env, "stats", id)
	// Una segunda corrida del implementer en la misma fase: ambas llevan #N en la etapa (R2, R4).
	p, _ := e.Store.Path(id, metrics.CallsFile)
	appendText(t, p, callLine("claude:agent-b", "implementer", "m5", "opus", t4.Add(time.Minute), 1, 0, 100, 5))
	_, out = textOf(t, env, "stats", id, "--calls")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != strings.Split(plain, "\n")[0] {
		t.Errorf("la primera línea es el resumen de la tarea:\n%q\n%q", lines[0], strings.Split(plain, "\n")[0])
	}
	for _, want := range []string{
		"implementer · implementing #1 · 2 llamadas · contexto final 2,001 · releído 3,000 · nuevo 348",
		"implementer · implementing #2 · 1 llamadas · contexto final 101 · releído 100 · nuevo 6",
		"sesión principal · implementing · 2 llamadas · contexto final 6,051 · releído 11,000 · nuevo 181",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("falta el encabezado %q en:\n%s", want, out)
		}
	}
	if strings.Contains(out, legacyNote) || strings.Contains(out, partialNote) {
		t.Errorf("los números cuadran: sin avisos\n%s", out)
	}
	hdr := []string{"#", "hora", "modelo", "entrada", "caché escrita", "releído", "salida", "contexto", "acum. releído", "acum. nuevo"}
	pos, header := 0, ""
	for _, l := range lines {
		if strings.Contains(l, "caché escrita") && strings.Contains(l, "acum. nuevo") {
			header = l
			break
		}
	}
	for _, h := range hdr {
		i := strings.Index(header[pos:], h)
		if i < 0 {
			t.Fatalf("columna %q fuera de lugar en el encabezado de la tabla: %q", h, header)
		}
		pos += i + len(h)
	}
	fields := func(n, hour string) []string {
		for _, l := range lines {
			if f := strings.Fields(l); len(f) > 2 && f[0] == n && f[1] == hour {
				return f
			}
		}
		return nil
	}
	hour := func(ts time.Time) string { return ts.Local().Format("15:04:05") }
	if got, want := fields("1", hour(t1)), []string{"1", hour(t1), "opus", "2", "300", "1,000", "40", "1,302", "1,000", "342"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fila 1: %v, want %v", got, want)
	}
	if got, want := fields("2", hour(t2)), []string{"2", hour(t2), "opus", "1", "0", "2,000", "5", "2,001", "3,000", "348"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fila 2: %v, want %v", got, want)
	}
	// Fila de totales de la corrida del implementer: sumas y contexto máximo.
	var total []string
	for _, l := range lines {
		if f := strings.Fields(l); len(f) > 0 && f[0] == "total" {
			total = f
			break
		}
	}
	if total == nil {
		t.Fatalf("falta la fila total:\n%s", out)
	}
	if !subsequence(total, []string{"3", "300", "3,000", "45", "2,001"}) {
		t.Errorf("total: entrada, caché escrita, releído, salida y contexto máximo: %v", total)
	}
}

func subsequence(have, want []string) bool {
	i := 0
	for _, h := range have {
		if i < len(want) && h == want[i] {
			i++
		}
	}
	return i == len(want)
}

func TestStatsCallsJSON(t *testing.T) {
	env, e, id := callsFixture(t, true)
	addTokenEntry(t, e, id, 14, 165, 14000, 350)
	code, out, raw := runJSON(t, env, "stats", id, "--calls")
	if code != 0 || out["ok"] != true {
		t.Fatalf("exit %d: %s", code, raw)
	}
	data := out["data"].(map[string]any)
	if _, ok := data["stats"].(map[string]any); !ok {
		t.Fatalf("data.stats como hoy: %s", raw)
	}
	runs, _ := data["calls"].([]any)
	if len(runs) != 2 {
		t.Fatalf("data.calls: una entrada por corrida: %s", raw)
	}
	r0 := runs[0].(map[string]any)
	if r0["run"] != "claude:agent-a" || r0["name"] != "implementer" || r0["stage"] != "implementing" || r0["label"] != "implementer · implementing" || r0["phase"] != "implementing" || r0["final_context"] != float64(2001) {
		t.Errorf("corrida 0: %v", r0)
	}
	rows := r0["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("filas: %v", rows)
	}
	row := rows[1].(map[string]any)
	for k, want := range map[string]any{"n": float64(2), "msg": "m2", "model": "opus", "input": float64(1), "cache_write": float64(0), "cache_read": float64(2000),
		"output": float64(5), "context": float64(2001), "acc_read": float64(3000), "acc_new": float64(348)} {
		if row[k] != want {
			t.Errorf("fila.%s = %v, want %v", k, row[k], want)
		}
	}
}

func TestStatsJSONUnchangedWithoutCalls(t *testing.T) {
	env, e, id := callsFixture(t, true)
	addTokenEntry(t, e, id, 14, 165, 14000, 350)
	_, out, raw := runJSON(t, env, "stats", id)
	data := out["data"].(map[string]any)
	if keys := mapKeys(data); !slices.Equal(keys, []string{"stats"}) {
		t.Errorf("sin --calls data solo trae stats aunque haya calls.jsonl: %v\n%s", keys, raw)
	}
	if strings.Contains(raw, "acc_read") || strings.Contains(raw, "calls_note") {
		t.Errorf("sin --calls no hay detalle por llamada: %s", raw)
	}
	// Con --calls, data.stats trae las mismas llaves que sin él.
	_, with, _ := runJSON(t, env, "stats", id, "--calls")
	if _, ok := with["data"].(map[string]any)["calls"]; !ok {
		t.Error("con --calls data trae calls")
	}
	a, b := mapKeys(data["stats"].(map[string]any)), mapKeys(with["data"].(map[string]any)["stats"].(map[string]any))
	if !slices.Equal(a, b) {
		t.Errorf("data.stats igual con y sin --calls: %v vs %v", a, b)
	}
}

func mapKeys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func TestStatsCallsNeedsID(t *testing.T) {
	env, _, _ := callsFixture(t, true)
	code, out, raw := runJSON(t, env, "stats", "--calls")
	if code == 0 || out["ok"] != false || out["code"] != "usage" || !strings.Contains(raw, "--calls requiere un ID") {
		t.Errorf("sin ID falla con usage: %d %s", code, raw)
	}
}

func TestStatsCallsLegacyNote(t *testing.T) {
	env, e, id := callsFixture(t, false)
	addTokenEntry(t, e, id, 14, 165, 14000, 350)
	code, out := textOf(t, env, "stats", id, "--calls")
	if code != 0 || !strings.Contains(out, legacyNote) {
		t.Errorf("tokens sin calls.jsonl: %d\n%s", code, out)
	}
	// Archivo que existe pero sin filas válidas: el mismo aviso.
	p, _ := e.Store.Path(id, metrics.CallsFile)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("basura\n{\"run\":\"x\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, out := textOf(t, env, "stats", id, "--calls"); !strings.Contains(out, legacyNote) {
		t.Errorf("sin filas válidas: %s", out)
	}
	// Sin tokens: el texto de "no disponibles" de stats.
	env2, _, id2 := callsFixture(t, false)
	_, plain := textOf(t, env2, "stats", id2)
	_, out = textOf(t, env2, "stats", id2, "--calls")
	if !strings.Contains(plain, noTokens) || !strings.Contains(out, noTokens) || strings.Contains(out, legacyNote) {
		t.Errorf("sin tokens registrados:\n%s", out)
	}
}

func TestStatsCallsPartialNote(t *testing.T) {
	env, e, id := callsFixture(t, true)
	addTokenEntry(t, e, id, 14, 165, 14000, 350)
	addTokenEntry(t, e, id, 1000, 0, 0, 0) // gasto anterior a GH-29: más nuevo que las corridas
	if _, out := textOf(t, env, "stats", id, "--calls"); !strings.Contains(out, partialNote) {
		t.Errorf("más tokens nuevos que en las corridas:\n%s", out)
	}
	env2, e2, id2 := callsFixture(t, true)
	addTokenEntry(t, e2, id2, 14, 165, 14000, 350)
	addTokenEntry(t, e2, id2, 0, 0, 5000, 0) // solo más releído
	if _, out := textOf(t, env2, "stats", id2, "--calls"); !strings.Contains(out, partialNote) {
		t.Errorf("más releído que en las corridas:\n%s", out)
	}
}

// ---- panel ----

func TestPanelTokensTable(t *testing.T) {
	log := []string{
		callLine("claude:agent-a", "implementer", "m1", "opus", t1, 2, 300, 1000, 40),
		callLine("claude:agent-a", "implementer", "m2", "opus", t2, 1, 0, 2000, 5),
		callLine("claude:agent-r", "reviewer", "m5", "opus", t1.Add(90*time.Second), 5, 0, 100, 5),
		callLine("claude:sess-1", "main", "m3", "opus", t3, 10, 0, 5000, 100),
		callLine("claude:sess-1", "main", "m4", "opus", t4, 1, 50, 6000, 20),
	}
	var calls []metrics.Call
	for _, l := range log {
		calls = append(calls, metrics.ParseCalls(strings.NewReader(l))...)
	}
	runs := metrics.Runs(calls)
	since := t1
	v := engine.View{ID: "API-7", Lane: flow.Light, Phase: flow.Implementing, Since: &since, Phases: flow.DefaultLanes()[flow.Light]}
	// Nuevo: implementer 348, main 181, "" 471 (total 1000). El reviewer solo está en calls.jsonl.
	agents := map[string]metrics.Usage{
		"implementer": {Input: 3, CacheWrite: 300, Output: 45, CacheRead: 3000, Calls: 2},
		"main":        {Input: 11, CacheWrite: 50, Output: 120, CacheRead: 11000, Calls: 2},
		"":            {Input: 471},
	}
	st := metrics.TaskStats{ID: "API-7", TokensAvailable: true, Agents: agents,
		Tokens: metrics.Usage{Input: 485, Output: 165, CacheRead: 14000, CacheWrite: 350, Calls: 4, MaxContext: 6051}}

	ps := buildPanel("dev", watchData{Repo: "api", Active: &v, Stats: st, Calls: runs, CallsFile: true}, t4.Add(time.Minute))
	task := ps.Task
	if task == nil {
		t.Fatal("sin tarea")
	}
	by := map[string]panelAgent{}
	var order []string
	for _, a := range task.Agents {
		by[a.Key] = a
		order = append(order, a.Key)
	}
	if len(task.Agents) != 4 {
		t.Fatalf("una fila por agente de st.Agents y una por el que solo está en calls.jsonl: %v", order)
	}
	if i, j, k := slices.Index(order, ""), slices.Index(order, "implementer"), slices.Index(order, "main"); !(i < j && j < k) {
		t.Errorf("orden por nuevo de mayor a menor: %v", order)
	}
	impl, main, none, rev := by["implementer"], by["main"], by[""], by["reviewer"]
	if impl.Name != "implementer" || main.Name != "sesión principal" || none.Name != "sin desglose" || rev.Name != "reviewer" {
		t.Errorf("nombres: %q %q %q %q", impl.Name, main.Name, none.Name, rev.Name)
	}
	if impl.Share != 34 || main.Share != 18 || none.Share != 47 {
		t.Errorf("share (0-100 del nuevo total, truncado): %d %d %d", impl.Share, main.Share, none.Share)
	}
	if impl.Calls != 2 || main.Calls != 2 || none.Calls != 0 || rev.Calls != 1 {
		t.Errorf("llamadas (0 = sin dato): %d %d %d %d", impl.Calls, main.Calls, none.Calls, rev.Calls)
	}
	if impl.New != "348" || main.New != "181" || none.New != "471" || rev.New != "10" {
		t.Errorf("nuevo en Human: %q %q %q %q", impl.New, main.New, none.New, rev.New)
	}
	if len(none.Runs) != 0 || none.Last != nil || none.Latest {
		t.Errorf("agente sin corridas: %+v", none)
	}
	if !main.Latest || impl.Latest || rev.Latest {
		t.Errorf("latest solo en el agente de la llamada más reciente: %v %v %v", main.Latest, impl.Latest, rev.Latest)
	}
	if impl.Last == nil || !impl.Last.Equal(t2) || main.Last == nil || !main.Last.Equal(t4) || rev.Last == nil || !rev.Last.Equal(t1.Add(90*time.Second)) {
		t.Errorf("last: %v %v %v", impl.Last, main.Last, rev.Last)
	}
	if len(impl.Runs) != 1 || len(main.Runs) != 1 || len(rev.Runs) != 1 {
		t.Fatalf("corridas anidadas: %d %d %d", len(impl.Runs), len(main.Runs), len(rev.Runs))
	}
	a, b := impl.Runs[0], main.Runs[0]
	if a.Key != "claude:agent-a" || a.Stage != "implementing" || a.Label != "implementer · implementing" ||
		b.Key != "claude:sess-1" || b.Label != "sesión principal · implementing" {
		t.Errorf("clave, etapa y etiqueta: %+v %+v", a, b)
	}
	if a.Summary != "2 llamadas · contexto final 2,001 · releído 3,000 · nuevo 348" {
		t.Errorf("resumen: %q", a.Summary)
	}
	if a.Calls != 2 || a.Context != "2.0k" || a.Read != "3.0k" || a.New != "348" || b.Context != "6.1k" || b.Read != "11.0k" || b.New != "181" {
		t.Errorf("cifras cortas de un decimal: %+v %+v", a, b)
	}
	hour := t1.Local().Format("15:04:05")
	if want := []string{"1", hour, "opus", "2", "300", "1,000", "40", "1,302", "1,000", "342"}; len(a.Rows) != 2 || !reflect.DeepEqual(a.Rows[0], want) {
		t.Errorf("celdas exactas, con la misma función que el CLI: %v", a.Rows)
	}
	if len(a.Total) != len(a.Rows[0]) || a.Total[0] != "total" || !subsequence(a.Total, []string{"3", "300", "3,000", "45", "2,001"}) {
		t.Errorf("fila total: %v", a.Total)
	}
	if !strings.Contains(task.CallsNote, partialNote) {
		t.Errorf("calls_note sigue igual: %q", task.CallsNote)
	}

	// Un agente con dos corridas las trae en orden de primera llamada.
	more := append(slices.Clone(calls), metrics.ParseCalls(strings.NewReader(
		callLine("claude:agent-c", "implementer", "m6", "opus", t4.Add(time.Minute), 1, 0, 10, 1)))...)
	ps = buildPanel("dev", watchData{Repo: "api", Active: &v, Stats: st, Calls: metrics.Runs(more), CallsFile: true}, t4)
	found := false
	for _, ag := range ps.Task.Agents {
		if ag.Key == "implementer" {
			found = true
			if len(ag.Runs) != 2 || ag.Runs[0].Key != "claude:agent-a" || ag.Runs[1].Key != "claude:agent-c" || ag.Runs[1].Stage != "implementing #2" || !ag.Latest {
				t.Errorf("dos corridas en orden de primera llamada: %+v", ag)
			}
		}
	}
	if !found {
		t.Error("falta la fila del implementer")
	}

	// Registrado antes de GH-29: tokens sin archivo de llamadas; las filas salen sin corridas.
	ps = buildPanel("dev", watchData{Repo: "api", Active: &v, Stats: st}, t4)
	if ps.Task.CallsNote != legacyNote || len(ps.Task.Agents) != 3 || ps.Task.Agents[0].Name == "" {
		t.Errorf("sin calls.jsonl: %+v", ps.Task)
	}
	// Sin tokens ni llamadas: nada que decir.
	ps = buildPanel("dev", watchData{Repo: "api", Active: &v, Stats: metrics.TaskStats{ID: "API-7"}}, t4)
	if len(ps.Task.Agents) != 0 || ps.Task.CallsNote != "" {
		t.Errorf("sin nada: %+v", ps.Task)
	}
}
