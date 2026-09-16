package backend

import (
	"context"
	"fmt"
)

// Workspace is the identity returned by the official workspace list command.
type Workspace struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type WorkspaceLister interface {
	ListWorkspaces() ([]Workspace, error)
}

func (c *CLI) ListWorkspaces() ([]Workspace, error) {
	var result []Workspace
	if err := c.runJSON(&result, "workspace", "list", "--output", "json"); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("workspace list returned null instead of an array")
	}
	return result, nil
}

// WithScope returns an independent CLI. It never changes the user's active
// profile/workspace, mutates the process environment, or starts/stops a daemon.
// Explicit flags override any inherited MULTICA_WORKSPACE_ID or profile default.
func (c *CLI) WithScope(profile, workspaceID string) *CLI {
	copy := *c
	runner := c.Runner
	if previous, ok := runner.(scopeRunner); ok {
		runner = previous.Runner
	}
	copy.Runner = scopeRunner{Runner: runner, profile: profile, workspaceID: workspaceID}
	return &copy
}

type scopeRunner struct {
	Runner
	profile, workspaceID string
}

func (r scopeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	prefix := make([]string, 0, len(args)+4)
	if r.profile != "" {
		prefix = append(prefix, "--profile", r.profile)
	}
	if r.workspaceID != "" {
		prefix = append(prefix, "--workspace-id", r.workspaceID)
	}
	return r.Runner.Run(ctx, name, append(prefix, args...)...)
}
