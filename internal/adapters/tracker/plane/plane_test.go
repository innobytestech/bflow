package plane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/testutil"
	"innobytes.tech/bflow/internal/tracker"
	"innobytes.tech/bflow/internal/tracker/trackertest"
)

const (
	ws  = "acme-dev"
	pid = "00000000-0000-4000-8000-000000000001"
)

// beStates es el set de estados que hoy tiene el proyecto API (lib.mjs:82-93).
var beStates = []map[string]any{
	{"id": "st-backlog", "name": "Backlog", "group": "backlog"},
	{"id": "st-todo", "name": "Todo", "group": "unstarted"},
	{"id": "st-discovery", "name": "Discovery", "group": "unstarted"},
	{"id": "st-specp", "name": "Spec pendiente", "group": "unstarted"},
	{"id": "st-speca", "name": "Spec por aprobar", "group": "unstarted"},
	{"id": "st-progress", "name": "In Progress", "group": "started"},
	{"id": "st-impl", "name": "Implementado", "group": "started"},
	{"id": "st-audit", "name": "Auditado", "group": "started"},
	{"id": "st-pr", "name": "Por PR", "group": "started"},
	{"id": "st-blocked", "name": "Bloqueado", "group": "started"},
	{"id": "st-done", "name": "Done", "group": "completed"},
	{"id": "st-cancel", "name": "Cancelled", "group": "cancelled"},
}

// fakePlane imita la API v1 de Plane en lo que bflow usa.
type fakePlane struct {
	mu           sync.Mutex
	t            *testing.T
	states       []map[string]any
	items        []map[string]any
	comments     map[string][]map[string]any
	calls        []string
	noWSEndpoint bool // el endpoint por identificador a nivel workspace no existe (versiones viejas)
	unauthorized bool
	throttleOnce bool
	postedHTML   []string
	failCreate   bool
}

func newFake(t *testing.T) *fakePlane {
	return &fakePlane{t: t, states: append([]map[string]any(nil), beStates...), comments: map[string][]map[string]any{}}
}

func (f *fakePlane) add(seq int, name string, state string) map[string]any {
	it := map[string]any{"id": fmt.Sprintf("wi-%d", seq), "sequence_id": seq, "name": name,
		"description_stripped": "descripción de " + name, "state": state, "start_date": nil, "target_date": nil,
		"updated_at": "2026-09-20T10:00:00Z", "project": pid}
	f.items = append(f.items, it)
	return it
}

func (f *fakePlane) page(w http.ResponseWriter, r *http.Request, rows []map[string]any) {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per == 0 {
		per = 100
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	end := min(start+per, len(rows))
	resp := map[string]any{"results": rows[start:end], "next_page_results": end < len(rows), "next_cursor": strconv.Itoa(end)}
	json.NewEncoder(w).Encode(resp)
}

func (f *fakePlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if f.unauthorized || r.Header.Get("X-API-Key") != "key" {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"detail":"Given API token is not valid"}`)
		return
	}
	if f.throttleOnce {
		f.throttleOnce = false
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"detail":"Request was throttled."}`)
		return
	}
	base := "/api/v1/workspaces/" + ws
	proj := base + "/projects/" + pid
	p := r.URL.Path
	switch {
	case r.Method == "GET" && p == base+"/projects/":
		f.page(w, r, []map[string]any{
			{"id": "otro-uuid", "identifier": "WEB", "name": "acme-web"},
			{"id": pid, "identifier": "API", "name": "acme-api"},
		})
	case r.Method == "GET" && p == proj+"/states/":
		f.page(w, r, f.states)
	case r.Method == "POST" && p == proj+"/states/":
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		in["id"] = "st-new-" + strconv.Itoa(len(f.states))
		f.states = append(f.states, in)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(in)
	case r.Method == "GET" && p == proj+"/work-items/":
		f.page(w, r, f.items)
	case r.Method == "POST" && p == proj+"/work-items/":
		if f.failCreate {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"name is required"}`)
			return
		}
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		it := f.add(len(f.items)+200, in["name"].(string), "st-backlog")
		it["description_html"] = in["description_html"]
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(it)
	case r.Method == "GET" && strings.HasPrefix(p, base+"/work-items/"):
		if f.noWSEndpoint {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"detail":"Not found."}`)
			return
		}
		key := strings.Trim(strings.TrimPrefix(p, base+"/work-items/"), "/")
		for _, it := range f.items {
			if "API-"+strconv.Itoa(it["sequence_id"].(int)) == key {
				out := map[string]any{}
				for k, v := range it {
					out[k] = v
				}
				for _, s := range f.states { // expand=state
					if s["id"] == it["state"] {
						out["state"] = s
					}
				}
				json.NewEncoder(w).Encode(out)
				return
			}
		}
		w.WriteHeader(404)
		fmt.Fprint(w, `{"detail":"Not found."}`)
	case strings.HasPrefix(p, proj+"/work-items/"):
		rest := strings.Split(strings.Trim(strings.TrimPrefix(p, proj+"/work-items/"), "/"), "/")
		var it map[string]any
		for _, x := range f.items {
			if x["id"] == rest[0] {
				it = x
			}
		}
		if it == nil {
			w.WriteHeader(404)
			return
		}
		switch {
		case len(rest) == 1 && r.Method == "PATCH":
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			for k, v := range in {
				it[k] = v
			}
			json.NewEncoder(w).Encode(it)
		case len(rest) == 2 && rest[1] == "comments" && r.Method == "POST":
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			f.postedHTML = append(f.postedHTML, in["comment_html"].(string))
			c := map[string]any{"id": "c" + strconv.Itoa(len(f.comments[rest[0]])), "comment_html": in["comment_html"], "created_at": "2026-09-25T10:00:00Z"}
			f.comments[rest[0]] = append(f.comments[rest[0]], c)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(c)
		case len(rest) == 2 && rest[1] == "comments":
			f.page(w, r, f.comments[rest[0]])
		default:
			w.WriteHeader(404)
		}
	default:
		f.t.Errorf("endpoint inesperado: %s %s", r.Method, r.URL)
		w.WriteHeader(404)
	}
}

func newClient(t *testing.T, f *fakePlane) *Client {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := New(Options{URL: srv.URL, Workspace: ws, Project: "API", Token: "key",
		CachePath: filepath.Join(testutil.TempDir(t), "plane.json")})
	c.sleep = func(time.Duration) {}
	fakesMu.Lock()
	fakes[c] = f
	fakesMu.Unlock()
	return c
}

var (
	fakesMu sync.Mutex
	fakes   = map[*Client]*fakePlane{}
)

func TestConformance(t *testing.T) {
	trackertest.Run(t, trackertest.Harness{
		New: func(t *testing.T) tracker.Tracker { return newClient(t, newFake(t)) },
		Seed: func(t *testing.T, tr tracker.Tracker) (string, string) {
			f := fakes[tr.(*Client)]
			f.add(120, "Errores de validación por campo", "st-backlog")
			return "API-120", "Errores de validación por campo"
		},
		Missing: "API-999",
	})
}

func TestCreate(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f)
	task, err := c.Create(context.Background(), "Idea nueva", "Primera línea\n\nSegunda")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "API-200" || task.Title != "Idea nueva" || task.State != "Backlog" || task.Phase != flow.Backlog {
		t.Errorf("tarea creada = %+v", task)
	}
	html, _ := f.items[0]["description_html"].(string)
	if !strings.Contains(html, "<p>Primera línea</p>") {
		t.Errorf("la descripción debe ir como HTML, got %q", html)
	}
	got, err := c.Get(context.Background(), "API-200")
	if err != nil || got.Title != "Idea nueva" {
		t.Errorf("la tarea creada se puede leer: %+v, %v", got, err)
	}
}

func TestCreateError(t *testing.T) {
	f := newFake(t)
	f.failCreate = true
	c := newClient(t, f)
	task, err := c.Create(context.Background(), "x", "")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("debe devolver el error de la API, got %v", err)
	}
	if task.ID != "" || len(f.items) != 0 {
		t.Errorf("sin tarea parcial: %+v, items %d", task, len(f.items))
	}
}

func TestPhaseMappingWithExistingBEStates(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f)
	f.add(171, "Ledger PPD", "st-backlog")
	ctx := context.Background()
	cases := map[flow.Phase]string{
		flow.Discovery: "st-discovery", flow.Spec: "st-specp", flow.Contract: "st-progress", flow.Implementing: "st-progress",
		flow.Paused: "st-impl", flow.Quality: "st-impl", flow.Documenting: "st-audit", flow.Walkthrough: "st-pr",
		flow.InReview: "st-pr", flow.Done: "st-done", flow.Blocked: "st-blocked", flow.Backlog: "st-backlog",
	}
	for phase, want := range cases {
		if err := c.Transition(ctx, "API-171", phase, tracker.Patch{}); err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		if got := f.items[0]["state"]; got != want {
			t.Errorf("%s → estado %v, want %s", phase, got, want)
		}
	}
	f.items[0]["state"] = "st-progress"
	if task, _ := c.Get(ctx, "API-171"); task.Phase != flow.Implementing {
		t.Errorf("In Progress se lee como implementing (su nombre preferido), got %s", task.Phase)
	}
	// Lectura: "Spec por aprobar" también es spec; "Cancelled" no es ninguna fase.
	f.items[0]["state"] = "st-speca"
	if task, _ := c.Get(ctx, "API-171"); task.Phase != flow.Spec || task.State != "Spec por aprobar" {
		t.Errorf("lectura: %+v", task)
	}
	f.items[0]["state"] = "st-cancel"
	if task, _ := c.Get(ctx, "API-171"); task.Phase != "" || !task.Closed {
		t.Errorf("cancelada: %+v", task)
	}
}

func TestMarkdownBecomesHTML(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f)
	f.add(5, "x", "st-backlog")
	if err := c.Comment(context.Background(), "API-5", "**Rechazado en spec:** falta `R3`"); err != nil {
		t.Fatal(err)
	}
	if got := f.postedHTML[0]; got != "<p><strong>Rechazado en spec:</strong> falta <code>R3</code></p>" {
		t.Errorf("html: %s", got)
	}
	cs, _ := c.Comments(context.Background(), "API-5")
	if cs[0].Body != "Rechazado en spec: falta R3" {
		t.Errorf("lectura como texto: %q", cs[0].Body)
	}
}

func TestProjectResolvedOnceAndCached(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f)
	f.add(1, "a", "st-backlog")
	ctx := context.Background()
	c.Get(ctx, "API-1")
	c.Transition(ctx, "API-1", flow.Spec, tracker.Patch{})
	c2 := New(Options{URL: c.URL, Workspace: ws, Project: "API", Token: "key", CachePath: c.cachePath})
	c2.HTTP = c.HTTP
	c2.Transition(ctx, "API-1", flow.Implementing, tracker.Patch{})
	n := 0
	for _, call := range f.calls {
		if strings.HasSuffix(call, "/projects/") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("el proyecto se resolvió %d veces; la caché debe evitarlo", n)
	}
}

func TestFallbackWithoutWorkspaceEndpointAndPagination(t *testing.T) {
	f := newFake(t)
	f.noWSEndpoint = true
	c := newClient(t, f)
	for i := 1; i <= 150; i++ {
		f.add(i, fmt.Sprintf("tarea %d", i), "st-backlog")
	}
	task, err := c.Get(context.Background(), "API-142")
	if err != nil || task.Title != "tarea 142" {
		t.Fatalf("fallback por sequence_id en la página 2: %+v %v", task, err)
	}
	all, err := c.List(context.Background(), tracker.Filter{})
	if err != nil || len(all) != 150 {
		t.Errorf("paginación: %d %v", len(all), err)
	}
}

func TestDatesAndURL(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f)
	it := f.add(7, "x", "st-backlog")
	it["target_date"] = "2026-10-01"
	task, err := c.Get(context.Background(), "API-7")
	if err != nil {
		t.Fatal(err)
	}
	if task.Due == nil || task.Due.Format("2006-01-02") != "2026-10-01" || task.Start != nil {
		t.Errorf("fechas: %+v", task)
	}
	if !strings.HasSuffix(task.URL, "/"+ws+"/browse/API-7/") {
		t.Errorf("url: %s", task.URL)
	}
	day := time.Date(2026, 9, 25, 18, 0, 0, 0, time.Local)
	if err := c.Transition(context.Background(), "API-7", flow.Implementing, tracker.Patch{StampStart: day}); err != nil {
		t.Fatal(err)
	}
	if it["start_date"] != "2026-09-25" {
		t.Errorf("start_date: %v", it["start_date"])
	}
}

func TestErrors(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f)
	ctx := context.Background()
	if _, err := c.Get(ctx, "WEB-3"); err == nil || !strings.Contains(err.Error(), "API") {
		t.Errorf("ID de otro proyecto: %v", err)
	}
	if _, err := c.Get(ctx, "no-es-id"); err == nil {
		t.Error("ID mal formado")
	}
	f.unauthorized = true
	_, err := c.Get(ctx, "API-1")
	if err == nil || !strings.Contains(err.Error(), "bflow connect plane") {
		t.Errorf("401: %v", err)
	}
	if errors.Is(err, tracker.ErrNotFound) {
		t.Error("401 no es ErrNotFound")
	}
	noKey := New(Options{URL: c.URL, Workspace: ws, Project: "API", CachePath: c.cachePath})
	if _, err := noKey.Get(ctx, "API-1"); err == nil || !strings.Contains(err.Error(), "bflow connect plane") {
		t.Errorf("sin token: %v", err)
	}
}

func TestThrottleRetries(t *testing.T) {
	f := newFake(t)
	c := newClient(t, f)
	f.add(1, "a", "st-backlog")
	f.throttleOnce = true
	if _, err := c.Get(context.Background(), "API-1"); err != nil {
		t.Errorf("un 429 con Retry-After se reintenta: %v", err)
	}
}

func TestEnsureStatesOnEmptyProject(t *testing.T) {
	f := newFake(t)
	f.states = nil
	c := newClient(t, f)
	plan, err := c.EnsureStates(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 12 || len(f.states) != 0 {
		t.Fatalf("dry-run: %v (creados %d)", plan, len(f.states))
	}
	if _, err := c.EnsureStates(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	groups := map[string]string{}
	for _, s := range f.states {
		groups[s["name"].(string)] = s["group"].(string)
	}
	if groups["Contrato"] != "started" || groups["Done"] != "completed" || groups["Backlog"] != "backlog" || groups["Discovery"] != "unstarted" {
		t.Errorf("estados creados: %v", groups)
	}
	again, _ := c.EnsureStates(context.Background(), false)
	if len(again) != 0 {
		t.Errorf("idempotente: %v", again)
	}
	// Con los estados del BE no falta ninguno.
	f2 := newFake(t)
	if plan, _ := newClient(t, f2).EnsureStates(context.Background(), true); len(plan) != 0 {
		t.Errorf("el BE ya tiene todo: %v", plan)
	}
}

func TestProjects(t *testing.T) {
	ps, err := newClient(t, newFake(t)).Projects(context.Background())
	if err != nil || len(ps) != 2 || ps[1].ID != "API" || ps[1].Name != "acme-api" {
		t.Errorf("proyectos: %+v %v", ps, err)
	}
}

// realStates es la lista real de estados de API (bflow tracker states, 2026-09-26):
// usa los nombres literales del harness anterior y no tiene estado de bloqueo.
var realStates = []map[string]any{
	{"id": "r-pending", "name": "pending", "group": "backlog"},
	{"id": "r-discovery", "name": "discovery", "group": "unstarted"},
	{"id": "r-rfs", "name": "readyForSpec", "group": "unstarted"},
	{"id": "r-sr", "name": "specReady", "group": "unstarted"},
	{"id": "r-ip", "name": "inProgress", "group": "started"},
	{"id": "r-impl", "name": "implemented", "group": "started"},
	{"id": "r-rev", "name": "reviewed", "group": "started"},
	{"id": "r-aud", "name": "audited", "group": "started"},
	{"id": "r-doc", "name": "documented", "group": "started"},
	{"id": "r-done", "name": "done", "group": "completed"},
	{"id": "r-cancel", "name": "cancelled", "group": "cancelled"},
}

func TestPhaseMappingWithRealHarnessNames(t *testing.T) {
	f := newFake(t)
	f.states = realStates
	c := newClient(t, f)
	f.add(171, "Libro ppd_payment", "r-pending")
	ctx := context.Background()
	write := map[flow.Phase]string{
		flow.Backlog: "r-pending", flow.Discovery: "r-discovery", flow.Spec: "r-rfs", flow.Contract: "r-ip",
		flow.Implementing: "r-ip", flow.Paused: "r-impl", flow.Quality: "r-impl", flow.Documenting: "r-aud",
		flow.Walkthrough: "r-doc", flow.InReview: "r-doc", flow.Done: "r-done",
	}
	for phase, want := range write {
		if err := c.Transition(ctx, "API-171", phase, tracker.Patch{}); err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		if got := f.items[0]["state"]; got != want {
			t.Errorf("%s → %v, want %s", phase, got, want)
		}
	}
	read := map[string]flow.Phase{"r-pending": flow.Backlog, "r-sr": flow.Spec, "r-ip": flow.Implementing,
		"r-rev": flow.Quality, "r-doc": flow.Walkthrough, "r-cancel": ""}
	for st, want := range read {
		f.items[0]["state"] = st
		if task, _ := c.Get(ctx, "API-171"); task.Phase != want {
			t.Errorf("leer %s → %q, want %q", st, task.Phase, want)
		}
	}
	plan, _ := c.EnsureStates(ctx, true)
	if len(plan) != 1 || !strings.Contains(plan[0], "blocked") {
		t.Errorf("con los nombres reales solo falta un estado para blocked: %v", plan)
	}
}
