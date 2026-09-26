package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

const featureList = `{
  "project": "acme-web",
  "rules": {"valid_status": ["pending","discovery","specReady","inProgress","implemented","documented","done","blocked"]},
  "features": [
    {"id": 1, "name": "cfdi-line-classifier", "title": "Clasificar producto/servicio", "description": "Reparar la importación", "acceptance": ["A1", "A2"], "status": "done", "sdd": true},
    {"id": 2, "name": "quotation-prefolio-draft", "title": "Borrador pre-folio", "description": "Compartido", "acceptance": ["Guarda solo"], "status": "inProgress", "sdd": true},
    {"id": 3, "name": "pos-open-session", "title": "Modal de sesión", "description": "", "status": "specReady", "sdd": true},
    {"id": 4, "name": "pendiente", "title": "Algo pendiente", "status": "pending"},
    {"id": 5, "name": "sin-status", "title": "Sin status"}
  ]
}`

const stateTimes = `{
  "API-142": {"state": "documented", "since": "2026-09-15T16:46:14-0500", "slug": "ledger-pue-settlement"},
  "API-171": {"state": "inProgress", "since": "2026-09-24T10:00:00-0500", "slug": "ledger-ppd-payment-double-credit"},
  "API-120": {"state": "done", "since": "2026-08-01T10:00:00-0500", "slug": "viejo"}
}`

func writeHarness(t *testing.T, root string) {
	t.Helper()
	os.MkdirAll(filepath.Join(root, "harness", "progress"), 0o755)
	os.WriteFile(filepath.Join(root, "harness", "feature-list.json"), []byte(featureList), 0o644)
	os.WriteFile(filepath.Join(root, "harness", "progress", ".state-times.json"), []byte(stateTimes), 0o644)
}

func TestImportFeatureListIntoCreatorTracker(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	writeHarness(t, v.e.Cfg.Root)

	plan, err := v.e.Import(ctx, v.e.Cfg.Root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 3 || len(plan.Skipped) != 2 {
		t.Fatalf("dry-run: %d a importar, %d saltadas: %+v", len(plan.Tasks), len(plan.Skipped), plan)
	}
	if recs, _ := v.e.Store.List(); len(recs) != 0 {
		t.Fatal("dry-run no debe escribir")
	}

	plan, err = v.e.Import(ctx, v.e.Cfg.Root, false)
	if err != nil {
		t.Fatal(err)
	}
	byOld := map[string]ImportedTask{}
	for _, it := range plan.Tasks {
		byOld[it.From] = it
	}
	draft := byOld["2"]
	if draft.Phase != flow.Implementing {
		t.Errorf("inProgress → implementing: %+v", draft)
	}
	rec, err := v.e.Store.Load(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Flow.Phase != flow.Implementing || rec.Flow.Lane != flow.Full || rec.Flow.Slug != "quotation-prefolio-draft" || !rec.Flow.BranchCreated {
		t.Errorf("registro importado: %+v", rec.Flow)
	}
	task, _ := v.tr.Get(ctx, draft.ID)
	if task.Phase != flow.Implementing || task.Title != "Borrador pre-folio" {
		t.Errorf("tarea en el tracker: %+v", task)
	}
	spec, _ := v.e.Store.Load(byOld["3"].ID)
	if spec.Flow.Gate == nil || spec.Flow.Gate.Name != flow.GateSpec {
		t.Errorf("specReady → spec con gate: %+v", spec.Flow)
	}
	if _, ok := byOld["4"]; !ok {
		t.Error("pending se importa como tarea en backlog")
	}
	if r, err := v.e.Store.Load(byOld["4"].ID); err == nil {
		t.Errorf("una tarea en backlog no tiene registro de flujo: %+v", r)
	}

	// Idempotente: una segunda importación no duplica.
	again, err := v.e.Import(ctx, v.e.Cfg.Root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Tasks) != 0 {
		t.Errorf("reimportar no debe crear tareas nuevas: %+v", again.Tasks)
	}
}

func TestImportStateTimesForExistingTracker(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	root := v.e.Cfg.Root
	writeHarness(t, root)
	os.Remove(filepath.Join(root, "harness", "feature-list.json"))

	plan, err := v.e.ImportStateTimes(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("importadas %+v", plan)
	}
	rec, err := v.e.Store.Load("API-142")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Flow.Phase != flow.Walkthrough || rec.Flow.Slug != "ledger-pue-settlement" {
		t.Errorf("documented → walkthrough: %+v", rec.Flow)
	}
	if rec.Since.Format("2006-01-02T15:04:05-07:00") != "2026-09-15T16:46:14-05:00" {
		t.Errorf("since conserva la fecha original: %s", rec.Since)
	}
	if len(v.tr.Calls) != 0 {
		t.Errorf("importar estado no debe escribir en el tracker: %v", v.tr.Calls)
	}
}
