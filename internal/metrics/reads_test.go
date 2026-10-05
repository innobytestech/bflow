package metrics

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var rt0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func rev(sec int, agent, path string, partial bool) ReadEvent {
	return ReadEvent{TS: rt0.Add(time.Duration(sec) * time.Second), Agent: agent, Phase: "implementing", Path: path, Partial: partial, Tool: "claude"}
}

// R20: las líneas inválidas y las sin path se saltan.
func TestParseReadEventsSkipsInvalid(t *testing.T) {
	in := `{"ts":"2026-10-01T10:00:00Z","agent":"main","phase":"spec","path":"a.go","tool":"claude"}
no es json
{"ts":"2026-10-01T10:00:01Z","agent":"scout","phase":"discovery","tool":"claude"}

{"ts":"2026-10-01T10:00:02Z","agent":"scout","phase":"discovery","path":"b.go","partial":true,"tool":"opencode"}
{"ts":
`
	got := ParseReadEvents(strings.NewReader(in))
	if len(got) != 2 || got[0].Path != "a.go" || got[0].Agent != "main" || got[0].Partial ||
		got[1].Path != "b.go" || !got[1].Partial || got[1].Tool != "opencode" || got[1].TS.Second() != 2 {
		t.Fatalf("dos eventos válidos, en orden: %+v", got)
	}
}

// R14: relectura entre agentes = otro agente leyó antes la misma ruta.
func TestSummarizeReadsCrossRereads(t *testing.T) {
	cases := []struct {
		name string
		evs  []ReadEvent
		want int
	}{
		{"A,A,B", []ReadEvent{rev(1, "A", "x", false), rev(2, "A", "x", false), rev(3, "B", "x", false)}, 1},
		{"A,B,A", []ReadEvent{rev(1, "A", "x", false), rev(2, "B", "x", false), rev(3, "A", "x", false)}, 2},
		{"desordenado por ts", []ReadEvent{rev(3, "B", "x", false), rev(1, "A", "x", false), rev(2, "A", "x", false)}, 1},
		{"empate de ts conserva el orden del archivo", []ReadEvent{rev(1, "A", "x", false), rev(1, "B", "x", false)}, 1},
		{"rutas distintas", []ReadEvent{rev(1, "A", "x", false), rev(2, "B", "y", false)}, 0},
	}
	for _, c := range cases {
		if got := SummarizeReads(c.evs).CrossRereads; got != c.want {
			t.Errorf("%s: relecturas %d, quiero %d", c.name, got, c.want)
		}
	}

	s := SummarizeReads([]ReadEvent{
		rev(1, "A", "x", false), rev(2, "B", "x", true), rev(3, "A", "y", false), rev(4, "A", "y", false), rev(5, "A", "z", false), rev(6, "C", "z", false),
		rev(7, "A", "w", false)})
	if s.Reads != 7 || s.Files != 4 || s.Partial != 1 || s.CrossRereads != 2 || s.SharedFiles != 2 {
		t.Fatalf("totales: %+v", s)
	}
	if s.CrossPct != 28.6 || s.SharedPct != 50 {
		t.Errorf("porcentajes con 1 decimal: cross=%v shared=%v", s.CrossPct, s.SharedPct)
	}
	if !s.Available {
		t.Error("con eventos, Available es true")
	}
}

// R15: lecturas desc, agentes desc, ruta asc; agentes en orden alfabético.
func TestSummarizeReadsOrder(t *testing.T) {
	s := SummarizeReads([]ReadEvent{
		rev(1, "main", "b.go", false), rev(2, "main", "b.go", false),
		rev(3, "main", "a.go", false), rev(4, "main", "a.go", false),
		rev(5, "scout", "c.go", false), rev(6, "implementer", "c.go", true), rev(7, "main", "c.go", false),
		rev(8, "main", "d.go", false),
	})
	want := []ReadRow{
		{Path: "c.go", Reads: 3, Partial: 1, Agents: []string{"implementer", "main", "scout"}},
		{Path: "a.go", Reads: 2, Agents: []string{"main"}},
		{Path: "b.go", Reads: 2, Agents: []string{"main"}},
		{Path: "d.go", Reads: 1, Agents: []string{"main"}},
	}
	if !reflect.DeepEqual(s.Rows, want) {
		t.Errorf("filas:\n got %+v\nwant %+v", s.Rows, want)
	}
	// Mismo número de lecturas: más agentes primero.
	s = SummarizeReads([]ReadEvent{rev(1, "a", "z.go", false), rev(2, "a", "z.go", false), rev(3, "a", "m.go", false), rev(4, "b", "m.go", false)})
	if len(s.Rows) != 2 || s.Rows[0].Path != "m.go" || s.Rows[1].Path != "z.go" {
		t.Errorf("a igual de lecturas manda el número de agentes: %+v", s.Rows)
	}
}

// R19: sin eventos no hay datos y los porcentajes valen 0.
func TestSummarizeReadsEmpty(t *testing.T) {
	s := SummarizeReads(nil)
	if s.Available || s.Reads != 0 || s.Files != 0 || s.CrossPct != 0 || s.SharedPct != 0 || len(s.Rows) != 0 {
		t.Errorf("vacío: %+v", s)
	}
}
