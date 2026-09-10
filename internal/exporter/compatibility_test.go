package exporter

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func TestCurrentAgentFieldsRoundTripIncludingUnbound(t *testing.T) {
	b := exampleBackend()
	b.agents[0].ServiceTier = "priority"
	b.agents[0].SystemKey = "mika"
	b.agents[0].ConversationStarters = []model.ConversationStarter{{Label: "Review", Prompt: "Review the current changes."}}
	b.agents[1].RuntimeID = ""
	b.agents[1].ArchivedAt = strptr("2026-09-01T00:00:00Z")
	out := filepath.Join(t.TempDir(), "export")
	if _, err := (Exporter{Backend: b}).Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	project, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range project.Agents {
		if a.ServiceTier == nil || a.ConversationStarters == nil {
			t.Fatalf("clearable fields were omitted: %s", a.Name)
		}
		if a.Name == "Unity Developer" && (*a.ServiceTier != "priority" || a.SystemKey == nil || *a.SystemKey != "mika" || !slices.Equal(*a.ConversationStarters, b.agents[0].ConversationStarters)) {
			t.Fatalf("new fields lost: %#v", a)
		}
		if a.Name == "Reviewer" && (!a.Unbound || a.RuntimeRef != "" || !a.Archived) {
			t.Fatal("unbound/archived state changed")
		}
	}
	changes, err := (reconcile.Reconciler{Backend: b}).Plan(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Action != reconcile.Noop {
			t.Fatalf("unexpected drift: %#v", c)
		}
	}
}

func TestExportOnlyUnboundAgentDoesNotInventRuntime(t *testing.T) {
	b := &fakeBackend{agents: []model.Agent{{ID: "a", Name: "Unbound", MaxConcurrentTasks: 1}}}
	out := filepath.Join(t.TempDir(), "export")
	result, err := (Exporter{Backend: b}).Export(Options{OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if result.Runtimes != 0 || result.Agents != 1 {
		t.Fatalf("%#v", result)
	}
}

func TestRedactedAgentDataDoesNotReplaceExport(t *testing.T) {
	for _, field := range []string{"allowlist", "gateway"} {
		t.Run(field, func(t *testing.T) {
			b := exampleBackend()
			if field == "allowlist" {
				b.agents[0].ComposioToolkitAllowlistRedacted = true
			}
			if field == "gateway" {
				b.agents[0].RuntimeConfig = map[string]any{"gateway": map[string]any{"token": "***"}}
			}
			out := t.TempDir()
			file := filepath.Join(out, "multica.yaml")
			if err := os.WriteFile(file, []byte("keep existing snapshot\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := (Exporter{Backend: b}).Export(Options{OutputDir: out, Force: true})
			if err == nil || (!strings.Contains(err.Error(), "redacted") && !strings.Contains(err.Error(), "masked")) {
				t.Fatalf("expected redaction failure: %v", err)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "keep existing snapshot\n" {
				t.Fatal("failed export modified destination")
			}
		})
	}
}

func strptr(v string) *string { return &v }

type workspaceMCPBackend struct{ *fakeBackend }

func (workspaceMCPBackend) ListWorkspaceMCPServers() ([]model.WorkspaceMCPServer, error) {
	return []model.WorkspaceMCPServer{{ID: "mcp-1", Name: "Fixture", Transport: "http"}}, nil
}
func TestExportWarnsAboutWriteOnlyWorkspaceMCPLibrary(t *testing.T) {
	result, err := (Exporter{Backend: workspaceMCPBackend{exampleBackend()}}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "export")})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "write-only") && strings.Contains(warning, "assignments") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing workspace MCP limitation: %v", result.Warnings)
	}
}
