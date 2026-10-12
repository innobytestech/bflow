package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
