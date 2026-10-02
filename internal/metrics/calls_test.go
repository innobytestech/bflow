package metrics

import (
	"reflect"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

func call(run, agent, msg string, min int, phase flow.Phase, in, cw, cr, out int64) Call {
	return Call{TS: at(min), Run: run, Tool: "claude", Agent: agent, Phase: phase, Model: "opus", Msg: msg,
		Input: in, CacheWrite: cw, CacheRead: cr, Output: out}
}

func TestRunKey(t *testing.T) {
	for _, c := range []struct{ tool, path, want string }{
		{"claude", "/home/u/.claude/projects/x/sess-1.jsonl", "claude:sess-1"},
		{"claude", `C:\Users\u\proj\sess-1\subagents\agent-abc.jsonl`, "claude:agent-abc"},
		{"opencode", "/repo/.bflow/cache/opencode/ses_kid.jsonl", "opencode:ses_kid"},
		{"claude", "sin-extension", "claude:sin-extension"},
	} {
		if got := RunKey(c.tool, c.path); got != c.want {
			t.Errorf("RunKey(%q, %q) = %q, want %q", c.tool, c.path, got, c.want)
		}
	}
}

func TestExact(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 17711: "17,711",
		344086: "344,086", 1234567: "1,234,567", -1234: "-1,234"} {
		if got := Exact(n); got != want {
			t.Errorf("Exact(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestParseCallsSkipsBadLines(t *testing.T) {
	in := strings.Join([]string{
		`{"ts":"2026-09-26T09:00:00Z","run":"claude:a","tool":"claude","agent":"main","phase":"spec","msg":"m1","input":1,"cache_write":2,"cache_read":3,"output":4}`,
		`esto no es json`,
		`{"ts":"2026-09-26T09:01:00Z","tool":"claude","msg":"m2","input":1}`,       // sin run
		`{"ts":"2026-09-26T09:02:00Z","run":"claude:a","tool":"claude","input":1}`, // sin msg
		`{"ts":"2026-09-26T09:03:00Z","run":"claude:a","msg":"m3","input":5`,       // truncada
		``, // vacía
		`{"ts":"2026-09-26T09:04:00Z","run":"claude:a","msg":"m4","model":"opus","output":9}`, // válida, campos ausentes en 0
	}, "\n") + "\n"
	got := ParseCalls(strings.NewReader(in))
	if len(got) != 2 {
		t.Fatalf("solo las dos líneas válidas: %+v", got)
	}
	want := Call{TS: at(0), Run: "claude:a", Tool: "claude", Agent: "main", Phase: flow.Spec, Msg: "m1", Input: 1, CacheWrite: 2, CacheRead: 3, Output: 4}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("primera línea: %+v, want %+v", got[0], want)
	}
	if got[1].Msg != "m4" || got[1].Output != 9 || got[1].Input != 0 || got[1].Model != "opus" {
		t.Errorf("campos ausentes quedan en 0: %+v", got[1])
	}
	if n := len(ParseCalls(strings.NewReader(""))); n != 0 {
		t.Errorf("vacío: %d", n)
	}
}

func TestRunsMergesSameMessage(t *testing.T) {
	// m1 se leyó en dos pasadas del hook: la primera con su hora, fase y modelo.
	first := call("claude:a", "implementer", "m1", 3, flow.Implementing, 2, 300, 1000, 10)
	second := call("claude:a", "implementer", "m1", 8, flow.Quality, 0, 0, 0, 30)
	second.Model = "otro"
	other := call("claude:a", "implementer", "m2", 5, flow.Implementing, 1, 0, 2000, 5)
	runs := Runs([]Call{first, other, second})
	if len(runs) != 1 || len(runs[0].Rows) != 2 {
		t.Fatalf("m1 en dos líneas es una sola fila: %+v", runs)
	}
	r := runs[0].Rows[0]
	if r.Msg != "m1" || r.Input != 2 || r.CacheWrite != 300 || r.CacheRead != 1000 || r.Output != 40 {
		t.Errorf("tokens sumados: %+v", r)
	}
	if !r.TS.Equal(at(3)) || r.Phase != flow.Implementing || r.Model != "opus" {
		t.Errorf("hora, fase y modelo de la primera: %+v", r)
	}
	if r.Context != 1302 {
		t.Errorf("contexto = entrada + releído + escrito: %d", r.Context)
	}
	// Mismo msg en otra corrida no se fusiona.
	runs = Runs([]Call{first, call("claude:b", "reviewer", "m1", 4, flow.Quality, 1, 0, 1, 1)})
	if len(runs) != 2 {
		t.Errorf("el mismo msg en corridas distintas son filas distintas: %d corridas", len(runs))
	}
}

func TestRunsAccumulatesPerRun(t *testing.T) {
	calls := []Call{
		call("claude:main", "main", "a3", 6, flow.Implementing, 1, 0, 300, 4), // desordenada a propósito
		call("claude:main", "main", "a1", 1, flow.Spec, 10, 100, 0, 1),
		call("claude:main", "main", "a2", 2, flow.Spec, 1, 0, 200, 2),
		call("claude:agent-x", "implementer", "b1", 3, flow.Implementing, 5, 50, 0, 7),
		call("claude:agent-x", "implementer", "b2", 4, flow.Implementing, 1, 0, 500, 3),
	}
	runs := Runs(calls)
	if len(runs) != 2 || runs[0].Run != "claude:main" || runs[1].Run != "claude:agent-x" {
		t.Fatalf("corridas en orden de su primera llamada: %+v", runs)
	}
	m := runs[0]
	var ns []int
	var read, nw []int64
	for _, r := range m.Rows {
		ns = append(ns, r.N)
		read = append(read, r.AccRead)
		nw = append(nw, r.AccNew)
	}
	if !reflect.DeepEqual(ns, []int{1, 2, 3}) || m.Rows[0].Msg != "a1" || m.Rows[2].Msg != "a3" {
		t.Errorf("filas por hora, numeradas desde 1: %v %+v", ns, m.Rows)
	}
	if !reflect.DeepEqual(read, []int64{0, 200, 500}) || !reflect.DeepEqual(nw, []int64{111, 114, 119}) {
		t.Errorf("acumulados de la corrida: releído %v nuevo %v", read, nw)
	}
	x := runs[1]
	if x.Rows[0].N != 1 || x.Rows[0].AccRead != 0 || x.Rows[0].AccNew != 62 || x.Rows[1].AccRead != 500 || x.Rows[1].AccNew != 66 {
		t.Errorf("los acumulados reinician en cada corrida: %+v", x.Rows)
	}
	if m.Total.Calls != 3 || m.Total.CacheRead != 500 || m.Total.New() != 119 || m.Total.MaxContext != 301 {
		t.Errorf("total de la corrida: %+v", m.Total)
	}
	if m.FinalContext != 301 || !m.Last.Equal(at(6)) || m.Phase != flow.Spec || m.Agent != "main" {
		t.Errorf("contexto final = el de la última fila, última hora, fase de la primera: %+v", m)
	}
	if len(Runs(nil)) != 0 {
		t.Error("sin llamadas no hay corridas")
	}
}

func TestRunsLabels(t *testing.T) {
	type got struct{ name, stage, label string }
	read := func(calls ...Call) map[string]got {
		out := map[string]got{}
		for _, r := range Runs(calls) {
			out[r.Run] = got{r.Name, r.Stage, r.Label}
		}
		return out
	}
	// R1, R3: fase única; varias fases "primera → última"; primera = última; sin fase.
	g := read(
		call("claude:i", "implementer", "a", 1, flow.Implementing, 1, 0, 0, 1),
		call("claude:i", "implementer", "b", 2, flow.Implementing, 1, 0, 0, 1),
		call("claude:m", "main", "a", 3, flow.Discovery, 1, 0, 0, 1),
		call("claude:m", "main", "b", 4, flow.Spec, 1, 0, 0, 1),
		call("claude:m", "main", "c", 5, flow.Contract, 1, 0, 0, 1),
		call("claude:s", "spec-author", "a", 6, flow.Spec, 1, 0, 0, 1),
		call("claude:s", "spec-author", "b", 7, flow.Contract, 1, 0, 0, 1),
		call("claude:s", "spec-author", "c", 8, flow.Spec, 1, 0, 0, 1),
		call("claude:x", "?", "a", 9, "", 1, 0, 0, 1),
	)
	for run, want := range map[string]got{
		"claude:i": {"implementer", "implementing", "implementer · implementing"},
		"claude:m": {"sesión principal", "discovery → contract", "sesión principal · discovery → contract"},
		"claude:s": {"spec-author", "spec", "spec-author · spec"},
		"claude:x": {"?", "sin fase", "? · sin fase"},
	} {
		if g[run] != want {
			t.Errorf("%s: %+v, want %+v", run, g[run], want)
		}
	}
	// R2: "#N" solo si se repiten Name y Stage, numerado por la primera llamada.
	g = read(
		call("claude:s2", "main", "a", 4, flow.Spec, 1, 0, 0, 1),
		call("claude:s1", "main", "a", 1, flow.Spec, 1, 0, 0, 1),
		call("claude:c", "main", "a", 2, flow.Contract, 1, 0, 0, 1),
		call("claude:q", "implementer", "a", 3, flow.Spec, 1, 0, 0, 1),
		call("claude:s3", "main", "a", 6, flow.Spec, 1, 0, 0, 1),
	)
	for run, want := range map[string]string{
		"claude:s1": "sesión principal · spec #1", "claude:s2": "sesión principal · spec #2", "claude:s3": "sesión principal · spec #3",
		"claude:c": "sesión principal · contract", "claude:q": "implementer · spec",
	} {
		if g[run].label != want {
			t.Errorf("%s: label %q, want %q", run, g[run].label, want)
		}
	}
	if g["claude:s2"].stage != "spec #2" {
		t.Errorf("el número va en Stage: %+v", g["claude:s2"])
	}
}

func TestPhasesMatchesAllot(t *testing.T) {
	log := pilotLog()
	// Agente con reporte: todas a la fase de su último reporte.
	got := Phases(log, "T-1", "implementer", []Sample{sample(3, "opus", 1), sample(8, "opus", 1), sample(9, "opus", 1)})
	if want := []flow.Phase{flow.Implementing, flow.Implementing, flow.Implementing}; !reflect.DeepEqual(got, want) {
		t.Errorf("implementer: %v", got)
	}
	// Agente sin reporte: la fase en que empezó, para todas.
	got = Phases(log, "T-1", "Explore", []Sample{sample(4, "haiku", 1), sample(12, "haiku", 1)})
	if want := []flow.Phase{flow.Implementing, flow.Implementing}; !reflect.DeepEqual(got, want) {
		t.Errorf("sin reporte: %v", got)
	}
	// Sesión principal: la fase de cada llamada.
	ss := []Sample{sample(-1, "opus", 1), sample(1, "opus", 2), sample(2, "opus", 4), sample(5, "sonnet", 8), sample(10, "opus", 16), sample(30, "opus", 32)}
	got = Phases(log, "T-1", "", ss)
	want := []flow.Phase{flow.Spec, flow.Spec, flow.Implementing, flow.Implementing, flow.Quality, flow.Documenting}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sesión principal: %v, want %v", got, want)
	}
	// Una fase por muestra y mismo reparto que Allot.
	perPhase := map[flow.Phase]int64{}
	for i, p := range got {
		perPhase[p] += ss[i].Output
	}
	for _, sh := range Allot(log, "T-1", "", ss) {
		perPhase[sh.Phase] -= sh.Output
	}
	for p, left := range perPhase {
		if left != 0 {
			t.Errorf("Phases y Allot difieren en %s: %d", p, left)
		}
	}
	if len(Phases(log, "T-1", "", nil)) != 0 {
		t.Error("sin muestras no hay fases")
	}
}
