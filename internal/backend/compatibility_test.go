package backend

import (
	"slices"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

func TestGetSkillRequestsAndPreservesContent(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"id":"s","name":"example","content":"Body.  \n\n","files":[{"id":"f","path":"references/details.md","content":"проверка\n"}]}`)}
	skill, err := (&CLI{Runner: runner}).GetSkill("s")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(runner.calls[0].args, []string{"skill", "get", "s", "--with-content", "--output", "json"}) {
		t.Fatal(runner.calls)
	}
	if skill.Content != "Body.  \n\n" || len(skill.Files) != 1 || skill.Files[0].Content != "проверка\n" {
		t.Fatalf("content was changed: %#v", skill)
	}
}

func TestGetSkillRejectsIncompleteContent(t *testing.T) {
	for _, output := range []string{
		`{"id":"s","files":[]}`,
		`{"id":"s","content":null,"files":[]}`,
		`{"id":"s","content":"body"}`,
		`{"id":"s","content":"body","files":null}`,
		`{"id":"s","content":"body","files":[{"id":"f","path":"readme"}]}`,
		`{"id":"s","content":"body","files":[{"id":"f","content":null}]}`,
	} {
		t.Run(output, func(t *testing.T) {
			_, err := (&CLI{Runner: &fakeRunner{stdout: []byte(output)}}).GetSkill("s")
			if err == nil || !strings.Contains(err.Error(), "--with-content") {
				t.Fatalf("expected incomplete content error: %v", err)
			}
		})
	}
}

func TestGetSkillAllowsExplicitEmptyContent(t *testing.T) {
	_, err := (&CLI{Runner: &fakeRunner{stdout: []byte(`{"id":"s","content":"","files":[]}`)}}).GetSkill("s")
	if err != nil {
		t.Fatal(err)
	}
}

func TestServiceTierTriState(t *testing.T) {
	for _, create := range []bool{false, true} {
		for _, tier := range []*string{nil, ptr(""), ptr("default"), ptr("priority"), ptr("future-tier")} {
			runner := &fakeRunner{stdout: []byte(`{"id":"a","service_tier":"priority"}`)}
			cli := &CLI{Runner: runner}
			input := model.AgentInput{Name: "Agent", RuntimeID: "r", ServiceTier: tier, MaxConcurrentTasks: 1}
			var err error
			if create {
				_, err = cli.CreateAgent(input)
			} else {
				_, err = cli.UpdateAgent("a", input)
			}
			if err != nil {
				t.Fatal(err)
			}
			args := runner.calls[0].args
			index := slices.Index(args, "--service-tier")
			if tier == nil && index != -1 {
				t.Fatal("omitted tier must remain unmanaged")
			}
			if tier != nil && (index < 0 || args[index+1] != *tier) {
				t.Fatalf("tier flag not preserved: %v", args)
			}
		}
	}
}

func TestUpdatingUnboundAgentOmitsRuntimeFlag(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"id":"a","runtime_id":""}`)}
	_, err := (&CLI{Runner: runner}).UpdateAgent("a", model.AgentInput{Name: "Unbound", MaxConcurrentTasks: 1})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(runner.calls[0].args, "--runtime-id") {
		t.Fatal("must not submit an empty runtime UUID")
	}
}

func ptr(s string) *string { return &s }
