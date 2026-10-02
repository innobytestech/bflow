package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
)

func isSplitInvalid(err error) bool {
	var rj *flow.Rejection
	return errors.As(err, &rj) && rj.Code == "split_invalid"
}

func div(body string) string { return "Objetivo.\n\n### División\n" + body }

// R15: tabla de divisiones válidas e inválidas.
func TestParseSplit(t *testing.T) {
	long := strings.Repeat("ñ", 120)
	var six, seven string
	for i, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		line := "- **Hija " + n + "**: alcance " + n + "\n"
		seven += line
		if i < 6 {
			six += line
		}
	}
	cases := []struct {
		name  string
		brief string
		want  []SplitPart // nil: debe ser split_invalid
	}{
		{"dos hijas", div("- **Uno**: primer alcance\n- **Dos**: segundo alcance\n"),
			[]SplitPart{{"Uno", "primer alcance"}, {"Dos", "segundo alcance"}}},
		{"sin acento y en minúsculas", "### division\n- **A**: x\n- **B**: y\n", []SplitPart{{"A", "x"}, {"B", "y"}}},
		{"mayúsculas", "### DIVISIÓN\n- **A**: x\n- **B**: y\n", []SplitPart{{"A", "x"}, {"B", "y"}}},
		{"sangría suma al alcance", div("- **Uno**: primero\n  más detalle\n    y más\n- **Dos**: segundo\n"),
			[]SplitPart{{"Uno", "primero\nmás detalle\ny más"}, {"Dos", "segundo"}}},
		{"termina en el siguiente encabezado", div("- **Uno**: a\n- **Dos**: b\n\n### Riesgos\n- **Tres**: c\n"),
			[]SplitPart{{"Uno", "a"}, {"Dos", "b"}}},
		{"seis hijas", div(six),
			[]SplitPart{{"Hija a", "alcance a"}, {"Hija b", "alcance b"}, {"Hija c", "alcance c"}, {"Hija d", "alcance d"}, {"Hija e", "alcance e"}, {"Hija f", "alcance f"}}},
		{"título de 120 runas", div("- **" + long + "**: x\n- **B**: y\n"), []SplitPart{{long, "x"}, {"B", "y"}}},

		{"sin sección", "Objetivo.\n\n### Riesgos\n- algo\n", nil},
		{"sección vacía", div(""), nil},
		{"una sola hija", div("- **Uno**: a\n"), nil},
		{"siete hijas", div(seven), nil},
		{"título de 121 runas", div("- **" + long + "x**: x\n- **B**: y\n"), nil},
		{"título vacío", div("- **  **: x\n- **B**: y\n"), nil},
		{"alcance vacío", div("- **Uno**:\n- **Dos**: b\n"), nil},
		{"títulos repetidos", div("- **Uno**: a\n- **Uno**: b\n"), nil},
		{"viñeta sin negritas", div("- Uno: a\n- **Dos**: b\n"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseSplit(c.brief)
			if c.want == nil {
				if !isSplitInvalid(err) {
					t.Fatalf("esperaba split_invalid, got %v (%v)", err, got)
				}
				var rj *flow.Rejection
				errors.As(err, &rj)
				if strings.TrimSpace(rj.Reason) == "" {
					t.Error("el rechazo debe decir qué falta")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("hija %d: %+v, want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

const twoChildren = "- **Cobros**: registrar cobros\n  con reintento\n- **Reportes**: exportar a CSV\n"

// toSplitGate deja la tarea en el gate split con la división indicada en el Brief.
func toSplitGate(t *testing.T, v *env, brief string) string {
	t.Helper()
	ctx := context.Background()
	id := v.task(t, "Pagos completos")
	mustT(t)(v.e.Start(ctx, id, flow.Light, "", ""))
	rec, _ := v.e.Store.Load(id)
	if err := os.WriteFile(v.e.specAbs(rec.Flow), []byte("## Brief\n\n"+brief+"\n\n## Requirements\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Split, Note: "son dos cosas"}))
	rec, _ = v.e.Store.Load(id)
	if rec.Flow.Gate == nil || rec.Flow.Gate.Name != flow.GateSplit {
		t.Fatalf("preparación: %+v", rec.Flow)
	}
	return id
}

// R14: SPLIT sin una división válida en el Brief se rechaza.
func TestReportSplitNeedsDivision(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Pagos completos")
	mustT(t)(v.e.Start(ctx, id, flow.Light, "", ""))
	_, err := v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Split, Note: "son dos"})
	if !isSplitInvalid(err) {
		t.Fatalf("sin ### División: %v", err)
	}
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.Gate != nil || rec.Flow.Phase != flow.Spec {
		t.Errorf("el estado no cambia: %+v", rec.Flow)
	}
	// con una sola hija tampoco
	_ = os.WriteFile(v.e.specAbs(rec.Flow), []byte("## Brief\n\n"+div("- **Uno**: a\n")+"\n\n## Requirements\n"), 0o644)
	if _, err := v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Split}); !isSplitInvalid(err) {
		t.Fatalf("una hija: %v", err)
	}
	_ = os.WriteFile(v.e.specAbs(rec.Flow), []byte("## Brief\n\n"+div(twoChildren)+"\n\n## Requirements\n"), 0o644)
	if o := mustT(t)(v.e.Report(ctx, id, ReportOpts{Agent: "spec-author", Verdict: flow.Split})); o.Gate != "split" {
		t.Errorf("con división válida abre el gate: %+v", o)
	}
	// otros veredictos no leen el Brief
	id2 := v.task(t, "Otra")
	mustT(t)(v.e.Start(ctx, id2, flow.Light, "", ""))
	mustT(t)(v.e.Report(ctx, id2, ReportOpts{Agent: "spec-author", Verdict: flow.Ready}))
}

func allTasks(t *testing.T, v *env) []tracker.Task {
	t.Helper()
	ts, err := v.tr.List(context.Background(), tracker.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// R17 a R23: aprobar el split crea las hijas y retira a la madre.
func TestApproveSplitCreatesChildren(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := toSplitGate(t, v, div(twoChildren))
	o := mustT(t)(v.e.Approve(ctx, id, ApproveOpts{}))
	if o.To != flow.Dropped || o.Phase != flow.Dropped {
		t.Fatalf("outcome: %+v", o)
	}
	ts := allTasks(t, v)
	if len(ts) != 3 {
		t.Fatalf("madre + 2 hijas, got %d: %+v", len(ts), ts)
	}
	byTitle := map[string]tracker.Task{}
	for _, tk := range ts {
		byTitle[tk.Title] = tk
	}
	c1, c2 := byTitle["Cobros"], byTitle["Reportes"]
	if c1.ID == "" || c2.ID == "" {
		t.Fatalf("hijas por título: %+v", byTitle)
	}
	if c1.Description != "registrar cobros\ncon reintento\n\nParte de "+id+" · Pagos completos" {
		t.Errorf("descripción: %q", c1.Description)
	}
	if c1.Phase != flow.Backlog || c2.Phase != flow.Backlog {
		t.Errorf("las hijas quedan en backlog: %s %s", c1.Phase, c2.Phase)
	}
	for _, c := range []tracker.Task{c1, c2} {
		if _, err := v.e.Store.Load(c.ID); err == nil {
			t.Errorf("%s no debe tener carril ni registro", c.ID)
		}
	}
	mother, _ := v.tr.Get(ctx, id)
	if !mother.Closed || mother.Phase != flow.Dropped {
		t.Errorf("la madre se cierra: %+v", mother)
	}
	cs, _ := v.tr.Comments(ctx, id)
	want := "**Dividida en:**\n- " + c1.ID + " · Cobros\n- " + c2.ID + " · Reportes"
	found := false
	for _, c := range cs {
		found = found || c.Body == want
	}
	if !found {
		t.Errorf("comentario %q en %+v", want, cs)
	}
	rec, _ := v.e.Store.Load(id)
	if len(rec.Split) != 2 || rec.Split[0].Title != "Cobros" || rec.Split[0].ID != c1.ID || rec.Split[1].ID != c2.ID {
		t.Errorf("Record.Split: %+v", rec.Split)
	}
	if rec.Flow.Gate != nil || rec.Flow.Block != nil {
		t.Errorf("sin gate ni bloqueo: %+v", rec.Flow)
	}
	en := lastEntry(t, v, id)
	raw, _ := json.Marshal(en.Data["children"])
	if string(raw) != `["`+c1.ID+`","`+c2.ID+`"]` {
		t.Errorf("data.children = %s", raw)
	}
}

// R17: sin Creator no se crea nada ni cambia el estado.
func TestApproveSplitWithoutCreator(t *testing.T) {
	v := newEnv(t, "")
	id := toSplitGate(t, v, div(twoChildren))
	v.e.Tracker = struct{ tracker.Tracker }{v.tr} // solo la interfaz base: sin Create
	_, err := v.e.Approve(context.Background(), id, ApproveOpts{})
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "no_creator" {
		t.Fatalf("err=%v", err)
	}
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.Phase != flow.Spec || rec.Flow.Gate == nil || rec.Flow.Gate.Name != flow.GateSplit || len(rec.Split) != 0 {
		t.Errorf("el estado no cambia: %+v", rec)
	}
	if n := len(allTasks(t, v)); n != 1 {
		t.Errorf("no crea nada: %d tareas", n)
	}
}

// R19, R20: si falla una hija, el error lo dice y reintentar crea solo las que faltan.
func TestApproveSplitResumesAfterFailure(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := toSplitGate(t, v, div(twoChildren))
	v.tr.FailCreateAfter = 2 // la madre y la primera hija
	_, err := v.e.Approve(ctx, id, ApproveOpts{})
	if err == nil {
		t.Fatal("debía fallar la segunda hija")
	}
	rec, _ := v.e.Store.Load(id)
	if len(rec.Split) != 1 || rec.Split[0].Title != "Cobros" {
		t.Fatalf("la primera hija ya quedó guardada: %+v", rec.Split)
	}
	if !strings.Contains(err.Error(), rec.Split[0].ID) || !strings.Contains(err.Error(), "bflow approve "+id+" --gate split") {
		t.Errorf("el error lista lo creado y cómo reintentar: %v", err)
	}
	if rec.Flow.Phase != flow.Spec || rec.Flow.Gate == nil || rec.Flow.Gate.Name != flow.GateSplit {
		t.Errorf("la madre sigue en el gate: %+v", rec.Flow)
	}
	if n := len(allTasks(t, v)); n != 2 {
		t.Fatalf("madre + 1 hija, got %d", n)
	}

	v.tr.FailCreateAfter = 0
	o := mustT(t)(v.e.Approve(ctx, id, ApproveOpts{Gate: "split"}))
	if o.To != flow.Dropped {
		t.Fatalf("reintento: %+v", o)
	}
	if n := len(allTasks(t, v)); n != 3 {
		t.Errorf("solo se crea la que faltaba: %d tareas", n)
	}
	rec, _ = v.e.Store.Load(id)
	if len(rec.Split) != 2 {
		t.Errorf("Split: %+v", rec.Split)
	}
	cs, _ := v.tr.Comments(ctx, id)
	listed := false
	for _, c := range cs {
		if strings.HasPrefix(c.Body, "**Dividida en:**") {
			listed = strings.Contains(c.Body, "Cobros") && strings.Contains(c.Body, "Reportes")
		}
	}
	if !listed {
		t.Errorf("el comentario lista a las dos hijas: %+v", cs)
	}
}
