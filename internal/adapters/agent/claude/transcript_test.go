package claude

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/testutil"
)

const (
	opus  = "claude-opus-5-5"
	haiku = "claude-haiku-4-5-20251001"
)

func line(id, model string, in, out, cr, cw int) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "isSidechain": false, "message": map[string]any{"id": id, "model": model,
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

func TestReadUsageIncrementalWithDuplicatesAndSubagents(t *testing.T) {
	dir := testutil.TempDir(t)
	main := filepath.Join(dir, "sess-1.jsonl")
	appendTo(t, main, `{"type":"user","message":{"content":"hola"}}`+"\n")
	appendTo(t, main, line("m1", opus, 2, 10, 1000, 300))
	appendTo(t, main, line("m1", opus, 2, 40, 1000, 300)) // mismo mensaje, uso final mayor
	appendTo(t, main, line("m2", opus, 1, 5, 2000, 0))
	appendTo(t, main, `{"type":"assistant","message":{"id":"m3","model":"`+opus+`","usage":{"input_tokens":`) // línea incompleta: aún se está escribiendo

	var a Agent
	cur := &metrics.Cursor{}
	u, err := a.ReadUsage(main, cur)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]metrics.Usage{opus: {Input: 3, Output: 45, CacheRead: 3000, CacheWrite: 300}}
	if !maps.Equal(u, want) {
		t.Fatalf("primera lectura %+v, want %+v", u, want)
	}

	// Se completa la línea, llega otra copia de m2 igual y un subagente.
	appendTo(t, main, `1,"output_tokens":7,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`+"\n")
	appendTo(t, main, line("m2", opus, 1, 5, 2000, 0))
	sub := filepath.Join(dir, "sess-1", "subagents")
	os.MkdirAll(sub, 0o755)
	appendTo(t, filepath.Join(sub, "agent-abc.jsonl"), line("s1", haiku, 3, 100, 5000, 1000))
	u, err = a.ReadUsage(main, cur)
	if err != nil {
		t.Fatal(err)
	}
	want = map[string]metrics.Usage{opus: {Input: 1, Output: 7}, haiku: {Input: 3, Output: 100, CacheRead: 5000, CacheWrite: 1000}}
	if !maps.Equal(u, want) {
		t.Fatalf("segunda lectura %+v, want %+v (m3 completo + subagente con su modelo, sin recontar m2)", u, want)
	}
	if u, _ := a.ReadUsage(main, cur); len(u) != 0 {
		t.Errorf("sin nada nuevo no suma: %+v", u)
	}
}

func TestTranscriptPath(t *testing.T) {
	var a Agent
	if p := a.TranscriptPath([]byte(`{"session_id":"x","transcript_path":"C:/t/s.jsonl","hook_event_name":"Stop"}`)); p != "C:/t/s.jsonl" {
		t.Errorf("%q", p)
	}
	if p := a.TranscriptPath([]byte(`nada`)); p != "" {
		t.Errorf("%q", p)
	}
}
