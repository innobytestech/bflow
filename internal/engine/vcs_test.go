package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/vcs"
)

type fakeGit struct {
	branch string
	dirty  []string
	calls  []string
}

func (g *fakeGit) CurrentBranch(context.Context) (string, error) { return g.branch, nil }
func (g *fakeGit) EnsureBranch(_ context.Context, name, base string) (string, error) {
	g.calls = append(g.calls, "branch "+name+" from "+base)
	g.branch = name
	return "created", nil
}
func (g *fakeGit) Dirty(context.Context, []string) ([]string, error)   { return g.dirty, nil }
func (g *fakeGit) HeadSHA(context.Context) (string, error)             { return "abc", nil }
func (g *fakeGit) DiffNames(context.Context, string) ([]string, error) { return nil, nil }
func (g *fakeGit) ChangedSince(context.Context, string, []string) ([]string, error) {
	return nil, nil
}
func (g *fakeGit) Push(_ context.Context, b string) error {
	g.calls = append(g.calls, "push "+b)
	return nil
}
func (g *fakeGit) RemoteURL(context.Context) (string, error) {
	return "https://github.com/o/r.git", nil
}

type fakeHost struct {
	prs     map[string]vcs.PR // por rama
	bodies  map[int]string
	noCreds bool
	opened  int
}

func newHost() *fakeHost { return &fakeHost{prs: map[string]vcs.PR{}, bodies: map[int]string{}} }

func (h *fakeHost) Name() string { return "fake" }
func (h *fakeHost) OpenPR(_ context.Context, s vcs.PRSpec) (vcs.PR, error) {
	if h.noCreds {
		return vcs.PR{}, vcs.ErrNoCredentials
	}
	h.opened++
	pr := vcs.PR{Number: 100 + h.opened, URL: "https://host/pr/" + s.Head, State: "open"}
	h.prs[s.Head] = pr
	h.bodies[pr.Number] = s.Body
	return pr, nil
}
func (h *fakeHost) FindPR(_ context.Context, head string) (vcs.PR, error) {
	if h.noCreds {
		return vcs.PR{}, vcs.ErrNoCredentials
	}
	if pr, ok := h.prs[head]; ok && pr.State == "open" {
		return pr, nil
	}
	return vcs.PR{}, vcs.ErrNoPR
}
func (h *fakeHost) UpdatePRBody(_ context.Context, n int, body string) error {
	h.bodies[n] = body
	return nil
}
func (h *fakeHost) PRStatus(_ context.Context, n int) (vcs.PR, error) {
	for _, pr := range h.prs {
		if pr.Number == n {
			return pr, nil
		}
	}
	return vcs.PR{}, errors.New("no existe")
}
func (h *fakeHost) CompareURL(base, head string) string {
	return "https://host/compare/" + base + "..." + head
}

func (h *fakeHost) merge(branch string) {
	pr := h.prs[branch]
	pr.State, pr.Merged = "merged", true
	h.prs[branch] = pr
}

// toWalkthrough lleva una tarea hotfix hasta el gate walkthrough.
func toWalkthrough(t *testing.T, v *env, id string) {
	t.Helper()
	ctx := context.Background()
	m := mustT(t)
	m(v.e.Start(ctx, id, flow.Hotfix, "", ""))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV}))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "reviewer", Verdict: flow.Approved}))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "security-auditor", Verdict: flow.Approved}))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "documenter", Verdict: flow.DoneV}))
}

func TestBranchCreatedOnSpecApproval(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	g := &fakeGit{branch: "dev"}
	v.e.Git = g
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Light, "", ""))
	if len(g.calls) != 0 {
		t.Fatalf("no se crea rama antes de aprobar el spec: %v", g.calls)
	}
	mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
	g.dirty = []string{"internal/x.go"}
	_, err := v.e.Approve(ctx, id, ApproveOpts{})
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "dirty_worktree" || !strings.Contains(rj.Reason, "internal/x.go") {
		t.Fatalf("con cambios sin commitear no se crea la rama: %v", err)
	}
	if phaseOf(t, v, id) != flow.Spec {
		t.Fatal("la transición no debe guardarse si falla la rama")
	}
	g.dirty = nil
	o := mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	if o.To != flow.Implementing || strings.Join(g.calls, ";") != "branch feature/"+id+"-demo from dev" {
		t.Errorf("rama: %v %+v", g.calls, o)
	}
	// La tarea activa sale de la rama actual aunque haya otras en curso.
	other := v.task(t, "Otra")
	mustT(t)(v.e.Start(ctx, other, flow.Full, "", ""))
	if a, err := v.e.Active(ctx); err != nil || a != id {
		t.Errorf("activa por rama: %q %v", a, err)
	}
}

func TestPROpenedOnWalkthroughAndIdempotent(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	g, h := &fakeGit{branch: "dev"}, newHost()
	v.e.Git, v.e.Host = g, h
	ctx := context.Background()
	id := v.task(t, "Demo")
	toWalkthrough(t, v, id)
	_ = v.e.Store.WriteFile(id, "reports/review-map.md", []byte("# Review-map\n🔴 internal/x.go: CAS en transición"))
	o := mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	if o.PR == nil || o.PR.Number != 101 || o.To != flow.InReview {
		t.Fatalf("PR: %+v", o)
	}
	body := h.bodies[101]
	if !strings.Contains(body, "## Review-map") || !strings.Contains(body, "### Review-map") || !strings.Contains(body, "CAS en transición") {
		t.Errorf("el review-map debe ir al cuerpo del PR (con encabezados bajados):\n%s", body)
	}
	if !strings.Contains(strings.Join(g.calls, ";"), "push hotfix/"+id+"-demo") {
		t.Errorf("push: %v", g.calls)
	}
	// bflow pr en in_review actualiza, no abre otro.
	_ = v.e.Store.WriteFile(id, "decisions.md", []byte("- decidimos X"))
	o, err := v.e.PR(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if h.opened != 1 || !strings.Contains(h.bodies[101], "decidimos X") {
		t.Errorf("pr debe actualizar el mismo PR: abiertos=%d", h.opened)
	}
	if _, err := v.e.PR(ctx, v.task(t, "sin empezar")); err == nil {
		t.Error("pr en una tarea que no está en in_review debe fallar")
	}
}

func TestPRDegradesWithoutCredentials(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	h := newHost()
	h.noCreds = true
	v.e.Git, v.e.Host = &fakeGit{branch: "dev"}, h
	id := v.task(t, "Demo")
	toWalkthrough(t, v, id)
	o := mustT(t)(v.e.Approve(context.Background(), id, ApproveOpts{}))
	if o.To != flow.InReview || o.PR == nil || !strings.Contains(o.PR.URL, "compare/dev...hotfix/") {
		t.Fatalf("degradado: %+v", o)
	}
	if !hasWarning(o, "pr-body.md") {
		t.Errorf("debe decir dónde quedó la descripción: %v", o.Warnings)
	}
	if b, err := v.e.Store.ReadFile(id, "pr-body.md"); err != nil || len(b) == 0 {
		t.Error("pr-body.md debe existir")
	}
}

func TestPanelClosesMergedAndRemindsSLA(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	g, h := &fakeGit{branch: "dev"}, newHost()
	v.e.Git, v.e.Host = g, h
	ctx := context.Background()

	merged := v.task(t, "Ya mergeada")
	toWalkthrough(t, v, merged)
	mustT(t)(v.e.Approve(ctx, merged, ApproveOpts{}))
	h.merge("hotfix/" + merged + "-ya-mergeada")

	waiting := v.task(t, "Esperando spec")
	mustT(t)(v.e.Start(ctx, waiting, flow.Light, "", ""))
	mustT(t)(v.e.Report(ctx, waiting, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))

	v.tick(30 * time.Hour)
	rep, err := v.e.Panel(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rep.Closed, ",") != merged || phaseOf(t, v, merged) != flow.Done {
		t.Errorf("merge → done: %+v", rep.Closed)
	}
	task, _ := v.tr.Get(ctx, merged)
	if task.Phase != flow.Done {
		t.Errorf("el tracker debe quedar en done: %s", task.Phase)
	}
	if len(rep.Items) != 1 || rep.Items[0].ID != waiting || !rep.Items[0].SLA {
		t.Fatalf("items: %+v", rep.Items)
	}
	cs, _ := v.tr.Comments(ctx, waiting)
	if len(cs) != 1 || !strings.Contains(cs[0].Body, "Esperando") || !strings.Contains(cs[0].Body, "spec") {
		t.Fatalf("recordatorio SLA: %+v", cs)
	}
	// Idempotente: no se repite el mismo recordatorio.
	if _, err := v.e.Panel(ctx, true); err != nil {
		t.Fatal(err)
	}
	cs, _ = v.tr.Comments(ctx, waiting)
	if len(cs) != 1 {
		t.Errorf("recordatorio duplicado: %d", len(cs))
	}
	// Sin --sla no escribe comentarios.
	v.tick(48 * time.Hour)
	mustT(t)(v.e.Reject(ctx, waiting, RejectOpts{Note: "cambia"}))
	mustT(t)(v.e.Report(ctx, waiting, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
	v.tick(30 * time.Hour)
	before, _ := v.tr.Comments(ctx, waiting)
	if _, err := v.e.Panel(ctx, false); err != nil {
		t.Fatal(err)
	}
	after, _ := v.tr.Comments(ctx, waiting)
	if len(after) != len(before) {
		t.Error("panel sin --sla no comenta")
	}
}

func TestContractApprovalFreezesTouchedTests(t *testing.T) {
	v := newEnv(t, "stack: go\nvcs: { base_branch: dev }\n")
	g := &fakeGit{branch: "dev"}
	v.e.Git = g
	ctx := context.Background()
	id := v.task(t, "Demo")
	m := mustT(t)
	m(v.e.Start(ctx, id, flow.Full, "", ""))
	m(v.e.Approve(ctx, id, ApproveOpts{Attachment: "d"}))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
	m(v.e.Approve(ctx, id, ApproveOpts{}))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.ContractReady}))
	g.dirty = []string{"internal/cas_test.go", "internal/cas.go", "docs/x.md"}
	o := m(v.e.Approve(ctx, id, ApproveOpts{}))
	if got := v.e.Frozen(id); len(got) != 1 || got[0] != "internal/cas_test.go" {
		t.Errorf("congeladas: %v", got)
	}
	if !hasWarning(o, "1 prueba(s) congelada(s)") {
		t.Errorf("debe avisar: %v", o.Warnings)
	}
}
