package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/envcheck"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/guard"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
)

func init() {
	Register(&Command{Name: "guard", Summary: "hook PreToolUse: lee la acción por stdin y la bloquea (exit 2) si rompe una regla",
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
	var a guard.Action
	cwd, ok := "", false
	if c.Agent != nil {
		a, cwd, ok = c.Agent.ParsePreToolUse(raw)
	}
	if !ok && json.Unmarshal(raw, &a) != nil {
		return output.Fail("bad_input", fmt.Errorf("entrada de guard no reconocida"))
	}
	dir := c.Dir
	if cwd != "" {
		dir = cwd
	}
	cfg, err := config.Load(dir)
	if err != nil {
		// Con la config rota no se bloquea al agente: lo reporta cualquier otro comando.
		return output.Envelope{OK: true, Code: "allowed", Quiet: true}
	}
	gc := guard.Context{Root: cfg.Root, Protected: cfg.VCS.ProtectedBranches, ProtectedPaths: cfg.Guard.ProtectedPaths,
		TestPatterns: cfg.Guard.TestPatterns, ForbidCoauthor: cfg.Guard.ForbidCoauthor, StrictLeader: cfg.Guard.StrictLeader,
		MaxDiffLines: cfg.Guard.MaxDiffLines, DiffLines: func() (int, error) { return diffLines(cfg) }}
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
		return output.Envelope{OK: true, Code: "allowed", Quiet: true}
	}
	fmt.Fprintln(c.Stderr, "bflow guard: "+d.Reason)
	logGuard(c, cfg.Root, d.Rule)
	env := output.Rejected(d.Rule, d.Reason)
	env.Quiet = !c.JSON
	return env
}

// logGuard registra el bloqueo en la tarea activa para medir la fricción. Solo
// corre al bloquear; sin tarea activa no hay a quién atribuirlo.
func logGuard(c *Ctx, root, rule string) {
	if c.Build == nil {
		return
	}
	e, err := c.Build(root)
	if err != nil {
		return
	}
	if id, err := e.Active(context.Background()); err == nil {
		_ = e.Store.Append(store.Entry{TS: time.Now(), ID: id, Event: "guard", By: e.User, Data: map[string]any{"rule": rule}})
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
	env := output.OK("session", data, next)
	env.Text = strings.Join(lines, "\n")
	return env
}
