package backend

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

type runnerCall struct {
	name string
	args []string
}
type fakeRunner struct {
	stdout, stderr []byte
	err            error
	calls          []runnerCall
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	return r.stdout, r.stderr, r.err
}
func TestUpdateAgentPassesClearableFields(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"id":"agent-1","name":"Agent"}`)}
	c := &CLI{Binary: "multica-test", Runner: runner}
	_, err := c.UpdateAgent("agent-1", model.AgentInput{Name: "Agent", RuntimeID: "runtime-1", ManageRuntimeConfig: true, Permission: "private", MaxConcurrentTasks: 1})
	if err != nil {
		t.Fatal(err)
	}
	args := runner.calls[0].args
	for _, v := range []string{"--description", "--instructions", "--model", "--thinking-level", "--custom-args", "--runtime-config"} {
		if !slices.Contains(args, v) {
			t.Fatalf("missing %s: %v", v, args)
		}
	}
}
func TestCreateWorkspaceAgentUsesPublicTarget(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"id":"a","name":"Agent"}`)}
	c := &CLI{Runner: runner}
	_, err := c.CreateAgent(model.AgentInput{Name: "Agent", RuntimeID: "r", Permission: "workspace", MaxConcurrentTasks: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.calls[0].args, "--public-to-workspace") {
		t.Fatal(runner.calls[0].args)
	}
}
func TestCreateAgentUsesMemberTargetsAndFiles(t *testing.T) {
	id := "member-1"
	runner := &fakeRunner{stdout: []byte(`{"id":"a","name":"Agent"}`)}
	c := &CLI{Runner: runner}
	_, err := c.CreateAgent(model.AgentInput{Name: "Agent", RuntimeID: "r", ManageRuntimeConfig: true, RuntimeConfig: map[string]any{"x": true}, PermissionMode: "public_to", InvocationTargets: []model.InvocationTarget{{TargetType: "member", TargetID: &id}}, ManageMCPConfig: true, MCPConfigFile: "mcp.json", MaxConcurrentTasks: 1})
	if err != nil {
		t.Fatal(err)
	}
	args := runner.calls[0].args
	for _, v := range []string{"--runtime-config", "--public-to-member", "member-1", "--mcp-config-file", "mcp.json"} {
		if !slices.Contains(args, v) {
			t.Fatalf("missing %s: %v", v, args)
		}
	}
}
func TestListAgentsIncludesArchived(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`[]`)}
	c := &CLI{Runner: runner}
	if _, err := c.ListAgents(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.calls[0].args, "--include-archived") {
		t.Fatal(runner.calls[0].args)
	}
}
func TestSquadCommands(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"id":"x"}`)}
	c := &CLI{Runner: runner}
	if _, err := c.CreateSquad(model.SquadInput{Name: "Team", LeaderID: "agent-1"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.calls[0].args, " "); !strings.Contains(got, "squad create") || !strings.Contains(got, "--leader agent-1") {
		t.Fatal(got)
	}
}
func TestRunnerErrorIncludesStderr(t *testing.T) {
	runner := &fakeRunner{stderr: []byte("authentication failed"), err: errors.New("exit")}
	c := &CLI{Runner: runner}
	_, err := c.ListSkills()
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatal(err)
	}
}
