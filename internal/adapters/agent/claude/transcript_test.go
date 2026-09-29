package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/testutil"
)

const (
	opus  = "claude-opus-5-5"
	haiku = "claude-haiku-4-5-20251001"
)

func line(id, model string, in, out, cr, cw int) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": "2026-09-28T22:35:00Z", "message": map[string]any{"id": id, "model": model,
		"usage": map[string]any{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cr, "cache_creation_input_tokens": cw}}})
	return string(b) + "\n"
}

func appendTo(t *testing.T, path, s string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

func sum(ss []metrics.Sample) map[string]metrics.Usage {
	out := map[string]metrics.Usage{}
	for _, s := range ss {
		u := out[s.Model]
		u.Add(s.Usage)
		out[s.Model] = u
	}
	return out
}

func TestReadUsageIncrementalWithDuplicates(t *testing.T) {
	dir := testutil.TempDir(t)
	main := filepath.Join(dir, "sess-1.jsonl")
	appendTo(t, main, `{"type":"user","message":{"content":"hola"}}`+"\n")
	appendTo(t, main, line("m1", opus, 2, 10, 1000, 300))
	appendTo(t, main, line("m1", opus, 2, 40, 1000, 300)) // mismo mensaje, uso final mayor
	appendTo(t, main, line("m2", opus, 1, 5, 2000, 0))
	appendTo(t, main, `{"type":"assistant","timestamp":"2026-09-28T22:40:00Z","message":{"id":"m3","model":"`+opus+`","usage":{"input_tokens":`) // línea incompleta: aún se está escribiendo

	var a Agent
	cur := &metrics.Cursor{}
	u, err := a.ReadUsage(main, cur)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]metrics.Usage{opus: {Input: 3, Output: 45, CacheRead: 3000, CacheWrite: 300}}
	if got := sum(u); !reflect.DeepEqual(got, want) {
		t.Fatalf("primera lectura %+v, want %+v", got, want)
	}

	// Se completa la línea y llega otra copia de m2 igual. Los subagentes no se
	// leen aquí: cada uno llega por su propio hook.
	appendTo(t, main, `1,"output_tokens":7,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`+"\n")
	appendTo(t, main, line("m2", opus, 1, 5, 2000, 0))
	sub := filepath.Join(dir, "sess-1", "subagents")
	os.MkdirAll(sub, 0o755)
	appendTo(t, filepath.Join(sub, "agent-abc.jsonl"), line("s1", haiku, 3, 100, 5000, 1000))
	u, err = a.ReadUsage(main, cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 1 || u[0].Usage != (metrics.Usage{Input: 1, Output: 7}) || !u[0].TS.Equal(time.Date(2026, 9, 28, 22, 40, 0, 0, time.UTC)) {
		t.Fatalf("segunda lectura %+v: solo m3 completo, con su hora", u)
	}
	if u, _ := a.ReadUsage(main, cur); len(u) != 0 {
		t.Errorf("sin nada nuevo no suma: %+v", u)
	}
	if u, err := a.ReadUsage(filepath.Join(dir, "no-existe.jsonl"), cur); err != nil || len(u) != 0 {
		t.Errorf("transcript inexistente: %v %v", u, err)
	}
}

func TestTokenSource(t *testing.T) {
	var a Agent
	for _, c := range []struct{ in, path, agent string }{
		{`{"session_id":"x","transcript_path":"C:/t/s.jsonl","hook_event_name":"Stop"}`, "C:/t/s.jsonl", ""},
		{`{"transcript_path":"C:/t/s.jsonl","hook_event_name":"SubagentStop","agent_id":"a1","agent_type":"bflow-implementer","agent_transcript_path":"C:/t/s/subagents/agent-a1.jsonl"}`,
			"C:/t/s/subagents/agent-a1.jsonl", "bflow-implementer"},
		// Sin agent_transcript_path se arma con la convención de Claude Code.
		{`{"transcript_path":"C:/t/s.jsonl","agent_id":"a2","agent_type":"Explore"}`, filepath.Join("C:/t/s", "subagents", "agent-a2.jsonl"), "Explore"},
		{`nada`, "", ""},
	} {
		if p, ag := a.TokenSource([]byte(c.in)); p != c.path || ag != c.agent {
			t.Errorf("%s → %q %q, want %q %q", c.in, p, ag, c.path, c.agent)
		}
	}
}
