package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/metrics"
)

// Pruebas de OpenCode 2.x (GH-98).

const (
	plugin2Rel = ".opencode/plugins/bflow2/index.js"
	package2   = ".opencode/plugins/bflow2/package.json"
)

// R16: el guard entiende los nombres y argumentos de 2.x.
func TestGuardOpenCodeV2Bloquea(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	g := func(tool string, args any) (int, string) {
		code, _, errOut := hookRun(t, env, guardIn(env, tool, args, false, ""), "guard", "--tool", "opencode")
		return code, errOut
	}
	if code, errOut := g("shell", map[string]any{"command": "git reset --hard HEAD~1"}); code != 2 || !strings.Contains(errOut, "reset") {
		t.Errorf("shell es bash: %d %q", code, errOut)
	}
	if code, errOut := g("shell", map[string]any{"command": "go test ./..."}); code != 0 || errOut != "" {
		t.Errorf("shell permitido es silencioso: %d %q", code, errOut)
	}
	for _, tool := range []string{"write", "edit"} {
		if code, errOut := g(tool, map[string]any{"path": ".env"}); code != 2 || !strings.Contains(errOut, ".env") {
			t.Errorf("%s con path fuera de alcance: %d %q", tool, code, errOut)
		}
	}
	if code, _ := g("write", map[string]any{"path": "src/a.go"}); code != 0 {
		t.Errorf("write con path permitido: %d", code)
	}
	if code, errOut := g("patch", map[string]any{"patchText": "*** Begin Patch\n*** Add File: .env\n+x\n*** End Patch"}); code != 2 || !strings.Contains(errOut, ".env") {
		t.Errorf("patch con patchText: %d %q", code, errOut)
	}
}

// R2: un archivo de bflow2/ sin la marca es conflicto y no se toca.
func TestRenderPlugin2Conflicto(t *testing.T) {
	setHome(t)
	for _, rel := range []string{plugin2Rel, package2} {
		env := toolsEnv(t, "agent: [claude, opencode]\n")
		mine := filepath.Join(env.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mine, []byte("// mio\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, raw := runJSON(t, env, "render")
		if code != 1 || out["code"] != "render_conflict" || !strings.Contains(raw, rel) {
			t.Errorf("%s sin la marca es conflicto: %d %s", rel, code, raw)
		}
		if b, _ := os.ReadFile(mine); string(b) != "// mio\n" {
			t.Errorf("no pisa %s", rel)
		}
	}
}

// R1, R4: render escribe bflow2/, --check lo vigila y, sin opencode, se borra con su carpeta.
func TestRenderPlugin2Stale(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render: %s", raw)
	}
	for _, rel := range []string{plugin2Rel, package2} {
		if !strings.Contains(rendered(t, env, rel), "generado por bflow render") {
			t.Fatalf("%s se escribe con la marca", rel)
		}
	}
	if !strings.Contains(rendered(t, env, plugin2Rel), "export default") || !strings.Contains(rendered(t, env, pluginRel), "export const BflowPlugin") {
		t.Fatal("bflow2/index.js es el de 2.x y bflow.js sigue igual")
	}
	if code, out, raw := runJSON(t, env, "render", "--check"); code != 0 || out["code"] != "render_ok" {
		t.Fatalf("al día: %d %s", code, raw)
	}
	p := filepath.Join(env.Dir, filepath.FromSlash(package2))
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, raw := runJSON(t, env, "render", "--check"); code == 0 || out["code"] != "render_outdated" || !strings.Contains(raw, package2) {
		t.Errorf("--check detecta package.json distinto: %d %s", code, raw)
	}
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render restaura: %s", raw)
	}

	if err := os.WriteFile(filepath.Join(env.Dir, "bflow.yaml"), []byte("agent: claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, raw := runJSON(t, env, "render", "--check")
	stale := strings.Join(stringsOf(dataOf(out)["stale"]), ",")
	if code == 0 || out["code"] != "render_outdated" || !strings.Contains(stale, plugin2Rel) || !strings.Contains(stale, package2) {
		t.Fatalf("sin opencode, los dos archivos de bflow2/ salen stale: %d %s", code, raw)
	}
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render: %s", raw)
	}
	if exists(env, plugin2Rel) || exists(env, package2) {
		t.Error("los archivos generados se borran")
	}
	if exists(env, ".opencode/plugins/bflow2") {
		t.Error("la carpeta vacía bflow2/ también se borra")
	}
}

// R3: doctor da un solo check plugin sobre los tres archivos; gana el peor estado.
func TestDoctorOpenCodePlugin2(t *testing.T) {
	setHome(t)
	setXDG(t)
	env := toolsEnv(t, "agent: [claude, opencode]\n")
	detail := func() (status, d string) {
		t.Helper()
		c := doctorChecks(t, env)["plugin"]
		if len(c) != 1 {
			t.Fatalf("doctor da un ítem plugin: %v", c)
		}
		return c[0]["status"].(string), fmt.Sprint(c[0]["detail"])
	}
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render: %s", raw)
	}
	if s, _ := detail(); s != "ok" {
		t.Fatalf("los tres al día: ok, es %s", s)
	}
	idx := filepath.Join(env.Dir, filepath.FromSlash(plugin2Rel))
	pkg := filepath.Join(env.Dir, filepath.FromSlash(package2))
	good, _ := os.ReadFile(idx)

	if err := os.Remove(idx); err != nil {
		t.Fatal(err)
	}
	if s, d := detail(); s != "warn" || !strings.Contains(d, "bflow2/index.js") || !strings.Contains(d, "bflow render") {
		t.Errorf("falta index.js de 2.x: warn que lo nombra: %s %s", s, d)
	}
	if err := os.WriteFile(idx, append(good, []byte("// cambio\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, d := detail(); s != "warn" || !strings.Contains(d, "bflow2/index.js") {
		t.Errorf("index.js difiere: warn: %s %s", s, d)
	}
	if err := os.WriteFile(idx, good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(pkg); err != nil {
		t.Fatal(err)
	}
	if s, d := detail(); s != "warn" || !strings.Contains(d, "bflow2/package.json") {
		t.Errorf("falta package.json: warn que lo nombra: %s %s", s, d)
	}
	if err := os.WriteFile(pkg, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := detail(); s != "fail" {
		t.Errorf("package.json sin la marca: fail: %s", s)
	}
	if err := os.WriteFile(idx, []byte("// mio\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, _ := detail(); s != "fail" {
		t.Errorf("fail gana sobre warn: %s", s)
	}
}

// R12: una caché escrita por el plugin 2.x (id evt_..., agente build) se cuenta igual que la de 1.x.
func TestHookTokensOpenCodeV2(t *testing.T) {
	notified := countNotify(t)
	env, e, id, cache := tokensEnv(t, true)
	raw, err := os.ReadFile(filepath.Join("..", "adapters", "agent", "opencode", "testdata", "usage_v2.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var first struct {
		SessionID string `json:"sessionID"`
	}
	if err := json.Unmarshal(raw[:bytes.IndexByte(raw, '\n')], &first); err != nil {
		t.Fatal(err)
	}
	// Los tiempos de la captura se corren a ahora: la ventana de marcas es relativa al reloj.
	var body strings.Builder
	for i, l := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(l, &m); err != nil {
			t.Fatal(err)
		}
		m["completed"] = time.Now().Add(-time.Duration(10-i) * time.Second).UnixMilli()
		b, _ := json.Marshal(m)
		body.Write(append(b, '\n'))
	}
	putFile(t, cache, first.SessionID+".jsonl", body.String(), 0)
	mark := metrics.Mark{TS: time.Now().Add(-6 * 24 * time.Hour), Session: first.SessionID, ID: id}
	if err := metrics.AppendMark(metrics.MarksPath(e.Store.Dir()), mark); err != nil {
		t.Fatal(err)
	}
	idle := `{"sessionID":"` + first.SessionID + `","parentID":"","cwd":"` + strings.ReplaceAll(env.Dir, `\`, `\\`) + `"}`
	if code, out, errOut := hookRun(t, env, idle, "hook", "tokens", "--tool", "opencode"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("el hook es silencioso: %d %q %q", code, out, errOut)
	}
	by, entries := tokensByAgent(t, e, id)
	if by["main"] != 100+101+102+103+104+105 {
		t.Errorf("seis pasos de la raíz a main: %v", by)
	}
	for _, en := range entries {
		if en.Data["tool"] != "opencode" || en.Data["model"] != "mock/mock" {
			t.Errorf("tool y modelo providerID/modelID: %v", en.Data)
		}
	}
	if *notified != 1 {
		t.Errorf("el idle de la raíz avisa una vez: %d", *notified)
	}
}
