// Package plane es el adaptador del tracker Plane (API v1). Traduce fases a
// estados del proyecto, Markdown a HTML y el identificador legible del
// proyecto (API) a su UUID, que guarda en caché.
package plane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/markdown"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
)

// StateDef es el estado de Plane que representa una fase.
type StateDef struct {
	Names []string // por preferencia: se escribe en el primero que exista
	Group string   // grupo de Plane al crearlo
	Color string
}

// DefaultStates son compatibles con los estados que ya usa acme (lib.mjs):
// tanto los nombres en español como los literales del harness (inProgress,
// specReady…), que es lo que tiene hoy API. Ejemplo: // si un proyecto tiene "Implementado" pero no "En revisión", quality escribe en
// "Implementado". tracker setup crea el primer nombre si no existe ninguno.
var DefaultStates = map[flow.Phase]StateDef{
	flow.Backlog:      {[]string{"Backlog", "Todo", "pending"}, "backlog", "#A3A3A3"},
	flow.Discovery:    {[]string{"Discovery"}, "unstarted", "#60A5FA"},
	flow.Spec:         {[]string{"Spec pendiente", "Spec por aprobar", "Spec", "readyForSpec", "specReady"}, "unstarted", "#818CF8"},
	flow.Contract:     {[]string{"Contrato", "In Progress", "En progreso", "inProgress"}, "started", "#6366F1"},
	flow.Implementing: {[]string{"In Progress", "En progreso", "inProgress"}, "started", "#3B82F6"},
	flow.Paused:       {[]string{"En pausa", "Implementado", "implemented"}, "started", "#8B5CF6"},
	flow.Quality:      {[]string{"En revisión", "Implementado", "implemented", "reviewed"}, "started", "#A855F7"},
	flow.Documenting:  {[]string{"Documentando", "Auditado", "audited"}, "started", "#14B8A6"},
	flow.Walkthrough:  {[]string{"Walkthrough", "Por PR", "documented"}, "started", "#F97316"},
	flow.InReview:     {[]string{"PR abierto", "Por PR", "documented"}, "started", "#FB923C"},
	flow.Done:         {[]string{"Done", "Hecho"}, "completed", "#22C55E"},
	flow.Blocked:      {[]string{"Bloqueado", "blocked"}, "started", "#EF4444"},
}

var phaseOrder = append(append([]flow.Phase{flow.Backlog}, flow.Order...), flow.Blocked)

// Options configura el cliente.
type Options struct {
	URL, Workspace, Project, Token string
	States                         map[string][]string // tracker.states de bflow.yaml
	CachePath                      string              // .bflow/cache/plane.json
}

// Client implementa tracker.Tracker sobre Plane.
type Client struct {
	URL, Workspace, Project string
	Token                   string
	HTTP                    *http.Client
	States                  map[flow.Phase]StateDef
	cachePath               string
	sleep                   func(time.Duration)

	mu     sync.Mutex
	cache  cacheFile
	loaded bool
	states []planeState // de esta ejecución
}

type cacheFile struct {
	Project string            `json:"project"`
	ID      string            `json:"project_id"`
	Items   map[string]string `json:"items,omitempty"` // API-120 → uuid
}

type planeState struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// New crea el cliente.
func New(o Options) *Client {
	c := &Client{URL: strings.TrimRight(o.URL, "/"), Workspace: o.Workspace, Project: strings.ToUpper(o.Project), Token: o.Token,
		HTTP: &http.Client{Timeout: 15 * time.Second}, States: map[flow.Phase]StateDef{}, cachePath: o.CachePath, sleep: time.Sleep}
	for p, d := range DefaultStates {
		c.States[p] = d
	}
	for p, names := range o.States {
		d := c.States[flow.Phase(p)]
		d.Names = names
		c.States[flow.Phase(p)] = d
	}
	return c
}

var (
	_ tracker.Tracker          = (*Client)(nil)
	_ tracker.StateProvisioner = (*Client)(nil)
	_ tracker.ProjectLister    = (*Client)(nil)
)

func (c *Client) Name() string { return "plane" }

// ---------- HTTP ----------

type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	if c.Token == "" {
		return fmt.Errorf("plane: no hay API key para %s (bflow connect plane, o BFLOW_PLANE_TOKEN)", c.URL)
	}
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.URL+"/api/v1/workspaces/"+c.Workspace+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("X-API-Key", c.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("plane: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode == 429 && attempt < 3 {
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			c.sleep(time.Duration(min(max(wait, 1), 30)) * time.Second)
			continue
		}
		if resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(raw))
			var d struct {
				Detail string `json:"detail"`
				Error  string `json:"error"`
			}
			if json.Unmarshal(raw, &d) == nil && (d.Detail != "" || d.Error != "") {
				msg = d.Detail + d.Error
			}
			if len(msg) > 200 {
				msg = msg[:200]
			}
			e := &apiError{status: resp.StatusCode, msg: fmt.Sprintf("plane %s %s → %d: %s", method, path, resp.StatusCode, msg)}
			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				e.msg += " (revisa la API key: bflow connect plane)"
			}
			return e
		}
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
}

// list recorre las páginas de un listado.
func list[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var out []T
	cursor := ""
	for i := 0; i < 50; i++ {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		p := path + sep + "per_page=100"
		if cursor != "" {
			p += "&cursor=" + cursor
		}
		var raw json.RawMessage
		if err := c.do(ctx, "GET", p, nil, &raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 && raw[0] == '[' {
			var rows []T
			err := json.Unmarshal(raw, &rows)
			return append(out, rows...), err
		}
		var page struct {
			Results []T    `json:"results"`
			Next    bool   `json:"next_page_results"`
			Cursor  string `json:"next_cursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Results...)
		if !page.Next {
			break
		}
		cursor = page.Cursor
	}
	return out, nil
}

// ---------- caché y resolución ----------

func (c *Client) loadCache() {
	if c.loaded {
		return
	}
	c.loaded = true
	if c.cachePath == "" {
		return
	}
	if b, err := os.ReadFile(c.cachePath); err == nil {
		var cf cacheFile
		if json.Unmarshal(b, &cf) == nil && cf.Project == c.Project {
			c.cache = cf
		}
	}
}

func (c *Client) saveCache() {
	if c.cachePath == "" {
		return
	}
	c.cache.Project = c.Project
	b, err := json.Marshal(c.cache)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(c.cachePath), 0o755)
	_ = store.WriteAtomic(c.cachePath, b)
}

// projectID resuelve API → UUID (una vez; luego sale de la caché).
func (c *Client) projectID(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadCache()
	if c.cache.ID != "" {
		return c.cache.ID, nil
	}
	ps, err := list[struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
	}](ctx, c, "/projects/")
	if err != nil {
		return "", err
	}
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.Identifier)
		if strings.EqualFold(p.Identifier, c.Project) {
			c.cache.ID = p.ID
			c.saveCache()
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("plane: el proyecto %s no existe en el workspace %s (hay: %s)", c.Project, c.Workspace, strings.Join(ids, ", "))
}

func (c *Client) proj(ctx context.Context) (string, error) {
	id, err := c.projectID(ctx)
	return "/projects/" + id, err
}

func (c *Client) loadStates(ctx context.Context) ([]planeState, error) {
	if c.states != nil {
		return c.states, nil
	}
	p, err := c.proj(ctx)
	if err != nil {
		return nil, err
	}
	sts, err := list[planeState](ctx, c, p+"/states/")
	if err == nil {
		c.states = sts
	}
	return sts, err
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// writeState es el estado donde se escribe una fase: su primer nombre existente.
func (c *Client) writeState(sts []planeState, p flow.Phase) (planeState, bool) {
	for _, n := range append([]string{string(p)}, c.States[p].Names...) {
		for _, s := range sts {
			if norm(s.Name) == norm(n) {
				return s, true
			}
		}
	}
	return planeState{}, false
}

// phaseOf traduce un estado de Plane a fase: gana la fase donde ese nombre
// aparece antes en su lista de preferencia (el nombre literal de la fase cuenta
// como el primero); en empate, el orden de las fases.
func (c *Client) phaseOf(name string) flow.Phase {
	best, bestIdx := flow.Phase(""), 1<<30
	for _, p := range phaseOrder {
		for i, n := range append([]string{string(p)}, c.States[p].Names...) {
			if norm(n) == norm(name) && i < bestIdx {
				best, bestIdx = p, i
				break
			}
		}
	}
	return best
}

// SameState indica si dos fases escriben en el mismo estado de Plane.
func (c *Client) SameState(a, b flow.Phase) bool {
	sts, err := c.loadStates(context.Background())
	if err != nil {
		return false
	}
	sa, oka := c.writeState(sts, a)
	sb, okb := c.writeState(sts, b)
	return oka && okb && sa.ID == sb.ID
}

// ---------- work items ----------

var keyRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)-(\d+)$`)

type workItem struct {
	ID          string          `json:"id"`
	Seq         int             `json:"sequence_id"`
	Name        string          `json:"name"`
	Description string          `json:"description_stripped"`
	State       json.RawMessage `json:"state"` // UUID o objeto con expand=state
	Start       *string         `json:"start_date"`
	Due         *string         `json:"target_date"`
	Updated     time.Time       `json:"updated_at"`
}

func (w workItem) stateID() string {
	var s string
	if json.Unmarshal(w.State, &s) == nil {
		return s
	}
	var o planeState
	_ = json.Unmarshal(w.State, &o)
	return o.ID
}

func (c *Client) parseKey(id string) (int, error) {
	m := keyRe.FindStringSubmatch(strings.TrimSpace(id))
	if m == nil {
		return 0, fmt.Errorf("ID inválido %q: se espera %s-123", id, c.Project)
	}
	if !strings.EqualFold(m[1], c.Project) {
		return 0, fmt.Errorf("%s pertenece al proyecto %s, pero este repo usa %s", id, strings.ToUpper(m[1]), c.Project)
	}
	n, _ := strconv.Atoi(m[2])
	return n, nil
}

func (c *Client) key(seq int) string { return fmt.Sprintf("%s-%d", c.Project, seq) }

func (c *Client) item(ctx context.Context, id string) (workItem, error) {
	seq, err := c.parseKey(id)
	if err != nil {
		return workItem{}, err
	}
	var w workItem
	err = c.do(ctx, "GET", "/work-items/"+c.key(seq)+"/?expand=state", nil, &w)
	var ae *apiError
	if err == nil {
		c.remember(w)
		return w, nil
	}
	if !errors.As(err, &ae) || ae.status != 404 {
		return w, err
	}
	// Versiones de Plane sin el endpoint por identificador: buscar por sequence_id.
	p, err := c.proj(ctx)
	if err != nil {
		return w, err
	}
	all, err := list[workItem](ctx, c, p+"/work-items/")
	if err != nil {
		return w, err
	}
	for _, it := range all {
		if it.Seq == seq {
			c.remember(it)
			return it, nil
		}
	}
	return w, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
}

func (c *Client) remember(w workItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadCache()
	if c.cache.Items == nil {
		c.cache.Items = map[string]string{}
	}
	k := c.key(w.Seq)
	if c.cache.Items[k] != w.ID {
		c.cache.Items[k] = w.ID
		c.saveCache()
	}
}

func (c *Client) uuid(ctx context.Context, id string) (string, error) {
	seq, err := c.parseKey(id)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.loadCache()
	u := c.cache.Items[c.key(seq)]
	c.mu.Unlock()
	if u != "" {
		return u, nil
	}
	w, err := c.item(ctx, id)
	return w.ID, err
}

func parseDate(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", (*s)[:min(10, len(*s))], time.Local)
	if err != nil {
		return nil
	}
	return &t
}

func (c *Client) task(sts []planeState, w workItem) tracker.Task {
	t := tracker.Task{ID: c.key(w.Seq), Title: w.Name, Description: w.Description, Start: parseDate(w.Start), Due: parseDate(w.Due),
		Updated: w.Updated, URL: fmt.Sprintf("%s/%s/browse/%s/", c.URL, c.Workspace, c.key(w.Seq))}
	sid := w.stateID()
	for _, s := range sts {
		if s.ID == sid {
			t.State = s.Name
			t.Phase = c.phaseOf(s.Name)
			t.Closed = s.Group == "completed" || s.Group == "cancelled"
		}
	}
	return t
}

func (c *Client) Get(ctx context.Context, id string) (tracker.Task, error) {
	w, err := c.item(ctx, id)
	if err != nil {
		return tracker.Task{}, err
	}
	sts, err := c.loadStates(ctx)
	if err != nil {
		return tracker.Task{}, err
	}
	return c.task(sts, w), nil
}

func (c *Client) List(ctx context.Context, f tracker.Filter) ([]tracker.Task, error) {
	p, err := c.proj(ctx)
	if err != nil {
		return nil, err
	}
	sts, err := c.loadStates(ctx)
	if err != nil {
		return nil, err
	}
	all, err := list[workItem](ctx, c, p+"/work-items/")
	if err != nil {
		return nil, err
	}
	var out []tracker.Task
	for _, w := range all {
		t := c.task(sts, w)
		if f.OpenOnly && t.Closed {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (c *Client) Transition(ctx context.Context, id string, to flow.Phase, p tracker.Patch) error {
	sts, err := c.loadStates(ctx)
	if err != nil {
		return err
	}
	st, ok := c.writeState(sts, to)
	if !ok {
		return fmt.Errorf("plane: el proyecto %s no tiene estado para la fase %s (%s); corre bflow tracker setup o ajusta tracker.states",
			c.Project, to, strings.Join(c.States[to].Names, " / "))
	}
	body := map[string]any{"state": st.ID}
	var u string
	if !p.StampStart.IsZero() {
		w, err := c.item(ctx, id) // hay que saber si ya tiene inicio: nunca se sobrescribe
		if err != nil {
			return err
		}
		u = w.ID
		if w.Start == nil || *w.Start == "" {
			body["start_date"] = p.StampStart.Format("2006-01-02")
		}
	} else if u, err = c.uuid(ctx, id); err != nil {
		return err
	}
	pp, err := c.proj(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, "PATCH", pp+"/work-items/"+u+"/", body, nil)
}

func (c *Client) Comment(ctx context.Context, id, md string) error {
	u, err := c.uuid(ctx, id)
	if err != nil {
		return err
	}
	p, err := c.proj(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, "POST", p+"/work-items/"+u+"/comments/", map[string]string{"comment_html": markdown.ToHTML(md)}, nil)
}

func (c *Client) Comments(ctx context.Context, id string) ([]tracker.Comment, error) {
	u, err := c.uuid(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := c.proj(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := list[struct {
		ID      string    `json:"id"`
		HTML    string    `json:"comment_html"`
		Created time.Time `json:"created_at"`
	}](ctx, c, p+"/work-items/"+u+"/comments/")
	if err != nil {
		return nil, err
	}
	out := make([]tracker.Comment, 0, len(rows))
	for _, r := range rows {
		out = append(out, tracker.Comment{ID: r.ID, Body: markdown.ToText(r.HTML), Created: r.Created})
	}
	return out, nil
}

// EnsureStates crea los estados que falten (el primer nombre de cada fase).
func (c *Client) EnsureStates(ctx context.Context, dryRun bool) ([]string, error) {
	sts, err := c.loadStates(ctx)
	if err != nil {
		return nil, err
	}
	p, err := c.proj(ctx)
	if err != nil {
		return nil, err
	}
	var created []string
	for _, ph := range phaseOrder {
		if _, ok := c.writeState(sts, ph); ok {
			continue
		}
		d := c.States[ph]
		name := d.Names[0]
		created = append(created, fmt.Sprintf("%s → %q [%s]", ph, name, d.Group))
		if dryRun {
			continue
		}
		var s planeState
		if err := c.do(ctx, "POST", p+"/states/", map[string]string{"name": name, "group": d.Group, "color": d.Color}, &s); err != nil {
			return created, err
		}
		sts = append(sts, s)
	}
	c.states = sts
	return created, nil
}

// Projects lista los proyectos del workspace (para bflow init y connect).
func (c *Client) Projects(ctx context.Context) ([]tracker.Project, error) {
	ps, err := list[struct {
		Identifier string `json:"identifier"`
		Name       string `json:"name"`
	}](ctx, c, "/projects/")
	if err != nil {
		return nil, err
	}
	out := make([]tracker.Project, 0, len(ps))
	for _, p := range ps {
		out = append(out, tracker.Project{ID: p.Identifier, Name: p.Name})
	}
	return out, nil
}

var _ tracker.StateLister = (*Client)(nil)

// StateMap lista los estados del proyecto con su traducción a fases.
func (c *Client) StateMap(ctx context.Context) ([]tracker.StateInfo, error) {
	sts, err := c.loadStates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]tracker.StateInfo, 0, len(sts))
	for _, s := range sts {
		info := tracker.StateInfo{Name: s.Name, Group: s.Group, Phase: c.phaseOf(s.Name)}
		for _, p := range phaseOrder {
			if w, ok := c.writeState(sts, p); ok && w.ID == s.ID {
				info.Writes = append(info.Writes, string(p))
			}
		}
		out = append(out, info)
	}
	return out, nil
}
