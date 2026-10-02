package tracker

import (
	"strings"

	"innobytes.tech/bflow/internal/flow"
)

// StateDef es el estado del tracker que representa una fase.
type StateDef struct {
	Names []string // por preferencia: se escribe en el primero que exista
	Group string   // grupo del tracker al crearlo (Plane)
	Color string   // "#RRGGBB"
}

// DefaultStates son compatibles con los estados que ya usa acme (lib.mjs):
// tanto los nombres en español como los literales del harness (inProgress,
// specReady…). Ejemplo: si un proyecto tiene "Implementado" pero no "En
// revisión", quality escribe en "Implementado".
var DefaultStates = map[flow.Phase]StateDef{
	flow.Backlog:      {[]string{"Backlog", "Todo", "pending"}, "backlog", "#A3A3A3"},
	flow.Discovery:    {[]string{"Discovery"}, "unstarted", "#60A5FA"},
	flow.Spec:         {[]string{"Spec pendiente", "Spec por aprobar", "Spec", "readyForSpec", "specReady"}, "unstarted", "#818CF8"},
	flow.Contract:     {[]string{"Contrato", "In Progress", "En progreso", "inProgress"}, "started", "#6366F1"},
	flow.Implementing: {[]string{"In Progress", "En progreso", "inProgress"}, "started", "#3B82F6"},
	flow.Paused:       {[]string{"En pausa", "Implementado", "implemented"}, "started", "#8B5CF6"},
	flow.Quality:      {[]string{"En revisión", "Implementado", "implemented", "reviewed"}, "started", "#A855F7"},
	flow.Documenting:  {[]string{"Documentando", "Auditado", "audited"}, "started", "#14B8A6"},
	flow.Walkthrough:  {[]string{"Walkthrough", "Por PR", "documented"}, "started", "#F97316"},
	flow.InReview:     {[]string{"PR abierto", "Por PR", "documented"}, "started", "#FB923C"},
	flow.Done:         {[]string{"Done", "Hecho"}, "completed", "#22C55E"},
	flow.Blocked:      {[]string{"Bloqueado", "blocked"}, "started", "#EF4444"},
	flow.Dropped:      {[]string{"Cancelled", "Cancelado", "Descartado", "Canceled"}, "cancelled", "#6B7280"},
}

// PhaseOrder son todas las fases que un tracker debe poder escribir.
var PhaseOrder = append(append([]flow.Phase{flow.Backlog}, flow.Order...), flow.Blocked, flow.Dropped)

// StateTable es la tabla fase → estado, con los ajustes de tracker.states.
type StateTable map[flow.Phase]StateDef

// NewStateTable parte de DefaultStates y reemplaza los nombres de las fases
// que overrides (tracker.states) indique.
func NewStateTable(overrides map[string][]string) StateTable {
	t := StateTable{}
	for p, d := range DefaultStates {
		t[p] = d
	}
	for p, names := range overrides {
		d := t[flow.Phase(p)]
		d.Names = names
		t[flow.Phase(p)] = d
	}
	return t
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Write devuelve el nombre, tal como existe en existing, donde se escribe p:
// el primero de su lista (el nombre literal de la fase cuenta como el primero).
func (t StateTable) Write(existing []string, p flow.Phase) (string, bool) {
	for _, n := range append([]string{string(p)}, t[p].Names...) {
		for _, e := range existing {
			if norm(e) == norm(n) {
				return e, true
			}
		}
	}
	return "", false
}

// PhaseOf traduce un nombre de estado a fase: gana la fase donde ese nombre
// aparece antes en su lista de preferencia (el nombre literal de la fase cuenta
// como el primero); en empate, el orden de las fases. "" si ninguna lo usa.
func (t StateTable) PhaseOf(name string) flow.Phase {
	best, bestIdx := flow.Phase(""), 1<<30
	for _, p := range PhaseOrder {
		for i, n := range append([]string{string(p)}, t[p].Names...) {
			if norm(n) == norm(name) && i < bestIdx {
				best, bestIdx = p, i
				break
			}
		}
	}
	return best
}
