package guard

import (
	"slices"
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
		{"vulnerabilidades aceptadas: solo una persona", Action{Tool: Edit, Path: "security/vulns-accepted.json", Subagent: true},
			func(c *Context) { c.HumanFiles = []string{"security/vulns-accepted.json"} }, false, "human_file"},
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
		{Action{Tool: Write, Path: ".bflow/tasks/API-1/frozen-tests.json"}, ctx(), false, "bflow_state"},
		{Action{Tool: Write, Path: ".bflow/log.jsonl"}, ctx(), false, "bflow_state"},
		{Action{Tool: Write, Path: ".bflow/tasks/API-1/contract.md"}, ctx(), true, ""},
		{Action{Tool: Write, Path: ".bflow/tasks/API-1/reports/review-map.md"}, ctx(), true, ""},
		{Action{Tool: Write, Path: ".bflow/tasks/API-1/walkthrough.md"}, ctx(), true, ""},
		{Action{Tool: Write, Path: ".bflow/tasks/API-1/reports"}, ctx(), false, "bflow_state"},
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

func TestHumanOnlySubagent(t *testing.T) {
	active := ctx()
	active.Phase = flow.Implementing
	sub := func(cmd string) Action { return Action{Tool: Bash, Command: cmd, Subagent: true, Agent: "implementer"} }
	main := func(cmd string) Action { return Action{Tool: Bash, Command: cmd} }
	for _, name := range []string{"approve", "reject", "unblock", "start", "new"} {
		cmd := "bflow " + name + " GH-1"
		d := Evaluate(sub(cmd), active)
		if d.Allow || d.Rule != "human_only" {
			t.Errorf("subagente %q → allow=%v rule=%q", cmd, d.Allow, d.Rule)
		}
		if !strings.Contains(d.Reason, name) || !strings.Contains(d.Reason, "bflow report") || !strings.Contains(d.Reason, "persona") {
			t.Errorf("motivo de %s: %q", name, d.Reason)
		}
		if d := Evaluate(main(cmd), active); !d.Allow {
			t.Errorf("la sesión principal corre %q: %+v", cmd, d)
		}
	}
	cases := []struct {
		cmd   string
		ctx   Context
		allow bool
	}{
		{"bflow report GH-1 --agent implementer --verdict DONE", active, true},
		{"bflow block GH-1 --note x", active, true},
		{"bflow status", active, true},
		{"bflow show GH-1 spec", active, true},
		{"bflow check GH-1", active, true},
		{"bflow pr GH-1", active, true},
		{"bflow task add titulo", active, true},
		{"bflow status && bflow approve GH-1", active, false},
		{"go test ./... ; ./bin/bflow.exe --json reject GH-1", active, false},
		{"bflow approve --help", active, false},
		{"bflow approve GH-1", ctx(), false}, // sin tarea activa ni fase
		{`env -i X=1 "C:\tools\BFLOW.EXE" unblock GH-1`, active, false},
	}
	for _, tc := range cases {
		d := Evaluate(sub(tc.cmd), tc.ctx)
		if d.Allow != tc.allow || (!tc.allow && d.Rule != "human_only") {
			t.Errorf("subagente %q → allow=%v rule=%q, want allow=%v", tc.cmd, d.Allow, d.Rule, tc.allow)
		}
	}
}

func TestFreezeEveryone(t *testing.T) {
	for _, cmd := range []string{"bflow freeze GH-1", "./bin/bflow.exe freeze GH-1 --allow a_test.go", "X=1 bflow --json freeze GH-1"} {
		for _, subagent := range []bool{true, false} {
			d := Evaluate(Action{Tool: Bash, Command: cmd, Subagent: subagent}, ctx())
			if d.Allow || d.Rule != "human_only" {
				t.Errorf("%q (subagente=%v) → allow=%v rule=%q", cmd, subagent, d.Allow, d.Rule)
			}
		}
	}
}

func TestBflowSubcommand(t *testing.T) {
	cases := []struct{ seg, want string }{
		{"bflow approve GH-1", "approve"},
		{"bflow", ""},
		{"BFLOW.EXE Approve GH-1", "approve"},
		{"./bin/bflow.exe freeze GH-1", "freeze"},
		{`C:\tools\bflow.exe reject GH-1`, "reject"},
		{`"/usr/local/bin/bflow" start GH-1`, "start"},
		{`'bflow' new idea`, "new"},
		{"X=1 bflow approve GH-1", "approve"},
		{"X=1 Y=2 bflow approve GH-1", "approve"},
		{"env -i X=1 bflow unblock GH-1", "unblock"},
		{"go run ./cmd/bflow approve GH-1", "approve"},
		{`go run -race C:\jaad\bflow\cmd\bflow\ approve GH-1`, "approve"},
		{"bflow --json approve GH-1", "approve"},
		{"bflow -json status", "status"},
		{"bflowx approve GH-1", ""},
		{"echo bflow approve", ""},
		{"go run ./cmd/other approve", ""},
		{"go test ./...", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := BflowSubcommand(c.seg); got != c.want {
			t.Errorf("BflowSubcommand(%q) = %q, want %q", c.seg, got, c.want)
		}
	}
}

func TestShellWrapped(t *testing.T) {
	active := ctx()
	active.Phase = flow.Implementing
	sub := func(cmd string) Decision {
		return Evaluate(Action{Tool: Bash, Command: cmd, Subagent: true, Agent: "implementer"}, active)
	}
	if d := sub(`bash -c "bflow approve X"`); d.Allow || d.Rule != "human_only" {
		t.Errorf("subagente bash -c approve → %+v", d)
	}
	if d := Evaluate(Action{Tool: Bash, Command: `bash -c "bflow approve X"`}, active); !d.Allow {
		t.Errorf("sesión principal debe poder: %+v", d)
	}
	for cmd, rule := range map[string]string{
		`sh -c 'git reset --hard'`:                 "git_destructive",
		`bash -lc "git push -f origin x"`:          "force_push",
		`/usr/bin/zsh -c "git push origin main"`:   "protected_branch",
		`bash -c "sh -c 'bflow approve X'"`:        "human_only",
		`bash -c "sh -c 'zsh -c bflow approve X'"`: "human_only",
	} {
		if d := sub(cmd); d.Allow || d.Rule != rule {
			t.Errorf("%q → allow=%v rule=%q, quería %q", cmd, d.Allow, d.Rule, rule)
		}
	}
	if !TaskScoped(`bash -c "gh pr create"`) {
		t.Error("TaskScoped debe ver dentro de bash -c")
	}
	if d := Evaluate(Action{Tool: Bash, Command: `bash -c "gh pr create"`}, active); d.Rule != "bflow_pr" {
		t.Errorf("bflow_pr dentro de bash -c → %+v", d)
	}
}

func TestSegmentsUnwrap(t *testing.T) {
	for _, cmd := range []string{
		`bash -c "x y"`, `/bin/bash -c "x y"`, `C:\Git\bin\bash.exe -c "x y"`, `BASH -c "x y"`,
		`"bash" -c "x y"`, `FOO=1 bash -c "x y"`, `env -i FOO=1 sh -c "x y"`, `bash -ec "x y"`,
		`bash -l -c "x y"`, `bash -o pipefail -c "x y"`, `zsh +O extglob -c 'x y'`, `sh -c x y`,
	} {
		if got := Segments(cmd); len(got) != 1 || got[0] != "x y" {
			t.Errorf("%q → %q", cmd, got)
		}
	}
	for _, cmd := range []string{`bash script.sh`, `bashx -c x`, `echo bash -c x`, `bash -x script.sh`, `bash -o pipefail`} {
		if got := Segments(cmd); len(got) != 1 || got[0] != cmd {
			t.Errorf("%q debía quedar igual, dio %q", cmd, got)
		}
	}
}

func TestShellEmptyTokens(t *testing.T) {
	for _, cmd := range []string{`bash "" -c x`, `bash '' -c "bflow approve X"`, `sh ""`, `bash -c`} {
		_ = segments(cmd, 0) // no debe entrar en pánico
	}
}

// R12: retirar una tarea es decisión de una persona.
func TestHumanOnlyDrop(t *testing.T) {
	active := ctx()
	active.Phase = flow.Implementing
	cmd := `bflow drop GH-1 --note "ya no aplica"`
	d := Evaluate(Action{Tool: Bash, Command: cmd, Subagent: true, Agent: "implementer"}, active)
	if d.Allow || d.Rule != "human_only" {
		t.Errorf("subagente %q → allow=%v rule=%q", cmd, d.Allow, d.Rule)
	}
	if d := Evaluate(Action{Tool: Bash, Command: cmd}, active); !d.Allow {
		t.Errorf("la sesión principal corre %q: %+v", cmd, d)
	}
	if !slices.Contains(HumanOnly, "drop") {
		t.Errorf("HumanOnly: %v", HumanOnly)
	}
}
