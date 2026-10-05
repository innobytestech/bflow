package opencode

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

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

// ParseActions traduce la entrada del plugin en acciones del guard. ok=false si
// no trae tool; una herramienta que no le importa al guard da cero acciones.
func (Agent) ParseActions(raw []byte) ([]guard.Action, string, bool) {
	var in guardInput
	if json.Unmarshal(raw, &in) != nil || in.Tool == "" {
		return nil, "", false
	}
	var args struct {
		Command   string          `json:"command"`
		FilePath  string          `json:"filePath"`
		PatchText string          `json:"patchText"`
		Offset    json.RawMessage `json:"offset"`
		Limit     json.RawMessage `json:"limit"`
	}
	_ = json.Unmarshal(in.Args, &args)
	mk := func(tool, path string) guard.Action {
		a := guard.Action{Tool: tool, Subagent: in.Subagent, Agent: in.Agent}
		if path != "" {
			a.Path = path
			if !filepath.IsAbs(path) && in.Cwd != "" {
				a.Path = filepath.Join(in.Cwd, path)
			}
		}
		return a
	}
	var acts []guard.Action
	switch in.Tool {
	case "bash":
		a := mk(guard.Bash, "")
		a.Command = args.Command
		acts = append(acts, a)
	case "edit", "multiedit":
		acts = append(acts, mk(guard.Edit, args.FilePath))
	case "read":
		a := mk(guard.Read, args.FilePath)
		a.Partial = present(args.Offset) || present(args.Limit)
		acts = append(acts, a)
	case "write":
		acts = append(acts, mk(guard.Write, args.FilePath))
	case "patch", "apply_patch":
		writes, edits := patchPaths(args.PatchText)
		for _, w := range writes {
			acts = append(acts, mk(guard.Write, w))
		}
		for _, e := range edits {
			acts = append(acts, mk(guard.Edit, e))
		}
	}
	return acts, in.Cwd, true
}

// patchPaths separa las rutas de un patch en escrituras (Add) y ediciones
// (Update, Delete, Move to), cada grupo en el orden del texto.
func patchPaths(text string) (writes, edits []string) {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, "*** Add File:"):
			writes = append(writes, strings.TrimSpace(strings.TrimPrefix(l, "*** Add File:")))
		case strings.HasPrefix(l, "*** Update File:"):
			edits = append(edits, strings.TrimSpace(strings.TrimPrefix(l, "*** Update File:")))
		case strings.HasPrefix(l, "*** Delete File:"):
			edits = append(edits, strings.TrimSpace(strings.TrimPrefix(l, "*** Delete File:")))
		case strings.HasPrefix(l, "*** Move to:"):
			edits = append(edits, strings.TrimSpace(strings.TrimPrefix(l, "*** Move to:")))
		}
	}
	return writes, edits
}

// present dice si un valor JSON existe y no es null.
func present(m json.RawMessage) bool {
	t := bytes.TrimSpace(m)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}
