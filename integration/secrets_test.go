//go:build integration

package integration

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func seedSecrets(f *fixture) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.env = map[string]string{"TOKEN": "env-secret-sentinel"}
	f.agent["has_custom_env"] = true
	f.agent["custom_env_key_count"] = 1
	f.agent["mcp_config"] = map[string]any{"mcpServers": map[string]any{"demo": map[string]any{"command": "demo", "env": map[string]any{"TOKEN": "mcp-secret-sentinel"}}}}
	f.agent["runtime_config"] = map[string]any{"gateway": map[string]any{"token": "runtime-secret-sentinel"}, "safe": true}
	f.agent["custom_args"] = []string{"--token=args-secret-sentinel"}
}

func exportWithoutSecrets(t *testing.T, cli *backend.CLI) (model.Project, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "snapshot")
	if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(dir, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Agents[0].PreserveSecrets {
		t.Fatal("export did not persist import policy")
	}
	return p, dir
}

func assertSecretRequestsAbsent(t *testing.T, f *fixture) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.requests {
		if strings.Contains(req.path, "/env") {
			t.Fatalf("secret env endpoint called: %s", req.method)
		}
		for _, field := range []string{"custom_env", "mcp_config", "runtime_config", "custom_args"} {
			if _, exists := req.body[field]; exists {
				t.Fatalf("secret-bearing field sent: %s", field)
			}
		}
	}
}

func TestOfficialCLIWithoutSecretsExportThenEditPreservesRemoteValues(t *testing.T) {
	f, cli := newFixture(t)
	seedSecrets(f)
	p, dir := exportWithoutSecrets(t, cli)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "secret-sentinel") {
			t.Fatalf("secret leaked into %s", path)
		}
		if d.Name() == "custom-env.json" || d.Name() == "mcp.json" {
			t.Fatal("secret file created")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	p.Agents[0].Description = "Changed safely"
	if _, err := (reconcile.Reconciler{Backend: cli}).Plan(p); err != nil {
		t.Fatal(err)
	}
	// Rotate values after the plan: apply must not replay stale/redacted values.
	f.mu.Lock()
	f.env["TOKEN"] = "rotated-env"
	f.agent["runtime_config"] = map[string]any{"gateway": map[string]any{"token": "***"}, "safe": false}
	f.agent["mcp_config"] = nil
	f.agent["mcp_config_redacted"] = true
	f.agent["custom_args"] = []string{"--token=rotated-args"}
	before := map[string]any{"runtime_config": f.agent["runtime_config"], "mcp_config": f.agent["mcp_config"], "custom_args": f.agent["custom_args"]}
	f.mu.Unlock()
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertSecretRequestsAbsent(t, f)
	f.mu.Lock()
	for k, v := range before {
		if !reflect.DeepEqual(f.agent[k], v) {
			t.Errorf("server %s changed", k)
		}
	}
	if f.env["TOKEN"] != "rotated-env" {
		t.Error("server env changed")
	}
	if f.agent["description"] != "Changed safely" {
		t.Error("ordinary field did not update")
	}
	f.mu.Unlock()
	assertNoop(t, cli, p)
	beforeWrites := len(f.mutations())
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != beforeWrites {
		t.Fatal("reapply should be a no-op")
	}
}

func TestOfficialCLIWithoutSecretsOverrideIgnoresFullSnapshotClears(t *testing.T) {
	f, cli := newFixture(t)
	seedSecrets(f)
	full, dir := exportProject(t, cli)
	// These explicitly clear credentials during a normal full import. In the
	// override mode neither file must be opened, even if invalid or missing.
	if err := os.WriteFile(full.Agents[0].CustomEnvFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full.Agents[0].MCPConfigFile, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadWithOptions(filepath.Join(dir, "multica.yaml"), config.Options{WithoutSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	p.Agents[0].Description = "Non-secret edit"
	f.mu.Lock()
	f.requests = nil
	f.mu.Unlock()
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertSecretRequestsAbsent(t, f)
	assertNoop(t, cli, p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.env["TOKEN"] != "env-secret-sentinel" || f.agent["mcp_config"] == nil {
		t.Fatal("override cleared remote secrets")
	}
}

func TestOfficialCLIWithoutSecretsCanCreateWithoutCopyingCredentials(t *testing.T) {
	source, cli := newFixture(t)
	seedSecrets(source)
	p, _ := exportWithoutSecrets(t, cli)
	// Conversation starters cannot be created through stock CLI; unrelated to secrets.
	p.Agents[0].ConversationStarters = nil
	target, targetCLI := newFixture(t)
	target.mu.Lock()
	target.agent = nil
	target.assigned = nil
	target.mu.Unlock()
	if err := (reconcile.Reconciler{Backend: targetCLI}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertSecretRequestsAbsent(t, target)
	assertNoop(t, targetCLI, p)
	if err := (reconcile.Reconciler{Backend: targetCLI}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialCLIWithoutSecretsAcceptsMaskedAndRedactedExport(t *testing.T) {
	f, cli := newFixture(t)
	seedSecrets(f)
	f.mu.Lock()
	f.agent["runtime_config"] = map[string]any{"gateway": map[string]any{"token": "***"}}
	f.agent["mcp_config"] = json.RawMessage(`null`)
	f.agent["mcp_config_redacted"] = true
	f.mu.Unlock()
	p, _ := exportWithoutSecrets(t, cli)
	assertNoop(t, cli, p)
	assertSecretRequestsAbsent(t, f)
}
