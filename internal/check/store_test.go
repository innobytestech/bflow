package check

import (
	"os"
	"path/filepath"
	"testing"

	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/testutil"
)

func failedTestRun(sha string) Result {
	return Result{SHA: sha, Result: "FAIL", Steps: []StepResult{
		{Name: "vet", Status: "pass"}, {Name: "test", Status: "fail", Exit: 1}}}
}

func TestSaveKeepsHistoryBySHA(t *testing.T) {
	st := store.Open(testutil.TempDir(t))
	if err := Save(st, "T-1", failedTestRun("aaa111")); err != nil {
		t.Fatal(err)
	}
	pass := Result{SHA: "bbb222", Result: "PASS", Steps: []StepResult{{Name: "test", Status: "pass"}}}
	if err := Save(st, "T-1", pass); err != nil {
		t.Fatal(err)
	}
	first, err := LoadAt(st, "T-1", "aaa111")
	if err != nil || first == nil || first.Result != "FAIL" {
		t.Fatalf("el check del primer commit sigue guardado: %+v %v", first, err)
	}
	second, err := LoadAt(st, "T-1", "bbb222")
	if err != nil || second == nil || second.Result != "PASS" {
		t.Fatalf("el check del segundo commit: %+v %v", second, err)
	}
	last, err := Load(st, "T-1")
	if err != nil || last == nil || last.SHA != "bbb222" {
		t.Fatalf("check.json sigue siendo el último: %+v %v", last, err)
	}
	if missing, err := LoadAt(st, "T-1", "ccc333"); err != nil || missing != nil {
		t.Errorf("sin check guardado: %+v %v", missing, err)
	}
	p, _ := st.Path("T-1", "check.md")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("check.md se sigue escribiendo: %v", err)
	}
}

func TestSaveSkipsHistoryWhenDirty(t *testing.T) {
	root := testutil.TempDir(t)
	st := store.Open(root)
	if err := Save(st, "T-1", failedTestRun("DIRTY")); err != nil {
		t.Fatal(err)
	}
	if err := Save(st, "", failedTestRun("aaa111")); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(root, ".bflow", "tasks", "T-1", "checks"), filepath.Join(root, ".bflow", "checks")} {
		if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
			t.Errorf("%s no debe guardar historial (DIRTY o sin tarea): %v", dir, ents)
		}
	}
	if last, _ := Load(st, "T-1"); last == nil || last.SHA != "DIRTY" {
		t.Errorf("check.json sí se escribe: %+v", last)
	}
}

func TestStepAt(t *testing.T) {
	st := store.Open(testutil.TempDir(t))
	if err := Save(st, "T-1", failedTestRun("aaa111")); err != nil {
		t.Fatal(err)
	}
	v := Verifier{Store: st}
	ctx := t.Context()
	cases := []struct {
		name, sha, step string
		found           bool
		status          string
	}{
		{"paso que falló", "aaa111", "test", true, "fail"},
		{"paso que pasó", "aaa111", "vet", true, "pass"},
		{"el check no trae el paso", "aaa111", "lint", true, ""},
		{"sin check para el commit", "zzz999", "test", false, ""},
	}
	for _, c := range cases {
		found, status, err := v.StepAt(ctx, "T-1", c.sha, c.step)
		if err != nil || found != c.found || status != c.status {
			t.Errorf("%s: found=%v status=%q err=%v", c.name, found, status, err)
		}
	}
}
