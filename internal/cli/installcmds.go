package cli

import (
	"flag"
	"fmt"

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
		return output.Fail("install", fmt.Errorf("install claude: no implementado"))
	case "opencode":
		return output.Fail("install_unsupported", fmt.Errorf("aún no hay adaptador de OpenCode (GH-13)"))
	}
	return output.Fail("usage", fmt.Errorf("uso: bflow install <herramienta>; disponibles: claude, opencode"))
}
