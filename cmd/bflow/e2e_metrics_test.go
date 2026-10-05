package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/metrics"
)

func transcriptLine(id string, in, out, cr, cw int) string {
	return transcriptLineAt(time.Now(), id, in, out, cr, cw)
}

func transcriptLineAt(ts time.Time, id string, in, out, cr, cw int) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": ts.UTC().Format(time.RFC3339Nano), "message": map[string]any{"id": id,
		"usage": map[string]any{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cr, "cache_creation_input_tokens": cw}}})
	return string(b) + "\n"
}

func TestStatsTokensAndStatusline(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	id := r.ok("task", "add", "Borrador pre-folio").Data["id"].(string)
	r.ok("start", id, "--lane", "full")
	r.writeMarks(metrics.Mark{TS: time.Now(), Session: "sess", ID: id}) // la sesión principal condujo la tarea
	os.WriteFile(filepath.Join(r.dir, "d.md"), []byte("d"), 0o644)
	r.ok("approve", id, "--file", "d.md")
	r.ok("report", id, "--agent", "spec-author", "--verdict", "READY")

	// La sesión principal (Stop) y el spec-author (SubagentStop) llegan cada
	// uno por su hook y solo con su transcript.
	tr := filepath.Join(r.dir, "sess.jsonl")
	subs := filepath.Join(r.dir, "sess", "subagents")
	os.MkdirAll(subs, 0o755)
	os.WriteFile(tr, []byte(transcriptLine("m1", 10, 500, 20000, 3000)+transcriptLine("m1", 10, 900, 20000, 3000)), 0o644)
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"sess","transcript_path":%q}`, tr)
	subStop := func(agent, file string) string {
		return fmt.Sprintf(`{"hook_event_name":"SubagentStop","session_id":"sess","transcript_path":%q,"agent_id":"x","agent_type":"bflow-%s","agent_transcript_path":%q}`,
			tr, agent, filepath.Join(subs, file))
	}
	os.WriteFile(filepath.Join(subs, "agent-s.jsonl"), []byte(transcriptLine("s1", 5, 100, 10000, 0)), 0o644)
	out, _, code := r.hook(subStop("spec-author", "agent-s.jsonl"), "hook", "tokens")
	if code != 0 || out != "" {
		t.Fatalf("hook tokens debe ser silencioso: %d %q", code, out)
	}
	r.hook(stop, "hook", "tokens")
	r.hook(stop, "hook", "tokens") // sin nada nuevo no suma otra vez

	r.ok("reject", id, "--note", "falta concurrencia")
	r.ok("report", id, "--agent", "spec-author", "--verdict", "READY")
	r.ok("approve", id)
	r.ok("report", id, "--agent", "implementer", "--verdict", "CONTRACT_READY")
	r.ok("approve", id)
	time.Sleep(20 * time.Millisecond)
	inImpl := time.Now() // la sesión principal responde ya en implementing
	time.Sleep(20 * time.Millisecond)
	r.ok("report", id, "--agent", "implementer", "--verdict", "NEEDS_DECISION", "--note", "¿409?", "--option", "a", "--option", "b")
	r.ok("approve", id, "--choice", "1")
	r.ok("report", id, "--agent", "implementer", "--verdict", "DONE")

	// El hook del implementer corre cuando la tarea ya pasó a quality: sus
	// tokens son de implementing. La respuesta de la sesión principal se
	// registra al terminar el turno, pero ocurrió en implementing.
	os.WriteFile(filepath.Join(subs, "agent-i.jsonl"), []byte(transcriptLine("i1", 3, 2000, 50000, 5000)), 0o644)
	r.hook(subStop("implementer", "agent-i.jsonl"), "hook", "tokens")
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(transcriptLineAt(inImpl, "m2", 1, 40, 8000, 0))
	f.Close()
	r.hook(stop, "hook", "tokens")

	r.ok("approve", id)
	r.ok("report", id, "--agent", "reviewer", "--verdict", "REJECTED")

	st := r.ok("stats", id).Data["stats"].(map[string]any)
	num := func(k string) float64 { return st[k].(float64) }
	if num("rejections") != 1 || num("rounds") != 1 || num("decisions") != 1 || st["phase"] != "implementing" {
		t.Errorf("iteraciones: %v", st)
	}
	if num("agent_ns")+num("human_ns")+num("blocked_ns") != num("total_ns") || num("total_ns") <= 0 {
		t.Errorf("los tiempos deben cuadrar: %v", st)
	}
	tok := st["tokens"].(map[string]any)
	if st["tokens_available"] != true || tok["output"].(float64) != 3040 || tok["cache_read"].(float64) != 88000 {
		t.Errorf("tokens (m1 contado una vez con su uso final): %v", tok)
	}
	byPhase := map[string]float64{}
	for _, p := range st["phases"].([]any) {
		pm := p.(map[string]any)
		byPhase[pm["phase"].(string)] = pm["tokens"].(map[string]any)["output"].(float64)
	}
	if byPhase["spec"] != 1000 || byPhase["implementing"] != 2040 || byPhase["quality"] != 0 || byPhase["contract"] != 0 {
		t.Errorf("cada agente en la fase donde trabajó: %v", byPhase)
	}
	agents := st["agents"].(map[string]any)
	outOf := func(a string) float64 { return agents[a].(map[string]any)["output"].(float64) }
	if len(agents) != 3 || outOf("main") != 940 || outOf("spec-author") != 100 || outOf("implementer") != 2000 {
		t.Errorf("por agente: %v", agents)
	}

	line, _, _ := r.hook(fmt.Sprintf(`{"workspace":{"current_dir":%q}}`, r.dir), "statusline")
	if !strings.HasPrefix(line, id+" · implementing") || !strings.Contains(line, "ronda 1") || !strings.HasSuffix(strings.TrimSpace(line), "11.1k nuevos · 88.0k caché") {
		t.Errorf("statusline: %q", line)
	}
	text, _, _ := r.hook("", "stats", id)
	for _, want := range []string{"11.1k nuevos · 88.0k releídos de caché en 4 llamadas", "sesión principal", "implementer", "caché escrita 8.0k", "ctx máx", "ctx final"} {
		if !strings.Contains(text, want) {
			t.Errorf("stats texto sin %q:\n%s", want, text)
		}
	}
	if w := r.ok("watch", "--once").Data; w == nil {
		t.Error("watch --once")
	}
}

func TestQualityFrictionAndModelMetrics(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	feat := r.ok("task", "add", "Alta de clientes").Data["id"].(string)
	r.ok("start", feat, "--lane", "light")
	r.writeMarks(metrics.Mark{TS: time.Now(), Session: "sess", ID: feat})

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
	r.hook(fmt.Sprintf(`{"hook_event_name":"Stop","session_id":"sess","transcript_path":%q}`, tr), "hook", "tokens")
	r.hook(fmt.Sprintf(`{"hook_event_name":"SubagentStop","session_id":"sess","transcript_path":%q,"agent_id":"x","agent_type":"Explore","agent_transcript_path":%q}`,
		tr, filepath.Join(r.dir, "sess", "subagents", "agent-x.jsonl")), "hook", "tokens")

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
	for _, want := range []string{"rechazos por gate: spec 1", "hotfixes: " + fix, "fricción: 1", "claude-haiku-4-5            100"} {
		if !strings.Contains(text, want) {
			t.Errorf("stats texto sin %q:\n%s", want, text)
		}
	}
}

func TestWatchOpen(t *testing.T) {
	r := newRepo(t)
	t.Setenv("CI", "true")
	r.writeConfig("ui: { watch: true }\n")
	id := r.ok("task", "add", "Panel").Data["id"].(string)
	// En CI start no abre ventanas ni avisa.
	if env := r.ok("start", id, "--lane", "light"); env.Data["warnings"] != nil {
		t.Errorf("start en CI: %v", env.Data)
	}
	if env := r.run("watch", "--open"); env.exit != 1 || env.Code != "no_terminal" {
		t.Errorf("watch --open en CI: exit %d %s", env.exit, env.Code)
	}
	// Con un panel vivo, --open no abre otro (y no llega a buscar terminal).
	alive := filepath.Join(r.dir, ".bflow", "cache", "watch.alive")
	os.MkdirAll(filepath.Dir(alive), 0o755)
	os.WriteFile(alive, []byte("2000"), 0o644)
	if env := r.ok("watch", "--open"); env.Data["watch"] != "already" {
		t.Errorf("con panel abierto: %v", env.Data)
	}
	text, _, _ := r.hook("", "watch", "--once")
	if !strings.Contains(text, "AHORA    AGENTE  spec-author escribe la spec") || !strings.Contains(text, "DESPUÉS  TÚ      aprobar la spec") || !strings.Contains(text, "[spec] > implementing") {
		t.Errorf("watch --once:\n%s", text)
	}
}
