package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/output"
)

func init() {
	Register(&Command{Name: "install", Summary: "instala la skill y los hooks de bflow para tu herramienta: install claude|opencode [--skill-only]",
		Setup: func(fs *flag.FlagSet) {
			fs.Bool("skill-only", false, "solo la skill del usuario; no toca settings.json ni corre render")
		},
		Run: runInstall})
}

func runInstall(c *Ctx) output.Envelope {
	tool := ""
	if len(c.Args) > 0 {
		tool = c.Args[0]
	}
	switch tool {
	case "claude":
	case "opencode":
		return output.Fail("install_unsupported", fmt.Errorf("aún no hay adaptador de OpenCode (GH-13)"))
	default:
		return output.Fail("usage", fmt.Errorf("uso: bflow install <herramienta>; disponibles: claude, opencode"))
	}
	if c.Agent == nil {
		return output.Fail("install", fmt.Errorf("no hay adaptador de agente"))
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return output.Fail("install", fmt.Errorf("no se pudo resolver tu carpeta de usuario: %v", err))
	}
	skill, err := c.Agent.InstallSkill(home)
	if err != nil {
		return output.Fail("install", err)
	}
	warns := []string{}
	data := map[string]any{"skill": skill, "settings": "", "changed": false, "render": false, "warnings": warns}
	var text strings.Builder
	text.WriteString("skill: " + skill)

	finish := func() output.Envelope {
		data["warnings"] = warns
		for _, w := range warns {
			text.WriteString("\naviso: " + w)
		}
		env := output.OK("installed", data, nil)
		env.Text = text.String()
		return env
	}
	if str(c.Flags, "skill-only") == "true" {
		return finish()
	}
	root := config.FindRoot(c.Dir)
	if _, err := os.Stat(filepath.Join(root, config.RepoFile)); err != nil {
		warns = append(warns, "no hay bflow.yaml aquí: los hooks se instalan por repo; corre bflow install claude dentro de él")
		return finish()
	}
	cfg, err := config.Load(c.Dir)
	if err != nil {
		return output.Fail("config", err)
	}
	res, err := c.Agent.InstallSettings(cfg.Root)
	if err != nil {
		return output.Fail("install", err)
	}
	data["settings"], data["changed"] = res.Path, res.Changed
	warns = append(warns, res.Warnings...)
	if res.Changed {
		text.WriteString("\nhooks: " + res.Path + " (actualizado)")
	} else {
		text.WriteString("\nhooks: " + res.Path + " (sin cambios)")
	}
	if !cfg.Agent.Has("claude") {
		warns = append(warns, "agent: no incluye claude en bflow.yaml, así que no se generaron los agentes; pon agent: claude y corre bflow render")
		return finish()
	}
	p, err := applyRender(c, cfg)
	switch {
	case err != nil:
		warns = append(warns, "render falló: "+err.Error()+"; corre bflow render")
	case len(p.Conflicts) > 0:
		warns = append(warns, fmt.Sprintf("%s ya existe y no lo generó bflow: renómbralo o bórralo y corre bflow render", strings.Join(p.Conflicts, ", ")))
	default:
		data["render"] = true
		text.WriteString("\nagentes: " + renderText(p))
	}
	return finish()
}
