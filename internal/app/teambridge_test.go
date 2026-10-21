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
