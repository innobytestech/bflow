package metrics

import (
	"bufio"
	"encoding/json"
	"io"
	"math"
	"sort"
	"time"
)

// ReadsAllFile es el archivo de la tarea con una línea por Read de cualquier agente.
const ReadsAllFile = "reads-all.jsonl"

// UnknownAgent es el agente de un subagente sin nombre (R3).
const UnknownAgent = "unknown"

// ReadEvent es una línea de reads-all.jsonl.
type ReadEvent struct {
	TS      time.Time `json:"ts"`
	Agent   string    `json:"agent"`             // MainSession, UnknownAgent o nombre sin "bflow-"
	Phase   string    `json:"phase"`             // flow.Phase como texto
	Path    string    `json:"path"`              // relativo al repo, con /
	Partial bool      `json:"partial,omitempty"` // Read con offset o limit
	Tool    string    `json:"tool"`              // "claude" | "opencode"
}

// ParseReadEvents salta las líneas inválidas y las que no tienen path (R20).
func ParseReadEvents(r io.Reader) []ReadEvent {
	var out []ReadEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var ev ReadEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Path == "" {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// ReadRow es una fila de la tabla por archivo.
type ReadRow struct {
	Path    string   `json:"path"`
	Reads   int      `json:"reads"`
	Partial int      `json:"partial"`
	Agents  []string `json:"agents"` // distintos, en orden alfabético
}

// ReadsSummary es el resumen de lecturas de una tarea.
type ReadsSummary struct {
	Available    bool      `json:"available"`
	Files        int       `json:"files"`
	Reads        int       `json:"reads"`
	Partial      int       `json:"partial"`
	CrossRereads int       `json:"cross_rereads"`
	CrossPct     float64   `json:"cross_pct"`
	SharedFiles  int       `json:"shared_files"`
	SharedPct    float64   `json:"shared_pct"`
	Rows         []ReadRow `json:"rows,omitempty"`
}

// SummarizeReads agrega las lecturas (R14, R15). Available = len(evs) > 0.
func SummarizeReads(evs []ReadEvent) ReadsSummary {
	sorted := append([]ReadEvent(nil), evs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TS.Before(sorted[j].TS) })
	s := ReadsSummary{Available: len(evs) > 0, Reads: len(evs)}
	seen := map[string]map[string]bool{}
	rows := map[string]*ReadRow{}
	for _, ev := range sorted {
		set := seen[ev.Path]
		if set == nil {
			set = map[string]bool{}
			seen[ev.Path] = set
			rows[ev.Path] = &ReadRow{Path: ev.Path}
		}
		for a := range set {
			if a != ev.Agent {
				s.CrossRereads++
				break
			}
		}
		row := rows[ev.Path]
		row.Reads++
		if ev.Partial {
			row.Partial++
			s.Partial++
		}
		if !set[ev.Agent] {
			set[ev.Agent] = true
			row.Agents = append(row.Agents, ev.Agent)
		}
	}
	for _, r := range rows {
		sort.Strings(r.Agents)
		s.Rows = append(s.Rows, *r)
		if len(r.Agents) >= 2 {
			s.SharedFiles++
		}
	}
	s.Files = len(s.Rows)
	sort.Slice(s.Rows, func(i, j int) bool {
		a, b := s.Rows[i], s.Rows[j]
		if a.Reads != b.Reads {
			return a.Reads > b.Reads
		}
		if len(a.Agents) != len(b.Agents) {
			return len(a.Agents) > len(b.Agents)
		}
		return a.Path < b.Path
	})
	s.CrossPct = pct(s.CrossRereads, s.Reads)
	s.SharedPct = pct(s.SharedFiles, s.Files)
	return s
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return math.Round(float64(n)/float64(d)*1000) / 10
}
