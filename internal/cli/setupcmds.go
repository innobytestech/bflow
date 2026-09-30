package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/agents"
	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/envcheck"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/setup"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/vcs"
)

// prompter pregunta por stderr y lee respuestas de stdin (consola o pipe).
type prompter struct {
	r   *bufio.Reader
	w   io.Writer
	yes bool
}

func newPrompter(c *Ctx, yes bool) *prompter {
	in := c.Stdin
	if in == nil {
		in = strings.NewReader("")
	}
	return &prompter{r: bufio.NewReader(in), w: c.Stderr, yes: yes}
}

func (p *prompter) line() string {
	s, _ := p.r.ReadString('\n')
	return strings.TrimSpace(s)
}

// ask pregunta un texto; vacío o --yes devuelve def.
func (p *prompter) ask(q, def string) string {
	if p.yes {
		return def
	}
	if def != "" {
		fmt.Fprintf(p.w, "%s [%s]: ", q, def)
	} else {
		fmt.Fprintf(p.w, "%s: ", q)
	}
	if s := p.line(); s != "" {
		return s
	}
	return def
}

// choose muestra opciones numeradas; devuelve el índice elegido.
func (p *prompter) choose(q string, opts []string, def int) int {
	if p.yes || len(opts) == 1 {
		return def
	}
	fmt.Fprintln(p.w, q)
	for i, o := range opts {
		fmt.Fprintf(p.w, "  %d. %s\n", i+1, o)
	}
	fmt.Fprintf(p.w, "elige [%d]: ", def+1)
	s := p.line()
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(opts) {
		return n - 1
	}
	for i, o := range opts {
		if strings.EqualFold(s, o) || strings.HasPrefix(strings.ToLower(o), strings.ToLower(s)+" ") {
			return i
		}
	}
	return def
}

func gitOut(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func init() {
	Register(&Command{Name: "init", Summary: "crea bflow.yaml detectando stack, remoto, rama base y pasos de check: init [--yes] [--profile p] [--tracker local|plane|github] [--project P (github: owner/N)]|plane] [--project P]",
		Setup: func(fs *flag.FlagSet) {
			for _, f := range []string{"stack", "tracker", "project", "url", "workspace", "profile", "host", "base", "agent"} {
				fs.String(f, "", "")
			}
			fs.Bool("yes", false, "no preguntar: usar lo detectado y los valores por defecto")
			fs.Bool("force", false, "sobrescribir un bflow.yaml existente")
		},
		Run: runInit})

	Register(&Command{Name: "profile add", Summary: "crea o completa un perfil global: profile add <nombre> [--tracker --url --workspace --host --base --agent]",
		Setup: func(fs *flag.FlagSet) {
			for _, f := range []string{"tracker", "url", "workspace", "host", "base", "agent"} {
				fs.String(f, "", "")
			}
		},
		Run: runProfileAdd})
	Register(&Command{Name: "profile list", Summary: "lista los perfiles globales", Run: runProfileList})
	Register(&Command{Name: "profile use", Summary: "usa un perfil en este repo: profile use <nombre>", Run: runProfileUse})
	Register(&Command{Name: "doctor", Summary: "valida config, herramientas, conexiones, entorno y hooks", Run: runDoctor})
}

// panelChoice es la respuesta a qué abrir al empezar una tarea. La página
// la sirve la ventana del panel, así que no hay navegador sin terminal.
type panelChoice struct {
	label, summary string
	watch, web     bool
}

var panelChoices = []panelChoice{
	{"el panel en otra terminal y su página en el navegador", "panel y navegador", true, true},
	{"solo el panel en otra terminal", "solo el panel", true, false},
	{"nada: los abro yo (bflow watch --open, bflow ui)", "nada", false, false},
}

func panelLabels() []string {
	var out []string
	for _, p := range panelChoices {
		out = append(out, p.label)
	}
	return out
}

func runInit(c *Ctx) output.Envelope {
	ctx := context.Background()
	root := config.FindRoot(c.Dir)
	path := filepath.Join(root, config.RepoFile)
	if _, err := os.Stat(path); err == nil && str(c.Flags, "force") != "true" && !c.DryRun {
		return output.Rejected("exists", path+" ya existe (usa --force para regenerarlo, o edita el archivo)")
	}
	p := newPrompter(c, str(c.Flags, "yes") == "true")
	det := setup.Detect(root, setup.Tools{Has: func(n string) bool { _, err := exec.LookPath(n); return err == nil }})

	a := setup.Answers{Stack: det.Stack, Steps: det.Steps, Quick: det.Quick, RequireEnv: det.DBEnv}
	if s := str(c.Flags, "stack"); s != "" {
		a.Stack = s
	}

	// Perfil
	profiles, err := config.LoadProfiles()
	if err != nil {
		return output.Fail("config", err)
	}
	a.Profile = str(c.Flags, "profile")
	if a.Profile == "" && len(profiles) > 0 {
		names := []string{"ninguno"}
		for n := range profiles {
			names = append(names, n)
		}
		sort.Strings(names[1:])
		if i := p.choose("¿Qué perfil usa este repo?", names, 0); i > 0 {
			a.Profile = names[i]
		}
	}
	var prof setup.Profile
	if a.Profile != "" {
		pc, ok := profiles[a.Profile]
		if !ok {
			return output.Fail("profile", fmt.Errorf("el perfil %q no existe (bflow profile list)", a.Profile))
		}
		prof = setup.Profile{Tracker: pc.Tracker.Adapter, URL: pc.Tracker.URL, Workspace: pc.Tracker.Workspace, Host: pc.VCS.Host, BaseBranch: pc.VCS.BaseBranch, Agent: pc.Agent}
	}

	// Host desde el remoto (el tracker github depende de él)
	remote := gitOut(root, "remote", "get-url", "origin")
	detHost, repo := setup.HostFromRemote(remote)
	a.Host = firstNonEmpty(str(c.Flags, "host"), prof.Host, detHost)
	if a.Host == "none" {
		a.Host = ""
	}

	// Tracker
	a.Tracker = firstNonEmpty(str(c.Flags, "tracker"), prof.Tracker)
	if a.Tracker == "" {
		names, labels := []string{"local", "plane"}, []string{"local (archivos en .bflow, sin cuenta)", "plane"}
		if a.Host == "github" {
			names, labels = append(names, "github"), append(labels, "github (issues del repo y, si quieres, un Project)")
		}
		a.Tracker = names[p.choose("¿Qué tracker usa?", labels, 0)]
	}
	if a.Tracker == "github" && a.Host != "github" {
		return output.Fail("incomplete", errors.New("el tracker github exige host github (--host github, o un remoto de GitHub)"))
	}
	if a.Tracker == "plane" {
		a.TrackerURL = firstNonEmpty(str(c.Flags, "url"), prof.URL)
		if a.TrackerURL == "" {
			a.TrackerURL = p.ask("URL de Plane", "")
		}
		a.Workspace = firstNonEmpty(str(c.Flags, "workspace"), prof.Workspace)
		if a.Workspace == "" {
			a.Workspace = p.ask("Workspace de Plane (slug)", "")
		}
		a.Project = strings.ToUpper(str(c.Flags, "project"))
		if a.Project == "" {
			var ids []string
			if c.Projects != nil {
				if ps, err := c.Projects(ctx, "plane", a.TrackerURL, a.Workspace); err == nil {
					for _, pr := range ps {
						ids = append(ids, pr.ID+" · "+pr.Name)
					}
				} else {
					fmt.Fprintln(c.Stderr, "no se pudieron listar los proyectos:", err)
				}
			}
			if len(ids) > 0 && !p.yes {
				a.Project = strings.Fields(ids[p.choose("¿Qué proyecto de Plane?", ids, 0)])[0]
			} else {
				a.Project = strings.ToUpper(p.ask("Identificador del proyecto (p. ej. API)", ""))
			}
		}
		if a.TrackerURL == "" || a.Workspace == "" || a.Project == "" {
			return output.Fail("incomplete", errors.New("plane necesita --url, --workspace y --project (o un perfil que los tenga)"))
		}
	} else if a.Tracker == "github" {
		a.Project = str(c.Flags, "project")
		if a.Project == "" && c.Projects != nil {
			ps, err := c.Projects(ctx, "github", "", repo)
			if err != nil {
				fmt.Fprintln(c.Stderr, "no se pudieron listar los Projects:", err)
			}
			if len(ps) > 0 && !p.yes {
				ids := []string{"sin project (la fase es una etiqueta bflow:*)"}
				for _, pr := range ps {
					ids = append(ids, pr.ID+" · "+pr.Name)
				}
				if i := p.choose("¿Qué Project de GitHub?", ids, 0); i > 0 {
					a.Project = strings.Fields(ids[i])[0]
				}
			}
		}
	} else if a.Tracker == "local" {
		a.Project = strings.ToUpper(str(c.Flags, "project"))
	}

	// Rama base desde el remoto
	head := strings.TrimPrefix(gitOut(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"), "origin/")
	var branches []string
	for _, b := range strings.Split(gitOut(root, "branch", "-r", "--format=%(refname:short)"), "\n") {
		branches = append(branches, strings.TrimPrefix(strings.TrimSpace(b), "origin/"))
	}
	if head == "" {
		head = gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
	}
	a.BaseBranch = firstNonEmpty(str(c.Flags, "base"), prof.BaseBranch)
	if a.BaseBranch == "" {
		a.BaseBranch = p.ask("Rama base de los PR", setup.PickBase(head, branches))
	}
	a.Agent = firstNonEmpty(str(c.Flags, "agent"), prof.Agent)
	if a.Agent == "" && usesClaude(root) {
		a.Agent = "claude"
	}

	if len(a.Steps) > 0 && !p.yes {
		fmt.Fprintln(c.Stderr, "Pasos de check propuestos:")
		for _, s := range a.Steps {
			opt := ""
			if s.Optional {
				opt = " (opcional: " + s.Needs + " no está instalado)"
			}
			fmt.Fprintf(c.Stderr, "  - %s: %s%s\n", s.Name, s.Run, opt)
		}
		if strings.HasPrefix(strings.ToLower(p.ask("¿Usarlos? (s/n)", "s")), "n") {
			a.Steps, a.Quick = nil, nil
		}
	}

	// Qué se abre al empezar una tarea es de la persona, no del repo: se
	// pregunta una vez por máquina y va a la config global.
	var panel *panelChoice
	if !p.yes && !c.DryRun && desktop(goos, os.Getenv) == nil {
		if decided, err := config.UIDecided(); err == nil && !decided {
			pc := panelChoices[p.choose("Al empezar una tarea, ¿qué abro? (es tuya: va a tu config global, no al repo)", panelLabels(), 0)]
			panel = &pc
		}
	}

	y, err := setup.RenderYAML(a, prof)
	if err != nil {
		return output.Fail("render", err)
	}
	data := map[string]any{"path": path, "stack": a.Stack, "tracker": a.Tracker, "host": a.Host, "base": a.BaseBranch, "steps": len(a.Steps), "yaml": y}
	if repo != "" {
		data["repo"] = repo
	}
	if c.DryRun {
		env := output.OK("init_plan", data, nil)
		env.Text = y
		return env
	}
	if err := os.WriteFile(path, []byte(y), 0o644); err != nil {
		return output.Fail("write", err)
	}
	if _, err := config.Load(root); err != nil {
		return output.Fail("invalid", fmt.Errorf("el bflow.yaml generado no es válido (repórtalo): %w", err))
	}
	_ = config.RememberRepo(root)
	panelText := ""
	if panel != nil {
		if err := config.SaveUI(map[string]any{"watch": panel.watch, "web": panel.web}); err != nil {
			fmt.Fprintln(c.Stderr, "no se pudo guardar qué abrir al empezar una tarea:", err)
		} else {
			data["ui"] = map[string]bool{"watch": panel.watch, "web": panel.web}
			panelText = "\nal empezar una tarea: " + panel.summary + " (ui: en " + config.GlobalPath() + ")"
		}
	}
	var next []string
	if a.Tracker == "plane" {
		next = append(next, "bflow connect plane (si aún no hay token)", "bflow tracker setup --dry-run")
	}
	if a.Tracker == "github" {
		next = append(next, "bflow connect github (token del tracker y de los PR)", "bflow tracker setup --dry-run")
	} else if a.Host == "github" {
		next = append(next, "bflow connect github (para abrir PR solo)")
	}
	if a.Agent == "claude" {
		next = append(next, "bflow render (genera los agentes en .claude/agents; commitéalos)")
	}
	next = append(next, "bflow doctor")
	env := output.OK("initialized", data, nil)
	env.Text = fmt.Sprintf("bflow.yaml creado · stack %s · tracker %s · base %s · %d pasos de check%s\nsiguiente:\n  %s",
		orDash(a.Stack), a.Tracker, orDash(a.BaseBranch), len(a.Steps), panelText, strings.Join(next, "\n  "))
	if m := bannerFor(c); m != bannerOff {
		env.Text = renderBanner(m, c.Version) + "\n" + env.Text
	}
	return env
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func runProfileAdd(c *Ctx) output.Envelope {
	if len(c.Args) != 1 {
		return output.Fail("usage", errors.New("uso: bflow profile add <nombre> [--tracker --url --workspace --host --base --agent]"))
	}
	v := map[string]any{}
	tr := map[string]any{}
	for flagName, key := range map[string]string{"tracker": "adapter", "url": "url", "workspace": "workspace"} {
		if s := str(c.Flags, flagName); s != "" {
			tr[key] = s
		}
	}
	if len(tr) > 0 {
		v["tracker"] = tr
	}
	vc := map[string]any{}
	if s := str(c.Flags, "host"); s != "" {
		vc["host"] = s
	}
	if s := str(c.Flags, "base"); s != "" {
		vc["base_branch"] = s
	}
	if len(vc) > 0 {
		v["vcs"] = vc
	}
	if s := str(c.Flags, "agent"); s != "" {
		v["agent"] = s
	}
	if len(v) == 0 {
		return output.Fail("usage", errors.New("indica al menos un valor (--tracker, --url, --workspace, --host, --base, --agent)"))
	}
	if err := config.SaveProfile(c.Args[0], v); err != nil {
		return output.Fail("profile", err)
	}
	env := output.OK("profile_saved", map[string]any{"name": c.Args[0], "path": config.GlobalPath()}, nil)
	env.Text = fmt.Sprintf("perfil %s guardado en %s", c.Args[0], config.GlobalPath())
	return env
}

func runProfileList(c *Ctx) output.Envelope {
	ps, err := config.LoadProfiles()
	if err != nil {
		return output.Fail("config", err)
	}
	var names []string
	for n := range ps {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		p := ps[n]
		fmt.Fprintf(&b, "%s · tracker %s %s · host %s · base %s\n", n, orDash(p.Tracker.Adapter), p.Tracker.URL, orDash(p.VCS.Host), orDash(p.VCS.BaseBranch))
	}
	env := output.OK("profiles", map[string]any{"profiles": names, "path": config.GlobalPath()}, nil)
	env.Text = strings.TrimRight(b.String(), "\n")
	if env.Text == "" {
		env.Text = "sin perfiles (bflow profile add <nombre> …)"
	}
	return env
}

func runProfileUse(c *Ctx) output.Envelope {
	if len(c.Args) != 1 {
		return output.Fail("usage", errors.New("uso: bflow profile use <nombre>"))
	}
	ps, err := config.LoadProfiles()
	if err != nil {
		return output.Fail("config", err)
	}
	if _, ok := ps[c.Args[0]]; !ok {
		return output.Fail("profile", fmt.Errorf("el perfil %q no existe (bflow profile list)", c.Args[0]))
	}
	root := config.FindRoot(c.Dir)
	if err := config.SetRepoProfile(root, c.Args[0]); err != nil {
		return output.Fail("write", err)
	}
	if _, err := config.Load(root); err != nil {
		return output.Fail("invalid", err)
	}
	env := output.OK("profile_used", map[string]any{"name": c.Args[0]}, nil)
	env.Text = "este repo usa el perfil " + c.Args[0]
	return env
}

// ---------- doctor ----------

type docItem struct {
	Area   string `json:"area"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"`
}

func runDoctor(c *Ctx) output.Envelope {
	ctx := context.Background()
	var items []docItem
	add := func(area, status, format string, a ...any) {
		items = append(items, docItem{Area: area, Status: status, Detail: fmt.Sprintf(format, a...)})
	}
	e, err := engineFor(c)
	if err != nil {
		add("config", "fail", "%v", err)
		return doctorEnvelope(items)
	}
	cfg := e.Cfg
	src := cfg.Sources.Repo
	if src == "" {
		src = "sin bflow.yaml (valores por defecto; bflow init)"
	}
	if cfg.Sources.Profile != "" {
		src += " · perfil " + cfg.Sources.Profile
	}
	add("config", "ok", "%s", src)

	if e.Git == nil {
		add("git", "warn", "no es un repo git: sin ramas, PR ni check")
	} else if r, err := e.Git.RemoteURL(ctx); err != nil {
		add("git", "warn", "sin remoto %s: no hay push ni PR", cfg.VCS.Remote)
	} else {
		add("git", "ok", "remoto %s · base %s", r, orDash(cfg.VCS.BaseBranch))
	}

	var missing, optional []string
	seen := map[string]bool{}
	for _, s := range cfg.Check.Steps {
		tool := s.Needs
		if tool == "" {
			if f := strings.Fields(s.Run); len(f) > 0 {
				tool = f[0]
			}
		}
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		if _, err := exec.LookPath(tool); err != nil {
			if s.Optional {
				optional = append(optional, tool)
			} else {
				missing = append(missing, tool)
			}
		}
	}
	switch {
	case len(cfg.Check.Steps) == 0:
		add("herramientas", "warn", "sin pasos de check: DONE se acepta sin verificar (bflow init los propone)")
	case len(missing) > 0:
		add("herramientas", "fail", "faltan %s (pasos obligatorios del check)", strings.Join(missing, ", "))
	case len(optional) > 0:
		add("herramientas", "warn", "check degradado: faltan %s (opcionales)", strings.Join(optional, ", "))
	default:
		add("herramientas", "ok", "%d pasos de check con sus herramientas instaladas", len(cfg.Check.Steps))
	}

	doctorTracker(ctx, e.Tracker, cfg, add)

	if e.Host != nil {
		if u, ok := e.Host.(interface {
			User(context.Context) (string, error)
		}); ok {
			if login, err := u.User(ctx); errors.Is(err, vcs.ErrNoCredentials) {
				add("host", "warn", "%s sin token: el PR se deja listo con URL de compare (bflow connect %s)", e.Host.Name(), e.Host.Name())
			} else if err != nil {
				add("host", "fail", "%v", err)
			} else if err := checkRepo(ctx, e.Host); err != nil {
				add("host", "fail", "%v", err)
			} else {
				add("host", "ok", "%s como %s", e.Host.Name(), login)
			}
		}
	} else if cfg.VCS.Host == "" {
		add("host", "ok", "sin host remoto configurado: push y PR a mano")
	}

	var runs []string
	for _, st := range cfg.Check.Steps {
		runs = append(runs, st.Run)
	}
	if n, gaps := setup.CIGaps(cfg.Root, runs); len(gaps) > 0 {
		for _, g := range gaps {
			add("ci", "warn", "%s: agrégalo a check.steps para verlo antes del PR", g)
		}
	} else if n > 0 {
		add("ci", "ok", "el check cubre los %d comandos de verificación de CI", n)
	}

	ec := cfg.Env
	if len(ec.HealthPaths)+len(ec.TCP)+len(ec.RequireEnv) > 0 {
		res := envcheck.Run(ctx, ec, os.Getenv)
		if n := envcheck.Failed(res); n > 0 {
			add("entorno", "warn", "%d de %d verificaciones fallan (bflow env check)", n, len(res))
		} else {
			add("entorno", "ok", "%d verificaciones", len(res))
		}
	}

	if cfg.Agent == "claude" {
		b, err := os.ReadFile(filepath.Join(cfg.Root, ".claude", "settings.json"))
		var missing []string
		for _, h := range []string{"bflow guard", "bflow hook session-start", "bflow hook subagent-stop"} {
			if !strings.Contains(string(b), h) {
				missing = append(missing, h)
			}
		}
		switch {
		case err != nil:
			add("agente", "warn", "claude: falta .claude/settings.json con los hooks de bflow (copia adapters/claude/settings.json)")
		case len(missing) > 0:
			add("agente", "warn", "claude: .claude/settings.json no llama a %s (compáralo con adapters/claude/settings.json)", strings.Join(missing, ", "))
		default:
			add("agente", "ok", "claude: hooks de bflow instalados")
		}
		switch have, min, ok, err := c.Agent.Version(); {
		case err != nil:
			add("agente", "warn", "claude: no se pudo leer la versión (%v); bflow necesita %s o posterior", err, min)
		case !ok:
			add("agente", "warn", "claude: versión %s; bflow necesita %s o posterior (claude update)", have, min)
		default:
			add("agente", "ok", "claude: versión %s", have)
		}
		switch p, err := planRender(c, cfg); {
		case err != nil:
			add("agentes", "fail", "%v", err)
		case len(p.Conflicts) > 0:
			add("agentes", "fail", "%s existe y no lo generó bflow: renómbralo y corre bflow render", strings.Join(p.Conflicts, ", "))
		case len(p.Changed)+len(p.Stale) > 0:
			add("agentes", "warn", "desactualizados: %s (bflow render y commitea)", strings.Join(append(p.Changed, p.Stale...), ", "))
		default:
			add("agentes", "ok", "%d archivos generados y al día", len(p.Files))
		}
		var procs []string
		for _, s := range c.Agent.Skills(cfg.Root) {
			if s.Name == "bflow" || slices.Contains(cfg.Doctor.IgnoreSkills, s.Name) {
				continue
			}
			if t := agents.ProcessTerms(s.Description); t != nil {
				procs = append(procs, fmt.Sprintf("%s (%s)", s.Name, strings.Join(t, ", ")))
			}
		}
		if len(procs) > 0 {
			add("skills", "warn", "parecen skills de proceso y pueden chocar con bflow (ramas, PR, tracker, specs): %s. Si no chocan, agrégalas a doctor.ignore_skills", strings.Join(procs, "; "))
		}
		status, detail := startupCheck(c.Agent.StartupContext(cfg.Root))
		add("contexto", status, "%s", detail)
		if cfg.Guard.ForbidCoauthor && !c.Agent.CoauthorOff(cfg.Root) {
			add("agente", "warn", `claude agrega Co-Authored-By a sus commits y guard.forbid_coauthor los bloquea: cada commit se rechaza y se repite. Pon "attribution": { "commit": "", "pr": "" } en .claude/settings.json`)
		}
	}

	if exe, err := os.Executable(); err == nil {
		var best time.Duration
		for i := 0; i < 2; i++ {
			t0 := time.Now()
			_ = exec.Command(exe, "version").Run()
			if d := time.Since(t0); best == 0 || d < best {
				best = d
			}
		}
		if best > 250*time.Millisecond {
			add("arranque", "warn", "bflow tarda %dms en arrancar: suele ser el antivirus escaneando un ejecutable sin firmar; el guard corre antes de cada herramienta (considera excluir %s)", best.Milliseconds(), filepath.Dir(exe))
		} else {
			add("arranque", "ok", "%dms", best.Milliseconds())
		}
	}
	return doctorEnvelope(items)
}

func doctorTracker(ctx context.Context, t tracker.Tracker, cfg *config.Config, add func(string, string, string, ...any)) {
	if cfg.Tracker.Adapter == "local" {
		add("tracker", "ok", "local")
		return
	}
	gh := cfg.Tracker.Adapter == "github"
	if pl, ok := t.(tracker.ProjectLister); ok && (!gh || cfg.Tracker.Project != "") {
		ps, err := pl.Projects(ctx)
		if err != nil {
			add("tracker", "fail", "%v", err)
			return
		}
		if !slices.ContainsFunc(ps, func(p tracker.Project) bool { return strings.EqualFold(p.ID, cfg.Tracker.Project) }) {
			if gh {
				add("tracker", "fail", "el project %s no aparece entre los que ve el token (¿falta el permiso de Projects? bflow connect github)", cfg.Tracker.Project)
			} else {
				add("tracker", "fail", "el proyecto %s no está en %s", cfg.Tracker.Project, cfg.Tracker.Workspace)
			}
			return
		}
	}
	msg := fmt.Sprintf("%s · proyecto %s", cfg.Tracker.Adapter, cfg.Tracker.Project)
	if gh {
		msg = "github · etiquetas bflow:* (sin project)"
		if cfg.Tracker.Project != "" {
			msg = "github · project " + cfg.Tracker.Project + " (campo Status)"
		}
	}
	if sl, ok := t.(tracker.StateLister); ok {
		sts, err := sl.StateMap(ctx)
		if err != nil {
			add("tracker", "fail", "%v", err)
			return
		}
		written := map[string]bool{}
		for _, s := range sts {
			for _, w := range s.Writes {
				written[w] = true
			}
		}
		var lack []string
		for _, p := range append(append([]flow.Phase{flow.Backlog}, flow.Order...), flow.Blocked) {
			if !written[string(p)] {
				lack = append(lack, string(p))
			}
		}
		if len(lack) > 0 {
			add("tracker", "warn", "%s · sin estado para: %s (bflow tracker setup)", msg, strings.Join(lack, ", "))
			return
		}
	}
	add("tracker", "ok", "%s", msg)
}

func doctorEnvelope(items []docItem) output.Envelope {
	var b strings.Builder
	fails := 0
	for _, it := range items {
		mark := map[string]string{"ok": "✅", "warn": "⚠️ ", "fail": "❌"}[it.Status]
		if it.Status == "fail" {
			fails++
		}
		fmt.Fprintf(&b, "%s %-13s %s\n", mark, it.Area, it.Detail)
	}
	env := output.OK("doctor", map[string]any{"checks": items, "failed": fails}, nil)
	env.Text = strings.TrimRight(b.String(), "\n")
	if fails > 0 {
		env.OK = false
		env = env.WithExit(output.ExitError)
	}
	return env
}

// usesClaude dice si el repo ya trabaja con Claude Code.
func usesClaude(root string) bool {
	for _, p := range []string{".claude", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(root, p)); err == nil {
			return true
		}
	}
	return false
}

// Presupuesto del contexto de archivos al iniciar. Se paga en cada llamada de
// la sesión principal (en la piloto de ms-sys arrancaba con ~50k tokens). Un
// solo archivo que pasa de fileBudget, como un MEMORY.md de 124 entradas, casi
// siempre arrastra cosas que ya no aplican.
const (
	startupBudget = 10_000
	fileBudget    = 4_000
)

// startupCheck resume lo que se carga al iniciar cada sesión: el total, las
// tres fuentes más pesadas y lo que conviene podar.
func startupCheck(srcs []agents.ContextSource) (status, detail string) {
	if len(srcs) == 0 {
		return "ok", "sin CLAUDE.md, memoria ni skills que cargar al iniciar"
	}
	total := 0
	var notes []string
	for _, s := range srcs {
		total += s.Bytes
		if t := agents.EstimateTokens(s.Bytes); t > fileBudget && s.Note == "" {
			s.Note = fmt.Sprintf("≈%s tokens, más de %s en un solo archivo", metrics.Human(int64(t)), metrics.Human(fileBudget))
		}
		if s.Note != "" {
			notes = append(notes, s.Label+" "+s.Note)
		}
	}
	top := slices.Clone(srcs)
	slices.SortStableFunc(top, func(a, b agents.ContextSource) int { return b.Bytes - a.Bytes })
	var parts []string
	for _, s := range top[:min(3, len(top))] {
		parts = append(parts, fmt.Sprintf("%s ≈%s", s.Label, metrics.Human(int64(agents.EstimateTokens(s.Bytes)))))
	}
	tok := agents.EstimateTokens(total)
	detail = fmt.Sprintf("≈%s tokens de archivos en cada llamada de la sesión principal (%s)", metrics.Human(int64(tok)), strings.Join(parts, ", "))
	if tok > startupBudget {
		notes = append(notes, "el total pasa de "+metrics.Human(startupBudget))
	}
	if len(notes) == 0 {
		return "ok", detail
	}
	return "warn", detail + ". " + strings.Join(notes, "; ") + ". Poda lo que ya no aplica o muévelo a archivos que se lean al necesitarlos"
}

// checkRepo confirma que el token ve el repositorio, en los hosts que saben
// comprobarlo: un token válido sin acceso al repo falla recién al abrir el PR.
func checkRepo(ctx context.Context, h vcs.Host) error {
	if r, ok := h.(interface{ CheckRepo(context.Context) error }); ok {
		return r.CheckRepo(ctx)
	}
	return nil
}
