package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
)

// ImportedTask es una tarea importada del harness anterior.
type ImportedTask struct {
	From  string     `json:"from"` // id en el harness anterior
	ID    string     `json:"id"`   // id en bflow
	Title string     `json:"title,omitempty"`
	Phase flow.Phase `json:"phase"`
}

// ImportPlan es el resultado (o el plan, en dry-run) de una importación.
type ImportPlan struct {
	Tasks   []ImportedTask `json:"tasks"`
	Skipped []string       `json:"skipped,omitempty"`
}

// oldStatus traduce los estados del harness anterior a fase y gate.
var oldStatus = map[string]struct {
	phase flow.Phase
	gate  flow.Gate
}{
	"pending":      {flow.Backlog, ""},
	"discovery":    {flow.Discovery, flow.GateDiscovery},
	"readyForSpec": {flow.Spec, ""},
	"specReady":    {flow.Spec, flow.GateSpec},
	"inProgress":   {flow.Implementing, ""},
	"implemented":  {flow.Quality, ""},
	"audited":      {flow.Documenting, ""},
	"documented":   {flow.Walkthrough, flow.GateWalkthrough},
	"blocked":      {flow.Blocked, ""},
}

type oldFeature struct {
	ID          json.Number `json:"id"`
	Name        string      `json:"name"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Acceptance  []string    `json:"acceptance"`
	Status      string      `json:"status"`
}

const importIndex = "imported.json"

func (e *Engine) importIndex() (map[string]string, string) {
	path := filepath.Join(e.Store.Dir(), importIndex)
	idx := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &idx)
	}
	return idx, path
}

// Import lee harness/feature-list.json y crea en el tracker (que debe permitir
// crear tareas) las features no cerradas, con su registro de flujo. Es
// idempotente: recuerda lo importado en .bflow/imported.json.
func (e *Engine) Import(ctx context.Context, harnessRoot string, dryRun bool) (ImportPlan, error) {
	var plan ImportPlan
	creator, ok := e.Tracker.(tracker.Creator)
	if !ok {
		return plan, fmt.Errorf("el tracker %s no permite crear tareas: importa solo el estado con --state-only", e.Tracker.Name())
	}
	b, err := os.ReadFile(filepath.Join(harnessRoot, "harness", "feature-list.json"))
	if err != nil {
		return plan, err
	}
	var fl struct {
		Features []oldFeature `json:"features"`
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&fl); err != nil {
		return plan, fmt.Errorf("feature-list.json: %w", err)
	}
	idx, idxPath := e.importIndex()
	for _, f := range fl.Features {
		key := "feature-list:" + f.ID.String()
		m, known := oldStatus[f.Status]
		if !known {
			plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s (%s): estado %q", f.ID, f.Name, f.Status))
			continue
		}
		if _, done := idx[key]; done {
			continue
		}
		title := f.Title
		if title == "" {
			title = f.Name
		}
		it := ImportedTask{From: f.ID.String(), Title: title, Phase: m.phase}
		if dryRun {
			plan.Tasks = append(plan.Tasks, it)
			continue
		}
		task, err := creator.Create(ctx, title, oldDescription(f))
		if err != nil {
			return plan, err
		}
		it.ID = task.ID
		if m.phase != flow.Backlog {
			if err := e.importRecord(task.ID, f.Name, title, task.URL, m.phase, m.gate, time.Time{}, key); err != nil {
				return plan, err
			}
			if err := e.Tracker.Transition(ctx, task.ID, m.phase, tracker.Patch{}); err != nil {
				return plan, err
			}
		}
		idx[key] = task.ID
		plan.Tasks = append(plan.Tasks, it)
		if err := writeJSON(idxPath, idx); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

func oldDescription(f oldFeature) string {
	d := strings.TrimSpace(f.Description)
	if len(f.Acceptance) > 0 {
		d += "\n\n**Aceptación**\n"
		for _, a := range f.Acceptance {
			d += "- " + a + "\n"
		}
	}
	return strings.TrimSpace(d)
}

// ImportStateTimes lee harness/progress/.state-times.json y crea el registro
// de flujo de las tareas en curso, que ya existen en el tracker. No escribe
// en el tracker.
func (e *Engine) ImportStateTimes(ctx context.Context, harnessRoot string, dryRun bool) (ImportPlan, error) {
	var plan ImportPlan
	b, err := os.ReadFile(filepath.Join(harnessRoot, "harness", "progress", ".state-times.json"))
	if err != nil {
		return plan, err
	}
	var st map[string]struct {
		State string `json:"state"`
		Since string `json:"since"`
		Slug  string `json:"slug"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return plan, fmt.Errorf(".state-times.json: %w", err)
	}
	keys := make([]string, 0, len(st))
	for k := range st {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, id := range keys {
		s := st[id]
		m, known := oldStatus[s.State]
		if !known || m.phase == flow.Backlog {
			plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s: estado %q", id, s.State))
			continue
		}
		if _, err := e.Store.Load(id); err == nil {
			continue
		}
		since, _ := time.Parse("2006-01-02T15:04:05-0700", s.Since)
		plan.Tasks = append(plan.Tasks, ImportedTask{From: id, ID: id, Phase: m.phase})
		if dryRun {
			continue
		}
		if err := e.importRecord(id, s.Slug, "", "", m.phase, m.gate, since, "state-times:"+id); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

func (e *Engine) importRecord(id, slug, title, url string, phase flow.Phase, gate flow.Gate, since time.Time, source string) error {
	_, err := e.Store.Update(id, func(r *store.Record, exists bool) error {
		if exists {
			return nil
		}
		s := flow.New(id, Slug(slug))
		s.Lane = flow.Full
		s.Phase = phase
		afterSpec := phase != flow.Discovery && phase != flow.Spec
		s.BranchCreated, s.Started = afterSpec, afterSpec
		if gate != "" {
			s.Gate = &flow.PendingGate{Name: gate}
		}
		if phase == flow.Blocked {
			s.Block = &flow.BlockInfo{Reason: "importada como bloqueada del harness anterior", From: flow.Implementing}
		}
		r.Flow, r.Title, r.URL = s, title, url
		r.Branch = e.BranchName(s)
		r.Adapter = map[string]string{"imported_from": source}
		return nil
	})
	if err != nil {
		return err
	}
	if !since.IsZero() {
		_, err = e.Store.Update(id, func(r *store.Record, _ bool) error { r.Since = since; return nil })
	}
	if err == nil {
		err = e.Store.Append(store.Entry{TS: e.now(), ID: id, Event: "import", To: phase, By: e.User, Data: map[string]any{"source": source}})
	}
	return err
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return store.WriteAtomic(path, append(b, '\n'))
}
