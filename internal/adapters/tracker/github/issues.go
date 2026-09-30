package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
)

const labelPrefix = "bflow:"

type ghIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	HTMLURL     string    `json:"html_url"`
	UpdatedAt   time.Time `json:"updated_at"`
	NodeID      string    `json:"node_id"`
	PullRequest any       `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (c *Client) issuePath(n int) string { return "/repos/" + c.Repo + "/issues/" + strconv.Itoa(n) }

// issue lee el issue de id; un PR cuenta como inexistente.
func (c *Client) issue(ctx context.Context, id string) (*ghIssue, error) {
	n, err := c.parseKey(id)
	if err != nil {
		return nil, err
	}
	var is ghIssue
	if err := c.do(ctx, "GET", c.issuePath(n), nil, &is); err != nil {
		if errors.Is(err, tracker.ErrNotFound) {
			return nil, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
		}
		return nil, err
	}
	if is.PullRequest != nil {
		return nil, fmt.Errorf("%s es un pull request: %w", id, tracker.ErrNotFound)
	}
	return &is, nil
}

func phaseFromLabel(name string) (flow.Phase, bool) {
	s, ok := strings.CutPrefix(name, labelPrefix)
	if !ok {
		return "", false
	}
	for _, p := range tracker.PhaseOrder {
		if string(p) == s {
			return p, true
		}
	}
	return "", false
}

// labelPhase es la fase de las etiquetas bflow:* (gana la más avanzada) y su
// etiqueta; "" si no hay ninguna.
func (is *ghIssue) labelPhase() (flow.Phase, string) {
	best, bestIdx, label := flow.Phase(""), -1, ""
	for _, l := range is.Labels {
		p, ok := phaseFromLabel(l.Name)
		if !ok {
			continue
		}
		for i, q := range tracker.PhaseOrder {
			if q == p && i > bestIdx {
				best, bestIdx, label = p, i, l.Name
			}
		}
	}
	return best, label
}

// task arma la tarea leyendo la fase de las etiquetas.
func (c *Client) task(is *ghIssue) tracker.Task {
	t := tracker.Task{ID: c.key(is.Number), Title: is.Title, Description: is.Body, URL: is.HTMLURL,
		Updated: is.UpdatedAt, Closed: is.State == "closed"}
	t.Phase, t.State = is.labelPhase()
	if t.Phase == "" {
		t.Phase = flow.Backlog
		if t.Closed {
			t.Phase = flow.Done
		}
	}
	return t
}

func (c *Client) Get(ctx context.Context, id string) (tracker.Task, error) {
	is, err := c.issue(ctx, id)
	if err != nil {
		return tracker.Task{}, err
	}
	t := c.task(is)
	if c.Project == "" {
		return t, nil
	}
	err = c.withProject(ctx, func(p *projInfo) error {
		it, err := c.itemOf(ctx, p, is.NodeID)
		if err != nil {
			return err
		}
		t.Phase, t.State, t.Start = flow.Backlog, "", nil
		if it != nil {
			t.Start = it.Start
			if it.Status != "" {
				t.State, t.Phase = it.Status, c.States.PhaseOf(it.Status)
			}
		}
		return nil
	})
	return t, err
}

func (c *Client) List(ctx context.Context, f tracker.Filter) ([]tracker.Task, error) {
	if c.Project != "" {
		return c.listProject(ctx, f)
	}
	state := "all"
	if f.OpenOnly {
		state = "open"
	}
	issues, err := getAll[ghIssue](ctx, c, "/repos/"+c.Repo+"/issues?state="+state+"&per_page=100")
	if err != nil {
		return nil, err
	}
	var out []tracker.Task
	for i := range issues {
		if issues[i].PullRequest == nil {
			out = append(out, c.task(&issues[i]))
		}
	}
	return out, nil
}

func (c *Client) Transition(ctx context.Context, id string, to flow.Phase, p tracker.Patch) error {
	is, err := c.issue(ctx, id)
	if err != nil {
		return err
	}
	if c.Project != "" {
		err = c.withProject(ctx, func(pr *projInfo) error { return c.setProjectStatus(ctx, pr, is, to, p) })
	} else {
		err = c.setLabel(ctx, is, to)
	}
	if err != nil {
		return err
	}
	if to == flow.Done && is.State != "closed" {
		return c.do(ctx, "PATCH", c.issuePath(is.Number), map[string]any{"state": "closed", "state_reason": "completed"}, nil)
	}
	return nil
}

// setLabel deja solo la etiqueta de la fase entre las bflow:*; las demás no se tocan.
func (c *Client) setLabel(ctx context.Context, is *ghIssue, to flow.Phase) error {
	target := labelPrefix + string(to)
	have := false
	var stale []string
	for _, l := range is.Labels {
		switch {
		case l.Name == target:
			have = true
		case strings.HasPrefix(l.Name, labelPrefix):
			stale = append(stale, l.Name)
		}
	}
	if !have {
		if err := c.do(ctx, "POST", c.issuePath(is.Number)+"/labels", map[string]any{"labels": []string{target}}, nil); err != nil {
			return err
		}
	}
	for _, name := range stale {
		err := c.do(ctx, "DELETE", c.issuePath(is.Number)+"/labels/"+url.PathEscape(name), nil, nil)
		var ae *apiError
		if err != nil && !(errors.As(err, &ae) && ae.status == 404) {
			return err
		}
	}
	return nil
}

func (c *Client) Comment(ctx context.Context, id string, markdown string) error {
	n, err := c.parseKey(id)
	if err != nil {
		return err
	}
	err = c.do(ctx, "POST", c.issuePath(n)+"/comments", map[string]any{"body": markdown}, nil)
	if errors.Is(err, tracker.ErrNotFound) {
		return fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
	}
	return err
}

func (c *Client) Comments(ctx context.Context, id string) ([]tracker.Comment, error) {
	n, err := c.parseKey(id)
	if err != nil {
		return nil, err
	}
	type ghComment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Created time.Time `json:"created_at"`
	}
	cs, err := getAll[ghComment](ctx, c, c.issuePath(n)+"/comments?per_page=100")
	if err != nil {
		if errors.Is(err, tracker.ErrNotFound) {
			return nil, fmt.Errorf("%s: %w", id, tracker.ErrNotFound)
		}
		return nil, err
	}
	var out []tracker.Comment
	for _, x := range cs {
		out = append(out, tracker.Comment{ID: strconv.FormatInt(x.ID, 10), Body: x.Body, Author: x.User.Login, Created: x.Created})
	}
	return out, nil
}

func (c *Client) Create(ctx context.Context, title, description string) (tracker.Task, error) {
	body := map[string]any{"title": title, "body": description}
	var is ghIssue
	if c.Project == "" {
		body["labels"] = []string{labelPrefix + string(flow.Backlog)}
		if err := c.do(ctx, "POST", "/repos/"+c.Repo+"/issues", body, &is); err != nil {
			return tracker.Task{}, err
		}
		return c.task(&is), nil
	}
	// La opción de backlog debe existir antes de crear el issue.
	if err := c.withProject(ctx, func(p *projInfo) error {
		_, _, err := p.optionFor(c, flow.Backlog)
		return err
	}); err != nil {
		return tracker.Task{}, err
	}
	if err := c.do(ctx, "POST", "/repos/"+c.Repo+"/issues", body, &is); err != nil {
		return tracker.Task{}, err
	}
	t := c.task(&is)
	t.Phase = flow.Backlog
	err := c.withProject(ctx, func(p *projInfo) error { return c.setProjectStatus(ctx, p, &is, flow.Backlog, tracker.Patch{}) })
	return t, err
}
