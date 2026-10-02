package trackertest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
)

// ErrInjected es el error que devuelve Memory cuando se le pide fallar.
var ErrInjected = errors.New("fallo inyectado")

// Memory es un tracker en memoria para pruebas del núcleo, con fallos a pedido.
type Memory struct {
	mu       sync.Mutex
	tasks    map[string]*tracker.Task
	comments map[string][]tracker.Comment
	seq      int
	created  []string // lo creado con Create, para FailCreateAfter

	// FailTransitions y FailComments hacen fallar esas operaciones mientras sean true.
	FailTransitions bool
	FailComments    bool
	// FailCreateAfter, si es mayor que 0, hace que Create devuelva ErrInjected
	// después de haber creado esa cantidad de tareas.
	FailCreateAfter int
	// Calls registra las operaciones de escritura en orden ("transition LOCAL-1 spec", "comment LOCAL-1").
	Calls []string
}

// NewMemory crea un tracker en memoria vacío.
func NewMemory() *Memory {
	return &Memory{tasks: map[string]*tracker.Task{}, comments: map[string][]tracker.Comment{}}
}

var (
	_ tracker.Tracker = (*Memory)(nil)
	_ tracker.Creator = (*Memory)(nil)
)

func (m *Memory) Name() string { return "memory" }

func (m *Memory) Create(_ context.Context, title, desc string) (tracker.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailCreateAfter > 0 && len(m.created) >= m.FailCreateAfter {
		return tracker.Task{}, ErrInjected
	}
	m.seq++
	t := &tracker.Task{ID: "MEM-" + strconv.Itoa(m.seq), Title: title, Description: desc, Phase: flow.Backlog,
		URL: fmt.Sprintf("https://tracker.test/MEM-%d", m.seq), Updated: time.Now()}
	m.tasks[t.ID] = t
	m.created = append(m.created, t.ID)
	return *t, nil
}

func (m *Memory) Get(_ context.Context, id string) (tracker.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return tracker.Task{}, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	return *t, nil
}

func (m *Memory) List(_ context.Context, f tracker.Filter) ([]tracker.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []tracker.Task
	for _, t := range m.tasks {
		if f.OpenOnly && (t.Phase == flow.Done || t.Phase == flow.Dropped) {
			continue
		}
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) Transition(_ context.Context, id string, to flow.Phase, p tracker.Patch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailTransitions {
		return ErrInjected
	}
	t, ok := m.tasks[id]
	if !ok {
		return tracker.ErrNotFound
	}
	t.Phase = to
	t.Closed = to == flow.Done || to == flow.Dropped
	if !p.StampStart.IsZero() && t.Start == nil {
		d := p.StampStart
		t.Start = &d
	}
	m.Calls = append(m.Calls, fmt.Sprintf("transition %s %s", id, to))
	return nil
}

func (m *Memory) Comment(_ context.Context, id, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailComments {
		return ErrInjected
	}
	if _, ok := m.tasks[id]; !ok {
		return tracker.ErrNotFound
	}
	m.comments[id] = append(m.comments[id], tracker.Comment{ID: strconv.Itoa(len(m.comments[id]) + 1), Body: body, Created: time.Now()})
	m.Calls = append(m.Calls, "comment "+id)
	return nil
}

func (m *Memory) Comments(_ context.Context, id string) ([]tracker.Comment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]tracker.Comment(nil), m.comments[id]...), nil
}

// CloseKeepingPhase simula un cierre hecho fuera de bflow (p. ej. "Closes #N"
// de un PR): la tarea queda cerrada pero conserva su fase y estado.
func (m *Memory) CloseKeepingPhase(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Closed = true
	}
}
