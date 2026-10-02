// Package guard decide si una acción de un agente se permite. Hace cumplir
// con código lo que en el harness anterior era prosa ("no edites pruebas",
// "no hagas git reset --hard"). Lo llaman los hooks de la herramienta de
// agente antes de cada uso de herramienta, así que es rápido y no pregunta.
package guard

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"innobytes.tech/bflow/internal/flow"
)

// Tipos de herramienta, ya traducidos por el adaptador del agente.
const (
	Bash  = "bash"
	Edit  = "edit"
	Write = "write"
	Read  = "read" // solo la registra el reviewer (guard --reads); Evaluate la permite
)

// Action es lo que el agente quiere hacer.
type Action struct {
	Tool     string `json:"tool"`
	Command  string `json:"command,omitempty"`
	Path     string `json:"path,omitempty"`
	Subagent bool   `json:"subagent,omitempty"`
	Agent    string `json:"agent,omitempty"` // agent_type del hook; "" en la sesión principal
}

// Context es lo que el guard sabe del repo y de la tarea activa.
type Context struct {
	Root           string
	Protected      []string // ramas protegidas
	ProtectedPaths []string // código: la sesión principal no lo edita con StrictLeader
	TestPatterns   []string
	ForbidCoauthor bool
	StrictLeader   bool
	MaxDiffLines   int
	DiffLines      func() (int, error) // se llama solo al commitear

	// HumanFiles: archivos que solo edita una persona (las vulnerabilidades
	// aceptadas en accept_file de check.steps).
	HumanFiles []string

	Phase  flow.Phase // fase de la tarea activa ("" si no hay)
	Frozen []string   // pruebas congeladas al aprobar el contrato
}

// Decision es el resultado.
type Decision struct {
	Allow  bool   `json:"allow"`
	Rule   string `json:"rule,omitempty"`
	Reason string `json:"reason,omitempty"`
}

var allow = Decision{Allow: true}

func deny(rule, format string, a ...any) Decision {
	return Decision{Rule: rule, Reason: fmt.Sprintf(format, a...)}
}

// frozenPhases son las fases en las que las pruebas del contrato no se tocan.
var frozenPhases = []flow.Phase{flow.Implementing, flow.Paused, flow.Quality, flow.Documenting, flow.Walkthrough}

// Evaluate aplica las reglas en orden.
func Evaluate(a Action, c Context) Decision {
	switch a.Tool {
	case Bash:
		return bash(a, c)
	case Edit, Write:
		return edit(a, c)
	}
	return allow
}

// Segments separa un comando en sus segmentos (&&, ||, ; y |), recortados y sin
// vacíos. Lo usan TaskScoped, bash y review.FromAction.
func Segments(cmd string) []string {
	var out []string
	for _, seg := range segSplit.Split(cmd, -1) {
		if s := strings.TrimSpace(seg); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var (
	segSplit     = regexp.MustCompile(`&&|\|\||;|\|`)
	resetHard    = regexp.MustCompile(`^git\s+(-\S+\s+)*reset\b.*--hard`)
	cleanForce   = regexp.MustCompile(`^git\s+(-\S+\s+)*clean\b.*\s-[a-zA-Z]*f`)
	checkoutDash = regexp.MustCompile(`^git\s+(-\S+\s+)*checkout\b.*\s--(\s|$)`)
	restore      = regexp.MustCompile(`^git\s+(-\S+\s+)*restore\b`)
	stashDrop    = regexp.MustCompile(`^git\s+(-\S+\s+)*stash\s+(drop|clear)\b`)
	pushRe       = regexp.MustCompile(`^git\s+(-\S+\s+)*push\b(.*)$`)
	forceRe      = regexp.MustCompile(`(^|\s)(--force(-with-lease)?|-f)(\s|$|=)`)
	commitRe     = regexp.MustCompile(`^git\s+(-\S+\s+)*commit\b`)
	coauthorRe   = regexp.MustCompile(`(?i)co-authored-by`)
	rmRe         = regexp.MustCompile(`^(rm|del|git\s+rm|mv|git\s+mv)\s`)

	// Acciones que le tocan a bflow mientras hay una tarea en curso.
	ghPRCreate  = regexp.MustCompile(`^gh\s+pr\s+create\b`)
	checkoutNew = regexp.MustCompile(`^git\s+(-\S+\s+)*checkout\s+(.*\s)?-[bB](\s|$)`)
	switchNew   = regexp.MustCompile(`^git\s+(-\S+\s+)*switch\s+(.*\s)?(-[cC]|--create|--force-create)(\s|$)`)
	branchNew   = regexp.MustCompile(`^git\s+(-\S+\s+)*branch\s+[^-\s]`)
	bflowFreeze = regexp.MustCompile(`^bflow(\.exe)?\s+freeze\b`)
)

// HumanOnly son los subcomandos que responden una decisión humana: un
// subagente no los corre (los corre la sesión principal después de preguntar).
var HumanOnly = []string{"approve", "reject", "unblock", "start", "new"}

// BflowSubcommand devuelve el subcomando de bflow de un segmento ya recortado
// ("approve" en `./bin/bflow.exe --json approve X`), o "" si el segmento no
// invoca a bflow.
func BflowSubcommand(seg string) string {
	t := strings.Fields(seg)
	for i := range t {
		t[i] = strings.Trim(t[i], `"'`)
	}
	isAssign := func(w string) bool { return strings.Contains(w, "=") && !strings.HasPrefix(w, "-") }
	for len(t) > 0 && isAssign(t[0]) {
		t = t[1:]
	}
	if len(t) > 0 && strings.ToLower(t[0]) == "env" {
		t = t[1:]
		for len(t) > 0 && (strings.HasPrefix(t[0], "-") || isAssign(t[0])) {
			t = t[1:]
		}
	}
	if len(t) == 0 {
		return ""
	}
	parts := func(w string) []string {
		return strings.Split(strings.ReplaceAll(w, `\`, "/"), "/")
	}
	name := func(w string) string {
		ps := parts(strings.TrimRight(strings.ReplaceAll(w, `\`, "/"), "/"))
		return strings.TrimSuffix(strings.ToLower(ps[len(ps)-1]), ".exe")
	}
	switch {
	case name(t[0]) == "bflow":
		t = t[1:]
	case strings.ToLower(t[0]) == "go" && len(t) > 1 && strings.ToLower(t[1]) == "run":
		t = t[2:]
		for len(t) > 0 && strings.HasPrefix(t[0], "-") {
			t = t[1:]
		}
		if len(t) == 0 {
			return ""
		}
		ps := parts(strings.TrimRight(strings.ReplaceAll(t[0], `\`, "/"), "/"))
		if len(ps) < 2 || ps[len(ps)-1] != "bflow" || ps[len(ps)-2] != "cmd" {
			return ""
		}
		t = t[1:]
	default:
		return ""
	}
	for len(t) > 0 && strings.HasPrefix(t[0], "-") {
		t = t[1:]
	}
	if len(t) == 0 {
		return ""
	}
	return strings.ToLower(t[0])
}

// TaskScoped dice si el comando toca algo que bflow maneja por tarea (rama,
// PR): el guard necesita saber si hay una tarea en curso.
func TaskScoped(cmd string) bool {
	for _, s := range Segments(cmd) {
		if ghPRCreate.MatchString(s) || checkoutNew.MatchString(s) || switchNew.MatchString(s) || branchNew.MatchString(s) {
			return true
		}
	}
	return false
}

func bash(a Action, c Context) Decision {
	for _, s := range Segments(a.Command) {
		sub := BflowSubcommand(s)
		switch {
		case resetHard.MatchString(s), cleanForce.MatchString(s), checkoutDash.MatchString(s), stashDrop.MatchString(s):
			return deny("git_destructive", "`%s` descarta trabajo sin forma de recuperarlo. Si necesitas deshacer algo, haz un commit que lo revierta o pregunta.", s)
		case restore.MatchString(s) && !strings.Contains(s, "--staged"):
			return deny("git_destructive", "`%s` descarta cambios del árbol de trabajo. Para quitar algo del stage usa git restore --staged.", s)
		case sub == "freeze":
			return deny("human_only", "bflow freeze acepta cambios a pruebas congeladas: lo corre una persona desde su terminal. Si una prueba está mal, reporta NEEDS_DECISION.")
		case a.Subagent && slices.Contains(HumanOnly, sub):
			return deny("human_only", "`bflow %s` responde una decisión de una persona: lo corre la sesión principal después de preguntarle. Termina tu parte con `bflow report` (o NEEDS_DECISION).", sub)
		case c.Phase != "" && ghPRCreate.MatchString(s):
			return deny("bflow_pr", "el PR lo abre bflow al aprobar el walkthrough, con el review-map y el walkthrough; para actualizarlo usa bflow pr.")
		case c.Phase != "" && (checkoutNew.MatchString(s) || switchNew.MatchString(s) || branchNew.MatchString(s)):
			return deny("bflow_branch", "la rama de la tarea la crea bflow (al aprobar el spec o al empezar un hotfix); sigue el next de bflow status.")
		}
		if m := pushRe.FindStringSubmatch(s); m != nil {
			args := m[2]
			if forceRe.MatchString(args) {
				return deny("force_push", "push forzado prohibido: reescribe historia compartida.")
			}
			for _, f := range strings.Fields(args) {
				target := f[strings.LastIndex(f, ":")+1:]
				if slices.Contains(c.Protected, target) {
					return deny("protected_branch", "no se empuja a %s: los cambios llegan por PR (bflow pr).", target)
				}
			}
		}
		if commitRe.MatchString(s) {
			if c.ForbidCoauthor && coauthorRe.MatchString(s) {
				return deny("coauthor", "este repo no usa el trailer Co-Authored-By en los commits.")
			}
			if c.MaxDiffLines > 0 && c.DiffLines != nil {
				if n, err := c.DiffLines(); err == nil && n > c.MaxDiffLines {
					return deny("diff_size", "el diff de la tarea suma %d líneas y el máximo es %d: divide el cambio o pide NEEDS_DECISION.", n, c.MaxDiffLines)
				}
			}
		}
		if m := rmRe.FindString(s); m != "" && isFrozenPhase(c) {
			for _, f := range strings.Fields(s)[1:] {
				if slices.Contains(c.Frozen, rel(c.Root, f)) {
					return deny("frozen_test", "%s es una prueba congelada al aprobar el contrato: no se borra ni se mueve.", f)
				}
			}
		}
	}
	return allow
}

func edit(a Action, c Context) Decision {
	p := rel(c.Root, a.Path)
	base := filepath.Base(p)
	if (base == ".env" || strings.HasPrefix(base, ".env.")) && base != ".env.example" {
		return deny("env_file", "%s puede tener secretos: no se edita desde un agente.", p)
	}
	if (p == ".bflow" || strings.HasPrefix(p, ".bflow/")) && !agentArtifact(p) {
		return deny("bflow_state", "%s es estado de bflow: cámbialo con sus comandos (report, approve, block…), no a mano. Los agentes solo escriben %s en .bflow/tasks/<ID>/.",
			p, strings.Join(flow.AgentArtifacts, ", "))
	}
	if slices.Contains(c.HumanFiles, p) {
		return deny("human_file", "%s guarda las vulnerabilidades aceptadas: lo edita una persona. Si hace falta aceptar una, reporta NEEDS_DECISION.", p)
	}
	if isFrozenPhase(c) && slices.Contains(c.Frozen, p) {
		return deny("frozen_test", "%s quedó congelada al aprobar el contrato. Si la prueba está mal, reporta NEEDS_DECISION con el cambio que necesitas; si una persona lo aprueba y corre `bflow freeze --allow %s`, podrás cambiarla una vez.", p, p)
	}
	if c.StrictLeader && !a.Subagent && underAny(p, c.ProtectedPaths) {
		return deny("leader_code", "la sesión principal no edita %s: eso lo hace el implementer.", p)
	}
	return allow
}

// agentArtifact dice si p (.bflow/tasks/<ID>/<rel>) es un archivo que escribe un agente.
func agentArtifact(p string) bool {
	parts := strings.SplitN(p, "/", 4)
	return len(parts) == 4 && parts[1] == "tasks" && flow.IsAgentArtifact(parts[3])
}

func isFrozenPhase(c Context) bool {
	return len(c.Frozen) > 0 && slices.Contains(frozenPhases, c.Phase)
}

// rel normaliza una ruta a relativa a la raíz con barras normales.
func rel(root, p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	r := strings.ReplaceAll(root, `\`, "/")
	if r != "" && strings.HasPrefix(strings.ToLower(p), strings.ToLower(strings.TrimSuffix(r, "/"))+"/") {
		p = p[len(strings.TrimSuffix(r, "/"))+1:]
	}
	return strings.TrimPrefix(p, "./")
}

func underAny(p string, dirs []string) bool {
	for _, d := range dirs {
		d = strings.Trim(strings.ReplaceAll(d, `\`, "/"), "/")
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

// MatchesTest dice si una ruta es de prueba según los patrones del stack.
func MatchesTest(patterns []string, p string) bool {
	p = strings.ReplaceAll(p, `\`, "/")
	for _, pat := range patterns {
		if strings.HasSuffix(pat, "/**") {
			dir := strings.TrimSuffix(pat, "/**")
			if strings.HasPrefix(p, dir+"/") || strings.Contains(p, "/"+dir+"/") {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(pat, filepath.Base(p)); ok {
			return true
		}
	}
	return false
}
