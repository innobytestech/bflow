package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoneRequiresVigentCheck(t *testing.T) {
	yaml := "stack: go\ncheck:\n  steps:\n    - { name: vet, run: \"go vet ./...\" }\n    - { name: test, run: \"go test -count=1 ./...\" }\n"
	r, _ := gitRepo(t, yaml)
	os.WriteFile(filepath.Join(r.dir, "go.mod"), []byte("module demo\n\ngo 1.21\n"), 0o644)
	os.WriteFile(filepath.Join(r.dir, "internal", "a_test.go"), []byte("package a\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { t.Fatal(\"rojo\") }\n"), 0o644)
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "base")
	id := r.ok("task", "add", "Demo").Data["id"].(string)
	r.ok("start", id, "--lane", "hotfix")

	env := r.run("report", "--agent", "implementer", "--verdict", "DONE")
	if env.exit != 2 || env.Code != "check_required" || !strings.Contains(env.Data["reason"].(string), "no hay check") {
		t.Fatalf("sin check: %d %s %v", env.exit, env.Code, env.Data)
	}
	env = r.run("check")
	if env.exit != 1 || env.Data["result"] != "FAIL" || !strings.Contains(strings.Join([]string{env.Data["failed"].([]any)[0].(map[string]any)["tail"].(string)}, ""), "rojo") {
		t.Fatalf("check rojo: %d %v", env.exit, env.Data)
	}
	if env = r.run("report", "--agent", "implementer", "--verdict", "DONE"); env.Code != "check_required" {
		t.Fatalf("con check rojo: %v", env.Data)
	}

	os.WriteFile(filepath.Join(r.dir, "internal", "a_test.go"), []byte("package a\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"), 0o644)
	if env = r.run("check"); env.Data["sha"] != "DIRTY" {
		t.Fatalf("sin commitear el sha es DIRTY: %v", env.Data)
	}
	git(t, r.dir, "commit", "-am", "fix")
	if env = r.ok("check"); env.Data["result"] != "PASS" {
		t.Fatalf("check verde: %v", env.Data)
	}
	if v := r.ok("check", "--verify"); v.Data["ok"] != true {
		t.Fatalf("verify: %v", v.Data)
	}
	if md := r.ok("show", id, "check").Data["content"].(string); !strings.Contains(md, "result: PASS") {
		t.Errorf("show check:\n%s", md)
	}

	// Cambio de código después del check: DONE vuelve a rechazarse.
	os.WriteFile(filepath.Join(r.dir, "internal", "b.go"), []byte("package a\n"), 0o644)
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "otro")
	env = r.run("report", "--agent", "implementer", "--verdict", "DONE")
	if env.Code != "check_required" || !strings.Contains(env.Data["reason"].(string), "internal/b.go") {
		t.Fatalf("cambios posteriores: %v", env.Data)
	}
	r.ok("check")
	if env = r.ok("report", "--agent", "implementer", "--verdict", "DONE"); env.Data["phase"] != "quality" {
		t.Fatalf("con check vigente avanza: %v", env.Data)
	}
}

// env_first exige lo que usan las pruebas (puertos y variables), no el API.
func TestCheckEnvFirstIgnoresAPIHealth(t *testing.T) {
	r, _ := gitRepo(t, `check:
  env_first: true
  steps: [{ name: ok, run: "go version" }]
env:
  api_url: http://127.0.0.1:1
  health_paths: [/api/health]
  require_env: { BFLOW_TEST_DSN: ":5433" }
`)
	t.Setenv("BFLOW_TEST_DSN", "postgres://localhost:5432/dev")
	if env := r.run("check"); env.Code != "env_not_ready" {
		t.Errorf("con la variable apuntando a otro lado el check no corre: %s", env.Code)
	}
	t.Setenv("BFLOW_TEST_DSN", "postgres://localhost:5433/test")
	if env := r.run("check"); env.Code == "env_not_ready" {
		t.Errorf("el API caído no debe impedir el check: %v", env.Data)
	}
}
