package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/tracker/trackertest"
	"innobytes.tech/bflow/internal/vcs"
)

type fakeGit struct {
	branch string
	dirty  []string
	calls  []string
	diff   []string // DiffNames
	lines  int      // DiffLines
}

func (g *fakeGit) CurrentBranch(context.Context) (string, error) { return g.branch, nil }
func (g *fakeGit) EnsureBranch(_ context.Context, name, base string) (string, error) {
	g.calls = append(g.calls, "branch "+name+" from "+base)
	g.branch = name
	return "created", nil
}
func (g *fakeGit) Dirty(context.Context, []string) ([]string, error)   { return g.dirty, nil }
func (g *fakeGit) HeadSHA(context.Context) (string, error)             { return "abc", nil }
func (g *fakeGit) DiffNames(context.Context, string) ([]string, error) { return g.diff, nil }
func (g *fakeGit) DiffLines(context.Context, string) (int, error)      { return g.lines, nil }
func (g *fakeGit) ChangedSince(context.Context, string, []string) ([]string, error) {
	return nil, nil
}
func (g *fakeGit) Commit(_ context.Context, paths []string, msg string) (bool, error) {
	g.calls = append(g.calls, "commit "+msg+" "+strings.Join(paths, ","))
	return true, nil
}
func (g *fakeGit) Push(_ context.Context, b string) error {
	g.calls = append(g.calls, "push "+b)
	return nil
}
func (g *fakeGit) RemoteURL(context.Context) (string, error) {
	return "https://github.com/o/r.git", nil
}

type fakeHost struct {
	prs        map[string]vcs.PR // por rama
	bodies     map[int]string
	deny       error // si no es nil, OpenPR y FindPR fallan con él
	opened     int
	findMerged int
}

func newHost() *fakeHost { return &fakeHost{prs: map[string]vcs.PR{}, bodies: map[int]string{}} }

func (h *fakeHost) Name() string { return "fake" }
func (h *fakeHost) OpenPR(_ context.Context, s vcs.PRSpec) (vcs.PR, error) {
	if h.deny != nil {
		return vcs.PR{}, h.deny
	}
	h.opened++
	pr := vcs.PR{Number: 100 + h.opened, URL: "https://host/pr/" + s.Head, State: "open"}
	h.prs[s.Head] = pr
	h.bodies[pr.Number] = s.Body
	return pr, nil
}
func (h *fakeHost) FindPR(_ context.Context, head string) (vcs.PR, error) {
	if h.deny != nil {
		return vcs.PR{}, h.deny
	}
	if pr, ok := h.prs[head]; ok && pr.State == "open" {
		return pr, nil
	}
	return vcs.PR{}, vcs.ErrNoPR
}
func (h *fakeHost) FindMergedPR(_ context.Context, head string) (vcs.PR, error) {
	h.findMerged++
	if h.deny != nil {
		return vcs.PR{}, h.deny
	}
	if pr, ok := h.prs[head]; ok && pr.Merged {
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
	m(v.e.Report(ctx, id, ReportOpts{Agent: "documenter", Verdict: flow.DoneV}))
	m(v.e.Approve(ctx, id, ApproveOpts{Gate: "questions"})) // saltar las preguntas de producto
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
	// La spec entra a la rama nueva: si nadie la commitea, no llega al PR.
	want := "branch feature/" + id + "-demo from dev;commit docs: " + id + " spec specs/" + id + "-demo/spec.md"
	if o.To != flow.Implementing || strings.Join(g.calls, ";") != want {
		t.Errorf("rama y commit de la spec: %v %+v", g.calls, o)
	}
	// La tarea activa sale de la rama actual aunque haya otras en curso.
	other := v.task(t, "Otra")
	mustT(t)(v.e.Start(ctx, other, flow.Full, "", ""))
	if a, err := v.e.Active(ctx); err != nil || a != id {
		t.Errorf("activa por rama: %q %v", a, err)
	}
}

func TestPRBodyReadsVersionedChangelog(t *testing.T) {
	v := newEnv(t, "flow: { consumer_changelog: \"docs/consumers/{id}.md\" }\n")
	id := "T-9"
	rec := store.Record{Flow: flow.State{ID: id, Slug: "demo", Lane: flow.Full}, Title: "Demo"}
	_ = v.e.Store.WriteFile(id, "consumer-changelog.md", []byte("de una tarea vieja"))
	if body := v.e.PRBody(rec); !strings.Contains(body, "de una tarea vieja") {
		t.Errorf("sin changelog versionado, usa el de .bflow/ de las tareas empezadas antes:\n%s", body)
	}
	p := filepath.Join(v.e.Cfg.Root, "docs", "consumers", id+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# Cambios\n- nuevo error 403"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := v.e.PRBody(rec)
	if !strings.Contains(body, "## Contrato para consumidores") || !strings.Contains(body, "### Cambios") || strings.Contains(body, "de una tarea vieja") {
		t.Errorf("el PR lleva el changelog versionado:\n%s", body)
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

func TestPRDegradesWithoutAccess(t *testing.T) {
	for name, deny := range map[string]error{"sin token": vcs.ErrNoCredentials, "token sin acceso al repo": fmt.Errorf("%w o/r", vcs.ErrNoAccess)} {
		t.Run(name, func(t *testing.T) {
			v := newEnv(t, "vcs: { base_branch: dev }\n")
			h := newHost()
			h.deny = deny
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
		})
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

// linkingTracker es la memoria con la capacidad opcional tracker.PRLinker.
type linkingTracker struct {
	*trackertest.Memory
	ref string
}

func (l linkingTracker) CloseRef(string) string { return l.ref }

func TestPRBodyClosesIssue(t *testing.T) {
	v := newEnv(t, "")
	rec := store.Record{Flow: flow.State{ID: "GH-42", Slug: "demo", Lane: flow.Full}, Title: "Demo"}

	v.e.Tracker = linkingTracker{v.tr, "Closes #42"}
	body := v.e.PRBody(rec)
	at, foot := strings.Index(body, "Closes #42"), strings.LastIndex(body, "\n---\n")
	if at < 0 || foot < 0 || at > foot {
		t.Errorf("el PR lleva «Closes #42» antes del pie:\n%s", body)
	}
	if strings.Count(body, "Closes #42") != 1 {
		t.Errorf("la línea aparece una sola vez:\n%s", body)
	}

	v.e.Tracker = linkingTracker{v.tr, ""}
	if body := v.e.PRBody(rec); strings.Contains(body, "Closes") {
		t.Errorf("CloseRef vacío no agrega nada:\n%s", body)
	}

	v.e.Tracker = v.tr // sin la capacidad
	if body := v.e.PRBody(rec); strings.Contains(body, "Closes") {
		t.Errorf("un tracker sin PRLinker no agrega nada:\n%s", body)
	}
}

// toDecision lleva una tarea light a implementing con un gate decision abierto
// (el caso GH-13) y con rama registrada.
func toDecision(t *testing.T, v *env, id string) {
	t.Helper()
	ctx := context.Background()
	m := mustT(t)
	m(v.e.Start(ctx, id, flow.Light, "", ""))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
	m(v.e.Approve(ctx, id, ApproveOpts{}))
	m(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.NeedsDecision, Note: "n", Options: []string{"A", "B"}}))
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.Phase != flow.Implementing || rec.Flow.Gate == nil || rec.Branch == "" {
		t.Fatalf("preparación: %+v", rec)
	}
}

func lastEntry(t *testing.T, v *env, id string) store.Entry {
	t.Helper()
	es, err := v.e.Store.Log(id)
	if err != nil || len(es) == 0 {
		t.Fatalf("log: %v", err)
	}
	return es[len(es)-1]
}

func TestPanelClosesTaskDoneInTracker(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	h := newHost()
	v.e.Host = h
	ctx := context.Background()
	id := v.task(t, "Traspaso")
	toDecision(t, v, id)
	v.tr.Calls = nil
	_ = v.tr.Transition(ctx, id, flow.Done, tracker.Patch{})
	v.tr.Calls = nil

	rep, err := v.e.Panel(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ClosedOutside) != 1 || rep.ClosedOutside[0] != (ClosedOutside{ID: id, From: flow.Implementing, Reason: "tracker"}) {
		t.Fatalf("closed_outside: %+v", rep.ClosedOutside)
	}
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.Phase != flow.Done || rec.Flow.Gate != nil {
		t.Errorf("local: %+v", rec.Flow)
	}
	if len(v.tr.Calls) != 0 {
		t.Errorf("no se escribe en el tracker: %v", v.tr.Calls)
	}
	en := lastEntry(t, v, id)
	if en.Event != "closed_outside" || en.From != flow.Implementing || en.To != flow.Done || en.Data["reason"] != "tracker" {
		t.Errorf("log: %+v", en)
	}
	if h.findMerged != 0 {
		t.Error("con el tracker en done no se consulta el PR")
	}
	if len(rep.Items) != 0 {
		t.Errorf("ya no es compromiso: %+v", rep.Items)
	}
}

func TestPanelClosesTaskWithMergedPRInAnyPhase(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	h := newHost()
	v.e.Host = h
	ctx := context.Background()
	byBranch, byNumber := v.task(t, "Por rama"), v.task(t, "Por numero")
	toDecision(t, v, byBranch)
	toDecision(t, v, byNumber)
	rb, _ := v.e.Store.Load(byBranch)
	rn, _ := v.e.Store.Load(byNumber)
	h.prs[rb.Branch] = vcs.PR{Number: 30, URL: "https://host/pull/30", State: "merged", Merged: true}
	h.prs[rn.Branch] = vcs.PR{Number: 31, URL: "https://host/pull/31", State: "merged", Merged: true}
	_, _ = v.e.Store.Update(byNumber, func(r *store.Record, _ bool) error { r.PR = &store.PR{Number: 31}; return nil })
	h.prs["otra"] = vcs.PR{Number: 99, State: "open"}

	rep, err := v.e.Panel(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ClosedOutside) != 2 || len(rep.Closed) != 0 {
		t.Fatalf("cierres: %+v %+v", rep.ClosedOutside, rep.Closed)
	}
	for id, n := range map[string]int{byBranch: 30, byNumber: 31} {
		rec, _ := v.e.Store.Load(id)
		if rec.Flow.Phase != flow.Done || rec.PR == nil || rec.PR.Number != n || !rec.PR.Merged {
			t.Errorf("%s: %+v %+v", id, rec.Flow, rec.PR)
		}
		if task, _ := v.tr.Get(ctx, id); task.Phase != flow.Done {
			t.Errorf("%s: el tracker debe pasar a done: %s", id, task.Phase)
		}
		if en := lastEntry(t, v, id); en.Event != "closed_outside" || en.Data["reason"] != "pr" || fmt.Sprint(en.Data["pr"]) != fmt.Sprint(n) {
			t.Errorf("%s log: %+v", id, en)
		}
	}
}

func TestPanelDropsPendingWhenTrackerDone(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	ctx := context.Background()
	id := v.task(t, "Pendientes")
	v.tr.FailTransitions = true
	toDecision(t, v, id)
	if rec, _ := v.e.Store.Load(id); len(rec.Pending) == 0 {
		t.Fatal("debían quedar efectos pendientes")
	}
	v.tr.FailTransitions = false
	_ = v.tr.Transition(ctx, id, flow.Done, tracker.Patch{})
	v.tr.Calls = nil

	rep, err := v.e.Panel(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := v.e.Store.Load(id)
	if len(rep.ClosedOutside) != 1 || rec.Flow.Phase != flow.Done || len(rec.Pending) != 0 {
		t.Fatalf("%+v %+v", rep.ClosedOutside, rec)
	}
	if len(v.tr.Calls) != 0 {
		t.Errorf("no debe reintentar ni comentar: %v", v.tr.Calls)
	}
	if task, _ := v.tr.Get(ctx, id); task.Phase != flow.Done {
		t.Errorf("el tracker no regresa: %s", task.Phase)
	}
}

func TestPanelKeepsTaskWhenPRClosedUnmerged(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	h := newHost()
	v.e.Host = h
	ctx := context.Background()
	id := v.task(t, "Cerrado sin merge")
	toDecision(t, v, id)
	rec, _ := v.e.Store.Load(id)
	_, _ = v.e.Store.Update(id, func(r *store.Record, _ bool) error { r.PR = &store.PR{Number: 40}; return nil })
	h.prs[rec.Branch] = vcs.PR{Number: 40, State: "closed"}

	rep, err := v.e.Panel(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ClosedOutside) != 0 || phaseOf(t, v, id) != flow.Implementing {
		t.Errorf("no debe cerrar: %+v", rep.ClosedOutside)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "sin merge") {
		t.Errorf("aviso: %v", rep.Warnings)
	}
	// Un host que falla tampoco cierra: solo avisa.
	_, _ = v.e.Store.Update(id, func(r *store.Record, _ bool) error { r.PR = nil; return nil })
	h.deny = errors.New("boom")
	rep, _ = v.e.Panel(ctx, false)
	if len(rep.ClosedOutside) != 0 || phaseOf(t, v, id) != flow.Implementing || len(rep.Warnings) == 0 {
		t.Errorf("host caído: %+v %v", rep.ClosedOutside, rep.Warnings)
	}
}

// GH-49: el issue se cierra por el merge del PR pero conserva la fase in_review;
// la tarea debe terminar en done también en el tracker.
func TestPanelMergedPRWithIssueClosedByMergeMovesTracker(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	h := newHost()
	v.e.Git, v.e.Host = &fakeGit{branch: "dev"}, h
	ctx := context.Background()
	id := v.task(t, "Cerrada por merge")
	toWalkthrough(t, v, id)
	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	h.merge("hotfix/" + id + "-cerrada-por-merge")
	v.tr.CloseKeepingPhase(id)
	v.tr.Calls = nil

	rep, err := v.e.Panel(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rep.Closed, ",") != id || len(rep.ClosedOutside) != 0 {
		t.Fatalf("debe cerrar por merge: %+v %+v", rep.Closed, rep.ClosedOutside)
	}
	task, _ := v.tr.Get(ctx, id)
	if task.Phase != flow.Done {
		t.Errorf("el tracker debe quedar en done: %s", task.Phase)
	}
	if phaseOf(t, v, id) != flow.Done {
		t.Error("local en done")
	}
}

func TestPanelIssueClosedWithoutMergeKeepsClosedOutside(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	v.e.Git, v.e.Host = &fakeGit{branch: "dev"}, newHost()
	ctx := context.Background()
	id := v.task(t, "Cerrada a mano")
	toWalkthrough(t, v, id)
	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	v.tr.CloseKeepingPhase(id)
	v.tr.Calls = nil

	rep, err := v.e.Panel(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Closed) != 0 || len(rep.ClosedOutside) != 1 || rep.ClosedOutside[0].Reason != "tracker" {
		t.Fatalf("sin merge sigue el cierre por tracker: %+v %+v", rep.Closed, rep.ClosedOutside)
	}
	if len(v.tr.Calls) != 0 {
		t.Errorf("no se escribe en el tracker: %v", v.tr.Calls)
	}
}
