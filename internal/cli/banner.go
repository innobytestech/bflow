package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/output"
)

// El banner es para personas: solo sale en una terminal interactiva y sin
// --json. Un agente corre bflow con --json o con la salida en un pipe, así
// que nunca le llega (ni a los hooks ni a la statusline) y no gasta tokens.

type bannerMode int

const (
	bannerOff   bannerMode = iota
	bannerASCII            // TERM=dumb: sin Unicode ni color
	bannerPlain            // NO_COLOR o consola sin secuencias VT
	bannerColor
)

// Degradado de la marca innobytes (#1B2CF2 → #38B6FF), ajustado para que
// ambos extremos tengan al menos 3:1 de contraste en terminales oscuras y
// claras (TestBannerContrast).
var (
	gradFrom = [3]int{0x33, 0x46, 0xFF}
	gradTo   = [3]int{0x1E, 0x9B, 0xE6}
)

var bannerLogo = []string{
	"      • ━━━━      ",
	"       ━━━━━━━    ",
	"  • ━━━━━━━━━━━   ",
	" ━━━━━━━━━━━━━━━  ",
	"  • ━━━━━━━━━━━━  ",
	"                  ",
}

var bannerWord = []string{
	"██████╗ ███████╗██╗      ██████╗ ██╗    ██╗",
	"██╔══██╗██╔════╝██║     ██╔═══██╗██║    ██║",
	"██████╔╝█████╗  ██║     ██║   ██║██║ █╗ ██║",
	"██╔══██╗██╔══╝  ██║     ██║   ██║██║███╗██║",
	"██████╔╝██║     ███████╗╚██████╔╝╚███╔███╔╝",
	"╚═════╝ ╚═╝     ╚══════╝ ╚═════╝  ╚══╝╚══╝ ",
}

// bannerASCIIWord es "bflow" con las letras de la fuente FIGlet standard,
// puestas una junto a otra.
var bannerASCIIWord = joinLetters(
	[]string{` _     `, `| |__  `, `| '_ \ `, `| |_) |`, `|_.__/ `},
	[]string{`  __ `, ` / _|`, `| |_ `, `|  _|`, `|_|  `},
	[]string{` _ `, `| |`, `| |`, `| |`, `|_|`},
	[]string{`       `, `  ___  `, ` / _ \ `, `| (_) |`, ` \___/ `},
	[]string{`          `, `__      __`, `\ \ /\ / /`, ` \ V  V / `, `  \_/\_/  `},
)

func joinLetters(letters ...[]string) []string {
	out := make([]string, len(letters[0]))
	for _, l := range letters {
		for i := range out {
			out[i] += l[i]
		}
	}
	return out
}

func modeFor(isTTY, asJSON bool, getenv func(string) string) bannerMode {
	switch {
	case !isTTY || asJSON:
		return bannerOff
	case getenv("TERM") == "dumb":
		return bannerASCII
	case getenv("NO_COLOR") != "":
		return bannerPlain
	}
	return bannerColor
}

// bannerFor decide el modo para la salida de c y, en Windows, activa las
// secuencias de color de la consola.
func bannerFor(c *Ctx) bannerMode {
	f, ok := c.Stdout.(*os.File)
	m := modeFor(ok && isTerminal(f), c.JSON, os.Getenv)
	if m == bannerColor && !enableVT(f) {
		m = bannerPlain
	}
	return m
}

func renderBanner(m bannerMode, version string) string {
	if m == bannerOff {
		return ""
	}
	var b strings.Builder
	if m == bannerASCII {
		for _, l := range bannerASCIIWord {
			b.WriteString("  ")
			b.WriteString(strings.TrimRight(l, " "))
			b.WriteString("\n")
		}
	} else {
		for i := range bannerWord {
			line := bannerLogo[i] + bannerWord[i]
			if m == bannerColor {
				line = gradient(line, gradFrom, gradTo)
			}
			b.WriteString(strings.TrimRight(line, " "))
			b.WriteString("\n")
		}
	}
	name, sep := "bflow", "·"
	switch m {
	case bannerColor:
		name = fmt.Sprintf("\x1b[1;38;2;%d;%d;%dmbflow\x1b[0m", gradTo[0], gradTo[1], gradTo[2])
	case bannerASCII:
		sep = "-"
	}
	fmt.Fprintf(&b, "\n  %s by innobytes.tech %s %s\n  flow engine Spec-Driven Development\n", name, sep, version)
	return b.String()
}

// gradient pinta cada carácter visible con el color interpolado según su columna.
func gradient(line string, from, to [3]int) string {
	rs := []rune(line)
	n := max(1, len(rs)-1)
	var b strings.Builder
	for i, r := range rs {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		c := func(k int) int { return from[k] + (to[k]-from[k])*i/n }
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm%c", c(0), c(1), c(2), r)
	}
	return b.String() + "\x1b[0m"
}

// dimLabel alinea las etiquetas del resumen bajo el banner.
func dimLabel(w io.Writer, m bannerMode, label, value string) {
	if m == bannerColor {
		fmt.Fprintf(w, "  \x1b[2m%-9s\x1b[0m %s\n", label, value)
		return
	}
	fmt.Fprintf(w, "  %-9s %s\n", label, value)
}

// homeCommand es bflow sin argumentos. En una terminal: banner, tarea activa
// y siguiente paso. Para un agente o un pipe, la lista de comandos de siempre.
var homeCommand = &Command{Name: "", Run: runHome}

func runHome(c *Ctx) output.Envelope {
	m := bannerFor(c)
	if m == bannerOff {
		return registry["help"].Run(c)
	}
	var b strings.Builder
	b.WriteString(renderBanner(m, c.Version))
	b.WriteString("\n")
	e, err := engineFor(c)
	switch {
	case err != nil:
		dimLabel(&b, m, "config", err.Error())
	case e.Cfg.Sources.Repo == "":
		dimLabel(&b, m, "config", "sin bflow.yaml en este repo: bflow init")
	default:
		ctx := context.Background()
		if id, err := e.Active(ctx); err != nil {
			dimLabel(&b, m, "tarea", "ninguna en curso · bflow start <ID> --lane full|light|hotfix")
		} else if v, err := e.Status(ctx, id); err == nil {
			task := v.ID + " · " + string(v.Phase)
			if sc := engine.ReadStatusCache(e.Cfg.Root); sc != nil && sc.ID == id {
				task = StatuslineText(*sc, time.Now())
			}
			dimLabel(&b, m, "tarea", task)
			dimLabel(&b, m, "siguiente", nextLabel(v)+"  (bflow status "+id+")")
		}
	}
	dimLabel(&b, m, "ayuda", "bflow help")
	env := output.OK("home", map[string]any{}, nil)
	env.Text = strings.TrimRight(b.String(), "\n")
	return env
}
