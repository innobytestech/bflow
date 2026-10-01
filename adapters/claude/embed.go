package claude

import (
	_ "embed"

	"innobytes.tech/bflow/adapters/leader"
)

// Skill es la skill del leader, fuente única de bflow install claude: el
// encabezado de skill_head.md más el cuerpo común de adapters/leader.
var Skill = must(leader.Render(head, map[string]string{"ask_tool": "AskUserQuestion"}))

// head es el encabezado (frontmatter) de la skill.
//
//go:embed skill_head.md
var head []byte

func must(b []byte, err error) []byte {
	if err != nil {
		panic("adapters/claude: " + err.Error())
	}
	return b
}

// Settings son los ajustes de bflow (hooks, statusLine, permisos) que install
// fusiona en .claude/settings.json del repo.
//
//go:embed settings.json
var Settings []byte
