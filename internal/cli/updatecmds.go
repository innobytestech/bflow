package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/release"
)

func init() {
	Register(&Command{Name: "update", Summary: "actualiza bflow a la última versión publicada: update [--check] [--force]",
		Setup: func(fs *flag.FlagSet) {
			fs.Bool("check", false, "solo decir si hay una versión nueva")
			fs.Bool("force", false, "reemplazar aunque este binario no venga de una versión publicada")
		},
		Run: runUpdate})
}

// executable es la ruta del binario que update reemplaza (las pruebas la cambian).
var executable = os.Executable

func runUpdate(c *Ctx) output.Envelope {
	ctx := context.Background()
	cl := release.NewClient()
	rel, err := cl.Latest(ctx)
	if err != nil {
		return output.Fail("update", fmt.Errorf("no se pudo consultar la última versión: %w", err))
	}
	data := map[string]any{"current": c.Version, "latest": rel.Tag}
	env := func(code, text string) output.Envelope {
		e := output.OK(code, data, nil)
		e.Text = text
		return e
	}
	cmp, err := release.Compare(c.Version, rel.Tag)
	published := err == nil && release.Published(c.Version)
	force := str(c.Flags, "force") == "true"
	switch {
	case !published && !force:
		return env("update_unknown", fmt.Sprintf("este bflow (%s) no viene de una versión publicada, así que no se puede comparar con %s. bflow update --force lo reemplaza por %s", c.Version, rel.Tag, rel.Tag))
	case published && cmp >= 0 && !force:
		return env("up_to_date", "ya tienes la última versión ("+c.Version+")")
	case str(c.Flags, "check") == "true":
		return env("update_available", fmt.Sprintf("hay una versión nueva: %s (tienes %s). bflow update la instala", rel.Tag, c.Version))
	}
	bin, err := cl.Binary(ctx, rel)
	if err != nil {
		return output.Fail("update", err)
	}
	exe, err := executable()
	if err != nil {
		return output.Fail("update", err)
	}
	home, herr := os.UserHomeDir()
	had := map[string]bool{}
	if herr == nil {
		for _, t := range c.tools() {
			_, st := t.SkillState(home)
			had[t.Name()] = st != "missing"
		}
	}
	if err := release.Replace(exe, bin); err != nil {
		return output.Fail("update", errors.New("no se pudo reemplazar el binario: "+err.Error()))
	}
	data["path"] = exe
	text := fmt.Sprintf("bflow %s → %s (%s)", c.Version, rel.Tag, exe)
	integrations := map[string]string{}
	for _, t := range c.tools() {
		name := t.Name()
		what, done := "la skill", "skill de %s actualizada"
		if name != "claude" {
			what, done = "el comando", "comando de %s actualizado"
		}
		if !had[name] {
			integrations[name] = "skipped"
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := exec.CommandContext(cctx, exe, "install", name, "--skill-only", "--json").Run()
		cancel()
		if err != nil {
			integrations[name] = "failed"
			text += fmt.Sprintf("\nno se pudo refrescar %s de %s; corre bflow install %s para refrescarla", what, name, name)
		} else {
			integrations[name] = "updated"
			text += fmt.Sprintf("\n"+done, name)
		}
	}
	data["integrations"] = integrations
	data["skill"] = "skipped"
	if v, ok := integrations["claude"]; ok {
		data["skill"] = v
	}
	return env("updated", text)
}
