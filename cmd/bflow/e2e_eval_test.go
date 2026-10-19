package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// TestE2EEvalReviewer recorre un caso en un repo git real: arranque, spawn,
// review-map escrito por el "agente", report y cierre con el JSON versionable.
func TestE2EEvalReviewer(t *testing.T) {
	r, _ := gitRepo(t, "stack: go\n")
	var src strings.Builder
	src.WriteString("package a\n\n")
	for i := 0; i < 30; i++ {
		src.WriteString("// relleno\n")
	}
	os.WriteFile(filepath.Join(r.dir, "internal", "a.go"), []byte(src.String()), 0o644)
	_ = os.MkdirAll(filepath.Join(r.dir, "internal", "agents", "craft"), 0o755)
	os.WriteFile(filepath.Join(r.dir, "internal", "agents", "craft", "reviewer.md"), []byte("# oficio\nrevisa\n"), 0o644)
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "base del caso")
	base := git(t, r.dir, "rev-parse", "HEAD")

	os.WriteFile(filepath.Join(r.dir, "internal", "a.go"), []byte(src.String()+"func Bug() int { return 1 / 0 }\n"), 0o644)
	git(t, r.dir, "add", ".")
	git(t, r.dir, "commit", "-m", "head del caso")
	head := git(t, r.dir, "rev-parse", "HEAD")

	caseDir := filepath.Join(r.dir, ".bflow", "evals", "reviewer", "uno")
	_ = os.MkdirAll(caseDir, 0o755)
	os.WriteFile(filepath.Join(caseDir, "case.yaml"), []byte("source: GH-1\nbase: "+base+"\nhead: "+head+
		"\ndefects:\n  - file: internal/a.go\n    lines: \"33\"\n    what: división por cero\n"), 0o644)

	// Sin el agente generado, rechaza antes de crear nada.
	if env := r.run("eval", "reviewer"); env.exit != 2 || env.Code != "eval_agent_missing" {
		t.Fatalf("sin render: exit %d %s %v", env.exit, env.Code, env.Data)
	}
	r.ok("render")
	if _, err := os.Stat(filepath.Join(r.dir, ".claude", "agents", "bflow-eval-reviewer.md")); err != nil {
		t.Fatalf("render genera el agente del eval: %v", err)
	}

	env := r.ok("eval", "reviewer")
	if env.Next.Action != "spawn" || len(env.Next.Agents) != 1 {
		t.Fatalf("spawn: %+v", env.Next)
	}
	ag := env.Next.Agents[0]
	if ag.Agent != "eval-reviewer" || ag.Subagent != "bflow-eval-reviewer" || ag.Args["id"] != "uno" || ag.Args["base"] != base ||
		ag.Report != "bflow eval reviewer report uno --verdict APPROVED|REJECTED" {
		t.Errorf("agente: %+v", ag)
	}
	if !strings.Contains(env.Next.Reason, "bflow eval reviewer") {
		t.Errorf("reason manda a volver a correr el comando: %q", env.Next.Reason)
	}
	// El worktree es el código de head: el defecto está ahí.
	if b, err := os.ReadFile(filepath.Join(ag.Args["worktree"], "internal", "a.go")); err != nil || !strings.Contains(string(b), "1 / 0") {
		t.Fatalf("worktree en head: %v", err)
	}
	craft, _ := os.ReadFile(ag.Args["craft"])
	if string(craft) != "# oficio\nrevisa\n" {
		t.Errorf("craft: %q", craft)
	}

	// Sin review-map no hay reporte.
	if e := r.run("eval", "reviewer", "report", "uno", "--verdict", "REJECTED"); e.exit != 2 || e.Code != "review_map_missing" {
		t.Fatalf("sin review-map: %d %s", e.exit, e.Code)
	}
	os.WriteFile(filepath.Join(ag.Args["out"], "review-map.md"), []byte("# Review\n- internal/a.go:33 división por cero\n- internal/a.go:2 ruido\n"), 0o644)
	rep := r.ok("eval", "reviewer", "report", "uno", "--verdict", "REJECTED", "--note", "internal/a.go:33 rompe")
	score, _ := rep.Data["score"].(map[string]any)
	if caught, _ := score["caught"].([]any); len(caught) != 1 {
		t.Errorf("score: %v", rep.Data)
	}
	if fp, _ := score["false_positives"].([]any); len(fp) != 1 || fp[0] != "internal/a.go:2" {
		t.Errorf("falsos positivos: %v", score)
	}
	if _, err := os.Stat(ag.Args["worktree"]); !os.IsNotExist(err) {
		t.Error("el report quita el worktree")
	}
	if wl := git(t, r.dir, "worktree", "list"); strings.Contains(wl, "evals") {
		t.Errorf("git ya no conoce el worktree: %s", wl)
	}

	done := r.ok("eval", "reviewer")
	if done.Next.Action != "done" || !strings.Contains(done.Next.Reason, "atrapó 1 de 1 defectos") {
		t.Fatalf("cierre: %+v", done.Next)
	}
	res, _ := done.Data["result"].(map[string]any)
	path, _ := res["path"].(string)
	if !strings.HasPrefix(path, ".bflow/evals/") || !strings.HasSuffix(path, ".json") {
		t.Fatalf("ruta del resultado: %v", done.Data)
	}
	b, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(path)))
	if err != nil || !strings.Contains(string(b), `"kind":"reviewer"`) && !strings.Contains(string(b), `"kind": "reviewer"`) {
		t.Fatalf("resultado: %v %s", err, b)
	}
	if entries, _ := os.ReadDir(filepath.Join(r.dir, ".bflow", "cache", "evals")); len(entries) != 0 {
		t.Errorf("la corrida se borra: %v", entries)
	}
	// El resultado y los casos se pueden versionar con un git add normal.
	if gi, _ := os.ReadFile(filepath.Join(r.dir, ".bflow", ".gitignore")); !strings.Contains(string(gi), "!evals/**") {
		t.Errorf(".bflow/.gitignore: %s", gi)
	}
	// git no ignora el resultado: se versiona con un git add normal.
	if _, err := exec.Command("git", "-C", r.dir, "check-ignore", "-q", path).CombinedOutput(); err == nil {
		t.Errorf("git ignora %s", path)
	}

	// Texto para personas: sin --json, con color permitido y sin emojis.
	text, _ := r.runIn("", "eval", "reviewer", "--abort")
	if text == "" {
		t.Error("el texto de --abort dice algo")
	}
	for _, ch := range text {
		if ch > 0x2000 && !unicode.IsLetter(ch) && ch != '·' && ch != '→' && ch != '—' && ch != '…' {
			t.Errorf("la salida de texto no lleva emojis: %q", ch)
		}
	}
}
