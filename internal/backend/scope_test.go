package backend

import (
	"context"
	"reflect"
	"testing"
)

type workspaceRecordingRunner struct {
	calls [][]string
	body  string
}

func (r *workspaceRecordingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	return []byte(r.body), nil, nil
}

func TestWorkspaceScopesAreIndependentAndExplicit(t *testing.T) {
	r := &workspaceRecordingRunner{body: "[]"}
	base := NewCLI("multica")
	base.Runner = r
	a, b := base.WithScope("production", "workspace-a"), base.WithScope("production", "workspace-b")
	if _, err := a.ListSkills(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ListAgents(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListRuntimes(); err != nil {
		t.Fatal(err)
	}
	if _, err := base.ListWorkspaces(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.WithScope("staging", "workspace-c").ListSkills(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"--profile", "production", "--workspace-id", "workspace-a", "skill", "list", "--output", "json"},
		{"--profile", "production", "--workspace-id", "workspace-b", "agent", "list", "--include-archived", "--output", "json"},
		{"--profile", "production", "--workspace-id", "workspace-a", "runtime", "list", "--output", "json"},
		{"workspace", "list", "--output", "json"},
		{"--profile", "staging", "--workspace-id", "workspace-c", "skill", "list", "--output", "json"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("calls = %#v, want %#v", r.calls, want)
	}
	if base.Runner != r {
		t.Fatal("scoping mutated original CLI")
	}
}

func TestWorkspaceListRejectsNullAndDecodesIdentities(t *testing.T) {
	r := &workspaceRecordingRunner{body: "null"}
	cli := NewCLI("multica")
	cli.Runner = r
	if _, err := cli.ListWorkspaces(); err == nil {
		t.Fatal("null must not be accepted as an empty successful snapshot")
	}
	r.body = `[{"id":"ws-1","slug":"hustlecastle","name":"Hustle Castle","unrelated":true}]`
	values, err := cli.ListWorkspaces()
	if err != nil || len(values) != 1 || values[0].ID != "ws-1" || values[0].Slug != "hustlecastle" {
		t.Fatalf("values=%#v err=%v", values, err)
	}
}
