// Package claude es el adaptador de Claude Code: traduce la entrada de sus
// hooks a acciones neutrales de bflow y lee sus transcripts.
package claude

import (
	"encoding/json"

	"innobytes.tech/bflow/internal/guard"
)

type hookInput struct {
	ToolName  string `json:"tool_name"`
	Cwd       string `json:"cwd"`
	AgentType string `json:"agent_type"`
	AgentID   string `json:"agent_id"`
	ToolInput struct {
		Command      string `json:"command"`
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	} `json:"tool_input"`
}

// ParsePreToolUse traduce el JSON de un hook PreToolUse. ok es false si la
// entrada no tiene la forma de Claude Code. Una herramienta que al guard no le
// importa (Read, Grep…) devuelve una acción vacía, que se permite.
func ParsePreToolUse(raw []byte) (a guard.Action, cwd string, ok bool) {
	var in hookInput
	if json.Unmarshal(raw, &in) != nil || in.ToolName == "" {
		return a, "", false
	}
	sub := in.AgentType != "" || in.AgentID != ""
	switch in.ToolName {
	case "Bash":
		a = guard.Action{Tool: guard.Bash, Command: in.ToolInput.Command, Subagent: sub}
	case "Edit", "MultiEdit":
		a = guard.Action{Tool: guard.Edit, Path: in.ToolInput.FilePath, Subagent: sub}
	case "Write":
		a = guard.Action{Tool: guard.Write, Path: in.ToolInput.FilePath, Subagent: sub}
	case "NotebookEdit":
		a = guard.Action{Tool: guard.Edit, Path: in.ToolInput.NotebookPath, Subagent: sub}
	}
	return a, in.Cwd, true
}
