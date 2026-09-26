// Package output define el contrato de salida de bflow: el envelope JSON que
// leen los agentes y los códigos de salida que leen los hooks.
//
// El campo Next es lo único que interpreta la skill del leader, así que debe
// bastar por sí solo para decidir qué hacer: qué preguntar y con qué opciones,
// qué agentes lanzar y con qué argumentos, o que no hay nada que hacer.
package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// Códigos de salida. Los hooks de las herramientas de agente dependen de ellos
// (en Claude Code, exit 2 bloquea la herramienta), por eso no cambian.
const (
	ExitOK       = 0
	ExitError    = 1
	ExitRejected = 2
)

// Acciones posibles de Next.
const (
	ActionAsk   = "ask"   // la sesión principal pregunta al humano
	ActionSpawn = "spawn" // la sesión principal lanza agentes
	ActionWait  = "wait"  // se espera algo fuera de la sesión (merge, humano ausente)
	ActionDone  = "done"  // no hay nada que hacer
)

// Option es una respuesta posible a una pregunta. Command es el comando bflow
// exacto que la sesión ejecuta si el humano la elige; así la skill no tiene
// que saber construir comandos.
type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command,omitempty"`
	NeedsNote   bool   `json:"needs_note,omitempty"`
}

// AgentCall es un agente a lanzar. Report es la plantilla del comando con el
// que el agente devuelve su veredicto.
type AgentCall struct {
	Agent  string            `json:"agent"`
	Args   map[string]string `json:"args"`
	Report string            `json:"report"`
}

// Next dice a la sesión principal qué hacer a continuación.
type Next struct {
	Action   string      `json:"action"`
	Gate     string      `json:"gate,omitempty"`
	Skill    string      `json:"skill,omitempty"` // skill que la sesión carga para conducir este paso
	Show     []string    `json:"show,omitempty"`  // comandos cuya salida se muestra íntegra antes de preguntar
	Question string      `json:"question,omitempty"`
	Options  []Option    `json:"options,omitempty"`
	Agents   []AgentCall `json:"agents,omitempty"`
	Parallel bool        `json:"parallel,omitempty"`
	Reason   string      `json:"reason,omitempty"`
}

// Envelope es la respuesta de todo subcomando.
type Envelope struct {
	OK   bool           `json:"ok"`
	Code string         `json:"code"`
	Data map[string]any `json:"data"`
	Next *Next          `json:"next"`

	// Text es la versión legible para humanos; no se serializa.
	Text string `json:"-"`
	// Quiet suprime la salida en modo texto (hooks que solo deben hablar al fallar).
	Quiet bool `json:"-"`
	exit  int
}

// OK construye una respuesta exitosa.
func OK(code string, data map[string]any, next *Next) Envelope {
	return Envelope{OK: true, Code: code, Data: data, Next: next, exit: ExitOK}
}

// Fail construye una respuesta de error (exit 1).
func Fail(code string, err error) Envelope {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return Envelope{Code: code, Data: map[string]any{"error": msg}, Text: "ERROR: " + msg, exit: ExitError}
}

// Rejected construye un rechazo o bloqueo (exit 2): la operación era válida
// como petición pero las reglas del flujo no la permiten.
func Rejected(code, reason string) Envelope {
	return Envelope{Code: code, Data: map[string]any{"reason": reason}, Text: "RECHAZADO: " + reason, exit: ExitRejected}
}

// WithExit fuerza un código de salida (p. ej. un check que falla es ok=false, exit 1).
func (e Envelope) WithExit(code int) Envelope { e.exit = code; return e }

// ExitCode devuelve el código de salida del proceso.
func (e Envelope) ExitCode() int { return e.exit }

// Write imprime el envelope en JSON o en texto.
func Write(w io.Writer, e Envelope, asJSON bool) error {
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	if e.Next == nil {
		e.Next = &Next{Action: ActionDone}
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		return enc.Encode(e)
	}
	if e.Quiet {
		return nil
	}
	if e.Text != "" {
		_, err := fmt.Fprintln(w, e.Text)
		return err
	}
	_, err := fmt.Fprintf(w, "%s\n", e.Code)
	return err
}
