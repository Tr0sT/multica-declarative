package exporter

import (
	"encoding/json"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type noEnvironmentReads struct{ *fakeBackend }

func (noEnvironmentReads) GetAgentEnv(string) (map[string]string, error) {
	panic("secret environment must not be read")
}

func TestExportWithoutSecretsAndForceRefresh(t *testing.T) {
	b := exampleBackend()
	b.agents[0].RuntimeConfig = map[string]any{"token": "runtime-synthetic-secret"}
	b.agents[0].CustomArgs = []string{"--token=args-synthetic-secret"}
	out := filepath.Join(t.TempDir(), "export")
	e := Exporter{Backend: b}
	if _, err := e.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	// Put an existing resource in a grouping directory and retain unrelated files.
	if err := os.MkdirAll(filepath.Join(out, "agents/group"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(out, "agents/unity-developer"), filepath.Join(out, "agents/group/unity-developer")); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(out, "README.md")
	if err := os.WriteFile(readme, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	e.Backend = noEnvironmentReads{b}
	result, err := e.Export(Options{OutputDir: out, Force: true, WithoutSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected secret omission notice")
	}
	err = filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == customEnvFileName || d.Name() == mcpConfigFileName {
			t.Fatal("stale secret file survived forced refresh")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, s := range []string{"env-secret", "mcp-secret", "runtime-synthetic-secret", "args-synthetic-secret", "customEnvFile:", "mcpConfigFile:", "runtimeConfig:", "customArgs:"} {
			if strings.Contains(string(data), s) {
				t.Fatalf("excluded field or credential leaked in %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(readme); err != nil || string(data) != "keep me" {
		t.Fatal("unrelated file changed")
	}
	if _, err := os.Stat(filepath.Join(out, "agents/group/unity-developer/agent.yaml")); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Agents {
		if !a.PreserveSecrets || a.ManageCustomEnv || a.ManageMCPConfig {
			t.Fatal("preservation policy lost")
		}
	}
	changes, err := (reconcile.Reconciler{Backend: e.Backend}).Plan(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Action != reconcile.Noop {
			t.Fatalf("secret omission caused false drift: %#v", c)
		}
	}
}

func TestExportWithoutSecretsAcceptsRedactedValues(t *testing.T) {
	b := exampleBackend()
	b.agents[0].RuntimeConfig = map[string]any{"gateway": map[string]any{"token": "***"}}
	b.agents[0].MCPConfigRedacted = true
	b.agents[0].MCPConfig = json.RawMessage(`null`)
	e := Exporter{Backend: noEnvironmentReads{b}}
	if _, err := e.Export(Options{OutputDir: filepath.Join(t.TempDir(), "out"), WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	// The full export continues failing rather than saving redacted placeholders.
	if _, err := (Exporter{Backend: b}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "out")}); err == nil {
		t.Fatal("full export silently accepted masked secrets")
	}
}
