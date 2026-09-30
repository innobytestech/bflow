// Package opencode es el adaptador de OpenCode: agentes en .opencode/agents,
// comando /bflow en la carpeta de configuración del usuario y alias de modelos.
// Hooks, guard y tokens de OpenCode llegan con GH-14.
package opencode

import (
	"errors"

	"innobytes.tech/bflow/internal/agents"
)

var errTodo = errors.New("opencode: sin implementar")

// agentsDir es donde OpenCode busca los subagentes del proyecto.
const agentsDir = ".opencode/agents"

// Agent implementa cli.ToolAdapter para OpenCode.
type Agent struct{}

func (Agent) Name() string { return "opencode" }

// ResolvesModels: OpenCode exige proveedor/modelo; los alias salen de models.opencode.
func (Agent) ResolvesModels() bool { return true }

// RenderAgents devuelve cada agente como subagente de OpenCode: ruta relativa → contenido.
func (Agent) RenderAgents(specs []agents.Spec) (map[string][]byte, error) { return nil, errTodo }

// GeneratedAgents devuelve los subagentes que ya escribió bflow render.
func (Agent) GeneratedAgents(root string) []string { return nil }

// InstallSkill escribe el comando /bflow en <config de OpenCode>/commands/bflow.md.
func (Agent) InstallSkill(home string) (string, error) { return "", errTodo }

// SkillState compara el comando instalado con el embebido.
func (Agent) SkillState(home string) (path, state string) { return "", "missing" }

// Version corre `opencode --version`; OpenCode no exige versión mínima.
func (Agent) Version() (have, min string, ok bool, err error) { return "", "", false, errTodo }
