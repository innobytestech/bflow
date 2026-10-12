// Package claude contiene los archivos del adaptador de Claude Code que se
// instalan con bflow install claude. Esta prueba los mantiene coherentes con bflow.
package claude_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	files "innobytes.tech/bflow/adapters/claude"
	"innobytes.tech/bflow/adapters/leader"
	"innobytes.tech/bflow/internal/cli"
)

var bflowCmd = regexp.MustCompile("`bflow ([a-z][a-z -]*)")

func commandExists(t *testing.T, mention string) {
	t.Helper()
	fields := strings.Fields(mention)
	for n := len(fields); n > 0; n-- {
		if slices.Contains(cli.Commands(), strings.Join(fields[:n], " ")) {
			return
		}
	}
	t.Errorf("el adaptador menciona `bflow %s`, que no existe", mention)
}

func TestSettings(t *testing.T) {
	b, err := os.ReadFile("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("settings.json no es JSON válido: %v", err)
	}
	want := map[string]string{"SessionStart": "bflow hook session-start", "PreToolUse": "bflow guard --reads",
		"Stop": "bflow hook tokens", "SubagentStop": "bflow hook tokens"}
	for event, cmd := range want {
		hs := s.Hooks[event]
		if len(hs) == 0 || hs[0].Hooks[0].Command != cmd {
			t.Errorf("%s debe llamar a %q: %+v", event, cmd, hs)
		}
		commandExists(t, strings.TrimPrefix(cmd, "bflow "))
	}
	if m := s.Hooks["PreToolUse"][0].Matcher; !strings.Contains(m, "Bash") || !strings.Contains(m, "Edit") || !strings.Contains(m, "Write") {
		t.Errorf("el guard debe cubrir Bash, Edit y Write: %q", m)
	}
	if s.StatusLine.Command != "bflow statusline" {
		t.Errorf("statusLine: %q", s.StatusLine.Command)
	}
}

func TestSkill(t *testing.T) {
	s := string(files.Skill)
	if !strings.HasPrefix(s, "---\nname: bflow\ndescription: ") {
		t.Error("frontmatter con name y description")
	}
	if n := strings.Count(s, "\n"); n > 45 {
		t.Errorf("la skill del leader debe ser corta (≈40 líneas): %d", n)
	}
	for _, w := range []string{"MUST", "CRITICAL", "IMPORTANT", "OBLIGATORIO", "PROHIBIDO", "NUNCA", "SIEMPRE", "❌"} {
		if strings.Contains(s, w) {
			t.Errorf("lenguaje enfático %q: las instrucciones van con su porqué", w)
		}
	}
	found := 0
	for _, m := range bflowCmd.FindAllStringSubmatch(s, -1) {
		commandExists(t, strings.TrimSpace(strings.Split(m[1], " --")[0]))
		found++
	}
	if found == 0 {
		t.Error("la skill debe invocar al menos bflow status")
	}
	for _, action := range []string{"`ask`", "`spawn`", "`wait`", "`done`"} {
		if !strings.Contains(s, action) {
			t.Errorf("la skill debe cubrir la acción %s", action)
		}
	}
	for _, skill := range []string{"## discovery", "## approve", "## walkthrough"} {
		if !strings.Contains(s, skill) {
			t.Errorf("falta la sección %s (Next.skill la nombra)", skill)
		}
	}
}

// skillBefore es el sha256 de la skill de Claude tal como salía antes de que
// compartiera cuerpo con el comando de OpenCode (git show <antes>:adapters/claude/skills/bflow/SKILL.md).
const skillBefore = "91b1f90afb8ef128b34aa9c9d65c268dff0e76907d59d9e0da78176be4f39d10"

func TestSkillFromCommonBody(t *testing.T) {
	head, err := os.ReadFile("skill_head.md")
	if err != nil {
		t.Fatal(err)
	}
	want, err := leader.Render(head, map[string]string{"ask_tool": "AskUserQuestion"})
	if err != nil {
		t.Fatalf("leader.Render: %v", err)
	}
	if string(files.Skill) != string(want) {
		t.Error("files.Skill sale de skill_head.md + el cuerpo común con AskUserQuestion")
	}
	sum := sha256.Sum256(files.Skill)
	if got := hex.EncodeToString(sum[:]); got != skillBefore {
		t.Errorf("la skill de Claude debe ser idéntica byte a byte a la de antes (R12): sha256 %s", got)
	}
	s := string(files.Skill)
	if strings.Contains(s, "{{") || strings.Contains(s, "herramienta `question`") {
		t.Error("sin marcadores ni el nombre de la herramienta de preguntas de OpenCode")
	}
}

// R17: el guard ve Read para medir las lecturas del reviewer.
func TestSettingsGuardSeesRead(t *testing.T) {
	b, err := os.ReadFile("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	pre := s.Hooks["PreToolUse"]
	if len(pre) != 1 || pre[0].Matcher != "Bash|Edit|Write|MultiEdit|NotebookEdit|Read|Task|Agent" {
		t.Fatalf("matcher de PreToolUse: %+v", pre)
	}
	if len(pre[0].Hooks) != 1 || pre[0].Hooks[0].Command != "bflow guard --reads" {
		t.Errorf("PreToolUse llama bflow guard --reads: %+v", pre[0].Hooks)
	}
}

// GH-86 R18: después de cada Bash, bflow revisa que no se haya escrito una prueba congelada.
func TestSettingsPostToolUse(t *testing.T) {
	b, err := os.ReadFile("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	post := s.Hooks["PostToolUse"]
	if len(post) != 1 || post[0].Matcher != "Bash" || len(post[0].Hooks) != 1 {
		t.Fatalf("PostToolUse con matcher Bash y un comando: %+v", post)
	}
	h := post[0].Hooks[0]
	if h.Type != "command" || h.Command != "bflow guard --post" || h.Timeout != 10 {
		t.Errorf("hook posterior: %+v", h)
	}
	commandExists(t, "guard")
}
