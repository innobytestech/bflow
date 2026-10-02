package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

// R1, R9: drop retira, comenta el motivo y queda en el log.
func TestDropRetiresTask(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Light, "", ""))
	o := mustT(t)(v.e.Drop(ctx, id, "cambió la prioridad"))
	if o.To != flow.Dropped || o.Phase != flow.Dropped {
		t.Fatalf("outcome: %+v", o)
	}
	tk, _ := v.tr.Get(ctx, id)
	if !tk.Closed || tk.Phase != flow.Dropped {
		t.Errorf("tracker: %+v", tk)
	}
	cs, _ := v.tr.Comments(ctx, id)
	if len(cs) != 1 || cs[0].Body != "**Retirada:** cambió la prioridad" {
		t.Errorf("comentarios: %+v", cs)
	}
	if en := lastEntry(t, v, id); en.Event != "drop" || en.Note != "cambió la prioridad" || en.To != flow.Dropped {
		t.Errorf("log: %+v", en)
	}
	if _, err := v.e.Drop(ctx, id, "otra vez"); err == nil {
		t.Error("retirar dos veces debe fallar")
	}
}

// R2, R13.
func TestDropNeedsNoteAndStarted(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	_, err := v.e.Drop(ctx, id, "motivo")
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "not_started" {
		t.Fatalf("sin empezar: %v", err)
	}
	mustT(t)(v.e.Start(ctx, id, flow.Light, "", ""))
	_, err = v.e.Drop(ctx, id, "  ")
	if !errors.As(err, &rj) || rj.Code != "note_required" {
		t.Fatalf("sin motivo: %v", err)
	}
	if phaseOf(t, v, id) != flow.Spec {
		t.Error("el estado no cambia")
	}
}

// R11: avisa de la rama y del PR, sin tocarlos.
func TestDropWarnsBranchAndPR(t *testing.T) {
	v := newEnv(t, "vcs: { base_branch: dev }\n")
	g, h := &fakeGit{branch: "dev"}, newHost()
	v.e.Git, v.e.Host = g, h
	ctx := context.Background()

	a := v.task(t, "Con rama")
	mustT(t)(v.e.Start(ctx, a, flow.Hotfix, "", ""))
	ra, _ := v.e.Store.Load(a)
	o := mustT(t)(v.e.Drop(ctx, a, "ya no"))
	if !hasWarning(o, "la rama "+ra.Branch+" sigue existiendo; bórrala a mano si ya no sirve") {
		t.Errorf("aviso de rama: %v", o.Warnings)
	}
	if hasWarning(o, "el PR") {
		t.Errorf("sin PR no hay aviso de PR: %v", o.Warnings)
	}

	b := v.task(t, "Con PR")
	toWalkthrough(t, v, b)
	ob := mustT(t)(v.e.Approve(ctx, b, ApproveOpts{}))
	if ob.PR == nil {
		t.Fatalf("preparación: %+v", ob)
	}
	calls := len(g.calls)
	o = mustT(t)(v.e.Drop(ctx, b, "ya no"))
	if !hasWarning(o, "el PR "+ob.PR.URL+" sigue abierto; ciérralo a mano si ya no sirve") {
		t.Errorf("aviso de PR: %v", o.Warnings)
	}
	if h.opened != 1 || len(g.calls) != calls {
		t.Errorf("no toca ni la rama ni el PR: abiertos=%d calls=%v", h.opened, g.calls[calls:])
	}
}

// R4: una tarea retirada sale de Views, de Active y del panel.
func TestViewsHideDropped(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	a, b := v.task(t, "Una"), v.task(t, "Otra")
	mustT(t)(v.e.Start(ctx, a, flow.Light, "", ""))
	mustT(t)(v.e.Start(ctx, b, flow.Light, "", ""))
	if _, err := v.e.Active(ctx); err == nil {
		t.Fatal("con dos abiertas Active es ambigua")
	}
	mustT(t)(v.e.Drop(ctx, a, "ya no"))
	views, err := v.e.Views(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].ID != b {
		t.Errorf("views: %+v", views)
	}
	if id, err := v.e.Active(ctx); err != nil || id != b {
		t.Errorf("Active = %q %v", id, err)
	}
	rep, err := v.e.Panel(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, a) {
			t.Errorf("el panel no debe mencionar la retirada: %s", w)
		}
	}
	if tk, _ := v.tr.Get(ctx, a); !tk.Closed || tk.Phase != flow.Dropped {
		t.Errorf("el panel no reabre ni reconcilia la retirada: %+v", tk)
	}
}
