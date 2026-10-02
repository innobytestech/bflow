// Package local es el tracker por defecto: una tarea por archivo JSON en una
// carpeta del repo (.bflow/local por defecto). Funciona sin red ni cuentas.
package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
)

// Tracker guarda tareas en Dir.
type Tracker struct {
	Dir    string
	Prefix string
	Now    func() time.Time
}

// New crea un tracker local en dir con el prefijo de IDs dado ("LOCAL" si vacío).
func New(dir, prefix string) *Tracker {
	if prefix == "" {
		prefix = "LOCAL"
	}
	return &Tracker{Dir: dir, Prefix: strings.ToUpper(prefix), Now: time.Now}
}

var (
	_ tracker.Tracker = (*Tracker)(nil)
	_ tracker.Creator = (*Tracker)(nil)
)

type file struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Phase       flow.Phase        `json:"phase"`
	Start       *time.Time        `json:"start,omitempty"`
	Due         *time.Time        `json:"due,omitempty"`
	Created     time.Time         `json:"created"`
	Updated     time.Time         `json:"updated"`
	Comments    []tracker.Comment `json:"comments,omitempty"`
}

func (t *Tracker) Name() string { return "local" }

func (t *Tracker) path(id string) (string, error) {
	if !store.ValidID(id) {
		return "", fmt.Errorf("identificador inválido %q", id)
	}
	return filepath.Join(t.Dir, id+".json"), nil
}

func (t *Tracker) read(id string) (file, error) {
	var f file
	p, err := t.path(id)
	if err != nil {
		return f, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return f, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("%s está dañado: %w", p, err)
	}
	return f, nil
}

func (t *Tracker) write(f file) error {
	p, err := t.path(f.ID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteAtomic(p, append(b, '\n'))
}

// modify lee, cambia y escribe una tarea bajo lock.
func (t *Tracker) modify(id string, fn func(f *file)) error {
	unlock, err := store.LockFile(filepath.Join(t.Dir, "."+id+".lock"), 10*time.Second, 2*time.Minute)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := t.read(id)
	if err != nil {
		return err
	}
	fn(&f)
	f.Updated = t.Now()
	return t.write(f)
}

func (f file) task() tracker.Task {
	return tracker.Task{ID: f.ID, Title: f.Title, Description: f.Description, Phase: f.Phase, State: string(f.Phase),
		Start: f.Start, Due: f.Due, Updated: f.Updated, Closed: f.Phase == flow.Done || f.Phase == flow.Dropped}
}

func (t *Tracker) Get(_ context.Context, id string) (tracker.Task, error) {
	f, err := t.read(id)
	if err != nil {
		return tracker.Task{}, err
	}
	return f.task(), nil
}

func (t *Tracker) List(_ context.Context, flt tracker.Filter) ([]tracker.Task, error) {
	entries, err := os.ReadDir(t.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []tracker.Task
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		f, err := t.read(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		if flt.OpenOnly && (f.Phase == flow.Done || f.Phase == flow.Dropped) {
			continue
		}
		out = append(out, f.task())
	}
	sort.Slice(out, func(i, j int) bool { return seq(out[i].ID) < seq(out[j].ID) })
	return out, nil
}

func seq(id string) int {
	n, _ := strconv.Atoi(id[strings.LastIndex(id, "-")+1:])
	return n
}

func (t *Tracker) Transition(_ context.Context, id string, to flow.Phase, p tracker.Patch) error {
	return t.modify(id, func(f *file) {
		f.Phase = to
		if !p.StampStart.IsZero() && f.Start == nil {
			d := p.StampStart
			f.Start = &d
		}
	})
}

func (t *Tracker) Comment(_ context.Context, id, markdown string) error {
	return t.modify(id, func(f *file) {
		f.Comments = append(f.Comments, tracker.Comment{
			ID: strconv.Itoa(len(f.Comments) + 1), Body: markdown, Author: os.Getenv("USER") + os.Getenv("USERNAME"), Created: t.Now()})
	})
}

func (t *Tracker) Comments(_ context.Context, id string) ([]tracker.Comment, error) {
	f, err := t.read(id)
	return f.Comments, err
}

// Create agrega una tarea con el siguiente número de la secuencia.
func (t *Tracker) Create(_ context.Context, title, description string) (tracker.Task, error) {
	if strings.TrimSpace(title) == "" {
		return tracker.Task{}, errors.New("la tarea necesita un título")
	}
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return tracker.Task{}, err
	}
	unlock, err := store.LockFile(filepath.Join(t.Dir, ".seq.lock"), 10*time.Second, 2*time.Minute)
	if err != nil {
		return tracker.Task{}, err
	}
	defer unlock()
	seqPath := filepath.Join(t.Dir, ".seq")
	n := 0
	if b, err := os.ReadFile(seqPath); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	n++
	now := t.Now()
	f := file{ID: fmt.Sprintf("%s-%d", t.Prefix, n), Title: strings.TrimSpace(title), Description: description,
		Phase: flow.Backlog, Created: now, Updated: now}
	if err := t.write(f); err != nil {
		return tracker.Task{}, err
	}
	if err := store.WriteAtomic(seqPath, []byte(strconv.Itoa(n)+"\n")); err != nil {
		return tracker.Task{}, err
	}
	return f.task(), nil
}
