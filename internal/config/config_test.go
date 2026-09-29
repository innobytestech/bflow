package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

// fixture crea un repo y un directorio de config global aislados.
func fixture(t *testing.T, global, repo string) (repoDir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("BFLOW_CONFIG_HOME", home)
	repoDir = t.TempDir()
	if global != "" {
		write(t, filepath.Join(home, "config.yaml"), global)
	}
	if repo != "" {
		write(t, filepath.Join(repoDir, "bflow.yaml"), repo)
	}
	return repoDir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultsWithoutFiles(t *testing.T) {
	c, err := Load(fixture(t, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if c.Tracker.Adapter != "local" {
		t.Errorf("tracker por defecto %q, want local", c.Tracker.Adapter)
	}
	if c.VCS.Host != "" {
		t.Errorf("sin host remoto por defecto, got %q", c.VCS.Host)
	}
	if c.VCS.Remote != "origin" || c.VCS.BranchPrefix.Feature != "feature/" || c.VCS.BranchPrefix.Hotfix != "hotfix/" {
		t.Errorf("vcs por defecto: %+v", c.VCS)
	}
	if c.Flow.MaxQualityRounds != 2 || c.Flow.SLAHours != 24 {
		t.Errorf("flow por defecto: %+v", c.Flow)
	}
	if c.Sources.Repo != "" || c.Sources.Global != "" {
		t.Errorf("no debería haber fuentes: %+v", c.Sources)
	}
	if err := c.Flow.Core().Validate(); err != nil {
		t.Errorf("flow por defecto inválido: %v", err)
	}
}

const globalMS = `
profiles:
  acme:
    tracker: { adapter: plane, url: https://plane.example.com, workspace: acme-dev }
    vcs: { host: github, base_branch: dev }
    agent: claude
    check:
      steps:
        - { name: vet, run: "go vet ./..." }
  otro:
    vcs: { base_branch: main }
`

func TestThreeLayersMerge(t *testing.T) {
	repo := fixture(t, globalMS, `
profile: acme
stack: go
tracker: { project: API }
vcs: { repo: acme/acme-api }
check:
  steps:
    - { name: test, run: "go test ./..." }
`)
	c, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"tracker.adapter":   c.Tracker.Adapter,
		"tracker.url":       c.Tracker.URL,
		"tracker.workspace": c.Tracker.Workspace,
		"tracker.project":   c.Tracker.Project,
		"vcs.host":          c.VCS.Host,
		"vcs.base_branch":   c.VCS.BaseBranch,
		"vcs.repo":          c.VCS.Repo,
		"vcs.remote":        c.VCS.Remote, // default que sobrevive al merge
		"agent":             c.Agent,
	}
	expect := map[string]string{
		"tracker.adapter": "plane", "tracker.url": "https://plane.example.com", "tracker.workspace": "acme-dev",
		"tracker.project": "API", "vcs.host": "github", "vcs.base_branch": "dev",
		"vcs.repo": "acme/acme-api", "vcs.remote": "origin", "agent": "claude",
	}
	for k, v := range expect {
		if want[k] != v {
			t.Errorf("%s = %q, want %q", k, want[k], v)
		}
	}
	// Las listas se reemplazan, no se concatenan: el repo manda.
	if len(c.Check.Steps) != 1 || c.Check.Steps[0].Name != "test" {
		t.Errorf("check.steps: %+v", c.Check.Steps)
	}
	if c.Sources.Profile != "acme" || c.Sources.Repo == "" || c.Sources.Global == "" {
		t.Errorf("fuentes: %+v", c.Sources)
	}
}

func TestRepoOverridesProfileScalar(t *testing.T) {
	c, err := Load(fixture(t, globalMS, "profile: acme\nvcs: { base_branch: main }\ntracker: { project: X }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.VCS.BaseBranch != "main" || c.VCS.Host != "github" {
		t.Errorf("vcs: %+v", c.VCS)
	}
}

func TestProfileEnvOverride(t *testing.T) {
	repo := fixture(t, globalMS, "tracker: { project: API }\n")
	t.Setenv("BFLOW_PROFILE", "otro")
	c, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.VCS.BaseBranch != "main" || c.Sources.Profile != "otro" {
		t.Errorf("BFLOW_PROFILE no se aplicó: %+v %+v", c.VCS, c.Sources)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name, global, repo string
		want               []string // fragmentos que el mensaje debe contener
	}{
		{"perfil inexistente", globalMS, "profile: nope\n", []string{`perfil "nope"`, "acme", "otro"}},
		{"campo desconocido", "", "trackr: { adapter: local }\n", []string{"bflow.yaml", "línea 1: campo desconocido «trackr»"}},
		{"campo desconocido global", "profiles:\n  x:\n    vsc: {}\n", "", []string{"config.yaml", "vsc"}},
		{"plane sin url", "", "tracker: { adapter: plane, workspace: w, project: P }\n", []string{"tracker.url"}},
		{"plane sin proyecto", "", "tracker: { adapter: plane, url: https://x, workspace: w }\n", []string{"tracker.project"}},
		{"proyecto como uuid", "", "tracker: { adapter: plane, url: https://x, workspace: w, project: 00000000-0000-4000-8000-000000000001 }\n",
			[]string{"identificador legible", "API"}},
		{"secreto en yaml", "", "tracker: { adapter: plane, url: https://x, workspace: w, project: P, api_key: abc }\n",
			[]string{"api_key", "llavero", "bflow connect"}},
		{"secreto en perfil", "profiles:\n  p:\n    vcs: { token: abc }\n", "profile: p\n", []string{"token", "llavero"}},
		{"adaptador desconocido", "", "tracker: { adapter: trello }\n", []string{"tracker.adapter", "trello", "local", "plane"}},
		{"carril inválido", "", "flow:\n  lanes:\n    hotfix: [implementing, documenting, walkthrough, in_review, done]\n", []string{"hotfix", "quality"}},
		{"fase desconocida", "", "flow:\n  lanes:\n    light: [spec, coding, quality, walkthrough, in_review, done]\n", []string{"coding"}},
		{"paso sin run", "", "check:\n  steps:\n    - { name: vet }\n", []string{"check.steps[0].run"}},
		{"yaml roto", "", "tracker: [\n", []string{"bflow.yaml"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(fixture(t, c.global, c.repo))
			if err == nil {
				t.Fatal("esperaba error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("el error no menciona %q:\n%v", w, err)
				}
			}
		})
	}
}

func TestSeveralValidationErrorsAtOnce(t *testing.T) {
	_, err := Load(fixture(t, "", "tracker: { adapter: plane }\ncheck:\n  steps:\n    - { run: x }\nflow:\n  lanes:\n    hotfix: [implementing, documenting, walkthrough, in_review, done]\n"))
	if err == nil {
		t.Fatal("esperaba error")
	}
	for _, w := range []string{"tracker.url", "tracker.workspace", "tracker.project", "check.steps[0].name", "quality"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("falta %q en:\n%v", w, err)
		}
	}
}

func TestFlowCoreFromConfig(t *testing.T) {
	c, err := Load(fixture(t, "", `
stack: angular
flow:
  max_quality_rounds: 3
  lanes:
    light: [spec, implementing, paused, quality, documenting, walkthrough, in_review, done]
  security_audit: false
`))
	if err != nil {
		t.Fatal(err)
	}
	core := c.Flow.Core()
	if core.MaxQualityRounds != 3 {
		t.Errorf("rondas %d", core.MaxQualityRounds)
	}
	if !slices.Contains(core.Lanes[flow.Light], flow.Paused) {
		t.Error("paused debería estar en light")
	}
	if !slices.Equal(core.Lanes[flow.Full], flow.DefaultLanes()[flow.Full]) {
		t.Error("full debe conservar su default si no se sobrescribe")
	}
	q := core.Agents[flow.Quality]
	if slices.Contains(q, "security-auditor") || !slices.Contains(q, "ux-auditor") || !slices.Contains(q, "reviewer") {
		t.Errorf("agentes de calidad para angular sin seguridad: %v", q)
	}
	if sp := core.Agents[flow.Spec]; !slices.Equal(sp, []string{"ui-designer", "spec-author"}) {
		t.Errorf("agentes de spec en stack con UI: %v", sp)
	}
	if !core.UI {
		t.Error("el núcleo debe saber que el stack tiene UI (gate de spec con blueprint)")
	}
}

func TestStackDefaults(t *testing.T) {
	c, err := Load(fixture(t, "", "stack: go\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.Check.CodePaths, "internal") || !slices.Contains(c.Guard.TestPatterns, "*_test.go") {
		t.Errorf("defaults del stack go: code=%v tests=%v", c.Check.CodePaths, c.Guard.TestPatterns)
	}
	c2, err := Load(fixture(t, "", "stack: go\ncheck: { code_paths: [src] }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c2.Check.CodePaths, []string{"src"}) {
		t.Errorf("el repo debe poder sobrescribir code_paths: %v", c2.Check.CodePaths)
	}
}

func TestGlobalDir(t *testing.T) {
	t.Setenv("BFLOW_CONFIG_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", `C:\Users\x\AppData\Roaming`)
	t.Setenv("HOME", "/home/x")
	d := GlobalDir()
	switch runtime.GOOS {
	case "windows":
		if d != filepath.Join(`C:\Users\x\AppData\Roaming`, "bflow") {
			t.Errorf("windows: %s", d)
		}
	default:
		if d != filepath.Join("/home/x", ".config", "bflow") {
			t.Errorf("unix: %s", d)
		}
	}
	t.Setenv("BFLOW_CONFIG_HOME", "/tmp/otro")
	if GlobalDir() != "/tmp/otro" {
		t.Errorf("BFLOW_CONFIG_HOME no se respeta: %s", GlobalDir())
	}
}

func TestFindRepoRootWalksUp(t *testing.T) {
	repo := fixture(t, "", "stack: go\n")
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if c.Root != repo || c.Stack != "go" {
		t.Errorf("root=%s stack=%s", c.Root, c.Stack)
	}
}

func TestUIWatchIsPersonal(t *testing.T) {
	// Preferencia personal en la config global; el repo puede apagarla.
	c, err := Load(fixture(t, "ui: { watch: true }\n", "stack: go\n"))
	if err != nil || !c.UI.Watch {
		t.Fatalf("global: %v %+v", err, c.UI)
	}
	c, err = Load(fixture(t, "ui: { watch: true }\n", "ui: { watch: false }\n"))
	if err != nil || c.UI.Watch {
		t.Errorf("el repo la pisa: %v %+v", err, c.UI)
	}
	if c, err := Load(fixture(t, "", "ui: { watch: true }\n")); err != nil || !c.UI.Watch {
		t.Errorf("en bflow.yaml: %v", err)
	}
}

func TestSecurityAuditAddsSeparateAuditor(t *testing.T) {
	for _, c := range []struct {
		yaml string
		want []string
	}{
		{"stack: go\n", []string{"reviewer"}},
		{"stack: go\nflow: { security_audit: true }\n", []string{"reviewer", "security-auditor"}},
		{"stack: go\nflow: { security_audit: false }\n", []string{"reviewer"}},
		{"stack: angular\nflow: { security_audit: true }\n", []string{"reviewer", "ux-auditor", "security-auditor"}},
	} {
		cfg, err := Load(fixture(t, "", c.yaml))
		if err != nil {
			t.Fatal(err)
		}
		if q := cfg.Flow.Core().Agents[flow.Quality]; !slices.Equal(q, c.want) {
			t.Errorf("%q: %v, want %v", c.yaml, q, c.want)
		}
	}
}

func TestSecurityAuditorConfigAfterFusion(t *testing.T) {
	// Un repo que ajustó al security-auditor: el error dice cómo seguir.
	_, err := Load(fixture(t, "", "stack: go\nagents:\n  security-auditor:\n    extra: docs/sec.md\n"))
	if err == nil || !strings.Contains(err.Error(), "Pasa su read y extra a agents.reviewer, o pon flow.security_audit: true") {
		t.Errorf("error: %v", err)
	}
	if _, err := Load(fixture(t, "", "stack: go\nflow: { security_audit: true }\nagents:\n  security-auditor:\n    extra: docs/sec.md\n")); err != nil {
		t.Errorf("con security_audit: true sus ajustes valen: %v", err)
	}
}
