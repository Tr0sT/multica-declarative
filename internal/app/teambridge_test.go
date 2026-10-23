package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

func TestTeamBridgeFailurePreflightsWholeWorkspaceSet(t *testing.T) {
	_, set := appWorkspaceSet(t)
	// The first workspace needs a skill create. The second explicitly declares
	// an integration its backend cannot handle. Nothing may be created first.
	set.Entries[1].Project.TeamBridge = []model.TeamBridgeBotSpec{{Name: "Review bot"}}
	a, b := &workspaceMemoryBackend{id: "a"}, &workspaceMemoryBackend{id: "b"}
	factory := func(id string) backend.Backend {
		if id == "ws-a" {
			return a
		}
		return b
	}
	var out, errout bytes.Buffer
	err := runWorkspaceSet("apply", set, appWorkspaceCatalog{{ID: "ws-a"}, {ID: "ws-b"}}, factory, &out, &errout)
	if err == nil || !strings.Contains(err.Error(), "TeamBridge") || a.writes+b.writes != 0 {
		t.Fatalf("error=%v writes=%d", err, a.writes+b.writes)
	}
}

func TestTeamBridgeExportFlagIsExplicit(t *testing.T) {
	var out, errout bytes.Buffer
	if code := Run([]string{"plan", "--with-teambridge"}, &out, &errout); code != 2 || !strings.Contains(errout.String(), "only used with export") {
		t.Fatalf("code=%d error=%s", code, errout.String())
	}
}

type tokenPreflightWorkspaceBackend struct {
	*workspaceMemoryBackend
	backend.TeamBridgeOperations
}

func (*tokenPreflightWorkspaceBackend) ListTeamBridgeInstallations() ([]model.TeamBridgeInstallation, error) {
	return nil, nil
}

func (*tokenPreflightWorkspaceBackend) ListAgents() ([]model.Agent, error) {
	return []model.Agent{{ID: "agent-b", Name: "Review"}}, nil
}

func (*tokenPreflightWorkspaceBackend) GetAgent(string) (model.Agent, error) {
	return model.Agent{ID: "agent-b", Name: "Review", PermissionMode: "private", MaxConcurrentTasks: 1}, nil
}

func (*tokenPreflightWorkspaceBackend) ListAgentSkills(string) ([]model.SkillSummary, error) {
	return nil, nil
}

func TestMissingTeamBridgeTokenPreflightsAllWorkspacesBeforeFirstWrite(t *testing.T) {
	for _, omit := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-file", true: "omit-create"}[omit], func(t *testing.T) {
			_, set := appWorkspaceSet(t)
			p := &set.Entries[1].Project
			p.WithoutSecrets = omit
			p.Agents = []model.AgentSpec{{Name: "Review", Unbound: true, PermissionMode: "private", MaxConcurrentTasks: 1}}
			p.TeamBridge = []model.TeamBridgeBotSpec{{Name: "Review", Agent: "Review", ServerURL: "https://chat.example.test", Status: "active", AccessMode: "members", Context: model.TeamBridgeContext{Mode: "message"}, DeniedReply: model.TeamBridgeReplySpec{Mode: "text"}, SourceDirectory: t.TempDir(), AccessTokenFile: "missing-token"}}
			a := &workspaceMemoryBackend{id: "a"}
			b := &tokenPreflightWorkspaceBackend{workspaceMemoryBackend: &workspaceMemoryBackend{id: "b"}}
			factory := func(id string) backend.Backend {
				if id == "ws-a" {
					return a
				}
				return b
			}
			var out, errout bytes.Buffer
			err := runWorkspaceSet("apply", set, appWorkspaceCatalog{{ID: "ws-a"}, {ID: "ws-b"}}, factory, &out, &errout)
			if err == nil || !strings.Contains(err.Error(), "TeamBridge") || !strings.Contains(strings.ToLower(err.Error()), "token") || a.writes+b.writes != 0 {
				t.Fatalf("token failure did not preflight all workspaces: error=%v writes=%d", err, a.writes+b.writes)
			}
		})
	}
}
