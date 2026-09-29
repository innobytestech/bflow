package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/testutil"
)

func byName(t *testing.T, specs []Spec) map[string]Spec {
	t.Helper()
	m := map[string]Spec{}
	for _, s := range specs {
		m[s.Name] = s
	}
	return m
}

func TestBuildDefaultFlow(t *testing.T) {
	specs, err := Build(flow.DefaultConfig(), nil, testutil.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	m := byName(t, specs)
	if len(m) != 4 || m["security-auditor"].Name != "" {
		t.Fatalf("agentes del flujo por defecto (el reviewer revisa también seguridad): %v", m)
	}
	imp := m["implementer"]
	if imp.Subagent != "bflow-implementer" || imp.Model != "sonnet" || imp.Effort != "medium" || imp.OmitClaudeMd {
		t.Errorf("implementer: %+v", imp)
	}
	for _, want := range []string{
		"en contract: `bflow report <id> --agent implementer --verdict CONTRACT_READY|NEEDS_DECISION|BLOCKED`",
		"en implementing: `bflow report <id> --agent implementer --verdict DONE|NEEDS_DECISION|BLOCKED`",
		"DONE exige `bflow check <id>`",
		"quedan congeladas",
		"`.bflow/tasks/<id>/contract.md` y `.bflow/tasks/<id>/reports/impl.md`",
		"Tu respuesta final es solo la salida de `bflow report`",
		"## Oficio\n",
	} {
		if !strings.Contains(imp.Body, want) {
			t.Errorf("el contrato del implementer no dice %q", want)
		}
	}
	rv := m["reviewer"].Body
	if !strings.Contains(rv, "**Seguridad**") || !strings.Contains(rv, "reports/security.md") {
		t.Errorf("el oficio del reviewer incluye la revisión de seguridad:\n%s", rv)
	}
	if !strings.Contains(rv, "--verdict APPROVED|REJECTED") || strings.Contains(rv, "NEEDS_DECISION") || strings.Contains(rv, "DONE exige") {
		t.Errorf("el contrato del reviewer sale de la fase quality:\n%s", rv)
	}
	if sa := m["spec-author"].Body; !strings.Contains(sa, "Con SPLIT agrega") || !strings.Contains(sa, "tu parte de la spec") {
		t.Errorf("spec-author:\n%s", sa)
	}
	if m["documenter"].Model != "haiku" || m["documenter"].Effort != "low" {
		t.Errorf("documenter: %+v", m["documenter"])
	}
}

func TestBuildRepoCraftAndCustomAgent(t *testing.T) {
	root := testutil.TempDir(t)
	os.MkdirAll(filepath.Join(root, "docs", "bflow"), 0o755)
	os.WriteFile(filepath.Join(root, "docs", "bflow", "impl.md"), []byte("Commits de una línea.\n"), 0o644)
	os.WriteFile(filepath.Join(root, "docs", "bflow", "perf.md"), []byte("Mide p95 de los endpoints tocados.\n"), 0o644)
	fc := flow.DefaultConfig()
	fc.Agents[flow.Quality] = append(fc.Agents[flow.Quality], "perf-auditor")
	no := false
	conf := map[string]config.AgentConf{
		"implementer":  {Model: "opus", Effort: "high", Read: []string{"docs/architecture/"}, Extra: "docs/bflow/impl.md"},
		"reviewer":     {Read: []string{"docs/architecture/"}, OmitClaudeMd: &no},
		"perf-auditor": {Extra: "docs/bflow/perf.md"},
	}
	specs, err := Build(fc, conf, root)
	if err != nil {
		t.Fatal(err)
	}
	m := byName(t, specs)
	imp := m["implementer"]
	if imp.Model != "opus" || imp.Effort != "high" || !imp.OmitClaudeMd {
		t.Errorf("ajustes del repo: %+v", imp)
	}
	if !strings.Contains(imp.Body, "## Oficio del repo\n\nAntes de empezar, lee lo que toque a la tarea en: `docs/architecture/`.\n\nCommits de una línea.") {
		t.Errorf("oficio del repo:\n%s", imp.Body)
	}
	if m["reviewer"].OmitClaudeMd {
		t.Error("omit_claude_md: false manda sobre el default de read")
	}
	perf := m["perf-auditor"]
	if !strings.Contains(perf.Body, "`.bflow/tasks/<id>/reports/perf-auditor.md`") || !strings.Contains(perf.Body, "Mide p95") ||
		strings.Contains(perf.Body, "## Oficio\n") {
		t.Errorf("agente propio: contrato de bflow + su oficio, sin oficio por defecto:\n%s", perf.Body)
	}

	delete(conf, "perf-auditor")
	if _, err := Build(fc, conf, root); err == nil || !strings.Contains(err.Error(), "agents.perf-auditor.extra") {
		t.Errorf("un agente propio sin oficio debe fallar: %v", err)
	}
}

func TestCatalogHasCraft(t *testing.T) {
	for name := range catalog {
		if b, err := craftFS.ReadFile("craft/" + name + ".md"); err != nil || len(b) == 0 {
			t.Errorf("%s no tiene oficio por defecto: %v", name, err)
		}
	}
}
