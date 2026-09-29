package cli

import (
	"fmt"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
)

// renderOutcome es el texto para humanos de un comando de flujo.
func renderOutcome(o engine.Outcome) string {
	var b strings.Builder
	head := o.ID + " · "
	if o.To != "" {
		head += fmt.Sprintf("%s → %s", o.From, o.To)
	} else {
		head += string(o.Phase)
	}
	if o.Round > 0 {
		head += fmt.Sprintf(" · ronda %d", o.Round)
	}
	if o.Branch != "" {
		head += " · rama " + o.Branch
	}
	b.WriteString(head + "\n")
	for _, w := range o.Warnings {
		b.WriteString("  ⚠ " + w + "\n")
	}
	b.WriteString(renderNext(o.Next))
	return strings.TrimRight(b.String(), "\n")
}

func renderView(v engine.View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", v.ID, v.Title)
	line := "  fase: " + string(v.Phase)
	if v.Lane != "" {
		line += " · carril " + string(v.Lane)
	}
	if v.Gate != "" {
		line += " · gate " + v.Gate
	}
	if v.Round > 0 {
		line += fmt.Sprintf(" · ronda %d", v.Round)
	}
	if v.Since != nil && !v.Since.IsZero() {
		line += " · hace " + human(time.Since(*v.Since))
	}
	b.WriteString(line + "\n")
	if v.TrackerState != "" {
		l := "  en el tracker: " + v.TrackerState
		if v.TrackerPhase != "" && v.TrackerPhase != flow.Backlog {
			l += fmt.Sprintf(" (%s): si ya estaba en curso con el harness anterior, bflow import --from harness --state-only", v.TrackerPhase)
		}
		b.WriteString(l + "\n")
	}
	if v.Branch != "" {
		b.WriteString("  rama: " + v.Branch + "\n")
	}
	if v.PR != nil {
		b.WriteString("  PR: " + v.PR.URL + "\n")
	}
	if v.Blocked != "" {
		b.WriteString("  bloqueada: " + v.Blocked + "\n")
	}
	if v.Pending > 0 {
		fmt.Fprintf(&b, "  ⚠ %d cambio(s) sin sincronizar con el tracker (bflow sync)\n", v.Pending)
	}
	b.WriteString(renderNext(v.Next))
	return strings.TrimRight(b.String(), "\n")
}

func renderNext(n output.Next) string {
	var b strings.Builder
	switch n.Action {
	case output.ActionAsk:
		fmt.Fprintf(&b, "Siguiente: decidir (gate %s)", n.Gate)
		if n.Skill != "" {
			fmt.Fprintf(&b, " · skill %s", n.Skill)
		}
		if n.Display != "" {
			b.WriteString("\n\n" + n.Display + "\n")
		}
		b.WriteString("\n  " + n.Question + "\n")
		if n.Display == "" {
			for _, s := range n.Show {
				b.WriteString("  antes, mostrar: " + s + "\n")
			}
		}
		for i, o := range n.Options {
			fmt.Fprintf(&b, "  %d. %s", i+1, o.Label)
			if o.Command != "" {
				b.WriteString(" → " + o.Command)
			}
			b.WriteString("\n")
		}
	case output.ActionSpawn:
		mode := "lanzar"
		if n.Parallel {
			mode = "lanzar en paralelo"
		}
		fmt.Fprintf(&b, "Siguiente: %s\n", mode)
		for _, a := range n.Agents {
			fmt.Fprintf(&b, "  · %s (%s)\n    reporta: %s\n", a.Agent, argLine(a.Args), a.Report)
		}
	case output.ActionWait:
		b.WriteString("Siguiente: esperar · " + n.Reason + "\n")
	case output.ActionDone:
		if n.Reason != "" {
			b.WriteString("Nada pendiente · " + n.Reason + "\n")
		}
	}
	return b.String()
}

func argLine(args map[string]string) string {
	var parts []string
	for _, k := range []string{"phase", "round", "resume", "decision", "note"} {
		if v, ok := args[k]; ok && v != "" && v != "0" {
			if len(v) > 60 {
				v = v[:57] + "…"
			}
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}

func human(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "segundos"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
