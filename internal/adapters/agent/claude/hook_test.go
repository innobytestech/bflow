package claude

import (
	"testing"

	"innobytes.tech/bflow/internal/guard"
)

func TestParsePreToolUse(t *testing.T) {
	cases := []struct {
		in   string
		want guard.Action
		ok   bool
	}{
		{`{"hook_event_name":"PreToolUse","cwd":"C:\\repo","tool_name":"Bash","tool_input":{"command":"git push -f","description":"x"}}`,
			guard.Action{Tool: guard.Bash, Command: "git push -f"}, true},
		{`{"tool_name":"Edit","tool_input":{"file_path":"C:\\repo\\internal\\a_test.go","old_string":"a","new_string":"b"},"agent_type":"implementer"}`,
			guard.Action{Tool: guard.Edit, Path: `C:\repo\internal\a_test.go`, Subagent: true, Agent: "implementer"}, true},
		{`{"tool_name":"MultiEdit","tool_input":{"file_path":"x.go","edits":[]}}`, guard.Action{Tool: guard.Edit, Path: "x.go"}, true},
		{`{"tool_name":"Write","tool_input":{"file_path":".env","content":"S=1"}}`, guard.Action{Tool: guard.Write, Path: ".env"}, true},
		{`{"tool_name":"NotebookEdit","tool_input":{"notebook_path":"n.ipynb"}}`, guard.Action{Tool: guard.Edit, Path: "n.ipynb"}, true},
		{`{"tool_name":"Grep","tool_input":{"pattern":"x"}}`, guard.Action{}, true},
		{`{"tool":"bash","command":"ls"}`, guard.Action{}, false},
	}
	for _, c := range cases {
		a, cwd, ok := ParsePreToolUse([]byte(c.in))
		if ok != c.ok || a != c.want {
			t.Errorf("%s\n got %+v ok=%v, want %+v ok=%v", c.in, a, ok, c.want, c.ok)
		}
		if c.in[2:15] == "hook_event_na" && cwd != `C:\repo` {
			t.Errorf("cwd: %q", cwd)
		}
	}
}

func TestParsePreToolUseAgent(t *testing.T) {
	a, _, ok := ParsePreToolUse([]byte(`{"tool_name":"Bash","tool_input":{"command":"bflow approve X"},"agent_type":"bflow-documenter"}`))
	if !ok || a.Agent != "bflow-documenter" || !a.Subagent || a.Command != "bflow approve X" {
		t.Errorf("con agent_type: %+v ok=%v", a, ok)
	}
	a, _, _ = ParsePreToolUse([]byte(`{"tool_name":"Bash","tool_input":{"command":"bflow approve X"}}`))
	if a.Agent != "" || a.Subagent {
		t.Errorf("sin agent_type: %+v", a)
	}
}

// R1: Read llega al guard como acción propia, con el agente que la hizo.
func TestParsePreToolUseRead(t *testing.T) {
	a, cwd, ok := ParsePreToolUse([]byte(`{"hook_event_name":"PreToolUse","cwd":"C:\\repo","tool_name":"Read","tool_input":{"file_path":"C:\\repo\\internal\\a.go","offset":10,"limit":50},"agent_type":"bflow-reviewer","agent_id":"abc"}`))
	want := guard.Action{Tool: guard.Read, Path: `C:\repo\internal\a.go`, Subagent: true, Agent: "bflow-reviewer"}
	if !ok || a != want || cwd != `C:\repo` {
		t.Errorf("reviewer: %+v cwd=%q ok=%v, quiero %+v", a, cwd, ok, want)
	}
	a, _, ok = ParsePreToolUse([]byte(`{"tool_name":"Read","tool_input":{"file_path":"x.go"}}`))
	if !ok || a != (guard.Action{Tool: guard.Read, Path: "x.go"}) {
		t.Errorf("sesión principal: %+v ok=%v", a, ok)
	}
	if d := guard.Evaluate(a, guard.Context{}); !d.Allow {
		t.Errorf("el guard permite Read: %+v", d)
	}
}
