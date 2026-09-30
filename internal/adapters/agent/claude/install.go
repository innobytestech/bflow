package claude

import (
	"errors"

	"innobytes.tech/bflow/internal/agents"
)

var errTodo = errors.New("no implementado")

// InstallSkill escribe la skill embebida en home/.claude/skills/bflow.
func (Agent) InstallSkill(home string) (string, error) { return "", errTodo }

// SkillState compara la skill instalada con la embebida.
func (Agent) SkillState(home string) (path, state string) { return "", "" }

// InstallSettings fusiona los ajustes de bflow en root/.claude/settings.json.
func (Agent) InstallSettings(root string) (agents.SettingsResult, error) {
	return agents.SettingsResult{}, errTodo
}

// mergeSettings fusiona ours (los ajustes del binario) en cur (el archivo).
func mergeSettings(cur, ours []byte) (out []byte, changed bool, warnings []string, err error) {
	return nil, false, nil, errTodo
}
