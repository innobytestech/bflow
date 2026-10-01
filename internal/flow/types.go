// Package flow es el núcleo de bflow: fases, carriles, gates, veredictos y
// transiciones. Es puro: no hace I/O ni conoce trackers, git o herramientas de
// agente. Apply recibe un estado y un evento y devuelve el estado nuevo, los
// efectos que alguien más debe ejecutar y lo que la sesión debe hacer después.
package flow

import (
	"fmt"
	"strings"
)

// Phase es una fase del flujo.
type Phase string

const (
	Backlog      Phase = "backlog"
	Discovery    Phase = "discovery"
	Spec         Phase = "spec"
	Contract     Phase = "contract"
	Implementing Phase = "implementing"
	Paused       Phase = "paused"
	Quality      Phase = "quality"
	Documenting  Phase = "documenting"
	Walkthrough  Phase = "walkthrough"
	InReview     Phase = "in_review"
	Done         Phase = "done"
	Blocked      Phase = "blocked"
)

// Order es el orden canónico de las fases de trabajo. Un carril recorre un
// subconjunto en este mismo orden.
var Order = []Phase{Discovery, Spec, Contract, Implementing, Paused, Quality, Documenting, Walkthrough, InReview, Done}

// Lane es un carril: qué fases recorre una tarea.
type Lane string

const (
	Full   Lane = "full"
	Light  Lane = "light"
	Hotfix Lane = "hotfix"
)

// Gate es una decisión humana pendiente.
type Gate string

const (
	GateLane        Gate = "lane"        // elegir carril (backlog)
	GateDiscovery   Gate = "discovery"   // cerrar discovery
	GateSpec        Gate = "spec"        // aprobar spec
	GateSplit       Gate = "split"       // el spec-author propone dividir
	GateContract    Gate = "contract"    // aprobar T1
	GateDecision    Gate = "decision"    // un agente devolvió NEEDS_DECISION
	GatePause       Gate = "pause"       // permiso para lanzar la revisión
	GateRounds      Gate = "rounds"      // cortacircuito de rondas de calidad
	GateQuestions   Gate = "questions"   // preguntas de producto, antes de ver el código
	GateWalkthrough Gate = "walkthrough" // aprobar el PR
)

// Verdict es lo que un agente reporta.
type Verdict string

const (
	Ready         Verdict = "READY"
	Split         Verdict = "SPLIT"
	NeedsDecision Verdict = "NEEDS_DECISION"
	ContractReady Verdict = "CONTRACT_READY"
	DoneV         Verdict = "DONE"
	BlockedV      Verdict = "BLOCKED"
	Approved      Verdict = "APPROVED"
	RejectedV     Verdict = "REJECTED"
)

// Rejection es un error de reglas del flujo (exit 2): la petición se entiende
// pero el estado actual no la permite.
type Rejection struct {
	Code   string
	Reason string
}

func (r *Rejection) Error() string { return r.Reason }

func reject(code, format string, a ...any) error {
	return &Rejection{Code: code, Reason: fmt.Sprintf(format, a...)}
}

// PendingGate es el gate que espera respuesta humana.
type PendingGate struct {
	Name    Gate     `json:"name"`
	Agent   string   `json:"agent,omitempty"`   // para decision/split: quién lo pidió
	Options []string `json:"options,omitempty"` // para decision: opciones que trajo el agente
	Note    string   `json:"note,omitempty"`    // problema o propuesta del agente
}

// BlockInfo guarda por qué y desde dónde se bloqueó una tarea.
type BlockInfo struct {
	Reason string `json:"reason"`
	From   Phase  `json:"from"`
}

// State es el estado de flujo de una tarea. Es lo único que el núcleo necesita;
// el store lo persiste junto con datos de adaptadores que el núcleo ignora.
type State struct {
	ID      string             `json:"id"`
	Slug    string             `json:"slug"`
	Lane    Lane               `json:"lane,omitempty"`
	Phase   Phase              `json:"phase"`
	Gate    *PendingGate       `json:"gate,omitempty"`
	Reports map[string]Verdict `json:"reports,omitempty"` // veredictos de la fase actual
	Round   int                `json:"round"`             // rechazos de la compuerta de calidad
	Block   *BlockInfo         `json:"block,omitempty"`

	BranchCreated bool   `json:"branch_created,omitempty"`
	Started       bool   `json:"started,omitempty"` // start_date sellada
	Resume        bool   `json:"resume,omitempty"`  // el próximo spawn reanuda al mismo agente
	Note          string `json:"note,omitempty"`    // último comentario humano para el agente
	Decision      string `json:"decision,omitempty"`
}

// New crea el estado inicial de una tarea.
func New(id, slug string) State { return State{ID: id, Slug: slug, Phase: Backlog} }

// EventKind es el tipo de evento.
type EventKind string

const (
	EvStart   EventKind = "start"
	EvApprove EventKind = "approve"
	EvReject  EventKind = "reject"
	EvReport  EventKind = "report"
	EvBlock   EventKind = "block"
	EvUnblock EventKind = "unblock"
	EvMerged  EventKind = "merged"
	// EvClosedOutside cierra una tarea que se terminó fuera de esta copia
	// (el tracker la da por hecha o su PR se mergeó), en cualquier fase empezada.
	EvClosedOutside EventKind = "closed_outside"
)

// Event es algo que pasó: una decisión humana, un reporte de agente o un
// hecho externo (merge). Solo se usan los campos del tipo correspondiente.
type Event struct {
	Kind EventKind `json:"kind"`

	Lane  Lane   `json:"lane,omitempty"`  // start
	Fixes string `json:"fixes,omitempty"` // start hotfix: tarea que corrige

	Gate       Gate   `json:"gate,omitempty"`       // approve/reject (vacío = el pendiente)
	To         Phase  `json:"to,omitempty"`         // reject: destino alternativo
	Choice     int    `json:"choice,omitempty"`     // approve decision: 1..n
	Note       string `json:"note,omitempty"`       // reject/block/approve decision libre
	Attachment string `json:"attachment,omitempty"` // approve discovery: texto completo

	Agent   string   `json:"agent,omitempty"` // report
	Verdict Verdict  `json:"verdict,omitempty"`
	Options []string `json:"options,omitempty"`  // report NEEDS_DECISION
	CheckOK bool     `json:"check_ok,omitempty"` // report DONE en implementing: check verificado en HEAD

	Reason string `json:"reason,omitempty"` // closed_outside: "tracker" o "pr"
}

// AgentArtifacts son los archivos de .bflow/tasks/<ID>/ que escriben los
// agentes (los que terminan en "/" son carpetas). El resto es estado de bflow.
var AgentArtifacts = []string{"contract.md", "walkthrough.md", "reports/"}

// IsAgentArtifact dice si rel (relativa a la carpeta de la tarea) es un
// artefacto que escribe un agente.
func IsAgentArtifact(rel string) bool {
	for _, a := range AgentArtifacts {
		if rel == a || (strings.HasSuffix(a, "/") && strings.HasPrefix(rel, a) && len(rel) > len(a)) {
			return true
		}
	}
	return false
}

// EffectKind es un efecto que el núcleo pide y el engine ejecuta.
type EffectKind string

const (
	FxTrackerState EffectKind = "tracker_state" // mover la tarea en el tracker a Phase
	FxComment      EffectKind = "comment"       // comentar Body (Markdown) en el tracker
	FxCreateBranch EffectKind = "create_branch" // crear o retomar la rama de la tarea
	FxStampStart   EffectKind = "stamp_start"   // sellar fecha de inicio si falta
	FxOpenPR       EffectKind = "open_pr"       // abrir o actualizar el PR
)

// Effect es una acción externa pendiente.
type Effect struct {
	Kind   EffectKind `json:"kind"`
	Phase  Phase      `json:"phase,omitempty"`
	Body   string     `json:"body,omitempty"`
	Hotfix bool       `json:"hotfix,omitempty"` // create_branch: prefijo de hotfix
}
