package opencode

import (
	"encoding/json"

	"innobytes.tech/bflow/internal/guard"
)

// guardInput es lo que el plugin le pasa por stdin a `bflow guard --tool opencode`.
type guardInput struct {
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	SessionID string          `json:"sessionID"`
	Agent     string          `json:"agent"`
	Cwd       string          `json:"cwd"`
	Subagent  bool            `json:"subagent"`
}

// ParseActions traduce la entrada del plugin en acciones del guard (T2).
func (Agent) ParseActions(raw []byte) ([]guard.Action, string, bool) { return nil, "", false }

// patchPaths separa las rutas de un patch en escrituras y ediciones (T2).
func patchPaths(text string) (writes, edits []string) { return nil, nil }
