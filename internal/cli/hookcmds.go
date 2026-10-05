package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/envcheck"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/metrics"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/review"
	"innobytes.tech/bflow/internal/store"
)

func init() {
	Register(&Command{Name: "guard", Summary: "hook PreToolUse: lee la acción por stdin y la bloquea (exit 2) si rompe una regla",
		Setup: func(fs *flag.FlagSet) {
			fs.String("tool", "", "herramienta cuya entrada se lee (opencode); vacío: Claude Code")
			fs.Bool("reads", false, "el hook también ve Read: registra las lecturas del reviewer en quality")
		},
		Run: runGuard})
	Register(&Command{Name: "hook session-start", Summary: "hook de inicio de sesión: entorno, compromisos y tarea activa en pocas líneas",
		Run: runSessionStart})
	Register(&Command{Name: "hook subagent-stop", Summary: "hook de fin de subagente: si un agente de bflow terminó sin reportar, lo hace seguir (máximo 2 veces) y después bloquea la tarea",
		Run: runSubagentStop})
}

// runSubagentStop nunca falla: un hook roto no debe atorar al subagente.
func runSubagentStop(c *Ctx) output.Envelope {
	quiet := output.Envelope{OK: true, Code: "stop", Quiet: true}
	raw, _ := io.ReadAll(c.Stdin)
	if c.Agent == nil || c.Build == nil {
		return quiet
	}
	sub, cwd, ok := c.Agent.SubagentStopped(raw)
	name, ours := strings.CutPrefix(sub, flow.SubagentPrefix)
	if !ok || !ours {
		return quiet
	}
	dir := c.Dir
	if cwd != "" {
		dir = cwd
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return quiet
	}
	e, err := c.Build(cfg.Root)
	if err != nil {
		return quiet
	}
	reason, err := e.Nudge(context.Background(), name)
	if err != nil || reason == "" {
		return quiet
	}
	env := output.OK("keep_working", map[string]any{"agent": name, "reason": reason}, nil)
	env.Text = c.Agent.KeepWorking(reason)
	return env
}

func runGuard(c *Ctx) output.Envelope {
	raw, _ := io.ReadAll(c.Stdin)
	allowed := output.Envelope{OK: true, Code: "allowed", Quiet: true}
	var acts []guard.Action
	cwd := ""
	if tool := str(c.Flags, "tool"); tool != "" {
		h := c.hooks(tool)
		if h == nil {
			return allowed
		}
		var ok bool
		if acts, cwd, ok = h.ParseActions(raw); !ok {
			return allowed // sin la forma esperada no se bloquea: el guard es abierto
		}
	} else {
		var a guard.Action
		ok := false
		if c.Agent != nil {
			a, cwd, ok = c.Agent.ParsePreToolUse(raw)
		}
		if !ok && json.Unmarshal(raw, &a) != nil {
			return output.Fail("bad_input", fmt.Errorf("entrada de guard no reconocida"))
		}
		acts = []guard.Action{a}
	}
	dir := c.Dir
	if cwd != "" {
		dir = cwd
	}
	cfg, err := config.Load(dir)
	if err != nil {
		// Con la config rota no se bloquea al agente: lo reporta cualquier otro comando.
		return allowed
	}
	if allReads(acts) {
		recordReads(c, cfg, acts, cwd, str(c.Flags, "reads") == "true") // un Read no se evalúa (R9)
		return allowed
	}
	// Las acciones se evalúan en orden: la primera rechazada da el registro y el motivo.
	for _, a := range acts {
		gc := guardContext(cfg)
		if needsTask(a, cfg) && c.Build != nil {
			if e, err := c.Build(cfg.Root); err == nil {
				if id, err := e.Active(context.Background()); err == nil {
					if v, err := e.Status(context.Background(), id); err == nil {
						gc.Phase = v.Phase
					}
					gc.Frozen = e.Frozen(id)
				}
			}
		}
		d := guard.Evaluate(a, gc)
		if d.Allow {
			continue
		}
		fmt.Fprintln(c.Stderr, "bflow guard: "+d.Reason)
		logGuard(c, cfg.Root, d.Rule, a)
		env := output.Rejected(d.Rule, d.Reason)
		env.Quiet = !c.JSON
		return env
	}
	recordReads(c, cfg, acts, cwd, str(c.Flags, "reads") == "true")
	return allowed
}

// isReviewer dice si la acción es del subagente reviewer de bflow.
func isReviewer(a guard.Action) bool {
	name, ok := strings.CutPrefix(a.Agent, flow.SubagentPrefix)
	return ok && name == "reviewer"
}

// allReads dice si hay acciones y todas son Read (se registran sin evaluar).
func allReads(acts []guard.Action) bool {
	for _, a := range acts {
		if a.Tool != guard.Read {
			return false
		}
	}
	return len(acts) > 0
}

// readAgent es el agente que se anota en reads-all.jsonl (R2, R3).
func readAgent(a guard.Action) string {
	if a.Agent != "" {
		return strings.TrimPrefix(a.Agent, flow.SubagentPrefix)
	}
	if a.Subagent {
		return metrics.UnknownAgent
	}
	return metrics.MainSession
}

// readTool es la herramienta que se anota: "claude" sin --tool (R8).
func readTool(c *Ctx) string {
	if t := str(c.Flags, "tool"); t != "" {
		return t
	}
	return "claude"
}

// recordReads anota las lecturas de todos los agentes en reads-all.jsonl y, en
// quality, las del reviewer en reads.jsonl (cobertura). Nunca bloquea ni falla.
func recordReads(c *Ctx, cfg *config.Config, acts []guard.Action, cwd string, readHook bool) {
	if c.Build == nil || len(acts) == 0 {
		return
	}
	relevant := false
	for _, a := range acts {
		if a.Tool == guard.Read || isReviewer(a) {
			relevant = true
			break
		}
	}
	if !relevant {
		return
	}
	e, err := c.Build(cfg.Root)
	if err != nil {
		return
	}
	id, err := e.Active(context.Background())
	if err != nil {
		return
	}
	rec, err := e.Store.Load(id)
	if err != nil {
		return
	}
	if cwd == "" {
		cwd = c.Dir
	}
	now := time.Now()
	var all, mine [][]byte
	for _, a := range acts {
		if n, ok := review.Normalize(cfg.Root, cwd, a.Path); ok && a.Tool == guard.Read {
			ev := metrics.ReadEvent{TS: now, Agent: readAgent(a), Phase: string(rec.Flow.Phase), Path: n, Partial: a.Partial, Tool: readTool(c)}
			if b, err := json.Marshal(ev); err == nil {
				all = append(all, b)
			}
		}
		if rec.Flow.Phase == flow.Quality && isReviewer(a) {
			if b, err := json.Marshal(review.FromAction(a, cfg.Root, cwd, readHook, now)); err == nil {
				mine = append(mine, b)
			}
		}
	}
	if len(all) > 0 {
		_ = e.Store.AppendFile(id, metrics.ReadsAllFile, all)
	}
	if len(mine) > 0 {
		_ = e.Store.AppendFile(id, review.ReadsFile, mine)
	}
}

// guardContext arma lo que el guard sabe del repo, sin la fase de la tarea.
func guardContext(cfg *config.Config) guard.Context {
	gc := guard.Context{Root: cfg.Root, Protected: cfg.VCS.ProtectedBranches, ProtectedPaths: cfg.Guard.ProtectedPaths,
		TestPatterns: cfg.Guard.TestPatterns, ForbidCoauthor: cfg.Guard.ForbidCoauthor, StrictLeader: cfg.Guard.StrictLeader,
		MaxDiffLines: cfg.Guard.MaxDiffLines, DiffLines: func() (int, error) { return diffLines(cfg) }}
	for _, st := range cfg.Check.Steps {
		if st.Accept != "" {
			gc.HumanFiles = append(gc.HumanFiles, filepath.ToSlash(filepath.Clean(st.Accept)))
		}
	}
	return gc
}

// logGuard registra el bloqueo en la tarea activa para medir la fricción. Solo
// corre al bloquear; sin tarea activa no hay a quién atribuirlo.
func logGuard(c *Ctx, root, rule string, a guard.Action) {
	if c.Build == nil {
		return
	}
	e, err := c.Build(root)
	if err != nil {
		return
	}
	if id, err := e.Active(context.Background()); err == nil {
		_ = e.Store.Append(store.Entry{TS: time.Now(), ID: id, Event: "guard", By: e.User, Agent: a.Agent,
			Data: map[string]any{"rule": rule, "command": truncateCmd(a.Command)}})
	}
}

// needsTask dice si la regla necesita la fase y las pruebas congeladas (cuesta
// leer el estado; el guard corre antes de cada herramienta).
func needsTask(a guard.Action, cfg *config.Config) bool {
	switch a.Tool {
	case guard.Edit, guard.Write:
		return guard.MatchesTest(cfg.Guard.TestPatterns, a.Path)
	case guard.Bash:
		return strings.Contains(a.Command, "rm ") || strings.Contains(a.Command, "mv ") || strings.Contains(a.Command, "del ") ||
			guard.TaskScoped(a.Command)
	}
	return false
}

// diffLines suma líneas agregadas y borradas de la rama más lo que está en stage.
func diffLines(cfg *config.Config) (int, error) {
	base := cfg.VCS.BaseBranch
	if base == "" {
		return 0, nil
	}
	if cfg.VCS.Remote != "" {
		base = cfg.VCS.Remote + "/" + base
	}
	total := 0
	for _, args := range [][]string{{"diff", "--numstat", base + "...HEAD"}, {"diff", "--numstat", "--cached"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = cfg.Root
		out, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		for _, l := range strings.Split(string(out), "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			add, _ := strconv.Atoi(f[0])
			del, _ := strconv.Atoi(f[1])
			total += add + del
		}
	}
	return total, nil
}

// runSessionStart junta en pocas líneas lo que la sesión necesita al empezar.
// Nunca falla: un hook de inicio que falla no debe impedir trabajar.
func runSessionStart(c *Ctx) output.Envelope {
	io.Copy(io.Discard, c.Stdin)
	ctx := context.Background()
	var lines []string
	data := map[string]any{}
	e, err := engineFor(c)
	if err != nil {
		env := output.OK("session", map[string]any{"error": err.Error()}, nil)
		env.Text = "bflow: " + err.Error()
		return env
	}
	ec := e.Cfg.Env
	if len(ec.HealthPaths)+len(ec.TCP)+len(ec.RequireEnv) > 0 {
		res := envcheck.Run(ctx, ec, os.Getenv)
		data["env_failed"] = envcheck.Failed(res)
		if envcheck.Failed(res) > 0 {
			lines = append(lines, envcheck.Text(res, true))
		}
	}
	if rep, err := e.Panel(ctx, true); err == nil {
		data["panel"] = rep
		if len(rep.Items) > 0 || len(rep.Closed) > 0 {
			lines = append(lines, renderPanel(rep))
		}
	} else {
		lines = append(lines, "⚠ panel: "+err.Error())
	}
	var next *output.Next
	if id, err := e.Active(ctx); err == nil {
		if v, err := e.Status(ctx, id); err == nil {
			brief := viewEnvelope(v, true)
			lines = append(lines, brief.Text)
			data["task"] = v.ID
			n := v.Next
			next = &n
		}
	} else if views, err := e.Views(ctx); err == nil && len(views) > 1 {
		lines = append(lines, fmt.Sprintf("bflow: %d tareas en curso: %s (bflow status <ID>)", len(views), strings.Join(engine.SortedIDs(views), ", ")))
	}
	if len(lines) == 0 {
		lines = append(lines, "bflow: sin tareas en curso")
	}
	if next != nil {
		if c := compactNext(*next); c != "" {
			lines = append(lines, "next: "+c, "Sigue con /bflow.")
		}
	}
	env := output.OK("session", data, next)
	env.Text = strings.Join(lines, "\n")
	return env
}

// truncateCmd corta el comando para el log: basta para saber qué se intentó.
func truncateCmd(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// compactNext serializa el next en una línea y sin display, para el hook de inicio.
func compactNext(n output.Next) string {
	n.Display = ""
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(n); err != nil {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}
