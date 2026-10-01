package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"maps"
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
	Files     map[string][]byte // todo lo que debe existir
	Changed   []string          // nuevo o distinto
	Stale     []string          // generado antes, ya no pertenece al flujo
	Conflicts []string          // existe con ese nombre pero no lo generó bflow
	// Unresolved son los alias de modelo sin equivalente, por herramienta.
	Unresolved map[string][]agents.Unresolved
}

// agentsMD es el archivo que leen otras herramientas (Codex, OpenCode). Si el
// repo lo tiene, render mantiene en él un bloque que les dice cómo retomar
// una tarea; si no lo tiene, no lo crea.
const agentsMD = "AGENTS.md"

const (
	agentsMDStart = "<!-- bflow:inicio (generado por bflow render) -->"
	agentsMDEnd   = "<!-- bflow:fin -->"
	agentsMDBody  = "Este repo usa bflow para llevar cada tarea de la idea al PR. Si hay una tarea en curso o te piden avanzar una, corre `bflow status --json` y sigue `next`."
)

// upsertBlock pone el bloque de bflow en doc: lo reemplaza si ya está o lo
// agrega al final.
func upsertBlock(doc string) string {
	block := agentsMDStart + "\n" + agentsMDBody + "\n" + agentsMDEnd
	if i := strings.Index(doc, agentsMDStart); i >= 0 {
		if j := strings.Index(doc[i:], agentsMDEnd); j >= 0 {
			return doc[:i] + block + doc[i+j+len(agentsMDEnd):]
		}
	}
	return strings.TrimRight(doc, "\n") + "\n\n" + block + "\n"
}

func planRender(c *Ctx, cfg *config.Config) (*renderPlan, error) {
	if len(c.tools()) == 0 {
		return nil, errors.New("no hay adaptador de agente")
	}
	specs, err := agents.Build(cfg.Flow.Core(), cfg.Agents, cfg.Root)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	p := &renderPlan{Files: files, Unresolved: map[string][]agents.Unresolved{}}
	for _, name := range cfg.Agent.Rendered() {
		t := c.tool(name)
		if t == nil {
			return nil, fmt.Errorf("no hay adaptador de %s", name)
		}
		toolSpecs := specs
		if t.ResolvesModels() {
			var un []agents.Unresolved
			toolSpecs, un = agents.ResolveModels(specs, cfg.Models[t.Name()])
			if len(un) > 0 {
				p.Unresolved[t.Name()] = un
			}
		}
		out, err := t.RenderAgents(toolSpecs)
		if err != nil {
			return nil, err
		}
		for rel, content := range out {
			files[rel] = content
		}
	}
	// Lo generado por cualquier herramienta registrada, también la que ya no está en agent:.
	var generated []string
	for _, t := range c.tools() {
		generated = append(generated, t.GeneratedAgents(cfg.Root)...)
	}
	for rel, want := range files {
		have, err := os.ReadFile(filepath.Join(cfg.Root, filepath.FromSlash(rel)))
		switch {
		case err == nil && !slices.Contains(generated, rel):
			p.Conflicts = append(p.Conflicts, rel)
		case err != nil || !bytes.Equal(bytes.ReplaceAll(have, []byte("\r\n"), []byte("\n")), want):
			p.Changed = append(p.Changed, rel)
		}
	}
	for _, rel := range generated {
		if _, ok := files[rel]; !ok {
			p.Stale = append(p.Stale, rel)
		}
	}
	if have, err := os.ReadFile(filepath.Join(cfg.Root, agentsMD)); err == nil {
		doc := strings.ReplaceAll(string(have), "\r\n", "\n")
		if want := upsertBlock(doc); want != doc {
			p.Files[agentsMD] = []byte(want)
			p.Changed = append(p.Changed, agentsMD)
		}
	}
	slices.Sort(p.Changed)
	slices.Sort(p.Stale)
	slices.Sort(p.Conflicts)
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
	data := map[string]any{"changed": p.Changed, "stale": p.Stale, "total": len(p.Files), "unresolved": p.unresolvedData(c)}
	if len(p.Conflicts) > 0 {
		env := output.Fail("render_conflict", fmt.Errorf("%s ya existe y no lo generó bflow: renómbralo o bórralo y vuelve a correr bflow render", strings.Join(p.Conflicts, ", ")))
		env.Data["conflicts"] = p.Conflicts
		return env
	}
	if str(c.Flags, "check") == "true" {
		if len(p.Changed)+len(p.Stale) == 0 {
			env := output.OK("render_ok", data, nil)
			env.Text = fmt.Sprintf("%d archivo(s) al día", len(p.Files))
			return env
		}
		env := output.Fail("render_outdated", fmt.Errorf("agentes desactualizados: %s; corre bflow render y commitea", strings.Join(append(p.Changed, p.Stale...), ", ")))
		env.Data = data
		return env
	}
	if err := writeRender(cfg, p); err != nil {
		return fail(err)
	}
	env := output.OK("rendered", data, nil)
	env.Text = renderText(p)
	return env
}

// applyRender planifica y escribe los agentes; con conflictos no escribe nada
// y devuelve el plan para que quien llama decida qué decir.
func applyRender(c *Ctx, cfg *config.Config) (*renderPlan, error) {
	p, err := planRender(c, cfg)
	if err != nil || len(p.Conflicts) > 0 {
		return p, err
	}
	return p, writeRender(cfg, p)
}

func writeRender(cfg *config.Config, p *renderPlan) error {
	for _, rel := range p.Changed {
		abs := filepath.Join(cfg.Root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, p.Files[rel], 0o644); err != nil {
			return err
		}
	}
	for _, rel := range p.Stale {
		if err := os.Remove(filepath.Join(cfg.Root, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

// unresolvedData aplana los alias sin resolver: [{tool, alias, agents}], por
// herramienta en el orden de registro y luego por alias.
func (p *renderPlan) unresolvedData(c *Ctx) []map[string]any {
	out := []map[string]any{}
	for _, t := range c.tools() {
		for _, u := range p.Unresolved[t.Name()] {
			out = append(out, map[string]any{"tool": t.Name(), "alias": u.Alias, "agents": u.Agents})
		}
	}
	return out
}

func renderText(p *renderPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d archivo(s): %d escrito(s), %d sin cambios", len(p.Files), len(p.Changed), len(p.Files)-len(p.Changed))
	for _, rel := range p.Changed {
		b.WriteString("\n  + " + rel)
	}
	for _, rel := range p.Stale {
		b.WriteString("\n  - " + rel + " (ya no está en el flujo)")
	}
	if len(p.Changed)+len(p.Stale) > 0 {
		b.WriteString("\nCommitea los cambios para que el equipo use los mismos agentes.")
	}
	for _, tool := range slices.Sorted(maps.Keys(p.Unresolved)) {
		for _, u := range p.Unresolved[tool] {
			fmt.Fprintf(&b, "\naviso: %s: el alias %q no tiene equivalente (%s) y esos agentes salen sin model; agrega models.%s.%s: proveedor/modelo en bflow.yaml o en la config global",
				tool, u.Alias, strings.Join(u.Agents, ", "), tool, u.Alias)
		}
	}
	return b.String()
}
