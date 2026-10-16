package opencode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"innobytes.tech/bflow/internal/guard"
)

// stripFP quita la huella (no determinista salvo por herramienta+entrada) para
// comparar el resto de los campos de la acción.
func stripFP(acts []guard.Action) []guard.Action {
	if acts == nil {
		return nil
	}
	out := make([]guard.Action, len(acts))
	for i, a := range acts {
		a.Fingerprint = ""
		out[i] = a
	}
	return out
}

// input arma la entrada que el plugin le pasa a `bflow guard --tool opencode`.
func input(t *testing.T, tool string, args any, extra map[string]any) []byte {
	t.Helper()
	m := map[string]any{"tool": tool, "args": args, "sessionID": "", "cwd": "", "agent": "", "subagent": false}
	for k, v := range extra {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseActionsBash(t *testing.T) {
	cwd := t.TempDir()
	acts, gotCwd, ok := Agent{}.ParseActions(input(t, "bash", map[string]any{"command": "git reset --hard"},
		map[string]any{"cwd": cwd, "subagent": true, "agent": "bflow-implementer"}))
	want := []guard.Action{{Tool: guard.Bash, Command: "git reset --hard", Subagent: true, Agent: "bflow-implementer"}}
	if !ok || gotCwd != cwd || !reflect.DeepEqual(stripFP(acts), want) {
		t.Fatalf("bash: ok=%v cwd=%q acts=%+v, want %+v", ok, gotCwd, acts, want)
	}
}

func TestParseActionsEditWrite(t *testing.T) {
	cwd := t.TempDir()
	f := filepath.Join(cwd, "a.go")
	for tool, want := range map[string]string{"edit": guard.Edit, "multiedit": guard.Edit, "write": guard.Write} {
		acts, _, ok := Agent{}.ParseActions(input(t, tool, map[string]any{"filePath": f}, map[string]any{"cwd": cwd}))
		if !ok || !reflect.DeepEqual(stripFP(acts), []guard.Action{{Tool: want, Path: f}}) {
			t.Errorf("%s: ok=%v acts=%+v, want una acción %s sobre %s", tool, ok, acts, want, f)
		}
	}
}

const patchText = "*** Begin Patch\n" +
	"*** Add File: nuevo.go\n+package a\n" +
	"*** Update File: viejo.go\n@@\n-x\n+y\n" +
	"*** Delete File: borrado.go\n" +
	"*** Update File: origen.go\n*** Move to: destino.go\n@@\n-a\n+b\n" +
	"*** End Patch"

func TestParseActionsPatchVariasRutas(t *testing.T) {
	writes, edits := patchPaths(patchText)
	if !reflect.DeepEqual(writes, []string{"nuevo.go"}) ||
		!reflect.DeepEqual(edits, []string{"viejo.go", "borrado.go", "origen.go", "destino.go"}) {
		t.Fatalf("patchPaths: writes=%v edits=%v", writes, edits)
	}
	cwd := t.TempDir()
	for _, tool := range []string{"patch", "apply_patch"} {
		acts, _, ok := Agent{}.ParseActions(input(t, tool, map[string]any{"patchText": patchText}, map[string]any{"cwd": cwd, "agent": "x"}))
		if !ok || len(acts) != 5 {
			t.Fatalf("%s: una acción por ruta (5): ok=%v %+v", tool, ok, acts)
		}
		// Primero las escrituras y luego las ediciones, cada grupo en el orden del texto.
		order := []struct{ tool, file string }{{guard.Write, "nuevo.go"}, {guard.Edit, "viejo.go"}, {guard.Edit, "borrado.go"},
			{guard.Edit, "origen.go"}, {guard.Edit, "destino.go"}}
		for i, w := range order {
			if acts[i].Tool != w.tool || acts[i].Path != filepath.Join(cwd, w.file) || acts[i].Agent != "x" {
				t.Errorf("%s acción %d: %+v, want %s %s", tool, i, acts[i], w.tool, w.file)
			}
		}
	}
}

func TestParseActionsRutaRelativa(t *testing.T) {
	cwd := t.TempDir()
	acts, _, _ := Agent{}.ParseActions(input(t, "write", map[string]any{"filePath": "src/a.go"}, map[string]any{"cwd": cwd}))
	if len(acts) != 1 || acts[0].Path != filepath.Join(cwd, "src", "a.go") {
		t.Errorf("la ruta relativa se resuelve contra cwd: %+v", acts)
	}
	abs := filepath.Join(cwd, "ya", "absoluta.go")
	acts, _, _ = Agent{}.ParseActions(input(t, "edit", map[string]any{"filePath": abs}, map[string]any{"cwd": "otra"}))
	if len(acts) != 1 || acts[0].Path != abs {
		t.Errorf("una ruta absoluta no se toca: %+v", acts)
	}
}

func TestParseActionsHerramientaIgnorada(t *testing.T) {
	for _, tool := range []string{"grep", "glob", "webfetch", "inventada"} {
		acts, _, ok := Agent{}.ParseActions(input(t, tool, map[string]any{"filePath": "a.go", "command": "git reset --hard"}, nil))
		if !ok || len(acts) != 0 {
			t.Errorf("%s no le importa al guard: ok=%v acts=%+v", tool, ok, acts)
		}
	}
}

func TestParseActionsEntradaInvalida(t *testing.T) {
	for _, raw := range []string{"", "no es json", "{}", `{"args":{"command":"ls"}}`, `{"tool":""}`} {
		if acts, _, ok := (Agent{}).ParseActions([]byte(raw)); ok || len(acts) != 0 {
			t.Errorf("%q: ok=%v acts=%+v, want ok=false", raw, ok, acts)
		}
	}
	if _, _, ok := (Agent{}).ParseActions([]byte(`{"tool":"read","args":{}}`)); !ok {
		t.Errorf("con tool y args la entrada sí tiene la forma")
	}
}

// La captura real de OpenCode 1.1.53 (testdata/tool_before.jsonl): cada línea es
// {input:{tool,sessionID,callID}, output:{args}} como la recibe tool.execute.before.
func TestParseActionsCaptura(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "tool_before.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cwd := t.TempDir()
	got := map[string][]guard.Action{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var l struct {
			Input struct {
				Tool      string `json:"tool"`
				SessionID string `json:"sessionID"`
			} `json:"input"`
			Output struct {
				Args json.RawMessage `json:"args"`
			} `json:"output"`
		}
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		acts, _, ok := Agent{}.ParseActions(input(t, l.Input.Tool, l.Output.Args, map[string]any{"cwd": cwd, "sessionID": l.Input.SessionID}))
		if !ok {
			t.Fatalf("la captura de %s debe reconocerse", l.Input.Tool)
		}
		got[l.Input.Tool] = append(got[l.Input.Tool], acts...)
	}
	if b := got["bash"]; len(b) != 2 || b[0].Tool != guard.Bash || b[0].Command != "echo hola" || b[1].Command != "echo hijo" {
		t.Errorf("bash (sesión principal e hija): %+v", b)
	}
	if w := got["write"]; len(w) != 1 || w[0].Tool != guard.Write || w[0].Path != filepath.Join(cwd, "a.txt") {
		t.Errorf("write: %+v", w)
	}
	if e := got["edit"]; len(e) != 1 || e[0].Tool != guard.Edit || e[0].Path != filepath.Join(cwd, "a.txt") {
		t.Errorf("edit: %+v", e)
	}
	for _, a := range got["task"] {
		if a.Tool != guard.Spawn {
			t.Errorf("task es un lanzamiento: %+v", a)
		}
	}
	var ses string
	if p := got["apply_patch"]; len(p) > 0 {
		ses = p[0].Session
	}
	want := []guard.Action{{Tool: guard.Write, Path: filepath.Join(cwd, "b.txt"), Session: ses}, {Tool: guard.Edit, Path: filepath.Join(cwd, "a.txt"), Session: ses}}
	if p := got["apply_patch"]; ses == "" || !reflect.DeepEqual(stripFP(p), want) {
		t.Errorf("apply_patch: %+v, want %+v", p, want)
	}
}

// R1: read llega al guard como acción propia, con el agente de la subsesión.
func TestParseActionsRead(t *testing.T) {
	cwd := t.TempDir()
	acts, _, ok := Agent{}.ParseActions(input(t, "read", map[string]any{"filePath": "internal/a.go"},
		map[string]any{"cwd": cwd, "subagent": true, "agent": "bflow-reviewer"}))
	want := []guard.Action{{Tool: guard.Read, Path: filepath.Join(cwd, "internal/a.go"), Subagent: true, Agent: "bflow-reviewer"}}
	if !ok || !reflect.DeepEqual(stripFP(acts), want) {
		t.Errorf("read: ok=%v acts=%+v, quiero %+v", ok, acts, want)
	}
	f := filepath.Join(cwd, "b.go")
	acts, _, _ = Agent{}.ParseActions(input(t, "read", map[string]any{"filePath": f}, map[string]any{"cwd": cwd}))
	if !reflect.DeepEqual(stripFP(acts), []guard.Action{{Tool: guard.Read, Path: f}}) {
		t.Errorf("ruta absoluta de la sesión principal: %+v", acts)
	}
}

// R7: offset o limit con valor distinto de null marcan la lectura como parcial.
func TestParseActionsReadPartial(t *testing.T) {
	cases := []struct {
		args map[string]any
		want bool
	}{
		{map[string]any{"filePath": "x.go", "offset": 10}, true},
		{map[string]any{"filePath": "x.go", "limit": 50}, true},
		{map[string]any{"filePath": "x.go", "offset": 5, "limit": 50}, true},
		{map[string]any{"filePath": "x.go", "offset": nil, "limit": nil}, false},
		{map[string]any{"filePath": "x.go"}, false},
	}
	for _, c := range cases {
		acts, _, ok := Agent{}.ParseActions(input(t, "read", c.args, nil))
		if !ok || len(acts) != 1 || acts[0].Tool != guard.Read || acts[0].Path != "x.go" || acts[0].Partial != c.want {
			t.Errorf("%v: %+v ok=%v, Partial debe ser %v", c.args, acts, ok, c.want)
		}
	}
}

func TestParseActionsSpawn(t *testing.T) {
	acts, _, ok := Agent{}.ParseActions(input(t, "task",
		map[string]any{"subagent_type": "bflow-implementer", "prompt": `{"id":"GH-7","lane":"full"}`},
		map[string]any{"sessionID": "ses_9"}))
	want := []guard.Action{{Tool: guard.Spawn, Target: "bflow-implementer", Command: `{"id":"GH-7","lane":"full"}`, Session: "ses_9"}}
	if !ok || !reflect.DeepEqual(stripFP(acts), want) {
		t.Fatalf("task: ok=%v acts=%+v, want %+v", ok, acts, want)
	}
	acts, _, _ = Agent{}.ParseActions(input(t, "bash", map[string]any{"command": "bflow status GH-7"}, map[string]any{"sessionID": "ses_9"}))
	if len(acts) != 1 || acts[0].Session != "ses_9" {
		t.Errorf("toda acción lleva el sessionID: %+v", acts)
	}
}

// La captura real de OpenCode 2.0.22 (testdata/tool_before_v2.jsonl): cada línea es
// el x de tool.execute.before ({tool, sessionID, agent, input}). El plugin manda
// x.input como args.
func TestParseActionsV2Captura(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "tool_before_v2.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cwd := t.TempDir()
	got := map[string][]guard.Action{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var x struct {
			Tool      string          `json:"tool"`
			SessionID string          `json:"sessionID"`
			Input     json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(sc.Bytes(), &x); err != nil {
			t.Fatal(err)
		}
		acts, _, ok := Agent{}.ParseActions(input(t, x.Tool, x.Input, map[string]any{"cwd": cwd, "sessionID": x.SessionID, "subagent": true, "agent": "bflow-implementer"}))
		if !ok {
			t.Fatalf("la captura de %s debe reconocerse", x.Tool)
		}
		got[x.Tool] = append(got[x.Tool], acts...)
	}
	if b := got["shell"]; len(b) != 1 || b[0].Tool != guard.Bash || b[0].Command != "echo hola" || !b[0].Subagent {
		t.Errorf("shell es bash: %+v", b)
	}
	if w := got["write"]; len(w) != 1 || w[0].Tool != guard.Write || w[0].Path != filepath.Join(cwd, "b.txt") {
		t.Errorf("write con path: %+v", w)
	}
	if e := got["edit"]; len(e) != 1 || e[0].Tool != guard.Edit || e[0].Path != filepath.Join(cwd, "b.txt") {
		t.Errorf("edit con path: %+v", e)
	}
	if r := got["read"]; len(r) != 1 || r[0].Tool != guard.Read || r[0].Path != filepath.Join(cwd, "a.txt") || !r[0].Partial {
		t.Errorf("read con path, offset y limit es parcial: %+v", r)
	}
	if s := got["subagent"]; len(s) != 1 || s[0].Tool != guard.Spawn || s[0].Target != "general" || s[0].Command != "SUBTASK responde ok" {
		t.Errorf("subagent es un lanzamiento (agent y prompt): %+v", s)
	}
	p := got["patch"]
	if len(p) != 1 || p[0].Tool != guard.Write || p[0].Path != filepath.Join(cwd, "c.txt") {
		t.Errorf("patch (patchText) con Add File es una escritura: %+v", p)
	}
}

func TestParseActionsPathV2(t *testing.T) {
	cwd := t.TempDir()
	for _, tool := range []string{"edit", "write", "read"} {
		acts, _, ok := Agent{}.ParseActions(input(t, tool, map[string]any{"path": "src/a.go"}, map[string]any{"cwd": cwd}))
		if !ok || len(acts) != 1 || acts[0].Path != filepath.Join(cwd, "src", "a.go") {
			t.Errorf("%s: path relativo contra cwd: %+v", tool, acts)
		}
		abs := filepath.Join(cwd, "ya", "absoluta.go")
		acts, _, _ = Agent{}.ParseActions(input(t, tool, map[string]any{"path": abs}, map[string]any{"cwd": "otra"}))
		if len(acts) != 1 || acts[0].Path != abs {
			t.Errorf("%s: path absoluto no se toca: %+v", tool, acts)
		}
	}
	acts, _, _ := Agent{}.ParseActions(input(t, "write", map[string]any{"filePath": "uno.go", "path": "dos.go"}, map[string]any{"cwd": cwd}))
	if len(acts) != 1 || acts[0].Path != filepath.Join(cwd, "uno.go") {
		t.Errorf("filePath manda sobre path: %+v", acts)
	}
}

// Los nombres y argumentos de 1.x dan las mismas acciones que antes de GH-98.
func TestParseActionsV1SinCambios(t *testing.T) {
	cwd := t.TempDir()
	a := filepath.Join(cwd, "a.go")
	cases := []struct {
		tool string
		args map[string]any
		want []guard.Action
	}{
		{"bash", map[string]any{"command": "ls"}, []guard.Action{{Tool: guard.Bash, Command: "ls"}}},
		{"edit", map[string]any{"filePath": "a.go"}, []guard.Action{{Tool: guard.Edit, Path: a}}},
		{"multiedit", map[string]any{"filePath": "a.go"}, []guard.Action{{Tool: guard.Edit, Path: a}}},
		{"write", map[string]any{"filePath": "a.go"}, []guard.Action{{Tool: guard.Write, Path: a}}},
		{"read", map[string]any{"filePath": "a.go", "limit": 5}, []guard.Action{{Tool: guard.Read, Path: a, Partial: true}}},
		{"task", map[string]any{"subagent_type": "bflow-reviewer", "prompt": "p"}, []guard.Action{{Tool: guard.Spawn, Target: "bflow-reviewer", Command: "p"}}},
		{"apply_patch", map[string]any{"patchText": "*** Begin Patch\n*** Add File: n.go\n*** End Patch"}, []guard.Action{{Tool: guard.Write, Path: filepath.Join(cwd, "n.go")}}},
		{"grep", map[string]any{"pattern": "x"}, nil},
	}
	for _, c := range cases {
		acts, _, ok := Agent{}.ParseActions(input(t, c.tool, c.args, map[string]any{"cwd": cwd}))
		if !ok || !reflect.DeepEqual(stripFP(acts), c.want) {
			t.Errorf("%s: ok=%v %+v, want %+v", c.tool, ok, acts, c.want)
		}
	}
}

// GH-16 R17/R18: la huella depende de la herramienta y los args crudos; dos
// llamadas iguales dan la misma huella, y una distinta, otra.
func TestOpenCodeFingerprint(t *testing.T) {
	cwd := t.TempDir()
	acts1, _, ok := Agent{}.ParseActions(input(t, "bash", map[string]any{"command": "go test ./..."}, map[string]any{"cwd": cwd}))
	if !ok || len(acts1) != 1 || acts1[0].Fingerprint == "" {
		t.Fatalf("esperaba una huella no vacía: %+v ok=%v", acts1, ok)
	}
	acts2, _, _ := Agent{}.ParseActions(input(t, "bash", map[string]any{"command": "go test ./..."}, map[string]any{"cwd": cwd}))
	if acts2[0].Fingerprint != acts1[0].Fingerprint {
		t.Errorf("la misma herramienta y args debía dar la misma huella: %q != %q", acts1[0].Fingerprint, acts2[0].Fingerprint)
	}
	acts3, _, _ := Agent{}.ParseActions(input(t, "bash", map[string]any{"command": "go vet ./..."}, map[string]any{"cwd": cwd}))
	if acts3[0].Fingerprint == acts1[0].Fingerprint {
		t.Errorf("args distintos debían dar huellas distintas")
	}
}
