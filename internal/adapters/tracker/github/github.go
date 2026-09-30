// Package github es el adaptador de tracker sobre GitHub Issues y, si se
// configura, un Project v2. REST para issues, comentarios y etiquetas; GraphQL
// solo para Projects. Sin project la fase es una etiqueta bflow:<fase>; con
// project es una opción del campo Status.
package github

import (
	"errors"
	"net/http"
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

	mu sync.Mutex
}

var errTodo = errors.New("github: no implementado")

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
func graphqlURL(api string) string { return "" }

// CloseRef es la línea que cierra el issue al mergear el PR ("Closes #42"),
// o "" si id no es de este prefijo.
func (c *Client) CloseRef(id string) string { return "" }

// SameState indica si dos fases escriben en la misma etiqueta u opción.
func (c *Client) SameState(a, b flow.Phase) bool { return false }
