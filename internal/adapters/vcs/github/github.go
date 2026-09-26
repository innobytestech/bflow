// Package github implementa vcs.Host con la API REST de GitHub.
package github

import (
	"bytes"
	"context"
	"encoding/json"
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
}

func (p apiPR) pr() vcs.PR {
	st := p.State
	if p.Merged {
		st = "merged"
	}
	return vcs.PR{Number: p.Number, URL: p.HTMLURL, State: st, Merged: p.Merged}
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
		return fmt.Errorf("github %s %s → %d: %s%s", method, path, resp.StatusCode, msg, hint)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *Client) pulls() string { return "/repos/" + c.Repo + "/pulls" }

func (c *Client) OpenPR(ctx context.Context, s vcs.PRSpec) (vcs.PR, error) {
	var p apiPR
	err := c.do(ctx, "POST", c.pulls(), map[string]string{"title": s.Title, "head": s.Head, "base": s.Base, "body": s.Body}, &p)
	return p.pr(), err
}

// FindPR busca un PR abierto cuya rama origen sea head.
func (c *Client) FindPR(ctx context.Context, head string) (vcs.PR, error) {
	owner := strings.SplitN(c.Repo, "/", 2)[0]
	q := url.Values{"head": {owner + ":" + head}, "state": {"open"}}
	var ps []apiPR
	if err := c.do(ctx, "GET", c.pulls()+"?"+q.Encode(), nil, &ps); err != nil {
		return vcs.PR{}, err
	}
	if len(ps) == 0 {
		return vcs.PR{}, vcs.ErrNoPR
	}
	return ps[0].pr(), nil
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
