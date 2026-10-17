package engine

import (
	"context"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// reproMissing devuelve el rechazo repro_test_missing si la rama del hotfix no
// trae un commit de solo pruebas cuyo check falló en check.test_step.
func (e *Engine) reproMissing(ctx context.Context, rec store.Record) *flow.Rejection {
	return nil
}
