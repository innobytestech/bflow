package store

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/testutil"
)

func TestCreateLoadUpdate(t *testing.T) {
	root := testutil.TempDir(t)
	clock := time.Date(2026, 9, 25, 10, 0, 0, 0, time.FixedZone("CST", -6*3600))
	s := Open(root)
	s.Now = func() time.Time { return clock }

	if _, err := s.Load("LOCAL-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tarea inexistente: %v", err)
	}
	rec, err := s.Update("LOCAL-1", func(r *Record, exists bool) error {
		if exists {
			t.Error("no debería existir")
		}
		r.Flow = flow.New("LOCAL-1", "demo")
		r.Title = "Demo"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Schema != SchemaVersion || !rec.Created.Equal(clock) || !rec.Updated.Equal(clock) {
		t.Errorf("metadatos: %+v", rec)
	}

	clock = clock.Add(time.Hour)
	if _, err := s.Update("LOCAL-1", func(r *Record, exists bool) error {
		if !exists {
			t.Error("debería existir")
		}
		r.Flow.Phase = flow.Spec
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("LOCAL-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Flow.Phase != flow.Spec || got.Title != "Demo" || !got.Updated.Equal(clock) || got.Created.Equal(clock) {
		t.Errorf("cargado: %+v", got)
	}
	if !strings.Contains(got.Updated.Format(time.RFC3339), "-06:00") {
		t.Errorf("las fechas deben conservar zona RFC 3339: %s", got.Updated.Format(time.RFC3339))
	}

	raw, _ := os.ReadFile(filepath.Join(root, ".bflow", "tasks", "LOCAL-1", "state.json"))
	if !strings.Contains(string(raw), `"schema": 1`) {
		t.Errorf("state.json debe ser JSON legible con schema:\n%s", raw)
	}
}

func TestUpdateErrorDoesNotWrite(t *testing.T) {
	s := Open(testutil.TempDir(t))
	if _, err := s.Update("X-1", func(r *Record, _ bool) error { r.Title = "a"; return nil }); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	_, err := s.Update("X-1", func(r *Record, _ bool) error { r.Title = "b"; return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err: %v", err)
	}
	if r, _ := s.Load("X-1"); r.Title != "a" {
		t.Errorf("un Update fallido no debe escribir: %q", r.Title)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "tasks", "X-1", ".lock")); !os.IsNotExist(err) {
		t.Errorf("el lock debe liberarse aunque fn falle: %v", err)
	}
}

func TestInvalidIDs(t *testing.T) {
	s := Open(testutil.TempDir(t))
	for _, id := range []string{"", "../x", "a/b", `a\b`, ".oculto", "con espacio"} {
		if _, err := s.Update(id, func(*Record, bool) error { return nil }); err == nil {
			t.Errorf("id %q debería rechazarse", id)
		}
	}
}

func TestSelfIgnoringDir(t *testing.T) {
	s := Open(testutil.TempDir(t))
	if _, err := s.Update("A-1", func(*Record, bool) error { return nil }); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir(), ".gitignore"))
	if err != nil || strings.TrimSpace(string(b)) != "*" {
		t.Errorf(".bflow/.gitignore: %q %v", b, err)
	}
}

func TestListAndFiles(t *testing.T) {
	s := Open(testutil.TempDir(t))
	for _, id := range []string{"B-2", "A-1", "B-10"} {
		if _, err := s.Update(id, func(r *Record, _ bool) error { r.Flow = flow.New(id, ""); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range recs {
		ids = append(ids, r.Flow.ID)
	}
	if strings.Join(ids, ",") != "A-1,B-2,B-10" {
		t.Errorf("List debe ordenar por prefijo y número: %v", ids)
	}
	if err := s.WriteFile("A-1", "reports/review.md", []byte("# ok")); err != nil {
		t.Fatal(err)
	}
	if b, err := s.ReadFile("A-1", "reports/review.md"); err != nil || string(b) != "# ok" {
		t.Errorf("ReadFile: %q %v", b, err)
	}
	if err := s.WriteFile("A-1", "../../escape.md", nil); err == nil {
		t.Error("WriteFile no debe salir de la carpeta de la tarea")
	}
}

func TestLogAppendAndRead(t *testing.T) {
	s := Open(testutil.TempDir(t))
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	entries := []Entry{
		{TS: t0, ID: "A-1", Event: "start", From: flow.Backlog, To: flow.Spec},
		{TS: t0.Add(time.Minute), ID: "B-1", Event: "start", From: flow.Backlog, To: flow.Implementing},
		{TS: t0.Add(2 * time.Minute), ID: "A-1", Event: "approve", Gate: "spec", From: flow.Spec, To: flow.Implementing, Note: "línea con\nsalto"},
	}
	if err := s.Append(entries...); err != nil {
		t.Fatal(err)
	}
	all, err := s.Log("")
	if err != nil || len(all) != 3 {
		t.Fatalf("log: %d %v", len(all), err)
	}
	a1, _ := s.Log("A-1")
	if len(a1) != 2 || a1[1].Note != "línea con\nsalto" {
		t.Errorf("filtro por ID: %+v", a1)
	}
	raw, _ := os.ReadFile(filepath.Join(s.Dir(), "log.jsonl"))
	if n := strings.Count(string(raw), "\n"); n != 3 {
		t.Errorf("una línea por entrada, got %d", n)
	}
}

func TestStaleLockIsBroken(t *testing.T) {
	s := Open(testutil.TempDir(t))
	s.LockTimeout = 2 * time.Second
	s.StaleAfter = 50 * time.Millisecond
	dir := filepath.Join(s.Dir(), "tasks", "A-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, ".lock")
	if err := os.WriteFile(lock, []byte("pid 999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("A-1", func(*Record, bool) error { return nil }); err != nil {
		t.Fatalf("un lock huérfano debe romperse: %v", err)
	}
}

func TestLockTimeout(t *testing.T) {
	s := Open(testutil.TempDir(t))
	s.LockTimeout = 100 * time.Millisecond
	s.StaleAfter = time.Hour
	dir := filepath.Join(s.Dir(), "tasks", "A-1")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".lock"), []byte("pid 1"), 0o644)
	_, err := s.Update("A-1", func(*Record, bool) error { return nil })
	if !errors.Is(err, ErrLocked) {
		t.Errorf("esperaba ErrLocked, got %v", err)
	}
}

// ---------- concurrencia ----------

const helperEnv = "BFLOW_STORE_HELPER"

// TestHelperProcess no es una prueba: es el cuerpo del proceso hijo.
func TestHelperProcess(t *testing.T) {
	spec := os.Getenv(helperEnv)
	if spec == "" {
		t.Skip("solo corre como proceso hijo")
	}
	parts := strings.Split(spec, "|")
	n, _ := strconv.Atoi(parts[2])
	if err := increments(Open(parts[0]), parts[1], n); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func increments(s *Store, who string, n int) error {
	for i := 0; i < n; i++ {
		var count int
		_, err := s.Update("RACE-1", func(r *Record, _ bool) error {
			if r.Adapter == nil {
				r.Adapter = map[string]string{}
			}
			count, _ = strconv.Atoi(r.Adapter["count"])
			count++
			r.Adapter["count"] = strconv.Itoa(count)
			return nil
		})
		if err != nil {
			return err
		}
		if err := s.Append(Entry{TS: time.Now(), ID: "RACE-1", Event: "tick", Note: fmt.Sprintf("%s-%d", who, i)}); err != nil {
			return err
		}
	}
	return nil
}

func TestConcurrentGoroutinesAndProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("prueba de concurrencia larga")
	}
	root := testutil.TempDir(t)
	const goroutines, procs, each = 20, 2, 15

	var cmds []*exec.Cmd
	for p := 0; p < procs; p++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s|proc%d|%d", helperEnv, root, p, each))
		var errb strings.Builder
		cmd.Stderr = &errb
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			if err := increments(Open(root), fmt.Sprintf("g%d", g), each); err != nil {
				errs <- err
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Errorf("proceso hijo: %v %s", err, c.Stderr)
		}
	}

	total := (goroutines + procs) * each
	rec, err := Open(root).Load("RACE-1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Adapter["count"] != strconv.Itoa(total) {
		t.Errorf("contador %s, want %d (se perdieron actualizaciones)", rec.Adapter["count"], total)
	}
	log, err := Open(root).Log("RACE-1")
	if err != nil {
		t.Fatalf("log ilegible (línea corrupta?): %v", err)
	}
	if len(log) != total {
		t.Errorf("log con %d entradas, want %d", len(log), total)
	}
	seen := map[string]bool{}
	for _, e := range log {
		if seen[e.Note] {
			t.Errorf("entrada duplicada %s", e.Note)
		}
		seen[e.Note] = true
	}
}

// Quien escribe en .bflow/ sin pasar por el Store (cachés) también deja
// .bflow/ ignorada por git.
func TestEnsureDirForIgnoresBflow(t *testing.T) {
	root := testutil.TempDir(t)
	p := filepath.Join(root, ".bflow", "cache", "plane.json")
	if err := EnsureDirFor(p); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(root, ".bflow", ".gitignore")); err != nil || string(b) != "*\n" {
		t.Errorf(".bflow/.gitignore: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Error("la carpeta de la caché debe existir")
	}
}
