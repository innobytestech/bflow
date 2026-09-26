package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func readGlobalMap() (map[string]any, error) {
	b, err := os.ReadFile(GlobalPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", GlobalPath(), err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// LoadProfiles lee los perfiles de la configuración global.
func LoadProfiles() (map[string]Config, error) {
	_, raw, err := readGlobal(GlobalPath())
	if err != nil {
		return nil, err
	}
	out := map[string]Config{}
	for name, m := range raw {
		b, _ := yaml.Marshal(m)
		var c Config
		if err := yaml.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("perfil %s: %w", name, err)
		}
		out[name] = c
	}
	return out, nil
}

// SaveProfile combina values en el perfil name de la configuración global.
func SaveProfile(name string, values map[string]any) error {
	if err := findSecrets(values, "profiles."+name); err != nil {
		return err
	}
	m, err := readGlobalMap()
	if err != nil {
		return err
	}
	profiles, _ := m["profiles"].(map[string]any)
	if profiles == nil {
		profiles = map[string]any{}
	}
	cur, _ := profiles[name].(map[string]any)
	profiles[name] = deepMerge(cur, values)
	m["profiles"] = profiles
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := strictDecode(b, &globalFile{}); err != nil {
		return fmt.Errorf("perfil inválido: %w", err)
	}
	if err := os.MkdirAll(GlobalDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(GlobalPath(), b, 0o644)
}

// SetRepoProfile fija `profile:` en bflow.yaml conservando comentarios y orden.
func SetRepoProfile(root, name string) error {
	path := filepath.Join(root, RepoFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, []byte("profile: "+name+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return os.WriteFile(path, []byte("profile: "+name+"\n"), 0o644)
	}
	root0 := doc.Content[0]
	if root0.Kind != yaml.MappingNode {
		return fmt.Errorf("%s no es un mapa", path)
	}
	for i := 0; i+1 < len(root0.Content); i += 2 {
		if root0.Content[i].Value == "profile" {
			root0.Content[i+1].Value = name
			return writeNode(path, &doc)
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Value: "profile"}
	val := &yaml.Node{Kind: yaml.ScalarNode, Value: name}
	root0.Content = append([]*yaml.Node{key, val}, root0.Content...)
	// El comentario de cabecera del primer campo pasa a la nueva primera clave.
	if len(root0.Content) > 2 {
		key.HeadComment, root0.Content[2].HeadComment = root0.Content[2].HeadComment, ""
	}
	return writeNode(path, &doc)
}

func writeNode(path string, n *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
