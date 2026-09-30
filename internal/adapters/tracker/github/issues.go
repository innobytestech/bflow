package github

import (
	"context"

	"innobytes.tech/bflow/internal/flow"
	"innobytes.tech/bflow/internal/tracker"
)

func (c *Client) Get(ctx context.Context, id string) (tracker.Task, error) {
	return tracker.Task{}, errTodo
}

func (c *Client) List(ctx context.Context, f tracker.Filter) ([]tracker.Task, error) {
	return nil, errTodo
}

func (c *Client) Transition(ctx context.Context, id string, to flow.Phase, p tracker.Patch) error {
	return errTodo
}

func (c *Client) Comment(ctx context.Context, id string, markdown string) error { return errTodo }

func (c *Client) Comments(ctx context.Context, id string) ([]tracker.Comment, error) {
	return nil, errTodo
}

func (c *Client) Create(ctx context.Context, title, description string) (tracker.Task, error) {
	return tracker.Task{}, errTodo
}
