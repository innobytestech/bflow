package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/tracker/trackertest"
)

// R12-R16: bflow report --stdin lee el reporte del scout de la entrada
// estándar y lo guarda; el resto de los usos se rechaza con código 2.
func TestReportStdinFlag(t *testing.T) {
	tr := trackertest.NewMemory()
	env := ghEnv(t, "tracker: { adapter: local }\nui: { notify: false }\n", tr)
	task, err := tr.Create(context.Background(), "Algo", "")
	if err != nil {
		t.Fatal(err)
	}
	if c, _, raw := runJSON(t, env, "start", task.ID, "--lane", "light"); c != 0 {
		t.Fatalf("start: %s", raw)
	}
	report := func(stdin string, args ...string) (int, string) {
		env.Stdin = strings.NewReader(stdin)
		env.Stdout = &bytes.Buffer{}
		code := Run(append([]string{"report", task.ID}, append(args, "--json")...), env)
		return code, env.Stdout.(*bytes.Buffer).String()
	}

	if code, out := report("", "--agent", "scout", "--verdict", "DONE", "--stdin"); code != 2 || !strings.Contains(out, "scout_empty") {
		t.Errorf("vacío: exit %d %s", code, out)
	}
	if code, out := report("x", "--agent", "spec-author", "--verdict", "READY", "--stdin"); code != 2 || !strings.Contains(out, "stdin_scout_only") {
		t.Errorf("otro agente: exit %d %s", code, out)
	}
	if code, out := report(strings.Repeat("- x\n", 41), "--agent", "scout", "--verdict", "DONE", "--stdin"); code != 2 || !strings.Contains(out, "scout_too_long") {
		t.Errorf("largo: exit %d %s", code, out)
	}
	// Sobre el tope de 64 KiB también es scout_too_long, sin cargar todo.
	if code, out := report(strings.Repeat("x", 70<<10), "--agent", "scout", "--verdict", "DONE", "--stdin"); code != 2 || !strings.Contains(out, "scout_too_long") {
		t.Errorf("64 KiB: exit %d %s", code, out)
	}

	const content = "- internal/flow/machine.go: start\n"
	if code, out := report(content, "--agent", "scout", "--verdict", "DONE", "--stdin"); code != 0 {
		t.Fatalf("scout: exit %d %s", code, out)
	}
	env.Stdout = &bytes.Buffer{}
	if code := Run([]string{"show", task.ID, "scout"}, env); code != 0 || env.Stdout.(*bytes.Buffer).String() != content {
		t.Errorf("show scout: exit %d %q", code, env.Stdout)
	}
}
