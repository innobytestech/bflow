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
