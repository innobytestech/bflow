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

// input arma la entrada que el plugin le pasa a `bflow guard --tool opencode`.
func input(t *testing.T, tool string, args any, extra map[string]any) []byte {
	t.Helper()
	m := map[string]any{"tool": tool, "args": args, "sessionID": "ses_1", "cwd": "", "agent": "", "subagent": false}
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
	if !ok || gotCwd != cwd || !reflect.DeepEqual(acts, want) {
		t.Fatalf("bash: ok=%v cwd=%q acts=%+v, want %+v", ok, gotCwd, acts, want)
	}
}

func TestParseActionsEditWrite(t *testing.T) {
	cwd := t.TempDir()
	f := filepath.Join(cwd, "a.go")
	for tool, want := range map[string]string{"edit": guard.Edit, "multiedit": guard.Edit, "write": guard.Write} {
		acts, _, ok := Agent{}.ParseActions(input(t, tool, map[string]any{"filePath": f}, map[string]any{"cwd": cwd}))
		if !ok || !reflect.DeepEqual(acts, []guard.Action{{Tool: want, Path: f}}) {
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
	for _, tool := range []string{"grep", "glob", "task", "webfetch", "inventada"} {
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
	if n := len(got["task"]); n != 0 {
		t.Errorf("task no genera acciones: %d", n)
	}
	want := []guard.Action{{Tool: guard.Write, Path: filepath.Join(cwd, "b.txt")}, {Tool: guard.Edit, Path: filepath.Join(cwd, "a.txt")}}
	if p := got["apply_patch"]; !reflect.DeepEqual(p, want) {
		t.Errorf("apply_patch: %+v, want %+v", p, want)
	}
}

// R1: read llega al guard como acción propia, con el agente de la subsesión.
func TestParseActionsRead(t *testing.T) {
	cwd := t.TempDir()
	acts, _, ok := Agent{}.ParseActions(input(t, "read", map[string]any{"filePath": "internal/a.go"},
		map[string]any{"cwd": cwd, "subagent": true, "agent": "bflow-reviewer"}))
	want := []guard.Action{{Tool: guard.Read, Path: filepath.Join(cwd, "internal/a.go"), Subagent: true, Agent: "bflow-reviewer"}}
	if !ok || !reflect.DeepEqual(acts, want) {
		t.Errorf("read: ok=%v acts=%+v, quiero %+v", ok, acts, want)
	}
	f := filepath.Join(cwd, "b.go")
	acts, _, _ = Agent{}.ParseActions(input(t, "read", map[string]any{"filePath": f}, map[string]any{"cwd": cwd}))
	if !reflect.DeepEqual(acts, []guard.Action{{Tool: guard.Read, Path: f}}) {
		t.Errorf("ruta absoluta de la sesión principal: %+v", acts)
	}
}
