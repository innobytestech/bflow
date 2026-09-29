package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
)

// maxDisplayLines acota lo que se muestra en una gate: más que eso no se lee
// para decidir, y el resto sigue a un comando de distancia.
const maxDisplayLines = 150

// withDisplay agrega a una pregunta lo que el humano tiene que ver para
// decidir (el brief, el contrato, el review-map…), ya leído. En la piloto la
// sesión corría `bflow show` pero no copiaba la salida al chat, y el humano
// aprobaba sin ver nada: con el texto en `next`, solo tiene que repetirlo.
func (e *Engine) withDisplay(ctx context.Context, rec store.Record, n output.Next) output.Next {
	if n.Action != output.ActionAsk || len(n.Show) == 0 {
		return n
	}
	var parts []string
	if g := rec.Flow.Gate; g != nil && g.Name == flow.GateWalkthrough && rec.Flow.Note != "" {
		parts = append(parts, "**Tus respuestas a las preguntas de producto:**\n"+rec.Flow.Note)
	}
	for _, c := range n.Show {
		f := strings.Fields(c) // bflow show <ID> <qué> [--section <s>]
		if len(f) < 4 || f[1] != "show" {
			continue
		}
		section := ""
		if len(f) >= 6 && f[4] == "--section" {
			section = f[5]
		}
		text, err := e.Show(ctx, f[2], f[3], section)
		if err != nil {
			text = "(" + err.Error() + ")"
		}
		parts = append(parts, clip(strings.TrimSpace(text), c))
	}
	n.Display = strings.Join(parts, "\n\n")
	return n
}

func clip(text, cmd string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxDisplayLines {
		return text
	}
	return strings.Join(lines[:maxDisplayLines], "\n") + fmt.Sprintf("\n… (%d líneas más: %s)", len(lines)-maxDisplayLines, cmd)
}

// productQuestions saca del review-map la sección de preguntas de producto sin
// las líneas "el código responde": el humano contesta sin ver la respuesta.
func (e *Engine) productQuestions(id string) (string, error) {
	b, err := e.Store.ReadFile(id, artifacts["review-map"])
	if err != nil {
		if os.IsNotExist(err) {
			return "(el review-map todavía no existe)", nil
		}
		return "", err
	}
	var out []string
	in, level := false, 0
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if h := strings.IndexFunc(t, func(r rune) bool { return r != '#' }); strings.HasPrefix(t, "#") && h > 0 {
			switch {
			case strings.Contains(strings.ToLower(t), "preguntas"):
				in, level = true, h
				continue
			case in && h <= level:
				in = false
			}
		}
		if in && !strings.Contains(strings.ToLower(t), "el código responde") {
			out = append(out, l)
		}
	}
	q := strings.TrimSpace(strings.Join(out, "\n"))
	if q == "" {
		return "(el review-map no trae preguntas de producto)", nil
	}
	return q, nil
}
