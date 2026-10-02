package github

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
)

// optionColors son los colores de las opciones de Status (enum de GitHub).
var optionColors = map[flow.Phase]string{
	flow.Backlog: "GRAY", flow.Discovery: "BLUE", flow.Spec: "BLUE", flow.Contract: "PURPLE", flow.Implementing: "BLUE",
	flow.Paused: "PURPLE", flow.Quality: "PINK", flow.Documenting: "GREEN", flow.Walkthrough: "ORANGE",
	flow.InReview: "ORANGE", flow.Done: "GREEN", flow.Blocked: "RED", flow.Dropped: "GRAY",
}

type ghLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (c *Client) labels(ctx context.Context) ([]ghLabel, error) {
	return getAll[ghLabel](ctx, c, "/repos/"+c.Repo+"/labels?per_page=100")
}

// missingOption es una opción de Status que hay que crear.
type missingOption struct {
	name, color, desc string
}

// missingOptions calcula las opciones que faltan: el primer nombre de cada
// fase sin opción (las que se agregan cuentan para las fases siguientes).
func (c *Client) missingOptions(p *projInfo) []missingOption {
	names := p.optionNames()
	var out []missingOption
	for _, ph := range tracker.PhaseOrder {
		if _, ok := c.States.Write(names, ph); ok {
			continue
		}
		n := c.States[ph].Names[0]
		names = append(names, n)
		out = append(out, missingOption{n, optionColors[ph], "bflow: " + string(ph)})
	}
	return out
}

const (
	introspectQ = `query{__type(name:"ProjectV2SingleSelectFieldOptionInput"){inputFields{name}}}`
	setFieldQ   = `mutation($fieldId:ID!,$options:[ProjectV2SingleSelectFieldOptionInput!]!){updateProjectV2Field(input:{` +
		`fieldId:$fieldId,singleSelectOptions:$options}){projectV2Field{... on ProjectV2SingleSelectField{options{id name color description}}}}}`
)

func (c *Client) EnsureStates(ctx context.Context, dryRun bool) ([]string, error) {
	if c.Project == "" {
		return c.ensureLabels(ctx, dryRun)
	}
	p, err := c.loadProject(ctx, true)
	if err != nil {
		return nil, err
	}
	missing := c.missingOptions(p)
	var names, manual []string
	for _, m := range missing {
		names = append(names, m.name)
		manual = append(manual, m.name+" ("+m.color+")")
	}
	if len(missing) == 0 || dryRun {
		return names, nil
	}
	var intro struct {
		Type *struct {
			InputFields []struct {
				Name string `json:"name"`
			} `json:"inputFields"`
		} `json:"__type"`
	}
	if err := c.graphql(ctx, introspectQ, nil, &intro); err != nil {
		return nil, err
	}
	hasID := false
	if intro.Type != nil {
		for _, f := range intro.Type.InputFields {
			hasID = hasID || f.Name == "id"
		}
	}
	if !hasID {
		return nil, &tracker.ManualSetupError{Where: "el project " + c.Project + ", campo Status", Missing: manual,
			Reason: "esta API no deja conservar los ids de las opciones existentes; escribir pisaría el Status de los items"}
	}
	opts := []map[string]any{}
	for _, o := range p.Opts {
		opts = append(opts, map[string]any{"id": o.ID, "name": o.Name, "color": o.Color, "description": o.Description})
	}
	for _, m := range missing {
		opts = append(opts, map[string]any{"name": m.name, "color": m.color, "description": m.desc})
	}
	var res struct {
		Update struct {
			Field struct {
				Options []optInfo `json:"options"`
			} `json:"projectV2Field"`
		} `json:"updateProjectV2Field"`
	}
	if err := c.graphql(ctx, setFieldQ, map[string]any{"fieldId": p.StatusField, "options": opts}, &res); err != nil {
		return nil, err
	}
	after := map[string]bool{}
	for _, o := range res.Update.Field.Options {
		after[o.ID] = true
	}
	for _, o := range p.Opts {
		if !after[o.ID] {
			c.dropCache()
			return nil, fmt.Errorf("github: al actualizar Status cambió el id de la opción %q; revisa el project %s", o.Name, c.Project)
		}
	}
	p.Opts, p.Options = res.Update.Field.Options, map[string]string{}
	for _, o := range p.Opts {
		p.Options[o.Name] = o.ID
	}
	c.writeCache(p.cacheFile)
	return names, nil
}

func (c *Client) ensureLabels(ctx context.Context, dryRun bool) ([]string, error) {
	have, err := c.labels(ctx)
	if err != nil {
		return nil, err
	}
	exists := map[string]bool{}
	for _, l := range have {
		exists[strings.ToLower(l.Name)] = true
	}
	var created []string
	for _, ph := range tracker.PhaseOrder {
		name := labelPrefix + string(ph)
		if exists[name] {
			continue
		}
		created = append(created, name)
		if dryRun {
			continue
		}
		body := map[string]any{"name": name, "color": strings.TrimPrefix(c.States[ph].Color, "#"), "description": "bflow: " + string(ph)}
		if err := c.do(ctx, "POST", "/repos/"+c.Repo+"/labels", body, nil); err != nil {
			if ae, ok := err.(*apiError); !ok || ae.status != http.StatusUnprocessableEntity { // ya existe: alguien la creó a la vez
				return created, err
			}
		}
	}
	return created, nil
}

func (c *Client) StateMap(ctx context.Context) ([]tracker.StateInfo, error) {
	var out []tracker.StateInfo
	if c.Project == "" {
		ls, err := c.labels(ctx)
		if err != nil {
			return nil, err
		}
		for _, l := range ls {
			if !strings.HasPrefix(l.Name, labelPrefix) {
				continue
			}
			s := tracker.StateInfo{Name: l.Name, Group: "label"}
			if ph, ok := phaseFromLabel(l.Name); ok {
				s.Phase, s.Writes = ph, []string{string(ph)}
			}
			out = append(out, s)
		}
		return out, nil
	}
	p, err := c.loadProject(ctx, true)
	if err != nil {
		return nil, err
	}
	names := p.optionNames()
	for _, o := range p.Opts {
		s := tracker.StateInfo{Name: o.Name, Group: "status", Phase: c.States.PhaseOf(o.Name)}
		for _, ph := range tracker.PhaseOrder {
			if n, ok := c.States.Write(names, ph); ok && strings.EqualFold(n, o.Name) {
				s.Writes = append(s.Writes, string(ph))
			}
		}
		out = append(out, s)
	}
	return out, nil
}

const projectsQuery = `query($login:String!){%s(login:$login){projectsV2(first:100){nodes{number title}}}}`

// Projects lista los Projects v2 del dueño del repo (organización o usuario).
func (c *Client) Projects(ctx context.Context) ([]tracker.Project, error) {
	owner, _, _ := strings.Cut(c.Repo, "/")
	for _, kind := range []string{"organization", "user"} {
		c.kind = map[string]string{"organization": "org", "user": "user"}[kind]
		var out map[string]*struct {
			Projects struct {
				Nodes []struct {
					Number int    `json:"number"`
					Title  string `json:"title"`
				} `json:"nodes"`
			} `json:"projectsV2"`
		}
		err := c.graphql(ctx, fmt.Sprintf(projectsQuery, kind), map[string]any{"login": owner}, &out)
		if err != nil {
			if isGQL(err, "NOT_FOUND") {
				continue
			}
			return nil, err
		}
		if out[kind] == nil {
			continue
		}
		var ps []tracker.Project
		for _, n := range out[kind].Projects.Nodes {
			ps = append(ps, tracker.Project{ID: owner + "/" + strconv.Itoa(n.Number), Name: n.Title})
		}
		return ps, nil
	}
	return nil, fmt.Errorf("github: no encuentro al dueño %q del repo entre organizaciones y usuarios", owner)
}
