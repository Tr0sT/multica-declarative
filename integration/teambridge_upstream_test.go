//go:build integration

package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func TestOfficialCLITeamBridgeUnavailableIsExplicitAndReadOnly(t *testing.T) {
	f, cli := newFixture(t)
	project, dir := exportProject(t, cli)
	assertNoop(t, cli, project)
	if err := (reconcile.Reconciler{Backend: cli}).Apply(project, nil); err != nil {
		t.Fatal(err)
	}
	if len(project.TeamBridge) != 0 {
		t.Fatal("ordinary export unexpectedly enabled TeamBridge")
	}
	manifest := filepath.Join(dir, "multica.yaml")
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (exporter.Exporter{Backend: cli}).Export(exporter.Options{
		OutputDir: dir, Force: true, WithTeamBridge: true,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "teambridge") {
		t.Fatalf("expected an explicit unsupported TeamBridge CLI error, got %v", err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("unsupported optional export replaced the previous snapshot")
	}
	if len(f.mutations()) != 0 {
		t.Fatal("default commands or unsupported optional export mutated Multica")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.requests {
		if strings.Contains(call.path, "/mattermost/") {
			t.Fatalf("unsupported CLI attempted an integration API request: %s", call.path)
		}
	}
}
