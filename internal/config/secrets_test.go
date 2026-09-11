package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithoutSecretsPolicyAndOverride(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		without      bool
	}{
		{"manifest", "omit", false},
		{"flag", "", true},
		{"flag restricts include", "include", true},
		{"false cannot override manifest", "omit", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			manifest := "apiVersion: multica-declarative/v1alpha1\n"
			if tc.policy != "" {
				manifest += "secrets: " + tc.policy + "\n"
			}
			if err := os.WriteFile(filepath.Join(root, "multica.yaml"), []byte(manifest), 0600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "agents", "builder")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			// Neither JSON file exists. Omission must happen before reading files.
			declaration := "name: Builder\ndescription: Keep this\nmultica:\n  unbound: true\n  customEnvFile: missing-env.json\n  mcpConfigFile: missing-mcp.json\n  customArgs: [--token, synthetic-arg-secret]\n  runtimeConfig:\n    arbitrary: synthetic-runtime-secret\n"
			if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(declaration), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := LoadWithOptions(filepath.Join(root, "multica.yaml"), LoadOptions{WithoutSecrets: tc.without})
			if err != nil {
				t.Fatal(err)
			}
			a := p.Agents[0]
			if !p.WithoutSecrets || !a.WithoutSecrets || a.ManageCustomEnv || a.ManageMCPConfig || a.CustomEnvFile != "" || a.MCPConfigFile != "" || len(a.RuntimeConfig) != 0 || len(a.CustomArgs) != 0 || a.Description != "Keep this" {
				t.Fatal("omitted fields became managed or other settings were lost")
			}
		})
	}
}

func TestUnknownSecretsPolicyFailsEvenWithOverride(t *testing.T) {
	for _, without := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "multica.yaml")
		if err := os.WriteFile(path, []byte("apiVersion: multica-declarative/v1alpha1\nsecrets: omti\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadWithOptions(path, LoadOptions{WithoutSecrets: without}); err == nil {
			t.Fatal("invalid policy silently accepted")
		}
	}
}
