package backend

import (
	"github.com/Tr0sT/multica-declarative/internal/model"
	"slices"
	"testing"
)

func TestPreserveSecretsOmitsFlagsEvenWithPopulatedInput(t *testing.T) {
	for _, update := range []bool{false, true} {
		runner := &fakeRunner{stdout: []byte(`{"id":"agent","name":"Agent"}`)}
		c := &CLI{Runner: runner}
		in := model.AgentInput{PreserveSecrets: true, Name: "Agent", RuntimeID: "runtime", Description: "New description", MaxConcurrentTasks: 1,
			CustomArgs: []string{"--token=synthetic"}, RuntimeConfig: map[string]any{"token": "synthetic"}, ManageMCPConfig: true, MCPConfigFile: "must-not-read.json"}
		var err error
		if update {
			_, err = c.UpdateAgent("agent", in)
		} else {
			_, err = c.CreateAgent(in)
		}
		if err != nil {
			t.Fatal(err)
		}
		args := runner.calls[0].args
		for _, flag := range []string{"--runtime-config", "--custom-args", "--mcp-config-file", "--custom-env-file"} {
			if slices.Contains(args, flag) {
				t.Fatalf("update=%v: secret-bearing flag %s was sent", update, flag)
			}
		}
		if !slices.Contains(args, "--description") {
			t.Fatal("ordinary settings must remain writable")
		}
	}
}
