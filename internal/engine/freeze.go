package engine

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/guard"
)

// FrozenFile guarda las pruebas congeladas de una tarea.
const FrozenFile = "frozen-tests.json"

// freezeTests congela las pruebas que la tarea escribió o tocó hasta aprobar
// el contrato: desde ahí el implementer hace pasar esas pruebas sin cambiarlas.
func (e *Engine) freezeTests(ctx context.Context, id string) (int, error) {
	if e.Git == nil {
		return 0, nil
	}
	base := e.Cfg.VCS.BaseBranch
	if base != "" && e.Cfg.VCS.Remote != "" {
		base = e.Cfg.VCS.Remote + "/" + base
	}
	var files []string
	if base != "" {
		if d, err := e.Git.DiffNames(ctx, base); err == nil {
			files = append(files, d...)
		}
	}
	if d, err := e.Git.Dirty(ctx, nil); err == nil {
		files = append(files, d...)
	}
	var frozen []string
	for _, f := range files {
		f = strings.ReplaceAll(f, `\`, "/")
		if guard.MatchesTest(e.Cfg.Guard.TestPatterns, f) && !slices.Contains(frozen, f) {
			frozen = append(frozen, f)
		}
	}
	slices.Sort(frozen)
	b, err := json.Marshal(frozen)
	if err != nil {
		return 0, err
	}
	return len(frozen), e.Store.WriteFile(id, FrozenFile, b)
}

// Frozen devuelve las pruebas congeladas de una tarea.
func (e *Engine) Frozen(id string) []string {
	b, err := e.Store.ReadFile(id, FrozenFile)
	if err != nil {
		return nil
	}
	var out []string
	_ = json.Unmarshal(b, &out)
	return out
}
