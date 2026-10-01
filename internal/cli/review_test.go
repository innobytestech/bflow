package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/review"
	"innobytes.tech/bflow/internal/store"
)

// qualityTask es una tarea activa que ya está en quality, con un archivo en el repo.
func qualityTask(t *testing.T) (*Env, *engine.Engine, string) {
	t.Helper()
	env, e, id, _ := tokensEnv(t, true)
	if _, err := e.Store.Update(id, func(r *store.Record, _ bool) error { r.Flow.Phase = flow.Quality; return nil }); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(env.Dir, "internal", "a.go")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return env, e, id
}

// claudeTool arma la entrada de un hook PreToolUse de Claude Code.
func claudeTool(env *Env, tool, agent string, input map[string]any) string {
	m := map[string]any{"hook_event_name": "PreToolUse", "cwd": env.Dir, "tool_name": tool, "tool_input": input}
	if agent != "" {
		m["agent_type"], m["agent_id"] = agent, "ag1"
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func readsOf(t *testing.T, e *engine.Engine, id string) []review.Read {
	t.Helper()
	b, err := e.Store.ReadFile(id, review.ReadsFile)
	if err != nil {
		return nil
	}
	return review.ParseReads(strings.NewReader(string(b)))
}

// R1: el reviewer en quality deja una línea por acción, aunque no lea nada.
func TestGuardRecordsReviewerReads(t *testing.T) {
	env, e, id := qualityTask(t)
	a := filepath.Join(env.Dir, "internal", "a.go")

	code, out, errOut := hookRun(t, env, claudeTool(env, "Read", "bflow-reviewer", map[string]any{"file_path": a}), "guard", "--reads")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("Read del reviewer se permite en silencio: %d %q %q", code, out, errOut)
	}
	rs := readsOf(t, e, id)
	if len(rs) != 1 || rs[0].Tool != guard.Read || len(rs[0].Paths) != 1 || rs[0].Paths[0] != "internal/a.go" || !rs[0].ReadHook || rs[0].TS.IsZero() {
		t.Fatalf("una línea con la ruta relativa: %+v", rs)
	}

	hookRun(t, env, claudeTool(env, "Bash", "bflow-reviewer", map[string]any{"command": "git diff main...HEAD"}), "guard", "--reads")
	hookRun(t, env, claudeTool(env, "Bash", "bflow-reviewer", map[string]any{"command": "go vet ./..."}), "guard", "--reads")
	rs = readsOf(t, e, id)
	if len(rs) != 3 || !rs[1].Whole || rs[1].Tool != guard.Bash || len(rs[2].Paths) != 0 || rs[2].Whole {
		t.Fatalf("un git diff entero y una acción que no lee nada también quedan: %+v", rs)
	}

	// Sin --reads el hook no ve los Read: lo que llega se marca como no medido.
	hookRun(t, env, claudeTool(env, "Bash", "bflow-reviewer", map[string]any{"command": "cat internal/a.go"}), "guard")
	rs = readsOf(t, e, id)
	if len(rs) != 4 || rs[3].ReadHook {
		t.Errorf("sin --reads, ReadHook es false: %+v", rs)
	}
}

// R5: solo el reviewer, solo en quality.
func TestGuardIgnoresOtherReads(t *testing.T) {
	env, e, id := qualityTask(t)
	a := filepath.Join(env.Dir, "internal", "a.go")
	read := func(agent string) {
		t.Helper()
		if code, out, errOut := hookRun(t, env, claudeTool(env, "Read", agent, map[string]any{"file_path": a}), "guard", "--reads"); code != 0 || out != "" || errOut != "" {
			t.Fatalf("%q: %d %q %q", agent, code, out, errOut)
		}
	}
	read("")                     // sesión principal
	read("bflow-implementer")    // otro agente
	read("bflow-security-audit") // otro agente de bflow
	read("reviewer")             // sin el prefijo bflow-: no es de bflow
	if rs := readsOf(t, e, id); len(rs) != 0 {
		t.Fatalf("no se registra nada: %+v", rs)
	}

	// El reviewer fuera de quality tampoco deja registro.
	if _, err := e.Store.Update(id, func(r *store.Record, _ bool) error { r.Flow.Phase = flow.Implementing; return nil }); err != nil {
		t.Fatal(err)
	}
	read("bflow-reviewer")
	if rs := readsOf(t, e, id); len(rs) != 0 {
		t.Fatalf("fuera de quality no se registra: %+v", rs)
	}

	// Control: en quality sí.
	if _, err := e.Store.Update(id, func(r *store.Record, _ bool) error { r.Flow.Phase = flow.Quality; return nil }); err != nil {
		t.Fatal(err)
	}
	read("bflow-reviewer")
	if rs := readsOf(t, e, id); len(rs) != 1 {
		t.Fatalf("en quality el reviewer sí queda: %+v", rs)
	}
}

// R5: registrar nunca bloquea; un rechazo del guard sigue siendo un rechazo y no se registra.
func TestGuardRecordNeverBlocks(t *testing.T) {
	env, e, id := qualityTask(t)
	a := filepath.Join(env.Dir, "internal", "a.go")
	in := claudeTool(env, "Read", "bflow-reviewer", map[string]any{"file_path": a})
	if code, _, _ := hookRun(t, env, in, "guard", "--reads"); code != 0 || len(readsOf(t, e, id)) != 1 {
		t.Fatalf("control: el Read se registra (%d)", code)
	}

	// reads.jsonl pasa a ser una carpeta: no se puede escribir.
	p, err := e.Store.Path(id, review.ReadsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := hookRun(t, env, in, "guard", "--reads")
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("si no se puede registrar, el guard permite en silencio: %d %q %q", code, out, errOut)
	}

	// Un rechazo sigue siendo un rechazo.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	bad := claudeTool(env, "Bash", "bflow-reviewer", map[string]any{"command": "git reset --hard"})
	if code, _, errOut := hookRun(t, env, bad, "guard", "--reads"); code != 2 || !strings.Contains(errOut, "bflow guard:") {
		t.Errorf("git reset --hard sigue bloqueado: %d %q", code, errOut)
	}
	if rs := readsOf(t, e, id); len(rs) != 0 {
		t.Errorf("lo bloqueado no se registra: %+v", rs)
	}
}

func coverageEntry(t *testing.T, e *engine.Engine, id string, ts time.Time, data map[string]any) {
	t.Helper()
	if err := e.Store.Append(store.Entry{TS: ts, ID: id, Event: "review_coverage", By: "dev", Agent: "reviewer", Data: data}); err != nil {
		t.Fatal(err)
	}
}

// R14: stats muestra la última cobertura; sin el evento, la salida no cambia.
func TestStatsReviewCoverage(t *testing.T) {
	env, e, id, _ := tokensEnv(t, true)
	_, plain := textOf(t, env, "stats", id)
	_, outPlain, rawPlain := runJSON(t, env, "stats", id)
	if strings.Contains(plain, "revisión:") || strings.Contains(rawPlain, `"review"`) {
		t.Fatalf("sin review_coverage no hay línea ni data.stats.review:\n%s\n%s", plain, rawPlain)
	}

	now := time.Now()
	coverageEntry(t, e, id, now, map[string]any{"measured": true, "total": 10, "read": 4, "round": 0, "unread": []string{"x/a.go"}})
	coverageEntry(t, e, id, now.Add(time.Minute), map[string]any{"measured": true, "total": 10, "read": 7, "round": 1, "unread": []string{"x/b.go"}})

	_, text := textOf(t, env, "stats", id)
	if !strings.Contains(text, "\n  revisión: el reviewer leyó 7 de 10 archivos del diff") || strings.Contains(text, "leyó 4 de 10") {
		t.Errorf("línea revisión con la última cobertura:\n%s", text)
	}
	if !strings.HasPrefix(text, strings.Split(plain, "\n")[0]) {
		t.Errorf("el resto de la salida sigue igual:\n%s", text)
	}

	_, out, raw := runJSON(t, env, "stats", id)
	st := dataOf(out)["stats"].(map[string]any)
	rv, ok := st["review"].(map[string]any)
	if !ok || rv["read"] != float64(7) || rv["total"] != float64(10) || rv["measured"] != true {
		t.Errorf("data.stats.review: %s", raw)
	}
	if _, has := dataOf(outPlain)["stats"].(map[string]any)["review"]; has {
		t.Error("sin el evento no hay review")
	}

	// Sin medir, la línea lo dice.
	coverageEntry(t, e, id, now.Add(2*time.Minute), map[string]any{"measured": false, "total": 10, "read": 0})
	if _, text := textOf(t, env, "stats", id); !strings.Contains(text, "  revisión: cobertura del reviewer: no medida") {
		t.Errorf("sin medir:\n%s", text)
	}
}

// R18: doctor avisa si el guard de Claude no ve Read.
func TestDoctorWarnsGuardWithoutReads(t *testing.T) {
	setHome(t)
	const warn = "claude: el guard no ve Read; la cobertura del reviewer no se mide (corre bflow install claude)"
	hooks := func(guardCmd string) string {
		return `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"bflow hook session-start"}]}],` +
			`"PreToolUse":[{"matcher":"Bash|Edit|Write|MultiEdit|NotebookEdit","hooks":[{"type":"command","command":"` + guardCmd + `"}]}],` +
			`"SubagentStop":[{"hooks":[{"type":"command","command":"bflow hook subagent-stop"}]}]}}`
	}
	has := func(env *Env) bool {
		for _, it := range doctorChecks(t, env)["agente"] {
			if strings.Contains(it["detail"].(string), warn) {
				return true
			}
		}
		return false
	}
	env := installEnv(t, "agent: claude\n")
	if err := os.MkdirAll(filepath.Join(env.Dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(env.Dir, ".claude", "settings.json")
	if err := os.WriteFile(settings, []byte(hooks("bflow guard")), 0o644); err != nil {
		t.Fatal(err)
	}
	if !has(env) {
		t.Errorf("guard sin --reads: falta el aviso %q", warn)
	}
	if err := os.WriteFile(settings, []byte(hooks("bflow guard --reads")), 0o644); err != nil {
		t.Fatal(err)
	}
	if has(env) {
		t.Error("guard con --reads: sin aviso")
	}
}
