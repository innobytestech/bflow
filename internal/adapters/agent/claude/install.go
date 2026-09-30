package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	files "innobytes.tech/bflow/adapters/claude"
	"innobytes.tech/bflow/internal/agents"
)

func skillPath(home string) string {
	return filepath.Join(home, ".claude", "skills", "bflow", "SKILL.md")
}

func lf(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// InstallSkill escribe la skill embebida en home/.claude/skills/bflow.
func (Agent) InstallSkill(home string) (string, error) {
	p := skillPath(home)
	if err := writeAtomic(p, files.Skill); err != nil {
		return p, fmt.Errorf("no se pudo escribir %s: %w", p, err)
	}
	return p, nil
}

// SkillState compara la skill instalada con la embebida.
func (Agent) SkillState(home string) (path, state string) {
	path = skillPath(home)
	have, err := os.ReadFile(path)
	switch {
	case err != nil:
		return path, "missing"
	case bytes.Equal(lf(have), lf(files.Skill)):
		return path, "ok"
	}
	return path, "stale"
}

// InstallSettings fusiona los ajustes de bflow en root/.claude/settings.json.
func (Agent) InstallSettings(root string) (agents.SettingsResult, error) {
	p := filepath.Join(root, ".claude", "settings.json")
	res := agents.SettingsResult{Path: p}
	cur, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("no se pudo leer %s: %w", p, err)
	}
	out, changed, warns, err := mergeSettings(cur, files.Settings)
	if err != nil {
		return res, fmt.Errorf("%s: %w", p, err)
	}
	res.Warnings = warns
	if !changed {
		return res, nil
	}
	if err := writeAtomic(p, out); err != nil {
		return res, fmt.Errorf("no se pudo escribir %s: %w", p, err)
	}
	res.Changed = true
	return res, nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".bflow-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, 0o644)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// node es un valor JSON que conserva el orden de las claves.
type node struct {
	kind byte // 'o' objeto, 'a' arreglo, 's' escalar
	mem  []member
	arr  []*node
	raw  string
}

type member struct {
	key string
	val *node
}

func (n *node) get(key string) *node {
	for _, m := range n.mem {
		if m.key == key {
			return m.val
		}
	}
	return nil
}

func (n *node) set(key string, v *node) {
	for i := range n.mem {
		if n.mem[i].key == key {
			n.mem[i].val = v
			return
		}
	}
	n.mem = append(n.mem, member{key, v})
}

func parseNode(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			n := &node{kind: 'o'}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := parseNode(dec)
				if err != nil {
					return nil, err
				}
				n.mem = append(n.mem, member{kt.(string), v})
			}
			_, err := dec.Token()
			return n, err
		}
		n := &node{kind: 'a'}
		for dec.More() {
			v, err := parseNode(dec)
			if err != nil {
				return nil, err
			}
			n.arr = append(n.arr, v)
		}
		_, err := dec.Token()
		return n, err
	case string:
		b, _ := json.Marshal(t)
		return &node{kind: 's', raw: string(b)}, nil
	case json.Number:
		return &node{kind: 's', raw: t.String()}, nil
	case bool:
		return &node{kind: 's', raw: fmt.Sprint(t)}, nil
	}
	return &node{kind: 's', raw: "null"}, nil
}

func parseDoc(b []byte) (*node, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	n, err := parseNode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("hay contenido después del JSON")
	}
	return n, nil
}

func (n *node) write(b *strings.Builder, depth int) {
	pad := strings.Repeat("  ", depth+1)
	end := strings.Repeat("  ", depth)
	switch n.kind {
	case 'o':
		if len(n.mem) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, m := range n.mem {
			k, _ := json.Marshal(m.key)
			b.WriteString(pad + string(k) + ": ")
			m.val.write(b, depth+1)
			if i < len(n.mem)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(end + "}")
	case 'a':
		if len(n.arr) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, v := range n.arr {
			b.WriteString(pad)
			v.write(b, depth+1)
			if i < len(n.arr)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(end + "]")
	default:
		b.WriteString(n.raw)
	}
}

// str devuelve el valor de una cadena escalar, o "" si no lo es.
func (n *node) str() string {
	if n == nil || n.kind != 's' {
		return ""
	}
	var s string
	if json.Unmarshal([]byte(n.raw), &s) != nil {
		return ""
	}
	return s
}

// onlyBflow dice si todos los comandos de una entrada de hook son de bflow.
func onlyBflow(e *node) bool {
	if e.kind != 'o' {
		return false
	}
	hs := e.get("hooks")
	if hs == nil || hs.kind != 'a' || len(hs.arr) == 0 {
		return false
	}
	for _, h := range hs.arr {
		if h.kind != 'o' || !strings.HasPrefix(h.get("command").str(), "bflow ") {
			return false
		}
	}
	return true
}

func obj(n *node, key string) *node {
	if v := n.get(key); v != nil && v.kind == 'o' {
		return v
	}
	v := &node{kind: 'o'}
	n.set(key, v)
	return v
}

// mergeSettings fusiona ours (los ajustes del binario) en cur (el archivo).
func mergeSettings(cur, ours []byte) (out []byte, changed bool, warnings []string, err error) {
	var root *node
	if len(bytes.TrimSpace(cur)) == 0 {
		root = &node{kind: 'o'}
	} else if root, err = parseDoc(cur); err != nil {
		return nil, false, nil, fmt.Errorf("no es JSON válido: %w", err)
	} else if root.kind != 'o' {
		return nil, false, nil, errors.New("la raíz no es un objeto JSON")
	}
	mine, err := parseDoc(ours)
	if err != nil || mine.kind != 'o' {
		return nil, false, nil, fmt.Errorf("ajustes embebidos inválidos: %v", err)
	}
	// hooks
	if mh := mine.get("hooks"); mh != nil && mh.kind == 'o' {
		hooks := obj(root, "hooks")
		for _, ev := range mh.mem {
			arr := hooks.get(ev.key)
			if arr == nil || arr.kind != 'a' {
				arr = &node{kind: 'a'}
				hooks.set(ev.key, arr)
			}
			kept := arr.arr[:0:0]
			for _, e := range arr.arr {
				if !onlyBflow(e) {
					kept = append(kept, e)
				}
			}
			arr.arr = append(kept, ev.val.arr...)
		}
	}
	// permisos
	if mp := mine.get("permissions"); mp != nil && mp.kind == 'o' {
		if ma := mp.get("allow"); ma != nil && ma.kind == 'a' {
			allow := obj(root, "permissions").get("allow")
			if allow == nil || allow.kind != 'a' {
				allow = &node{kind: 'a'}
				obj(root, "permissions").set("allow", allow)
			}
			for _, want := range ma.arr {
				found := false
				for _, have := range allow.arr {
					found = found || have.raw == want.raw
				}
				if !found {
					allow.arr = append(allow.arr, want)
				}
			}
		}
	}
	// statusLine
	if ms := mine.get("statusLine"); ms != nil {
		if have := root.get("statusLine"); have == nil {
			root.set("statusLine", ms)
		} else if have.kind != 'o' || !strings.HasPrefix(have.get("command").str(), "bflow ") {
			warnings = append(warnings, "tu statusLine no es de bflow y se dejó como está; para verla con bflow usa el comando `bflow statusline`")
		}
	}
	// attribution
	if root.get("attribution") == nil {
		root.set("attribution", &node{kind: 'o', mem: []member{
			{"commit", &node{kind: 's', raw: `""`}}, {"pr", &node{kind: 's', raw: `""`}}}})
	}
	var b strings.Builder
	root.write(&b, 0)
	b.WriteString("\n")
	out = []byte(b.String())
	changed = !bytes.Equal(out, lf(cur))
	return out, changed, warnings, nil
}
