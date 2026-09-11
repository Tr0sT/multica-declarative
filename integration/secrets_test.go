//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func setFixtureSecrets(f *fixture, suffix string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.env = map[string]string{"TOKEN": "synthetic-env-" + suffix, "UNRELATED": "keep-this-too"}
	f.agent["has_custom_env"], f.agent["custom_env_key_count"] = true, len(f.env)
	f.agent["runtime_config"] = map[string]any{"gateway": map[string]any{"token": "synthetic-gateway-" + suffix}, "safe-setting": true}
	f.agent["custom_args"] = []string{"--token", "synthetic-arg-" + suffix}
	f.agent["mcp_config"] = map[string]any{"mcpServers": map[string]any{"fixture": map[string]any{"command": "fixture", "env": map[string]any{"TOKEN": "synthetic-mcp-" + suffix}}}}
}

func fixtureSecretState(t *testing.T, f *fixture) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	encoded, err := json.Marshal([]any{f.env, f.agent["mcp_config"], f.agent["runtime_config"], f.agent["custom_args"]})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertNoSecretAccess(t *testing.T, f *fixture) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.requests {
		if strings.HasSuffix(req.path, "/env") {
			t.Fatal("secret-free operation accessed the custom environment endpoint")
		}
		for _, key := range []string{"custom_env", "mcp_config", "runtime_config", "custom_args"} {
			if _, present := req.body[key]; present {
				t.Fatalf("secret-free operation sent %s", key)
			}
		}
	}
}

func TestOfficialCLIWithoutSecretsImportPreservesExistingValues(t *testing.T) {
	for _, withoutExport := range []bool{false, true} {
		t.Run(map[bool]string{true: "sanitized-manifest", false: "full-snapshot-with-override"}[withoutExport], func(t *testing.T) {
			f, cli := newFixture(t)
			setFixtureSecrets(f, "source")
			out := filepath.Join(t.TempDir(), "snapshot")
			if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: out, WithoutSecrets: withoutExport}); err != nil {
				t.Fatal(err)
			}
			if withoutExport {
				assertNoSecretAccess(t, f)
			} else {
				// A full snapshot can be imported without copying its secret files.
				for _, name := range []string{"custom-env.json", "mcp.json"} {
					if err := os.Remove(filepath.Join(out, "agents", "builder", name)); err != nil {
						t.Fatal(err)
					}
				}
			}
			p, err := config.LoadWithOptions(filepath.Join(out, "multica.yaml"), config.LoadOptions{WithoutSecrets: !withoutExport})
			if err != nil {
				t.Fatal(err)
			}
			setFixtureSecrets(f, "destination")
			f.mu.Lock()
			f.agent["mcp_config_redacted"] = true
			f.agent["runtime_config"].(map[string]any)["gateway"].(map[string]any)["token"] = "***"
			f.requests = nil
			f.mu.Unlock()
			before := fixtureSecretState(t, f)
			assertNoop(t, cli, p) // Different/redacted secrets are not drift.
			p.Agents[0].Description = "Non-secret description updated"
			if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			assertNoop(t, cli, p)
			if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != 1 {
				t.Fatal("expected only the description update, with a no-op second import")
			}
			assertNoSecretAccess(t, f)
			if fixtureSecretState(t, f) != before {
				t.Fatal("import changed existing credentials or secret-bearing settings")
			}
		})
	}
}

func TestOfficialCLIWithoutSecretsCreatesWithoutCredentials(t *testing.T) {
	f, cli := newFixture(t)
	setFixtureSecrets(f, "source")
	f.mu.Lock()
	// The official CLI cannot recreate conversation starters in this baseline.
	f.agent["conversation_starters"] = []any{}
	f.mu.Unlock()
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: out, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.agent, f.skill, f.files, f.assigned, f.env, f.requests = nil, nil, nil, nil, nil, nil
	f.mu.Unlock()
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	writes := len(f.mutations())
	if writes == 0 {
		t.Fatal("nothing was created")
	}
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != writes {
		t.Fatal("second import was not idempotent")
	}
	assertNoSecretAccess(t, f)
}

func TestOfficialCLIFullSnapshotClearsAreIgnoredWithWithoutSecrets(t *testing.T) {
	f, cli := newFixture(t)
	setFixtureSecrets(f, "preserve")
	_, out := exportProject(t, cli)
	for name, value := range map[string]string{"custom-env.json": "{}", "mcp.json": "null"} {
		if err := os.WriteFile(filepath.Join(out, "agents", "builder", name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := config.LoadWithOptions(filepath.Join(out, "multica.yaml"), config.LoadOptions{WithoutSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	before := fixtureSecretState(t, f)
	f.mu.Lock()
	f.requests = nil
	f.mu.Unlock()
	p.Agents[0].Description = "Still update non-secret configuration"
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoSecretAccess(t, f)
	if fixtureSecretState(t, f) != before {
		t.Fatal("explicit empty secret files overrode preservation mode")
	}
}

func TestOfficialCLIWithoutSecretsCommandRoundTrip(t *testing.T) {
	f, cli := newFixture(t)
	setFixtureSecrets(f, "command")
	binary := filepath.Join(t.TempDir(), "multica-declarative")
	build := exec.Command("go", "build", "-o", binary, "../cmd/multica-declarative")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command: %v: %s", err, output)
	}
	out := filepath.Join(t.TempDir(), "snapshot")
	run := func(args ...string) {
		t.Helper()
		args = append(args, "--multica-bin", cli.Binary)
		stdout, stderr, err := cli.Runner.Run(context.Background(), binary, args...)
		if err != nil {
			t.Fatalf("command failed: %v: %s", err, stderr)
		}
		for _, value := range []string{"synthetic-env-command", "synthetic-mcp-command", "synthetic-arg-command", "synthetic-gateway-command"} {
			if strings.Contains(string(stdout)+string(stderr), value) {
				t.Fatal("command printed an excluded credential")
			}
		}
	}
	run("export", "--without-secrets", "--output-dir", out)
	manifest := filepath.Join(out, "multica.yaml")
	run("validate", "--config", manifest)
	// Update an ordinary field in files, then import without repeating the flag.
	if err := os.WriteFile(filepath.Join(out, "agents", "builder", "AGENT.md"), []byte("Updated instructions."), 0644); err != nil {
		t.Fatal(err)
	}
	before := fixtureSecretState(t, f)
	run("plan", "--config", manifest)
	run("apply", "--config", manifest)
	run("apply", "--config", manifest)
	assertNoSecretAccess(t, f)
	if len(f.mutations()) != 1 || fixtureSecretState(t, f) != before {
		t.Fatal("command round-trip did not preserve credentials or converge")
	}
}
