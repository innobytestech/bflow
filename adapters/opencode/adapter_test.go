// Package opencode contiene el comando /bflow del adaptador de OpenCode. Esta
// prueba lo mantiene coherente con bflow y con el cuerpo común de la skill.
package opencode_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"innobytes.tech/bflow/adapters/leader"
	files "innobytes.tech/bflow/adapters/opencode"
	"innobytes.tech/bflow/internal/cli"
)

// askTool es cómo el comando nombra la herramienta de preguntas de OpenCode.
const askTool = "la herramienta `question`"

var bflowCmd = regexp.MustCompile("`bflow ([a-z][a-z -]*)")

func commandExists(t *testing.T, mention string) {
	t.Helper()
	fields := strings.Fields(mention)
	for n := len(fields); n > 0; n-- {
		if slices.Contains(cli.Commands(), strings.Join(fields[:n], " ")) {
			return
		}
	}
	t.Errorf("el comando menciona `bflow %s`, que no existe", mention)
}

func TestCommandFromCommonBody(t *testing.T) {
	head, err := os.ReadFile("command_head.md")
	if err != nil {
		t.Fatal(err)
	}
	want, err := leader.Render(head, map[string]string{"ask_tool": askTool})
	if err != nil {
		t.Fatalf("leader.Render: %v", err)
	}
	if string(files.Command) != string(want) || len(want) == 0 {
		t.Fatalf("Command sale de command_head.md + el cuerpo común con %s", askTool)
	}
	s := string(files.Command)
	if !strings.HasPrefix(s, "---\ndescription: ") {
		t.Error("frontmatter con description")
	}
	if !strings.Contains(s, "!`bflow status $ARGUMENTS --json`") {
		t.Error("el comando inyecta la salida de bflow status (R13)")
	}
	if strings.Contains(s, "{{") || strings.Contains(s, "AskUserQuestion") {
		t.Error("sin marcadores ni el nombre de la herramienta de preguntas de Claude")
	}
	if !strings.Contains(s, "Pregunta con "+askTool) {
		t.Error("las preguntas con opciones usan la herramienta question de OpenCode")
	}
	found := 0
	for _, m := range bflowCmd.FindAllStringSubmatch(s, -1) {
		commandExists(t, strings.TrimSpace(strings.Split(m[1], " --")[0]))
		found++
	}
	if found == 0 {
		t.Error("el comando debe invocar al menos bflow status")
	}
	for _, action := range []string{"`ask`", "`spawn`", "`wait`", "`done`"} {
		if !strings.Contains(s, action) {
			t.Errorf("el comando debe cubrir la acción %s", action)
		}
	}
	for _, section := range []string{"## discovery", "## approve", "## walkthrough", "## intake"} {
		if !strings.Contains(s, section) {
			t.Errorf("falta la sección %s (Next.skill la nombra)", section)
		}
	}
}
