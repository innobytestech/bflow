// Package opencode es el adaptador de OpenCode: agentes en .opencode/agents,
// comando /bflow en la carpeta de configuración del usuario y alias de modelos.
// El plugin .opencode/plugins/bflow.js da el guard y el conteo de tokens.
package opencode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	files "innobytes.tech/bflow/adapters/opencode"
	"innobytes.tech/bflow/internal/agents"
	"innobytes.tech/bflow/internal/flow"
)

// agentsDir es donde OpenCode busca los subagentes del proyecto.
const agentsDir = ".opencode/agents"

// pluginFile es el plugin de guard y tokens que bflow render escribe.
const pluginFile = ".opencode/plugins/bflow.js"

// El plugin de OpenCode 2.x es un directorio (2.x lo carga; 1.x lo ignora).
const (
	plugin2Dir     = ".opencode/plugins/bflow2"
	plugin2File    = plugin2Dir + "/index.js"
	plugin2Package = plugin2Dir + "/package.json"
)

// PluginFiles son las rutas del plugin que bflow render escribe: 1.x y 2.x.
var PluginFiles = []string{pluginFile, plugin2File, plugin2Package}

// Agent implementa cli.ToolAdapter para OpenCode.
type Agent struct{}

func (Agent) Name() string { return "opencode" }

// ResolvesModels: OpenCode exige proveedor/modelo; los alias salen de models.opencode.
func (Agent) ResolvesModels() bool { return true }

type permission struct {
	Edit string `yaml:"edit"`
	Bash string `yaml:"bash"`
}

type frontmatter struct {
	Description string     `yaml:"description"`
	Mode        string     `yaml:"mode"`
	Model       string     `yaml:"model,omitempty"`
	Permission  permission `yaml:"permission"`
}

func allow(ok bool) string {
	if ok {
		return "allow"
	}
	return "deny"
}

// RenderAgents devuelve cada agente como subagente de OpenCode: ruta relativa → contenido.
func (Agent) RenderAgents(specs []agents.Spec) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, s := range specs {
		var write, bash bool
		for _, t := range s.Tools {
			write = write || t == agents.Write
			bash = bash || t == agents.Bash
		}
		fm, err := yaml.Marshal(frontmatter{Description: s.Description, Mode: "subagent", Model: s.Model,
			Permission: permission{Edit: allow(write), Bash: allow(bash)}})
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		b.WriteString("---\n")
		b.Write(fm)
		b.WriteString("---\n" + agents.GeneratedMark + "\n\n")
		b.WriteString(strings.TrimSpace(s.Body))
		b.WriteString("\n")
		out[agentsDir+"/"+s.Subagent+".md"] = b.Bytes()
	}
	out[pluginFile] = files.Plugin
	out[plugin2File] = files.Plugin2
	out[plugin2Package] = files.Plugin2Package
	return out, nil
}

// GeneratedAgents devuelve los subagentes que ya escribió bflow render.
func (Agent) GeneratedAgents(root string) []string {
	matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(agentsDir), flow.SubagentPrefix+"*.md"))
	var out []string
	for _, m := range matches {
		if b, err := os.ReadFile(m); err == nil && bytes.Contains(b, []byte(agents.GeneratedMark)) {
			out = append(out, agentsDir+"/"+filepath.Base(m))
		}
	}
	for _, p := range PluginFiles {
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p))); err == nil && bytes.Contains(b, []byte(agents.GeneratedMark)) {
			out = append(out, p)
		}
	}
	return out
}

// configDir es la config de OpenCode del usuario: $XDG_CONFIG_HOME/opencode
// o ~/.config/opencode. Una XDG_CONFIG_HOME relativa se rechaza para no escribir
// según el directorio actual.
func configDir(home string) (string, error) {
	var dir string
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		if !filepath.IsAbs(x) {
			return "", fmt.Errorf("XDG_CONFIG_HOME debe ser una ruta absoluta y es %q", x)
		}
		dir = x
	} else {
		if home == "" {
			return "", fmt.Errorf("no se encontró la carpeta de configuración de OpenCode: XDG_CONFIG_HOME está vacía y no hay carpeta personal")
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "opencode"), nil
}

// commandPath es <config de OpenCode>/commands/bflow.md.
func commandPath(home string) (string, error) {
	d, err := configDir(home)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "commands", "bflow.md"), nil
}

// InstallSkill escribe el comando /bflow en <config de OpenCode>/commands/bflow.md.
func (Agent) InstallSkill(home string) (string, error) {
	p, err := commandPath(home)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(p, files.Command); err != nil {
		return p, fmt.Errorf("no se pudo escribir %s: %w", p, err)
	}
	return p, nil
}

func lf(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// SkillState compara el comando instalado con el embebido.
func (Agent) SkillState(home string) (path, state string) {
	path, err := commandPath(home)
	if err != nil {
		return path, "missing"
	}
	have, err := os.ReadFile(path)
	switch {
	case err != nil:
		return path, "missing"
	case bytes.Equal(lf(have), lf(files.Command)):
		return path, "ok"
	}
	return path, "stale"
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+\S*`)

// Version corre `opencode --version`; OpenCode no exige versión mínima.
func (Agent) Version() (have, min string, ok bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "opencode", "--version").Output()
	if err != nil {
		return "", "", false, err
	}
	have = versionRe.FindString(string(out))
	if have == "" {
		return "", "", false, fmt.Errorf("versión no reconocida: %q", strings.TrimSpace(string(out)))
	}
	return have, "", true, nil
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
