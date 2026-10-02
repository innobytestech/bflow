package engine

import "errors"

// SplitPart es una hija propuesta en la `### División` del Brief.
type SplitPart struct{ Title, Scope string }

const (
	maxSplitParts = 6
	maxSplitTitle = 120
)

// ParseSplit lee la `### División` del Brief. Cualquier violación de las
// reglas (2 a 6 hijas, título y alcance no vacíos, título único y de hasta
// 120 runas) devuelve *flow.Rejection con código split_invalid.
func ParseSplit(brief string) ([]SplitPart, error) {
	return nil, errors.New("no implementado")
}
