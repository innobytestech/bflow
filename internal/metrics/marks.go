package metrics

import (
	"path/filepath"
	"time"
)

// UnassignedLabel es cómo las vistas llaman a las llamadas sin tarea.
const UnassignedLabel = "sin tarea"

// Mark dice que una sesión condujo una tarea desde TS.
type Mark struct {
	TS      time.Time `json:"ts"`
	Session string    `json:"session"`
	ID      string    `json:"id"`
}

// MarksPath es el archivo de marcas.
func MarksPath(bflowDir string) string { return filepath.Join(bflowDir, "cache", "sessions.jsonl") }

// UnassignedPath es el calls.jsonl de las llamadas sin tarea.
func UnassignedPath(bflowDir string) string { return filepath.Join(bflowDir, "metrics", "calls.jsonl") }

// AppendMark anota una marca bajo path+".lock" (1 s, obsoleto a 1 min), una línea, O_APPEND.
func AppendMark(path string, m Mark) error { return nil } // T2

// ReadMarks lee las marcas; ignora líneas rotas y un archivo ausente da nil.
func ReadMarks(path string) []Mark { return nil } // T2

// PruneMarks borra las marcas con más de olderThan bajo el mismo lock.
func PruneMarks(path string, olderThan time.Duration, now time.Time) error { return nil } // T2

// TaskFor aplica R5/R6: la tarea a la que va una llamada de src a la hora ts; "" = sin tarea.
func TaskFor(marks []Mark, src TokenSource, ts time.Time) string { return "" } // T2

// UnassignedUsage suma las llamadas sin tarea de <bflowDir>/metrics/calls.jsonl.
func UnassignedUsage(bflowDir string) Usage { return Usage{} } // T5
