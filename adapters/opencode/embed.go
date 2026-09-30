// Package opencode contiene el comando /bflow del adaptador de OpenCode que se
// instala con bflow install opencode.
package opencode

import _ "embed"

// Command es el comando /bflow: command_head.md más el cuerpo común de
// adapters/leader, con la herramienta de preguntas de OpenCode. Fuente única
// de bflow install opencode.
var Command []byte

// head es el encabezado (frontmatter) del comando.
//
//go:embed command_head.md
var head []byte
