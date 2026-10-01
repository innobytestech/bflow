package opencode

import (
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

// TokenSources dice qué archivos leer en un session.idle (T3).
func (Agent) TokenSources(raw []byte, cacheDir string) ([]metrics.TokenSource, bool) {
	return nil, false
}

// ReadUsage lee las líneas nuevas de un archivo de uso (T3).
func (Agent) ReadUsage(path string, cur *metrics.Cursor) ([]metrics.Sample, error) { return nil, nil }

// Prune borra los archivos leídos por completo y viejos (T3).
func (Agent) Prune(cacheDir string, cur *metrics.Cursor, olderThan time.Duration) {}
