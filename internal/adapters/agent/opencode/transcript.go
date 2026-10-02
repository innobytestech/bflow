package opencode

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"innobytes.tech/bflow/internal/metrics"
)

// usageLine es una línea de .bflow/cache/opencode/<sessionID>.jsonl: solo
// metadatos de un mensaje de asistente terminado.
type usageLine struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionID"`
	ParentID   string `json:"parentID"`
	Agent      string `json:"agent"`
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Created    int64  `json:"created"`
	Completed  int64  `json:"completed"`
	Tokens     struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

var sessionRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// TokenSources dice qué archivos leer en un session.idle. En el de una sesión
// principal (sin parentID) son todos los *.jsonl de cacheDir, porque los hijos
// no siempre tienen un idle propio; en el de un hijo, solo el suyo. El agente de
// cada archivo sale de su primera línea completa: "" la principal, el agente del
// hijo o "?" si no se supo.
func (Agent) TokenSources(raw []byte, cacheDir string) ([]metrics.TokenSource, bool) {
	var in struct {
		SessionID string `json:"sessionID"`
		ParentID  string `json:"parentID"`
	}
	if json.Unmarshal(raw, &in) != nil || !sessionRe.MatchString(in.SessionID) {
		return nil, false
	}
	main := in.ParentID == ""
	paths := []string{filepath.Join(cacheDir, in.SessionID+".jsonl")}
	if main {
		paths, _ = filepath.Glob(filepath.Join(cacheDir, "*.jsonl"))
	}
	var out []metrics.TokenSource
	for _, p := range paths {
		l, ok := firstLine(p)
		if !ok {
			continue
		}
		agent := ""
		if l.ParentID != "" {
			if agent = l.Agent; agent == "" {
				agent = "?"
			}
		}
		out = append(out, metrics.TokenSource{Path: p, Agent: agent})
	}
	return out, main
}

// firstLine lee la primera línea completa de un archivo de uso.
func firstLine(path string) (usageLine, bool) {
	var l usageLine
	f, err := os.Open(path)
	if err != nil {
		return l, false
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 1<<16).ReadBytes('\n')
	if err != nil || json.Unmarshal(line, &l) != nil {
		return l, false
	}
	return l, true
}

// ReadUsage lee las líneas nuevas de un archivo de uso: solo líneas completas,
// una llamada por id con el uso más alto visto.
func (Agent) ReadUsage(path string, cur *metrics.Cursor) ([]metrics.Sample, error) {
	if cur.Offsets == nil {
		cur.Offsets = map[string]int64{}
	}
	if cur.Seen == nil {
		cur.Seen = map[string]metrics.Usage{}
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
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
		var l usageLine
		if json.Unmarshal(line, &l) != nil || l.ID == "" {
			continue
		}
		key := "opencode:" + l.ID
		t := l.Tokens
		u := metrics.Usage{Input: t.Input, Output: t.Output + t.Reasoning, CacheRead: t.Cache.Read, CacheWrite: t.Cache.Write}
		prev, seen := cur.Seen[key]
		delta := metrics.Usage{Input: max(0, u.Input-prev.Input), Output: max(0, u.Output-prev.Output),
			CacheRead: max(0, u.CacheRead-prev.CacheRead), CacheWrite: max(0, u.CacheWrite-prev.CacheWrite),
			MaxContext: u.Input + u.CacheRead + u.CacheWrite}
		delta.LastContext = max(u.Input, prev.Input) + max(u.CacheRead, prev.CacheRead) +
			max(u.CacheWrite, prev.CacheWrite) + max(u.Output, prev.Output)
		if !seen {
			delta.Calls = 1
		}
		if delta.Total() > 0 {
			ts := time.UnixMilli(l.Completed)
			if l.Completed == 0 {
				ts = time.Now()
			}
			out = append(out, metrics.Sample{TS: ts, Model: l.ProviderID + "/" + l.ModelID, Msg: key, Usage: delta})
		}
		cur.Seen[key] = metrics.Usage{Input: max(u.Input, prev.Input), Output: max(u.Output, prev.Output),
			CacheRead: max(u.CacheRead, prev.CacheRead), CacheWrite: max(u.CacheWrite, prev.CacheWrite)}
	}
	cur.Offsets[path] = off
	return out, nil
}

// Prune borra los *.jsonl leídos por completo y sin cambios desde hace olderThan,
// junto con su offset.
func (Agent) Prune(cacheDir string, cur *metrics.Cursor, olderThan time.Duration) {
	paths, _ := filepath.Glob(filepath.Join(cacheDir, "*.jsonl"))
	for _, p := range paths {
		off, ok := cur.Offsets[p]
		st, err := os.Stat(p)
		if !ok || err != nil || off < st.Size() || time.Since(st.ModTime()) < olderThan {
			continue
		}
		if os.Remove(p) == nil {
			delete(cur.Offsets, p)
		}
	}
}
