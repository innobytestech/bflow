package opencode

import (
	"bytes"
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
	if n := strings.Count(strings.TrimRight(src, "\n"), "\n") + 1; n > 100 {
		t.Errorf("el plugin tiene %d líneas, máximo 100", n)
	}
}

// R17: read solo se manda al guard desde la subsesión del reviewer, y con --reads.
func TestPluginReadsOnlyReviewer(t *testing.T) {
	src := string(files.Plugin)
	guarded := regexp.MustCompile(`GUARDED\s*=\s*new Set\(\[([^\]]*)\]`).FindStringSubmatch(src)
	if guarded == nil || !strings.Contains(guarded[1], `"read"`) {
		t.Fatalf("GUARDED debe incluir \"read\": %v", guarded)
	}
	if !strings.Contains(src, `"--reads"`) {
		t.Error(`el comando del guard lleva "--reads"`)
	}
	// Antes de lanzar el proceso, un read de otra sesión sale sin hacer nada.
	iRead := strings.Index(src, `input.tool === "read"`)
	iRun := strings.Index(src, `run(["guard"`)
	if iRead < 0 || iRun < 0 || iRead > iRun {
		t.Fatalf("falta el filtro de read antes de run(guard): read=%d run=%d", iRead, iRun)
	}
	between := src[iRead:iRun]
	if !strings.Contains(between, "bflow-reviewer") || !strings.Contains(between, "return") {
		t.Errorf("el filtro compara con bflow-reviewer y sale con return:\n%s", between)
	}
	if n := strings.Count(strings.TrimRight(src, "\n"), "\n") + 1; n > 100 {
		t.Errorf("el plugin tiene %d líneas, máximo 100", n)
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
	if n := strings.Count(strings.TrimRight(src, "\n"), "\n") + 1; n > 100 {
		t.Errorf("el plugin tiene %d líneas, máximo 100", n)
	}
}
