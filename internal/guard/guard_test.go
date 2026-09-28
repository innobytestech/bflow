package guard

import (
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

func ctx() Context {
	return Context{
		Protected:      []string{"main", "master", "dev"},
		ProtectedPaths: []string{"cmd", "internal", "pkg"},
		TestPatterns:   []string{"*_test.go"},
		ForbidCoauthor: true,
	}
}

func TestBashRules(t *testing.T) {
	cases := []struct {
		cmd   string
		allow bool
		rule  string
	}{
		{"git status", true, ""},
		{"go test ./...", true, ""},
		{"git reset --hard HEAD~1", false, "git_destructive"},
		{"git reset --soft HEAD~1", true, ""},
		{"git clean -fd", false, "git_destructive"},
		{"git checkout -- internal/a.go", false, "git_destructive"},
		{"git checkout HEAD -- internal/a.go", false, "git_destructive"},
		{"git checkout -b feature/x", true, ""},
		{"git restore internal/a.go", false, "git_destructive"},
		{"git restore --staged internal/a.go", true, ""},
		{"git stash drop", false, "git_destructive"},
		{"git stash", true, ""},
		{"git push --force origin feature/x", false, "force_push"},
		{"git push -f", false, "force_push"},
		{"git push origin feature/x", true, ""},
		{"git push origin dev", false, "protected_branch"},
		{"git push origin HEAD:main", false, "protected_branch"},
		{"go vet ./... && git push origin master", false, "protected_branch"},
		{`git commit -m "feat: x" -m "Co-Authored-By: Claude <noreply@anthropic.com>"`, false, "coauthor"},
		{`git commit -m "feat: x"`, true, ""},
	}
	for _, c := range cases {
		d := Evaluate(Action{Tool: Bash, Command: c.cmd}, ctx())
		if d.Allow != c.allow || d.Rule != c.rule {
			t.Errorf("%q → allow=%v rule=%q, want allow=%v rule=%q", c.cmd, d.Allow, d.Rule, c.allow, c.rule)
		}
		if !d.Allow && d.Reason == "" {
			t.Errorf("%q: un bloqueo sin motivo", c.cmd)
		}
	}
}

func TestEditRules(t *testing.T) {
	c := ctx()
	cases := []struct {
		name  string
		a     Action
		mod   func(*Context)
		allow bool
		rule  string
	}{
		{"código normal", Action{Tool: Edit, Path: "internal/a.go"}, nil, true, ""},
		{".env", Action{Tool: Write, Path: ".env"}, nil, false, "env_file"},
		{".env.local", Action{Tool: Edit, Path: "config/.env.local"}, nil, false, "env_file"},
		{".env.example se permite", Action{Tool: Edit, Path: ".env.example"}, nil, true, ""},
		{"leader estricto en código", Action{Tool: Edit, Path: "internal/a.go"}, func(c *Context) { c.StrictLeader = true }, false, "leader_code"},
		{"subagente con leader estricto", Action{Tool: Edit, Path: "internal/a.go", Subagent: true}, func(c *Context) { c.StrictLeader = true }, true, ""},
		{"leader estricto fuera de código", Action{Tool: Edit, Path: "docs/x.md"}, func(c *Context) { c.StrictLeader = true }, true, ""},
		{"prueba congelada en implementing", Action{Tool: Edit, Path: "internal/a_test.go", Subagent: true},
			func(c *Context) { c.Phase = flow.Implementing; c.Frozen = []string{"internal/a_test.go"} }, false, "frozen_test"},
		{"prueba congelada en quality", Action{Tool: Write, Path: "internal/a_test.go"},
			func(c *Context) { c.Phase = flow.Quality; c.Frozen = []string{"internal/a_test.go"} }, false, "frozen_test"},
		{"prueba nueva se permite", Action{Tool: Write, Path: "internal/b_test.go"},
			func(c *Context) { c.Phase = flow.Implementing; c.Frozen = []string{"internal/a_test.go"} }, true, ""},
		{"en contract todavía se edita", Action{Tool: Edit, Path: "internal/a_test.go"},
			func(c *Context) { c.Phase = flow.Contract; c.Frozen = []string{"internal/a_test.go"} }, true, ""},
		{"borrar una prueba congelada por bash", Action{Tool: Bash, Command: "rm internal/a_test.go"},
			func(c *Context) { c.Phase = flow.Implementing; c.Frozen = []string{"internal/a_test.go"} }, false, "frozen_test"},
	}
	for _, tc := range cases {
		cc := c
		if tc.mod != nil {
			tc.mod(&cc)
		}
		d := Evaluate(tc.a, cc)
		if d.Allow != tc.allow || d.Rule != tc.rule {
			t.Errorf("%s → allow=%v rule=%q (%s), want allow=%v rule=%q", tc.name, d.Allow, d.Rule, d.Reason, tc.allow, tc.rule)
		}
	}
}

func TestBflowOwnedActions(t *testing.T) {
	active := ctx()
	active.Phase = flow.Implementing
	cases := []struct {
		a     Action
		c     Context
		allow bool
		rule  string
	}{
		// Con una tarea en curso, rama y PR los maneja bflow.
		{Action{Tool: Bash, Command: "gh pr create --fill"}, active, false, "bflow_pr"},
		{Action{Tool: Bash, Command: "go vet ./... && gh pr create -t x"}, active, false, "bflow_pr"},
		{Action{Tool: Bash, Command: "gh pr view 12"}, active, true, ""},
		{Action{Tool: Bash, Command: "git checkout -b feature/x"}, active, false, "bflow_branch"},
		{Action{Tool: Bash, Command: "git checkout -B feature/x origin/dev"}, active, false, "bflow_branch"},
		{Action{Tool: Bash, Command: "git switch -c feature/x"}, active, false, "bflow_branch"},
		{Action{Tool: Bash, Command: "git branch feature/x"}, active, false, "bflow_branch"},
		{Action{Tool: Bash, Command: "git branch --show-current"}, active, true, ""},
		{Action{Tool: Bash, Command: "git checkout feature/x"}, active, true, ""},
		{Action{Tool: Bash, Command: "git switch dev"}, active, true, ""},
		// Sin tarea en curso, el agente puede trabajar fuera del flujo.
		{Action{Tool: Bash, Command: "gh pr create --fill"}, ctx(), true, ""},
		{Action{Tool: Bash, Command: "git switch -c chore/x"}, ctx(), true, ""},
		// Siempre: estado de bflow y re-congelar pruebas.
		{Action{Tool: Bash, Command: "bflow freeze API-1"}, ctx(), false, "human_only"},
		{Action{Tool: Bash, Command: "bflow status --json"}, ctx(), true, ""},
		{Action{Tool: Edit, Path: ".bflow/tasks/API-1/state.json"}, ctx(), false, "bflow_state"},
		{Action{Tool: Write, Path: ".bflowrc"}, ctx(), true, ""},
	}
	for _, tc := range cases {
		d := Evaluate(tc.a, tc.c)
		if d.Allow != tc.allow || d.Rule != tc.rule {
			t.Errorf("%+v (fase %q) → allow=%v rule=%q, want allow=%v rule=%q", tc.a, tc.c.Phase, d.Allow, d.Rule, tc.allow, tc.rule)
		}
	}
	for _, cmd := range []string{"gh pr create", "cd x && git switch -c y", "git branch z"} {
		if !TaskScoped(cmd) {
			t.Errorf("TaskScoped(%q) = false", cmd)
		}
	}
	if TaskScoped("git branch -a") || TaskScoped("go test ./...") {
		t.Error("TaskScoped no debe pedir la tarea para comandos ajenos")
	}
}

func TestDiffSize(t *testing.T) {
	c := ctx()
	c.MaxDiffLines = 400
	c.DiffLines = func() (int, error) { return 650, nil }
	d := Evaluate(Action{Tool: Bash, Command: `git commit -m "feat: grande"`}, c)
	if d.Allow || d.Rule != "diff_size" || !strings.Contains(d.Reason, "650") || !strings.Contains(d.Reason, "400") {
		t.Errorf("diff grande: %+v", d)
	}
	c.DiffLines = func() (int, error) { return 120, nil }
	if d := Evaluate(Action{Tool: Bash, Command: `git commit -m "x"`}, c); !d.Allow {
		t.Errorf("diff chico: %+v", d)
	}
	called := false
	c.DiffLines = func() (int, error) { called = true; return 0, nil }
	Evaluate(Action{Tool: Bash, Command: "go test ./..."}, c)
	if called {
		t.Error("el tamaño del diff solo se calcula al commitear (el guard corre en cada herramienta)")
	}
}

func TestPathNormalization(t *testing.T) {
	c := ctx()
	c.Root = `C:\repo`
	c.Phase = flow.Implementing
	c.Frozen = []string{"internal/a_test.go"}
	d := Evaluate(Action{Tool: Edit, Path: `C:\repo\internal\a_test.go`}, c)
	if d.Allow {
		t.Error("rutas absolutas de Windows deben normalizarse a relativas")
	}
}
