package claude

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/agents"
	"innobytes.tech/bflow/internal/testutil"
)

func put(t *testing.T, path, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStartupContext(t *testing.T) {
	base, home := testutil.TempDir(t), testutil.TempDir(t)
	root := filepath.Join(base, "work", "api")
	put(t, filepath.Join(base, "work", "CLAUDE.md"), "reglas del monorepo\n")
	put(t, filepath.Join(root, "CLAUDE.md"), "Lee @docs/arch.md y @~/.claude/mias.md.\n`@docs/no.md` es código.\n```\n@docs/tampoco.md\n```\ncorreo a @equipo\n")
	put(t, filepath.Join(root, "docs", "arch.md"), "arquitectura @otra.md\n")
	put(t, filepath.Join(root, "docs", "otra.md"), "otra\n")
	put(t, filepath.Join(root, "docs", "no.md"), strings.Repeat("x", 999))
	put(t, filepath.Join(home, ".claude", "mias.md"), "mías\n")
	put(t, filepath.Join(home, ".claude", "CLAUDE.md"), "globales\n")
	put(t, filepath.Join(root, ".claude", "rules", "go.md"), "siempre\n")
	put(t, filepath.Join(root, ".claude", "rules", "sql.md"), "---\npaths: [\"**/*.sql\"]\n---\nsolo al leer SQL\n")
	put(t, filepath.Join(root, ".claude", "agents", "bflow-reviewer.md"), "---\nname: bflow-reviewer\ndescription: Revisa el diff.\n---\n")
	proj := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(root, "-")
	put(t, filepath.Join(home, ".claude", "projects", strings.ToLower(proj), "memory", "MEMORY.md"), strings.Repeat("- [nota](n.md) — algo\n", 250))

	got := map[string]agents.ContextSource{}
	for _, s := range startupContext(root, home, []agents.Skill{{Name: "bflow", Description: "Conduce el flujo."}}) {
		got[s.Label] = s
	}
	for _, want := range []string{"~/.claude/CLAUDE.md", "CLAUDE.md", "CLAUDE.md → @docs/arch.md", "CLAUDE.md → @docs/arch.md → @otra.md",
		"CLAUDE.md → @~/.claude/mias.md", ".claude/rules/go.md", filepath.ToSlash(filepath.Join(base, "work", "CLAUDE.md")),
		"MEMORY.md (memoria automática)", "descripciones de skills y agentes"} {
		if _, ok := got[want]; !ok {
			t.Errorf("falta %q", want)
		}
	}
	for l := range got {
		if strings.Contains(l, "no.md") || strings.Contains(l, "tampoco") || strings.Contains(l, "sql.md") || strings.Contains(l, "equipo") {
			t.Errorf("no se carga al iniciar: %q", l)
		}
	}
	if m := got["MEMORY.md (memoria automática)"]; m.Bytes != 200*len("- [nota](n.md) — algo\n") || !strings.Contains(m.Note, "se corta") {
		t.Errorf("memoria: solo 200 líneas y avisa del corte: %+v", m)
	}
}

func TestMemoryLoadedByteLimit(t *testing.T) {
	line := strings.Repeat("a", 1000) + "\n"
	n, cut := memoryLoaded([]byte(strings.Repeat(line, 30)))
	if n != 25*len(line) || !cut {
		t.Errorf("25 KB: %d %v", n, cut)
	}
	if n, cut := memoryLoaded([]byte("uno\ndos")); n != 7 || cut {
		t.Errorf("corto y sin salto final: %d %v", n, cut)
	}
}

func TestCoauthorOff(t *testing.T) {
	root, home := testutil.TempDir(t), testutil.TempDir(t)
	if coauthorOff(root, home) {
		t.Fatal("sin configuración Claude agrega el trailer")
	}
	put(t, filepath.Join(home, ".claude", "settings.json"), `{"includeCoAuthoredBy": false}`)
	if !coauthorOff(root, home) {
		t.Error("includeCoAuthoredBy: false del usuario")
	}
	put(t, filepath.Join(root, ".claude", "settings.json"), `{"attribution": {"commit": "Co-Authored-By: Claude"}}`)
	if coauthorOff(root, home) {
		t.Error("el proyecto manda sobre el usuario")
	}
	put(t, filepath.Join(root, ".claude", "settings.local.json"), `{"attribution": {"commit": ""}}`)
	if !coauthorOff(root, home) {
		t.Error("local manda sobre proyecto")
	}
	put(t, filepath.Join(root, ".claude", "settings.local.json"), `{"attribution": {"commit": false}}`)
	if !coauthorOff(root, home) {
		t.Error("commit: false")
	}
}
