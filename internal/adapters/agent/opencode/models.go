package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/agents"
)

// userConfig es el archivo de OpenCode del usuario donde van los overrides de modelo.
const userConfig = "opencode.json"

// ModelSync es el resultado de llevar los overrides a opencode.json.
type ModelSync = agents.ModelSync

// loadUser lee opencode.json. editable=false si solo existe el .jsonc o el
// JSON no es un objeto; exists dice si opencode.json existe.
func loadUser(home string) (path string, m map[string]any, exists, editable bool, err error) {
	dir, err := configDir(home)
	if err != nil {
		return "", nil, false, false, err
	}
	path = filepath.Join(dir, userConfig)
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var v any
		if derr := dec.Decode(&v); derr != nil {
			return path, nil, true, false, nil
		}
		obj, ok := v.(map[string]any)
		if !ok || dec.More() {
			return path, nil, true, false, nil
		}
		return path, obj, true, true, nil
	case errors.Is(err, os.ErrNotExist):
		jsonc := filepath.Join(dir, "opencode.jsonc")
		if _, serr := os.Stat(jsonc); serr == nil {
			return jsonc, nil, false, false, nil
		}
		return path, map[string]any{}, false, true, nil
	}
	return path, nil, false, false, err
}

// apply lleva want a m["agent"].bflow-*.model y dice si algo cambió (R7).
func apply(m map[string]any, want map[string]string) bool {
	changed := false
	ags, _ := m["agent"].(map[string]any)
	for name, model := range want {
		if ags == nil {
			ags = map[string]any{}
			m["agent"] = ags
			changed = true
		}
		a, ok := ags[name].(map[string]any)
		if !ok {
			a = map[string]any{}
			ags[name] = a
			changed = true
		}
		if a["model"] != model {
			a["model"] = model
			changed = true
		}
	}
	for name, v := range ags {
		if !strings.HasPrefix(name, "bflow-") {
			continue
		}
		if _, keep := want[name]; keep {
			continue
		}
		a, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if _, has := a["model"]; !has {
			continue
		}
		delete(a, "model")
		changed = true
		if len(a) == 0 {
			delete(ags, name)
		}
	}
	return changed
}

func pasteBlock(want map[string]string) string {
	if len(want) == 0 {
		return ""
	}
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	ags := map[string]any{}
	for _, n := range names {
		ags[n] = map[string]string{"model": want[n]}
	}
	b, _ := json.Marshal(map[string]any{"agent": ags})
	return string(b)
}

// SyncModels lleva want a agent.<subagente>.model de opencode.json (R6-R9).
func (Agent) SyncModels(home string, want map[string]string) (ModelSync, error) {
	res := ModelSync{Want: want}
	path, m, exists, editable, err := loadUser(home)
	res.Path = path
	if err != nil {
		return res, err
	}
	if !editable {
		res.Paste = pasteBlock(want)
		return res, nil
	}
	if !apply(m, want) {
		return res, nil
	}
	if !exists && len(want) == 0 {
		return res, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return res, err
	}
	if err := writeAtomic(path, buf.Bytes()); err != nil {
		return res, err
	}
	res.Changed = true
	return res, nil
}

// ModelsState compara opencode.json con want: ok|stale|manual (R11).
func (Agent) ModelsState(home string, want map[string]string) (ModelSync, string) {
	res := ModelSync{Want: want}
	path, m, _, editable, err := loadUser(home)
	res.Path = path
	if err != nil {
		return res, "ok"
	}
	if !editable {
		res.Paste = pasteBlock(want)
		if res.Paste == "" {
			return res, "ok"
		}
		return res, "manual"
	}
	if apply(m, want) {
		return res, "stale"
	}
	return res, "ok"
}

// AgentModel es el modelo efectivo de un subagente de bflow. Source vale
// "repo" (model: del frontmatter) o "personal" (agent.<n>.model de opencode.json).
type AgentModel struct{ Agent, Model, Source string }

// agentModels calcula el modelo efectivo de cada bflow-*, ordenado por agente.
// Un agente sin modelo no entra: hereda el de la sesión.
func agentModels(root, home string) []AgentModel {
	byName := map[string]AgentModel{}
	if _, m, _, editable, err := loadUser(home); err == nil && editable {
		ags, _ := m["agent"].(map[string]any)
		for name, v := range ags {
			a, _ := v.(map[string]any)
			if s, _ := a["model"].(string); strings.HasPrefix(name, "bflow-") && s != "" {
				byName[name] = AgentModel{name, s, "personal"}
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, ".opencode", "agents", "bflow-*.md"))
	for _, f := range files {
		if model := frontmatterModel(f); model != "" {
			name := strings.TrimSuffix(filepath.Base(f), ".md")
			byName[name] = AgentModel{name, model, "repo"}
		}
	}
	out := make([]AgentModel, 0, len(byName))
	for _, am := range byName {
		out = append(out, am)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

func frontmatterModel(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			break
		}
		if strings.HasPrefix(ln, "model:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(ln, "model:")), `"'`)
		}
	}
	return ""
}

// modelsMtime es el mtime más reciente de opencode.json y de .opencode/agents/bflow-*.md.
func modelsMtime(root, home string) time.Time {
	var m time.Time
	touch := func(p string) {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(m) {
			m = fi.ModTime()
		}
	}
	if dir, err := configDir(home); err == nil {
		touch(filepath.Join(dir, userConfig))
	}
	files, _ := filepath.Glob(filepath.Join(root, ".opencode", "agents", "bflow-*.md"))
	for _, f := range files {
		touch(f)
	}
	return m
}
