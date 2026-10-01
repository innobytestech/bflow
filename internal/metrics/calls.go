package metrics

import (
	"io"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
)

// CallsFile es el archivo de la tarea con una línea por muestra del modelo.
const CallsFile = "calls.jsonl"

// Call es una muestra de una llamada del modelo tal como se guarda: puede ser
// un delta de una respuesta que otra pasada del hook ya había leído en parte.
type Call struct {
	TS         time.Time  `json:"ts"`
	Run        string     `json:"run"`   // RunKey
	Tool       string     `json:"tool"`  // claude | opencode
	Agent      string     `json:"agent"` // MainSession o nombre sin prefijo
	Phase      flow.Phase `json:"phase"`
	Model      string     `json:"model,omitempty"`
	Msg        string     `json:"msg"`
	Input      int64      `json:"input"`
	CacheWrite int64      `json:"cache_write"`
	CacheRead  int64      `json:"cache_read"`
	Output     int64      `json:"output"`
}

// Context es lo que la llamada leyó como contexto: Input + CacheRead + CacheWrite.
func (c Call) Context() int64 { return c.Input + c.CacheRead + c.CacheWrite }

// New es igual que Usage.New: Input + CacheWrite + Output.
func (c Call) New() int64 { return c.Input + c.CacheWrite + c.Output }

// CallRow es una llamada ya fusionada, con su lugar en la corrida.
type CallRow struct {
	N int `json:"n"`
	Call
	Context int64 `json:"context"`
	AccRead int64 `json:"acc_read"`
	AccNew  int64 `json:"acc_new"`
}

// Run es una corrida: un transcript de un agente o de la sesión principal.
type Run struct {
	Run          string     `json:"run"`
	Agent        string     `json:"agent"`
	Label        string     `json:"label"` // "implementer", "implementer #2", "sesión principal"
	Phase        flow.Phase `json:"phase"` // de la primera fila
	Rows         []CallRow  `json:"rows"`
	Total        Usage      `json:"total"` // Calls = len(Rows), MaxContext = máximo
	FinalContext int64      `json:"final_context"`
	Last         time.Time  `json:"last"`
}

// RunKey identifica la corrida de un transcript: tool + ":" + base sin extensión.
func RunKey(tool, path string) string { return "" }

// ParseCalls lee calls.jsonl y salta las líneas malas (R6).
func ParseCalls(r io.Reader) []Call { return nil }

// Runs fusiona (R3), agrupa y acumula (R4); orden por primera llamada.
func Runs(calls []Call) []Run { return nil }

// Exact da una cifra con separador de miles: 17711 -> "17,711".
func Exact(n int64) string { return "" }

// Phases da una fase por muestra, con la regla de Allot (R2).
func Phases(entries []store.Entry, id, agent string, samples []Sample) []flow.Phase {
	return nil
}
