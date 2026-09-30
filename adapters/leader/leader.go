// Package leader guarda el cuerpo común con el que la sesión principal conduce
// el flujo: la skill de Claude Code y el comando /bflow de OpenCode salen de
// él, cada una con su encabezado y el nombre de su herramienta de preguntas.
package leader

import (
	_ "embed"
	"errors"
)

// body es el cuerpo común; lleva marcadores {{nombre}} que Render reemplaza.
//
//go:embed bflow.md
var body []byte

// Render concatena head y el cuerpo común y reemplaza cada {{nombre}} por
// vars[nombre]. Falla si queda un marcador sin reemplazar o sobra una variable.
func Render(head []byte, vars map[string]string) ([]byte, error) {
	return nil, errors.New("leader.Render: sin implementar")
}
