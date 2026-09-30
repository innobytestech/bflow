package github

import (
	"context"

	"innobytes.tech/bflow/internal/tracker"
)

func (c *Client) EnsureStates(ctx context.Context, dryRun bool) ([]string, error) {
	return nil, errTodo
}

func (c *Client) StateMap(ctx context.Context) ([]tracker.StateInfo, error) { return nil, errTodo }

func (c *Client) Projects(ctx context.Context) ([]tracker.Project, error) { return nil, errTodo }
