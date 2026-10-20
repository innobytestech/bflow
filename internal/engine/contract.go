package engine

import (
	"context"
	"strings"

	"innobytes.tech/bflow/internal/flow"
)

// contractCheck valida el contrato antes de aceptar CONTRACT_READY. verified
// es true si el último check confirma que las T1 fallan contra los stubs; false
// sin rechazo significa que no se pudo validar y decide la gate contract.
func (e *Engine) contractCheck(ctx context.Context, id string) (verified bool, rej *flow.Rejection) {
	if hollow := hollowTests(e.Cfg.Root, e.taskTests(ctx)); len(hollow) > 0 {
		return false, &flow.Rejection{Code: "contract_hollow", Reason: "el contrato tiene pruebas que se saltan siempre o no tienen cuerpo; al aprobarlo se congelan y ya no se pueden completar:" + "\n" +
			strings.Join(hollow, "\n") + "\n" + "Escribe su cuerpo real (preparar, actuar, verificar contra las firmas) para que fallen contra los stubs, y reporta otra vez."}
	}
	if e.Contract != nil {
		return false, &flow.Rejection{Code: "contract_unimplemented", Reason: "sin implementar"}
	}
	return false, nil
}

// contractTestNames son los nombres con los que una línea de fallo nombra una
// prueba T1: el nombre base de cada archivo y cada función Test* de Go.
func contractTestNames(root string, files []string) []string {
	return nil
}

// Pause marca la tarea para parar antes de quality.
func (e *Engine) Pause(ctx context.Context, id string) (Outcome, error) {
	return e.apply(ctx, id, flow.Event{Kind: flow.EvPause}, "")
}
