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

// GH-51: los tokens van a la tarea que la sesión condujo (marcas), no a la activa.

func (r *repo) bflowPath(parts ...string) string {
	return filepath.Join(append([]string{r.dir, ".bflow"}, parts...)...)
}

// writeMarks deja las marcas directo en sessions.jsonl, sin pasar por el guard.
func (r *repo) writeMarks(marks ...metrics.Mark) {
	r.t.Helper()
	p := r.bflowPath("cache", "sessions.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range marks {
		l, _ := json.Marshal(m)
		b.Write(l)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) startTask(title string) string {
	r.t.Helper()
	id := r.ok("task", "add", title).Data["id"].(string)
	r.ok("start", id, "--lane", "light")
	return id
}

func stopJSON(session, transcript string) string {
	return fmt.Sprintf(`{"hook_event_name":"Stop","session_id":%q,"transcript_path":%q}`, session, transcript)
}

func subStopJSON(session, agentID, transcript, agentTranscript string) string {
	return fmt.Sprintf(`{"hook_event_name":"SubagentStop","session_id":%q,"agent_id":%q,"agent_type":"bflow-implementer","transcript_path":%q,"agent_transcript_path":%q}`,
		session, agentID, transcript, agentTranscript)
}

func (r *repo) writeTranscript(name string, lines ...string) string {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "")), 0o644); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// taskOutput es la salida (tokens) que stats le cuenta a la tarea.
func (r *repo) taskOutput(id string) float64 {
	r.t.Helper()
	st := r.ok("stats", id).Data["stats"].(map[string]any)
	return st["tokens"].(map[string]any)["output"].(float64)
}

// unassignedCalls devuelve las líneas de .bflow/metrics/calls.jsonl.
func (r *repo) unassignedCalls() []map[string]any {
	r.t.Helper()
	raw, err := os.ReadFile(r.bflowPath("metrics", "calls.jsonl"))
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func (r *repo) marks() []metrics.Mark {
	return metrics.ReadMarks(r.bflowPath("cache", "sessions.jsonl"))
}

func guardJSON(dir, session, tool string, input map[string]any) string {
	b, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": dir, "session_id": session, "tool_name": tool, "tool_input": input})
	return string(b)
}

func TestGuardMarksSession(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	id := r.startTask("Marcas de sesión")

	// R1: un comando bflow con el ID de una tarea existente marca la sesión.
	if _, _, code := r.hook(guardJSON(r.dir, "s1", "Bash", map[string]any{"command": "bflow status " + id}), "guard"); code != 0 {
		t.Fatalf("el guard no bloquea por marcar: %d", code)
	}
	ms := r.marks()
	if len(ms) != 1 || ms[0].Session != "s1" || ms[0].ID != id || time.Since(ms[0].TS) > time.Minute {
		t.Fatalf("marca de bash: %+v", ms)
	}
	// Sin tarea con ese ID, sin ID o sin ser bflow: no marca.
	for _, cmd := range []string{"bflow status ZZ-999", "bflow status", "git status " + id, "echo bflow status " + id} {
		r.hook(guardJSON(r.dir, "s2", "Bash", map[string]any{"command": cmd}), "guard")
	}
	if ms := r.marks(); len(ms) != 1 {
		t.Errorf("solo la primera acción marca: %+v", ms)
	}
	// El subagente marca con su llave session:agent.
	b, _ := json.Marshal(map[string]any{"cwd": r.dir, "session_id": "s3", "agent_id": "a1", "agent_type": "bflow-implementer",
		"tool_name": "Bash", "tool_input": map[string]any{"command": "bflow report " + id + " --agent implementer --verdict DONE"}})
	r.hook(string(b), "guard")
	if ms := r.marks(); len(ms) != 2 || ms[1].Session != "s3:a1" || ms[1].ID != id {
		t.Errorf("marca de subagente: %+v", ms)
	}

	// R2: el lanzamiento de un bflow-* con "id":"<ID>" marca y siempre se permite.
	for i, tool := range []string{"Task", "Agent"} {
		sess := fmt.Sprintf("sp%d", i)
		_, _, code := r.hook(guardJSON(r.dir, sess, tool, map[string]any{"subagent_type": "bflow-spec-author", "prompt": `{"id":"` + id + `","lane":"light"}`}), "guard")
		if code != 0 {
			t.Fatalf("%s: un lanzamiento se permite: %d", tool, code)
		}
	}
	if ms := r.marks(); len(ms) != 4 || ms[2].Session != "sp0" || ms[3].Session != "sp1" || ms[3].ID != id {
		t.Errorf("marcas de lanzamientos: %+v", ms)
	}
	// Otro tipo de subagente, o un ID que no existe, no marca.
	r.hook(guardJSON(r.dir, "sp9", "Task", map[string]any{"subagent_type": "Explore", "prompt": `{"id":"` + id + `"}`}), "guard")
	r.hook(guardJSON(r.dir, "sp9", "Task", map[string]any{"subagent_type": "bflow-reviewer", "prompt": `{"id":"ZZ-999"}`}), "guard")
	if ms := r.marks(); len(ms) != 4 {
		t.Errorf("no deben marcar: %+v", ms)
	}
}

func TestHookTokensOnlyMarkedSession(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	id := r.startTask("Solo la que la condujo") // única tarea en curso: antes recibía todo
	base := time.Now().Add(-time.Hour)
	r.writeMarks(metrics.Mark{TS: base, Session: "con-marca", ID: id})
	a := r.writeTranscript("a.jsonl", transcriptLineAt(base.Add(time.Minute), "ma", 1, 100, 1000, 0))
	b := r.writeTranscript("b.jsonl", transcriptLineAt(base.Add(time.Minute), "mb", 1, 7, 1000, 0))
	r.hook(stopJSON("sin-marca", b), "hook", "tokens")
	r.hook(stopJSON("con-marca", a), "hook", "tokens")
	if got := r.taskOutput(id); got != 100 {
		t.Errorf("solo suma la sesión que condujo la tarea: output %v", got)
	}
	calls := r.unassignedCalls()
	if len(calls) != 1 || calls[0]["msg"] != "mb" || calls[0]["phase"] != "" {
		t.Errorf("la otra sesión queda sin tarea: %v", calls)
	}
}

func TestHookTokensUnassignedWithoutActive(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	tr := r.writeTranscript("s.jsonl", transcriptLine("m1", 2, 30, 500, 0))
	out, _, code := r.hook(stopJSON("s1", tr), "hook", "tokens")
	if code != 0 || out != "" {
		t.Fatalf("silencioso: %d %q", code, out)
	}
	calls := r.unassignedCalls()
	if len(calls) != 1 || calls[0]["msg"] != "m1" || calls[0]["agent"] != "main" || calls[0]["phase"] != "" || calls[0]["output"].(float64) != 30 {
		t.Fatalf("sin tarea activa no se pierde nada: %v", calls)
	}
	if raw, _ := os.ReadFile(r.bflowPath("log.jsonl")); strings.Contains(string(raw), `"tokens"`) {
		t.Errorf("sin tarea no se escribe en log.jsonl:\n%s", raw)
	}
	r.hook(stopJSON("s1", tr), "hook", "tokens")
	if got := len(r.unassignedCalls()); got != 1 {
		t.Errorf("el cursor avanzó: no se repite (%d líneas)", got)
	}
}

func TestHookTokensSplitByTime(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	ida := r.startTask("Primera")
	idb := r.startTask("Segunda")
	base := time.Now().Add(-2 * time.Hour)
	r.writeMarks(
		metrics.Mark{TS: base, Session: "s1", ID: ida},
		metrics.Mark{TS: base.Add(30 * time.Minute), Session: "s1", ID: idb},
	)
	tr := r.writeTranscript("s.jsonl",
		transcriptLineAt(base.Add(10*time.Minute), "m1", 1, 100, 0, 0),
		transcriptLineAt(base.Add(20*time.Minute), "m2", 1, 10, 0, 0),
		transcriptLineAt(base.Add(40*time.Minute), "m3", 1, 1000, 0, 0),
		transcriptLineAt(base.Add(-10*time.Minute), "m0", 1, 5000, 0, 0), // antes de toda marca
	)
	r.hook(stopJSON("s1", tr), "hook", "tokens")
	if a, b := r.taskOutput(ida), r.taskOutput(idb); a != 110 || b != 1000 {
		t.Errorf("cada llamada a la tarea de la última marca previa: A=%v B=%v", a, b)
	}
	if calls := r.unassignedCalls(); len(calls) != 1 || calls[0]["msg"] != "m0" {
		t.Errorf("sin marca previa: sin tarea: %v", calls)
	}
}

func TestHookTokensSubagentFollowsParent(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	ida := r.startTask("De la madre")
	idb := r.startTask("Propia")
	base := time.Now().Add(-time.Hour)
	r.writeMarks(
		metrics.Mark{TS: base, Session: "s1", ID: ida},
		metrics.Mark{TS: base.Add(10 * time.Minute), Session: "s1:own", ID: idb},
	)
	tr := r.writeTranscript("s.jsonl", transcriptLineAt(base, "m0", 1, 1, 0, 0))
	sub := func(file string, lines ...string) string {
		return r.writeTranscript(filepath.Join("s", "subagents", file), lines...)
	}
	// Sin marcas propias, sigue a la madre.
	p := sub("agent-kid.jsonl", transcriptLineAt(base.Add(5*time.Minute), "k1", 1, 40, 0, 0))
	r.hook(subStopJSON("s1", "kid", tr, p), "hook", "tokens")
	// Con marca propia va a la suya, aunque su llamada sea anterior a la marca.
	p = sub("agent-own.jsonl", transcriptLineAt(base.Add(5*time.Minute), "o1", 1, 900, 0, 0))
	r.hook(subStopJSON("s1", "own", tr, p), "hook", "tokens")
	if a, b := r.taskOutput(ida), r.taskOutput(idb); a != 40 || b != 900 {
		t.Errorf("subagente sin marcas sigue a la madre, con marcas va a la suya: A=%v B=%v", a, b)
	}
}

func TestHookTokensDroppedTaskUnassigned(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	r.startTask("Existe")
	base := time.Now().Add(-time.Hour)
	r.writeMarks(metrics.Mark{TS: base, Session: "s1", ID: "ZZ-404"}) // la tarea ya no existe
	tr := r.writeTranscript("s.jsonl", transcriptLineAt(base.Add(time.Minute), "m1", 1, 60, 0, 0))
	r.hook(stopJSON("s1", tr), "hook", "tokens")
	if calls := r.unassignedCalls(); len(calls) != 1 || calls[0]["output"].(float64) != 60 {
		t.Errorf("marca a una tarea que no se encuentra: sin tarea: %v", calls)
	}
	if _, err := os.Stat(r.bflowPath("tasks", "ZZ-404")); err == nil {
		t.Error("no debe crear la carpeta de una tarea inexistente")
	}
}

func TestHookTokensCursorHeldOnWriteError(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	tr := r.writeTranscript("s.jsonl", transcriptLine("m1", 1, 77, 0, 0))
	// .bflow/metrics es un archivo: escribir sin tarea falla.
	if err := os.MkdirAll(r.bflowPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.bflowPath("metrics"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := r.hook(stopJSON("s1", tr), "hook", "tokens")
	if code != 0 || out != "" {
		t.Fatalf("silencioso aun con error: %d %q", code, out)
	}
	if _, err := os.Stat(r.bflowPath("cache", "tokens-cursor.json")); err == nil {
		t.Fatal("el cursor no se guarda si falló una escritura")
	}
	// Al arreglar el disco, el siguiente Stop recupera las llamadas.
	os.Remove(r.bflowPath("metrics"))
	r.hook(stopJSON("s1", tr), "hook", "tokens")
	if calls := r.unassignedCalls(); len(calls) != 1 || calls[0]["output"].(float64) != 77 {
		t.Errorf("no se perdió nada: %v", calls)
	}
}

func unassignedLine(msg string, out int) string {
	b, _ := json.Marshal(map[string]any{"ts": time.Now().UTC().Format(time.RFC3339), "run": "claude:s1", "tool": "claude", "agent": "main",
		"phase": "", "msg": msg, "input": 1, "cache_write": 0, "cache_read": 2000, "output": out})
	return string(b) + "\n"
}

func (r *repo) writeUnassigned(lines ...string) {
	r.t.Helper()
	p := r.bflowPath("metrics", "calls.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "")), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func TestStatsUnassigned(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	r.startTask("Una")
	text, _, _ := r.hook("", "stats")
	if strings.Contains(text, metrics.UnassignedLabel) {
		t.Errorf("con 0 no muestra nada:\n%s", text)
	}
	if _, ok := r.ok("stats").Data["unassigned"]; ok {
		t.Error("con 0 no hay data.unassigned")
	}
	r.writeUnassigned(unassignedLine("m1", 4000), unassignedLine("m2", 1000))
	text, _, _ = r.hook("", "stats")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "sin tarea  ") || !strings.Contains(last, "5.0k") {
		t.Errorf("última línea:\n%s", text)
	}
	un, ok := r.ok("stats").Data["unassigned"].(map[string]any)
	if !ok || un["output"].(float64) != 5000 || un["cache_read"].(float64) != 4000 {
		t.Errorf("data.unassigned: %v", un)
	}
}

func TestWatchUnassigned(t *testing.T) {
	r := newRepo(t)
	r.writeConfig("")
	r.startTask("Panel")
	text, _, _ := r.hook("", "watch", "--once")
	if strings.Contains(text, metrics.UnassignedLabel) {
		t.Errorf("con 0 no muestra nada:\n%s", text)
	}
	r.writeUnassigned(unassignedLine("m1", 4000))
	text, _, _ = r.hook("", "watch", "--once")
	if !strings.Contains(text, "sin tarea  ") {
		t.Errorf("el panel con tarea activa muestra la línea:\n%s", text)
	}
	// También sin tarea activa.
	r2 := newRepo(t)
	r2.writeConfig("")
	r2.writeUnassigned(unassignedLine("m1", 4000))
	text, _, _ = r2.hook("", "watch", "--once")
	if !strings.Contains(text, "sin tarea  ") {
		t.Errorf("el panel sin tarea activa muestra la línea:\n%s", text)
	}
}
