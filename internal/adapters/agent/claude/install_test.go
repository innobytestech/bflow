package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	files "innobytes.tech/bflow/adapters/claude"
	"innobytes.tech/bflow/internal/testutil"
)

// merge corre mergeSettings con los ajustes embebidos y decodifica el resultado.
func merge(t *testing.T, cur string) (map[string]any, string, bool, []string) {
	t.Helper()
	out, changed, warns, err := mergeSettings([]byte(cur), files.Settings)
	if err != nil {
		t.Fatalf("mergeSettings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("la salida no es JSON: %v\n%s", err, out)
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		t.Errorf("la salida debe terminar en salto de línea")
	}
	return m, string(out), changed, warns
}

// commands lista los comandos de las entradas de un evento, en orden.
func commands(t *testing.T, m map[string]any, event string) [][]string {
	t.Helper()
	var out [][]string
	hooks, _ := m["hooks"].(map[string]any)
	entries, _ := hooks[event].([]any)
	for _, e := range entries {
		var cmds []string
		for _, h := range e.(map[string]any)["hooks"].([]any) {
			cmds = append(cmds, h.(map[string]any)["command"].(string))
		}
		out = append(out, cmds)
	}
	return out
}

func TestMergeSettingsEmpty(t *testing.T) {
	for _, cur := range []string{"", "{}"} {
		m, _, changed, _ := merge(t, cur)
		if !changed {
			t.Errorf("%q: un archivo vacío cambia", cur)
		}
		var want map[string]any
		if err := json.Unmarshal(files.Settings, &want); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"hooks", "statusLine"} {
			if !reflect.DeepEqual(m[k], want[k]) {
				t.Errorf("%q: %s = %v, quiero %v", cur, k, m[k], want[k])
			}
		}
		perm, _ := m["permissions"].(map[string]any)
		if !reflect.DeepEqual(perm["allow"], []any{"Bash(bflow *)"}) {
			t.Errorf("permissions.allow: %v", perm["allow"])
		}
		if !reflect.DeepEqual(m["attribution"], map[string]any{"commit": "", "pr": ""}) {
			t.Errorf("attribution: %v", m["attribution"])
		}
	}
}

func TestMergeSettingsKeepsUserHooksAndOrder(t *testing.T) {
	cur := `{
  "model": "opus",
  "permissions": { "allow": ["Bash(ls)"], "deny": ["Read(.env)"] },
  "hooks": {
    "PreToolUse": [{ "matcher": "Bash", "hooks": [{ "type": "command", "command": "echo mio" }] }],
    "Notification": [{ "hooks": [{ "type": "command", "command": "notify" }] }]
  },
  "env": { "A": "1" }
}`
	m, out, _, _ := merge(t, cur)
	pre := commands(t, m, "PreToolUse")
	if len(pre) != 2 || pre[0][0] != "echo mio" || pre[1][0] != "bflow guard" {
		t.Errorf("PreToolUse: la del usuario primero y la de bflow al final: %v", pre)
	}
	if n := commands(t, m, "Notification"); len(n) != 1 || n[0][0] != "notify" {
		t.Errorf("un evento que bflow no usa queda intacto: %v", n)
	}
	if m["model"] != "opus" || m["env"].(map[string]any)["A"] != "1" {
		t.Errorf("claves desconocidas: %v", m)
	}
	perm := m["permissions"].(map[string]any)
	if !reflect.DeepEqual(perm["allow"], []any{"Bash(ls)", "Bash(bflow *)"}) || perm["deny"] == nil {
		t.Errorf("permissions: %v", perm)
	}
	idx := func(k string) int { return strings.Index(out, `"`+k+`"`) }
	if idx("model") >= idx("permissions") || !(idx("permissions") < idx("hooks") && idx("hooks") < idx("env") && idx("env") < idx("statusLine")) {
		t.Errorf("orden de claves del usuario, lo nuevo al final:\n%s", out)
	}
}

func TestMergeSettingsReplacesBflowHooks(t *testing.T) {
	cur := `{"hooks": {"SubagentStop": [
  {"hooks": [{"type": "command", "command": "mine-before"}]},
  {"hooks": [{"type": "command", "command": "bflow hook tokens", "timeout": 5}]},
  {"hooks": [{"type": "command", "command": "mine-mixed"}, {"type": "command", "command": "bflow hook tokens"}]},
  {"matcher": "bflow-.*", "hooks": [{"type": "command", "command": "bflow hook subagent-stop", "timeout": 5}]},
  {"hooks": [{"type": "command", "command": "mine-after"}]}
]}}`
	m, out, _, _ := merge(t, cur)
	got := commands(t, m, "SubagentStop")
	want := [][]string{{"mine-before"}, {"mine-mixed", "bflow hook tokens"}, {"mine-after"}, {"bflow hook tokens"}, {"bflow hook subagent-stop"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubagentStop = %v, quiero %v", got, want)
	}
	if strings.Contains(out, `"timeout": 5`) {
		t.Errorf("las entradas viejas de bflow se reemplazan por las del binario:\n%s", out)
	}
}

func TestMergeSettingsIdempotent(t *testing.T) {
	_, first, changed, _ := merge(t, `{"model": "opus", "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "mio"}]}]}}`)
	if !changed {
		t.Fatal("la primera vez cambia")
	}
	_, second, changed, _ := merge(t, first)
	if changed || second != first {
		t.Errorf("la segunda vez no cambia nada (changed=%v):\n%s\n---\n%s", changed, first, second)
	}
	_, _, changed, _ = merge(t, strings.ReplaceAll(first, "\n", "\r\n"))
	if changed {
		t.Error("CRLF en el archivo no cuenta como cambio")
	}
	if n := strings.Count(second, "bflow guard"); n != 1 {
		t.Errorf("hooks duplicados: %d", n)
	}
}

func TestMergeSettingsForeignStatusLine(t *testing.T) {
	m, _, _, warns := merge(t, `{"statusLine": {"type": "command", "command": "mi-linea"}}`)
	if m["statusLine"].(map[string]any)["command"] != "mi-linea" {
		t.Errorf("no se toca una statusLine ajena: %v", m["statusLine"])
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "statusLine") {
		t.Errorf("debe avisar de la statusLine: %v", warns)
	}
	_, _, _, warns = merge(t, `{}`)
	if len(warns) != 0 {
		t.Errorf("sin statusLine ajena no hay aviso: %v", warns)
	}
}

func TestMergeSettingsAttributionOnlyIfMissing(t *testing.T) {
	m, _, _, _ := merge(t, `{"attribution": {"commit": "Co-Authored-By: x"}}`)
	if !reflect.DeepEqual(m["attribution"], map[string]any{"commit": "Co-Authored-By: x"}) {
		t.Errorf("attribution existente no se toca: %v", m["attribution"])
	}
}

func TestMergeSettingsInvalidJSON(t *testing.T) {
	for _, cur := range []string{`{"hooks": `, `[1, 2]`, `nope`} {
		out, _, _, err := mergeSettings([]byte(cur), files.Settings)
		if err == nil || out != nil {
			t.Errorf("%q: debe fallar sin salida (err=%v)", cur, err)
		}
	}
	root := testutil.TempDir(t)
	path := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const bad = `{"hooks": [oops`
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Agent{}.InstallSettings(root)
	if err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Errorf("el error dice la ruta: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != bad {
		t.Errorf("un JSON inválido no se sobrescribe: %q", b)
	}
}

func TestInstallSettingsWritesOnlyOnChange(t *testing.T) {
	root := testutil.TempDir(t)
	r, err := Agent{}.InstallSettings(root)
	if err != nil || !r.Changed || filepath.Base(r.Path) != "settings.json" {
		t.Fatalf("primera vez: %+v %v", r, err)
	}
	first, err := os.ReadFile(r.Path)
	if err != nil || !bytes.Contains(first, []byte("bflow guard")) {
		t.Fatalf("settings.json: %v %s", err, first)
	}
	r, err = Agent{}.InstallSettings(root)
	if err != nil || r.Changed {
		t.Errorf("segunda vez no cambia: %+v %v", r, err)
	}
	if again, _ := os.ReadFile(r.Path); !bytes.Equal(first, again) {
		t.Error("el archivo cambió en la segunda corrida")
	}
}

func TestSkillState(t *testing.T) {
	home := testutil.TempDir(t)
	path, state := Agent{}.SkillState(home)
	if state != "missing" || path != filepath.Join(home, ".claude", "skills", "bflow", "SKILL.md") {
		t.Fatalf("sin skill: %q %q", path, state)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("vieja"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, state := (Agent{}).SkillState(home); state != "stale" {
		t.Errorf("distinta: %q", state)
	}
	got, err := Agent{}.InstallSkill(home)
	if err != nil || got != path {
		t.Fatalf("InstallSkill: %q %v", got, err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, files.Skill) {
		t.Error("InstallSkill sobrescribe con la skill embebida")
	}
	if _, state := (Agent{}).SkillState(home); state != "ok" {
		t.Errorf("recién instalada: %q", state)
	}
	crlf := bytes.ReplaceAll(files.Skill, []byte("\n"), []byte("\r\n"))
	if err := os.WriteFile(path, crlf, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, state := (Agent{}).SkillState(home); state != "ok" {
		t.Errorf("CRLF se normaliza: %q", state)
	}
}
