package config

import (
	"path/filepath"
	"testing"
)

func TestPreserveSecretsDoesNotReadSecretFiles(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		options     Options
	}{
		{"declaration", "  preserveSecrets: true\n", Options{}},
		{"flag", "", Options{WithoutSecrets: true}},
		{"flagOverridesFalse", "  preserveSecrets: false\n", Options{WithoutSecrets: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n")
			writeFile(t, filepath.Join(root, "agents/a/agent.yaml"), `name: Agent
multica:
  unbound: true
  customEnvFile: missing-env.json
  mcpConfigFile: missing-mcp.json
  runtimeConfig: {gateway: {token: "***"}}
  customArgs: ["--token=do-not-use"]
`+tc.field)
			p, err := LoadWithOptions(filepath.Join(root, "multica.yaml"), tc.options)
			if err != nil {
				t.Fatal(err)
			}
			a := p.Agents[0]
			if !a.PreserveSecrets || a.ManageCustomEnv || a.ManageMCPConfig || a.CustomEnvFile != "" || a.MCPConfigFile != "" || len(a.RuntimeConfig) != 0 || len(a.CustomArgs) != 0 {
				t.Fatal("secret-bearing settings were not made unmanaged")
			}
		})
	}
}

func TestPreserveSecretsDefaultKeepsExplicitClears(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "agent.yaml")
	writeFile(t, file, "name: Agent\nmultica:\n  unbound: true\n  customEnvFile: env.json\n  mcpConfigFile: mcp.json\n")
	writeFile(t, filepath.Join(root, "env.json"), "{}")
	writeFile(t, filepath.Join(root, "mcp.json"), "null")
	a, err := loadAgent(file)
	if err != nil {
		t.Fatal(err)
	}
	if a.PreserveSecrets || !a.ManageCustomEnv || !a.ManageMCPConfig || len(a.CustomEnv) != 0 || string(a.MCPConfig) != "null" {
		t.Fatal("legacy explicit clear semantics changed")
	}
}
