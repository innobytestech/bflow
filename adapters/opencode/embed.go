// Package opencode contiene el comando /bflow del adaptador de OpenCode que se
// instala con bflow install opencode.
package opencode

import (
	_ "embed"

	"innobytes.tech/bflow/adapters/leader"
)

// Command es el comando /bflow: command_head.md más el cuerpo común de
// adapters/leader, con la herramienta de preguntas de OpenCode. Fuente única
// de bflow install opencode.
var Command = must(leader.Render(head, map[string]string{"ask_tool": "la herramienta `question`"}))

// head es el encabezado (frontmatter) del comando.
//
//go:embed command_head.md
var head []byte

// Plugin es el plugin de OpenCode que bflow render escribe en
// .opencode/plugins/bflow.js: guard y conteo de tokens.
//
//go:embed plugin.js
var Plugin []byte

func must(b []byte, err error) []byte {
	if err != nil {
		panic("adapters/opencode: " + err.Error())
	}
	return b
}

// Plugin2 es el plugin de OpenCode 2.x que bflow render escribe en
// .opencode/plugins/bflow2/index.js.
//
//go:embed plugin2/index.js
var Plugin2 []byte

// Plugin2Package es el package.json que acompaña a Plugin2 en bflow2/.
//
//go:embed plugin2/package.json
var Plugin2Package []byte
