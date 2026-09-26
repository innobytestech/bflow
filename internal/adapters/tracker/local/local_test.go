package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"innobytes.tech/bflow/internal/testutil"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/tracker/trackertest"
)

func TestConformance(t *testing.T) {
	trackertest.Run(t, trackertest.Harness{
		New: func(t *testing.T) tracker.Tracker { return New(testutil.TempDir(t), "") },
		Seed: func(t *testing.T, tr tracker.Tracker) (string, string) {
			task, err := tr.(*Tracker).Create(context.Background(), "Demo", "descripción")
			if err != nil {
				t.Fatal(err)
			}
			return task.ID, "Demo"
		},
		Missing: "LOCAL-999",
	})
}

func TestSequentialIDsAndPrefix(t *testing.T) {
	dir := testutil.TempDir(t)
	tr := New(dir, "ACME")
	ctx := context.Background()
	for i, want := range []string{"ACME-1", "ACME-2", "ACME-3"} {
		task, err := tr.Create(ctx, "t", "")
		if err != nil {
			t.Fatal(err)
		}
		if task.ID != want {
			t.Errorf("tarea %d: %s, want %s", i, task.ID, want)
		}
	}
	// Otra instancia sobre la misma carpeta continúa la secuencia.
	task, _ := New(dir, "ACME").Create(ctx, "t", "")
	if task.ID != "ACME-4" {
		t.Errorf("secuencia persistente: %s", task.ID)
	}
}

func TestConcurrentCreateUniqueIDs(t *testing.T) {
	dir := testutil.TempDir(t)
	var wg sync.WaitGroup
	ids := make(chan string, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := New(dir, "").Create(context.Background(), "t", "")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- task.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Errorf("ID duplicado %s", id)
		}
		seen[id] = true
	}
	if len(seen) != 30 {
		t.Errorf("%d IDs únicos, want 30", len(seen))
	}
}

func TestFilesAreReadable(t *testing.T) {
	dir := testutil.TempDir(t)
	tr := New(dir, "")
	task, _ := tr.Create(context.Background(), "Título con acentos: módulo", "línea 1\nlínea 2")
	b, err := os.ReadFile(filepath.Join(dir, task.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Título con acentos: módulo") || !strings.Contains(string(b), "\n  ") {
		t.Errorf("el archivo debe ser JSON indentado y legible:\n%s", b)
	}
}
