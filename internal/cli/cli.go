// Package cli despacha los subcomandos de bflow. No conoce adaptadores: los
// recibe ya construidos en Env desde cmd/bflow, que es la raíz de composición.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/secrets"
	"innobytes.tech/bflow/internal/tracker"
)

// Env reúne lo que los comandos necesitan del exterior.
type Env struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Stdin   io.Reader
	Version string
	Dir     string // directorio de trabajo

	// Build construye el engine del repo que contiene dir con los adaptadores
	// que indique su configuración. Lo provee cmd/bflow.
	Build func(dir string) (*engine.Engine, error)

	// Connect valida una credencial contra un servicio (lo provee cmd/bflow).
	Connect func(ctx context.Context, service string, o ConnectOpts) (string, error)
	// Secrets es el llavero donde connect guarda las credenciales.
	Secrets secrets.Store
	// Projects lista los proyectos de un tracker (para init, antes de tener config).
	Projects func(ctx context.Context, service, url, workspace string) ([]tracker.Project, error)
	// Agent es el adaptador de la herramienta de agente (Claude Code): traduce
	// sus hooks y lee sus transcripts. Lo provee cmd/bflow.
	Agent AgentAdapter
}

// Ctx es lo que recibe cada comando.
type Ctx struct {
	*Env
	JSON   bool
	DryRun bool
	Flags  *flag.FlagSet
	Args   []string
}

// Command es un subcomando registrado.
type Command struct {
	Name    string
	Summary string
	// Setup declara banderas propias antes de parsear.
	Setup func(fs *flag.FlagSet)
	Run   func(c *Ctx) output.Envelope
}

var registry = map[string]*Command{}

// Register agrega un comando. Nombres compuestos ("env check") se registran con espacio.
func Register(c *Command) { registry[c.Name] = c }

func init() {
	Register(&Command{Name: "version", Summary: "versión de bflow", Run: func(c *Ctx) output.Envelope {
		e := output.OK("version", map[string]any{"version": c.Version}, nil)
		e.Text = "bflow " + c.Version
		return e
	}})
	Register(&Command{Name: "help", Summary: "lista los comandos", Run: func(c *Ctx) output.Envelope {
		names := make([]string, 0, len(registry))
		for n := range registry {
			names = append(names, n)
		}
		sort.Strings(names)
		var b strings.Builder
		b.WriteString("Byteflow by innobytes — uso: bflow <comando> [--json]\n\n")
		for _, n := range names {
			fmt.Fprintf(&b, "  %-22s %s\n", n, registry[n].Summary)
		}
		e := output.OK("help", map[string]any{"commands": names}, nil)
		e.Text = strings.TrimRight(b.String(), "\n")
		return e
	}})
}

// Run ejecuta args (sin el nombre del binario) y devuelve el código de salida.
func Run(args []string, env *Env) int {
	asJSON := false
	var rest []string
	for _, a := range args {
		if a == "--json" || a == "-json" {
			asJSON = true
			continue
		}
		rest = append(rest, a)
	}
	cmd, params := resolve(rest)
	if cmd == nil {
		name := strings.Join(rest, " ")
		if name == "" {
			cmd, params = registry["help"], nil
		} else {
			return emit(env, asJSON, output.Fail("unknown_command", fmt.Errorf("comando desconocido: %q (bflow help)", name)))
		}
	}
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dry := fs.Bool("dry-run", false, "no aplica cambios")
	if cmd.Setup != nil {
		cmd.Setup(fs)
	}
	if err := fs.Parse(interleave(fs, params)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return emit(env, asJSON, output.OK("help", map[string]any{"command": cmd.Name}, nil))
		}
		return emit(env, asJSON, output.Fail("bad_flags", err))
	}
	c := &Ctx{Env: env, JSON: asJSON, DryRun: *dry, Flags: fs, Args: fs.Args()}
	return emit(env, asJSON, cmd.Run(c))
}

// resolve busca el comando más largo que coincida ("env check" antes que "env").
func resolve(args []string) (*Command, []string) {
	for n := min(len(args), 3); n > 0; n-- {
		if c, ok := registry[strings.Join(args[:n], " ")]; ok {
			return c, args[n:]
		}
	}
	return nil, nil
}

// interleave permite banderas después de argumentos posicionales
// (`bflow approve LOCAL-1 --gate spec`), que flag de stdlib no admite.
func interleave(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue
			}
			if f := fs.Lookup(name); f != nil {
				if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
					continue
				}
				if i+1 < len(args) {
					flags = append(flags, args[i+1])
					i++
				}
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func emit(env *Env, asJSON bool, e output.Envelope) int {
	if err := output.Write(env.Stdout, e, asJSON); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return output.ExitError
	}
	return e.ExitCode()
}

// AgentAdapter es lo que la CLI necesita de la herramienta de agente.
type AgentAdapter interface {
	ParsePreToolUse(raw []byte) (a guard.Action, cwd string, ok bool)
	TranscriptPath(raw []byte) string
	ReadUsage(path string, cur *metrics.Cursor) (metrics.Usage, error)
}

// Commands devuelve los nombres de los comandos registrados.
func Commands() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
