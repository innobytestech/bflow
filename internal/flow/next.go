package flow

import (
	"fmt"
	"strconv"
	"strings"

	"innobytes.tech/bflow/internal/output"
)

// SpecPath es la ruta del spec de una tarea, relativa a la raíz del repo.
func SpecPath(id, slug string) string {
	if slug == "" {
		return "specs/" + id + "/spec.md"
	}
	return "specs/" + id + "-" + slug + "/spec.md"
}

// TaskDir es la carpeta de trabajo (ignorada por git) de una tarea.
func TaskDir(id string) string { return ".bflow/tasks/" + id }

// NextFor dice qué debe hacer la sesión principal con la tarea en el estado s.
func NextFor(cfg Config, s State) output.Next {
	switch s.Phase {
	case Done:
		return output.Next{Action: output.ActionDone, Reason: "tarea cerrada"}
	case InReview:
		return output.Next{Action: output.ActionWait,
			Reason: "se espera el merge del PR; `bflow panel` cierra la tarea al detectarlo"}
	case Blocked:
		reason := ""
		if s.Block != nil {
			reason = s.Block.Reason
		}
		return output.Next{Action: output.ActionAsk, Gate: "blocked",
			Question: fmt.Sprintf("%s está bloqueada: %s. ¿Se resolvió?", s.ID, reason),
			Options: []output.Option{
				{ID: "unblock", Label: "Desbloquear y continuar", Command: cmd("unblock", s.ID)},
				{ID: "keep", Label: "Sigue bloqueada"},
			}}
	case Backlog:
		return output.Next{Action: output.ActionAsk, Gate: string(GateLane),
			Question: fmt.Sprintf("¿En qué carril va %s?", s.ID),
			Options:  laneOptions(cfg, s.ID)}
	}
	if s.Gate != nil {
		return gateNext(cfg, s)
	}
	return spawnNext(cfg, s)
}

func cmd(verb, id string, extra ...string) string {
	return strings.Join(append([]string{"bflow", verb, id}, extra...), " ")
}

var laneHelp = map[Lane]string{
	Full:   "Feature normal: discovery, spec, contrato T1, implementación, calidad, walkthrough.",
	Light:  "Feature pequeña (pocos archivos, sin migración, sin cambio de contrato ni permisos): spec breve, sin T1.",
	Hotfix: "Defecto ya mergeado que no añade alcance: directo a implementar; la compuerta de calidad no se salta.",
}

func laneOptions(cfg Config, id string) []output.Option {
	var out []output.Option
	for _, l := range []Lane{Full, Light, Hotfix} {
		if _, ok := cfg.Lanes[l]; ok {
			out = append(out, output.Option{ID: string(l), Label: string(l), Description: laneHelp[l],
				Command: cmd("start", id, "--lane", string(l))})
		}
	}
	for l := range cfg.Lanes {
		if _, known := laneHelp[l]; !known {
			out = append(out, output.Option{ID: string(l), Label: string(l), Command: cmd("start", id, "--lane", string(l))})
		}
	}
	return out
}

func approveOpt(id string, g Gate, label string) output.Option {
	return output.Option{ID: "approve", Label: label, Command: cmd("approve", id, "--gate", string(g))}
}

func rejectOpt(id string, g Gate, optID, label string, to Phase) output.Option {
	extra := []string{"--gate", string(g)}
	if to != "" {
		extra = append(extra, "--to", string(to))
	}
	extra = append(extra, `--note "<motivo>"`)
	return output.Option{ID: optID, Label: label, NeedsNote: true, Command: cmd("reject", id, extra...)}
}

func gateNext(cfg Config, s State) output.Next {
	id, g := s.ID, s.Gate
	n := output.Next{Action: output.ActionAsk, Gate: string(g.Name)}
	switch g.Name {
	case GateDiscovery:
		n.Skill = "discovery"
		n.Question = "Conduce el discovery (alcance, datos clave, errores, restricciones; máx. 3 preguntas por tanda). Cuando no quede ambigüedad, escribe el discovery completo en un archivo y ciérralo."
		n.Options = []output.Option{{ID: "approve", Label: "Cerrar discovery",
			Command: cmd("approve", id, "--gate", "discovery", "--file", "<discovery.md>")}}
	case GateSpec:
		n.Skill = "approve"
		n.Show = []string{cmd("show", id, "brief")}
		if cfg.UI {
			n.Show = append(n.Show, cmd("show", id, "spec", "--section", "ui-blueprint"))
		}
		n.Question = "¿Apruebas el spec?"
		n.Options = []output.Option{
			approveOpt(id, g.Name, "Aprobar"),
			rejectOpt(id, g.Name, "changes", "Pedir cambios puntuales", ""),
		}
	case GateSplit:
		n.Question = "El spec-author propone dividir la feature: " + g.Note
		n.Options = []output.Option{
			approveOpt(id, g.Name, "Dividir (la tarea queda bloqueada hasta crear las nuevas)"),
			rejectOpt(id, g.Name, "keep", "Seguir como una sola feature", ""),
		}
	case GateContract:
		n.Show = []string{cmd("show", id, "contract")}
		n.Question = "¿Apruebas el contrato T1 (interfaces, firmas y nombres de pruebas)?"
		n.Options = []output.Option{
			approveOpt(id, g.Name, "Continuar"),
			rejectOpt(id, g.Name, "adjust", "Ajustar el contrato", ""),
		}
		if cfg.has(s.Lane, Spec) {
			n.Options = append(n.Options, rejectOpt(id, g.Name, "spec", "Volver a spec", Spec))
		}
	case GateDecision:
		n.Question = fmt.Sprintf("%s necesita una decisión: %s", g.Agent, g.Note)
		for i, o := range g.Options {
			n.Options = append(n.Options, output.Option{ID: "opt" + strconv.Itoa(i+1), Label: o,
				Command: cmd("approve", id, "--gate", "decision", "--choice", strconv.Itoa(i+1))})
		}
		n.Options = append(n.Options, output.Option{ID: "other", Label: "Otra decisión", NeedsNote: true,
			Command: cmd("approve", id, "--gate", "decision", `--note "<decisión>"`)})
		for _, p := range decisionTargets(cfg, s.Lane, s.Phase) {
			label := "Rehacer el contrato (al aprobarlo se vuelven a congelar las pruebas)"
			if p == Spec {
				label = "Volver a spec"
			}
			n.Options = append(n.Options, rejectOpt(id, GateDecision, string(p), label, p))
		}
	case GatePause:
		n.Question = "La implementación terminó con el check en verde. ¿Lanzar la revisión de calidad?"
		n.Options = []output.Option{
			approveOpt(id, g.Name, "Lanzar revisión"),
			rejectOpt(id, g.Name, "fix", "Volver a implementar", ""),
		}
	case GateRounds:
		n.Question = fmt.Sprintf("La compuerta de calidad rechazó %d rondas. ¿Qué hacemos?", s.Round)
		n.Options = []output.Option{
			{ID: "split", Label: "Dividir la feature", Command: cmd("block", id, `--reason "dividir la feature"`)},
		}
		if cfg.has(s.Lane, Spec) {
			n.Options = append(n.Options, rejectOpt(id, g.Name, "spec", "Volver a spec", ""))
		}
		n.Options = append(n.Options, approveOpt(id, g.Name, "Una ronda más"))
	case GateQuestions:
		n.Skill = "walkthrough"
		n.Show = []string{cmd("show", id, "questions")}
		n.Question = "Antes de ver el código: ¿qué esperas que haga el cambio en estos casos? Tus respuestas se comparan después con lo que hace el código."
		n.Options = []output.Option{
			{ID: "answer", Label: "Ya respondí", NeedsNote: true, Command: cmd("approve", id, "--gate", string(g.Name), `--note "<respuestas>"`)},
			approveOpt(id, g.Name, "Saltar las preguntas"),
		}
	case GateWalkthrough:
		n.Skill = "walkthrough"
		n.Show = []string{cmd("show", id, "review-map")}
		n.Question = "Recorre el diff con el review-map. ¿Apruebas el PR?"
		n.Options = []output.Option{
			approveOpt(id, g.Name, "Aprobar y abrir PR"),
			rejectOpt(id, g.Name, "fix", "No me convence (volver a implementar)", ""),
		}
		if cfg.has(s.Lane, Spec) {
			n.Options = append(n.Options, rejectOpt(id, g.Name, "spec", "Volver a spec", Spec))
		}
	}
	return n
}

func spawnNext(cfg Config, s State) output.Next {
	agents := cfg.Agents[s.Phase]
	var pending []string
	for _, a := range agents {
		if _, done := s.Reports[a]; !done {
			pending = append(pending, a)
		}
	}
	if len(pending) == 0 {
		return output.Next{Action: output.ActionDone, Reason: "sin trabajo pendiente en " + string(s.Phase)}
	}
	parallel := s.Phase == Quality
	if !parallel {
		pending = pending[:1]
	}
	n := output.Next{Action: output.ActionSpawn, Parallel: parallel && len(pending) > 1}
	for _, a := range pending {
		args := map[string]string{
			"id":    s.ID,
			"lane":  string(s.Lane),
			"phase": string(s.Phase),
		}
		if cfg.has(s.Lane, Spec) {
			args["spec"] = SpecPath(s.ID, s.Slug)
		}
		if s.Round > 0 {
			args["round"] = strconv.Itoa(s.Round)
		}
		if s.Note != "" {
			args["note"] = s.Note
		}
		if s.Decision != "" {
			args["decision"] = s.Decision
		}
		if s.Resume && !parallel {
			args["resume"] = "true"
		}
		n.Agents = append(n.Agents, output.AgentCall{Agent: a, Subagent: SubagentPrefix + a, Args: args,
			Report: cmd("report", s.ID, "--agent", a, "--verdict", joinVerdicts(Verdicts(s.Phase)))})
	}
	return n
}

// Step es lo que viene después del paso actual, para el panel de una persona:
// una gate que decide ella, agentes que trabajan en una fase, o el merge.
type Step struct {
	Phase  Phase
	Gate   Gate     // decide la persona
	Agents []string // o trabajan estos agentes
	Merge  bool     // o se espera el merge del PR
}

// Upcoming dice qué viene después del paso actual si todo sale bien. Vacío
// si no hay un siguiente paso claro (terminada, bloqueada).
func Upcoming(cfg Config, s State) Step {
	enter := func(p Phase) Step {
		switch p {
		case Discovery:
			return Step{Phase: p, Gate: GateDiscovery}
		case Paused:
			return Step{Phase: p, Gate: GatePause}
		case Walkthrough:
			return Step{Phase: p, Gate: GateQuestions}
		case InReview:
			return Step{Phase: p, Merge: true}
		case Done:
			return Step{Phase: p}
		}
		return Step{Phase: p, Agents: cfg.Agents[p]}
	}
	if g := s.Gate; g != nil {
		switch g.Name {
		case GateDiscovery:
			return enter(cfg.after(s.Lane, Discovery))
		case GateSpec:
			return enter(cfg.after(s.Lane, Spec))
		case GateContract:
			return enter(cfg.after(s.Lane, Contract))
		case GatePause:
			return enter(cfg.after(s.Lane, Paused))
		case GateQuestions:
			return Step{Phase: Walkthrough, Gate: GateWalkthrough}
		case GateWalkthrough:
			return enter(cfg.after(s.Lane, Walkthrough))
		case GateSplit:
			return Step{}
		}
		agents := cfg.Agents[s.Phase] // decision, rounds: el agente retoma
		if g.Agent != "" {
			agents = []string{g.Agent}
		}
		return Step{Phase: s.Phase, Agents: agents}
	}
	switch s.Phase {
	case Spec:
		return Step{Phase: Spec, Gate: GateSpec}
	case Contract:
		return Step{Phase: Contract, Gate: GateContract}
	case InReview:
		return Step{Phase: Done}
	case Implementing, Quality, Documenting:
		return enter(cfg.after(s.Lane, s.Phase))
	}
	return Step{}
}
