package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/tracker/trackertest"
)

var ctx = context.Background()

func ids(ts []tracker.Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func sorted(s []string) []string { s = slices.Clone(s); sort.Strings(s); return s }

// ---- contrato

func TestContractLabels(t *testing.T) {
	var f *fake
	trackertest.Run(t, trackertest.Harness{
		New: func(t *testing.T) tracker.Tracker { f = newFake(t); return f.client(Options{}) },
		Seed: func(t *testing.T, tr tracker.Tracker) (string, string) {
			f.issue(42, "Errores de validación")
			return "GH-42", "Errores de validación"
		},
		Missing:     "GH-999",
		NoStartDate: true,
	})
}

func TestContractProject(t *testing.T) {
	var f *fake
	trackertest.Run(t, trackertest.Harness{
		New: func(t *testing.T) tracker.Tracker {
			f = newFake(t)
			return f.client(Options{Project: "acme/7"})
		},
		Seed: func(t *testing.T, tr tracker.Tracker) (string, string) {
			p := f.project("acme", false, 7)
			p.add(f, f.issue(42, "Errores de validación"), "Backlog")
			return "GH-42", "Errores de validación"
		},
		Missing: "GH-999",
	})
}

// ---- identificadores y lectura

func TestGetPullRequestIsNotFound(t *testing.T) {
	f := newFake(t)
	f.issue(7, "es un PR").PR = true
	if _, err := f.client(Options{}).Get(ctx, "GH-7"); !errors.Is(err, tracker.ErrNotFound) {
		t.Errorf("un PR no es una tarea: %v", err)
	}
}

func TestWrongPrefixIsNotFound(t *testing.T) {
	f := newFake(t)
	f.issue(42, "x")
	c := f.client(Options{})
	for _, id := range []string{"XX-42", "gh-abc", "GH-", "GH-0", "GH-1.5", "42", "GH-42-1", ""} {
		if _, err := c.Get(ctx, id); !errors.Is(err, tracker.ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", id, err)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("un ID mal formado no llega a la red: %v", f.calls)
	}
	if _, err := c.Get(ctx, "GH-999"); !errors.Is(err, tracker.ErrNotFound) {
		t.Errorf("issue inexistente: %v", err)
	}
	// el prefijo se configura y se compara sin distinguir mayúsculas
	if task, err := f.client(Options{Prefix: "api"}).Get(ctx, "API-42"); err != nil || task.ID != "API-42" {
		t.Errorf("prefix api: %+v %v", task, err)
	}
}

func TestUnlabeledIssueIsBacklog(t *testing.T) {
	f := newFake(t)
	f.issue(1, "sin etiqueta", "bug")
	f.issue(2, "varias", "bflow:spec", "bflow:quality", "bug")
	c := f.client(Options{})
	got, err := c.Get(ctx, "GH-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != flow.Backlog || got.Closed || got.Title != "sin etiqueta" || got.Description != "cuerpo de sin etiqueta" ||
		got.URL != "https://github.com/acme/app/issues/1" || got.Start != nil || got.ID != "GH-1" {
		t.Errorf("issue abierto sin bflow:*: %+v", got)
	}
	if got, _ := c.Get(ctx, "GH-2"); got.Phase != flow.Quality {
		t.Errorf("con varias etiquetas gana la fase más avanzada: %q", got.Phase)
	}
}

func TestClosedUnlabeledIsDone(t *testing.T) {
	f := newFake(t)
	f.issue(1, "cerrado").State = "closed"
	f.issue(2, "cerrado con etiqueta", "bflow:quality").State = "closed"
	c := f.client(Options{})
	if got, _ := c.Get(ctx, "GH-1"); got.Phase != flow.Done || !got.Closed {
		t.Errorf("cerrado sin etiqueta es done: %+v", got)
	}
	if got, _ := c.Get(ctx, "GH-2"); got.Phase != flow.Quality || !got.Closed {
		t.Errorf("la etiqueta manda sobre el cierre: %+v", got)
	}
}

func TestListSkipsPullRequests(t *testing.T) {
	f := newFake(t)
	f.pageSize = 2
	f.issue(1, "a")
	f.issue(2, "pr").PR = true
	f.issue(3, "cerrado").State = "closed"
	f.issue(4, "b")
	f.issue(5, "c")
	f.issue(6, "otro pr").PR = true
	f.issue(7, "d")
	c := f.client(Options{})
	open, err := c.List(ctx, tracker.Filter{OpenOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(open); !slices.Equal(sorted(got), []string{"GH-1", "GH-4", "GH-5", "GH-7"}) {
		t.Errorf("abiertos sin PR y de todas las páginas: %v", got)
	}
	all, _ := c.List(ctx, tracker.Filter{})
	if got := ids(all); !slices.Equal(sorted(got), []string{"GH-1", "GH-3", "GH-4", "GH-5", "GH-7"}) {
		t.Errorf("con cerrados: %v", got)
	}
}

func TestListProjectOnlyThisRepo(t *testing.T) {
	f := newFake(t)
	f.pageSize = 2
	p := f.project("acme", false, 7)
	p.add(f, f.issue(1, "en el project"), "In Progress")
	p.add(f, f.issue(3, "cerrado en el project"), "Done").Issue.State = "closed"
	f.issue(2, "fuera del project")
	p.add(f, &fIssue{N: 9, Title: "de otro repo", State: "open", Repo: "acme/otro"}, "Backlog")
	p.add(f, &fIssue{N: 5, Title: "un PR", State: "open", PR: true}, "Backlog")
	p.Items = append(p.Items, &fItem{ID: "PVTI_draft", Dates: map[string]string{}})
	c := f.client(Options{Project: "acme/7"})

	all, err := c.List(ctx, tracker.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(all); !slices.Equal(sorted(got), []string{"GH-1", "GH-3"}) {
		t.Errorf("solo issues de este repo en el project: %v", got)
	}
	open, _ := c.List(ctx, tracker.Filter{OpenOnly: true})
	if got := ids(open); !slices.Equal(got, []string{"GH-1"}) {
		t.Errorf("OpenOnly excluye cerrados: %v", got)
	}
	if open[0].Phase != flow.Implementing || open[0].State != "In Progress" {
		t.Errorf("fase y estado desde Status: %+v", open[0])
	}
	if f.countCalls("GET /repos/acme/app/issues?") != 0 {
		t.Errorf("con project no se lista por REST: %v", f.calls)
	}
}

func TestProjectStatusReading(t *testing.T) {
	f := newFake(t)
	p := f.project("acme", false, 7, "Todo", "In Progress", "Done")
	it := p.add(f, f.issue(1, "a"), "Todo")
	it.Dates["F_start"] = "2026-09-01"
	p.add(f, f.issue(2, "b"), "In Progress")
	p.add(f, f.issue(3, "c"), "Done")
	f.issue(4, "fuera")
	p.add(f, f.issue(5, "sin status"), "")
	c := f.client(Options{Project: "acme/7"})
	want := map[string]struct {
		ph    flow.Phase
		state string
	}{"GH-1": {flow.Backlog, "Todo"}, "GH-2": {flow.Implementing, "In Progress"}, "GH-3": {flow.Done, "Done"},
		"GH-4": {flow.Backlog, ""}, "GH-5": {flow.Backlog, ""}}
	for id, w := range want {
		got, err := c.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Phase != w.ph || got.State != w.state {
			t.Errorf("%s: fase %q estado %q, want %q %q", id, got.Phase, got.State, w.ph, w.state)
		}
	}
	got, _ := c.Get(ctx, "GH-1")
	if got.Start == nil || got.Start.Format("2006-01-02") != "2026-09-01" || got.Due != nil {
		t.Errorf("Start sale del start_field: %+v", got)
	}
}

// ---- escritura

func TestTransitionKeepsOnlyTargetLabel(t *testing.T) {
	f := newFake(t)
	is := f.issue(1, "x", "bug", "bflow:spec", "bflow:quality")
	is.Ghost = []string{"bflow:discovery"} // GitHub lo muestra pero ya no está: el DELETE da 404
	c := f.client(Options{})
	if err := c.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sorted(is.Labels), []string{"bflow:implementing", "bug"}) {
		t.Errorf("etiquetas: %v", is.Labels)
	}
	if slices.Contains(f.deleteSeen, "bflow:implementing") || slices.Contains(f.deleteSeen, "bug") {
		t.Errorf("no se quitan la de destino ni las ajenas: %v", f.deleteSeen)
	}
	if err := c.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Transition(ctx, "GH-1", flow.Paused, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sorted(is.Labels), []string{"bflow:paused", "bug"}) {
		t.Errorf("tras pausar: %v", is.Labels)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "DELETE /repos/acme/app/issues/1/labels/bflow:spec") &&
		!strings.Contains(strings.Join(f.calls, "\n"), "DELETE /repos/acme/app/issues/1/labels/bflow%3Aspec") {
		t.Errorf("quita las etiquetas por su ruta: %v", f.calls)
	}
}

func TestDoneClosesIssueOnce(t *testing.T) {
	f := newFake(t)
	is := f.issue(1, "x", "bflow:quality")
	c := f.client(Options{})
	if err := c.Transition(ctx, "GH-1", flow.Done, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if is.State != "closed" || is.Reason != "completed" || !slices.Equal(is.Labels, []string{"bflow:done"}) || f.patches != 1 {
		t.Errorf("done cierra como completado: %s/%s %v patches=%d", is.State, is.Reason, is.Labels, f.patches)
	}
	if err := c.Transition(ctx, "GH-1", flow.Done, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if f.patches != 1 {
		t.Errorf("ya cerrado no vuelve a hacer PATCH: %d", f.patches)
	}
	if err := c.Transition(ctx, "GH-1", flow.Quality, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if is.State != "closed" || f.patches != 1 {
		t.Errorf("ningún otro destino reabre: %s patches=%d", is.State, f.patches)
	}
}

// R5: dropped cierra el issue como no planeado, antes de escribir la etiqueta,
// y no vuelve a cerrar uno ya cerrado.
func TestTransitionDroppedClosesNotPlanned(t *testing.T) {
	f := newFake(t)
	is := f.issue(1, "x", "bflow:spec")
	c := f.client(Options{})
	if err := c.Transition(ctx, "GH-1", flow.Dropped, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if is.State != "closed" || is.Reason != "not_planned" || !slices.Equal(is.Labels, []string{"bflow:dropped"}) || f.patches != 1 {
		t.Errorf("dropped cierra como no planeado: %s/%s %v patches=%d", is.State, is.Reason, is.Labels, f.patches)
	}
	patch, label := -1, -1
	for i, call := range f.calls {
		if strings.HasPrefix(call, "PATCH ") && patch < 0 {
			patch = i
		}
		if strings.Contains(call, "/labels") && strings.HasPrefix(call, "POST ") && label < 0 {
			label = i
		}
	}
	if patch < 0 || label < 0 || patch > label {
		t.Errorf("primero se cierra y después se escribe la etiqueta: %v", f.calls)
	}
	if err := c.Transition(ctx, "GH-1", flow.Dropped, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	if f.patches != 1 {
		t.Errorf("ya cerrado no vuelve a hacer PATCH: %d", f.patches)
	}
	done := f.issue(2, "y", "bflow:quality")
	if err := c.Transition(ctx, "GH-2", flow.Done, tracker.Patch{}); err != nil || done.Reason != "completed" {
		t.Errorf("done sigue cerrando como completado: %v %q", err, done.Reason)
	}
}

func TestStartAddsIssueToProject(t *testing.T) {
	f := newFake(t)
	p := f.project("acme", false, 7)
	is := f.issue(1, "x")
	c := f.client(Options{Project: "acme/7"})
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if err := c.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{StampStart: first}); err != nil {
		t.Fatal(err)
	}
	it := p.item(is)
	if it == nil {
		t.Fatal("el issue no quedó en el project")
	}
	if o := p.optByID(it.Status); o == nil || o.Name != "In Progress" {
		t.Errorf("Status = %+v", o)
	}
	if it.Dates["F_start"] != "2026-09-01" {
		t.Errorf("fecha de inicio: %v", it.Dates)
	}
	if err := c.Transition(ctx, "GH-1", flow.Quality, tracker.Patch{StampStart: first.AddDate(0, 0, 9)}); err != nil {
		t.Fatal(err)
	}
	if f.count("add") != 1 || len(p.Items) != 1 || it.Dates["F_start"] != "2026-09-01" {
		t.Errorf("add=%d items=%d fecha=%v: se agrega una vez y no se pisa el inicio", f.count("add"), len(p.Items), it.Dates)
	}
	if o := p.optByID(it.Status); o.Name != "En revisión" {
		t.Errorf("Status tras quality: %s", o.Name)
	}

	t.Run("sin opción para la fase", func(t *testing.T) {
		f := newFake(t)
		f.project("acme", false, 7, "Backlog", "Done")
		f.issue(1, "x")
		err := f.client(Options{Project: "acme/7"}).Transition(ctx, "GH-1", flow.Discovery, tracker.Patch{})
		if err == nil || !strings.Contains(err.Error(), "discovery") || !strings.Contains(err.Error(), "bflow tracker setup") ||
			!strings.Contains(err.Error(), "tracker.states") {
			t.Errorf("el error nombra la fase y sugiere setup o tracker.states: %v", err)
		}
	})
}

func TestStampStartMissingFieldIsNoop(t *testing.T) {
	f := newFake(t)
	p := f.project("acme", false, 7)
	p.DateFields = []fField{{"F_due", "Due date"}} // no hay "Start date"
	is := f.issue(1, "x")
	c := f.client(Options{Project: "acme/7"})
	if err := c.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{StampStart: time.Now()}); err != nil {
		t.Fatalf("sin campo de inicio no es error: %v", err)
	}
	if f.count("setDate") != 0 || len(p.item(is).Dates) != 0 {
		t.Errorf("nunca se escribe otro campo de fecha: %v", p.item(is).Dates)
	}
	if o := p.optByID(p.item(is).Status); o == nil || o.Name != "In Progress" {
		t.Error("el Status sí se escribe")
	}
	// start_field configurable: el campo con otro nombre se sella.
	p.DateFields = append(p.DateFields, fField{"F_inicio", "Inicio"})
	c2 := f.client(Options{Project: "acme/7", StartField: "Inicio"})
	if err := c2.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{StampStart: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if d := p.item(is).Dates; d["F_inicio"] != "2026-09-02" || d["F_due"] != "" {
		t.Errorf("start_field Inicio: %v", d)
	}
}

func TestCreateLeavesBacklog(t *testing.T) {
	f := newFake(t)
	got, err := f.client(Options{}).Create(ctx, "Nueva", "el cuerpo")
	if err != nil {
		t.Fatal(err)
	}
	is := f.find(1)
	if is == nil || is.Title != "Nueva" || is.Body != "el cuerpo" || !slices.Contains(is.Labels, "bflow:backlog") {
		t.Fatalf("issue creado con etiqueta de backlog: %+v", is)
	}
	if got.ID != "GH-1" || got.Phase != flow.Backlog || got.Title != "Nueva" {
		t.Errorf("Task devuelta: %+v", got)
	}

	f2 := newFake(t)
	p := f2.project("acme", false, 7)
	got, err = f2.client(Options{Project: "acme/7"}).Create(ctx, "En project", "b")
	if err != nil {
		t.Fatal(err)
	}
	it := p.item(f2.find(1))
	if it == nil || p.optByID(it.Status) == nil || p.optByID(it.Status).Name != "Backlog" || got.Phase != flow.Backlog {
		t.Errorf("con project queda con Status Backlog: %+v %+v", it, got)
	}
	if len(f2.find(1).Labels) != 0 {
		t.Errorf("con project no hay etiquetas de fase: %v", f2.find(1).Labels)
	}
}

func TestCloseRef(t *testing.T) {
	c := New(Options{Repo: "acme/app"})
	cases := map[string]string{"GH-42": "Closes #42", "gh-42": "Closes #42", "XX-42": "", "GH-x": "", "": "", "GH-0": "", "42": ""}
	for id, want := range cases {
		if got := c.CloseRef(id); got != want {
			t.Errorf("CloseRef(%q) = %q, want %q", id, got, want)
		}
	}
	if got := New(Options{Prefix: "api"}).CloseRef("API-7"); got != "Closes #7" {
		t.Errorf("prefijo configurado: %q", got)
	}
}

func TestStateMap(t *testing.T) {
	f := newFake(t)
	f.labels["bflow:spec"] = fOpt{Color: "818CF8"}
	f.labels["bflow:quality"] = fOpt{Color: "A855F7"}
	f.labels["bug"] = fOpt{Color: "ff0000"}
	got, err := f.client(Options{}).StateMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("solo las etiquetas bflow:*: %+v", got)
	}
	for _, s := range got {
		if s.Group != "label" || s.Phase == "" || !slices.Equal(s.Writes, []string{string(s.Phase)}) {
			t.Errorf("etiqueta: %+v", s)
		}
	}

	f2 := newFake(t)
	f2.project("acme", false, 7, "Backlog", "In Progress", "Done", "Cancelled")
	got, err = f2.client(Options{Project: "acme/7"}).StateMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]tracker.StateInfo{}
	for _, s := range got {
		by[s.Name] = s
	}
	ip := by["In Progress"]
	if len(got) != 4 || ip.Group != "status" || ip.Phase != flow.Implementing ||
		!slices.Contains(ip.Writes, "contract") || !slices.Contains(ip.Writes, "implementing") {
		t.Errorf("Status: %+v", got)
	}
	if by["Cancelled"].Phase != flow.Dropped || !slices.Equal(by["Cancelled"].Writes, []string{"dropped"}) {
		t.Errorf("Cancelled es el estado de dropped: %+v", by["Cancelled"])
	}
}

// ---- setup y projects

func phaseNames() []string {
	var out []string
	for _, p := range tracker.PhaseOrder {
		out = append(out, "bflow:"+string(p))
	}
	return out
}

func TestSetupLabelsDryRun(t *testing.T) {
	f := newFake(t)
	f.labels["bflow:backlog"] = fOpt{Color: "A3A3A3"}
	f.labels["bflow:done"] = fOpt{Color: "22C55E"}
	c := f.client(Options{})
	want := slices.DeleteFunc(phaseNames(), func(n string) bool { return n == "bflow:backlog" || n == "bflow:done" })
	got, err := c.EnsureStates(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sorted(got), sorted(want)) || f.countCalls("POST /repos/acme/app/labels") != 0 {
		t.Errorf("dry-run lista y no crea: %v / %v", got, f.calls)
	}
	got, err = c.EnsureStates(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sorted(got), sorted(want)) || len(f.labels) != 13 {
		t.Errorf("crea las que faltan: %v (%d etiquetas)", got, len(f.labels))
	}
	for _, p := range tracker.PhaseOrder {
		l := f.labels["bflow:"+string(p)]
		if p != flow.Backlog && p != flow.Done && (l.Color != strings.TrimPrefix(tracker.DefaultStates[p].Color, "#") || l.Desc != "bflow: "+string(p)) {
			t.Errorf("%s: color %q desc %q", p, l.Color, l.Desc)
		}
	}
	if got, _ := c.EnsureStates(ctx, false); len(got) != 0 {
		t.Errorf("idempotente: %v", got)
	}
}

func statusColors() map[string]string {
	return map[string]string{"Spec pendiente": "BLUE", "Contrato": "PURPLE", "In Progress": "BLUE", "En pausa": "PURPLE",
		"En revisión": "PINK", "Documentando": "GREEN", "Walkthrough": "ORANGE", "PR abierto": "ORANGE", "Bloqueado": "RED"}
}

func TestSetupOptionsKeepIDs(t *testing.T) {
	f := newFake(t)
	p := f.project("acme", false, 7, "Backlog", "Discovery", "Done")
	before := slices.Clone(p.Options)
	is := f.issue(1, "x")
	p.add(f, is, "Discovery")
	c := f.client(Options{Project: "acme/7"})

	got, err := c.EnsureStates(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	wantNew := []string{"Spec pendiente", "Contrato", "In Progress", "En pausa", "En revisión", "Documentando", "Walkthrough", "PR abierto", "Bloqueado", "Cancelled"}
	if !slices.Equal(sorted(got), sorted(wantNew)) || f.count("setField") != 0 {
		t.Errorf("dry-run lista las faltantes sin escribir: %v", got)
	}

	got, err = c.EnsureStates(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sorted(got), sorted(wantNew)) || f.count("setField") != 1 {
		t.Fatalf("una sola mutación: %v setField=%d", got, f.count("setField"))
	}
	if len(p.Options) != 13 {
		t.Fatalf("13 opciones: %v", p.names())
	}
	for i, o := range before {
		if p.Options[i] != o {
			t.Errorf("la opción existente cambió (id, nombre, color o descripción): %+v -> %+v", o, p.Options[i])
		}
	}
	for name, color := range statusColors() {
		o := p.opt(name)
		if o == nil || o.Color != color || !strings.HasPrefix(o.Desc, "bflow: ") {
			t.Errorf("%s: %+v, want color %s", name, o, color)
		}
	}
	if o := p.opt("Contrato"); o == nil || o.Desc != "bflow: contract" {
		t.Errorf("descripción de Contrato: %+v", o)
	}
	if got, err := c.Get(ctx, "GH-1"); err != nil || got.Phase != flow.Discovery {
		t.Errorf("el item conserva su Status: %+v %v", got, err)
	}
	if p.item(is).Status != before[1].ID {
		t.Error("el item apunta a otro id de opción")
	}
	if got, _ := c.EnsureStates(ctx, false); len(got) != 0 || f.count("setField") != 1 {
		t.Errorf("idempotente: %v", got)
	}
}

func TestSetupManualWhenIDUnsupported(t *testing.T) {
	f := newFake(t)
	f.idInput = false
	p := f.project("acme", false, 7, "Backlog", "Discovery", "Done")
	before := slices.Clone(p.Options)
	c := f.client(Options{Project: "acme/7"})

	_, err := c.EnsureStates(ctx, false)
	var me *tracker.ManualSetupError
	if !errors.As(err, &me) {
		t.Fatalf("sin id en la entrada no se escribe: %v", err)
	}
	if !strings.Contains(me.Where, "acme/7") || me.Reason == "" {
		t.Errorf("dónde y por qué: %+v", me)
	}
	for name, color := range statusColors() {
		if !slices.Contains(me.Missing, name+" ("+color+")") {
			t.Errorf("falta %q (%s) en %v", name, color, me.Missing)
		}
	}
	if len(me.Missing) != 10 || f.count("setField") != 0 || f.destroyed || !slices.Equal(p.Options, before) {
		t.Errorf("no modifica nada: %d faltantes, setField=%d", len(me.Missing), f.count("setField"))
	}
	if got, err := c.EnsureStates(ctx, true); err != nil || len(got) != 10 {
		t.Errorf("dry-run solo lista: %v %v", got, err)
	}
}

func TestOwnerResolvesOrgThenUser(t *testing.T) {
	f := newFake(t)
	f.project("jdoe", true, 3)
	f.issue(1, "x")
	// El repo es acme/app; el owner del project es otro usuario: solo hay que resolverlo.
	c := f.client(Options{Project: "jdoe/3"})
	if err := c.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{}); err != nil {
		t.Fatal(err)
	}
	ops := strings.Join(f.gqlOps, ",")
	if !strings.Contains(ops, "resolve,resolve") {
		t.Errorf("primero organización y luego usuario: %s", ops)
	}
	raw, err := os.ReadFile(c.cachePath)
	if err != nil {
		t.Fatalf("la caché debe existir: %v", err)
	}
	for _, w := range []string{`"project"`, "jdoe/3", `"owner_type"`, "user", "PVT_3", "F_status", "F_start", "In Progress"} {
		if !strings.Contains(string(raw), w) {
			t.Errorf("la caché no dice %q: %s", w, raw)
		}
	}
	if strings.Contains(string(raw), f.token) {
		t.Errorf("la caché no guarda el token: %s", raw)
	}

	f2 := newFake(t)
	f2.project("acme", false, 7)
	f2.project("acme", false, 9)
	ps, err := f2.client(Options{}).Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pids []string
	for _, p := range ps {
		pids = append(pids, p.ID+"="+p.Name)
	}
	if !slices.Equal(sorted(pids), []string{"acme/7=Tablero 7", "acme/9=Tablero 9"}) {
		t.Errorf("Projects de la organización: %v", pids)
	}

	f3 := newFake(t)
	f3.project("acme", true, 4) // el owner del repo es un usuario
	ps, err = f3.client(Options{}).Projects(ctx)
	if err != nil || len(ps) != 1 || ps[0].ID != "acme/4" {
		t.Errorf("Projects del usuario: %+v %v", ps, err)
	}
}

func TestStaleCacheRetriesOnce(t *testing.T) {
	f := newFake(t)
	p := f.project("acme", false, 7)
	f.issue(1, "x")
	cache := filepath.Join(t.TempDir(), "github.json")
	stale := `{"project":"acme/7","owner_type":"org","project_id":"PVT_7","status_field":"F_status","options":{"In Progress":"OLD"},"start_field":"F_start"}`
	if err := os.WriteFile(cache, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	c := f.client(Options{Project: "acme/7", CachePath: cache})
	if err := c.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{}); err != nil {
		t.Fatalf("una caché vieja se descarta y se reintenta: %v", err)
	}
	if n := f.count("resolve"); n != 1 {
		t.Errorf("resolvió %d veces, quería 1", n)
	}
	if o := p.optByID(p.item(f.find(1)).Status); o == nil || o.Name != "In Progress" {
		t.Errorf("Status final: %+v", o)
	}
	raw, _ := os.ReadFile(cache)
	if strings.Contains(string(raw), "OLD") {
		t.Errorf("la caché se reescribe: %s", raw)
	}

	f2 := newFake(t)
	f2.project("acme", false, 7)
	f2.issue(1, "x")
	f2.notFound = true
	c2 := f2.client(Options{Project: "acme/7"})
	if err := c2.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{}); err == nil {
		t.Error("si sigue fallando, falla")
	}
	if n := f2.count("setStatus"); n > 2 {
		t.Errorf("reintenta una sola vez: %d intentos", n)
	}
}

// ---- errores y red

func TestErrorMessages(t *testing.T) {
	type tc struct {
		name string
		set  func(f *fake) Options
		op   func(c *Client) error
		want []string
		nf   bool
	}
	get := func(c *Client) error { _, err := c.Get(ctx, "GH-42"); return err }
	start := func(c *Client) error { return c.Transition(ctx, "GH-42", flow.Implementing, tracker.Patch{}) }
	cases := []tc{
		{name: "401", set: func(f *fake) Options { f.status = 401; return Options{} }, op: get, want: []string{"token inválido o vencido", "bflow connect github"}},
		{name: "404", set: func(f *fake) Options { f.status = 404; return Options{} }, op: get, nf: true},
		{name: "500 con cuerpo largo y el token", set: func(f *fake) Options {
			f.status = 500
			f.errBody = `{"message":"bad ` + f.token + ` ` + strings.Repeat("x", 1000) + `"}`
			return Options{}
		}, op: get, want: []string{"GET", "/repos/acme/app/issues/42", "500"}},
		{name: "sin token", set: func(f *fake) Options { return Options{Token: "-"} }, op: get, want: []string{"bflow connect github"}},
		{name: "403 en Projects", set: func(f *fake) Options { f.gqlStatus = 403; return Options{Project: "acme/7"} }, op: start, want: []string{"Projects"}},
		{name: "scope de organización", set: func(f *fake) Options {
			f.project("acme", false, 7)
			f.scopeErr = "INSUFFICIENT_SCOPES"
			return Options{Project: "acme/7"}
		}, op: start, want: []string{"Projects", "fine-grained", "organización"}},
		{name: "FORBIDDEN de usuario", set: func(f *fake) Options {
			f.project("jdoe", true, 3)
			f.scopeErr = "FORBIDDEN"
			return Options{Project: "jdoe/3"}
		}, op: start, want: []string{"Projects", "clásico", "project"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			f.issue(42, "x")
			cl := f.client(c.set(f))
			err := c.op(cl)
			if err == nil {
				t.Fatal("esperaba un error")
			}
			if c.nf && !errors.Is(err, tracker.ErrNotFound) {
				t.Errorf("404 envuelve ErrNotFound: %v", err)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("el error no dice %q: %v", w, err)
				}
			}
			if strings.Contains(err.Error(), f.token) || strings.Contains(err.Error(), "tok-secret") {
				t.Errorf("el error contiene el token: %v", err)
			}
			if len(err.Error()) > 600 {
				t.Errorf("el cuerpo de error se recorta a 200 caracteres: %d", len(err.Error()))
			}
			if c.name == "sin token" && len(f.calls) != 0 {
				t.Errorf("sin token no se llama a la red: %v", f.calls)
			}
		})
	}
}

func TestRateLimitRetry(t *testing.T) {
	f := newFake(t)
	f.issue(42, "x")
	f.rateLeft, f.rateStatus, f.rateHeader = 2, 429, map[string]string{"Retry-After": "2"}
	c := f.client(Options{})
	if _, err := c.Get(ctx, "GH-42"); err != nil {
		t.Fatalf("reintenta tras 429: %v", err)
	}
	if len(f.sleeps) != 2 || f.sleeps[0] != 2*time.Second {
		t.Errorf("espera Retry-After: %v", f.sleeps)
	}

	f.sleeps, f.calls = nil, nil
	reset := time.Now().Add(100 * time.Second).Unix()
	f.rateLeft, f.rateStatus = 1, 403
	f.rateHeader = map[string]string{"x-ratelimit-remaining": "0", "x-ratelimit-reset": itoa(reset)}
	if _, err := c.Get(ctx, "GH-42"); err != nil {
		t.Fatalf("reintenta tras 403 con límite agotado: %v", err)
	}
	if len(f.sleeps) != 1 || f.sleeps[0] < time.Second || f.sleeps[0] > 30*time.Second {
		t.Errorf("la espera se acota entre 1 y 30 s: %v", f.sleeps)
	}

	f.sleeps, f.calls = nil, nil
	f.rateLeft, f.rateStatus, f.rateHeader = 100, 429, map[string]string{"Retry-After": "1"}
	if _, err := c.Get(ctx, "GH-42"); err == nil {
		t.Error("tras 3 reintentos se rinde")
	}
	if len(f.calls) != 4 || len(f.sleeps) != 3 {
		t.Errorf("1 intento + 3 reintentos: %d llamadas, %d esperas", len(f.calls), len(f.sleeps))
	}

	f.sleeps, f.calls, f.rateLeft = nil, nil, 0
	f.status = 403 // 403 sin señales de límite no se reintenta
	if _, err := c.Get(ctx, "GH-42"); err == nil || len(f.calls) != 1 || len(f.sleeps) != 0 {
		t.Errorf("403 simple: %v %d llamadas", err, len(f.calls))
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestGraphQLURLEnterprise(t *testing.T) {
	cases := map[string]string{
		"https://api.github.com":         "https://api.github.com/graphql",
		"https://ghe.example.com/api/v3": "https://ghe.example.com/api/graphql",
	}
	for api, want := range cases {
		if got := graphqlURL(api); got != want {
			t.Errorf("graphqlURL(%q) = %q, want %q", api, got, want)
		}
	}
	f := newFake(t)
	p := f.project("acme", false, 7)
	is := f.issue(1, "x")
	c := f.client(Options{API: f.srv.URL + "/api/v3", Project: "acme/7"})
	if err := c.Transition(ctx, "GH-1", flow.Implementing, tracker.Patch{}); err != nil {
		t.Fatalf("REST bajo /api/v3 y GraphQL en /api/graphql: %v", err)
	}
	if p.item(is) == nil || !slices.Contains(f.calls, "POST /api/graphql") {
		t.Errorf("calls: %v", f.calls)
	}
}

func TestHTTPError422ShowsFieldDetail(t *testing.T) {
	c := &Client{Token: "tok123"}
	raw := []byte(`{"message":"Validation Failed","errors":[{"resource":"Issue","field":"title","code":"invalid"},{"code":"custom","message":"tok123 malo"}]}`)
	err := c.httpError("POST", "/repos/a/b/issues", 422, raw, false)
	got := err.Error()
	for _, want := range []string{"Validation Failed", "Issue.title: invalid", "custom: *** malo"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en %q", want, got)
		}
	}
	if strings.Contains(got, "tok123") {
		t.Errorf("token filtrado: %q", got)
	}
}

func TestRedactTrimsByRunes(t *testing.T) {
	c := &Client{}
	got := c.redact(strings.Repeat("é", 300))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 200 {
		t.Errorf("recorte inválido: %d runas, válido=%v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
}
