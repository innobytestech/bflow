package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hook corre bflow con stdin como lo hace Claude Code y devuelve stdout, stderr y exit.
func (r *repo) hook(stdin string, args ...string) (string, string, int) {
	r.t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = r.dir
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

func preToolUse(dir, tool string, input map[string]any) string {
	b, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "cwd": dir, "tool_name": tool, "tool_input": input})
	return string(b)
}

func TestGuardHook(t *testing.T) {
	r, _ := gitRepo(t, "stack: go\nvcs: { base_branch: dev }\nguard: { forbid_coauthor: true }\n")
	out, errOut, code := r.hook(preToolUse(r.dir, "Bash", map[string]any{"command": "go test ./..."}), "guard")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("permitido debe ser silencioso: %d %q %q", code, out, errOut)
	}
	_, errOut, code = r.hook(preToolUse(r.dir, "Bash", map[string]any{"command": "git push origin dev"}), "guard")
	if code != 2 || !strings.Contains(errOut, "dev") {
		t.Fatalf("push a dev: %d %q", code, errOut)
	}
	_, errOut, code = r.hook(preToolUse(r.dir, "Write", map[string]any{"file_path": filepath.Join(r.dir, ".env")}), "guard")
	if code != 2 || !strings.Contains(errOut, ".env") {
		t.Fatalf(".env: %d %q", code, errOut)
	}

	// Prueba congelada: la tarea pasa por el contrato con una prueba escrita.
	id := r.ok("task", "add", "Demo").Data["id"].(string)
	r.ok("start", id, "--lane", "full")
	os.WriteFile(filepath.Join(r.dir, "d.md"), []byte("d"), 0o644)
	r.ok("approve", id, "--file", "d.md")
	r.ok("report", id, "--agent", "spec-author", "--verdict", "READY")
	r.ok("approve", id)
	os.WriteFile(filepath.Join(r.dir, "internal", "cas_test.go"), []byte("package a\n"), 0o644)
	r.ok("report", id, "--agent", "implementer", "--verdict", "CONTRACT_READY")
	r.ok("approve", id)
	edit := preToolUse(r.dir, "Edit", map[string]any{"file_path": filepath.Join(r.dir, "internal", "cas_test.go"), "old_string": "a", "new_string": "b"})
	_, errOut, code = r.hook(edit, "guard")
	if code != 2 || !strings.Contains(errOut, "congelada") {
		t.Fatalf("prueba congelada: %d %q", code, errOut)
	}
	_, _, code = r.hook(preToolUse(r.dir, "Edit", map[string]any{"file_path": filepath.Join(r.dir, "internal", "cas.go")}), "guard")
	if code != 0 {
		t.Errorf("el código sí se edita: %d", code)
	}
	_, errOut, code = r.hook(preToolUse(r.dir, "Bash", map[string]any{"command": "git checkout -b mi-rama"}), "guard")
	if code != 2 || !strings.Contains(errOut, "la rama de la tarea la crea bflow") {
		t.Errorf("con tarea en curso la rama la crea bflow: %d %q", code, errOut)
	}

	out, _, code = r.hook(`{"source":"startup"}`, "hook", "session-start")
	if code != 0 || !strings.Contains(out, id) || strings.Count(out, "\n") > 10 {
		t.Errorf("session-start: %d\n%s", code, out)
	}
}

// Sin hooks (otra herramienta, o un agente que se los salta), el cambio a una
// prueba congelada se detecta al reportar DONE.
func TestFrozenTestsWithoutHooks(t *testing.T) {
	r, _ := gitRepo(t, "vcs: { base_branch: dev }\nguard: { test_patterns: [\"*_test.go\"] }\n")
	id := r.ok("task", "add", "Demo").Data["id"].(string)
	r.ok("start", id, "--lane", "full")
	os.WriteFile(filepath.Join(r.dir, "d.md"), []byte("d"), 0o644)
	r.ok("approve", id, "--file", "d.md")
	r.ok("report", id, "--agent", "spec-author", "--verdict", "READY")
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "spec")
	r.ok("approve", id)
	test := filepath.Join(r.dir, "internal", "cas_test.go")
	os.WriteFile(test, []byte("package a\n"), 0o644)
	r.ok("report", id, "--agent", "implementer", "--verdict", "CONTRACT_READY")
	r.ok("approve", id)

	os.WriteFile(test, []byte("package a // debilitada\n"), 0o644)
	env := r.run("report", id, "--agent", "implementer", "--verdict", "DONE")
	if env.exit != 2 || env.Code != "frozen_changed" {
		t.Fatalf("DONE con una prueba congelada cambiada: exit %d code %s", env.exit, env.Code)
	}

	// Aceptar el cambio lo decide una persona: el agente no puede.
	if _, _, code := r.hook(preToolUse(r.dir, "Bash", map[string]any{"command": "bflow freeze " + id}), "guard"); code != 2 {
		t.Errorf("guard debe bloquear bflow freeze desde el agente: %d", code)
	}
	if n := r.ok("freeze", id).Data["files"].(float64); n != 1 {
		t.Errorf("freeze: %v archivos", n)
	}
	os.WriteFile(test, []byte("package a // debilitada\r\n"), 0o644) // autocrlf no cuenta como cambio
	// Aceptado el cambio, lo que falta es commitearlo: sin commit no entraría al PR.
	if env := r.run("report", id, "--agent", "implementer", "--verdict", "DONE"); env.Code != "uncommitted" || !strings.Contains(fmt.Sprint(env.Data), "internal/cas_test.go") {
		t.Fatalf("DONE sin commit: %s %v", env.Code, env.Data)
	}
	git(t, r.dir, "add", "internal")
	git(t, r.dir, "commit", "-m", "prueba")
	r.ok("report", id, "--agent", "implementer", "--verdict", "DONE")
}
