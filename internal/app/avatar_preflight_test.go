package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

type avatarWorkspaceState struct {
	*workspaceMemoryBackend
}

func (b *avatarWorkspaceState) ListRuntimes() ([]model.Runtime, error) {
	return []model.Runtime{{ID: "runtime-b"}}, nil
}

type unsupportedAvatarWorkspace struct {
	*avatarWorkspaceState
	checks int
}

func (b *unsupportedAvatarWorkspace) CheckAgentAvatarURL() error {
	b.checks++
	return errors.New("agent update --avatar-url is missing")
}

func (b *unsupportedAvatarWorkspace) SetAgentAvatarURL(string, string) error {
	b.writes++
	return errors.New("unexpected avatar mutation")
}

func TestWorkspaceSetAvatarPreflightPreventsEarlierWorkspaceWrites(t *testing.T) {
	for _, missingInterface := range []bool{false, true} {
		name := "unsupported-cli"
		if missingInterface {
			name = "missing-backend-interface"
		}
		t.Run(name, func(t *testing.T) {
			_, set := appWorkspaceSet(t)
			avatar := "emoji:🐝"
			set.Entries[1].Project.RuntimeSelectors = map[string]model.RuntimeSelector{
				"runtime": {ID: "runtime-b"},
			}
			set.Entries[1].Project.Agents = []model.AgentSpec{{
				Name: "B", RuntimeRef: "runtime", RuntimeConfig: map[string]any{},
				MaxConcurrentTasks: 1, PermissionMode: "private", AvatarURL: &avatar,
			}}
			a := &workspaceMemoryBackend{id: "a"}
			b := &unsupportedAvatarWorkspace{avatarWorkspaceState: &avatarWorkspaceState{
				workspaceMemoryBackend: &workspaceMemoryBackend{id: "b"},
			}}
			factory := func(id string) backend.Backend {
				if id == "ws-a" {
					return a
				}
				if missingInterface {
					return b.avatarWorkspaceState
				}
				return b
			}
			catalog := appWorkspaceCatalog{{ID: "ws-a"}, {ID: "ws-b"}}
			var out, errout bytes.Buffer
			// A stock CLI must still be able to inspect and display avatar drift.
			if err := runWorkspaceSet("plan", set, catalog, factory, &out, &errout); err != nil {
				t.Fatal(err)
			}
			if b.checks != 0 || a.writes+b.writes != 0 {
				t.Fatal("plan checked write support or mutated a workspace")
			}
			err := runWorkspaceSet("apply", set, catalog, factory, &out, &errout)
			if err == nil || !strings.Contains(err.Error(), "workspace \"b\" preflight") || !strings.Contains(err.Error(), "no changes applied") {
				t.Fatalf("expected attributed preflight failure, got %v", err)
			}
			if a.writes+b.writes != 0 {
				t.Fatalf("avatar failure left partial writes: a=%d b=%d", a.writes, b.writes)
			}
			if !missingInterface && b.checks != 1 {
				t.Fatalf("expected one capability probe, got %d", b.checks)
			}
		})
	}
}

func TestWorkspaceSetApplyWithoutAvatarWritesNeedsNoAvatarCapability(t *testing.T) {
	_, set := appWorkspaceSet(t)
	a := &workspaceMemoryBackend{id: "a"}
	b := &unsupportedAvatarWorkspace{avatarWorkspaceState: &avatarWorkspaceState{
		workspaceMemoryBackend: &workspaceMemoryBackend{id: "b"},
	}}
	factory := func(id string) backend.Backend {
		if id == "ws-a" {
			return a
		}
		return b
	}
	var out, errout bytes.Buffer
	catalog := appWorkspaceCatalog{{ID: "ws-a"}, {ID: "ws-b"}}
	if err := runWorkspaceSet("apply", set, catalog, factory, &out, &errout); err != nil {
		t.Fatal(err)
	}
	if b.checks != 0 || a.writes != 1 || b.writes != 1 {
		t.Fatalf("checks=%d writes=%d/%d", b.checks, a.writes, b.writes)
	}
}
