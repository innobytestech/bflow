// Package tracker define lo que el núcleo necesita de un gestor de tareas.
// Los adaptadores (local, plane, …) traducen: nombres de estados, formato de
// comentarios e identificadores. El núcleo solo habla de tareas, fases y
// comentarios en Markdown.
package tracker

import (
	"context"
	"errors"
	"time"

	"innobytes.tech/bflow/internal/flow"
)

// ErrNotFound indica que la tarea no existe en el tracker.
var ErrNotFound = errors.New("la tarea no existe en el tracker")

// Task es una tarea vista desde el núcleo.
type Task struct {
	ID          string     `json:"id"` // identificador legible: LOCAL-1, API-120
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"` // Markdown
	Phase       flow.Phase `json:"phase,omitempty"`       // "" si el estado del tracker no corresponde a ninguna fase
	State       string     `json:"state,omitempty"`       // nombre del estado en el tracker, para mostrar
	URL         string     `json:"url,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	Due         *time.Time `json:"due,omitempty"` // solo lectura: bflow nunca mueve vencimientos
	Updated     time.Time  `json:"updated"`
	Closed      bool       `json:"closed,omitempty"`
}

// Comment es un comentario en Markdown.
type Comment struct {
	ID      string    `json:"id"`
	Body    string    `json:"body"`
	Author  string    `json:"author,omitempty"`
	Created time.Time `json:"created"`
}

// Filter acota List.
type Filter struct {
	OpenOnly bool
}

// Patch son cambios adicionales al transicionar.
type Patch struct {
	// StampStart pide sellar la fecha de inicio si la tarea no la tiene.
	StampStart time.Time
}

// Tracker es la interfaz mínima que todo adaptador implementa.
type Tracker interface {
	Name() string
	Get(ctx context.Context, id string) (Task, error)
	List(ctx context.Context, f Filter) ([]Task, error)
	Transition(ctx context.Context, id string, to flow.Phase, p Patch) error
	Comment(ctx context.Context, id string, markdown string) error
	Comments(ctx context.Context, id string) ([]Comment, error)
}

// Creator es una capacidad opcional: crear tareas (el tracker local no tiene
// otra interfaz para hacerlo).
type Creator interface {
	Create(ctx context.Context, title, description string) (Task, error)
}

// StateProvisioner es una capacidad opcional: crear en el tracker los estados
// que las fases necesitan. Devuelve lo que creó (o crearía, con dryRun).
type StateProvisioner interface {
	EnsureStates(ctx context.Context, dryRun bool) ([]string, error)
}

// Project es un proyecto del tracker (para bflow init).
type Project struct {
	ID   string `json:"id"` // identificador legible
	Name string `json:"name"`
}

// ProjectLister es una capacidad opcional: listar proyectos para elegir en init.
type ProjectLister interface {
	Projects(ctx context.Context) ([]Project, error)
}

// StateInfo es un estado del tracker y la fase con la que bflow lo lee.
type StateInfo struct {
	Name   string     `json:"name"`
	Group  string     `json:"group,omitempty"`
	Phase  flow.Phase `json:"phase,omitempty"`  // "" si no corresponde a ninguna fase
	Writes []string   `json:"writes,omitempty"` // fases que escriben en este estado
}

// StateLister es una capacidad opcional: mostrar los estados y su traducción.
type StateLister interface {
	StateMap(ctx context.Context) ([]StateInfo, error)
}
