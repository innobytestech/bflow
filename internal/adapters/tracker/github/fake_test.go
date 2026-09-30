package github

// fake imita la API de GitHub en lo que bflow usa: REST (issues, comentarios,
// etiquetas) y GraphQL (Projects v2). Las consultas GraphQL se reconocen por
// una subcadena y leen estas variables (el adaptador debe usar estos nombres):
//
//	resolución y projects:  login, number
//	issue → items:          id (node_id del issue), start (nombre del campo de inicio)
//	items del project:      id (id del project), cursor, start
//	add:                    projectId, contentId
//	valor de campo:         projectId, itemId, fieldId y optionId o date
//	opciones de Status:     fieldId, options ([{id?, name, color, description}])
//
// Las respuestas traen los alias status{name,optionId} y start{date}.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"innobytes.tech/bflow/internal/tracker"
)

type fIssue struct {
	N             int
	Title, Body   string
	State, Reason string // "open" | "closed"
	PR            bool
	Repo          string // "" = acme/app
	Labels, Ghost []string
	Comments      []string
}

type fOpt struct{ ID, Name, Color, Desc string }

type fItem struct {
	ID     string
	Issue  *fIssue // nil: borrador
	Status string  // id de opción
	Dates  map[string]string
}

type fField struct{ ID, Name string }

type fProject struct {
	ID         string
	Number     int
	Title      string
	Options    []fOpt
	DateFields []fField
	Items      []*fItem
}

type fake struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	token  string
	issues []*fIssue
	labels map[string]fOpt // nombre → color/descripción
	nextID int

	pageSize    int
	orgs, users map[string][]*fProject
	idInput     bool // la introspección de ProjectV2SingleSelectFieldOptionInput incluye id

	calls   []string // "METHOD /ruta"
	gqlOps  []string
	patches int
	sleeps  []time.Duration

	status     int    // si no es 0, todo REST responde esto
	errBody    string // cuerpo de esos errores
	gqlStatus  int
	scopeErr   string // tipo de error GraphQL al resolver un project existente
	rateLeft   int
	rateStatus int
	rateHeader map[string]string
	notFound   bool  // toda mutación de valor responde NOT_FOUND
	lastFields []any // opciones que recibió updateProjectV2Field
	destroyed  bool  // updateProjectV2Field sin id soportado: rehizo los ids
	deleteSeen []string
}

func newFake(t *testing.T) *fake {
	f := &fake{t: t, token: "tok-secret-123", labels: map[string]fOpt{}, pageSize: 100, idInput: true,
		orgs: map[string][]*fProject{}, users: map[string][]*fProject{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// client arma un Client contra el fake. Token "-" es sin token.
func (f *fake) client(o Options) *Client {
	if o.API == "" {
		o.API = f.srv.URL
	}
	if o.Repo == "" {
		o.Repo = "acme/app"
	}
	if o.Prefix == "" {
		o.Prefix = "GH"
	}
	switch o.Token {
	case "":
		o.Token = f.token
	case "-":
		o.Token = ""
	}
	if o.CachePath == "" {
		o.CachePath = filepath.Join(f.t.TempDir(), "github.json")
	}
	c := New(o)
	c.sleep = func(d time.Duration) { f.mu.Lock(); f.sleeps = append(f.sleeps, d); f.mu.Unlock() }
	return c
}

func (f *fake) id(prefix string) string { f.nextID++; return fmt.Sprintf("%s_%d", prefix, f.nextID) }

func (f *fake) issue(n int, title string, labels ...string) *fIssue {
	is := &fIssue{N: n, Title: title, Body: "cuerpo de " + title, State: "open", Labels: labels}
	f.issues = append(f.issues, is)
	return is
}

func (f *fake) find(n int) *fIssue {
	for _, is := range f.issues {
		if is.N == n {
			return is
		}
	}
	return nil
}

func (f *fake) count(op string) int {
	n := 0
	for _, o := range f.gqlOps {
		if o == op {
			n++
		}
	}
	return n
}

func (f *fake) countCalls(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// project crea un project de una organización (user=false) o usuario, con el
// campo de fecha "Start date". Sin statuses, las opciones son el primer nombre
// de cada fase de la tabla por defecto.
func (f *fake) project(owner string, user bool, number int, statuses ...string) *fProject {
	if len(statuses) == 0 {
		seen := map[string]bool{}
		for _, p := range tracker.PhaseOrder {
			n := tracker.DefaultStates[p].Names[0]
			if !seen[n] {
				seen[n] = true
				statuses = append(statuses, n)
			}
		}
	}
	p := &fProject{ID: fmt.Sprintf("PVT_%d", number), Number: number, Title: fmt.Sprintf("Tablero %d", number),
		DateFields: []fField{{"F_start", "Start date"}}}
	for _, n := range statuses {
		p.Options = append(p.Options, fOpt{ID: f.id("OPT"), Name: n, Color: "GRAY", Desc: "existente"})
	}
	if user {
		f.users[owner] = append(f.users[owner], p)
	} else {
		f.orgs[owner] = append(f.orgs[owner], p)
	}
	return p
}

func (p *fProject) opt(name string) *fOpt {
	for i := range p.Options {
		if p.Options[i].Name == name {
			return &p.Options[i]
		}
	}
	return nil
}

func (p *fProject) add(f *fake, is *fIssue, status string) *fItem {
	it := &fItem{ID: f.id("PVTI"), Issue: is, Dates: map[string]string{}}
	if status != "" {
		it.Status = p.opt(status).ID
	}
	p.Items = append(p.Items, it)
	return it
}

func (p *fProject) item(is *fIssue) *fItem {
	for _, it := range p.Items {
		if it.Issue == is {
			return it
		}
	}
	return nil
}

func (f *fake) projectByID(id string) *fProject {
	for _, m := range []map[string][]*fProject{f.orgs, f.users} {
		for _, ps := range m {
			for _, p := range ps {
				if p.ID == id {
					return p
				}
			}
		}
	}
	return nil
}

// ---- HTTP

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
		return
	}
	if r.URL.Path == "/graphql" || r.URL.Path == "/api/graphql" {
		f.graphql(w, r)
		return
	}
	f.rest(w, r)
}

func (f *fake) rest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v3")
	f.calls = append(f.calls, r.Method+" "+strings.TrimPrefix(r.URL.EscapedPath(), "/api/v3"))
	if f.rateLeft > 0 {
		f.rateLeft--
		for k, v := range f.rateHeader {
			w.Header().Set(k, v)
		}
		w.WriteHeader(f.rateStatus)
		_, _ = io.WriteString(w, `{"message":"rate limit"}`)
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.errBody)
		return
	}
	if r.Header.Get("X-GitHub-Api-Version") == "" || !strings.Contains(r.Header.Get("Accept"), "github+json") {
		f.t.Errorf("faltan los headers de la API: %v", r.Header)
	}
	rest, ok := strings.CutPrefix(path, "/repos/acme/app/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	var in map[string]any
	if r.Method == "POST" || r.Method == "PATCH" {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}
	switch {
	case rest == "issues" && r.Method == "GET":
		var all []any
		for _, is := range f.issues {
			if r.URL.Query().Get("state") != "all" && is.State != "open" {
				continue
			}
			all = append(all, f.issueJSON(is))
		}
		f.page(w, r, all)
	case rest == "issues" && r.Method == "POST":
		is := f.issue(len(f.issues)+1, fmt.Sprint(in["title"]))
		is.Body, _ = in["body"].(string)
		for _, l := range asList(in["labels"]) {
			is.Labels = append(is.Labels, fmt.Sprint(l))
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.issueJSON(is))
	case rest == "labels" && r.Method == "GET":
		var all []any
		names := make([]string, 0, len(f.labels))
		for n := range f.labels {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			all = append(all, map[string]any{"name": n, "color": f.labels[n].Color, "description": f.labels[n].Desc})
		}
		f.page(w, r, all)
	case rest == "labels" && r.Method == "POST":
		name := fmt.Sprint(in["name"])
		if _, dup := f.labels[name]; dup {
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message":"Validation Failed","errors":[{"code":"already_exists"}]}`)
			return
		}
		f.labels[name] = fOpt{Name: name, Color: fmt.Sprint(in["color"]), Desc: fmt.Sprint(in["description"])}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{}`)
	case parts[0] == "issues" && len(parts) >= 2:
		n, _ := strconv.Atoi(parts[1])
		is := f.find(n)
		if is == nil {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		f.issueRoute(w, r, is, parts[2:], in)
	default:
		http.NotFound(w, r)
	}
}

func (f *fake) issueRoute(w http.ResponseWriter, r *http.Request, is *fIssue, sub []string, in map[string]any) {
	enc := json.NewEncoder(w)
	switch {
	case len(sub) == 0 && r.Method == "GET":
		_ = enc.Encode(f.issueJSON(is))
	case len(sub) == 0 && r.Method == "PATCH":
		f.patches++
		if s, ok := in["state"].(string); ok {
			is.State = s
		}
		if s, ok := in["state_reason"].(string); ok {
			is.Reason = s
		}
		_ = enc.Encode(f.issueJSON(is))
	case sub[0] == "comments" && r.Method == "GET":
		var all []any
		for i, c := range is.Comments {
			all = append(all, map[string]any{"id": i + 1, "body": c, "user": map[string]any{"login": "dev"}, "created_at": "2026-09-10T12:00:00Z"})
		}
		f.page(w, r, all)
	case sub[0] == "comments" && r.Method == "POST":
		is.Comments = append(is.Comments, fmt.Sprint(in["body"]))
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{}`)
	case sub[0] == "labels" && r.Method == "POST":
		for _, l := range asList(in["labels"]) {
			name := fmt.Sprint(l)
			if !contains(is.Labels, name) {
				is.Labels = append(is.Labels, name)
			}
		}
		_ = enc.Encode(f.issueJSON(is)["labels"])
	case sub[0] == "labels" && r.Method == "DELETE" && len(sub) == 2:
		name, _ := url.PathUnescape(sub[1])
		f.deleteSeen = append(f.deleteSeen, name)
		for i, l := range is.Labels {
			if l == name {
				is.Labels = append(is.Labels[:i:i], is.Labels[i+1:]...)
				io.WriteString(w, `[]`)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Label does not exist"}`)
	default:
		http.NotFound(w, r)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func asList(v any) []any { l, _ := v.([]any); return l }

func (f *fake) issueJSON(is *fIssue) map[string]any {
	var ls []any
	for _, l := range append(append([]string{}, is.Labels...), is.Ghost...) {
		ls = append(ls, map[string]any{"name": l})
	}
	m := map[string]any{"number": is.N, "title": is.Title, "body": is.Body, "state": is.State, "state_reason": is.Reason,
		"html_url": fmt.Sprintf("https://github.com/acme/app/issues/%d", is.N), "updated_at": "2026-09-10T12:00:00Z",
		"node_id": fmt.Sprintf("I_%d", is.N), "labels": ls}
	if is.PR {
		m["pull_request"] = map[string]any{"url": "x"}
	}
	return m
}

// page responde la página ?page=N con Link rel="next" si hay más.
func (f *fake) page(w http.ResponseWriter, r *http.Request, all []any) {
	pg, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pg < 1 {
		pg = 1
	}
	lo, hi := (pg-1)*f.pageSize, pg*f.pageSize
	if lo > len(all) {
		lo = len(all)
	}
	if hi < len(all) {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(pg+1))
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?%s>; rel="next"`, f.srv.URL, r.URL.Path, q.Encode()))
	} else {
		hi = len(all)
	}
	out := all[lo:hi]
	if out == nil {
		out = []any{}
	}
	_ = json.NewEncoder(w).Encode(out)
}

// ---- GraphQL

func (f *fake) graphql(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, "POST "+r.URL.Path)
	if f.gqlStatus != 0 {
		w.WriteHeader(f.gqlStatus)
		io.WriteString(w, `{"message":"Resource not accessible"}`)
		return
	}
	var req struct {
		Query string         `json:"query"`
		Vars  map[string]any `json:"variables"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	q, v := req.Query, req.Vars
	str := func(k string) string { s, _ := v[k].(string); return s }
	data := func(m map[string]any) { json.NewEncoder(w).Encode(map[string]any{"data": m}) }
	fail := func(typ, msg string, d map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{"data": d, "errors": []any{map[string]any{"type": typ, "message": msg}}})
	}
	statusJSON := func(p *fProject, it *fItem) any {
		for _, o := range p.Options {
			if o.ID == it.Status {
				return map[string]any{"name": o.Name, "optionId": o.ID}
			}
		}
		return nil
	}
	startJSON := func(p *fProject, it *fItem) any {
		for _, df := range p.DateFields {
			if df.Name == str("start") && it.Dates[df.ID] != "" {
				return map[string]any{"date": it.Dates[df.ID]}
			}
		}
		return nil
	}
	switch {
	case strings.Contains(q, "addProjectV2ItemById"):
		f.gqlOps = append(f.gqlOps, "add")
		p := f.projectByID(str("projectId"))
		var is *fIssue
		if n, err := strconv.Atoi(strings.TrimPrefix(str("contentId"), "I_")); err == nil {
			is = f.find(n)
		}
		if p == nil || is == nil {
			fail("NOT_FOUND", "no existe", nil)
			return
		}
		it := p.item(is)
		if it == nil {
			it = p.add(f, is, "")
		}
		data(map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]any{"id": it.ID}}})
	case strings.Contains(q, "updateProjectV2ItemFieldValue"):
		p := f.projectByID(str("projectId"))
		var it *fItem
		if p != nil {
			for _, x := range p.Items {
				if x.ID == str("itemId") {
					it = x
				}
			}
		}
		kind := "setDate"
		if _, ok := v["optionId"]; ok {
			kind = "setStatus"
		}
		f.gqlOps = append(f.gqlOps, kind)
		if f.notFound || it == nil {
			fail("NOT_FOUND", "no existe", nil)
			return
		}
		if kind == "setStatus" {
			if str("fieldId") != "F_status" || p.optByID(str("optionId")) == nil {
				fail("NOT_FOUND", "opción desconocida", nil)
				return
			}
			it.Status = str("optionId")
		} else {
			ok := false
			for _, df := range p.DateFields {
				ok = ok || df.ID == str("fieldId")
			}
			if !ok {
				fail("NOT_FOUND", "campo desconocido", nil)
				return
			}
			it.Dates[str("fieldId")] = str("date")
		}
		data(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": it.ID}}})
	case strings.Contains(q, "updateProjectV2Field"):
		f.gqlOps = append(f.gqlOps, "setField")
		f.lastFields = asList(v["options"])
		var p *fProject
		for _, ps := range []map[string][]*fProject{f.orgs, f.users} {
			for _, l := range ps {
				for _, x := range l {
					if str("fieldId") == "F_status" && x.Number > 0 && p == nil {
						p = x
					}
				}
			}
		}
		if p == nil {
			fail("NOT_FOUND", "campo desconocido", nil)
			return
		}
		old := map[string]fOpt{}
		for _, o := range p.Options {
			old[o.ID] = o
		}
		var next []fOpt
		for _, raw := range f.lastFields {
			m, _ := raw.(map[string]any)
			o := fOpt{Name: fmt.Sprint(m["name"]), Color: fmt.Sprint(m["color"]), Desc: fmt.Sprint(m["description"])}
			if id, ok := m["id"].(string); ok && f.idInput {
				o.ID = id
			} else {
				o.ID = f.id("OPT")
				if !f.idInput {
					f.destroyed = true
				}
			}
			next = append(next, o)
		}
		p.Options = next
		data(map[string]any{"updateProjectV2Field": map[string]any{"projectV2Field": map[string]any{"options": optsJSON(p.Options)}}})
	case strings.Contains(q, "__type"):
		f.gqlOps = append(f.gqlOps, "introspect")
		fields := []any{map[string]any{"name": "name"}, map[string]any{"name": "color"}, map[string]any{"name": "description"}}
		if f.idInput {
			fields = append(fields, map[string]any{"name": "id"})
		}
		data(map[string]any{"__type": map[string]any{"inputFields": fields}})
	case strings.Contains(q, "projectsV2"):
		f.gqlOps = append(f.gqlOps, "projects")
		kind, owners := "user", f.users
		if strings.Contains(q, "organization(") {
			kind, owners = "organization", f.orgs
		}
		ps, ok := owners[str("login")]
		if !ok {
			fail("NOT_FOUND", "Could not resolve to a "+kind, map[string]any{kind: nil})
			return
		}
		var nodes []any
		for _, p := range ps {
			nodes = append(nodes, map[string]any{"number": p.Number, "title": p.Title})
		}
		data(map[string]any{kind: map[string]any{"projectsV2": map[string]any{"nodes": nodes}}})
	case strings.Contains(q, "projectItems"):
		f.gqlOps = append(f.gqlOps, "item")
		n, _ := strconv.Atoi(strings.TrimPrefix(str("id"), "I_"))
		is := f.find(n)
		if is == nil {
			data(map[string]any{"node": nil})
			return
		}
		nodes := []any{}
		for _, m := range []map[string][]*fProject{f.orgs, f.users} {
			for _, ps := range m {
				for _, p := range ps {
					if it := p.item(is); it != nil {
						nodes = append(nodes, map[string]any{"id": it.ID, "project": map[string]any{"id": p.ID},
							"status": statusJSON(p, it), "start": startJSON(p, it)})
					}
				}
			}
		}
		data(map[string]any{"node": map[string]any{"projectItems": map[string]any{"nodes": nodes}}})
	case strings.Contains(q, "items("):
		f.gqlOps = append(f.gqlOps, "list")
		p := f.projectByID(str("id"))
		if p == nil {
			data(map[string]any{"node": nil})
			return
		}
		from, _ := strconv.Atoi(str("cursor"))
		to := from + f.pageSize
		more := to < len(p.Items)
		if !more {
			to = len(p.Items)
		}
		nodes := []any{}
		for _, it := range p.Items[from:to] {
			content := map[string]any{} // borradores y PR no coinciden con "... on Issue"
			if is := it.Issue; is != nil && !is.PR {
				repo := is.Repo
				if repo == "" {
					repo = "acme/app"
				}
				content = map[string]any{"number": is.N, "title": is.Title, "body": is.Body, "state": strings.ToUpper(is.State),
					"url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, is.N), "updatedAt": "2026-09-10T12:00:00Z",
					"repository": map[string]any{"nameWithOwner": repo}}
			}
			nodes = append(nodes, map[string]any{"id": it.ID, "status": statusJSON(p, it), "start": startJSON(p, it), "content": content})
		}
		data(map[string]any{"node": map[string]any{"items": map[string]any{"nodes": nodes,
			"pageInfo": map[string]any{"hasNextPage": more, "endCursor": strconv.Itoa(to)}}}})
	case strings.Contains(q, "projectV2("):
		f.gqlOps = append(f.gqlOps, "resolve")
		kind, owners := "user", f.users
		if strings.Contains(q, "organization(") {
			kind, owners = "organization", f.orgs
		}
		ps, ok := owners[str("login")]
		if !ok {
			fail("NOT_FOUND", "Could not resolve to a "+kind, map[string]any{kind: nil})
			return
		}
		if f.scopeErr != "" {
			fail(f.scopeErr, "Your token has not been granted the required scopes", map[string]any{kind: nil})
			return
		}
		num, _ := v["number"].(float64)
		for _, p := range ps {
			if float64(p.Number) == num {
				fields := []any{map[string]any{"id": "F_title", "name": "Title", "dataType": "TITLE"},
					map[string]any{"id": "F_status", "name": "Status", "options": optsJSON(p.Options)}}
				for _, df := range p.DateFields {
					fields = append(fields, map[string]any{"id": df.ID, "name": df.Name, "dataType": "DATE"})
				}
				data(map[string]any{kind: map[string]any{"projectV2": map[string]any{"id": p.ID, "title": p.Title,
					"fields": map[string]any{"nodes": fields}}}})
				return
			}
		}
		fail("NOT_FOUND", "no existe el project", map[string]any{kind: map[string]any{"projectV2": nil}})
	default:
		f.t.Errorf("consulta GraphQL no reconocida por el fake: %s", q)
		fail("BAD_REQUEST", "consulta desconocida", nil)
	}
}

func (p *fProject) optByID(id string) *fOpt {
	for i := range p.Options {
		if p.Options[i].ID == id {
			return &p.Options[i]
		}
	}
	return nil
}

func optsJSON(os []fOpt) []any {
	out := []any{}
	for _, o := range os {
		out = append(out, map[string]any{"id": o.ID, "name": o.Name, "color": o.Color, "description": o.Desc})
	}
	return out
}

// names lista los nombres de las opciones de Status.
func (p *fProject) names() []string {
	var out []string
	for _, o := range p.Options {
		out = append(out, o.Name)
	}
	return out
}

// TestFakeServes verifica al propio fake con HTTP crudo: si miente, las pruebas
// del adaptador no significan nada.
func TestFakeServes(t *testing.T) {
	f := newFake(t)
	f.pageSize = 2
	for i := 1; i <= 3; i++ {
		f.issue(i, "t")
	}
	p := f.project("acme", false, 7)
	do := func(method, path, body string) (int, string, http.Header) {
		req, _ := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+f.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b), res.Header
	}
	if code, body, h := do("GET", "/repos/acme/app/issues", ""); code != 200 || !strings.Contains(h.Get("Link"), `rel="next"`) || strings.Count(body, `"number"`) != 2 {
		t.Errorf("paginación: %d %s %v", code, body, h)
	}
	if code, _, _ := do("DELETE", "/repos/acme/app/issues/1/labels/nada", ""); code != 404 {
		t.Errorf("quitar una etiqueta ausente da 404: %d", code)
	}
	gql := func(q string, vars string) string {
		_, b, _ := do("POST", "/graphql", `{"query":"`+q+`","variables":`+vars+`}`)
		return b
	}
	if b := gql("organization(login:$login){projectV2(number:$number){id}}", `{"login":"acme","number":7}`); !strings.Contains(b, `"PVT_7"`) || !strings.Contains(b, "F_status") {
		t.Errorf("resolución: %s", b)
	}
	if b := gql("organization(login:$login){projectV2(number:$number){id}}", `{"login":"nadie","number":7}`); !strings.Contains(b, "NOT_FOUND") {
		t.Errorf("owner inexistente: %s", b)
	}
	gql("addProjectV2ItemById", `{"projectId":"PVT_7","contentId":"I_1"}`)
	gql("addProjectV2ItemById", `{"projectId":"PVT_7","contentId":"I_1"}`)
	if len(p.Items) != 1 {
		t.Errorf("add es idempotente: %d", len(p.Items))
	}
	gql("updateProjectV2ItemFieldValue", `{"projectId":"PVT_7","itemId":"`+p.Items[0].ID+`","fieldId":"F_status","optionId":"`+p.Options[1].ID+`"}`)
	if b := gql("node(id:$id){...on Issue{projectItems(first:50){nodes{id}}}}", `{"id":"I_1","start":"Start date"}`); !strings.Contains(b, p.Options[1].Name) {
		t.Errorf("item con status: %s", b)
	}
	if b := gql("node(id:$id){...on ProjectV2{items(first:100){nodes{id}}}}", `{"id":"PVT_7"}`); !strings.Contains(b, `"number":1`) || !strings.Contains(b, "hasNextPage") {
		t.Errorf("items: %s", b)
	}
}
