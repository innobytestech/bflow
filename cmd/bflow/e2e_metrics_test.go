package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transcriptLine(id string, in, out, cr, cw int) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"id": id,
		"usage": map[string]any{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cr, "cache_creation_input_tokens": cw}}})
	return string(b) + "\n"
}

func TestStatsTokensAndStatusline(t *testing.T) {
	r := newRepo(t)
	id := r.ok("task", "add", "Borrador pre-folio").Data["id"].(string)
	r.ok("start", id, "--lane", "full")
	os.WriteFile(filepath.Join(r.dir, "d.md"), []byte("d"), 0o644)
	r.ok("approve", id, "--file", "d.md")
	r.ok("report", id, "--agent", "spec-author", "--verdict", "READY")

	// Tokens del spec-author: transcript principal y de un subagente.
	tr := filepath.Join(r.dir, "sess.jsonl")
	os.WriteFile(tr, []byte(transcriptLine("m1", 10, 500, 20000, 3000)+transcriptLine("m1", 10, 900, 20000, 3000)), 0o644)
	os.MkdirAll(filepath.Join(r.dir, "sess", "subagents"), 0o755)
	os.WriteFile(filepath.Join(r.dir, "sess", "subagents", "agent-x.jsonl"), []byte(transcriptLine("s1", 5, 100, 10000, 0)), 0o644)
	hookIn := fmt.Sprintf(`{"hook_event_name":"SubagentStop","transcript_path":%q}`, tr)
	out, _, code := r.hook(hookIn, "hook", "tokens")
	if code != 0 || out != "" {
		t.Fatalf("hook tokens debe ser silencioso: %d %q", code, out)
	}
	r.hook(hookIn, "hook", "tokens") // sin nada nuevo no suma otra vez

	r.ok("reject", id, "--note", "falta concurrencia")
	r.ok("report", id, "--agent", "spec-author", "--verdict", "READY")
	r.ok("approve", id)
	r.ok("report", id, "--agent", "implementer", "--verdict", "CONTRACT_READY")
	r.ok("approve", id)
	r.ok("report", id, "--agent", "implementer", "--verdict", "NEEDS_DECISION", "--note", "¿409?", "--option", "a", "--option", "b")
	r.ok("approve", id, "--choice", "1")
	r.ok("report", id, "--agent", "implementer", "--verdict", "DONE")
	r.ok("approve", id)
	r.ok("report", id, "--agent", "reviewer", "--verdict", "APPROVED")
	r.ok("report", id, "--agent", "security-auditor", "--verdict", "REJECTED")

	st := r.ok("stats", id).Data["stats"].(map[string]any)
	num := func(k string) float64 { return st[k].(float64) }
	if num("rejections") != 1 || num("rounds") != 1 || num("decisions") != 1 || st["phase"] != "implementing" {
		t.Errorf("iteraciones: %v", st)
	}
	if num("agent_ns")+num("human_ns")+num("blocked_ns") != num("total_ns") || num("total_ns") <= 0 {
		t.Errorf("los tiempos deben cuadrar: %v", st)
	}
	tok := st["tokens"].(map[string]any)
	if st["tokens_available"] != true || tok["output"].(float64) != 1000 || tok["cache_read"].(float64) != 30000 {
		t.Errorf("tokens (m1 contado una vez con su uso final + subagente): %v", tok)
	}
	phases := st["phases"].([]any)
	specTok := 0.0
	for _, p := range phases {
		pm := p.(map[string]any)
		if pm["phase"] == "spec" {
			specTok = pm["tokens"].(map[string]any)["output"].(float64)
		}
	}
	if specTok != 1000 {
		t.Errorf("los tokens se atribuyen a la fase activa (spec): %v", phases)
	}

	line, _, _ := r.hook(fmt.Sprintf(`{"workspace":{"current_dir":%q}}`, r.dir), "statusline")
	if !strings.HasPrefix(line, id+" · implementing") || !strings.Contains(line, "ronda 1") {
		t.Errorf("statusline: %q", line)
	}
	if text, _, _ := r.hook("", "stats", id); !strings.Contains(text, "caché leída") {
		t.Errorf("stats texto:\n%s", text)
	}
	if w := r.ok("watch", "--once").Data; w == nil {
		t.Error("watch --once")
	}
}
