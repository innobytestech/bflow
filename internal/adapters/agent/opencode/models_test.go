package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// userDir fija XDG_CONFIG_HOME en una carpeta nueva y devuelve <xdg>/opencode.
func userDir(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("BFLOW_CONFIG_HOME", t.TempDir())
	return filepath.Join(xdg, "opencode")
}

func put(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s no es JSON: %v\n%s", path, err, b)
	}
	return m
}

func agentOf(m map[string]any, name string) (map[string]any, bool) {
	ags, _ := m["agent"].(map[string]any)
	a, ok := ags[name].(map[string]any)
	return a, ok
}

func pasteOf(t *testing.T, paste string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(paste), &m); err != nil {
		t.Fatalf("Paste no es JSON: %v\n%s", err, paste)
	}
	return m
}

// R6, R9: sin archivo y con overrides se crea opencode.json; sin overrides no se crea nada.
func TestSyncModelsCreate(t *testing.T) {
	dir := userDir(t)
	want := map[string]string{"bflow-implementer": "p/m1", "bflow-reviewer": "p/m2"}

	res, err := Agent{}.SyncModels("", map[string]string{})
	if err != nil || res.Changed {
		t.Fatalf("sin overrides no hay cambio: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode.json")); !os.IsNotExist(err) {
		t.Fatal("sin overrides y sin archivo no se crea opencode.json")
	}

	res, err = Agent{}.SyncModels("", want)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "opencode.json")
	if !res.Changed || res.Paste != "" || res.Path != path {
		t.Fatalf("crea el archivo: %+v", res)
	}
	m := readJSON(t, path)
	for name, model := range want {
		if a, ok := agentOf(m, name); !ok || a["model"] != model {
			t.Errorf("agent.%s.model = %v, quiero %s", name, a, model)
		}
	}
	b, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(b), "}\n") {
		t.Errorf("termina con salto de línea: %q", b)
	}
}

// R7: conserva todo lo que no sea agent.bflow-*.model; limpia lo que ya no corresponde.
func TestSyncModelsPreserve(t *testing.T) {
	dir := userDir(t)
	path := put(t, dir, "opencode.json", `{
  "$schema": "https://opencode.ai/config.json",
  "provider": {"mio": {"options": {"apiKey": "secreto"}}},
  "agent": {
    "otro": {"model": "x/y", "prompt": "hola"},
    "bflow-implementer": {"model": "viejo/m", "prompt": "mi prompt"},
    "bflow-reviewer": {"model": "viejo/r"},
    "bflow-scout": {"model": "viejo/s", "temperature": 0.2}
  }
}`)
	want := map[string]string{"bflow-implementer": "nuevo/m"}
	res, err := Agent{}.SyncModels("", want)
	if err != nil || !res.Changed {
		t.Fatalf("hay cambios: %+v %v", res, err)
	}
	m := readJSON(t, path)
	if m["$schema"] != "https://opencode.ai/config.json" {
		t.Errorf("conserva $schema: %v", m)
	}
	prov, _ := m["provider"].(map[string]any)
	if mio, _ := prov["mio"].(map[string]any); mio == nil || mio["options"].(map[string]any)["apiKey"] != "secreto" {
		t.Errorf("conserva provider: %v", m["provider"])
	}
	if a, ok := agentOf(m, "otro"); !ok || a["model"] != "x/y" || a["prompt"] != "hola" {
		t.Errorf("no toca agentes que no son bflow-*: %v", a)
	}
	if a, _ := agentOf(m, "bflow-implementer"); a["model"] != "nuevo/m" || a["prompt"] != "mi prompt" {
		t.Errorf("actualiza el modelo y conserva otras claves: %v", a)
	}
	if _, ok := agentOf(m, "bflow-reviewer"); ok {
		t.Error("la entrada bflow-* que queda vacía se borra")
	}
	if a, ok := agentOf(m, "bflow-scout"); !ok || a["model"] != nil || a["temperature"] != 0.2 {
		t.Errorf("quita solo model de la entrada que no corresponde: %v", a)
	}
}

// R7: si nada cambia, no se escribe (el archivo queda byte a byte igual).
func TestSyncModelsNoop(t *testing.T) {
	dir := userDir(t)
	orig := `{"agent":{"bflow-implementer":{"model":"p/m"}},"x":1}`
	path := put(t, dir, "opencode.json", orig)
	res, err := Agent{}.SyncModels("", map[string]string{"bflow-implementer": "p/m"})
	if err != nil || res.Changed || res.Paste != "" || res.Path != path {
		t.Fatalf("sin cambios: %+v %v", res, err)
	}
	if b, _ := os.ReadFile(path); string(b) != orig {
		t.Errorf("no debe reescribir el archivo:\n%s", b)
	}
	// Sin agent y sin overrides tampoco hay cambio.
	path2 := put(t, userDir(t), "opencode.json", `{"x":1}`)
	if res, err := (Agent{}).SyncModels("", nil); err != nil || res.Changed {
		t.Fatalf("sin overrides ni entradas: %+v %v", res, err)
	}
	if b, _ := os.ReadFile(path2); string(b) != `{"x":1}` {
		t.Errorf("no debe reescribir: %s", b)
	}
}

// R9: con solo opencode.jsonc no se edita nada; se devuelve el bloque a pegar.
func TestSyncModelsJSONC(t *testing.T) {
	dir := userDir(t)
	orig := "{\n  // comentario\n  \"provider\": {},\n}\n"
	jsonc := put(t, dir, "opencode.jsonc", orig)
	res, err := Agent{}.SyncModels("", map[string]string{"bflow-implementer": "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Path != jsonc {
		t.Errorf("no cambia nada y apunta al .jsonc: %+v", res)
	}
	if a, ok := agentOf(pasteOf(t, res.Paste), "bflow-implementer"); !ok || a["model"] != "p/m" {
		t.Errorf("Paste trae el bloque agent: %q", res.Paste)
	}
	if b, _ := os.ReadFile(jsonc); string(b) != orig {
		t.Error("el .jsonc no se toca")
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode.json")); !os.IsNotExist(err) {
		t.Error("con .jsonc no se crea opencode.json")
	}
}

// R9: un opencode.json que no se lee como objeto no se sobrescribe.
func TestSyncModelsBadJSON(t *testing.T) {
	for _, bad := range []string{"{ no es json", "[1, 2]", "", "null"} {
		dir := userDir(t)
		path := put(t, dir, "opencode.json", bad)
		res, err := Agent{}.SyncModels("", map[string]string{"bflow-implementer": "p/m"})
		if err != nil {
			t.Fatalf("%q: no es un error de E/S: %v", bad, err)
		}
		if res.Changed || res.Paste == "" {
			t.Errorf("%q: no edita y devuelve Paste: %+v", bad, res)
		}
		if a, ok := agentOf(pasteOf(t, res.Paste), "bflow-implementer"); !ok || a["model"] != "p/m" {
			t.Errorf("%q: Paste: %q", bad, res.Paste)
		}
		if b, _ := os.ReadFile(path); string(b) != bad {
			t.Errorf("%q: el archivo no se toca: %q", bad, b)
		}
	}
}

// R11: ok / stale / manual.
func TestModelsState(t *testing.T) {
	want := map[string]string{"bflow-implementer": "p/m"}
	state := func(w map[string]string) string {
		_, s := Agent{}.ModelsState("", w)
		return s
	}

	dir := userDir(t)
	if s := state(nil); s != "ok" {
		t.Errorf("sin archivo y sin overrides: %q", s)
	}
	if s := state(want); s != "stale" {
		t.Errorf("sin archivo y con overrides: %q", s)
	}
	path := put(t, dir, "opencode.json", `{"agent":{"bflow-implementer":{"model":"p/m"}}}`)
	if sy, s := (Agent{}).ModelsState("", want); s != "ok" || sy.Path != path {
		t.Errorf("coincide: %q %+v", s, sy)
	}
	put(t, dir, "opencode.json", `{"agent":{"bflow-implementer":{"model":"otro/m"}}}`)
	if s := state(want); s != "stale" {
		t.Errorf("modelo distinto: %q", s)
	}
	put(t, dir, "opencode.json", `{"agent":{"bflow-implementer":{"model":"p/m"},"bflow-scout":{"model":"x/y"}}}`)
	if s := state(want); s != "stale" {
		t.Errorf("sobra un override bflow-*: %q", s)
	}
	put(t, dir, "opencode.json", `{"agent":{"propio":{"model":"x/y"}}}`)
	if s := state(nil); s != "ok" {
		t.Errorf("los agentes que no son bflow-* no cuentan: %q", s)
	}
	put(t, dir, "opencode.json", "{ roto")
	if sy, s := (Agent{}).ModelsState("", want); s != "manual" || !strings.Contains(sy.Paste, "bflow-implementer") {
		t.Errorf("JSON ilegible: %q %+v", s, sy)
	}
	put(t, userDir(t), "opencode.jsonc", "{}")
	if s := state(want); s != "manual" {
		t.Errorf("solo .jsonc: %q", s)
	}
}
