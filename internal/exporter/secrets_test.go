package exporter

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

type noEnvReads struct{ *fakeBackend }

func (noEnvReads) GetAgentEnv(string) (map[string]string, error) {
	panic("secret-free export/plan/apply must not read custom env")
}

func TestWithoutSecretsExportHandlesRedactedFields(t *testing.T) {
	b := exampleBackend()
	b.agents[0].MCPConfigRedacted = true
	b.agents[0].MCPConfig = json.RawMessage(`{"token":"synthetic-mcp-secret"}`)
	b.agents[0].RuntimeConfig = map[string]any{"gateway": map[string]any{"token": "***"}, "other": "synthetic-runtime-secret"}
	b.agents[0].CustomArgs = []string{"--token", "synthetic-arg-secret"}
	out := filepath.Join(t.TempDir(), "snapshot")
	result, err := (Exporter{Backend: noEnvReads{b}}).Export(Options{OutputDir: out, WithoutSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), "unmanaged on import") {
		t.Fatal("missing omission notice")
	}
	if err := filepath.WalkDir(out, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, value := range []string{"synthetic-mcp-secret", "synthetic-runtime-secret", "synthetic-arg-secret", "customEnvFile:", "mcpConfigFile:", "runtimeConfig:", "customArgs:"} {
			if strings.Contains(string(data), value) {
				t.Fatal("secret-bearing field found in exported files")
			}
		}
		if entry.Name() == customEnvFileName || entry.Name() == mcpConfigFileName {
			t.Fatal("export created a secret JSON file")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil || !p.WithoutSecrets {
		t.Fatalf("missing persistent import policy: %v", err)
	}
	changes, err := (reconcile.Reconciler{Backend: noEnvReads{b}}).Plan(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if change.Action != reconcile.Noop {
			t.Fatalf("unexpected drift: %v", change)
		}
	}
	// The fake backend panics on any mutation. Reimport must be a no-op.
	if err := (reconcile.Reconciler{Backend: noEnvReads{b}}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWithoutSecretsForcedRefreshRemovesOldGeneratedSecrets(t *testing.T) {
	b := exampleBackend()
	out := filepath.Join(t.TempDir(), "snapshot")
	export := Exporter{Backend: b}
	if _, err := export.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "agents", "unity-developer", customEnvFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("full export did not contain secret fixture")
	}
	readme := filepath.Join(out, "README.md")
	if err := os.WriteFile(readme, []byte("keep unrelated files"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := export.Export(Options{OutputDir: out, Force: true, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{customEnvFileName, mcpConfigFileName} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), name)); !os.IsNotExist(err) {
			t.Fatal("old generated secret file survived refresh")
		}
	}
	if data, err := os.ReadFile(readme); err != nil || string(data) != "keep unrelated files" {
		t.Fatal("refresh changed unrelated file")
	}
	// Normal export can explicitly replace a sanitized snapshot with a full one.
	if _, err := export.Export(Options{OutputDir: out, Force: true}); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil || p.WithoutSecrets {
		t.Fatalf("normal export unexpectedly omits secrets: %v", err)
	}
}
