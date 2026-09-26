package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/vcs"
)

// PR publica y abre o actualiza el PR de una tarea en in_review (reintento
// tras un fallo o refresco de la descripción).
func (e *Engine) PR(ctx context.Context, id string) (Outcome, error) {
	out := Outcome{ID: id}
	rec, err := e.Store.Update(id, func(rec *store.Record, exists bool) error {
		if !exists {
			return &flow.Rejection{Code: "not_started", Reason: id + " no ha empezado"}
		}
		if rec.Flow.Phase != flow.InReview {
			return &flow.Rejection{Code: "pr_not_ready", Reason: fmt.Sprintf("el PR se abre al aprobar el walkthrough; %s está en %s", id, rec.Flow.Phase)}
		}
		w, err := e.openPR(ctx, rec)
		out.Warnings = w
		return err
	})
	if err != nil {
		return out, err
	}
	e.fill(&out, rec)
	return out, nil
}

// PanelItem es un compromiso que requiere atención humana.
type PanelItem struct {
	ID      string     `json:"id"`
	Title   string     `json:"title,omitempty"`
	Phase   flow.Phase `json:"phase"`
	Gate    string     `json:"gate,omitempty"`
	Hours   int        `json:"waiting_hours,omitempty"`
	SLA     bool       `json:"sla,omitempty"` // esperando más de sla_hours
	Due     string     `json:"due,omitempty"`
	Overdue bool       `json:"overdue,omitempty"`
	score   int
}

// PanelReport es el resultado de bflow panel.
type PanelReport struct {
	Items    []PanelItem `json:"items"`
	More     int         `json:"more,omitempty"`
	Closed   []string    `json:"closed,omitempty"`   // in_review → done por merge
	Reminded []string    `json:"reminded,omitempty"` // comentarios de SLA/vencimiento publicados
	Warnings []string    `json:"warnings,omitempty"`
}

const panelMax = 9

// Panel revisa las tareas en curso: sincroniza pendientes, cierra las que se
// mergearon y arma la lista de compromisos. Con sla, además comenta en el
// tracker los gates que llevan más de sla_hours esperando y las vencidas
// (una sola vez por aviso).
func (e *Engine) Panel(ctx context.Context, sla bool) (PanelReport, error) {
	var rep PanelReport
	recs, err := e.Store.List()
	if err != nil {
		return rep, err
	}
	due := map[string]*time.Time{}
	if tasks, err := e.Tracker.List(ctx, tracker.Filter{OpenOnly: true}); err == nil {
		for _, t := range tasks {
			due[t.ID] = t.Due
		}
	} else {
		rep.Warnings = append(rep.Warnings, "tracker no disponible: "+err.Error())
	}
	now := e.now()
	slaHours := e.Cfg.Flow.SLAHours
	for _, rec := range recs {
		if rec.Flow.Phase == flow.Done {
			continue
		}
		id := rec.Flow.ID
		if len(rec.Pending) > 0 {
			if _, err := e.Sync(ctx, id); err != nil {
				rep.Warnings = append(rep.Warnings, id+": "+err.Error())
			}
		}
		if rec.Flow.Phase == flow.InReview {
			closed, warn := e.checkMerged(ctx, rec)
			if warn != "" {
				rep.Warnings = append(rep.Warnings, warn)
			}
			if closed {
				rep.Closed = append(rep.Closed, id)
				continue
			}
		}
		it := PanelItem{ID: id, Title: rec.Title, Phase: rec.Flow.Phase}
		waitingSince := time.Time{}
		switch {
		case rec.Flow.Phase == flow.Blocked:
			it.Gate, waitingSince = "blocked", rec.Since
		case rec.Flow.Gate != nil:
			it.Gate, waitingSince = string(rec.Flow.Gate.Name), rec.GateSince
		}
		if !waitingSince.IsZero() {
			it.Hours = int(now.Sub(waitingSince).Hours())
			it.SLA = it.Hours >= slaHours
			it.score++
			if it.SLA {
				it.score += 2
			}
		}
		if d := due[id]; d != nil {
			it.Due = d.Format("2006-01-02")
			days := d.Sub(now).Hours() / 24
			if days < 0 {
				it.Overdue = true
				it.score += 4
			} else if days <= 3 {
				it.score++
			}
		}
		if it.score == 0 {
			continue
		}
		if sla {
			rep.Reminded = append(rep.Reminded, e.remind(ctx, it, waitingSince, &rep)...)
		}
		rep.Items = append(rep.Items, it)
	}
	sort.SliceStable(rep.Items, func(i, j int) bool { return rep.Items[i].score > rep.Items[j].score })
	if len(rep.Items) > panelMax {
		rep.More = len(rep.Items) - panelMax
		rep.Items = rep.Items[:panelMax]
	}
	return rep, nil
}

// checkMerged consulta el PR de una tarea en in_review y la cierra si se mergeó.
func (e *Engine) checkMerged(ctx context.Context, rec store.Record) (bool, string) {
	id := rec.Flow.ID
	if e.Host == nil || rec.PR == nil {
		return false, ""
	}
	n := rec.PR.Number
	if n == 0 { // PR creado a mano desde la URL de compare: buscarlo por rama
		pr, err := e.Host.FindPR(ctx, rec.Branch)
		if err != nil {
			return false, ""
		}
		n = pr.Number
		_, _ = e.Store.Update(id, func(r *store.Record, _ bool) error {
			r.PR = &store.PR{Number: pr.Number, URL: pr.URL}
			return nil
		})
	}
	pr, err := e.Host.PRStatus(ctx, n)
	if errors.Is(err, vcs.ErrNoCredentials) {
		return false, ""
	}
	if err != nil {
		return false, fmt.Sprintf("%s: no se pudo consultar el PR #%d: %v", id, n, err)
	}
	switch {
	case pr.Merged:
		if _, err := e.Merged(ctx, id); err != nil {
			return false, fmt.Sprintf("%s: PR mergeado pero no se pudo cerrar: %v", id, err)
		}
		return true, ""
	case pr.State == "closed":
		return false, fmt.Sprintf("%s: el PR #%d se cerró sin merge", id, n)
	}
	return false, ""
}

// remind publica en el tracker los avisos de SLA y vencimiento, una sola vez.
func (e *Engine) remind(ctx context.Context, it PanelItem, since time.Time, rep *PanelReport) []string {
	var want []string
	if it.SLA {
		want = append(want, fmt.Sprintf("⏳ Esperando %s desde %s", it.Gate, since.Format("2006-01-02 15:04")))
	}
	if it.Overdue {
		want = append(want, fmt.Sprintf("⌛ Vencida (due %s)", it.Due))
	}
	if len(want) == 0 {
		return nil
	}
	existing, err := e.Tracker.Comments(ctx, it.ID)
	if err != nil {
		rep.Warnings = append(rep.Warnings, it.ID+": "+err.Error())
		return nil
	}
	var posted []string
	for _, w := range want {
		dup := false
		for _, c := range existing {
			if strings.Contains(c.Body, w) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if err := e.Tracker.Comment(ctx, it.ID, w); err != nil {
			rep.Warnings = append(rep.Warnings, it.ID+": "+err.Error())
			continue
		}
		posted = append(posted, it.ID+": "+w)
	}
	return posted
}
