package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

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
	exe, err := os.Executable()
	if err != nil {
		return output.Fail("update", err)
	}
	if err := release.Replace(exe, bin); err != nil {
		return output.Fail("update", errors.New("no se pudo reemplazar el binario: "+err.Error()))
	}
	data["path"] = exe
	return env("updated", fmt.Sprintf("bflow %s → %s (%s)", c.Version, rel.Tag, exe))
}
