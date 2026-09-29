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
	"time"

	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/metrics"
)

// Agent agrupa lo que bflow usa de Claude Code.
type Agent struct{}

// Name identifica la herramienta en las métricas.
func (Agent) Name() string { return "claude" }

// ParsePreToolUse ver hook.go.
func (Agent) ParsePreToolUse(raw []byte) (guard.Action, string, bool) { return ParsePreToolUse(raw) }

// TokenSource dice qué transcript leer al terminar un turno o un subagente. En
// Stop es el de la sesión principal (agent vacío); en SubagentStop, solo el
// del subagente, para no mezclar sus tokens con los de la sesión.
func (Agent) TokenSource(raw []byte) (path, agent string) {
	var in struct {
		TranscriptPath      string `json:"transcript_path"`
		AgentTranscriptPath string `json:"agent_transcript_path"`
		AgentType           string `json:"agent_type"`
		AgentID             string `json:"agent_id"`
	}
	_ = json.Unmarshal(raw, &in)
	if in.AgentType == "" && in.AgentID == "" {
		return in.TranscriptPath, ""
	}
	path = in.AgentTranscriptPath
	if path == "" && in.AgentID != "" && in.TranscriptPath != "" { // versiones que no mandan la ruta
		path = filepath.Join(strings.TrimSuffix(in.TranscriptPath, filepath.Ext(in.TranscriptPath)), "subagents", "agent-"+in.AgentID+".jsonl")
	}
	agent = in.AgentType
	if agent == "" {
		agent = "?"
	}
	return path, agent
}

// ReadUsage devuelve las respuestas nuevas del transcript, con su hora y
// modelo. Claude Code escribe cada respuesta en varias líneas con el mismo id
// de mensaje: se cuenta una vez, con el uso más alto visto. Solo se leen
// líneas completas.
func (Agent) ReadUsage(path string, cur *metrics.Cursor) ([]metrics.Sample, error) {
	if cur.Offsets == nil {
		cur.Offsets = map[string]int64{}
	}
	if cur.Seen == nil {
		cur.Seen = map[string]metrics.Usage{}
	}
	out, err := readFile(path, cur)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return out, err
}

type entry struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func readFile(path string, cur *metrics.Cursor) ([]metrics.Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	off := cur.Offsets[path]
	if st, err := f.Stat(); err == nil && st.Size() < off {
		off = 0 // el archivo se truncó o se reemplazó
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	var out []metrics.Sample
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
		if delta.Total() > 0 {
			ts := e.Timestamp
			if ts.IsZero() {
				ts = time.Now()
			}
			out = append(out, metrics.Sample{TS: ts, Model: e.Message.Model, Usage: delta})
		}
		cur.Seen[e.Message.ID] = metrics.Usage{Input: max(u.Input, prev.Input), Output: max(u.Output, prev.Output),
			CacheRead: max(u.CacheRead, prev.CacheRead), CacheWrite: max(u.CacheWrite, prev.CacheWrite)}
	}
	cur.Offsets[path] = off
	return out, nil
}
