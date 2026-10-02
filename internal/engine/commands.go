package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/output"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
)

// Start toma la tarea del tracker y la pone en la primera fase del carril.
// fixes (solo hotfix) liga la tarea a la feature que corrige, para sus métricas.
func (e *Engine) Start(ctx context.Context, id string, lane flow.Lane, slug, fixes string) (Outcome, error) {
	if slug != "" {
		slug = Slug(slug)
	}
	fixes = strings.ToUpper(strings.TrimSpace(fixes))
	out, err := e.apply(ctx, id, flow.Event{Kind: flow.EvStart, Lane: lane, Fixes: fixes}, slug)
	if err == nil && fixes != "" {
		if _, lerr := e.Store.Load(fixes); lerr != nil {
			out.Warnings = append(out.Warnings, fixes+" no tiene historial en bflow: el hotfix no sumará a sus métricas")
		}
	}
	return out, err
}

// New crea la tarea en el tracker y la arranca en el carril. Si el carril no
// existe no crea nada. slug es opcional (vacío: se deriva del título). Si Start falla, la tarea ya existe: se devuelve con un
// error que dice cómo reintentar.
func (e *Engine) New(ctx context.Context, lane flow.Lane, title, desc string, slug ...string) (tracker.Task, Outcome, error) {
	if _, ok := e.flowCfg().Lanes[lane]; !ok {
		var names []string
		for l := range e.flowCfg().Lanes {
			names = append(names, string(l))
		}
		sort.Strings(names)
		return tracker.Task{}, Outcome{}, &flow.Rejection{Code: "unknown_lane",
			Reason: fmt.Sprintf("carril desconocido %q (disponibles: %s)", lane, strings.Join(names, ", "))}
	}
	task, err := e.CreateTask(ctx, title, desc)
	if err != nil {
		return tracker.Task{}, Outcome{}, err
	}
	out, err := e.Start(ctx, task.ID, lane, strings.Join(slug, ""), "")
	if err != nil {
		return task, Outcome{ID: task.ID}, fmt.Errorf("%s creada, pero no arrancó: %w. Reintenta con bflow start %s --lane %s", task.ID, err, task.ID, lane)
	}
	return task, out, nil
}

// OpenTasks lista las tareas abiertas del tracker.
func (e *Engine) OpenTasks(ctx context.Context) ([]tracker.Task, error) {
	return e.Tracker.List(ctx, tracker.Filter{OpenOnly: true})
}

// ApproveOpts son las opciones de approve.
type ApproveOpts struct {
	Gate       string
	Choice     int
	Note       string
	Attachment string // discovery completo
}

// Approve aprueba el gate pendiente.
func (e *Engine) Approve(ctx context.Context, id string, o ApproveOpts) (Outcome, error) {
	var pending *flow.PendingGate
	if rec, err := e.Store.Load(id); err == nil {
		pending = rec.Flow.Gate
	}
	out, err := e.apply(ctx, id, flow.Event{Kind: flow.EvApprove, Gate: flow.Gate(o.Gate), Choice: o.Choice, Note: o.Note, Attachment: o.Attachment}, "")
	if err != nil {
		return out, err
	}
	if pending != nil && pending.Name == flow.GateDiscovery && strings.TrimSpace(o.Attachment) != "" {
		if err := e.Store.WriteFile(id, "discovery.md", []byte(o.Attachment)); err != nil {
			out.Warnings = append(out.Warnings, "no se pudo guardar discovery.md: "+err.Error())
		}
	}
	if pending != nil && pending.Name == flow.GateContract && out.To == flow.Implementing {
		if n, err := e.freezeTests(ctx, id); err != nil {
			out.Warnings = append(out.Warnings, "no se pudieron congelar las pruebas: "+err.Error())
		} else if n > 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%d prueba(s) congelada(s): no se editan hasta el PR", n))
		}
	}
	if pending != nil && pending.Name == flow.GateDecision && out.Record != nil {
		line := fmt.Sprintf("- %s · %s: %s → **%s** (%s)\n", e.now().Format("2006-01-02"), pending.Agent,
			oneLine(pending.Note), out.Record.Flow.Decision, e.User)
		if err := e.appendFile(id, "decisions.md", "# Decisiones en vuelo\n\n", line); err != nil {
			out.Warnings = append(out.Warnings, "no se pudo guardar decisions.md: "+err.Error())
		}
	}
	return out, nil
}

// RejectOpts son las opciones de reject.
type RejectOpts struct {
	Gate string
	To   string
	Note string
}

// Reject rechaza el gate pendiente.
func (e *Engine) Reject(ctx context.Context, id string, o RejectOpts) (Outcome, error) {
	return e.apply(ctx, id, flow.Event{Kind: flow.EvReject, Gate: flow.Gate(o.Gate), To: flow.Phase(o.To), Note: o.Note}, "")
}

// ReportOpts son las opciones de report.
type ReportOpts struct {
	Agent   string
	Verdict flow.Verdict
	Note    string
	File    string
	Options []string
}

// Report registra el veredicto de un agente.
func (e *Engine) Report(ctx context.Context, id string, o ReportOpts) (Outcome, error) {
	ev := flow.Event{Kind: flow.EvReport, Agent: o.Agent, Verdict: flow.Verdict(strings.ToUpper(string(o.Verdict))), Note: o.Note, Options: o.Options}
	out, err := e.apply(ctx, id, ev, "")
	if err == nil && o.File != "" {
		_ = e.Store.Append(store.Entry{TS: e.now(), ID: id, Event: "report_file", Agent: o.Agent, By: e.User, Data: map[string]any{"file": o.File}})
	}
	return out, err
}

// Block bloquea la tarea con un motivo.
func (e *Engine) Block(ctx context.Context, id, reason string) (Outcome, error) {
	return e.apply(ctx, id, flow.Event{Kind: flow.EvBlock, Note: reason}, "")
}

// Unblock regresa la tarea a la fase donde se bloqueó.
func (e *Engine) Unblock(ctx context.Context, id string) (Outcome, error) {
	return e.apply(ctx, id, flow.Event{Kind: flow.EvUnblock}, "")
}

// Drop retira la tarea sin terminarla: la cierra en el tracker y la saca de
// status. No toca la rama ni el PR; avisa si existen.
func (e *Engine) Drop(ctx context.Context, id, note string) (Outcome, error) {
	return Outcome{ID: id}, errors.New("no implementado")
}

// Merged cierra una tarea en in_review cuyo PR se mergeó.
func (e *Engine) Merged(ctx context.Context, id string) (Outcome, error) {
	out, err := e.apply(ctx, id, flow.Event{Kind: flow.EvMerged}, "")
	if err == nil {
		_, _ = e.Store.Update(id, func(r *store.Record, _ bool) error {
			if r.PR != nil {
				r.PR.Merged = true
			}
			return nil
		})
	}
	return out, err
}

// View es la foto de una tarea para status.
type View struct {
	ID      string     `json:"id"`
	Title   string     `json:"title,omitempty"`
	URL     string     `json:"url,omitempty"`
	Lane    flow.Lane  `json:"lane,omitempty"`
	Phase   flow.Phase `json:"phase"`
	Gate    string     `json:"gate,omitempty"`
	Round   int        `json:"round,omitempty"`
	Since   *time.Time `json:"since,omitempty"`
	Branch  string     `json:"branch,omitempty"`
	PR      *store.PR  `json:"pr,omitempty"`
	Blocked string     `json:"blocked,omitempty"`
	Pending int        `json:"pending,omitempty"`
	Started bool       `json:"started,omitempty"`
	// Estado en el tracker de una tarea que bflow todavía no empezó.
	TrackerState string      `json:"tracker_state,omitempty"`
	TrackerPhase flow.Phase  `json:"tracker_phase,omitempty"`
	Next         output.Next `json:"-"`
	// Para el panel de una persona (bflow watch); no van en la salida de status.
	Upcoming  flow.Step    `json:"-"` // qué viene si todo sale bien
	Phases    []flow.Phase `json:"-"` // las del carril, en orden
	Reported  []string     `json:"-"` // agentes que ya reportaron en esta fase
	GateSince time.Time    `json:"-"`
	// Dónde estaba al bloquearse.
	BlockedFrom flow.Phase `json:"-"`
}

// Status devuelve la vista de una tarea, empezada o no.
func (e *Engine) Status(ctx context.Context, id string) (View, error) {
	rec, err := e.Store.Load(id)
	if errors.Is(err, store.ErrNotFound) {
		task, terr := e.Tracker.Get(ctx, id)
		if terr != nil {
			return View{}, trackerErr(id, terr)
		}
		s := flow.New(id, Slug(task.Title))
		return View{ID: id, Title: task.Title, URL: task.URL, Phase: flow.Backlog, TrackerState: task.State, TrackerPhase: task.Phase,
			Next: flow.NextFor(e.flowCfg(), s)}, nil
	}
	if err != nil {
		return View{}, err
	}
	v := e.view(rec)
	v.Next = e.withDisplay(ctx, rec, v.Next)
	return v, nil
}

func (e *Engine) view(rec store.Record) View {
	v := View{ID: rec.Flow.ID, Title: rec.Title, URL: rec.URL, Lane: rec.Flow.Lane, Phase: rec.Flow.Phase, Round: rec.Flow.Round,
		Since: &rec.Since, Branch: rec.Branch, PR: rec.PR, Pending: len(rec.Pending), Started: true,
		Next: flow.NextFor(e.flowCfg(), rec.Flow), Upcoming: flow.Upcoming(e.flowCfg(), rec.Flow), GateSince: rec.GateSince,
		Phases: e.flowCfg().Lanes[rec.Flow.Lane]}
	for _, a := range slices.Sorted(maps.Keys(rec.Flow.Reports)) {
		v.Reported = append(v.Reported, a)
	}
	if rec.Flow.Gate != nil {
		v.Gate = string(rec.Flow.Gate.Name)
	}
	if rec.Flow.Block != nil {
		v.Blocked = rec.Flow.Block.Reason
		v.BlockedFrom = rec.Flow.Block.From
	}
	return v
}

// Views devuelve todas las tareas empezadas y no cerradas.
func (e *Engine) Views(ctx context.Context) ([]View, error) {
	recs, err := e.Store.List()
	if err != nil {
		return nil, err
	}
	var out []View
	for _, r := range recs {
		if r.Flow.Phase != flow.Done {
			out = append(out, e.view(r))
		}
	}
	return out, nil
}

// Active resuelve la tarea sobre la que actuar cuando no se da ID: la de la
// rama actual; si no, la única tarea abierta.
func (e *Engine) Active(ctx context.Context) (string, error) {
	views, err := e.Views(ctx)
	if err != nil {
		return "", err
	}
	if e.Git != nil {
		if br, err := e.Git.CurrentBranch(ctx); err == nil && br != "" {
			for _, v := range views {
				if v.Branch == br {
					return v.ID, nil
				}
			}
		}
	}
	switch len(views) {
	case 0:
		return "", &flow.Rejection{Code: "no_active_task", Reason: "no hay tareas en curso; indica el ID (bflow status <ID>) o empieza una con bflow start <ID> --lane <carril>"}
	case 1:
		return views[0].ID, nil
	}
	var ids []string
	for _, v := range views {
		ids = append(ids, fmt.Sprintf("%s (%s)", v.ID, v.Phase))
	}
	return "", &flow.Rejection{Code: "ambiguous_task", Reason: "hay varias tareas en curso; indica cuál: " + strings.Join(ids, ", ")}
}

// CreateTask crea una tarea si el tracker lo permite.
func (e *Engine) CreateTask(ctx context.Context, title, desc string) (tracker.Task, error) {
	c, ok := e.Tracker.(tracker.Creator)
	if !ok {
		return tracker.Task{}, fmt.Errorf("el tracker %s no permite crear tareas desde bflow; créala en el tracker y usa su ID", e.Tracker.Name())
	}
	return c.Create(ctx, title, desc)
}

// afterTransition hace el trabajo de archivos que depende de la fase nueva.
func (e *Engine) afterTransition(rec store.Record) []string {
	if rec.Flow.Phase == flow.Spec {
		if err := e.ensureSpec(rec); err != nil {
			return []string{"no se pudo crear el spec: " + err.Error()}
		}
	}
	return nil
}

func (e *Engine) appendFile(id, rel, header, line string) error {
	b, err := e.Store.ReadFile(id, rel)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(b) == 0 {
		b = []byte(header)
	}
	return e.Store.WriteFile(id, rel, append(b, []byte(line)...))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// SortedIDs es un auxiliar para mensajes.
func SortedIDs(views []View) []string {
	var ids []string
	for _, v := range views {
		ids = append(ids, v.ID)
	}
	sort.Strings(ids)
	return ids
}

// specAbs es la ruta absoluta del spec.
func (e *Engine) specAbs(s flow.State) string {
	return filepath.Join(e.Cfg.Root, filepath.FromSlash(flow.SpecPath(s.ID, s.Slug)))
}
