package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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
	if !strings.Contains(rv, "**Seguridad**") || strings.Contains(rv, "security.md") {
		t.Errorf("el reviewer revisa seguridad, pero security.md es del security-auditor, que corre en paralelo:\n%s", rv)
	}
	if !strings.Contains(imp.Body, "No escribes changelogs") {
		t.Errorf("el implementer no debe escribir changelogs:\n%s", imp.Body)
	}
	if doc := m["documenter"].Body; !strings.Contains(doc, "ruta que recibes en `changelog`") || !strings.Contains(doc, "Es el único") || strings.Contains(doc, "consumer-changelog.md") {
		t.Errorf("el documenter escribe el único changelog, versionado:\n%s", doc)
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

func TestResolveModels(t *testing.T) {
	specs := []Spec{
		{Name: "implementer", Subagent: "bflow-implementer", Model: "sonnet"},
		{Name: "reviewer", Subagent: "bflow-reviewer", Model: "sonnet"},
		{Name: "documenter", Subagent: "bflow-documenter", Model: "haiku"},
		{Name: "spec-author", Subagent: "bflow-spec-author", Model: ""},
		{Name: "perf", Subagent: "bflow-perf", Model: "openai/gpt-5"},
		{Name: "otro", Subagent: "bflow-otro", Model: "opus"},
	}
	orig := append([]Spec(nil), specs...)
	out, un := ResolveModels(specs, map[string]string{"sonnet": "anthropic/claude-sonnet-4"})

	got := map[string]string{}
	for _, s := range out {
		got[s.Name] = s.Model
	}
	want := map[string]string{
		"implementer": "anthropic/claude-sonnet-4", // alias de la tabla
		"reviewer":    "anthropic/claude-sonnet-4",
		"documenter":  "",             // alias ausente: se omite
		"spec-author": "",             // vacío sigue vacío
		"perf":        "openai/gpt-5", // con "/" va tal cual
		"otro":        "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: model %q, quiero %q", k, got[k], v)
		}
	}
	if len(out) != len(specs) {
		t.Fatalf("mismos agentes: %d", len(out))
	}
	for i := range specs {
		if !reflect.DeepEqual(specs[i], orig[i]) {
			t.Errorf("no muta la entrada: %+v", specs[i])
		}
	}

	if len(un) != 2 || un[0].Alias != "haiku" || un[1].Alias != "opus" {
		t.Fatalf("sin resolver, ordenado por alias: %+v", un)
	}
	if !slices.Equal(un[0].Agents, []string{"bflow-documenter"}) || !slices.Equal(un[1].Agents, []string{"bflow-otro"}) {
		t.Errorf("agentes afectados: %+v", un)
	}

	_, un = ResolveModels(specs, map[string]string{"sonnet": "a/b", "haiku": "a/c", "opus": "a/d"})
	if len(un) != 0 {
		t.Errorf("con toda la tabla no queda nada sin resolver: %+v", un)
	}
	_, un = ResolveModels(specs, nil)
	if len(un) != 3 || !slices.Equal(un[1].Agents, []string{"bflow-otro"}) || !slices.Equal(un[2].Agents, []string{"bflow-implementer", "bflow-reviewer"}) {
		t.Errorf("sin tabla, cada alias con sus agentes ordenados: %+v", un)
	}
}

// R16: el documenter escribe reports/docs.md además del walkthrough.
func TestDocumenterWritesDocsReport(t *testing.T) {
	specs, err := Build(flow.DefaultConfig(), nil, testutil.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	doc := byName(t, specs)["documenter"].Body
	want := "- Escribes `.bflow/tasks/<id>/walkthrough.md` y `.bflow/tasks/<id>/reports/docs.md`. En `.bflow/` no tocas nada más."
	if !strings.Contains(doc, want) {
		t.Errorf("el contrato del documenter no lista reports/docs.md:\n%s", doc)
	}
}

// variableInPrefix: lo que cambia entre tareas o entre máquinas y rompería el
// prefijo de caché si entrara en el cuerpo de un agente (GH-19).
var variableInPrefix = []*regexp.Regexp{
	regexp.MustCompile(`[A-Z][A-Z0-9]*-[0-9]+`),
	regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}`),
	regexp.MustCompile(`\.bflow/tasks/[^<\s]`),
}

// allCatalogConfig arma un flujo con todos los agentes del catálogo y el scout.
func allCatalogConfig() flow.Config {
	fc := flow.DefaultConfig()
	fc.Agents = map[flow.Phase][]string{
		flow.Spec:         {"spec-author", "ui-designer"},
		flow.Contract:     {"implementer"},
		flow.Implementing: {"implementer"},
		flow.Quality:      {"reviewer", "security-auditor", "ux-auditor"},
		flow.Documenting:  {"documenter"},
	}
	fc.Scout = "scout"
	return fc
}

func TestAgentBodiesHaveNoTaskData(t *testing.T) {
	root := testutil.TempDir(t)
	specs, err := Build(allCatalogConfig(), nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 8 {
		t.Fatalf("todos los agentes del catálogo y el scout: %d", len(specs))
	}
	for _, s := range specs {
		pats := append([]*regexp.Regexp{regexp.MustCompile(regexp.QuoteMeta(root)), regexp.MustCompile(regexp.QuoteMeta(filepath.ToSlash(root)))}, variableInPrefix...)
		for _, re := range pats {
			if loc := re.FindStringIndex(s.Body); loc != nil {
				line := s.Body[strings.LastIndex(s.Body[:loc[0]], "\n")+1:]
				line, _, _ = strings.Cut(line, "\n")
				t.Errorf("%s: %q aparece en el cuerpo, rompe el prefijo de caché: %s", s.Name, re, line)
			}
		}
	}
}

func TestAgentSpecsSameAcrossRoots(t *testing.T) {
	a, err := Build(allCatalogConfig(), nil, testutil.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(allCatalogConfig(), nil, testutil.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("los Spec no dependen de la raíz del repo")
	}
}
