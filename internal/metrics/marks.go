package metrics

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"innobytes.tech/bflow/internal/store"
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
func AppendMark(path string, m Mark) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := store.EnsureDirFor(path); err != nil {
		return err
	}
	unlock, err := store.LockFile(path+".lock", time.Second, time.Minute)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// ReadMarks lee las marcas; ignora líneas rotas y un archivo ausente da nil.
func ReadMarks(path string) []Mark {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Mark
	for _, line := range bytes.Split(b, []byte("\n")) {
		var m Mark
		if len(bytes.TrimSpace(line)) > 0 && json.Unmarshal(line, &m) == nil && m.Session != "" && m.ID != "" {
			out = append(out, m)
		}
	}
	return out
}

// PruneMarks borra las marcas con más de olderThan bajo el mismo lock.
func PruneMarks(path string, olderThan time.Duration, now time.Time) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	unlock, err := store.LockFile(path+".lock", time.Second, time.Minute)
	if err != nil {
		return err
	}
	defer unlock()
	var buf bytes.Buffer
	for _, m := range ReadMarks(path) {
		if now.Sub(m.TS) > olderThan {
			continue
		}
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		buf.Write(append(b, '\n'))
	}
	return store.WriteAtomic(path, buf.Bytes())
}

// lastMark es la tarea de la última marca de la sesión con ts <= at; first
// (si no hay previa) es la primera marca de la sesión.
func lastMark(marks []Mark, session string, at time.Time) (prev, first string) {
	var pts time.Time
	for _, m := range marks {
		if m.Session != session {
			continue
		}
		if first == "" {
			first = m.ID
		}
		if !m.TS.After(at) && (prev == "" || !m.TS.Before(pts)) {
			prev, pts = m.ID, m.TS
		}
	}
	return prev, first
}

// TaskFor aplica R5/R6: la tarea a la que va una llamada de src a la hora ts; "" = sin tarea.
func TaskFor(marks []Mark, src TokenSource, ts time.Time) string {
	prev, first := lastMark(marks, src.Session, ts)
	if src.Parent == "" {
		return prev
	}
	if prev != "" {
		return prev
	}
	if first != "" {
		return first
	}
	prev, _ = lastMark(marks, src.Parent, ts)
	return prev
}

// UnassignedUsage suma las llamadas sin tarea de <bflowDir>/metrics/calls.jsonl.
func UnassignedUsage(bflowDir string) Usage {
	f, err := os.Open(UnassignedPath(bflowDir))
	if err != nil {
		return Usage{}
	}
	defer f.Close()
	var u Usage
	for _, r := range Runs(ParseCalls(f)) {
		u.Add(r.Total)
	}
	return u
}
