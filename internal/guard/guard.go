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
	Read  = "read"  // la registran todas las lecturas (guard --reads); Evaluate la permite
	Spawn = "spawn" // lanzamiento de un subagente (Command = prompt, Target = tipo); Evaluate la permite
)

// Action es lo que el agente quiere hacer.
type Action struct {
	Tool     string `json:"tool"`
	Command  string `json:"command,omitempty"`
	Path     string `json:"path,omitempty"`
	Subagent bool   `json:"subagent,omitempty"`
	Agent    string `json:"agent,omitempty"`   // agent_type del hook; "" en la sesión principal
	Partial  bool   `json:"partial,omitempty"` // Read con offset o limit; solo la llenan los parsers
	Session  string `json:"session,omitempty"` // llave de sesión (R4); la llenan los parsers
	Target   string `json:"target,omitempty"`  // Spawn: tipo de subagente lanzado
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
func Segments(cmd string) []string { return segments(cmd, 0) }

var heredocRe = regexp.MustCompile(`(?:^|[^<])<<-?\s*(?:'([^']+)'|"([^"]+)"|([A-Za-z_][A-Za-z0-9_]*))`)

var shellNames = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true}

// feedsShell dice si algún segmento de la línea ejecuta un shell (bash <<EOF,
// cat <<EOF | bash): entonces el cuerpo del heredoc son comandos.
func feedsShell(line string) bool {
	for _, seg := range segSplit.Split(line, -1) {
		for _, f := range strings.Fields(seg) {
			if strings.Contains(f, "=") && !strings.HasPrefix(f, "-") {
				continue
			}
			f = strings.Trim(f, `"'`)
			f = strings.ToLower(f[strings.LastIndexAny(f, `/\`)+1:])
			f = strings.TrimSuffix(f, ".exe")
			if shellNames[f] {
				return true
			}
			if f != "sudo" && f != "env" && f != "exec" {
				break
			}
		}
	}
	return false
}

// stripHeredocs quita el cuerpo de los heredocs que leen otros programas: es
// texto. Si el heredoc alimenta a un shell, el cuerpo se conserva (se ejecuta).
func stripHeredocs(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	var out []string
	end, run := "", false
	for _, line := range strings.Split(cmd, "\n") {
		if end != "" {
			if strings.TrimSpace(line) == end {
				end = ""
			} else if run {
				out = append(out, line)
			}
			continue
		}
		out = append(out, line)
		if m := heredocRe.FindStringSubmatch(line); m != nil {
			end = m[1] + m[2] + m[3]
			run = feedsShell(line)
		}
	}
	return strings.Join(out, "\n")
}

// splitCommand parte en &&, ||, ;, | y saltos de línea, pero no dentro de
// comillas ni en el cuerpo de un heredoc. Si las comillas no cierran o hay sustitución ($(...), `) entre comillas dobles, cae al
// corte simple para no esconder comandos.
func splitCommand(cmd string) []string {
	cmd = stripHeredocs(cmd)
	var out []string
	var q byte
	subst := false
	start := 0
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			} else if c == '\\' && q == '"' {
				i++
			} else if q == '"' && (c == '`' || c == '$' && i+1 < len(cmd) && cmd[i+1] == '(') {
				subst = true
			}
		case c == '"' || c == '\'':
			q = c
		case c == '\\':
			i++
		case c == ';' || c == '|' || c == '\n' || c == '&' && i+1 < len(cmd) && cmd[i+1] == '&':
			out = append(out, cmd[start:i])
			if (c == '|' || c == '&') && i+1 < len(cmd) && cmd[i+1] == c {
				i++
			}
			start = i + 1
		}
	}
	if subst {
		cmd = strings.NewReplacer("$(", ";", "`", ";", ")", ";").Replace(cmd)
	}
	if q != 0 || subst {
		return segSplit.Split(cmd, -1)
	}
	return append(out, cmd[start:])
}

func segments(cmd string, depth int) []string {
	var out []string
	for _, seg := range splitCommand(cmd) {
		s := strings.TrimSpace(seg)
		if s == "" {
			continue
		}
		if depth < 3 {
			if inner, ok := unwrapShell(s); ok {
				out = append(out, segments(inner, depth+1)...)
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

var groupedFlags = regexp.MustCompile(`^-[A-Za-z]+$`)

// unwrapShell devuelve <cmd> de `bash|sh|zsh [-flags] -c "<cmd>"`.
func unwrapShell(seg string) (string, bool) {
	fields := strings.Fields(seg)
	t := skipPrefix(slices.Clone(fields))
	if len(t) == 0 {
		return "", false
	}
	switch exeName(t[0]) {
	case "bash", "sh", "zsh":
	default:
		return "", false
	}
	i := 1
	found := false
	for ; i < len(t) && t[i] != "" && (t[i][0] == '-' || t[i][0] == '+'); i++ {
		switch t[i] {
		case "-o", "+o", "-O", "+O":
			i++
			continue
		}
		if groupedFlags.MatchString(t[i]) && strings.Contains(t[i], "c") {
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	// texto original tras el token -c
	rest := seg
	for k := 0; k <= i+len(fields)-len(t); k++ {
		rest = strings.TrimSpace(rest)
		f := strings.Fields(rest)
		if len(f) == 0 {
			return "", false
		}
		rest = rest[len(f[0]):]
	}
	rest = strings.TrimSpace(rest)
	if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
		q := rest[0]
		rest = rest[1:]
		if n := len(rest); n > 0 && rest[n-1] == q {
			rest = rest[:n-1]
		}
	}
	return rest, true
}

// exeName es el nombre base en minúsculas, sin comillas, ruta ni .exe.
func exeName(w string) string {
	w = strings.TrimRight(strings.ReplaceAll(strings.Trim(w, `"'`), `\`, "/"), "/")
	return strings.TrimSuffix(strings.ToLower(w[strings.LastIndex(w, "/")+1:]), ".exe")
}

// skipPrefix quita asignaciones VAR=x y `env [flags] VAR=x` del inicio, y
// las comillas de cada token.
func skipPrefix(t []string) []string {
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
	return t
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
	addRe        = regexp.MustCompile(`^git\s+(-[cC]\s+\S+\s+|-\S+\s+)*(add|stage)\b(.*)$`)
	commitRe     = regexp.MustCompile(`^git\s+(-\S+\s+)*commit\b`)
	coauthorRe   = regexp.MustCompile(`(?i)co-authored-by`)
	rmRe         = regexp.MustCompile(`^(rm|del|git\s+rm|mv|git\s+mv)\s`)

	// Acciones que le tocan a bflow mientras hay una tarea en curso.
	ghPRCreate  = regexp.MustCompile(`^gh\s+pr\s+create\b`)
	checkoutNew = regexp.MustCompile(`^git\s+(-\S+\s+)*checkout\s+(.*\s)?-[bB](\s|$)`)
	switchNew   = regexp.MustCompile(`^git\s+(-\S+\s+)*switch\s+(.*\s)?(-[cC]|--create|--force-create)(\s|$)`)
	branchNew   = regexp.MustCompile(`^git\s+(-\S+\s+)*branch\s+[^-\s]`)
)

// HumanOnly son los subcomandos que responden una decisión humana: un
// subagente no los corre (los corre la sesión principal después de preguntar).
var HumanOnly = []string{"approve", "reject", "unblock", "start", "new", "drop"}

// BflowSubcommand devuelve el subcomando de bflow de un segmento ya recortado
// ("approve" en `./bin/bflow.exe --json approve X`), o "" si el segmento no
// invoca a bflow.
func BflowSubcommand(seg string) string {
	t, ok := bflowTokens(seg)
	if !ok || len(t) == 0 {
		return ""
	}
	return strings.ToLower(t[0])
}

// BflowArgs devuelve los argumentos sin guion tras el subcomando de un segmento
// bflow (misma lógica de BflowSubcommand); nil si el segmento no es bflow.
func BflowArgs(seg string) []string {
	t, ok := bflowTokens(seg)
	if !ok || len(t) < 2 {
		return nil
	}
	var out []string
	for _, w := range t[1:] {
		if !strings.HasPrefix(w, "-") {
			out = append(out, w)
		}
	}
	return out
}

// bflowTokens devuelve los tokens de un segmento bflow desde el subcomando
// (sin los flags previos a él); ok=false si el segmento no invoca a bflow.
func bflowTokens(seg string) ([]string, bool) {
	t := skipPrefix(strings.Fields(seg))
	if len(t) == 0 {
		return nil, false
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
			return nil, false
		}
		ps := parts(strings.TrimRight(strings.ReplaceAll(t[0], `\`, "/"), "/"))
		if len(ps) < 2 || ps[len(ps)-1] != "bflow" || ps[len(ps)-2] != "cmd" {
			return nil, false
		}
		t = t[1:]
	default:
		return nil, false
	}
	for len(t) > 0 && strings.HasPrefix(t[0], "-") {
		t = t[1:]
	}
	return t, true
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
		if m := addRe.FindStringSubmatch(s); m != nil {
			bad := false
			for _, f := range strings.Fields(m[3]) {
				if f == "--force" || (strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") && strings.Contains(f, "f")) {
					bad = true
					break
				}
				r := strings.ReplaceAll(rel(c.Root, f), `\`, "/")
				if r == ".bflow" || strings.HasPrefix(r, ".bflow/") {
					bad = true
					break
				}
			}
			if bad {
				return deny("bflow_tracked", "`%s` metería archivos que git ignora: lo de .bflow/ (walkthrough, reportes) se queda fuera del repo; bflow commitea la spec y el changelog. Agrega solo código y docs, sin -f.", s)
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
