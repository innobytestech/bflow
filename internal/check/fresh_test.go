package check

import (
	"context"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/testutil"
)

// Fresh mira la vigencia del check, no su resultado: un FAIL sobre el código
// actual es vigente.
func TestFreshIgnoresResult(t *testing.T) {
	ctx := context.Background()
	paths := []string{"internal"}
	cases := []struct {
		name  string
		res   *Result
		git   fakeGit
		fresh bool
		want  string
	}{
		{"sin check", nil, fakeGit{}, false, "no hay check"},
		{"corrió sucio", &Result{SHA: "DIRTY", Result: "FAIL"}, fakeGit{}, false, "sin commitear"},
		{"código sucio después", &Result{SHA: "abc", Result: "FAIL"}, fakeGit{dirty: []string{"internal/a.go"}}, false, "internal/a.go"},
		{"cambios desde el SHA", &Result{SHA: "abc", Result: "FAIL"}, fakeGit{changed: []string{"internal/b.go"}}, false, "internal/b.go"},
		{"FAIL vigente", &Result{SHA: "abc12345678", Result: "FAIL"}, fakeGit{}, true, "abc12345"},
		{"PASS vigente", &Result{SHA: "abc12345678", Result: "PASS"}, fakeGit{}, true, "abc12345"},
	}
	for _, c := range cases {
		g := c.git
		ok, detail := Fresh(ctx, &g, paths, c.res)
		if ok != c.fresh || !strings.Contains(detail, c.want) {
			t.Errorf("%s: fresh=%v detail=%q", c.name, ok, detail)
		}
	}
}

func TestVerifierLast(t *testing.T) {
	ctx := context.Background()
	st := store.Open(testutil.TempDir(t))
	v := Verifier{Store: st, Git: &fakeGit{}, CodePaths: []string{"internal"}}

	res, fresh, _, err := v.Last(ctx, "T-1")
	if err != nil || res != nil || fresh {
		t.Fatalf("sin check: %+v %v %v", res, fresh, err)
	}
	if err := Save(st, "T-1", failedTestRun("abc12345678")); err != nil {
		t.Fatal(err)
	}
	res, fresh, _, err = v.Last(ctx, "T-1")
	if err != nil || res == nil || res.Result != "FAIL" || !fresh {
		t.Fatalf("FAIL vigente: %+v %v %v", res, fresh, err)
	}
	v.Git = &fakeGit{changed: []string{"internal/x.go"}}
	res, fresh, detail, _ := v.Last(ctx, "T-1")
	if res == nil || fresh || !strings.Contains(detail, "internal/x.go") {
		t.Fatalf("con cambios posteriores: %+v %v %q", res, fresh, detail)
	}
	// Verify sigue exigiendo PASS aunque el check sea vigente.
	v.Git = &fakeGit{}
	if ok, _, _ := v.Verify(ctx, "T-1"); ok {
		t.Error("Verify no debe aceptar un check en FAIL")
	}
}
