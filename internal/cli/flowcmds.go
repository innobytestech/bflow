package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
)

// multi es una bandera repetible (--option a --option b).
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

var looksLikeID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-\d+$`)

// engineFor construye el engine del repo actual.
func engineFor(c *Ctx) (*engine.Engine, error) {
	if c.Build == nil {
		return nil, errors.New("bflow se compiló sin adaptadores")
	}
	return c.Build(c.Dir)
}

// resolveID toma el ID de los argumentos o, si no hay, la tarea activa.
func resolveID(ctx context.Context, e *engine.Engine, args []string) (string, []string, error) {
	if len(args) > 0 && looksLikeID.MatchString(args[0]) {
		return strings.ToUpper(args[0]), args[1:], nil
	}
	id, err := e.Active(ctx)
	return id, args, err
}

// fail convierte un error en envelope: los rechazos del flujo son exit 2.
func fail(err error) output.Envelope {
	var rj *flow.Rejection
	if errors.As(err, &rj) {
		return output.Rejected(rj.Code, rj.Reason)
	}
	return output.Fail("error", err)
}

func outcomeEnvelope(o engine.Outcome) output.Envelope {
	data := map[string]any{"id": o.ID, "phase": o.Phase}
	if o.Round > 0 {
		data["round"] = o.Round
	}
	if o.Gate != "" {
		data["gate"] = o.Gate
	}
	if o.Branch != "" {
		data["branch"] = o.Branch
	}
	if o.PR != nil {
		data["pr"] = o.PR
	}
	if len(o.Warnings) > 0 {
		data["warnings"] = o.Warnings
	}
	code := "unchanged"
	if o.To != "" {
		code = "advanced"
	}
	next := o.Next
	env := output.OK(code, data, &next)
	env.Text = renderOutcome(o)
	return env
}

// flowCommand envuelve el patrón común: construir engine, resolver ID, ejecutar.
func flowCommand(run func(ctx context.Context, e *engine.Engine, id string, c *Ctx, rest []string) (engine.Outcome, error)) func(*Ctx) output.Envelope {
	return func(c *Ctx) output.Envelope {
		ctx := context.Background()
		e, err := engineFor(c)
		if err != nil {
			return output.Fail("config", err)
		}
		id, rest, err := resolveID(ctx, e, c.Args)
		if err != nil {
			return fail(err)
		}
		o, err := run(ctx, e, id, c, rest)
		if err != nil {
			return fail(err)
		}
		return outcomeEnvelope(o)
	}
}

func str(fs *flag.FlagSet, name string) string { return fs.Lookup(name).Value.String() }

func init() {
	Register(&Command{Name: "start", Summary: "empieza una tarea en un carril: start <ID> --lane full|light|hotfix [--slug s] [--fixes ID]",
		Setup: func(fs *flag.FlagSet) {
			fs.String("lane", "", "carril")
			fs.String("slug", "", "slug para rama y carpeta del spec")
			fs.String("fixes", "", "hotfix: ID de la feature que corrige")
		},
		Run: func(c *Ctx) output.Envelope {
			if len(c.Args) == 0 {
				return output.Fail("usage", errors.New("uso: bflow start <ID> --lane full|light|hotfix"))
			}
			lane := str(c.Flags, "lane")
			if lane == "" {
				return output.Fail("usage", errors.New("falta --lane (full, light o hotfix); bflow status <ID> muestra las opciones"))
			}
			return flowCommand(func(ctx context.Context, e *engine.Engine, id string, c *Ctx, _ []string) (engine.Outcome, error) {
				return e.Start(ctx, id, flow.Lane(lane), str(c.Flags, "slug"), str(c.Flags, "fixes"))
			})(c)
		}})

	Register(&Command{Name: "approve", Summary: "aprueba el gate pendiente: approve [ID] [--gate g] [--choice n] [--note t] [--file f]",
		Setup: func(fs *flag.FlagSet) {
			fs.String("gate", "", "gate que se aprueba (por seguridad; vacío = el pendiente)")
			fs.Int("choice", 0, "opción elegida en un gate decision")
			fs.String("note", "", "decisión libre o comentario")
			fs.String("file", "", "archivo adjunto (discovery completo)")
		},
		Run: flowCommand(func(ctx context.Context, e *engine.Engine, id string, c *Ctx, _ []string) (engine.Outcome, error) {
			o := engine.ApproveOpts{Gate: str(c.Flags, "gate"), Note: str(c.Flags, "note")}
			fmt.Sscan(str(c.Flags, "choice"), &o.Choice)
			if f := str(c.Flags, "file"); f != "" {
				b, err := os.ReadFile(abs(c.Dir, f))
				if err != nil {
					return engine.Outcome{}, err
				}
				o.Attachment = string(b)
			}
			return e.Approve(ctx, id, o)
		})})

	Register(&Command{Name: "reject", Summary: "rechaza el gate pendiente: reject [ID] --note \"motivo\" [--gate g] [--to fase]",
		Setup: func(fs *flag.FlagSet) {
			fs.String("gate", "", "gate que se rechaza")
			fs.String("to", "", "fase destino alternativa")
			fs.String("note", "", "motivo (obligatorio)")
		},
		Run: flowCommand(func(ctx context.Context, e *engine.Engine, id string, c *Ctx, _ []string) (engine.Outcome, error) {
			return e.Reject(ctx, id, engine.RejectOpts{Gate: str(c.Flags, "gate"), To: str(c.Flags, "to"), Note: str(c.Flags, "note")})
		})})

	Register(&Command{Name: "report", Summary: "un agente reporta su veredicto: report [ID] --agent a --verdict V [--note] [--file] [--option o]...",
		Setup: func(fs *flag.FlagSet) {
			fs.String("agent", "", "nombre del agente")
			fs.String("verdict", "", "veredicto")
			fs.String("note", "", "detalle (problema en NEEDS_DECISION, motivo en BLOCKED)")
			fs.String("file", "", "archivo del reporte")
			fs.Var(&multi{}, "option", "opción para NEEDS_DECISION (repetible)")
		},
		Run: flowCommand(func(ctx context.Context, e *engine.Engine, id string, c *Ctx, _ []string) (engine.Outcome, error) {
			opts := *(c.Flags.Lookup("option").Value.(*multi))
			return e.Report(ctx, id, engine.ReportOpts{Agent: str(c.Flags, "agent"), Verdict: flow.Verdict(str(c.Flags, "verdict")),
				Note: str(c.Flags, "note"), File: str(c.Flags, "file"), Options: opts})
		})})

	Register(&Command{Name: "block", Summary: "bloquea la tarea: block [ID] --reason \"motivo\"",
		Setup: func(fs *flag.FlagSet) { fs.String("reason", "", "motivo") },
		Run: flowCommand(func(ctx context.Context, e *engine.Engine, id string, c *Ctx, _ []string) (engine.Outcome, error) {
			return e.Block(ctx, id, str(c.Flags, "reason"))
		})})

	Register(&Command{Name: "unblock", Summary: "desbloquea la tarea y vuelve a su fase",
		Run: flowCommand(func(ctx context.Context, e *engine.Engine, id string, _ *Ctx, _ []string) (engine.Outcome, error) {
			return e.Unblock(ctx, id)
		})})

	Register(&Command{Name: "freeze", Summary: "vuelve a congelar las pruebas del contrato tal como están (solo una persona): freeze [ID]",
		Run: func(c *Ctx) output.Envelope {
			ctx := context.Background()
			e, err := engineFor(c)
			if err != nil {
				return output.Fail("config", err)
			}
			id, _, err := resolveID(ctx, e, c.Args)
			if err != nil {
				return fail(err)
			}
			n, err := e.Refreeze(id)
			if err != nil {
				return fail(err)
			}
			env := output.OK("refrozen", map[string]any{"id": id, "files": n}, nil)
			env.Text = fmt.Sprintf("%s: %d prueba(s) congelada(s) de nuevo con su contenido actual", id, n)
			return env
		}})

	Register(&Command{Name: "status", Summary: "estado y siguiente paso: status [ID] [--brief]",
		Setup: func(fs *flag.FlagSet) { fs.Bool("brief", false, "resumen de pocas líneas (hooks)") },
		Run:   runStatus})

	Register(&Command{Name: "show", Summary: "muestra un artefacto: show [ID] task|brief|spec|contract|review-map|decisions|discovery|check [--section s]",
		Setup: func(fs *flag.FlagSet) { fs.String("section", "", "sección del spec") },
		Run: func(c *Ctx) output.Envelope {
			ctx := context.Background()
			e, err := engineFor(c)
			if err != nil {
				return output.Fail("config", err)
			}
			id, rest, err := resolveID(ctx, e, c.Args)
			if err != nil {
				return fail(err)
			}
			if len(rest) == 0 {
				return output.Fail("usage", errors.New("uso: bflow show [ID] task|brief|spec|contract|review-map|decisions|discovery|check"))
			}
			text, err := e.Show(ctx, id, rest[0], str(c.Flags, "section"))
			if err != nil {
				return output.Fail("not_found", err)
			}
			env := output.OK("shown", map[string]any{"id": id, "what": rest[0], "content": text}, nil)
			env.Text = text
			if strings.TrimSpace(text) == "" {
				env.Text = fmt.Sprintf("(%s de %s está vacío todavía)", rest[0], id)
			}
			return env
		}})

	Register(&Command{Name: "task add", Summary: "crea una tarea (trackers que lo permiten, como local): task add \"título\" [--description t]",
		Setup: func(fs *flag.FlagSet) { fs.String("description", "", "descripción en Markdown") },
		Run: func(c *Ctx) output.Envelope {
			title := strings.TrimSpace(strings.Join(c.Args, " "))
			if title == "" {
				return output.Fail("usage", errors.New(`uso: bflow task add "título"`))
			}
			e, err := engineFor(c)
			if err != nil {
				return output.Fail("config", err)
			}
			ctx := context.Background()
			task, err := e.CreateTask(ctx, title, str(c.Flags, "description"))
			if err != nil {
				return fail(err)
			}
			view, err := e.Status(ctx, task.ID)
			if err != nil {
				return fail(err)
			}
			next := view.Next
			env := output.OK("created", map[string]any{"id": task.ID, "title": task.Title}, &next)
			env.Text = fmt.Sprintf("%s creada · %s\n%s", task.ID, task.Title, strings.TrimRight(renderNext(next), "\n"))
			return env
		}})

	Register(&Command{Name: "sync", Summary: "reintenta los cambios pendientes con el tracker: sync [ID]",
		Run: func(c *Ctx) output.Envelope {
			ctx := context.Background()
			e, err := engineFor(c)
			if err != nil {
				return output.Fail("config", err)
			}
			ids := []string{}
			if len(c.Args) > 0 {
				ids = append(ids, strings.ToUpper(c.Args[0]))
			} else {
				views, err := e.Views(ctx)
				if err != nil {
					return fail(err)
				}
				for _, v := range views {
					if v.Pending > 0 {
						ids = append(ids, v.ID)
					}
				}
			}
			applied := map[string]int{}
			var remaining int
			for _, id := range ids {
				n, err := e.Sync(ctx, id)
				if err != nil {
					return fail(err)
				}
				applied[id] = n
				if v, err := e.Status(ctx, id); err == nil {
					remaining += v.Pending
				}
			}
			env := output.OK("synced", map[string]any{"applied": applied, "remaining": remaining}, nil)
			env.Text = fmt.Sprintf("sincronizados: %v · pendientes: %d", applied, remaining)
			if remaining > 0 {
				env = env.WithExit(output.ExitError)
				env.OK = false
			}
			return env
		}})
}

func runStatus(c *Ctx) output.Envelope {
	ctx := context.Background()
	e, err := engineFor(c)
	if err != nil {
		return output.Fail("config", err)
	}
	brief := c.Flags.Lookup("brief").Value.String() == "true"
	if len(c.Args) > 0 {
		v, err := e.Status(ctx, strings.ToUpper(c.Args[0]))
		if err != nil {
			return fail(err)
		}
		return viewEnvelope(v, brief)
	}
	views, err := e.Views(ctx)
	if err != nil {
		return fail(err)
	}
	if id, err := e.Active(ctx); err == nil {
		for _, v := range views {
			if v.ID == id {
				return viewEnvelope(v, brief)
			}
		}
	}
	var lines []string
	for _, v := range views {
		l := fmt.Sprintf("%s · %s", v.ID, v.Phase)
		if v.Gate != "" {
			l += " · gate " + v.Gate
		}
		if !brief {
			l += " · " + v.Title
		}
		lines = append(lines, l)
	}
	text := "bflow: sin tareas en curso"
	if len(lines) > 0 {
		text = "bflow: " + fmt.Sprint(len(lines)) + " tareas en curso (indica el ID)\n  " + strings.Join(lines, "\n  ")
	}
	env := output.OK("status_list", map[string]any{"tasks": views}, nil)
	env.Text = text
	return env
}

func viewEnvelope(v engine.View, brief bool) output.Envelope {
	next := v.Next
	env := output.OK("status", map[string]any{"task": v}, &next)
	if brief {
		l := fmt.Sprintf("bflow: %s · %s", v.ID, v.Phase)
		if v.Gate != "" {
			l += " · gate " + v.Gate
		}
		if v.Pending > 0 {
			l += fmt.Sprintf(" · %d sin sincronizar", v.Pending)
		}
		l += " · siguiente: " + next.Action
		if next.Action == output.ActionSpawn && len(next.Agents) > 0 {
			var as []string
			for _, a := range next.Agents {
				as = append(as, a.Agent)
			}
			l += " " + strings.Join(as, ", ")
		}
		env.Text = l + " (bflow status " + v.ID + " --json)"
		return env
	}
	env.Text = renderView(v)
	return env
}

func abs(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func init() {
	Register(&Command{Name: "import", Summary: "importa el harness anterior una vez: import --from harness [--dir ruta] [--state-only] [--dry-run]",
		Setup: func(fs *flag.FlagSet) {
			fs.String("from", "harness", "origen (solo harness)")
			fs.String("dir", "", "raíz del repo con harness/ (por defecto, el repo actual)")
			fs.Bool("state-only", false, "solo .state-times.json: las tareas ya existen en el tracker")
		},
		Run: func(c *Ctx) output.Envelope {
			if str(c.Flags, "from") != "harness" {
				return output.Fail("usage", errors.New("solo se importa --from harness"))
			}
			e, err := engineFor(c)
			if err != nil {
				return output.Fail("config", err)
			}
			dir := e.Cfg.Root
			if d := str(c.Flags, "dir"); d != "" {
				dir = abs(c.Dir, d)
			}
			ctx := context.Background()
			var plan engine.ImportPlan
			if str(c.Flags, "state-only") == "true" {
				plan, err = e.ImportStateTimes(ctx, dir, c.DryRun)
			} else {
				plan, err = e.Import(ctx, dir, c.DryRun)
			}
			if err != nil {
				return fail(err)
			}
			code := "imported"
			if c.DryRun {
				code = "import_plan"
			}
			env := output.OK(code, map[string]any{"tasks": plan.Tasks, "skipped": plan.Skipped}, nil)
			var b strings.Builder
			fmt.Fprintf(&b, "%d tarea(s)", len(plan.Tasks))
			if c.DryRun {
				b.WriteString(" se importarían")
			} else {
				b.WriteString(" importadas")
			}
			for _, t := range plan.Tasks {
				fmt.Fprintf(&b, "\n  %s → %s · %s · %s", t.From, t.ID, t.Phase, t.Title)
			}
			if len(plan.Skipped) > 0 {
				fmt.Fprintf(&b, "\n%d saltadas (cerradas o sin estado)", len(plan.Skipped))
			}
			env.Text = b.String()
			return env
		}})
}

func init() {
	Register(&Command{Name: "pr", Summary: "publica la rama y abre o actualiza el PR de una tarea en in_review: pr [ID]",
		Run: flowCommand(func(ctx context.Context, e *engine.Engine, id string, _ *Ctx, _ []string) (engine.Outcome, error) {
			return e.PR(ctx, id)
		})})

	Register(&Command{Name: "panel", Summary: "compromisos: cierra PR mergeados, sincroniza y avisa SLA: panel [--sla]",
		Setup: func(fs *flag.FlagSet) {
			fs.Bool("sla", false, "comentar en el tracker los gates vencidos y las tareas vencidas")
		},
		Run: func(c *Ctx) output.Envelope {
			e, err := engineFor(c)
			if err != nil {
				return output.Fail("config", err)
			}
			rep, err := e.Panel(context.Background(), str(c.Flags, "sla") == "true")
			if err != nil {
				return fail(err)
			}
			env := output.OK("panel", map[string]any{"items": rep.Items, "more": rep.More, "closed": rep.Closed,
				"reminded": rep.Reminded, "warnings": rep.Warnings}, nil)
			env.Text = renderPanel(rep)
			return env
		}})
}

func renderPanel(rep engine.PanelReport) string {
	var b strings.Builder
	for _, id := range rep.Closed {
		fmt.Fprintf(&b, "✅ %s cerrada: PR mergeado\n", id)
	}
	if len(rep.Items) == 0 {
		b.WriteString("📋 sin compromisos pendientes\n")
	} else {
		fmt.Fprintf(&b, "📋 compromisos (%d)\n", len(rep.Items)+rep.More)
	}
	for _, it := range rep.Items {
		mark := "🟡"
		if it.SLA || it.Overdue {
			mark = "🔴"
		}
		title := it.Title
		if len([]rune(title)) > 40 {
			title = string([]rune(title)[:39]) + "…"
		}
		l := fmt.Sprintf("%s %s %s · %s", mark, it.ID, title, it.Phase)
		if it.Gate != "" {
			l += fmt.Sprintf(" · espera %s %dh", it.Gate, it.Hours)
		}
		if it.Overdue {
			l += " · VENCIDA " + it.Due
		} else if it.Due != "" {
			l += " · due " + it.Due
		}
		b.WriteString(l + "\n")
	}
	if rep.More > 0 {
		fmt.Fprintf(&b, "… y %d más\n", rep.More)
	}
	for _, r := range rep.Reminded {
		b.WriteString("  ↳ comentado: " + r + "\n")
	}
	for _, w := range rep.Warnings {
		b.WriteString("  ⚠ " + w + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
