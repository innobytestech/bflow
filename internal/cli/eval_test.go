package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const evalCaseYAML = "source: GH-1\nbase: HEAD\nhead: HEAD\ndefects:\n  - { file: a.go, lines: \"1\", what: x }\n"

func writeEvalCase(t *testing.T, env *Env, name string) {
	t.Helper()
	dir := filepath.Join(env.Dir, ".bflow", "evals", "reviewer", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "case.yaml"), []byte(evalCaseYAML), 0o644); err != nil {
		t.Fatal(err)
	}
}

// El agente del eval solo existe si el repo tiene casos.
func TestRenderEvalAgentOnlyWithCases(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude, opencode]\nflow: { agents: { quality: [reviewer] } }\nagents:\n  reviewer: { model: sonnet, effort: high }\nmodels:\n  opencode: { sonnet: anthropic/claude-sonnet-4, haiku: anthropic/claude-haiku-4, opus: anthropic/claude-opus-4 }\n")
	const claudeRel, ocRel = ".claude/agents/bflow-eval-reviewer.md", ".opencode/agents/bflow-eval-reviewer.md"

	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render sin casos: %s", raw)
	}
	if exists(env, claudeRel) || exists(env, ocRel) {
		t.Fatal("sin .bflow/evals/reviewer/ no se genera el agente del eval")
	}
	if code, _, raw := runJSON(t, env, "render", "--check"); code != 0 {
		t.Fatalf("sin carpeta, el check no lo marca: %s", raw)
	}

	writeEvalCase(t, env, "caso-uno")
	if code, out, raw := runJSON(t, env, "render", "--check"); code != 1 || out["code"] != "render_outdated" {
		t.Fatalf("con casos falta el agente: %d %s", code, raw)
	}
	if code, _, raw := runJSON(t, env, "render"); code != 0 {
		t.Fatalf("render con casos: %s", raw)
	}
	cl := rendered(t, env, claudeRel)
	for _, want := range []string{"name: bflow-eval-reviewer", "model: sonnet", "bflow eval reviewer report <id> --verdict APPROVED|REJECTED"} {
		if !strings.Contains(cl, want) {
			t.Errorf("claude no dice %q:\n%s", want, cl)
		}
	}
	if strings.Contains(cl, "## Oficio\n") {
		t.Error("el agente del eval no trae oficio embebido: lo lee de craft")
	}
	oc := rendered(t, env, ocRel)
	if !strings.Contains(oc, "anthropic/claude-sonnet-4") {
		t.Errorf("opencode resuelve el modelo del reviewer:\n%s", oc)
	}
	if code, _, raw := runJSON(t, env, "render", "--check"); code != 0 {
		t.Fatalf("tras render el check pasa: %s", raw)
	}

	if err := os.RemoveAll(filepath.Join(env.Dir, ".bflow", "evals")); err != nil {
		t.Fatal(err)
	}
	if code, out, raw := runJSON(t, env, "render"); code != 0 || len(stringsOf(dataOf(out)["stale"])) != 2 {
		t.Fatalf("sin carpeta, el agente generado queda obsoleto y se quita: %s", raw)
	}
	if exists(env, claudeRel) || exists(env, ocRel) {
		t.Error("el agente obsoleto debe borrarse")
	}
}

// Sin el agente generado el comando manda a render antes de tocar nada.
func TestEvalAgentMissing(t *testing.T) {
	setHome(t)
	env := toolsEnv(t, "agent: [claude]\n")
	writeEvalCase(t, env, "caso-uno")

	code, out, raw := runJSON(t, env, "eval", "reviewer")
	d := dataOf(out)
	reason, _ := d["reason"].(string)
	if code != 2 || out["code"] != "eval_agent_missing" || !strings.Contains(reason, "bflow render") || !strings.Contains(reason, "reinici") {
		t.Fatalf("sin agente: %d %s", code, raw)
	}
	if exists(env, ".bflow/cache/evals") {
		t.Error("el rechazo no crea corrida")
	}
}
