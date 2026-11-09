package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/check"
	"innobytes.tech/bflow/internal/flow"
)

const navSpec = "src/app/navbar/navbar.component.spec.ts"

// El DONE avisa y deja en el log las pruebas que el check no cobró porque ya
// fallaban en la base.
func TestDoneWarnsAndLogsTestsAlreadyRedInBase(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Hotfix, "", ""))
	rr := &check.Rerun{Base: "base0123456789", Files: []string{navSpec}, Ignored: []string{navSpec + " > Navbar > a", navSpec + " > Navbar > b"}}
	v.e.Contract = &fakeContract{fresh: true, res: &check.Result{SHA: "abc", Result: "PASS", Steps: []check.StepResult{
		{Name: "vet", Status: "pass"}, {Name: "test", Status: "pass", Exit: 1, Rerun: rr},
	}}}
	v.ver.ok = true
	out, err := v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(out.Warnings, func(w string) bool {
		return strings.Contains(w, `paso "test"`) && strings.Contains(w, "2 prueba(s) que ya fallaban en la base base0123") && strings.Contains(w, "Navbar > b")
	}) {
		t.Fatalf("el DONE debe avisar lo ignorado: %q", out.Warnings)
	}
	log, err := v.e.Store.Log(id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, en := range log {
		red, ok := en.Data["preexisting_tests"].(map[string]any)
		if !ok {
			continue
		}
		step, _ := red["test"].(map[string]any)
		tests, _ := step["tests"].([]any)
		if step["base"] == "base0123456789" && len(tests) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("log.jsonl debe guardar lo ignorado: %+v", log)
	}
}

// Sin rojo ignorado, el DONE no avisa nada nuevo.
func TestDoneWithoutRerunHasNoPreexistingNote(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Hotfix, "", ""))
	v.e.Contract = &fakeContract{fresh: true, res: &check.Result{SHA: "abc", Result: "PASS", Steps: []check.StepResult{{Name: "test", Status: "pass"}}}}
	v.ver.ok = true
	out, err := v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(out.Warnings, func(w string) bool { return strings.Contains(w, "ya fallaban en la base") }) {
		t.Fatalf("%q", out.Warnings)
	}
}

// Si el rojo no contó como de la base, el rechazo del DONE dice por qué.
func TestDoneRejectionSaysWhyRedIsNotFromBase(t *testing.T) {
	v := newEnv(t, "")
	ctx := context.Background()
	id := v.task(t, "Demo")
	mustT(t)(v.e.Start(ctx, id, flow.Hotfix, "", ""))
	res := &check.Result{SHA: "abc", Result: "FAIL", Steps: []check.StepResult{{Name: "test", Status: "fail", Rerun: &check.Rerun{Why: "«x > a» falla en la rama y no en la base base0123"}}}}
	_, detail := check.Verify(ctx, nil, nil, res)
	v.ver.ok, v.ver.detail = false, detail
	_, err := v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV})
	var rj *flow.Rejection
	if !errors.As(err, &rj) || rj.Code != "check_required" || !strings.Contains(rj.Reason, "falla en la rama y no en la base") {
		t.Fatalf("%v", err)
	}
}
