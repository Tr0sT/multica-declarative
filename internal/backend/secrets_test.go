package backend

import (
	"slices"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

func TestWithoutSecretsNeverSendsSecretBearingFlags(t *testing.T) {
	for _, update := range []bool{false, true} {
		in := model.AgentInput{
			WithoutSecrets: true, Name: "Agent", RuntimeID: "runtime", Description: "changed",
			CustomArgs: []string{"synthetic-secret"}, RuntimeConfig: map[string]any{"token": "synthetic-secret"},
			ManageMCPConfig: true, MCPConfigFile: "synthetic-secret", MaxConcurrentTasks: 1,
		}
		args, err := (&CLI{}).agentArgs([]string{"agent", "update"}, in, update)
		if err != nil {
			t.Fatal(err)
		}
		for _, flag := range []string{"--runtime-config", "--custom-args", "--mcp-config-file", "--custom-env-file"} {
			if slices.Contains(args, flag) {
				t.Fatalf("unexpected write flag: %s", flag)
			}
		}
		if strings.Contains(strings.Join(args, " "), "synthetic-secret") || !slices.Contains(args, "changed") {
			t.Fatal("secret handling also lost unrelated settings or leaked a value")
		}
	}
}
