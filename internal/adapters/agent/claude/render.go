package claude

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"innobytes.tech/bflow/internal/agents"
	"innobytes.tech/bflow/internal/flow"
)

// agentsDir es donde Claude Code busca los subagentes del proyecto.
const agentsDir = ".claude/agents"

// generatedMark distingue los archivos que escribe bflow render de los que
// escribió una persona.
const generatedMark = "<!-- generado por bflow render: no lo edites; cambia bflow.yaml (agents) o el archivo extra y vuelve a correrlo -->"

var tools = map[string][]string{
	agents.Read:  {"Read", "Grep", "Glob"},
	agents.Write: {"Edit", "Write"},
	agents.Bash:  {"Bash"},
}

type frontmatter struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	Tools        string `yaml:"tools"`
	Model        string `yaml:"model,omitempty"`
	Effort       string `yaml:"effort,omitempty"`
	OmitClaudeMd bool   `yaml:"omitClaudeMd,omitempty"`
}

// RenderAgents devuelve cada agente como subagente de Claude Code: ruta
// relativa a la raíz del repo → contenido.
func (Agent) RenderAgents(specs []agents.Spec) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, s := range specs {
		var ts []string
		for _, t := range s.Tools {
			ts = append(ts, tools[t]...)
		}
		fm, err := yaml.Marshal(frontmatter{Name: s.Subagent, Description: s.Description, Tools: strings.Join(ts, ", "),
			Model: s.Model, Effort: s.Effort, OmitClaudeMd: s.OmitClaudeMd})
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		b.WriteString("---\n")
		b.Write(fm)
		b.WriteString("---\n" + generatedMark + "\n\n")
		b.WriteString(strings.TrimSpace(s.Body) + "\n")
		out[agentsDir+"/"+s.Subagent+".md"] = b.Bytes()
	}
	return out, nil
}

// GeneratedAgents devuelve los subagentes que ya escribió bflow render.
func (Agent) GeneratedAgents(root string) []string {
	matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(agentsDir), flow.SubagentPrefix+"*.md"))
	var out []string
	for _, m := range matches {
		if IsGenerated(m) {
			out = append(out, agentsDir+"/"+filepath.Base(m))
		}
	}
	return out
}

// Skills lista las skills del proyecto (.claude/skills) y del usuario
// (~/.claude/skills) con su descripción.
func (Agent) Skills(root string) []agents.Skill {
	dirs := []string{filepath.Join(root, ".claude", "skills")}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".claude", "skills"))
	}
	var out []agents.Skill
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, "*", "SKILL.md"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			s := agents.Skill{Name: filepath.Base(filepath.Dir(f)), Path: f}
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(l), "description:"); ok {
					s.Description = strings.Trim(strings.TrimSpace(v), `"'`)
					break
				}
			}
			out = append(out, s)
		}
	}
	return out
}

// IsGenerated dice si el archivo lo escribió bflow render.
func IsGenerated(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(b, []byte(generatedMark))
}

// MinVersion es la versión de Claude Code que bflow necesita: omitClaudeMd
// en los agentes generados existe desde la v2.1.271.
const MinVersion = "2.1.271"

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Version compara la versión instalada de Claude Code con MinVersion.
func (Agent) Version() (have, min string, ok bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "claude", "--version").Output()
	if err != nil {
		return "", MinVersion, false, err
	}
	have = versionRe.FindString(string(out))
	if have == "" {
		return "", MinVersion, false, fmt.Errorf("versión no reconocida: %q", strings.TrimSpace(string(out)))
	}
	return have, MinVersion, !older(have, MinVersion), nil
}

// older dice si la versión a es anterior a b (ambas x.y.z).
func older(a, b string) bool {
	pa, pb := versionRe.FindStringSubmatch(a), versionRe.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	return false
}
