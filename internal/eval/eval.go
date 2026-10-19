// Package eval mide si un oficio del reviewer atrapa los defectos de casos
// conocidos. No lanza agentes: prepara un caso, le pide a la sesión principal
// que lance bflow-eval-reviewer, califica su review-map y escribe el resultado.
package eval

import (
	"context"
	"time"

	"innobytes.tech/bflow/internal/metrics"
)

// Tolerance son las líneas de holgura del puntaje: un hallazgo a ±5 líneas de
// un defecto cuenta como atrapado.
const Tolerance = 5

// AgentName es el nombre del subagente del eval sin prefijo; el agente generado
// se llama flow.SubagentPrefix+AgentName.
const AgentName = "eval-reviewer"

// CraftPath es el archivo del oficio del reviewer que el agente del eval lee
// del ref que recibe en cada corrida.
const CraftPath = "internal/agents/craft/reviewer.md"

// Defect es un defecto conocido de un caso: archivo, rango de líneas (N o N-M)
// y una descripción para personas.
type Defect struct {
	File  string `yaml:"file" json:"file"`
	Lines string `yaml:"lines" json:"lines"`
	What  string `yaml:"what" json:"what"`
}

// Case es un caso del eval leído de case.yaml. Base y Head son los commits
// entre los que está el defecto; draft marca los esqueletos que no se corren.
type Case struct {
	parseErr error // case.yaml ilegible: Problems lo informa

	Name    string   `yaml:"-" json:"case"`
	Source  string   `yaml:"source" json:"source,omitempty"`
	Hotfix  string   `yaml:"hotfix,omitempty" json:"hotfix,omitempty"`
	Base    string   `yaml:"base" json:"base"`
	Head    string   `yaml:"head" json:"head"`
	Spec    string   `yaml:"spec,omitempty" json:"spec,omitempty"`
	Draft   bool     `yaml:"draft,omitempty" json:"-"`
	Defects []Defect `yaml:"defects" json:"defects"`
}

// Rejection es un rechazo de reglas (código 2): Code y Reason van al envelope.
type Rejection struct{ Code, Reason string }

// Error devuelve "código: motivo", para que Rejection sea un error de Go.
func (r *Rejection) Error() string { return r.Code + ": " + r.Reason }

// Git son las operaciones que el eval pide a git; las implementa adapters/vcs/git.
type Git interface {
	ResolveCommit(ctx context.Context, ref string) (string, error) // SHA completo; error si no es commit
	ShowFile(ctx context.Context, ref, path string) ([]byte, error)
	WorktreeAdd(ctx context.Context, dir, sha string) error // --detach
	WorktreeRemove(ctx context.Context, dir string) error   // --force, luego prune
	IsIgnored(ctx context.Context, path string) (bool, error)
}

// Finding es una referencia ruta:N o ruta:N-M que hizo el agente, normalizada
// a ruta relativa al worktree con "/".
type Finding struct {
	File     string
	From, To int
}

// Score es el resultado de calificar un caso: defectos atrapados y no
// atrapados, y los hallazgos que no coinciden con ninguno.
type Score struct {
	Caught         []Defect `json:"caught"`
	Missed         []Defect `json:"missed"`
	FalsePositives []string `json:"false_positives"`
}

// Opts son las banderas de una corrida: oficio (ref git o vacío), casos
// elegidos y si se cancela.
type Opts struct {
	Craft string
	Cases []string
	Abort bool
}

// Spawn es el caso que la sesión principal debe lanzar: los args del agente y
// el comando de report con el que el agente reporta.
type Spawn struct {
	Case   string
	Args   map[string]string
	Report string
}

// Step es lo que devuelve Runner.Next: el caso a lanzar, o el resultado al
// cerrar la corrida, con avisos y casos omitidos.
type Step struct {
	Spawn    *Spawn  // caso a lanzar; nil si terminó
	Result   *Result // al cerrar
	Path     string  // ruta del resultado, relativa a la raíz
	Skipped  []string
	Warnings []string
}

// Runner corre el eval de un repo. Root es la raíz del repo; Git, Now y
// Version son inyectables para las pruebas.
type Runner struct {
	Root    string
	Git     Git
	Now     func() time.Time
	Model   string // modelo configurado de agents.reviewer (respaldo si el agente no lo reporta)
	Version string
}

// CaseResult es el resultado de un caso: veredicto, calificación, tiempo y
// tokens. TokensAvailable es false si no hubo llamadas del agente que contar.
type CaseResult struct {
	Case    string `json:"case"`
	Verdict string `json:"verdict"`
	Note    string `json:"note,omitempty"`
	Score
	Spawned         time.Time     `json:"spawned"`
	Reported        time.Time     `json:"reported"`
	Seconds         int64         `json:"seconds"`
	Model           string        `json:"model"`
	Tokens          metrics.Usage `json:"tokens"`
	TokensAvailable bool          `json:"tokens_available"`
}

// Result es el JSON que se escribe en .bflow/evals/<fecha>.json al cerrar una
// corrida: oficio, modelo, casos y totales.
type Result struct {
	Kind    string `json:"kind"` // "reviewer"
	Run     string `json:"run"`
	Version string `json:"version"`
	Craft   struct {
		Ref    string `json:"ref"`
		SHA256 string `json:"sha256"`
	} `json:"craft"`
	Model  string       `json:"model"`
	Cases  []CaseResult `json:"cases"`
	Totals struct {
		Defects        int           `json:"defects"`
		Caught         int           `json:"caught"`
		Missed         int           `json:"missed"`
		FalsePositives int           `json:"false_positives"`
		Tokens         metrics.Usage `json:"tokens"`
		Seconds        int64         `json:"seconds"`
	} `json:"totals"`
}
