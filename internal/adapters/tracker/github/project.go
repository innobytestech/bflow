package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/store"
	"innobytes.tech/bflow/internal/tracker"
)

// cacheFile es .bflow/cache/github.json: solo ids públicos del project.
type cacheFile struct {
	Project     string            `json:"project"`
	OwnerType   string            `json:"owner_type"` // org | user
	ProjectID   string            `json:"project_id"`
	StatusField string            `json:"status_field"`
	Options     map[string]string `json:"options"` // nombre de la opción → id
	StartField  string            `json:"start_field,omitempty"`
	StartName   string            `json:"start_name,omitempty"`
}

type optInfo struct{ ID, Name, Color, Description string }

// projInfo es el project resuelto. Opts (con color y descripción) solo viene
// de la red; de la caché vienen nada más los ids.
type projInfo struct {
	cacheFile
	Opts      []optInfo
	fromCache bool
}

func (p *projInfo) optionNames() []string {
	var out []string
	for n := range p.Options {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// noOptionError: el project no tiene opción de Status para la fase. Con una
// caché vieja puede ser solo que alguien la agregó después.
type noOptionError struct{ msg string }

func (e *noOptionError) Error() string { return e.msg }

// optionFor es la opción de Status donde se escribe la fase.
func (p *projInfo) optionFor(c *Client, phase flow.Phase) (name, id string, err error) {
	name, ok := c.States.Write(p.optionNames(), phase)
	if !ok {
		return "", "", &noOptionError{fmt.Sprintf("github: el project %s no tiene opción de Status para la fase %s (corre bflow tracker setup, o ajusta tracker.states)", c.Project, phase)}
	}
	for n, id := range p.Options {
		if strings.EqualFold(n, name) {
			return n, id, nil
		}
	}
	return "", "", &noOptionError{"github: opción de Status sin id: " + name}
}

func (c *Client) readCache() (cacheFile, bool) {
	var cf cacheFile
	if c.cachePath == "" {
		return cf, false
	}
	raw, err := os.ReadFile(c.cachePath)
	if err != nil || json.Unmarshal(raw, &cf) != nil {
		return cf, false
	}
	ok := cf.Project == c.Project && cf.ProjectID != "" && cf.StatusField != "" &&
		(cf.StartName == "" || strings.EqualFold(cf.StartName, c.StartField))
	return cf, ok
}

func (c *Client) writeCache(cf cacheFile) {
	if c.cachePath == "" {
		return
	}
	raw, err := json.MarshalIndent(cf, "", "  ")
	if err != nil || os.MkdirAll(filepath.Dir(c.cachePath), 0o755) != nil {
		return
	}
	_ = store.WriteAtomic(c.cachePath, append(raw, '\n'))
}

// loadProject da el project: de esta ejecución, de la caché o de la red. Con
// fresh lo lee siempre de la red y reescribe la caché.
func (c *Client) loadProject(ctx context.Context, fresh bool) (*projInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !fresh {
		if c.proj != nil {
			return c.proj, nil
		}
		if cf, ok := c.readCache(); ok {
			c.kind = cf.OwnerType
			c.proj = &projInfo{cacheFile: cf, fromCache: true}
			return c.proj, nil
		}
	}
	p, err := c.resolveProject(ctx)
	if err != nil {
		return nil, err
	}
	c.proj = p
	c.writeCache(p.cacheFile)
	return p, nil
}

func (c *Client) dropCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proj = nil
	if c.cachePath != "" {
		_ = os.Remove(c.cachePath)
	}
}

// withProject ejecuta fn con el project. Si falla por un id que vino de la
// caché (NOT_FOUND u opción que falta), la descarta y reintenta una vez.
func (c *Client) withProject(ctx context.Context, fn func(*projInfo) error) error {
	p, err := c.loadProject(ctx, false)
	if err != nil {
		return err
	}
	err = fn(p)
	var no *noOptionError
	if err != nil && p.fromCache && (isGQL(err, "NOT_FOUND") || errors.As(err, &no)) {
		c.dropCache()
		if p, err = c.loadProject(ctx, true); err != nil {
			return err
		}
		return fn(p)
	}
	return err
}

func splitProject(project string) (owner string, number int, err error) {
	o, n, ok := strings.Cut(project, "/")
	number, convErr := strconv.Atoi(n)
	if !ok || o == "" || convErr != nil || number < 1 {
		return "", 0, fmt.Errorf("github: tracker.project debe ser owner/número, no %q", project)
	}
	return o, number, nil
}

const resolveQuery = `query($login:String!,$number:Int!){%s(login:$login){projectV2(number:$number){id title fields(first:50){nodes{` +
	`... on ProjectV2SingleSelectField{id name options{id name color description}} ... on ProjectV2Field{id name dataType}}}}}}`

type fieldNode struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	DataType string    `json:"dataType"`
	Options  []optInfo `json:"options"`
}

// resolveProject resuelve owner/N primero como organización y luego como usuario.
func (c *Client) resolveProject(ctx context.Context) (*projInfo, error) {
	owner, number, err := splitProject(c.Project)
	if err != nil {
		return nil, err
	}
	for _, kind := range []string{"organization", "user"} {
		c.kind = map[string]string{"organization": "org", "user": "user"}[kind]
		var out map[string]*struct {
			ProjectV2 *struct {
				ID     string `json:"id"`
				Fields struct {
					Nodes []fieldNode `json:"nodes"`
				} `json:"fields"`
			} `json:"projectV2"`
		}
		err := c.graphql(ctx, fmt.Sprintf(resolveQuery, kind), map[string]any{"login": owner, "number": number}, &out)
		if err != nil && !isGQL(err, "NOT_FOUND") {
			return nil, err
		}
		od := out[kind]
		if od == nil || od.ProjectV2 == nil {
			continue
		}
		pv := od.ProjectV2
		p := &projInfo{cacheFile: cacheFile{Project: c.Project, OwnerType: c.kind, ProjectID: pv.ID,
			Options: map[string]string{}, StartName: c.StartField}}
		for _, f := range pv.Fields.Nodes {
			switch {
			case strings.EqualFold(f.Name, "Status") && f.DataType == "":
				p.StatusField = f.ID
				p.Opts = f.Options
				for _, o := range f.Options {
					p.Options[o.Name] = o.ID
				}
			case f.DataType == "DATE" && strings.EqualFold(f.Name, c.StartField):
				p.StartField = f.ID
			}
		}
		if p.StatusField == "" {
			return nil, fmt.Errorf("github: el project %s no tiene el campo Status (campo de selección única)", c.Project)
		}
		return p, nil
	}
	return nil, fmt.Errorf("github: no encuentro el project %s (¿existe y el token lo ve?)", c.Project)
}

// ---------- items ----------

type itemInfo struct {
	ID       string
	Status   string
	OptionID string
	Start    *time.Time
}

type statusVal struct {
	Name     string `json:"name"`
	OptionID string `json:"optionId"`
}

type dateVal struct {
	Date string `json:"date"`
}

func parseDate(d *dateVal) *time.Time {
	if d == nil || d.Date == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", d.Date)
	if err != nil {
		return nil
	}
	return &t
}

const itemQuery = `query($id:ID!,$start:String!){node(id:$id){... on Issue{projectItems(first:50){nodes{id project{id} ` +
	`status:fieldValueByName(name:"Status"){... on ProjectV2ItemFieldSingleSelectValue{name optionId}} ` +
	`start:fieldValueByName(name:$start){... on ProjectV2ItemFieldDateValue{date}}}}}}}`

// itemOf es el item del issue en este project, o nil si no está.
func (c *Client) itemOf(ctx context.Context, p *projInfo, nodeID string) (*itemInfo, error) {
	var out struct {
		Node *struct {
			ProjectItems struct {
				Nodes []struct {
					ID      string `json:"id"`
					Project struct {
						ID string `json:"id"`
					} `json:"project"`
					Status *statusVal `json:"status"`
					Start  *dateVal   `json:"start"`
				} `json:"nodes"`
			} `json:"projectItems"`
		} `json:"node"`
	}
	if err := c.graphql(ctx, itemQuery, map[string]any{"id": nodeID, "start": c.StartField}, &out); err != nil {
		return nil, err
	}
	if out.Node == nil {
		return nil, nil
	}
	for _, n := range out.Node.ProjectItems.Nodes {
		if n.Project.ID != p.ProjectID {
			continue
		}
		it := &itemInfo{ID: n.ID, Start: parseDate(n.Start)}
		if n.Status != nil {
			it.Status, it.OptionID = n.Status.Name, n.Status.OptionID
		}
		return it, nil
	}
	return nil, nil
}

const (
	addMutation = `mutation($projectId:ID!,$contentId:ID!){addProjectV2ItemById(input:{projectId:$projectId,contentId:$contentId}){item{id}}}`
	setStatusQ  = `mutation($projectId:ID!,$itemId:ID!,$fieldId:ID!,$optionId:String!){updateProjectV2ItemFieldValue(input:{` +
		`projectId:$projectId,itemId:$itemId,fieldId:$fieldId,value:{singleSelectOptionId:$optionId}}){projectV2Item{id}}}`
	setDateQ = `mutation($projectId:ID!,$itemId:ID!,$fieldId:ID!,$date:Date!){updateProjectV2ItemFieldValue(input:{` +
		`projectId:$projectId,itemId:$itemId,fieldId:$fieldId,value:{date:$date}}){projectV2Item{id}}}`
)

// setProjectStatus agrega el issue al project si no está, pone el Status de la
// fase y sella la fecha de inicio si hace falta. Es idempotente.
func (c *Client) setProjectStatus(ctx context.Context, p *projInfo, is *ghIssue, to flow.Phase, patch tracker.Patch) error {
	_, optID, err := p.optionFor(c, to)
	if err != nil {
		return err
	}
	it, err := c.itemOf(ctx, p, is.NodeID)
	if err != nil {
		return err
	}
	if it == nil {
		var out struct {
			Add struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		}
		if err := c.graphql(ctx, addMutation, map[string]any{"projectId": p.ProjectID, "contentId": is.NodeID}, &out); err != nil {
			return err
		}
		it = &itemInfo{ID: out.Add.Item.ID}
	}
	if it.OptionID != optID {
		err := c.graphql(ctx, setStatusQ, map[string]any{"projectId": p.ProjectID, "itemId": it.ID, "fieldId": p.StatusField, "optionId": optID}, nil)
		if err != nil {
			return err
		}
	}
	if !patch.StampStart.IsZero() && p.StartField != "" && it.Start == nil {
		return c.graphql(ctx, setDateQ, map[string]any{"projectId": p.ProjectID, "itemId": it.ID, "fieldId": p.StartField,
			"date": patch.StampStart.Format("2006-01-02")}, nil)
	}
	return nil
}

const listQuery = `query($id:ID!,$cursor:String,$start:String!){node(id:$id){... on ProjectV2{items(first:100,after:$cursor){nodes{id ` +
	`status:fieldValueByName(name:"Status"){... on ProjectV2ItemFieldSingleSelectValue{name optionId}} ` +
	`start:fieldValueByName(name:$start){... on ProjectV2ItemFieldDateValue{date}} ` +
	`content{... on Issue{number title body state url updatedAt repository{nameWithOwner}}}} pageInfo{hasNextPage endCursor}}}}}`

// listProject lista los issues de este repo que están en el project.
func (c *Client) listProject(ctx context.Context, f tracker.Filter) ([]tracker.Task, error) {
	var out []tracker.Task
	err := c.withProject(ctx, func(p *projInfo) error {
		out = nil
		cursor := ""
		for page := 0; ; page++ {
			if page >= 50 {
				return fmt.Errorf("github: el project %s tiene más de 50 páginas de items", c.Project)
			}
			vars := map[string]any{"id": p.ProjectID, "start": c.StartField}
			if cursor != "" {
				vars["cursor"] = cursor
			}
			var res struct {
				Node *struct {
					Items struct {
						Nodes []struct {
							Status  *statusVal `json:"status"`
							Start   *dateVal   `json:"start"`
							Content struct {
								Number     int       `json:"number"`
								Title      string    `json:"title"`
								Body       string    `json:"body"`
								State      string    `json:"state"`
								URL        string    `json:"url"`
								UpdatedAt  time.Time `json:"updatedAt"`
								Repository struct {
									NameWithOwner string `json:"nameWithOwner"`
								} `json:"repository"`
							} `json:"content"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"items"`
				} `json:"node"`
			}
			if err := c.graphql(ctx, listQuery, vars, &res); err != nil {
				return err
			}
			if res.Node == nil {
				return &gqlError{Type: "NOT_FOUND", Message: "el project ya no existe"}
			}
			for _, n := range res.Node.Items.Nodes {
				k := n.Content
				if k.Number == 0 || !strings.EqualFold(k.Repository.NameWithOwner, c.Repo) || (f.OpenOnly && k.State == "CLOSED") {
					continue
				}
				t := tracker.Task{ID: c.key(k.Number), Title: k.Title, Description: k.Body, URL: k.URL, Updated: k.UpdatedAt,
					Closed: k.State == "CLOSED", Phase: flow.Backlog, Start: parseDate(n.Start)}
				if n.Status != nil && n.Status.Name != "" {
					t.State, t.Phase = n.Status.Name, c.States.PhaseOf(n.Status.Name)
				}
				out = append(out, t)
			}
			if !res.Node.Items.PageInfo.HasNextPage {
				return nil
			}
			cursor = res.Node.Items.PageInfo.EndCursor
		}
	})
	return out, err
}
