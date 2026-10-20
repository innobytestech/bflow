package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// fakeChecks es el historial de check: sha → paso → estado. Un sha presente
// con mapa vacío es un check guardado que no trae pasos.
type fakeChecks struct{ runs map[string]map[string]string }

func (f *fakeChecks) StepAt(_ context.Context, _, sha, step string) (bool, string, error) {
	steps, ok := f.runs[sha]
	return ok, steps[step], nil
}

const (
	shaHead  = "feedface00000000000000000000000000000001"
	shaTest  = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaTest2 = "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	shaFix   = "3333333ccccccccccccccccccccccccccccccc"
)

// reproEnv arma un hotfix en implementing con historial de check y git falsos.
func reproEnv(t *testing.T) (*env, *fakeGit, *fakeChecks, string) {
	t.Helper()
	v := newEnv(t, "stack: go\ncheck:\n  steps:\n    - { name: vet, run: \"go vet ./...\" }\n    - { name: test, run: \"go test ./...\" }\n")
	g := &fakeGit{branch: "main", head: "start111", files: map[string][]string{}}
	c := &fakeChecks{runs: map[string]map[string]string{}}
	v.e.Git, v.e.Checks = g, c
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(context.Background(), id, flow.Hotfix, "", ""))
	g.head = shaHead
	return v, g, c, id
}

func doneHotfix(v *env, id string) (Outcome, error) {
	return v.e.Report(context.Background(), id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV})
}

func rejectionOf(t *testing.T, err error) *flow.Rejection {
	t.Helper()
	var rj *flow.Rejection
	if !errors.As(err, &rj) {
		t.Fatalf("esperaba una Rejection, got %v", err)
	}
	return rj
}

func TestReproMissing(t *testing.T) {
	fail := map[string]string{"vet": "pass", "test": "fail"}
	cases := []struct {
		name    string
		setup   func(g *fakeGit, c *fakeChecks, v *env, id string)
		accept  bool
		reason  []string // fragmentos del reason cuando se rechaza
		baseArg string   // base con que debe pedirse el rango
	}{
		{name: "sin commits en el rango",
			setup:  func(g *fakeGit, c *fakeChecks, v *env, id string) { g.commits = nil },
			reason: []string{"no hay commit de solo pruebas"}},
		{name: "solo código",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a.go"}
				c.runs[shaTest] = fail
			},
			reason: []string{"no hay commit de solo pruebas"}},
		{name: "prueba mezclada con código",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go", "internal/a.go"}
				c.runs[shaTest] = fail
			},
			reason: []string{"no hay commit de solo pruebas"}},
		{name: "prueba sin check",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaTest2, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go"}
				g.files[shaTest2] = []string{"internal/b_test.go"}
			},
			reason: []string{"1111111", "2222222", "bflow check TASK"}},
		{name: "check que pasó",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go"}
				c.runs[shaTest] = map[string]string{"vet": "pass", "test": "pass"}
			},
			reason: []string{"test", "no falló"}},
		{name: "falló otro paso y no el de pruebas",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go"}
				c.runs[shaTest] = map[string]string{"vet": "fail", "test": "pass"}
			},
			reason: []string{"no falló"}},
		{name: "test_step inexistente",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				v.e.Cfg.Check.TestStep = "unit"
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go"}
				c.runs[shaTest] = fail
			},
			reason: []string{"unit", "check.steps"}},
		{name: "HEAD de solo pruebas no cuenta",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaHead}
				g.files[shaHead] = []string{"internal/a_test.go"}
				c.runs[shaHead] = fail
			},
			reason: []string{"no hay commit de solo pruebas"}},
		{name: "commit vacío no cuenta",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = nil
				c.runs[shaTest] = fail
			},
			reason: []string{"no hay commit de solo pruebas"}},
		{name: "error de git sale como rechazo",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commitsErr = errors.New("rev-list explotó")
			},
			reason: []string{"rev-list explotó"}},
		{name: "base desde StartSHA sin base_branch", baseArg: "start111", accept: true,
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				v.e.Cfg.VCS.BaseBranch = ""
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go"}
				c.runs[shaTest] = fail
			}},
		{name: "commit que solo borra pruebas no cuenta",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go"}
				g.kept2 = map[string][]string{shaTest: nil}
				c.runs[shaTest] = fail
			},
			reason: []string{"no hay commit de solo pruebas"}},
		{name: "sin base ni StartSHA",
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				v.e.Cfg.VCS.BaseBranch = ""
				if _, err := v.e.Store.Update(id, func(r *store.Record, _ bool) error { r.StartSHA = ""; return nil }); err != nil {
					t.Fatal(err)
				}
				g.commits = []string{shaTest, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go"}
				c.runs[shaTest] = fail
			},
			reason: []string{"vcs.base_branch"}},
		{name: "evidencia válida con base_branch", baseArg: "origin/main", accept: true,
			setup: func(g *fakeGit, c *fakeChecks, v *env, id string) {
				g.commits = []string{shaTest, shaFix, shaHead}
				g.files[shaTest] = []string{"internal/a_test.go", "docs/x_test.go"}
				g.files[shaFix] = []string{"internal/a.go"}
				c.runs[shaTest] = fail
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, g, c, id := reproEnv(t)
			v.e.Cfg.Guard.TestPatterns = []string{"*_test.go"}
			tc.setup(g, c, v, id)
			out, err := doneHotfix(v, id)
			if tc.accept {
				if err != nil || out.To != flow.Quality {
					t.Fatalf("con evidencia el DONE avanza: %+v %v", out, err)
				}
				if g.commitsBase != tc.baseArg {
					t.Errorf("base del rango %q, want %q", g.commitsBase, tc.baseArg)
				}
				return
			}
			rj := rejectionOf(t, err)
			if rj.Code != "repro_test_missing" {
				t.Fatalf("código %q (%s), want repro_test_missing", rj.Code, rj.Reason)
			}
			for _, w := range tc.reason {
				if !strings.Contains(rj.Reason, strings.ReplaceAll(w, "TASK", id)) {
					t.Errorf("el reason debe decir %q:\n%s", w, rj.Reason)
				}
			}
			if phaseOf(t, v, id) != flow.Implementing {
				t.Error("el rechazo no cambia de fase")
			}
		})
	}
}

func TestReproSkippedAfterApproval(t *testing.T) {
	v, g, _, id := reproEnv(t)
	ctx := context.Background()
	m := mustT(t)
	g.commits = []string{shaHead}

	if _, err := v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV, NoRepro: ptr("  ")}); rejectionOf(t, err).Code != "no_repro_invalid" {
		t.Fatalf("--no-repro en blanco: %v", err)
	}
	o := m(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV, NoRepro: ptr("defecto de configuración")}))
	if o.Phase != flow.Implementing || o.Gate != "repro" {
		t.Fatalf("--no-repro abre el gate sin avanzar: %+v", o)
	}
	m(v.e.Approve(ctx, id, ApproveOpts{Gate: "repro"}))
	if phaseOf(t, v, id) != flow.Quality {
		t.Fatal("aprobar el gate repro avanza a quality")
	}
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.NoRepro != "defecto de configuración" {
		t.Errorf("el motivo queda en el estado: %q", rec.Flow.NoRepro)
	}

	m(v.e.Report(ctx, id, ReportOpts{Agent: "reviewer", Verdict: flow.RejectedV, Note: "falta X"}))
	if phaseOf(t, v, id) != flow.Implementing {
		t.Fatal("la calidad rechazó: vuelve a implementing")
	}
	g.head = "ddddddd0000000000000000000000000000000000"
	o = m(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV}))
	if o.To != flow.Quality {
		t.Errorf("con el repro aprobado la ronda de corrección no pide evidencia: %+v", o)
	}
}

func TestReproOnlyHotfix(t *testing.T) {
	v := newEnv(t, "stack: go\ncheck:\n  steps:\n    - { name: test, run: \"go test ./...\" }\n")
	v.e.Git = &fakeGit{branch: "main", head: shaHead, files: map[string][]string{}}
	v.e.Checks = &fakeChecks{runs: map[string]map[string]string{}}
	ctx := context.Background()
	id := v.task(t, "Demo")
	m := mustT(t)
	m(v.e.Start(ctx, id, flow.Light, "", ""))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
	m(v.e.Approve(ctx, id, ApproveOpts{}))

	if _, err := v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV, NoRepro: ptr("x")}); rejectionOf(t, err).Code != "no_repro_invalid" {
		t.Fatalf("--no-repro fuera de hotfix: %v", err)
	}
	o := m(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV}))
	if o.To != flow.Quality {
		t.Errorf("en light el DONE no pide prueba de reproducción: %+v", o)
	}
}

func ptr(s string) *string { return &s }

func TestStartResumedBranchUsesMergeBase(t *testing.T) {
	for _, tc := range []struct {
		name, how, want string
		mbErr           error
	}{
		{"rama que ya existía", "resumed", "merge111", nil},
		{"rama nueva", "created", "start111", nil},
		{"sin merge-base", "resumed", "start111", errors.New("sin principal")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newEnv(t, "stack: go\n")
			g := &fakeGit{branch: "main", head: "start111", how: tc.how, mergeBase: "merge111", mergeBaseErr: tc.mbErr}
			v.e.Git = g
			id := v.task(t, "Demo")
			mustT(t)(v.e.Start(context.Background(), id, flow.Hotfix, "", ""))
			rec, err := v.e.Store.Load(id)
			if err != nil {
				t.Fatal(err)
			}
			if rec.StartSHA != tc.want {
				t.Errorf("StartSHA %q, want %q", rec.StartSHA, tc.want)
			}
		})
	}
}
