package engine

import (
	"context"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

func TestReproHeadMergeAndMixedChecks(t *testing.T) {
	fail := map[string]string{"vet": "pass", "test": "fail"}
	pass := map[string]string{"vet": "pass", "test": "pass"}
	t.Run("HEAD es un merge: no se descarta el último commit de la lista", func(t *testing.T) {
		v, g, c, id := reproEnv(t)
		v.e.Cfg.Guard.TestPatterns = []string{"*_test.go"}
		g.commits = []string{shaTest, shaFix} // --no-merges deja fuera a HEAD
		g.files[shaTest] = []string{"internal/a_test.go"}
		g.files[shaFix] = []string{"internal/a.go"}
		c.runs[shaTest] = fail
		if out, err := doneHotfix(v, id); err != nil || out.To != flow.Quality {
			t.Fatalf("el DONE avanza: %+v %v", out, err)
		}
	})
	t.Run("el commit de pruebas que es HEAD no cuenta aunque no sea el último", func(t *testing.T) {
		v, g, c, id := reproEnv(t)
		v.e.Cfg.Guard.TestPatterns = []string{"*_test.go"}
		g.commits = []string{shaHead, shaFix}
		g.files[shaHead] = []string{"internal/a_test.go"}
		g.files[shaFix] = []string{"internal/a.go"}
		c.runs[shaHead] = fail
		rj := rejectionOf(t, func() error { _, err := doneHotfix(v, id); return err }())
		if rj.Code != "repro_test_missing" {
			t.Fatalf("código %q", rj.Code)
		}
	})
	t.Run("mezcla sin check y con check verde lista los sin check", func(t *testing.T) {
		v, g, c, id := reproEnv(t)
		v.e.Cfg.Guard.TestPatterns = []string{"*_test.go"}
		g.commits = []string{shaTest, shaTest2, shaHead}
		g.files[shaTest] = []string{"internal/a_test.go"}
		g.files[shaTest2] = []string{"internal/b_test.go"}
		c.runs[shaTest] = pass
		_, err := doneHotfix(v, id)
		rj := rejectionOf(t, err)
		for _, w := range []string{"2222222", "bflow check " + id} {
			if !strings.Contains(rj.Reason, w) {
				t.Errorf("el reason debe decir %q:\n%s", w, rj.Reason)
			}
		}
		if strings.Contains(rj.Reason, "1111111") {
			t.Errorf("el commit con check no se lista:\n%s", rj.Reason)
		}
	})
}

func TestNoReproUnavailableAfterReject(t *testing.T) {
	v, g, _, id := reproEnv(t)
	ctx := context.Background()
	m := mustT(t)
	g.commits = []string{shaHead}
	m(v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV, NoRepro: ptr("config")}))
	m(v.e.Reject(ctx, id, RejectOpts{Gate: "repro", Note: "sí se puede probar"}))
	_, err := v.e.Report(ctx, id, ReportOpts{Agent: "implementer", Verdict: flow.DoneV, NoRepro: ptr("config otra vez")})
	if rejectionOf(t, err).Code != "no_repro_rejected" {
		t.Fatalf("el escape rechazado no se reabre: %v", err)
	}
}
