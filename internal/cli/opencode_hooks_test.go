package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker/trackertest"
)

// hookRun corre bflow con stdin como lo hace el plugin de OpenCode.
func hookRun(t *testing.T, env *Env, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	env.Stdin = strings.NewReader(stdin)
	env.Stdout, env.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
	code = Run(args, env)
	return code, env.Stdout.(*bytes.Buffer).String(), env.Stderr.(*bytes.Buffer).String()
}

// guardIn es la entrada de `bflow guard --tool opencode`.
func guardIn(env *Env, tool string, args any, subagent bool, agent string) string {
	b, _ := json.Marshal(map[string]any{"tool": tool, "args": args, "sessionID": "ses_1", "agent": agent, "subagent": subagent, "cwd": env.Dir})
	return string(b)
}

func TestGuardOpenCodeBloquea(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	code, out, errOut := hookRun(t, env, guardIn(env, "bash", map[string]any{"command": "git reset --hard HEAD~1"}, false, ""), "guard", "--tool", "opencode")
	if code != 2 || !strings.Contains(errOut, "bflow guard:") || !strings.Contains(errOut, "reset") {
		t.Fatalf("git reset --hard se bloquea con el motivo en stderr: %d %q %q", code, out, errOut)
	}
	code, out, errOut = hookRun(t, env, guardIn(env, "bash", map[string]any{"command": "go test ./..."}, false, ""), "guard", "--tool", "opencode")
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("permitido es silencioso: %d %q %q", code, out, errOut)
	}
	code, _, errOut = hookRun(t, env, guardIn(env, "write", map[string]any{"filePath": ".env"}, false, ""), "guard", "--tool", "opencode")
	if code != 2 || !strings.Contains(errOut, ".env") {
		t.Errorf("escribir .env (ruta relativa a cwd): %d %q", code, errOut)
	}
	code, _, _ = hookRun(t, env, guardIn(env, "read", map[string]any{"filePath": ".env"}, false, ""), "guard", "--tool", "opencode")
	if code != 0 {
		t.Errorf("una herramienta que no es de escritura se permite: %d", code)
	}
}

func TestGuardOpenCodePatchUnaBloqueada(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	patch := func(files ...string) string {
		s := "*** Begin Patch\n"
		for _, f := range files {
			s += "*** " + f + "\n+x\n"
		}
		return s + "*** End Patch"
	}
	ok := patch("Add File: src/a.go", "Update File: src/b.go")
	if code, _, errOut := hookRun(t, env, guardIn(env, "apply_patch", map[string]any{"patchText": ok}, false, ""), "guard", "--tool", "opencode"); code != 0 {
		t.Fatalf("un patch con rutas permitidas pasa: %d %q", code, errOut)
	}
	// La primera rechazada (en el orden escrituras y luego ediciones) da el motivo.
	bad := patch("Update File: src/b.go", "Add File: .env", "Add File: .bflow/log.jsonl")
	code, _, errOut := hookRun(t, env, guardIn(env, "apply_patch", map[string]any{"patchText": bad}, false, ""), "guard", "--tool", "opencode")
	if code != 2 || !strings.Contains(errOut, ".env") || strings.Contains(errOut, "log.jsonl") {
		t.Errorf("basta una ruta bloqueada y el motivo es el de la primera: %d %q", code, errOut)
	}
	if code, _, _ := hookRun(t, env, guardIn(env, "patch", map[string]any{"patchText": patch("Delete File: .env.production")}, false, ""), "guard", "--tool", "opencode"); code != 2 {
		t.Errorf("también la herramienta patch, y Delete cuenta como edición: %d", code)
	}
}

func TestGuardOpenCodeSubagente(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\nguard: { strict_leader: true, protected_paths: [internal] }\n")
	w := map[string]any{"filePath": "internal/x.go"}
	code, _, errOut := hookRun(t, env, guardIn(env, "write", w, false, ""), "guard", "--tool", "opencode")
	if code != 2 || !strings.Contains(errOut, "implementer") {
		t.Errorf("la sesión principal no edita el código protegido: %d %q", code, errOut)
	}
	code, _, errOut = hookRun(t, env, guardIn(env, "write", w, true, "bflow-implementer"), "guard", "--tool", "opencode")
	if code != 0 {
		t.Errorf("un subagente sí: %d %q", code, errOut)
	}
}

func TestGuardOpenCodeConfigRota(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	if err := os.WriteFile(filepath.Join(env.Dir, "bflow.yaml"), []byte("agent: [\n  : roto"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := hookRun(t, env, guardIn(env, "bash", map[string]any{"command": "git reset --hard"}, false, ""), "guard", "--tool", "opencode")
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("con la config rota se permite en silencio: %d %q %q", code, out, errOut)
	}
	// Con la config sana la misma entrada sí se bloquea: la prueba mide la config, no el parser.
	if err := os.WriteFile(filepath.Join(env.Dir, "bflow.yaml"), []byte("agent: [claude, opencode]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := hookRun(t, env, guardIn(env, "bash", map[string]any{"command": "git reset --hard"}, false, ""), "guard", "--tool", "opencode"); code != 2 {
		t.Errorf("control: con la config sana se bloquea: %d", code)
	}
}

func TestGuardToolDesconocida(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	in := guardIn(env, "bash", map[string]any{"command": "git reset --hard"}, false, "")
	for _, tool := range []string{"nada", "claude"} { // no registrada; registrada pero sin HookAdapter
		code, out, errOut := hookRun(t, env, in, "guard", "--tool", tool)
		if code != 0 || out != "" || errOut != "" {
			t.Errorf("--tool %s: se permite en silencio: %d %q %q", tool, code, out, errOut)
		}
	}
	code, out, _ := hookRun(t, env, in, "hook", "tokens", "--tool", "nada")
	if code != 0 || out != "" {
		t.Errorf("hook tokens con una herramienta desconocida sale callado: %d %q", code, out)
	}
}

func TestGuardSinToolIgualQueAntes(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	claude := func(cmd string) string {
		b, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": env.Dir, "tool_name": "Bash", "tool_input": map[string]any{"command": cmd}})
		return string(b)
	}
	if code, _, errOut := hookRun(t, env, claude("git reset --hard"), "guard"); code != 2 || !strings.Contains(errOut, "bflow guard:") {
		t.Errorf("sin --tool sigue el camino de Claude: %d %q", code, errOut)
	}
	if code, out, _ := hookRun(t, env, claude("git status"), "guard"); code != 0 || out != "" {
		t.Errorf("permitido: %d %q", code, out)
	}
	if code, _, _ := hookRun(t, env, "basura", "guard"); code != 1 {
		t.Errorf("entrada no reconocida sigue siendo bad_input: %d", code)
	}
	// Con --tool opencode, una entrada de Claude no se adivina: no tiene la forma de OpenCode.
	if code, _, _ := hookRun(t, env, claude("git reset --hard"), "guard", "--tool", "opencode"); code != 0 {
		t.Errorf("--tool opencode solo entiende la entrada del plugin (fail-open): %d", code)
	}
}

// ---- tokens ----

// tokensEnv es un repo con una tarea en curso y la caché de OpenCode vacía.
func tokensEnv(t *testing.T, startTask bool) (env *Env, e *engine.Engine, id, cacheDir string) {
	t.Helper()
	setHome(t)
	tr := trackertest.NewMemory()
	env = ghEnv(t, "agent: [claude, opencode]\ntracker: { adapter: local }\nui: { notify: false }\n", tr)
	env.Tools = bothTools()
	env.Agent = bothTools()[0].(AgentAdapter)
	if startTask {
		task, err := tr.Create(context.Background(), "Algo", "")
		if err != nil {
			t.Fatal(err)
		}
		if c, _, raw := runJSON(t, env, "start", task.ID, "--lane", "light"); c != 0 {
			t.Fatalf("start: %s", raw)
		}
		id = task.ID
	}
	var err error
	if e, err = env.Build(env.Dir); err != nil {
		t.Fatal(err)
	}
	cacheDir = filepath.Join(e.Store.Dir(), "cache", "opencode")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

// usageEntry es una línea de la caché que escribe el plugin.
func usageEntry(id, session, parent, agent string, in, out int64) string {
	b, _ := json.Marshal(map[string]any{"id": id, "sessionID": session, "parentID": parent, "agent": agent,
		"providerID": "anthropic", "modelID": "claude-sonnet-4", "created": 1790877056000, "completed": 1790877056500,
		"tokens": map[string]any{"input": in, "output": out, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}})
	return string(b) + "\n"
}

func putFile(t *testing.T, dir, name, body string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
	return p
}

func num(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

// tokensByAgent suma el log de tokens de la tarea por agente.
func tokensByAgent(t *testing.T, e *engine.Engine, id string) (in map[string]int64, entries []store.Entry) {
	t.Helper()
	log, err := e.Store.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	in = map[string]int64{}
	for _, en := range log {
		if en.Event == "tokens" {
			in[en.Agent] += num(en.Data["input"])
			entries = append(entries, en)
		}
	}
	return
}

// countNotify cambia notifyActive por un contador mientras dura la prueba.
func countNotify(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := notifyActive
	notifyActive = func(*engine.Engine) { n++ }
	t.Cleanup(func() { notifyActive = orig })
	return &n
}

func TestHookTokensOpenCodePrincipal(t *testing.T) {
	notified := countNotify(t)
	env, e, id, cache := tokensEnv(t, true)
	putFile(t, cache, "ses_main.jsonl", usageEntry("m1", "ses_main", "", "build", 100, 10)+usageEntry("m2", "ses_main", "", "build", 1, 1), 0)
	putFile(t, cache, "ses_kid.jsonl", usageEntry("k1", "ses_kid", "ses_main", "bflow-implementer", 200, 20), 0) // hijo sin idle propio
	old := putFile(t, cache, "ses_old.jsonl", usageEntry("o1", "ses_old", "", "build", 5, 5), 8*24*time.Hour)
	idle := `{"sessionID":"ses_main","parentID":"","cwd":"` + strings.ReplaceAll(env.Dir, `\`, `\\`) + `"}`

	code, out, errOut := hookRun(t, env, idle, "hook", "tokens", "--tool", "opencode")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("el hook es silencioso: %d %q %q", code, out, errOut)
	}
	by, entries := tokensByAgent(t, e, id)
	if by["main"] != 106 || by["implementer"] != 200 {
		t.Errorf("la principal a main (incluida la vieja sin leer antes) y el hijo a su agente sin el prefijo bflow-: %v", by)
	}
	for _, en := range entries {
		if en.Data["tool"] != "opencode" || en.Data["model"] != "anthropic/claude-sonnet-4" || en.Data["phase"] == nil {
			t.Errorf("tool, modelo y fase en cada entrada: %v", en.Data)
		}
	}
	if *notified != 1 {
		t.Errorf("el idle de la principal avisa una vez como el Stop de Claude: %d", *notified)
	}
	if _, err := os.Stat(old); err == nil {
		t.Errorf("leído por completo y de hace 8 días: se borra")
	}
	if _, err := os.Stat(filepath.Join(cache, "ses_main.jsonl")); err != nil {
		t.Errorf("lo reciente se queda: %v", err)
	}
	cur, _ := os.ReadFile(filepath.Join(e.Store.Dir(), "cache", "tokens-cursor.json"))
	if !strings.Contains(string(cur), `"opencode:m1"`) || strings.Contains(string(cur), "ses_old") {
		t.Errorf("el cursor guarda los ids con prefijo opencode: y no el offset del archivo borrado: %s", cur)
	}

	hookRun(t, env, idle, "hook", "tokens", "--tool", "opencode")
	if by2, _ := tokensByAgent(t, e, id); by2["main"] != 106 || by2["implementer"] != 200 {
		t.Errorf("un segundo idle sin nada nuevo no suma otra vez: %v", by2)
	}
}

func TestHookTokensOpenCodeHijo(t *testing.T) {
	notified := countNotify(t)
	env, e, id, cache := tokensEnv(t, true)
	putFile(t, cache, "ses_main.jsonl", usageEntry("m1", "ses_main", "", "build", 100, 10), 0)
	putFile(t, cache, "ses_kid.jsonl", usageEntry("k1", "ses_kid", "ses_main", "bflow-implementer", 200, 20), 0)

	kid := `{"sessionID":"ses_kid","parentID":"ses_main","cwd":"x"}`
	if code, out, _ := hookRun(t, env, kid, "hook", "tokens", "--tool", "opencode"); code != 0 || out != "" {
		t.Fatalf("silencioso: %d %q", code, out)
	}
	by, _ := tokensByAgent(t, e, id)
	if len(by) != 1 || by["implementer"] != 200 {
		t.Errorf("el idle de un hijo procesa solo su archivo: %v", by)
	}
	if *notified != 0 {
		t.Errorf("el idle de un hijo no avisa: %d", *notified)
	}

	hookRun(t, env, `{"sessionID":"ses_main","parentID":"","cwd":"x"}`, "hook", "tokens", "--tool", "opencode")
	by, _ = tokensByAgent(t, e, id)
	if by["main"] != 100 || by["implementer"] != 200 {
		t.Errorf("después el idle de la principal suma lo suyo y no repite al hijo: %v", by)
	}
	if *notified != 1 {
		t.Errorf("y esa sí avisa: %d", *notified)
	}
}

func TestHookTokensOpenCodeSinTarea(t *testing.T) {
	env, e, _, cache := tokensEnv(t, false)
	putFile(t, cache, "ses_main.jsonl", usageEntry("m1", "ses_main", "", "build", 100, 10), 0)
	idle := `{"sessionID":"ses_main","parentID":"","cwd":"x"}`

	code, out, errOut := hookRun(t, env, idle, "hook", "tokens", "--tool", "opencode")
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("sin tarea activa sale callado con exit 0: %d %q %q", code, out, errOut)
	}
	if log, _ := e.Store.Log(""); len(log) != 0 {
		t.Errorf("no registra nada: %v", log)
	}

	// Sin repo de bflow tampoco falla, ni con entrada que no es JSON.
	bare := &Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Dir: t.TempDir(), Tools: bothTools(), Agent: bothTools()[0].(AgentAdapter)}
	for _, in := range []string{idle, "no es json", ""} {
		if code, out, errOut := hookRun(t, bare, in, "hook", "tokens", "--tool", "opencode"); code != 0 || out != "" || errOut != "" {
			t.Errorf("sin repo, entrada %q: %d %q %q", in, code, out, errOut)
		}
	}
	// Control: con tarea activa la misma caché sí se registra.
	env2, e2, id2, cache2 := tokensEnv(t, true)
	putFile(t, cache2, "ses_main.jsonl", usageEntry("m1", "ses_main", "", "build", 100, 10), 0)
	hookRun(t, env2, idle, "hook", "tokens", "--tool", "opencode")
	if by, _ := tokensByAgent(t, e2, id2); by["main"] != 100 {
		t.Errorf("control con tarea: %v", by)
	}
}

// ---- render y doctor ----

const pluginRel = ".opencode/plugins/bflow.js"

func TestRenderPluginConflicto(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	mine := filepath.Join(env.Dir, ".opencode", "plugins", "bflow.js")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("// mi plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, raw := runJSON(t, env, "render")
	if code != 1 || out["code"] != "render_conflict" || !strings.Contains(raw, pluginRel) {
		t.Errorf("un plugin sin la marca en esa ruta es conflicto: %d %s", code, raw)
	}
	if b, _ := os.ReadFile(mine); string(b) != "// mi plugin\n" {
		t.Error("no pisa el archivo de la persona")
	}
}

func TestRenderPluginStale(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render: %s", raw)
	}
	first, _, _ := strings.Cut(rendered(t, env, pluginRel), "\n")
	if !strings.HasPrefix(first, "// <!-- generado por bflow render") {
		t.Fatalf("render escribe el plugin con la marca en la primera línea: %q", first)
	}
	if code, out, raw := runJSON(t, env, "render", "--check"); code != 0 || out["code"] != "render_ok" {
		t.Fatalf("al día, --check cubre el plugin y pasa: %d %s", code, raw)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, ".opencode", "plugins", "bflow.js"), []byte(first+"\n// editado a mano\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, raw := runJSON(t, env, "render", "--check"); code == 0 || out["code"] != "render_outdated" || !strings.Contains(raw, pluginRel) {
		t.Errorf("--check detecta el plugin distinto: %d %s", code, raw)
	}
	if code, _, raw := runJSON(t, env, "render"); code != 0 || !strings.Contains(rendered(t, env, pluginRel), "export const BflowPlugin") {
		t.Fatalf("render lo restaura: %d %s", code, raw)
	}

	if err := os.WriteFile(filepath.Join(env.Dir, "bflow.yaml"), []byte("agent: claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, raw := runJSON(t, env, "render", "--check")
	if code == 0 || out["code"] != "render_outdated" || !strings.Contains(strings.Join(stringsOf(dataOf(out)["stale"]), ","), pluginRel) {
		t.Fatalf("sin opencode en agent:, el plugin generado sale stale: %d %s", code, raw)
	}
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render: %s", raw)
	}
	if exists(env, pluginRel) {
		t.Error("el plugin generado se borra")
	}
}

func TestDoctorOpenCodePlugin(t *testing.T) {
	setHome(t)
	setXDG(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	detail := func(area string) (status, d string) {
		t.Helper()
		c := doctorChecks(t, env)[area]
		if len(c) != 1 {
			t.Fatalf("doctor da un ítem %q: %v", area, c)
		}
		return c[0]["status"].(string), fmt.Sprint(c[0]["detail"])
	}

	if s, d := detail("plugin"); s != "warn" || !strings.Contains(d, "bflow render") {
		t.Errorf("sin plugin: warn que manda a bflow render: %s %s", s, d)
	}
	if g := doctorChecks(t, env)["guard"]; len(g) != 0 {
		t.Errorf("desaparece el warn de que el guard llega con GH-14: %v", g)
	}
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render: %s", raw)
	}
	if s, d := detail("plugin"); s != "ok" || !strings.Contains(d, "opencode") {
		t.Errorf("al día: ok: %s %s", s, d)
	}
	path := filepath.Join(env.Dir, ".opencode", "plugins", "bflow.js")
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(b, []byte("// cambio\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, d := detail("plugin"); s != "warn" || !strings.Contains(d, "bflow render") {
		t.Errorf("distinto: warn: %s %s", s, d)
	}
	if err := os.WriteFile(path, []byte("// mi plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := detail("plugin"); s != "fail" {
		t.Errorf("sin la marca: fail: %s", s)
	}

	s, d := detail("cobertura")
	if s != "ok" {
		t.Errorf("la nota de lo que OpenCode no cubre es informativa (ok), no warn: %s", s)
	}
	for _, w := range []string{"nudge", "bflow report", "bflow check --verify", "AGENTS.md"} {
		if !strings.Contains(d, w) {
			t.Errorf("la nota debe mencionar %q: %s", w, d)
		}
	}
}

// R1, R7, R8: los Read de OpenCode (sesión raíz y subagente) se registran con tool opencode.
func TestOpenCodeGuardRecordsReads(t *testing.T) {
	env, e, id, _ := tokensEnv(t, true)
	if err := os.WriteFile(filepath.Join(env.Dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := func(args any, sub bool, agent string) {
		t.Helper()
		if code, out, errOut := hookRun(t, env, guardIn(env, "read", args, sub, agent), "guard", "--tool", "opencode", "--reads"); code != 0 || out != "" || errOut != "" {
			t.Fatalf("%d %q %q", code, out, errOut)
		}
	}
	g(map[string]any{"filePath": "a.go"}, false, "")
	g(map[string]any{"filePath": filepath.Join(env.Dir, "a.go"), "offset": 3, "limit": 10}, true, "bflow-implementer")
	g(map[string]any{"filePath": "a.go"}, true, "")
	got := allReadsOf(t, e, id)
	if len(got) != 3 {
		t.Fatalf("tres lecturas: %+v", got)
	}
	wantAgent := []string{metrics.MainSession, "implementer", metrics.UnknownAgent}
	for i, g := range got {
		if g.Tool != "opencode" || g.Path != "a.go" || g.Agent != wantAgent[i] || g.Partial != (i == 1) || g.Phase == "" {
			t.Errorf("línea %d: %+v", i, g)
		}
	}
}
