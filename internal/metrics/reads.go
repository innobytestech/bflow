package metrics

import (
	"io"
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
func ParseReadEvents(r io.Reader) []ReadEvent { panic("TODO") }

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
func SummarizeReads(evs []ReadEvent) ReadsSummary { panic("TODO") }
