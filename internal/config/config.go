// Package config carga la configuración de bflow en tres capas: global
// (perfiles compartidos entre repos), perfil y repo (bflow.yaml). La última gana.
// Los secretos nunca viven aquí: van al llavero del sistema o a variables de entorno.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"innobytes.tech/bflow/internal/flow"
)

// RepoFile es el nombre del archivo de configuración del repo.
const RepoFile = "bflow.yaml"

// Config es la configuración efectiva de un repo.
type Config struct {
	Profile string               `yaml:"profile,omitempty"`
	Project Project              `yaml:"project,omitempty"`
	Stack   string               `yaml:"stack,omitempty"`
	Agent   string               `yaml:"agent,omitempty"`
	Tracker Tracker              `yaml:"tracker,omitempty"`
	VCS     VCS                  `yaml:"vcs,omitempty"`
	Flow    Flow                 `yaml:"flow,omitempty"`
	Check   Check                `yaml:"check,omitempty"`
	Env     Env                  `yaml:"env,omitempty"`
	Guard   Guard                `yaml:"guard,omitempty"`
	Agents  map[string]AgentConf `yaml:"agents,omitempty"` // ajustes por agente para bflow render
	Doctor  Doctor               `yaml:"doctor,omitempty"`
	UI      UI                   `yaml:"ui,omitempty"` // preferencias de la persona; también en la config global

	Root    string  `yaml:"-"` // raíz del repo
	Sources Sources `yaml:"-"`
}

// Sources dice de dónde salió la configuración (para doctor y mensajes).
type Sources struct {
	Global  string
	Profile string
	Repo    string
}

type Project struct {
	Name     string `yaml:"name,omitempty"`
	Language string `yaml:"language,omitempty"`
}

type Tracker struct {
	Adapter   string              `yaml:"adapter,omitempty"`
	URL       string              `yaml:"url,omitempty"`
	Workspace string              `yaml:"workspace,omitempty"`
	Project   string              `yaml:"project,omitempty"` // identificador legible (API), nunca UUID
	Path      string              `yaml:"path,omitempty"`    // tracker local: carpeta de las tareas (.bflow/local por defecto)
	States    map[string][]string `yaml:"states,omitempty"`  // fase bflow → nombres en el tracker (el primero se usa al escribir)
}

type BranchPrefix struct {
	Feature string `yaml:"feature,omitempty"`
	Hotfix  string `yaml:"hotfix,omitempty"`
}

type VCS struct {
	Host              string       `yaml:"host,omitempty"`    // "" = sin host remoto
	APIURL            string       `yaml:"api_url,omitempty"` // API del host (GitHub Enterprise); por defecto la pública
	Repo              string       `yaml:"repo,omitempty"`    // owner/name; si falta se deduce del remoto
	Remote            string       `yaml:"remote,omitempty"`
	BaseBranch        string       `yaml:"base_branch,omitempty"`
	BranchPrefix      BranchPrefix `yaml:"branch_prefix,omitempty"`
	BranchPattern     string       `yaml:"branch_pattern,omitempty"` // {prefix}{id}-{slug} por defecto
	CommitStyle       string       `yaml:"commit_style,omitempty"`   // commits que hace bflow: {type}: {id} {summary} por defecto
	ProtectedBranches []string     `yaml:"protected_branches,omitempty"`
}

// AgentConf ajusta un agente del flujo. El contrato con bflow no se configura:
// sale del flujo. Esto es el oficio del repo y cómo se ejecuta el agente.
type AgentConf struct {
	Model  string   `yaml:"model,omitempty"`
	Effort string   `yaml:"effort,omitempty"`
	Read   []string `yaml:"read,omitempty"`  // documentos del repo que el agente lee antes de empezar
	Extra  string   `yaml:"extra,omitempty"` // archivo con instrucciones propias; se copia al agente
	// OmitClaudeMd evita cargar CLAUDE.md en el agente. Por defecto, sí cuando
	// hay Read: las reglas del repo le llegan por esas rutas.
	OmitClaudeMd *bool `yaml:"omit_claude_md,omitempty"`
}

// Doctor ajusta los avisos de bflow doctor.
// UI son preferencias de cómo bflow se muestra a la persona.
type UI struct {
	Watch bool `yaml:"watch,omitempty"` // start abre el panel (bflow watch) en otra ventana
}

type Doctor struct {
	IgnoreSkills []string `yaml:"ignore_skills,omitempty"` // skills que no chocan con el flujo aunque lo parezcan
}

// Efforts son los niveles de esfuerzo que acepta un agente.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

type Flow struct {
	Lanes            map[string][]string `yaml:"lanes,omitempty"`
	Agents           map[string][]string `yaml:"agents,omitempty"`
	MaxQualityRounds int                 `yaml:"max_quality_rounds,omitempty"`
	SLAHours         int                 `yaml:"sla_hours,omitempty"`
	SecurityAudit    *bool               `yaml:"security_audit,omitempty"`
	UI               *bool               `yaml:"ui,omitempty"` // por defecto lo decide el stack
}

type Step struct {
	Name     string `yaml:"name"`
	Run      string `yaml:"run"`
	Optional bool   `yaml:"optional,omitempty"` // si la herramienta falta, el check queda degradado, no falla
	Needs    string `yaml:"needs,omitempty"`    // binario requerido (para detectar el degradado)
	Accept   string `yaml:"accept_file,omitempty"`
	Baseline string `yaml:"baseline,omitempty"` // "new-only": solo cuentan hallazgos en archivos tocados
}

type Check struct {
	Steps         []Step   `yaml:"steps,omitempty"`
	Quick         []string `yaml:"quick,omitempty"`
	CodePaths     []string `yaml:"code_paths,omitempty"`
	EnvFirst      bool     `yaml:"env_first,omitempty"` // no correr si fallan env.tcp o env.require_env (la salud del API no cuenta)
	DBTestPattern string   `yaml:"db_test_pattern,omitempty"`
	DBTestImports []string `yaml:"db_test_imports,omitempty"` // sufijos de import que marcan un paquete de tests con BD
	FailPattern   string   `yaml:"fail_pattern,omitempty"`    // líneas que resumen un fallo (por defecto, las del stack)
}

type Env struct {
	APIURL      string            `yaml:"api_url,omitempty"`
	HealthPaths []string          `yaml:"health_paths,omitempty"`
	TCP         []string          `yaml:"tcp,omitempty"` // nombre=host:puerto
	TimeoutMS   int               `yaml:"timeout_ms,omitempty"`
	RequireEnv  map[string]string `yaml:"require_env,omitempty"` // VAR → fragmento que debe contener
}

type Guard struct {
	ProtectedPaths []string `yaml:"protected_paths,omitempty"` // la sesión principal no las edita
	TestPatterns   []string `yaml:"test_patterns,omitempty"`   // no se editan después de aprobar el contrato
	MaxDiffLines   int      `yaml:"max_diff_lines,omitempty"`
	ForbidCoauthor bool     `yaml:"forbid_coauthor,omitempty"` // bloquear el trailer Co-Authored-By en commits
	StrictLeader   bool     `yaml:"strict_leader,omitempty"`   // la sesión principal no edita protected_paths
}

type globalFile struct {
	Profiles map[string]Config `yaml:"profiles"`
	UI       UI                `yaml:"ui"`
}

// Known son los adaptadores que este binario sabe construir.
var Known = struct{ Trackers, Hosts, Agents []string }{
	Trackers: []string{"local", "plane"},
	Hosts:    []string{"", "github"},
	Agents:   []string{"", "claude"},
}

// GlobalDir es la carpeta de configuración global.
func GlobalDir() string {
	if d := os.Getenv("BFLOW_CONFIG_HOME"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("AppData"); d != "" {
			return filepath.Join(d, "bflow")
		}
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "bflow")
	}
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".config", "bflow")
}

// GlobalPath es la ruta del archivo global.
func GlobalPath() string { return filepath.Join(GlobalDir(), "config.yaml") }

// FindRoot sube desde dir hasta encontrar bflow.yaml o .git. Si no hay
// ninguno, la raíz es dir.
func FindRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	for d := abs; ; {
		for _, marker := range []string{RepoFile, ".git"} {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return abs
		}
		d = parent
	}
}

// Load carga la configuración efectiva para el repo que contiene dir.
func Load(dir string) (*Config, error) {
	root := FindRoot(dir)
	c := &Config{Root: root}

	globalMap, globalProfiles, err := readGlobal(GlobalPath())
	if err != nil {
		return nil, err
	}
	if globalMap != nil {
		c.Sources.Global = GlobalPath()
	}

	repoPath := filepath.Join(root, RepoFile)
	repoMap, err := readRepo(repoPath)
	if err != nil {
		return nil, err
	}
	if repoMap != nil {
		c.Sources.Repo = repoPath
	}

	profile, _ := repoMap["profile"].(string)
	if env := os.Getenv("BFLOW_PROFILE"); env != "" {
		profile = env
	}
	merged := map[string]any{}
	if ui, ok := globalMap["ui"].(map[string]any); ok { // preferencias personales: el repo o el perfil las pisan
		merged["ui"] = ui
	}
	if profile != "" {
		pm, ok := globalProfiles[profile]
		if !ok {
			return nil, fmt.Errorf("perfil %q no existe en %s (disponibles: %s)", profile, GlobalPath(), strings.Join(sortedKeys(globalProfiles), ", "))
		}
		merged = deepMerge(merged, pm)
		c.Sources.Profile = profile
	}
	merged = deepMerge(merged, repoMap)

	raw, err := yaml.Marshal(merged)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("configuración combinada: %w", err)
	}
	c.Root = root
	c.Profile = profile
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func readGlobal(path string) (map[string]any, map[string]map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, map[string]map[string]any{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := findSecrets(m, ""); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := strictDecode(data, &globalFile{}); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	profiles := map[string]map[string]any{}
	if ps, ok := m["profiles"].(map[string]any); ok {
		for name, p := range ps {
			pm, _ := p.(map[string]any)
			if pm == nil {
				pm = map[string]any{}
			}
			profiles[name] = pm
		}
	}
	return m, profiles, nil
}

func readRepo(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	if err := findSecrets(m, ""); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := strictDecode(data, &Config{}); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func strictDecode(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return humanize(err)
	}
	return nil
}

var secretKey = regexp.MustCompile(`(?i)^(token|api_?key|password|passwd|secret|client_secret|.*_token|.*_secret|.*_password)$`)

func findSecrets(v any, path string) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range sortedKeys(m) {
		p := k
		if path != "" {
			p = path + "." + k
		}
		if secretKey.MatchString(k) {
			return fmt.Errorf("%s parece un secreto; los secretos no van en YAML sino en el llavero del sistema (bflow connect <plane|github>) o en una variable de entorno", p)
		}
		if err := findSecrets(m[k], p); err != nil {
			return err
		}
	}
	return nil
}

// deepMerge combina b sobre a: los mapas se combinan, lo demás (listas
// incluidas) se reemplaza.
func deepMerge(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if bm, ok := v.(map[string]any); ok {
			if am, ok := out[k].(map[string]any); ok {
				out[k] = deepMerge(am, bm)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func (c *Config) applyDefaults() {
	if c.Tracker.Adapter == "" {
		c.Tracker.Adapter = "local"
	}
	if c.VCS.Remote == "" {
		c.VCS.Remote = "origin"
	}
	if c.VCS.BranchPrefix.Feature == "" {
		c.VCS.BranchPrefix.Feature = "feature/"
	}
	if c.VCS.BranchPrefix.Hotfix == "" {
		c.VCS.BranchPrefix.Hotfix = "hotfix/"
	}
	if c.VCS.BranchPattern == "" {
		c.VCS.BranchPattern = "{prefix}{id}-{slug}"
	}
	if c.VCS.CommitStyle == "" {
		c.VCS.CommitStyle = "{type}: {id} {summary}"
	}
	if len(c.VCS.ProtectedBranches) == 0 {
		c.VCS.ProtectedBranches = []string{"main", "master"}
		if c.VCS.BaseBranch != "" && !slices.Contains(c.VCS.ProtectedBranches, c.VCS.BaseBranch) {
			c.VCS.ProtectedBranches = append(c.VCS.ProtectedBranches, c.VCS.BaseBranch)
		}
	}
	if c.Flow.MaxQualityRounds == 0 {
		c.Flow.MaxQualityRounds = 2
	}
	if c.Flow.SLAHours == 0 {
		c.Flow.SLAHours = 24
	}
	if c.Env.TimeoutMS == 0 {
		c.Env.TimeoutMS = 2500
	}
	st := stacks[c.Stack]
	if len(c.Check.CodePaths) == 0 {
		c.Check.CodePaths = st.CodePaths
	}
	if len(c.Guard.TestPatterns) == 0 {
		c.Guard.TestPatterns = st.TestPatterns
	}
	if len(c.Guard.ProtectedPaths) == 0 {
		c.Guard.ProtectedPaths = c.Check.CodePaths
	}
	if c.Flow.UI == nil {
		ui := st.UI
		c.Flow.UI = &ui
	}
}

// ValidationError agrupa todos los problemas encontrados.
type ValidationError struct {
	File     string
	Problems []string
}

func (e *ValidationError) Error() string {
	where := "configuración"
	if e.File != "" {
		where = e.File
	}
	return fmt.Sprintf("%s tiene %d problema(s):\n  - %s", where, len(e.Problems), strings.Join(e.Problems, "\n  - "))
}

var uuidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Validate revisa la configuración efectiva y reporta todos los problemas juntos.
func (c *Config) Validate() error {
	var p []string
	add := func(f string, a ...any) { p = append(p, fmt.Sprintf(f, a...)) }

	if !slices.Contains(Known.Trackers, c.Tracker.Adapter) {
		add("tracker.adapter %q no existe (disponibles: %s)", c.Tracker.Adapter, strings.Join(Known.Trackers, ", "))
	}
	if c.Tracker.Adapter == "plane" {
		if c.Tracker.URL == "" {
			add("tracker.url es obligatorio con plane")
		}
		if c.Tracker.Workspace == "" {
			add("tracker.workspace es obligatorio con plane")
		}
		if c.Tracker.Project == "" {
			add("tracker.project es obligatorio con plane (identificador legible, p. ej. API)")
		}
	}
	if uuidRe.MatchString(c.Tracker.Project) {
		add("tracker.project %q es un UUID; usa el identificador legible del proyecto (p. ej. API) y bflow resuelve el ID interno", c.Tracker.Project)
	}
	if !slices.Contains(Known.Hosts, c.VCS.Host) {
		add("vcs.host %q no existe (disponibles: github, o vacío para solo git local)", c.VCS.Host)
	}
	if !slices.Contains(Known.Agents, c.Agent) {
		add("agent %q no existe (disponibles: claude)", c.Agent)
	}
	for i, s := range c.Check.Steps {
		if s.Name == "" {
			add("check.steps[%d].name es obligatorio", i)
		}
		if strings.TrimSpace(s.Run) == "" {
			add("check.steps[%d].run es obligatorio", i)
		}
		if s.Baseline != "" && s.Baseline != "new-only" {
			add("check.steps[%d].baseline solo admite new-only", i)
		}
	}
	if c.Check.DBTestPattern != "" {
		if _, err := regexp.Compile(c.Check.DBTestPattern); err != nil {
			add("check.db_test_pattern no es una regex válida: %v", err)
		}
	}
	for lane, phases := range c.Flow.Lanes {
		for _, ph := range phases {
			if !slices.Contains(flow.Order, flow.Phase(ph)) {
				add("flow.lanes.%s: fase desconocida %q", lane, ph)
			}
		}
	}
	unknownPhase := slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, "fase desconocida") })
	if !unknownPhase {
		core := c.Flow.Core()
		if err := core.Validate(); err != nil {
			add("flow: %v", err)
		}
		for _, name := range sortedKeys(c.Agents) {
			a := c.Agents[name]
			if len(core.PhasesOf(name)) == 0 {
				add("agents.%s no trabaja en ninguna fase (revisa flow.agents)", name)
			}
			if a.Effort != "" && !slices.Contains(Efforts, a.Effort) {
				add("agents.%s.effort %q no existe (disponibles: %s)", name, a.Effort, strings.Join(Efforts, ", "))
			}
			for _, r := range append(slices.Clone(a.Read), a.Extra) {
				if filepath.IsAbs(r) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(r)), "../") {
					add("agents.%s: %q debe ser una ruta dentro del repo", name, r)
				}
			}
		}
	}
	if len(p) > 0 {
		return &ValidationError{File: c.Sources.Repo, Problems: p}
	}
	return nil
}

// Core traduce la sección flow a la configuración del núcleo.
func (f Flow) Core() flow.Config {
	core := flow.DefaultConfig()
	for lane, phases := range f.Lanes {
		ps := make([]flow.Phase, len(phases))
		for i, p := range phases {
			ps[i] = flow.Phase(p)
		}
		core.Lanes[flow.Lane(lane)] = ps
	}
	if f.MaxQualityRounds > 0 {
		core.MaxQualityRounds = f.MaxQualityRounds
	}
	if f.UI != nil && *f.UI {
		core.UI = true
		core.Agents[flow.Spec] = []string{"ui-designer", "spec-author"}
		core.Agents[flow.Quality] = append(slices.Clone(core.Agents[flow.Quality]), "ux-auditor")
	}
	if f.SecurityAudit != nil && !*f.SecurityAudit {
		core.Agents[flow.Quality] = slices.DeleteFunc(slices.Clone(core.Agents[flow.Quality]), func(a string) bool { return a == "security-auditor" })
	}
	for phase, agents := range f.Agents {
		core.Agents[flow.Phase(phase)] = slices.Clone(agents)
	}
	return core
}

var unknownField = regexp.MustCompile(`line (\d+): field (\S+) not found in type \S+`)

// humanize traduce los errores de yaml.v3 que exponen tipos internos de Go.
func humanize(err error) error {
	msg := unknownField.ReplaceAllString(err.Error(), "línea $1: campo desconocido «$2»")
	msg = strings.Replace(msg, "yaml: unmarshal errors:\n", "", 1)
	return errors.New(strings.TrimSpace(msg))
}
