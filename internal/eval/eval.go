// Package eval mide si un oficio del reviewer atrapa los defectos de casos
// conocidos. No lanza agentes: prepara un caso, le pide a la sesión principal
// que lance bflow-eval-reviewer, califica su review-map y escribe el resultado.
package eval

import (
	"context"
	"errors"
	"time"

	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/store"
)

const Tolerance = 5               // líneas de holgura del puntaje
const AgentName = "eval-reviewer" // nombre sin prefijo; subagente flow.SubagentPrefix+AgentName
const CraftPath = "internal/agents/craft/reviewer.md"

var errTodo = errors.New("sin implementar")

type Defect struct {
	File  string `yaml:"file" json:"file"`
	Lines string `yaml:"lines" json:"lines"`
	What  string `yaml:"what" json:"what"`
}

type Case struct {
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

func (r *Rejection) Error() string { return r.Code + ": " + r.Reason }

// Git son las operaciones que el eval pide a git; las implementa adapters/vcs/git.
type Git interface {
	ResolveCommit(ctx context.Context, ref string) (string, error) // SHA completo; error si no es commit
	ShowFile(ctx context.Context, ref, path string) ([]byte, error)
	WorktreeAdd(ctx context.Context, dir, sha string) error // --detach
	WorktreeRemove(ctx context.Context, dir string) error   // --force, luego prune
	IsIgnored(ctx context.Context, path string) (bool, error)
}

func LoadCases(root string, names []string) (cases []Case, skipped []string, err error) {
	return nil, nil, errTodo
}

func (c Case) Problems(ctx context.Context, g Git) []string { return []string{"sin implementar"} }

func ParseLines(s string) (from, to int, err error) { return 0, 0, errTodo }

type Finding struct {
	File     string
	From, To int
}

// Findings: docs es nombre .md → contenido.
func Findings(note string, docs map[string]string, worktree string) []Finding { return nil }

type Score struct {
	Caught         []Defect `json:"caught"`
	Missed         []Defect `json:"missed"`
	FalsePositives []string `json:"false_positives"`
}

func ScoreCase(defects []Defect, found []Finding) Score { return Score{} }

func Tokens(bflowDir string, from, to time.Time) (u metrics.Usage, model string, ok bool) {
	return metrics.Usage{}, "", false
}

type Opts struct {
	Craft string
	Cases []string
	Abort bool
}

type Spawn struct {
	Case   string
	Args   map[string]string
	Report string
}

type Step struct {
	Spawn    *Spawn  // caso a lanzar; nil si terminó
	Result   *Result // al cerrar
	Path     string  // ruta del resultado, relativa a la raíz
	Skipped  []string
	Warnings []string
}

type Runner struct {
	Root    string
	Git     Git
	Now     func() time.Time
	Model   string // modelo configurado de agents.reviewer (respaldo de R10)
	Version string
}

func (r *Runner) Next(ctx context.Context, o Opts) (Step, error) { return Step{}, errTodo }

func (r *Runner) Report(ctx context.Context, caseName, verdict, note string) (CaseResult, error) {
	return CaseResult{}, errTodo
}

func FromFixes(root string, log []store.Entry) (created []string, warnings []string, err error) {
	return nil, nil, errTodo
}

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
