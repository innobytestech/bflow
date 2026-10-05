package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var mk0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func mkAt(min int) time.Time { return mk0.Add(time.Duration(min) * time.Minute) }

func TestTaskForMain(t *testing.T) {
	marks := []Mark{
		{TS: mkAt(0), Session: "s1", ID: "GH-1"},
		{TS: mkAt(10), Session: "s1", ID: "GH-2"},
		{TS: mkAt(5), Session: "s2", ID: "GH-9"},
	}
	main := TokenSource{Session: "s1"}
	for _, c := range []struct {
		ts   time.Time
		want string
	}{
		{mkAt(-1), ""},    // antes de toda marca: sin tarea
		{mkAt(0), "GH-1"}, // ts igual a la hora de la marca cuenta
		{mkAt(9), "GH-1"}, // la última marca previa
		{mkAt(10), "GH-2"},
		{mkAt(60), "GH-2"},
	} {
		if got := TaskFor(marks, main, c.ts); got != c.want {
			t.Errorf("principal en %v: %q, want %q", c.ts, got, c.want)
		}
	}
	if got := TaskFor(marks, TokenSource{Session: "s3"}, mkAt(60)); got != "" {
		t.Errorf("sesión sin marcas: %q", got)
	}
	if got := TaskFor(nil, main, mkAt(1)); got != "" {
		t.Errorf("sin marcas: %q", got)
	}
}

func TestTaskForSubagent(t *testing.T) {
	marks := []Mark{
		{TS: mkAt(0), Session: "s1", ID: "GH-1"},
		{TS: mkAt(20), Session: "s1", ID: "GH-2"},
		{TS: mkAt(30), Session: "s1:a1", ID: "GH-3"},
		{TS: mkAt(40), Session: "s1:a1", ID: "GH-4"},
	}
	own := TokenSource{Session: "s1:a1", Parent: "s1", Agent: "bflow-implementer"}
	for _, c := range []struct {
		ts   time.Time
		want string
	}{
		{mkAt(35), "GH-3"}, // última marca propia previa
		{mkAt(45), "GH-4"},
		{mkAt(10), "GH-3"}, // antes de su primera marca: la primera propia
	} {
		if got := TaskFor(marks, own, c.ts); got != c.want {
			t.Errorf("con marcas propias en %v: %q, want %q", c.ts, got, c.want)
		}
	}
	kid := TokenSource{Session: "s1:a2", Parent: "s1"}
	for _, c := range []struct {
		ts   time.Time
		want string
	}{
		{mkAt(10), "GH-1"}, // sin marcas propias: la última de la madre previa
		{mkAt(25), "GH-2"},
		{mkAt(-5), ""}, // ni la madre tenía marca
	} {
		if got := TaskFor(marks, kid, c.ts); got != c.want {
			t.Errorf("sin marcas propias en %v: %q, want %q", c.ts, got, c.want)
		}
	}
	if got := TaskFor(marks, TokenSource{Session: "x:y", Parent: "x"}, mkAt(10)); got != "" {
		t.Errorf("ni propias ni de la madre: %q", got)
	}
}

func TestMarksAppendPrune(t *testing.T) {
	path := MarksPath(t.TempDir())
	if got := ReadMarks(path); got != nil {
		t.Fatalf("archivo ausente = nil: %v", got)
	}
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	old := Mark{TS: now.Add(-8 * 24 * time.Hour), Session: "s1", ID: "GH-1"}
	fresh := Mark{TS: now.Add(-time.Hour), Session: "s2", ID: "GH-2"}
	for _, m := range []Mark{old, fresh} {
		if err := AppendMark(path, m); err != nil {
			t.Fatal(err)
		}
	}
	// una línea rota entre marcas buenas se ignora
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{no es json\n")
	f.Close()
	if err := AppendMark(path, Mark{TS: now, Session: "s3", ID: "GH-3"}); err != nil {
		t.Fatal(err)
	}
	got := ReadMarks(path)
	if len(got) != 3 || got[0].ID != "GH-1" || got[2].Session != "s3" || !got[1].TS.Equal(fresh.TS) {
		t.Fatalf("ReadMarks: %+v", got)
	}
	if err := PruneMarks(path, 7*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	got = ReadMarks(path)
	if len(got) != 2 || got[0].ID != "GH-2" || got[1].ID != "GH-3" {
		t.Errorf("prune borra solo lo de más de 7 días: %+v", got)
	}
	if _, err := os.Stat(path + ".lock"); err == nil {
		t.Error("el lock debe soltarse")
	}
}

func TestUnassignedUsage(t *testing.T) {
	dir := t.TempDir()
	if u := UnassignedUsage(dir); u.Total() != 0 {
		t.Fatalf("sin archivo: %+v", u)
	}
	p := UnassignedPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"ts":"2026-10-01T12:00:00Z","run":"claude:s1","tool":"claude","agent":"main","phase":"","msg":"m1","input":10,"cache_write":100,"cache_read":1000,"output":50}
{"ts":"2026-10-01T12:01:00Z","run":"claude:s1","tool":"claude","agent":"main","phase":"","msg":"m1","input":0,"cache_write":0,"cache_read":0,"output":25}
mala línea
{"ts":"2026-10-01T12:02:00Z","run":"claude:s2","tool":"claude","agent":"main","phase":"","msg":"m2","input":1,"cache_write":0,"cache_read":0,"output":2}
`
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	u := UnassignedUsage(dir)
	if u.Input != 11 || u.Output != 77 || u.CacheRead != 1000 || u.CacheWrite != 100 || u.Calls != 2 {
		t.Errorf("m1 se fusiona en una llamada, m2 es otra: %+v", u)
	}
}
