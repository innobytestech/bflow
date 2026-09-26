package setup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/testutil"
)

func files(t *testing.T, m map[string]string) string {
	t.Helper()
	dir := testutil.TempDir(t)
	for f, c := range m {
		p := filepath.Join(dir, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(c), 0o644)
	}
	return dir
}

func names(steps []StepProposal) string {
	var n []string
	for _, s := range steps {
		n = append(n, s.Name)
	}
	return strings.Join(n, ",")
}

func TestDetectGo(t *testing.T) {
	dir := files(t, map[string]string{"go.mod": "module x\n", "cmd/api/main.go": "package main", "internal/a.go": "package a"})
	d := Detect(dir, Tools{Has: func(n string) bool { return n == "golangci-lint" }})
	if d.Stack != "go" {
		t.Fatalf("stack %q", d.Stack)
	}
	if got := names(d.Steps); got != "vet,test,build,lint,vulns,secrets" {
		t.Errorf("pasos go: %s", got)
	}
	for _, s := range d.Steps {
		if s.Name == "lint" && (s.Needs != "golangci-lint" || s.Optional) {
			t.Errorf("lint instalado debe ser obligatorio: %+v", s)
		}
		if s.Name == "vulns" && (!s.Optional || s.Needs != "govulncheck") {
			t.Errorf("herramienta ausente queda opcional: %+v", s)
		}
	}
	if !slices.Contains(d.Quick, "go test -count=1 {pkg}") {
		t.Errorf("quick: %v", d.Quick)
	}
}

func TestDetectAngularFromPackageJSON(t *testing.T) {
	dir := files(t, map[string]string{
		"angular.json": "{}",
		"package.json": `{"scripts":{"start":"ng serve","build":"ng build","test":"ng test","test:e2e":"playwright test",
			"check:architecture":"node scripts/a.js","check:ui":"x","check:ui:full":"y","check:touch:list":"z","check:theme":"t","check:theme:hex":"h","check:all":"npm run a && b","lint":"ng lint"}}`,
	})
	d := Detect(dir, Tools{Has: func(string) bool { return true }})
	if d.Stack != "angular" {
		t.Fatalf("stack %q", d.Stack)
	}
	got := names(d.Steps)
	for _, want := range []string{"check:architecture", "check:ui:full", "lint", "test", "build"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %s en %s", want, got)
		}
	}
	for _, skip := range []string{"check:ui,", "check:all", "check:touch:list", "check:theme:hex", "start", "test:e2e"} {
		if strings.Contains(got+",", skip) {
			t.Errorf("no debería proponer %s (%s)", skip, got)
		}
	}
	for _, s := range d.Steps {
		if s.Name == "test" && !strings.Contains(s.Run, "--watch=false") {
			t.Errorf("las pruebas no deben quedarse en modo watch: %s", s.Run)
		}
	}
}

func TestDetectMakefile(t *testing.T) {
	dir := files(t, map[string]string{"Makefile": "build:\n\tgo build\ntest: build\n\tgo test\nlint:\n\tx\n.PHONY: test\ndeploy:\n\tx\n"})
	d := Detect(dir, Tools{Has: func(string) bool { return true }})
	if got := names(d.Steps); got != "lint,test,build" {
		t.Errorf("pasos desde Makefile: %s", got)
	}
}

func TestParseRemoteAndBase(t *testing.T) {
	cases := []struct {
		remote, head string
		branches     []string
		host, repo   string
		base         string
	}{
		{"https://github.com/acme/acme-api.git", "main", []string{"main", "dev"}, "github", "acme/acme-api", "dev"},
		{"git@github.com:innobytes/bflow.git", "main", []string{"main"}, "github", "innobytes/bflow", "main"},
		{"https://gitlab.com/a/b.git", "master", []string{"master", "develop"}, "", "a/b", "develop"},
		{"", "", nil, "", "", ""},
	}
	for _, c := range cases {
		host, repo := HostFromRemote(c.remote)
		base := PickBase(c.head, c.branches)
		if host != c.host || repo != c.repo || base != c.base {
			t.Errorf("%s → %q %q %q, want %q %q %q", c.remote, host, repo, base, c.host, c.repo, c.base)
		}
	}
}

func TestRenderYAMLIsShortAndValid(t *testing.T) {
	a := Answers{Stack: "go", Tracker: "plane", Project: "API", Profile: "acme", BaseBranch: "dev", Host: "github",
		Steps: []StepProposal{{Name: "vet", Run: "go vet ./..."}, {Name: "vulns", Run: "govulncheck ./...", Needs: "govulncheck", Optional: true}}}
	y, err := RenderYAML(a, Profile{Tracker: "plane", Host: "github", BaseBranch: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"profile: acme", "stack: go", "project: API", "go vet ./...", "optional: true"} {
		if !strings.Contains(y, want) {
			t.Errorf("falta %q:\n%s", want, y)
		}
	}
	// Lo que ya da el perfil no se repite en el repo.
	for _, dup := range []string{"adapter: plane", "host: github", "base_branch"} {
		if strings.Contains(y, dup) {
			t.Errorf("repite %q que ya está en el perfil:\n%s", dup, y)
		}
	}
}
