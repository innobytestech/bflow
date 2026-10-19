package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/agents"
	"innobytes.tech/bflow/internal/config"
	"innobytes.tech/bflow/internal/eval"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
)

func init() {
	Register(&Command{Name: "eval reviewer",
		Summary: "mide qué defectos conocidos atrapa el reviewer: eval reviewer [--craft ref] [--case c]... [--abort] [--from-fixes]",
		Setup: func(fs *flag.FlagSet) {
			fs.String("craft", "", "ref git del que se lee el oficio del reviewer (vacío = el árbol)")
			fs.Var(&multi{}, "case", "corre solo este caso (repetible)")
			fs.Bool("abort", false, "cancela la corrida abierta y quita sus worktrees")
			fs.Bool("from-fixes", false, "crea esqueletos de caso desde los hotfix con --fixes del log")
		},
		Run: runEvalReviewer})
	Register(&Command{Name: "eval reviewer report",
		Summary: "el agente del eval reporta su caso: eval reviewer report <caso> --verdict APPROVED|REJECTED [--note t]",
		Setup: func(fs *flag.FlagSet) {
			fs.String("verdict", "", "APPROVED o REJECTED")
			fs.String("note", "", "motivo si REJECTED")
		},
		Run: runEvalReport})
}

func evalFail(err error) output.Envelope {
	var rj *eval.Rejection
	if errors.As(err, &rj) {
		return output.Rejected(rj.Code, rj.Reason)
	}
	return output.Fail("error", err)
}

// evalAgentFile es el subagente del eval generado para la herramienta del repo.
func evalAgentFile(rel string) bool {
	return strings.HasSuffix(rel, "/"+flow.SubagentPrefix+eval.AgentName+".md")
}

// checkEvalAgent comprueba, con el plan de render, que el agente del eval ya exista.
func checkEvalAgent(c *Ctx, cfg *config.Config) error {
	p, err := planRender(c, cfg)
	if err != nil {
		return err
	}
	for rel := range p.Files {
		if !evalAgentFile(rel) {
			continue
		}
		if _, err := os.Stat(filepath.Join(cfg.Root, filepath.FromSlash(rel))); err != nil {
			return &eval.Rejection{Code: "eval_agent_missing", Reason: "falta " + rel + ": corre `bflow render` y reinicia la sesión para que la herramienta vea el agente nuevo"}
		}
	}
	return nil
}

func runEvalReviewer(c *Ctx) output.Envelope {
	cfg, err := config.Load(c.Dir)
	if err != nil {
		return output.Fail("config", err)
	}
	abort, fromFixes := str(c.Flags, "abort") == "true", str(c.Flags, "from-fixes") == "true"
	if !abort && !fromFixes {
		if err := checkEvalAgent(c, cfg); err != nil {
			return evalFail(err)
		}
	}
	if c.EvalGit == nil {
		return output.Fail("config", errors.New("bflow se compiló sin git"))
	}
	ctx := context.Background()
	runner := &eval.Runner{Root: cfg.Root, Git: c.EvalGit(cfg.Root), Now: time.Now, Version: c.Version,
		Model: agents.Models(cfg.Flow.Core(), cfg.Agents)["reviewer"]}
	cases := *(c.Flags.Lookup("case").Value.(*multi))
	craft := str(c.Flags, "craft")

	if run := runner.Open(); run != "" && !abort && (craft != "" || len(cases) > 0 || fromFixes) {
		return evalFail(&eval.Rejection{Code: "eval_open", Reason: fmt.Sprintf("la corrida %s sigue abierta: termínala con `bflow eval reviewer` o cancélala con `bflow eval reviewer --abort`", run)})
	}
	if fromFixes {
		return evalFromFixes(ctx, c, cfg, runner)
	}
	open := runner.Open()
	step, err := runner.Next(ctx, eval.Opts{Craft: craft, Cases: cases, Abort: abort})
	if err != nil {
		return evalFail(err)
	}
	data := map[string]any{"skipped": orEmpty(step.Skipped), "warnings": orEmpty(step.Warnings)}
	m := bannerFor(c)
	switch {
	case abort:
		data["aborted"] = open
		env := output.OK("eval_aborted", data, nil)
		env.Text = "Sin corrida abierta: nada que cancelar."
		if open != "" {
			env.Text = "Corrida " + open + " cancelada: worktrees y carpeta quitados."
		}
		return env
	case step.Spawn != nil:
		sp := step.Spawn
		data["run"], data["case"] = runner.Open(), sp.Case
		next := &output.Next{Action: output.ActionSpawn, Agents: []output.AgentCall{{
			Agent: eval.AgentName, Subagent: flow.SubagentPrefix + eval.AgentName, Args: sp.Args, Report: sp.Report}},
			Reason: "lanza bflow-eval-reviewer con esos args; cuando termine, corre `bflow eval reviewer` otra vez"}
		env := output.OK("eval_spawn", data, next)
		env.Text = paint(m, cAgent, "AGENTE") + " caso " + paint(m, cBold, sp.Case) + ": lanza " + flow.SubagentPrefix + eval.AgentName +
			"; al terminar, corre `bflow eval reviewer`" + warnText(step.Warnings)
		return env
	default:
		res := step.Result
		t := res.Totals
		data["run"] = res.Run
		data["result"] = map[string]any{"path": step.Path, "totals": t}
		reason := fmt.Sprintf("atrapó %d de %d defectos, %d posibles falsos positivos (%s)", t.Caught, t.Defects, t.FalsePositives, step.Path)
		env := output.OK("eval_done", data, &output.Next{Action: output.ActionDone, Reason: reason})
		env.Text = paint(m, cDone, "Eval terminado") + ": " + reason + warnText(step.Warnings)
		return env
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func warnText(w []string) string {
	var b strings.Builder
	for _, x := range w {
		b.WriteString("\naviso: " + x)
	}
	return b.String()
}

func evalFromFixes(ctx context.Context, c *Ctx, cfg *config.Config, runner *eval.Runner) output.Envelope {
	log, err := store.Open(cfg.Root).Log("")
	if err != nil {
		return output.Fail("error", err)
	}
	created, warnings, err := eval.FromFixes(cfg.Root, log)
	if err != nil {
		return evalFail(err)
	}
	if len(created) > 0 {
		w, err := eval.Versioning(ctx, cfg.Root, runner.Git)
		if err != nil {
			return evalFail(err)
		}
		warnings = append(warnings, w...)
	}
	env := output.OK("eval_from_fixes", map[string]any{"created": created, "warnings": orEmpty(warnings)}, nil)
	env.Text = fmt.Sprintf("%d esqueleto(s) creados en %s: completa base, head y defects, y quita draft: true.", len(created), eval.CasesDir) + warnText(warnings)
	return env
}

func runEvalReport(c *Ctx) output.Envelope {
	if len(c.Args) != 1 {
		return output.Fail("usage", errors.New("uso: bflow eval reviewer report <caso> --verdict APPROVED|REJECTED [--note t]"))
	}
	cfg, err := config.Load(c.Dir)
	if err != nil {
		return output.Fail("config", err)
	}
	if c.EvalGit == nil {
		return output.Fail("config", errors.New("bflow se compiló sin git"))
	}
	runner := &eval.Runner{Root: cfg.Root, Git: c.EvalGit(cfg.Root), Now: time.Now, Version: c.Version,
		Model: agents.Models(cfg.Flow.Core(), cfg.Agents)["reviewer"]}
	res, err := runner.Report(context.Background(), c.Args[0], str(c.Flags, "verdict"), str(c.Flags, "note"))
	if err != nil {
		return evalFail(err)
	}
	env := output.OK("eval_reported", map[string]any{"case": res.Case, "score": res.Score}, nil)
	env.Text = fmt.Sprintf("Caso %s calificado: atrapó %d de %d defectos, %d posibles falsos positivos. Corre `bflow eval reviewer` para seguir.",
		res.Case, len(res.Caught), len(res.Caught)+len(res.Missed), len(res.FalsePositives))
	return env
}
