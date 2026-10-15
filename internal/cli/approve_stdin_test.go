package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/tracker/trackertest"
)

func approveStdinEnv(t *testing.T) (*Env, string, func(stdin string, args ...string) (int, string)) {
	t.Helper()
	tr := trackertest.NewMemory()
	env := ghEnv(t, "tracker: { adapter: local }\nui: { notify: false }\nflow: { scout: false }\n", tr)
	task, err := tr.Create(context.Background(), "Algo", "")
	if err != nil {
		t.Fatal(err)
	}
	if c, _, raw := runJSON(t, env, "start", task.ID, "--lane", "full"); c != 0 {
		t.Fatalf("start: %s", raw)
	}
	run := func(stdin string, args ...string) (int, string) {
		env.Stdin = strings.NewReader(stdin)
		env.Stdout = &bytes.Buffer{}
		code := Run(append([]string{"approve", task.ID}, append(args, "--json")...), env)
		return code, env.Stdout.(*bytes.Buffer).String()
	}
	return env, task.ID, run
}

func TestApproveDiscoveryStdin(t *testing.T) {
	env, id, approve := approveStdinEnv(t)
	if code, out := approve("x", "--gate", "discovery", "--stdin", "--file", "a.md"); code != 2 || !strings.Contains(out, "usage") {
		t.Errorf("stdin+file: exit %d %s", code, out)
	}
	if code, out := approve("# D\n- entra: borrador\n", "--gate", "discovery", "--stdin"); code != 0 {
		t.Fatalf("approve: exit %d %s", code, out)
	}
	env.Stdout = &bytes.Buffer{}
	if code := Run([]string{"show", id, "discovery"}, env); code != 0 || !strings.Contains(env.Stdout.(*bytes.Buffer).String(), "entra: borrador") {
		t.Errorf("show discovery: exit %d %q", code, env.Stdout)
	}
}

func TestApproveDiscoveryStdinTooLong(t *testing.T) {
	_, _, approve := approveStdinEnv(t)
	if code, out := approve(strings.Repeat("x", 70<<10), "--gate", "discovery", "--stdin"); code != 2 || !strings.Contains(out, "discovery_too_long") {
		t.Errorf("64 KiB: exit %d %s", code, out)
	}
}

func TestApproveStdinOtherGate(t *testing.T) {
	_, _, approve := approveStdinEnv(t)
	if code, out := approve("x", "--gate", "spec", "--stdin"); code != 2 || !strings.Contains(out, "bad_attachment") {
		t.Errorf("otro gate: exit %d %s", code, out)
	}
}
