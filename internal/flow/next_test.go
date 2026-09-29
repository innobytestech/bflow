package flow

import (
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/output"
)

func optIDs(n output.Next) []string {
	var ids []string
	for _, o := range n.Options {
		ids = append(ids, o.ID)
	}
	return ids
}

func TestNextByState(t *testing.T) {
	cfg := testConfig()
	cases := []struct {
		name    string
		s       State
		action  string
		gate    string
		skill   string
		options string // ids separados por coma
		show    string
		agents  string // agentes separados por coma
	}{
		{name: "backlog", s: New("T-1", "demo"), action: "ask", gate: "lane", options: "full,light,hotfix"},
		{name: "discovery", s: stateFor(row{lane: Full, from: Discovery, gate: GateDiscovery}), action: "ask", gate: "discovery", skill: "discovery", options: "approve"},
		{name: "spec trabajando", s: stateFor(row{lane: Full, from: Spec}), action: "spawn", agents: "spec-author"},
		{name: "spec gate", s: stateFor(row{lane: Full, from: Spec, gate: GateSpec}), action: "ask", gate: "spec", skill: "approve", options: "approve,changes", show: "bflow show T-1 brief"},
		{name: "split", s: stateFor(row{lane: Full, from: Spec, gate: GateSplit}), action: "ask", gate: "split", options: "approve,keep"},
		{name: "contract full", s: stateFor(row{lane: Full, from: Contract, gate: GateContract}), action: "ask", gate: "contract", options: "approve,adjust,spec", show: "bflow show T-1 contract"},
		{name: "decision", s: stateFor(row{lane: Full, from: Implementing, gate: GateDecision}), action: "ask", gate: "decision", options: "opt1,opt2,other,contract,spec"},
		{name: "decision hotfix", s: stateFor(row{lane: Hotfix, from: Implementing, gate: GateDecision}), action: "ask", gate: "decision", options: "opt1,opt2,other"},
		{name: "pause", s: stateFor(row{lane: Full, from: Paused, gate: GatePause}), action: "ask", gate: "pause", options: "approve,fix"},
		{name: "quality paralelo", s: stateFor(row{lane: Full, from: Quality}), action: "spawn", agents: "reviewer,security-auditor"},
		{name: "quality falta uno", s: stateFor(row{lane: Full, from: Quality, reports: ok}), action: "spawn", agents: "security-auditor"},
		{name: "rounds full", s: stateFor(row{lane: Full, from: Implementing, gate: GateRounds, round: 2}), action: "ask", gate: "rounds", options: "split,spec,approve"},
		{name: "rounds hotfix", s: stateFor(row{lane: Hotfix, from: Implementing, gate: GateRounds, round: 2}), action: "ask", gate: "rounds", options: "split,approve"},
		{name: "walkthrough full", s: stateFor(row{lane: Full, from: Walkthrough, gate: GateWalkthrough}), action: "ask", gate: "walkthrough", skill: "walkthrough", options: "approve,fix,spec", show: "bflow show T-1 review-map"},
		{name: "walkthrough hotfix", s: stateFor(row{lane: Hotfix, from: Walkthrough, gate: GateWalkthrough}), action: "ask", gate: "walkthrough", options: "approve,fix"},
		{name: "in_review", s: stateFor(row{lane: Full, from: InReview}), action: "wait"},
		{name: "done", s: stateFor(row{lane: Full, from: Done}), action: "done"},
		{name: "blocked", s: stateFor(row{lane: Full, from: Blocked}), action: "ask", gate: "blocked", options: "unblock,keep"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := NextFor(cfg, c.s)
			if n.Action != c.action {
				t.Fatalf("action %q, want %q", n.Action, c.action)
			}
			if n.Gate != c.gate {
				t.Errorf("gate %q, want %q", n.Gate, c.gate)
			}
			if c.skill != "" && n.Skill != c.skill {
				t.Errorf("skill %q, want %q", n.Skill, c.skill)
			}
			if c.options != "" && strings.Join(optIDs(n), ",") != c.options {
				t.Errorf("opciones %v, want %s", optIDs(n), c.options)
			}
			if c.show != "" && (len(n.Show) != 1 || n.Show[0] != c.show) {
				t.Errorf("show %v, want %s", n.Show, c.show)
			}
			if c.agents != "" {
				var names []string
				for _, a := range n.Agents {
					names = append(names, a.Agent)
				}
				if strings.Join(names, ",") != c.agents {
					t.Errorf("agentes %v, want %s", names, c.agents)
				}
			}
			if n.Action == "ask" {
				if n.Question == "" {
					t.Error("una pregunta sin texto")
				}
				for _, o := range n.Options {
					if o.ID != "keep" && !strings.HasPrefix(o.Command, "bflow ") {
						t.Errorf("opción %q sin comando bflow: %q", o.ID, o.Command)
					}
					if o.NeedsNote && !strings.Contains(o.Command, "<") {
						t.Errorf("opción %q pide nota pero el comando no tiene marcador: %q", o.ID, o.Command)
					}
				}
			}
		})
	}
}

// Cada comando que Next ofrece tiene que ser aceptado por Apply: si la skill
// ejecuta la opción tal cual (con la nota rellenada), el flujo avanza.
func TestNextOptionsAreExecutable(t *testing.T) {
	cfg := testConfig()
	states := []State{
		stateFor(row{lane: Full, from: Spec, gate: GateSpec}),
		stateFor(row{lane: Full, from: Spec, gate: GateSplit}),
		stateFor(row{lane: Full, from: Contract, gate: GateContract}),
		stateFor(row{lane: Full, from: Implementing, gate: GateDecision}),
		stateFor(row{lane: Full, from: Paused, gate: GatePause}),
		stateFor(row{lane: Full, from: Implementing, gate: GateRounds, round: 2}),
		stateFor(row{lane: Light, from: Walkthrough, gate: GateWalkthrough}),
		stateFor(row{lane: Full, from: Discovery, gate: GateDiscovery}),
		New("T-1", "demo"),
	}
	for _, s := range states {
		for _, o := range NextFor(cfg, s).Options {
			ev, ok := eventFromCommand(o.Command)
			if !ok {
				continue
			}
			if _, err := Apply(cfg, s, ev); err != nil {
				t.Errorf("%s[%s] opción %q (%s): %v", s.Phase, gateName(s), o.ID, o.Command, err)
			}
		}
	}
}

// eventFromCommand traduce un comando de opción al evento que produciría.
// Es un parser mínimo para la prueba; el real vive en la CLI.
func eventFromCommand(c string) (Event, bool) {
	f := strings.Fields(c)
	if len(f) < 3 {
		return Event{}, false
	}
	var ev Event
	switch f[1] {
	case "approve":
		ev.Kind = EvApprove
	case "reject":
		ev.Kind = EvReject
	case "start":
		ev.Kind = EvStart
	case "block":
		ev.Kind = EvBlock
	case "unblock":
		ev.Kind = EvUnblock
	default:
		return ev, false
	}
	for i := 3; i < len(f); i++ {
		val := ""
		if i+1 < len(f) {
			val = f[i+1]
		}
		switch f[i] {
		case "--gate":
			ev.Gate = Gate(val)
		case "--to":
			ev.To = Phase(val)
		case "--lane":
			ev.Lane = Lane(val)
		case "--choice":
			ev.Choice = int(val[0] - '0')
		case "--note", "--reason":
			ev.Note = "texto del humano"
		case "--file":
			ev.Attachment = "discovery"
		}
	}
	return ev, true
}

func TestSpawnArgs(t *testing.T) {
	s := stateFor(row{lane: Full, from: Implementing, gate: GateDecision})
	res, err := Apply(testConfig(), s, approveChoice(1))
	if err != nil {
		t.Fatal(err)
	}
	n := res.Next
	if n.Action != "spawn" || len(n.Agents) != 1 {
		t.Fatalf("next: %+v", n)
	}
	a := n.Agents[0]
	want := map[string]string{"id": "T-1", "lane": "full", "phase": "implementing",
		"spec": "specs/T-1-demo/spec.md", "decision": "opción A", "resume": "true"}
	for k, v := range want {
		if a.Args[k] != v {
			t.Errorf("args[%s]=%q, want %q", k, a.Args[k], v)
		}
	}
	if !strings.Contains(a.Report, "--verdict DONE|NEEDS_DECISION|BLOCKED") {
		t.Errorf("report: %s", a.Report)
	}
}

func TestSpecGateShowsUIBlueprint(t *testing.T) {
	s := stateFor(row{lane: Full, from: Spec, gate: GateSpec})
	cfg := testConfig()
	if n := NextFor(cfg, s); len(n.Show) != 1 {
		t.Errorf("sin UI solo el brief: %v", n.Show)
	}
	cfg.UI = true
	n := NextFor(cfg, s)
	if len(n.Show) != 2 || n.Show[1] != "bflow show T-1 spec --section ui-blueprint" {
		t.Errorf("con UI el humano ve el blueprint al aprobar: %v", n.Show)
	}
}

func TestUpcoming(t *testing.T) {
	cfg := DefaultConfig()
	st := func(lane Lane, p Phase, g Gate) State {
		s := State{ID: "T-1", Lane: lane, Phase: p}
		if g != "" {
			s.Gate = &PendingGate{Name: g, Agent: "implementer"}
		}
		return s
	}
	for _, c := range []struct {
		s    State
		want string
	}{
		{st(Light, Spec, ""), "spec gate=spec"},
		{st(Light, Spec, GateSpec), "implementing agentes=implementer"},
		{st(Full, Discovery, GateDiscovery), "spec agentes=spec-author"},
		{st(Full, Spec, GateSpec), "contract agentes=implementer"},
		{st(Full, Contract, ""), "contract gate=contract"},
		{st(Full, Contract, GateContract), "implementing agentes=implementer"},
		{st(Full, Implementing, ""), "paused gate=pause"},
		{st(Full, Paused, GatePause), "quality agentes=reviewer"},
		{st(Light, Implementing, ""), "quality agentes=reviewer"},
		{st(Light, Implementing, GateDecision), "implementing agentes=implementer"},
		{st(Light, Quality, ""), "documenting agentes=documenter"},
		{st(Light, Documenting, ""), "walkthrough gate=questions"},
		{st(Light, Walkthrough, GateQuestions), "walkthrough gate=walkthrough"},
		{st(Light, Walkthrough, GateWalkthrough), "in_review merge"},
		{st(Light, InReview, ""), "done"},
		{st(Light, Blocked, ""), ""},
		{st(Light, Done, ""), ""},
	} {
		u := Upcoming(cfg, c.s)
		got := string(u.Phase)
		switch {
		case u.Gate != "":
			got += " gate=" + string(u.Gate)
		case len(u.Agents) > 0:
			got += " agentes=" + strings.Join(u.Agents, ",")
		case u.Merge:
			got += " merge"
		}
		if got != c.want {
			t.Errorf("%s/%s gate %v: %q, want %q", c.s.Lane, c.s.Phase, c.s.Gate, got, c.want)
		}
	}
}
