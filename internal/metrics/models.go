package metrics

import (
	"time"

	"innobytes.tech/bflow/internal/store"
)

// ModelSet es agente → modelo normalizado.
type ModelSet map[string]string

// NormalizeModel: minúsculas, sin prefijo proveedor/, y la familia opus, sonnet
// o haiku si el nombre la contiene; "" se muestra como "heredado".
func NormalizeModel(m string) string { return m } // stub del contrato (GH-18 T3)

// TaskModels da los modelos de la tarea y su origen: "start" | "tokens" | "".
func TaskModels(id string, entries []store.Entry) (ModelSet, string) { return nil, "x" }

// Matches (R11): comparten al menos un agente y en los compartidos el modelo es igual.
func (a ModelSet) Matches(b ModelSet) bool { return true }

// String es "agente=modelo,..." ordenado por agente.
func (a ModelSet) String() string { return "" }

// ModelGroup es un grupo de tareas con la misma configuración de modelos.
type ModelGroup struct {
	Models     ModelSet           `json:"models"`
	Tasks      []string           `json:"tasks"` // IDs, más reciente primero, máximo ModelWindow
	Rejections map[string]float64 `json:"rejections"`
	Rounds     float64            `json:"rounds"`
	Hotfixes   float64            `json:"hotfixes"`
}

// ModelChange es un agente que cambió de modelo.
type ModelChange struct {
	Agent string `json:"agent"`
	From  string `json:"from"` // anterior: al que volver
	To    string `json:"to"`   // vigente
}

// ModelVerdict compara la configuración vigente con la anterior.
type ModelVerdict struct {
	Verdict  string        `json:"verdict"` // "sube" | "sin_regresion" | "sin_veredicto" | "sin_datos"
	Current  ModelGroup    `json:"current"`
	Previous *ModelGroup   `json:"previous,omitempty"`
	Changes  []ModelChange `json:"changes,omitempty"`
	Rose     []string      `json:"rose,omitempty"` // "rondas 1.7 > 1.0", "rechazos spec 0.7 > 0.3"
}

// ModelWindow es cuántas tareas por grupo exige la comparación.
const ModelWindow = 3

// CompareModels aplica R8-R14 sobre el log.
func CompareModels(entries []store.Entry, now time.Time) ModelVerdict {
	return ModelVerdict{} // stub del contrato (GH-18 T3)
}
