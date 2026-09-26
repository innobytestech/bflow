package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/testutil"
	"innobytes.tech/bflow/internal/tracker/trackertest"
)

type fakeVerifier struct {
	ok     bool
	detail string
}

func (f *fakeVerifier) Verify(context.Context, string) (bool, string, error) {
	return f.ok, f.detail, nil
}

type env struct {
	e   *Engine
	tr  *trackertest.Memory
	ver *fakeVerifier
	now time.Time
}

func newEnv(t *testing.T, yaml string) *env {
	t.Helper()
	root := testutil.TempDir(t)
	t.Setenv("BFLOW_CONFIG_HOME", testutil.TempDir(t))
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(root, "bflow.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ev := &env{tr: trackertest.NewMemory(), ver: &fakeVerifier{ok: true}, now: time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)}
	st := store.Open(root)
	st.Now = func() time.Time { return ev.now }
	ev.e = &Engine{Cfg: cfg, Store: st, Tracker: ev.tr, Verifier: ev.ver, Now: func() time.Time { return ev.now }, User: "dev"}
	return ev
}

func (v *env) task(t *testing.T, title string) string {
	t.Helper()
	task, err := v.tr.Create(context.Background(), title, "descripción")
	if err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func (v *env) tick(d time.Duration) { v.now = v.now.Add(d) }

func mustT(t *testing.T) func(Outcome, error) Outcome {
	return func(o Outcome, err error) Outcome {
		t.Helper()
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		return o
	}
}

func phaseOf(t *testing.T, v *env, id string) flow.Phase {
	t.Helper()
	r, err := v.e.Store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return r.Flow.Phase
}

func TestFullLaneEndToEnd(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Crédito: reabrir por rechazo fiscal")

	o := mustT(t)(v.e.Start(ctx, id, flow.Full, ""))
	if o.To != flow.Discovery || o.Next.Gate != "discovery" {
		t.Fatalf("start: %+v", o)
	}
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.Slug != "credito-reabrir-por-rechazo-fiscal" || rec.Title == "" {
		t.Errorf("slug/título: %q %q", rec.Flow.Slug, rec.Title)
	}

	v.tick(time.Hour)
	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Gate: "discovery", Attachment: "# Discovery\n- entra: X\n- no entra: Y"}))
	spec := filepath.Join(v.e.Cfg.Root, "specs", id+"-credito-reabrir-por-rechazo-fiscal", "spec.md")
	b, err := os.ReadFile(spec)
	if err != nil {
		t.Fatalf("el spec debe crearse al entrar a spec: %v", err)
	}
	for _, h := range []string{"## Brief", "## Discovery", "## Requirements", "## Design", "## Tasks"} {
		if !strings.Contains(string(b), h) {
			t.Errorf("plantilla sin %q", h)
		}
	}
	if strings.Contains(string(b), "## UI blueprint") {
		t.Error("sin UI no debe haber sección ui-blueprint")
	}
	cs, _ := v.tr.Comments(ctx, id)
	if len(cs) != 1 || !strings.Contains(cs[0].Body, "entra: X") {
		t.Errorf("el discovery completo debe ir al tracker: %+v", cs)
	}

	o = mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
	if o.Next.Gate != "spec" || o.Next.Show[0] != "bflow show "+id+" brief" {
		t.Fatalf("spec listo: %+v", o.Next)
	}
	o = mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	if o.To != flow.Contract {
		t.Fatalf("aprobar spec: %+v", o)
	}
	rec, _ = v.e.Store.Load(id)
	if rec.Branch != "feature/"+id+"-credito-reabrir-por-rechazo-fiscal" {
		t.Errorf("rama: %q", rec.Branch)
	}
	if !hasWarning(o, "git") {
		t.Errorf("sin git debe avisar que no creó la rama: %v", o.Warnings)
	}

	mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.ContractReady}))
	v.tick(30 * time.Minute)
	o = mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Gate: "contract"}))
	if o.To != flow.Implementing || o.Next.Agents[0].Args["resume"] != "true" {
		t.Fatalf("aprobar contrato: %+v", o.Next)
	}
	task, _ := v.tr.Get(ctx, id)
	if task.Start == nil || !task.Start.Equal(v.now) {
		t.Errorf("start_date debe sellarse al entrar a implementing: %v", task.Start)
	}

	o = mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV, File: ".bflow/tasks/" + id + "/impl.md"}))
	if o.To != flow.Paused {
		t.Fatalf("DONE: %+v", o)
	}
	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	o = mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "reviewer", Verdict: flow.Approved}))
	if o.To != "" || phaseOf(t, v, id) != flow.Quality {
		t.Fatalf("primer revisor no debe mover la fase: %+v", o)
	}
	mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "security-auditor", Verdict: flow.Approved}))
	o = mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "documenter", Verdict: flow.DoneV}))
	if o.Next.Gate != "walkthrough" {
		t.Fatalf("walkthrough: %+v", o.Next)
	}
	o = mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	if o.To != flow.InReview || o.Next.Action != "wait" || !hasWarning(o, "PR") {
		t.Fatalf("aprobar walkthrough sin host: %+v", o)
	}
	o = mustT(t)(v.e.Merged(ctx, id))
	if o.To != flow.Done {
		t.Fatalf("merge: %+v", o)
	}

	task, _ = v.tr.Get(ctx, id)
	if task.Phase != flow.Done {
		t.Errorf("el tracker debe quedar en done: %s", task.Phase)
	}
	log, _ := v.e.Store.Log(id)
	if len(log) < 14 {
		t.Errorf("log con %d entradas", len(log))
	}
	for _, e := range log {
		if e.By == "" || e.TS.IsZero() || e.Event == "" {
			t.Errorf("entrada incompleta: %+v", e)
		}
	}
}

func hasWarning(o Outcome, frag string) bool {
	for _, w := range o.Warnings {
		if strings.Contains(w, frag) {
			return true
		}
	}
	return false
}

func TestHotfixLane(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Migración mal numerada")
	o := mustT(t)(v.e.Start(ctx, id, flow.Hotfix, "migracion-124"))
	rec, _ := v.e.Store.Load(id)
	if o.To != flow.Implementing || rec.Branch != "hotfix/"+id+"-migracion-124" {
		t.Errorf("hotfix: to=%s rama=%s", o.To, rec.Branch)
	}
	if _, err := os.Stat(filepath.Join(v.e.Cfg.Root, "specs")); !os.IsNotExist(err) {
		t.Error("hotfix no crea spec")
	}
}

func TestTrackerFailureQueuesAndSyncs(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	v.tr.FailTransitions = true
	v.tr.FailComments = true

	o := mustT(t)(v.e.Start(ctx, id, flow.Full, ""))
	if o.To != flow.Discovery || !hasWarning(o, "pendiente") {
		t.Fatalf("con el tracker caído el estado local avanza y avisa: %+v", o)
	}
	mustT(t)(v.e.Reject(ctx, id, RejectOpts{Note: "sigue abierto"}))
	rec, _ := v.e.Store.Load(id)
	if len(rec.Pending) != 2 {
		t.Fatalf("pendientes: %+v", rec.Pending)
	}

	v.tr.FailTransitions = false
	v.tr.FailComments = false
	n, err := v.e.Sync(ctx, id)
	if err != nil || n != 2 {
		t.Fatalf("sync: %d %v", n, err)
	}
	rec, _ = v.e.Store.Load(id)
	if len(rec.Pending) != 0 {
		t.Errorf("tras sync no deben quedar pendientes: %+v", rec.Pending)
	}
	if strings.Join(v.tr.Calls, "; ") != "transition "+id+" discovery; comment "+id {
		t.Errorf("orden de sync: %v", v.tr.Calls)
	}
	task, _ := v.tr.Get(ctx, id)
	if task.Phase != flow.Discovery {
		t.Errorf("tracker: %s", task.Phase)
	}
}

func TestPendingKeepsOrderAcrossCommands(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	v.tr.FailTransitions = true
	mustT(t)(v.e.Start(ctx, id, flow.Light, "")) // transition spec queda pendiente
	v.tr.FailTransitions = false
	mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{})) // debe sincronizar spec antes de implementing
	want := "transition " + id + " spec; transition " + id + " implementing"
	if got := strings.Join(v.tr.Calls, "; "); !strings.HasPrefix(got, want) {
		t.Errorf("orden: %s", got)
	}
}

func TestDoneRequiresVerifiedCheck(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Hotfix, ""))
	v.ver.ok, v.ver.detail = false, "cambios de código posteriores al check: internal/x.go"
	_, err := v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV})
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "check_required" || !strings.Contains(rj.Reason, "internal/x.go") {
		t.Fatalf("esperaba check_required con el detalle: %v", err)
	}
	if phaseOf(t, v, id) != flow.Implementing {
		t.Error("no debe avanzar")
	}
	v.ver.ok = true
	mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV}))
	if phaseOf(t, v, id) != flow.Quality {
		t.Error("con check verde avanza")
	}
}

func TestNotStartedAndUnknown(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	_, err := v.e.Approve(ctx, id, ApproveOpts{})
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "not_started" {
		t.Errorf("aprobar sin empezar: %v", err)
	}
	if _, err := v.e.Start(ctx, "MEM-99", flow.Full, ""); err == nil || !strings.Contains(err.Error(), "no existe") {
		t.Errorf("tarea inexistente: %v", err)
	}
	view, err := v.e.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Phase != flow.Backlog || view.Next.Gate != "lane" || view.Title != "Demo" {
		t.Errorf("status de tarea sin empezar: %+v", view)
	}
}

func TestActiveTask(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	if _, err := v.e.Active(ctx); err == nil {
		t.Error("sin tareas debe fallar")
	}
	a := v.task(t, "A")
	mustT(t)(v.e.Start(ctx, a, flow.Full, ""))
	if id, err := v.e.Active(ctx); err != nil || id != a {
		t.Errorf("una sola tarea activa: %q %v", id, err)
	}
	b := v.task(t, "B")
	mustT(t)(v.e.Start(ctx, b, flow.Full, ""))
	_, err := v.e.Active(ctx)
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "ambiguous_task" || !strings.Contains(rj.Reason, a) || !strings.Contains(rj.Reason, b) {
		t.Errorf("dos activas: %v", err)
	}
}

func TestDecisionRecorded(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Hotfix, ""))
	o := mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.NeedsDecision,
		Note: "¿Reintento o fallo rápido?", Options: []string{"Reintentar 3 veces", "Fallar rápido"}}))
	if o.Next.Gate != "decision" || len(o.Next.Options) != 3 {
		t.Fatalf("decisión: %+v", o.Next)
	}
	mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Choice: 2}))
	b, err := v.e.Store.ReadFile(id, "decisions.md")
	if err != nil || !strings.Contains(string(b), "¿Reintento o fallo rápido?") || !strings.Contains(string(b), "Fallar rápido") {
		t.Errorf("decisions.md: %q %v", b, err)
	}
}

func TestShowSections(t *testing.T) {
	v := newEnv(t, "stack: angular\n")
	ctx := context.Background()
	id := v.task(t, "Pantalla de cotizaciones")
	mustT(t)(v.e.Start(ctx, id, flow.Light, ""))
	rec, _ := v.e.Store.Load(id)
	path := filepath.Join(v.e.Cfg.Root, flow.SpecPath(id, rec.Flow.Slug))
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "## UI blueprint") {
		t.Error("stack con UI debe incluir la sección ui-blueprint")
	}
	body := strings.Replace(string(b), "## Brief\n", "## Brief\n\n**Objetivo:** cotizar rápido.\n", 1)
	_ = os.WriteFile(path, []byte(body), 0o644)

	out, err := v.e.Show(ctx, id, "brief", "")
	if err != nil || !strings.Contains(out, "cotizar rápido") || strings.Contains(out, "## Requirements") {
		t.Errorf("show brief: %q %v", out, err)
	}
	if _, err := v.e.Show(ctx, id, "spec", "nope"); err == nil || !strings.Contains(err.Error(), "brief") {
		t.Errorf("sección inexistente debe listar las válidas: %v", err)
	}
	if _, err := v.e.Show(ctx, id, "contract", ""); err == nil {
		t.Error("contrato inexistente debe fallar")
	}
	_ = v.e.Store.WriteFile(id, "contract.md", []byte("type X interface{}"))
	if out, _ := v.e.Show(ctx, id, "contract", ""); out != "type X interface{}" {
		t.Errorf("contract: %q", out)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Crédito: reabrir por rechazo fiscal":        "credito-reabrir-por-rechazo-fiscal",
		"  Ñandú & Pingüino  ":                       "nandu-pinguino",
		"Borrador PRE-FOLIO de cotización (WEB-108)": "borrador-pre-folio-de-cotizacion-web-108",
		strings.Repeat("palabra ", 20):               "palabra-palabra-palabra-palabra-palabra-palabra",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
