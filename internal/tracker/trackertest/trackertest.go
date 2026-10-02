// Package trackertest es la suite de conformidad que todo adaptador de tracker
// debe pasar. Así el núcleo puede confiar en el mismo comportamiento con
// cualquier tracker.
package trackertest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
)

// Harness construye un tracker limpio y siembra una tarea abierta.
type Harness struct {
	New  func(t *testing.T) tracker.Tracker
	Seed func(t *testing.T, tr tracker.Tracker) (id, title string)
	// Missing es un ID bien formado que no existe.
	Missing string
	// NoStartDate es para trackers sin campo de fecha de inicio: "stamp start
	// once" solo verifica que Transition con StampStart no falla.
	NoStartDate bool
}

// Run ejecuta la suite.
func Run(t *testing.T, h Harness) {
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		tr := h.New(t)
		id, title := h.Seed(t, tr)
		task, err := tr.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.ID != id || task.Title != title {
			t.Errorf("Get: %+v, want id=%s title=%s", task, id, title)
		}
		if _, err := tr.Get(ctx, h.Missing); !errors.Is(err, tracker.ErrNotFound) {
			t.Errorf("Get(%s) = %v, want ErrNotFound", h.Missing, err)
		}
	})

	t.Run("transition to every phase", func(t *testing.T) {
		tr := h.New(t)
		id, _ := h.Seed(t, tr)
		phases := append([]flow.Phase{flow.Backlog}, flow.Order...)
		phases = append(phases, flow.Blocked)
		for _, p := range phases {
			if err := tr.Transition(ctx, id, p, tracker.Patch{}); err != nil {
				t.Fatalf("Transition(%s): %v", p, err)
			}
			task, err := tr.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if task.Phase != p && !sharesState(t, tr, p, task.Phase) {
				t.Errorf("tras Transition(%s) la tarea está en %q", p, task.Phase)
			}
		}
	})

	t.Run("dropped closes", func(t *testing.T) {
		tr := h.New(t)
		id, _ := h.Seed(t, tr)
		if err := tr.Transition(ctx, id, flow.Dropped, tracker.Patch{}); err != nil {
			t.Fatalf("Transition(dropped): %v", err)
		}
		task, err := tr.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !task.Closed {
			t.Errorf("tras Transition(dropped) la tarea sigue abierta: %+v", task)
		}
		if contains(t, tr, id) {
			t.Errorf("%s retirada sigue en List(OpenOnly)", id)
		}
	})

	t.Run("stamp start once", func(t *testing.T) {
		tr := h.New(t)
		id, _ := h.Seed(t, tr)
		first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		if err := tr.Transition(ctx, id, flow.Implementing, tracker.Patch{StampStart: first}); err != nil {
			t.Fatal(err)
		}
		if err := tr.Transition(ctx, id, flow.Implementing, tracker.Patch{StampStart: first.AddDate(0, 0, 5)}); err != nil {
			t.Fatal(err)
		}
		if h.NoStartDate {
			return
		}
		task, _ := tr.Get(ctx, id)
		if task.Start == nil || task.Start.Format("2006-01-02") != "2026-09-01" {
			t.Errorf("start = %v, want 2026-09-01 (no se sobrescribe)", task.Start)
		}
	})

	t.Run("comments", func(t *testing.T) {
		tr := h.New(t)
		id, _ := h.Seed(t, tr)
		if err := tr.Comment(ctx, id, "**Rechazado en spec:** falta el caso de error"); err != nil {
			t.Fatal(err)
		}
		cs, err := tr.Comments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range cs {
			if strings.Contains(c.Body, "falta el caso de error") {
				found = true
			}
		}
		if !found {
			t.Errorf("el comentario no aparece: %+v", cs)
		}
	})

	t.Run("list open", func(t *testing.T) {
		tr := h.New(t)
		id, _ := h.Seed(t, tr)
		if !contains(t, tr, id) {
			t.Fatalf("%s no aparece en List(OpenOnly)", id)
		}
		if err := tr.Transition(ctx, id, flow.Done, tracker.Patch{}); err != nil {
			t.Fatal(err)
		}
		if contains(t, tr, id) {
			t.Errorf("%s cerrada sigue en List(OpenOnly)", id)
		}
	})
}

func contains(t *testing.T, tr tracker.Tracker, id string) bool {
	t.Helper()
	ts, err := tr.List(context.Background(), tracker.Filter{OpenOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range ts {
		if task.ID == id {
			return true
		}
	}
	return false
}

// sharesState permite que dos fases compartan estado en el tracker (p. ej.
// contract e implementing en "In Progress"). El adaptador lo declara.
func sharesState(t *testing.T, tr tracker.Tracker, a, b flow.Phase) bool {
	s, ok := tr.(interface{ SameState(a, b flow.Phase) bool })
	return ok && s.SameState(a, b)
}
