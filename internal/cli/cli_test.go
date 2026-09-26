package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/output"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &Env{Stdout: &out, Stderr: &errb, Version: "test"})
	return code, out.String(), errb.String()
}

func TestVersionJSON(t *testing.T) {
	code, out, _ := run(t, "version", "--json")
	if code != output.ExitOK {
		t.Fatalf("exit %d", code)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("no es JSON: %v\n%s", err, out)
	}
	if env["data"].(map[string]any)["version"] != "test" {
		t.Errorf("version: %v", env)
	}
}

func TestJSONFlagAnywhere(t *testing.T) {
	code, out, _ := run(t, "--json", "version")
	if code != 0 || !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("--json antes del subcomando: exit=%d out=%q", code, out)
	}
}

func TestUnknownCommandIsError(t *testing.T) {
	code, out, _ := run(t, "nope", "--json")
	if code != output.ExitError {
		t.Errorf("exit=%d, want 1", code)
	}
	if !strings.Contains(out, `"unknown_command"`) {
		t.Errorf("salida: %s", out)
	}
}

func TestHelpListsCommands(t *testing.T) {
	code, out, _ := run(t, "help")
	if code != 0 || !strings.Contains(out, "version") {
		t.Errorf("help: exit=%d out=%q", code, out)
	}
}
