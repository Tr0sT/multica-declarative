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
	stdout []byte
	stderr []byte
	err    error
	calls  []runnerCall
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	return r.stdout, r.stderr, r.err
}

func TestUpdateAgentPassesClearableFields(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{stdout: []byte(`{"id":"agent-1","name":"Agent"}`)}
	client := &CLI{Binary: "multica-test", Runner: runner}

	_, err := client.UpdateAgent("agent-1", model.AgentInput{
		Name:               "Agent",
		RuntimeID:          "runtime-1",
		Permission:         "private",
		MaxConcurrentTasks: 1,
	})
	if err != nil {
		t.Fatalf("UpdateAgent returned error: %v", err)
	}
	args := runner.calls[0].args
	for _, expected := range []string{"--description", "--instructions", "--model", "--thinking-level", "--custom-args"} {
		if !slices.Contains(args, expected) {
			t.Fatalf("arguments do not contain %s: %v", expected, args)
		}
	}
}

func TestCreateWorkspaceAgentUsesPublicTarget(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{stdout: []byte(`{"id":"agent-1","name":"Agent"}`)}
	client := &CLI{Binary: "multica-test", Runner: runner}

	_, err := client.CreateAgent(model.AgentInput{
		Name:               "Agent",
		RuntimeID:          "runtime-1",
		Permission:         "workspace",
		MaxConcurrentTasks: 2,
	})
	if err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}
	args := runner.calls[0].args
	if !slices.Contains(args, "--public-to-workspace") {
		t.Fatalf("arguments do not contain workspace target: %v", args)
	}
}

func TestRunnerErrorIncludesStderr(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{stderr: []byte("authentication failed"), err: errors.New("exit status 1")}
	client := &CLI{Binary: "multica-test", Runner: runner}

	_, err := client.ListSkills()
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
