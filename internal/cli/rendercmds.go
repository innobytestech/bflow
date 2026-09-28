package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/agents"
	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/output"
)

func init() {
	Register(&Command{Name: "render", Summary: "genera los agentes de bflow para la herramienta del repo: render [--check]",
		Setup: func(fs *flag.FlagSet) {
			fs.Bool("check", false, "no escribe: falla si los agentes generados no coinciden con la configuración (CI)")
		},
		Run: runRender})
}

// renderPlan compara lo que render generaría con lo que hay en el repo.
type renderPlan struct {
	Files   map[string][]byte // todo lo que debe existir
	Changed []string          // nuevo o distinto
	Stale   []string          // generado antes, ya no pertenece al flujo
}

func planRender(c *Ctx, cfg *config.Config) (*renderPlan, error) {
	if c.Agent == nil {
		return nil, errors.New("no hay adaptador de agente")
	}
	specs, err := agents.Build(cfg.Flow.Core(), cfg.Agents, cfg.Root)
	if err != nil {
		return nil, err
	}
	files, err := c.Agent.RenderAgents(specs)
	if err != nil {
		return nil, err
	}
	p := &renderPlan{Files: files}
	for rel, want := range files {
		have, err := os.ReadFile(filepath.Join(cfg.Root, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(bytes.ReplaceAll(have, []byte("\r\n"), []byte("\n")), want) {
			p.Changed = append(p.Changed, rel)
		}
	}
	for _, rel := range c.Agent.GeneratedAgents(cfg.Root) {
		if _, ok := files[rel]; !ok {
			p.Stale = append(p.Stale, rel)
		}
	}
	slices.Sort(p.Changed)
	slices.Sort(p.Stale)
	return p, nil
}

func runRender(c *Ctx) output.Envelope {
	cfg, err := config.Load(c.Dir)
	if err != nil {
		return output.Fail("config", err)
	}
	p, err := planRender(c, cfg)
	if err != nil {
		return output.Fail("render", err)
	}
	data := map[string]any{"changed": p.Changed, "stale": p.Stale, "total": len(p.Files)}
	if str(c.Flags, "check") == "true" {
		if len(p.Changed)+len(p.Stale) == 0 {
			env := output.OK("render_ok", data, nil)
			env.Text = fmt.Sprintf("%d agente(s) al día", len(p.Files))
			return env
		}
		env := output.Fail("render_outdated", fmt.Errorf("agentes desactualizados: %s; corre bflow render y commitea", strings.Join(append(p.Changed, p.Stale...), ", ")))
		env.Data = data
		return env
	}
	for _, rel := range p.Changed {
		abs := filepath.Join(cfg.Root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(abs, p.Files[rel], 0o644); err != nil {
			return fail(err)
		}
	}
	for _, rel := range p.Stale {
		if err := os.Remove(filepath.Join(cfg.Root, filepath.FromSlash(rel))); err != nil {
			return fail(err)
		}
	}
	env := output.OK("rendered", data, nil)
	var b strings.Builder
	fmt.Fprintf(&b, "%d agente(s): %d escrito(s), %d sin cambios", len(p.Files), len(p.Changed), len(p.Files)-len(p.Changed))
	for _, rel := range p.Changed {
		b.WriteString("\n  + " + rel)
	}
	for _, rel := range p.Stale {
		b.WriteString("\n  - " + rel + " (ya no está en el flujo)")
	}
	if len(p.Changed)+len(p.Stale) > 0 {
		b.WriteString("\nCommitea los cambios para que el equipo use los mismos agentes.")
	}
	env.Text = b.String()
	return env
}
