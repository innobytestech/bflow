// Package github es el adaptador de tracker sobre GitHub Issues y, si se
// configura, un Project v2. REST para issues, comentarios y etiquetas; GraphQL
// solo para Projects. Sin project la fase es una etiqueta bflow:<fase>; con
// project es una opción del campo Status.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
)

// Options configura el cliente.
type Options struct {
	API, Repo, Prefix, Project, StartField, Token string
	States                                        map[string][]string // tracker.states de bflow.yaml
	CachePath                                     string              // .bflow/cache/github.json
}

// Client implementa tracker.Tracker sobre GitHub.
type Client struct {
	API, Repo, Prefix, Project, StartField string
	Token                                  string
	HTTP                                   *http.Client
	States                                 tracker.StateTable
	cachePath                              string
	sleep                                  func(time.Duration)

	mu   sync.Mutex
	proj *projInfo // project resuelto en esta ejecución
	kind string    // "org", "user" o "": dueño que se consulta, para el mensaje de permisos
}

// New crea el cliente. El prefijo va en mayúsculas ("GH" si falta) y el campo
// de inicio es "Start date" si falta.
func New(o Options) *Client {
	c := &Client{API: strings.TrimRight(o.API, "/"), Repo: o.Repo, Prefix: strings.ToUpper(o.Prefix), Project: o.Project,
		StartField: o.StartField, Token: o.Token, HTTP: &http.Client{Timeout: 20 * time.Second},
		States: tracker.NewStateTable(o.States), cachePath: o.CachePath, sleep: time.Sleep}
	if c.Prefix == "" {
		c.Prefix = "GH"
	}
	if c.StartField == "" {
		c.StartField = "Start date"
	}
	if c.API == "" {
		c.API = "https://api.github.com"
	}
	return c
}

var (
	_ tracker.Tracker          = (*Client)(nil)
	_ tracker.Creator          = (*Client)(nil)
	_ tracker.StateProvisioner = (*Client)(nil)
	_ tracker.StateLister      = (*Client)(nil)
	_ tracker.ProjectLister    = (*Client)(nil)
	_ tracker.PRLinker         = (*Client)(nil)
)

func (c *Client) Name() string { return "github" }

// graphqlURL es …/api/graphql cuando la API termina en /api/v3 (Enterprise) y
// API + "/graphql" en otro caso.
func graphqlURL(api string) string {
	api = strings.TrimRight(api, "/")
	if base, ok := strings.CutSuffix(api, "/api/v3"); ok {
		return base + "/api/graphql"
	}
	return api + "/graphql"
}

// ---------- identificadores ----------

var keyRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)-([1-9][0-9]*)$`)

// parseKey devuelve N de "<PREFIJO>-N". Todo lo demás envuelve ErrNotFound.
func (c *Client) parseKey(id string) (int, error) {
	m := keyRe.FindStringSubmatch(id)
	if m == nil || !strings.EqualFold(m[1], c.Prefix) {
		return 0, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	return n, nil
}

// CloseRef es la línea que cierra el issue al mergear el PR ("Closes #42"),
// o "" si id no es de este prefijo.
func (c *Client) CloseRef(id string) string {
	n, err := c.parseKey(id)
	if err != nil {
		return ""
	}
	return "Closes #" + strconv.Itoa(n)
}

func (c *Client) key(n int) string { return c.Prefix + "-" + strconv.Itoa(n) }

// ---------- HTTP ----------

// apiError es una respuesta de error de la API, sin headers ni token.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func (e *apiError) Unwrap() error {
	if e.status == http.StatusNotFound {
		return tracker.ErrNotFound
	}
	return nil
}

// gqlError es un error del arreglo "errors" de una respuesta GraphQL.
type gqlError struct{ Type, Message string }

func (e *gqlError) Error() string { return "github graphql: " + e.Type + ": " + e.Message }

func isGQL(err error, typ string) bool {
	var g *gqlError
	return errors.As(err, &g) && g.Type == typ
}

func (c *Client) redact(s string) string {
	if c.Token != "" {
		s = strings.ReplaceAll(s, c.Token, "***")
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// permError dice que falta el permiso de Projects y qué token hace falta.
func (c *Client) permError(detail string) error {
	org := "una organización necesita un token fine-grained con el permiso de organización Projects"
	usr := "un proyecto personal necesita un token clásico con el scope project"
	var need string
	switch c.kind {
	case "org":
		need = org
	case "user":
		need = usr
	default:
		need = org + "; " + usr
	}
	e := "github: el token no tiene el permiso de Projects: " + need + " (bflow connect github)"
	if detail != "" {
		e += ": " + c.redact(detail)
	}
	return errors.New(e)
}

func noQuery(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

// send hace una petición con los reintentos por límite de uso. Ningún error
// contiene el token ni headers.
func (c *Client) send(ctx context.Context, method, rawURL string, in, out any, graphql bool) (http.Header, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("github: no hay token para %s (bflow connect github, o GH_TOKEN/GITHUB_TOKEN)", c.API)
	}
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return nil, err
		}
	}
	shown := noQuery(strings.TrimPrefix(rawURL, c.API))
	if graphql {
		shown = "/graphql"
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("github %s %s: %s", method, shown, c.redact(err.Error()))
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		h := resp.Header
		limited := resp.StatusCode == 429 ||
			(resp.StatusCode == 403 && (h.Get("Retry-After") != "" || h.Get("x-ratelimit-remaining") == "0"))
		if limited && attempt < 3 {
			c.sleep(rateWait(h))
			continue
		}
		if resp.StatusCode >= 300 {
			return h, c.httpError(method, shown, resp.StatusCode, raw, graphql)
		}
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return h, fmt.Errorf("github %s %s: respuesta ilegible: %s", method, shown, c.redact(err.Error()))
			}
		}
		return h, nil
	}
}

// rateWait es la espera antes de reintentar: Retry-After, o hasta
// x-ratelimit-reset, entre 1 y 30 s.
func rateWait(h http.Header) time.Duration {
	secs := 1
	if v, err := strconv.Atoi(h.Get("Retry-After")); err == nil {
		secs = v
	} else if v, err := strconv.ParseInt(h.Get("x-ratelimit-reset"), 10, 64); err == nil {
		secs = int(v - time.Now().Unix())
	}
	return time.Duration(min(max(secs, 1), 30)) * time.Second
}

func (c *Client) httpError(method, shown string, status int, raw []byte, graphql bool) error {
	msg := strings.TrimSpace(string(raw))
	var d struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &d) == nil && d.Message != "" {
		msg = d.Message
	}
	msg = c.redact(msg)
	switch {
	case status == 401:
		return &apiError{status, "github: token inválido o vencido (bflow connect github)"}
	case status == 403 && graphql:
		return c.permError(msg)
	case status == 403:
		return &apiError{status, fmt.Sprintf("github %s %s → 403: %s (revisa los permisos del token: bflow connect github)", method, shown, msg)}
	}
	return &apiError{status, fmt.Sprintf("github %s %s → %d: %s", method, shown, status, msg)}
}

// do hace una petición REST sobre la API.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	_, err := c.send(ctx, method, c.API+path, in, out, false)
	return err
}

// getAll recorre todas las páginas de un listado REST (Link rel="next", tope de 50).
func getAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	base, err := url.Parse(c.API)
	if err != nil {
		return nil, err
	}
	var all []T
	next := c.API + path
	for page := 0; next != ""; page++ {
		if page >= 50 {
			return nil, fmt.Errorf("github GET %s: más de 50 páginas", noQuery(path))
		}
		var items []T
		h, err := c.send(ctx, "GET", next, nil, &items, false)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		next = nextLink(h.Get("Link"))
		if next != "" {
			if u, err := url.Parse(next); err != nil || u.Host != base.Host {
				return nil, fmt.Errorf("github GET %s: Link a otro host", noQuery(path))
			}
		}
	}
	return all, nil
}

func nextLink(link string) string {
	for _, part := range strings.Split(link, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		s, e := strings.IndexByte(part, '<'), strings.IndexByte(part, '>')
		if s >= 0 && e > s {
			return part[s+1 : e]
		}
	}
	return ""
}

// graphql ejecuta una consulta. Decodifica data en out aunque haya errores y
// devuelve el primero (*gqlError), salvo los de permisos, que dicen qué token
// hace falta.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	body := map[string]any{"query": query, "variables": vars}
	if _, err := c.send(ctx, "POST", graphqlURL(c.API), body, &resp, true); err != nil {
		return err
	}
	if out != nil && len(resp.Data) > 0 && string(resp.Data) != "null" {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("github graphql: respuesta ilegible: %s", c.redact(err.Error()))
		}
	}
	if len(resp.Errors) > 0 {
		e := resp.Errors[0]
		if e.Type == "INSUFFICIENT_SCOPES" || e.Type == "FORBIDDEN" {
			return c.permError(e.Message)
		}
		e.Message = c.redact(e.Message)
		return &e
	}
	return nil
}

// SameState indica si dos fases escriben en la misma etiqueta u opción.
func (c *Client) SameState(a, b flow.Phase) bool {
	if c.Project == "" {
		return a == b
	}
	p, err := c.loadProject(context.Background(), false)
	if err != nil {
		return false
	}
	na, oka := c.States.Write(p.optionNames(), a)
	nb, okb := c.States.Write(p.optionNames(), b)
	return oka && okb && strings.EqualFold(na, nb)
}
