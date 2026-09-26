package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// StatusCache es lo que bflow statusline lee sin tocar el tracker ni git: la
// última tarea tocada y su estado. Lo escribe cada comando que cambia estado.
type StatusCache struct {
	ID     string     `json:"id"`
	Phase  flow.Phase `json:"phase"`
	Gate   string     `json:"gate,omitempty"`
	Since  time.Time  `json:"since"`
	Round  int        `json:"round,omitempty"`
	Tokens int64      `json:"tokens,omitempty"`
}

// StatusCachePath es la ruta de la caché de statusline.
func StatusCachePath(root string) string {
	return filepath.Join(root, ".bflow", "cache", "statusline.json")
}

// ReadStatusCache lee la caché (nil si no hay).
func ReadStatusCache(root string) *StatusCache {
	b, err := os.ReadFile(StatusCachePath(root))
	if err != nil {
		return nil
	}
	var c StatusCache
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	return &c
}

func writeStatusCache(root string, c StatusCache) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	p := StatusCachePath(root)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = store.WriteAtomic(p, b)
}

func (e *Engine) updateStatusCache(rec store.Record) {
	c := StatusCache{ID: rec.Flow.ID, Phase: rec.Flow.Phase, Since: rec.Since, Round: rec.Flow.Round}
	if rec.Flow.Gate != nil {
		c.Gate = string(rec.Flow.Gate.Name)
	}
	if old := ReadStatusCache(e.Cfg.Root); old != nil && old.ID == c.ID {
		c.Tokens = old.Tokens
	}
	writeStatusCache(e.Cfg.Root, c)
}

// AddTokens suma tokens a la tarea en la caché de statusline.
func (e *Engine) AddTokens(id string, n int64) {
	c := ReadStatusCache(e.Cfg.Root)
	if c == nil || c.ID != id {
		rec, err := e.Store.Load(id)
		if err != nil {
			return
		}
		e.updateStatusCache(rec)
		if c = ReadStatusCache(e.Cfg.Root); c == nil {
			return
		}
		c.Tokens = 0
	}
	c.Tokens += n
	writeStatusCache(e.Cfg.Root, *c)
}
