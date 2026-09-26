package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/metrics"
)

// Agent agrupa lo que bflow usa de Claude Code.
type Agent struct{}

// ParsePreToolUse ver hook.go.
func (Agent) ParsePreToolUse(raw []byte) (guard.Action, string, bool) { return ParsePreToolUse(raw) }

// TranscriptPath saca la ruta del transcript de la entrada de un hook.
func (Agent) TranscriptPath(raw []byte) string {
	var in struct {
		TranscriptPath string `json:"transcript_path"`
	}
	_ = json.Unmarshal(raw, &in)
	return in.TranscriptPath
}

// ReadUsage suma los tokens nuevos del transcript de la sesión y de los de sus
// subagentes (<sesión>/subagents/*.jsonl). Claude Code escribe cada respuesta
// en varias líneas con el mismo id de mensaje: se cuenta una vez, con el uso
// más alto visto. Solo se leen líneas completas.
func (Agent) ReadUsage(path string, cur *metrics.Cursor) (metrics.Usage, error) {
	if cur.Offsets == nil {
		cur.Offsets = map[string]int64{}
	}
	if cur.Seen == nil {
		cur.Seen = map[string]metrics.Usage{}
	}
	files := []string{path}
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, filepath.Ext(path)), "subagents", "*.jsonl"))
	files = append(files, subs...)
	var total metrics.Usage
	for _, f := range files {
		u, err := readFile(f, cur)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return total, err
		}
		total.Add(u)
	}
	return total, nil
}

type entry struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Usage struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func readFile(path string, cur *metrics.Cursor) (metrics.Usage, error) {
	var total metrics.Usage
	f, err := os.Open(path)
	if err != nil {
		return total, err
	}
	defer f.Close()
	off := cur.Offsets[path]
	if st, err := f.Stat(); err == nil && st.Size() < off {
		off = 0 // el archivo se truncó o se reemplazó
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return total, err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil { // línea incompleta: se lee en la próxima pasada
			break
		}
		off += int64(len(line))
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var e entry
		if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.Message.ID == "" {
			continue
		}
		u := metrics.Usage{Input: e.Message.Usage.Input, Output: e.Message.Usage.Output,
			CacheRead: e.Message.Usage.CacheRead, CacheWrite: e.Message.Usage.CacheWrite}
		prev := cur.Seen[e.Message.ID]
		delta := metrics.Usage{Input: max(0, u.Input-prev.Input), Output: max(0, u.Output-prev.Output),
			CacheRead: max(0, u.CacheRead-prev.CacheRead), CacheWrite: max(0, u.CacheWrite-prev.CacheWrite)}
		total.Add(delta)
		cur.Seen[e.Message.ID] = metrics.Usage{Input: max(u.Input, prev.Input), Output: max(u.Output, prev.Output),
			CacheRead: max(u.CacheRead, prev.CacheRead), CacheWrite: max(u.CacheWrite, prev.CacheWrite)}
	}
	cur.Offsets[path] = off
	return total, nil
}
