// Package github implementa vcs.Host con la API REST de GitHub.
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
	"strings"
	"time"

	"innobytes.tech/bflow/internal/vcs"
)

// Client habla con GitHub para un repo owner/name.
type Client struct {
	API   string // https://api.github.com (GitHub Enterprise: https://host/api/v3)
	Web   string // https://github.com
	Repo  string // owner/name
	Token string
	HTTP  *http.Client
}

// New crea un cliente para repo con el token dado ("" = sin credenciales).
func New(repo, token string) *Client {
	return &Client{API: "https://api.github.com", Web: "https://github.com", Repo: repo, Token: token,
		HTTP: &http.Client{Timeout: 20 * time.Second}}
}

var _ vcs.Host = (*Client)(nil)

func (c *Client) Name() string { return "github" }

type apiPR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	// La lista de pulls no trae "merged", solo "merged_at".
	MergedAt *string `json:"merged_at"`
}

func (p apiPR) pr() vcs.PR {
	merged := p.Merged || p.MergedAt != nil
	st := p.State
	if merged {
		st = "merged"
	}
	return vcs.PR{Number: p.Number, URL: p.HTMLURL, State: st, Merged: merged}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	if c.Token == "" {
		return vcs.ErrNoCredentials
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.API, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Message
		for _, x := range e.Errors {
			msg += "; " + x.Message
		}
		hint := ""
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			hint = " (revisa el token: bflow connect github)"
		}
		return &apiError{status: resp.StatusCode, text: fmt.Sprintf("github %s %s → %d: %s%s", method, path, resp.StatusCode, msg, hint)}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type apiError struct {
	status int
	text   string
}

func (e *apiError) Error() string { return e.text }

// noAccess traduce un 404 sobre el repositorio: GitHub lo responde cuando el
// token no ve el repo, para no revelar que existe.
func (c *Client) noAccess(err error) error {
	var ae *apiError
	if errors.As(err, &ae) && ae.status == http.StatusNotFound {
		return fmt.Errorf("%w %s: GitHub responde 404 cuando el token no lo ve (bflow connect github con un token que tenga acceso)", vcs.ErrNoAccess, c.Repo)
	}
	return err
}

func (c *Client) pulls() string { return "/repos/" + c.Repo + "/pulls" }

// CheckRepo confirma que el token ve el repositorio.
func (c *Client) CheckRepo(ctx context.Context) error {
	return c.noAccess(c.do(ctx, "GET", "/repos/"+c.Repo, nil, nil))
}

func (c *Client) OpenPR(ctx context.Context, s vcs.PRSpec) (vcs.PR, error) {
	var p apiPR
	err := c.do(ctx, "POST", c.pulls(), map[string]string{"title": s.Title, "head": s.Head, "base": s.Base, "body": s.Body}, &p)
	return p.pr(), c.noAccess(err)
}

// FindPR busca un PR abierto cuya rama origen sea head.
func (c *Client) FindPR(ctx context.Context, head string) (vcs.PR, error) {
	owner := strings.SplitN(c.Repo, "/", 2)[0]
	q := url.Values{"head": {owner + ":" + head}, "state": {"open"}}
	var ps []apiPR
	if err := c.do(ctx, "GET", c.pulls()+"?"+q.Encode(), nil, &ps); err != nil {
		return vcs.PR{}, c.noAccess(err)
	}
	if len(ps) == 0 {
		return vcs.PR{}, vcs.ErrNoPR
	}
	return ps[0].pr(), nil
}

// FindMergedPR busca un PR ya mergeado cuya rama origen sea head.
func (c *Client) FindMergedPR(ctx context.Context, head string) (vcs.PR, error) {
	owner := strings.SplitN(c.Repo, "/", 2)[0]
	q := url.Values{"head": {owner + ":" + head}, "state": {"closed"}}
	var ps []apiPR
	if err := c.do(ctx, "GET", c.pulls()+"?"+q.Encode(), nil, &ps); err != nil {
		return vcs.PR{}, c.noAccess(err)
	}
	for _, p := range ps {
		if pr := p.pr(); pr.Merged {
			return pr, nil
		}
	}
	return vcs.PR{}, vcs.ErrNoPR
}

func (c *Client) UpdatePRBody(ctx context.Context, n int, body string) error {
	return c.do(ctx, "PATCH", fmt.Sprintf("%s/%d", c.pulls(), n), map[string]string{"body": body}, nil)
}

func (c *Client) PRStatus(ctx context.Context, n int) (vcs.PR, error) {
	var p apiPR
	err := c.do(ctx, "GET", fmt.Sprintf("%s/%d", c.pulls(), n), nil, &p)
	return p.pr(), err
}

func (c *Client) CompareURL(base, head string) string {
	return fmt.Sprintf("%s/%s/compare/%s...%s?expand=1", strings.TrimRight(c.Web, "/"), c.Repo, base, head)
}

// User devuelve el login del dueño del token (valida credenciales).
func (c *Client) User(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	err := c.do(ctx, "GET", "/user", nil, &u)
	return u.Login, err
}
