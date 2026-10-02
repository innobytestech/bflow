package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/flow"
)

const scoutReportFile = "reports/scout.md"

func scoutReport(content string) ReportOpts {
	return ReportOpts{Agent: "scout", Verdict: flow.DoneV, Content: &content}
}

func rejectionCode(err error) string {
	var rj *flow.Rejection
	if errors.As(err, &rj) {
		return rj.Code
	}
	return ""
}

// scoutTask empieza una tarea full con la config por defecto: el scout está
// activo y pendiente.
func scoutTask(t *testing.T) (*env, string) {
	t.Helper()
	v := newEnv(t, "")
	id := v.task(t, "Algo que explorar")
	mustT(t)(v.e.Start(context.Background(), id, flow.Full, "", ""))
	rec, err := v.e.Store.Load(id)
	if err != nil || rec.Flow.Scout != flow.ScoutPending {
		t.Fatalf("el scout debe quedar pendiente: %+v %v", rec.Flow.Scout, err)
	}
	return v, id
}

// R12: el reporte del scout se guarda junto con el estado.
func TestReportScoutWritesFile(t *testing.T) {
	v, id := scoutTask(t)
	content := "- internal/flow/machine.go: start, enter\n- internal/flow/next.go: NextFor\n"
	o := mustT(t)(v.e.Report(context.Background(), id, scoutReport(content)))
	if o.Gate != "discovery" {
		t.Errorf("tras el scout se abre el gate de discovery: %+v", o)
	}
	b, err := v.e.Store.ReadFile(id, scoutReportFile)
	if err != nil || string(b) != content {
		t.Fatalf("scout.md: %q %v", b, err)
	}
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.Scout != flow.ScoutDone {
		t.Errorf("scout: %q", rec.Flow.Scout)
	}
	// Un rechazo del flujo no escribe nada: ya reportó.
	_, err = v.e.Report(context.Background(), id, scoutReport("otro"))
	if rejectionCode(err) != "already_reported" {
		t.Errorf("segundo reporte: %v", err)
	}
	if b, _ := v.e.Store.ReadFile(id, scoutReportFile); string(b) != content {
		t.Errorf("el rechazo no debe tocar el archivo: %q", b)
	}
}

// R13: vacío o solo espacios se rechaza sin cambiar nada.
func TestReportScoutEmpty(t *testing.T) {
	v, id := scoutTask(t)
	for _, c := range []string{"", "  \n\t\n"} {
		_, err := v.e.Report(context.Background(), id, scoutReport(c))
		if rejectionCode(err) != "scout_empty" {
			t.Errorf("%q: %v", c, err)
		}
	}
	rec, _ := v.e.Store.Load(id)
	if rec.Flow.Scout != flow.ScoutPending {
		t.Errorf("el estado no debe cambiar: %q", rec.Flow.Scout)
	}
	if _, err := v.e.Store.ReadFile(id, scoutReportFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no debe escribirse el archivo: %v", err)
	}
}

// R14: más de 40 líneas se rechaza diciendo cuántas trae.
func TestReportScoutTooLong(t *testing.T) {
	v, id := scoutTask(t)
	ok := strings.Repeat("- x\n", 40)
	long := strings.Repeat("- x\n", 41)
	_, err := v.e.Report(context.Background(), id, scoutReport(long))
	if rejectionCode(err) != "scout_too_long" || !strings.Contains(err.Error(), "41") {
		t.Fatalf("41 líneas: %v", err)
	}
	if _, err := v.e.Store.ReadFile(id, scoutReportFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no debe escribirse el archivo: %v", err)
	}
	// 40 líneas con salto final es el límite y pasa.
	if _, err := v.e.Report(context.Background(), id, scoutReport(ok)); err != nil {
		t.Errorf("40 líneas: %v", err)
	}
}

// R15: solo el scout manda --stdin.
func TestReportStdinOnlyScout(t *testing.T) {
	v, id := scoutTask(t)
	c := "texto"
	_, err := v.e.Report(context.Background(), id, ReportOpts{Agent: "spec-author", Verdict: flow.Ready, Content: &c})
	if rejectionCode(err) != "stdin_scout_only" {
		t.Errorf("%v", err)
	}
}

// R16: show scout devuelve el reporte o dice que no hay, sin error.
func TestShowScoutMissing(t *testing.T) {
	v, id := scoutTask(t)
	ctx := context.Background()
	got, err := v.e.Show(ctx, id, "scout", "")
	if err != nil || got != "("+id+" no tiene reporte del scout)" {
		t.Errorf("sin reporte: %q %v", got, err)
	}
	mustT(t)(v.e.Report(ctx, id, scoutReport("- a.go\n")))
	got, err = v.e.Show(ctx, id, "scout", "")
	if err != nil || got != "- a.go\n" {
		t.Errorf("con reporte: %q %v", got, err)
	}
}

// R17: el scout que termina sin reportar recibe el aviso con --stdin y tiene
// el mismo límite de avisos que los demás.
func TestNudgeScout(t *testing.T) {
	v, id := scoutTask(t)
	ctx := context.Background()
	for i := 1; i <= MaxNudges; i++ {
		reason, err := v.e.Nudge(ctx, "scout")
		if err != nil || !strings.Contains(reason, "bflow report "+id+" --agent scout --verdict DONE --stdin") {
			t.Fatalf("aviso %d: %q %v", i, reason, err)
		}
	}
	if reason, err := v.e.Nudge(ctx, "scout"); err != nil || reason != "" {
		t.Fatalf("agotados los avisos: %q %v", reason, err)
	}
	if got := phaseOf(t, v, id); got != flow.Blocked {
		t.Errorf("la tarea debe quedar bloqueada: %s", got)
	}
	// Con el scout ya reportado no hay aviso.
	v2, id2 := scoutTask(t)
	mustT(t)(v2.e.Report(ctx, id2, scoutReport("- a.go\n")))
	if reason, _ := v2.e.Nudge(ctx, "scout"); reason != "" {
		t.Errorf("ya reportó: %q", reason)
	}
}
