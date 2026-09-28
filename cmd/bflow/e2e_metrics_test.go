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

func TestQualityFrictionAndModelMetrics(t *testing.T) {
	r := newRepo(t)
	feat := r.ok("task", "add", "Alta de clientes").Data["id"].(string)
	r.ok("start", feat, "--lane", "light")

	// Fricción: un pedido que el flujo rechaza y una acción que bloquea guard.
	if env := r.run("approve", feat); env.exit != 2 {
		t.Fatalf("aprobar sin gate pendiente debe rechazarse: exit %d", env.exit)
	}
	if _, _, code := r.hook(preToolUse(r.dir, "Bash", map[string]any{"command": "git push --force origin main"}), "guard"); code != 2 {
		t.Fatalf("guard debe bloquear el push forzado: exit %d", code)
	}

	// Tokens de dos modelos: la sesión principal y un subagente.
	tr := filepath.Join(r.dir, "sess.jsonl")
	line := func(id, model string, out int) string {
		b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"id": id, "model": model,
			"usage": map[string]any{"input_tokens": 1, "output_tokens": out}}})
		return string(b) + "\n"
	}
	os.WriteFile(tr, []byte(line("m1", "claude-opus-5-5", 300)), 0o644)
	os.MkdirAll(filepath.Join(r.dir, "sess", "subagents"), 0o755)
	os.WriteFile(filepath.Join(r.dir, "sess", "subagents", "agent-x.jsonl"), []byte(line("s1", "claude-haiku-4-5", 99)), 0o644)
	r.hook(fmt.Sprintf(`{"hook_event_name":"Stop","transcript_path":%q}`, tr), "hook", "tokens")

	r.ok("report", feat, "--agent", "spec-author", "--verdict", "READY")
	r.ok("reject", feat, "--note", "falta el RFC genérico")

	// Hotfix ligado a la feature.
	fix := r.ok("task", "add", "Corrige alta").Data["id"].(string)
	if env := r.run("start", fix, "--lane", "light", "--fixes", feat); env.exit != 2 || env.Code != "fixes_needs_hotfix" {
		t.Errorf("--fixes fuera de hotfix: exit %d code %s", env.exit, env.Code)
	}
	r.ok("start", fix, "--lane", "hotfix", "--fixes", strings.ToLower(feat))

	st := r.ok("stats", feat).Data["stats"].(map[string]any)
	if st["refused"].(float64) != 1 || st["guarded"].(float64) != 1 {
		t.Errorf("fricción: refused %v guarded %v", st["refused"], st["guarded"])
	}
	if g := st["rejections_by_gate"].(map[string]any); g["spec"].(float64) != 1 {
		t.Errorf("rechazos por gate: %v", g)
	}
	if h := st["hotfixes"].([]any); len(h) != 1 || h[0] != fix {
		t.Errorf("hotfixes: %v", h)
	}
	models := st["models"].(map[string]any)
	opus := models["claude-opus-5-5"].(map[string]any)
	haiku := models["claude-haiku-4-5"].(map[string]any)
	if opus["output"].(float64) != 300 || haiku["output"].(float64) != 99 || len(models) != 2 {
		t.Errorf("tokens por modelo: %v", models)
	}
	text, _, _ := r.hook("", "stats", feat)
	for _, want := range []string{"rechazos por gate: spec 1", "hotfixes: " + fix, "fricción: 1", "por modelo: claude-haiku-4-5"} {
		if !strings.Contains(text, want) {
			t.Errorf("stats texto sin %q:\n%s", want, text)
		}
	}
}
