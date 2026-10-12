package opencode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	files "innobytes.tech/bflow/adapters/opencode"
	"innobytes.tech/bflow/internal/agents"
)

const pluginPath = ".opencode/plugins/bflow.js"

func TestRenderIncluyePlugin(t *testing.T) {
	out, err := Agent{}.RenderAgents([]agents.Spec{{Name: "implementer", Subagent: "bflow-implementer", Description: "d", Tools: []string{agents.Read}, Body: "x\n"}})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out[pluginPath]
	if !ok || !bytes.Equal(got, files.Plugin) {
		t.Fatalf("render debe incluir %s con el plugin embebido (%v)", pluginPath, keys(out))
	}
	if first, _, _ := strings.Cut(string(got), "\n"); first != "// "+agents.GeneratedMark {
		t.Errorf("la primera línea lleva la marca de generado: %q", first)
	}

	root := t.TempDir()
	if g := (Agent{}).GeneratedAgents(root); slices.Contains(g, pluginPath) {
		t.Errorf("sin archivo no hay plugin generado: %v", g)
	}
	full := filepath.Join(root, filepath.FromSlash(pluginPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("export const Mio = async () => ({})\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g := (Agent{}).GeneratedAgents(root); slices.Contains(g, pluginPath) {
		t.Errorf("un plugin ajeno (sin la marca) no cuenta como generado: %v", g)
	}
	if err := os.WriteFile(full, got, 0o644); err != nil {
		t.Fatal(err)
	}
	if g := (Agent{}).GeneratedAgents(root); !slices.Contains(g, pluginPath) {
		t.Errorf("el plugin con la marca sí: %v", g)
	}
}

// R2: el plugin corre en Bun dentro de OpenCode; solo puede importar módulos node:.
func TestPluginSinDependencias(t *testing.T) {
	src := string(files.Plugin)
	for _, w := range []string{"export const BflowPlugin", "tool.execute.before", "session.idle", "message.updated"} {
		if !strings.Contains(src, w) {
			t.Errorf("el plugin no trae %q", w)
		}
	}
	spec := regexp.MustCompile(`(?m)(?:\bfrom\s+|\bimport\s*\(\s*|\brequire\s*\(\s*|^\s*import\s+)["']([^"']+)["']`)
	for _, m := range spec.FindAllStringSubmatch(src, -1) {
		if !strings.HasPrefix(m[1], "node:") {
			t.Errorf("importa %q: solo se permiten módulos node:", m[1])
		}
	}
	if n := strings.Count(strings.TrimRight(src, "\n"), "\n") + 1; n > 120 {
		t.Errorf("el plugin tiene %d líneas, máximo 120", n)
	}
}

// GH-15 R8, R9: al crearse una sesión raíz, el plugin inyecta session-start sin pedir respuesta.
func TestPluginSessionStart(t *testing.T) {
	src := string(files.Plugin)
	for _, w := range []string{`"session-start"`, "noReply", "session.prompt", "parentID"} {
		if !strings.Contains(src, w) {
			t.Errorf("el plugin no trae %q", w)
		}
	}
	if !strings.Contains(src, "noReply: true") {
		t.Error("la inyección no pide respuesta al modelo (noReply: true)")
	}
	iEvt := strings.Index(src, `"session.created"`)
	iHook := strings.Index(src, `"session-start"`)
	if iEvt < 0 || iHook < 0 {
		t.Fatalf("faltan session.created o session-start")
	}
	if !regexp.MustCompile(`!\s*p\.info\.parentID`).MatchString(src) {
		t.Error("solo se inyecta en sesiones sin parentID")
	}
	if n := strings.Count(strings.TrimRight(src, "\n"), "\n") + 1; n > 120 {
		t.Errorf("el plugin tiene %d líneas, máximo 120", n)
	}
}

// R12: el read que no es del reviewer se lanza sin esperar (run con wait=false) y sale;
// el del reviewer y las demás herramientas esperan al guard.
func TestPluginReadsFireAndForget(t *testing.T) {
	src := string(files.Plugin)
	guarded := regexp.MustCompile(`GUARDED\s*=\s*new Set\(\[([^\]]*)\]`).FindStringSubmatch(src)
	if guarded == nil || !strings.Contains(guarded[1], `"read"`) {
		t.Fatalf("GUARDED debe incluir \"read\": %v", guarded)
	}
	if !regexp.MustCompile(`const run = \(args, input, wait = true\)`).MatchString(src) {
		t.Fatal("run(args, input, wait = true): con wait en false no se espera al proceso")
	}
	idx := func(sub string, from int) int {
		i := strings.Index(src[from:], sub)
		if i < 0 {
			t.Fatalf("falta %q", sub)
		}
		return from + i
	}
	if n := strings.Count(src, `"--reads"`); n != 2 {
		t.Fatalf("dos llamadas al guard con --reads (sin esperar y esperando): %d", n)
	}
	iRead := idx(`input.tool === "read"`, 0)
	i1 := idx(`"--reads"`, iRead)
	i2 := idx(`"--reads"`, i1+1)
	if !strings.Contains(src[iRead:i1], "bflow-reviewer") {
		t.Errorf("el filtro compara con bflow-reviewer antes de lanzar sin esperar:\n%s", src[iRead:i1])
	}
	fire := src[i1:i2]
	iFalse, iRet := strings.Index(fire, "false"), strings.Index(fire, "return")
	if iFalse < 0 || iRet < 0 || iFalse > iRet {
		t.Errorf("el read ajeno se lanza con false y luego sale con return:\n%s", fire)
	}
	if i1 > idx("proc.stderr", 0) {
		t.Error("el read sin esperar se lanza antes de leer stderr del proceso")
	}
	wait := src[i2:idx("proc.exited", i2)]
	if strings.Contains(wait, "false") {
		t.Errorf("la llamada que espera no lleva false:\n%s", wait)
	}
	if !strings.Contains(src, `stderr: "ignore"`) {
		t.Error(`sin esperar, el proceso se lanza con stderr: "ignore"`)
	}
	if n := strings.Count(strings.TrimRight(src, "\n"), "\n") + 1; n > 120 {
		t.Errorf("el plugin tiene %d líneas, máximo 120", n)
	}
}

// ---- plugin 2.x ----

const (
	plugin2Path = ".opencode/plugins/bflow2/index.js"
	package2    = ".opencode/plugins/bflow2/package.json"
)

func lines(s string) int { return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1 }

// R1, R5: render trae las tres rutas con la marca; GeneratedAgents solo cuenta las marcadas.
func TestRenderIncluyePlugin2(t *testing.T) {
	out, err := Agent{}.RenderAgents([]agents.Spec{{Name: "implementer", Subagent: "bflow-implementer", Description: "d", Tools: []string{agents.Read}, Body: "x\n"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{pluginPath: files.Plugin, plugin2Path: files.Plugin2, package2: files.Plugin2Package}
	for p, b := range want {
		got, ok := out[p]
		if !ok || !bytes.Equal(got, b) {
			t.Fatalf("render debe incluir %s con la fuente embebida (%v)", p, keys(out))
		}
		if !bytes.Contains(got, []byte(agents.GeneratedMark)) {
			t.Errorf("%s lleva la marca de generado", p)
		}
	}
	if first, _, _ := strings.Cut(string(files.Plugin2), "\n"); first != "// "+agents.GeneratedMark {
		t.Errorf("la primera línea de index.js lleva la marca: %q", first)
	}
	if !slices.Equal(PluginFiles, []string{pluginPath, plugin2Path, package2}) {
		t.Errorf("PluginFiles: %v", PluginFiles)
	}

	root := t.TempDir()
	for _, p := range []string{plugin2Path, package2} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if g := (Agent{}).GeneratedAgents(root); slices.Contains(g, p) {
			t.Errorf("%s sin la marca no cuenta como generado: %v", p, g)
		}
		if err := os.WriteFile(full, out[p], 0o644); err != nil {
			t.Fatal(err)
		}
		if g := (Agent{}).GeneratedAgents(root); !slices.Contains(g, p) {
			t.Errorf("%s con la marca sí: %v", p, g)
		}
	}
}

// R5, R6, R10, R11, R15: 2.x carga un módulo ESM con export default y solo importa node:.
func TestPlugin2SinDependencias(t *testing.T) {
	src := string(files.Plugin2)
	for _, w := range []string{"export default", "id:", "setup", "execute.before", "session.step.ended", "session.execution.succeeded"} {
		if !strings.Contains(src, w) {
			t.Errorf("el plugin 2.x no trae %q", w)
		}
	}
	if !regexp.MustCompile(`export default\s*\{\s*id:\s*"bflow"`).MatchString(src) {
		t.Error(`el export es export default { id: "bflow", ... }`)
	}
	spec := regexp.MustCompile(`(?m)(?:\bfrom\s+|\bimport\s*\(\s*|\brequire\s*\(\s*|^\s*import\s+)["']([^"']+)["']`)
	for _, m := range spec.FindAllStringSubmatch(src, -1) {
		if !strings.HasPrefix(m[1], "node:") {
			t.Errorf("importa %q: solo se permiten módulos node:", m[1])
		}
	}
	if n := lines(src); n > 160 {
		t.Errorf("el plugin 2.x tiene %d líneas, máximo 160", n)
	}
	if n := lines(string(files.Plugin)); n > 120 || !strings.Contains(string(files.Plugin), "export const BflowPlugin") {
		t.Errorf("bflow.js conserva su formato y sus 120 líneas: %d", n)
	}
}

// R6, R7, R8, R9, R14: el guard de 2.x.
func TestPlugin2Guard(t *testing.T) {
	src := string(files.Plugin2)
	g := regexp.MustCompile(`GUARDED\s*=\s*new Set\(\[([^\]]*)\]`).FindStringSubmatch(src)
	if g == nil {
		t.Fatal("falta GUARDED = new Set([...])")
	}
	for _, n := range []string{"shell", "edit", "write", "patch", "read", "subagent"} {
		if !strings.Contains(g[1], `"`+n+`"`) {
			t.Errorf("GUARDED debe incluir %q: %s", n, g[1])
		}
	}
	if !regexp.MustCompile(`const run = \(args, input, wait = true\)`).MatchString(src) {
		t.Fatal("run(args, input, wait = true)")
	}
	if n := strings.Count(src, `"--reads"`); n != 2 {
		t.Fatalf("dos llamadas al guard con --reads (sin esperar y esperando): %d", n)
	}
	i1 := strings.Index(src, `"--reads"`)
	i2 := i1 + 1 + strings.Index(src[i1+1:], `"--reads"`)
	fire := src[i1:i2]
	iFalse, iRet := strings.Index(fire, "false"), strings.Index(fire, "return")
	if iFalse < 0 || iRet < 0 || iFalse > iRet {
		t.Errorf("la llamada sin esperar lleva false y luego return:\n%s", fire)
	}
	if !strings.Contains(src[:i1], "bflow-reviewer") {
		t.Error("el filtro del read compara con bflow-reviewer antes de lanzar sin esperar")
	}
	if !strings.Contains(src, `stderr: "ignore"`) {
		t.Error(`sin esperar, el proceso se lanza con stderr: "ignore"`)
	}
	after := src[i2:]
	if iThrow := strings.Index(after, "throw new Error"); iThrow < 0 || !regexp.MustCompile(`(?s)===\s*2.*throw new Error`).MatchString(after) {
		t.Error("tras el código 2 se lanza Error con stderr")
	}
	if !strings.Contains(src, "10000") {
		t.Error("el guard espera hasta 10 s (10000) y luego mata el proceso")
	}
	if !strings.Contains(src, "x.input") {
		t.Error("2.x trae los argumentos de la herramienta en x.input")
	}
}

// R13: la inyección sale del hook prompt, una vez por sesión raíz.
func TestPlugin2SessionStart(t *testing.T) {
	src := string(files.Plugin2)
	for _, w := range []string{`"session-start"`, `session.hook("prompt"`, "synthetic", "parentID", "injected"} {
		if !strings.Contains(src, w) {
			t.Errorf("el plugin 2.x no trae %q", w)
		}
	}
	if strings.Contains(src, "noReply") {
		t.Error("2.x no usa noReply: la inyección es ctx.session.synthetic")
	}
}

// R1: package.json es válido, ESM y lleva la marca.
func TestPlugin2Package(t *testing.T) {
	var p struct {
		Name, Type, Main, Description string
	}
	if err := json.Unmarshal(files.Plugin2Package, &p); err != nil {
		t.Fatalf("package.json no es JSON válido: %v", err)
	}
	if p.Type != "module" || p.Main != "index.js" || p.Name != "bflow" || p.Description != agents.GeneratedMark {
		t.Errorf("package.json: %+v", p)
	}
	if !bytes.HasSuffix(files.Plugin2Package, []byte("}\n")) || bytes.Count(files.Plugin2Package, []byte("\n")) != 1 {
		t.Error("una línea con salto final")
	}
}

// GH-103: los tokens de ejecuciones fallidas o interrumpidas también se reportan.
func TestPlugin2TokensEnFallos(t *testing.T) {
	src := string(files.Plugin2)
	re := regexp.MustCompile(`ev\.type === "session\.execution\.(\w+)"`)
	got := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		got[m[1]] = true
	}
	for _, n := range []string{"succeeded", "failed", "interrupted"} {
		if !got[n] {
			t.Errorf("session.execution.%s debe disparar bflow hook tokens", n)
		}
	}
	i := strings.Index(src, `"session.execution.succeeded"`)
	if i < 0 || !strings.Contains(src[i:], `"hook", "tokens", "--tool", "opencode"`) {
		t.Error("las ejecuciones lanzan hook tokens")
	}
}

func TestPlugin2StepFailedEscribeCache(t *testing.T) {
	src := string(files.Plugin2)
	if !regexp.MustCompile(`ev\.type === "session\.step\.ended"\s*\|\|\s*ev\.type === "session\.step\.failed"`).MatchString(src) {
		t.Fatal("step.failed comparte la rama de step.ended (misma forma de línea)")
	}
	if !strings.Contains(src, "tokens: d.tokens") {
		t.Error("la línea de caché lleva tokens")
	}
	if !strings.Contains(src, "!d.tokens") {
		t.Error("step.failed sin tokens no escribe línea")
	}
}

func TestPlugin2StepFailedSinTokensLimpiaMapa(t *testing.T) {
	src := string(files.Plugin2)
	del := strings.Index(src, "steps.delete(d.assistantMessageID)")
	skip := strings.Index(src, `ev.type === "session.step.failed" && !d.tokens`)
	if del < 0 || skip < 0 || skip < del {
		t.Error("steps.delete debe ir antes del continue de step.failed sin tokens")
	}
}

// fixtureLines lee un archivo JSONL de testdata como objetos.
func fixtureLines(t *testing.T, name string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out = append(out, m)
	}
	return out
}

func sub(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

// GH-86 R19: tool.execute.after de 1.x revisa el bash que ya corrió y le agrega el motivo a la salida.
func TestPluginExecuteAfter(t *testing.T) {
	// La forma real (OpenCode 1.1.53): after trae callID pero no los argumentos; before guarda los suyos por callID.
	after := fixtureLines(t, "tool_after.jsonl")
	if len(after) == 0 {
		t.Fatal("tool_after.jsonl vacío")
	}
	for _, l := range after {
		if _, ok := sub(l, "input", "callID").(string); !ok {
			t.Errorf("after trae input.callID: %v", l)
		}
		if _, ok := sub(l, "output", "output").(string); !ok {
			t.Errorf("after trae output.output (texto que ve el modelo): %v", l)
		}
		if sub(l, "input", "args") != nil || sub(l, "output", "args") != nil {
			t.Errorf("after no trae los argumentos: %v", l)
		}
	}
	for _, l := range fixtureLines(t, "tool_before.jsonl") {
		if _, ok := sub(l, "input", "callID").(string); !ok {
			t.Errorf("before trae input.callID, la llave del Map: %v", l)
		}
	}

	src := string(files.Plugin)
	i := strings.Index(src, `"tool.execute.after": async (input, output)`)
	if i < 0 {
		t.Fatal(`falta "tool.execute.after": async (input, output)`)
	}
	blk := src[i:]
	for _, w := range []string{`input.tool === "bash"`, `"--post"`, "10000", "output.output =", "code === 2", "input.callID", ".delete(", "sessionID: input.sessionID", "warn("} {
		if !strings.Contains(blk, w) {
			t.Errorf("tool.execute.after no trae %q", w)
		}
	}
	if strings.Contains(blk, "throw") {
		t.Error("after nunca lanza: el comando ya corrió")
	}
	if !regexp.MustCompile(`\.set\(input\.callID`).MatchString(src[:i]) {
		t.Error("before guarda los argumentos del bash por input.callID")
	}
}

// GH-86 R20: execute.after de 2.x entrega el motivo en result.content (lo que ve el modelo).
func TestPlugin2ExecuteAfter(t *testing.T) {
	// La forma real (OpenCode 2.0.22): x trae input y result.content[].text; cambiar result.output.output no llega al modelo.
	x := fixtureLines(t, "tool_after_v2.jsonl")
	if len(x) == 0 || sub(x[0], "tool") != "shell" {
		t.Fatalf("tool_after_v2.jsonl: un shell: %v", x)
	}
	if c, ok := sub(x[0], "result", "content").([]any); !ok || len(c) == 0 || sub(c[0].(map[string]any), "text") == nil {
		t.Errorf("result.content[0].text: %v", x[0])
	}
	if sub(x[0], "input", "command") == nil {
		t.Errorf("x.input trae el comando: %v", x[0])
	}

	src := string(files.Plugin2)
	i := strings.Index(src, `ctx.tool.hook("execute.after"`)
	if i < 0 {
		t.Fatal(`falta ctx.tool.hook("execute.after"`)
	}
	blk := src[i:]
	if j := strings.Index(blk, "session.hook("); j > 0 {
		blk = blk[:j]
	}
	for _, w := range []string{`"shell"`, `"--post"`, "10000", "x.result.content", "code === 2", "x.input", "sessionID: x.sessionID"} {
		if !strings.Contains(blk, w) {
			t.Errorf("execute.after no trae %q", w)
		}
	}
	if strings.Contains(blk, "throw") {
		t.Error("after nunca lanza: el comando ya corrió")
	}
}
