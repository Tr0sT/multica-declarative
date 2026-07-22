package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), `apiVersion: multica-declarative/v1alpha1
runtimes:
  desktop:
    name: desktop
    provider: codex
`)
	writeFile(t, filepath.Join(root, "skills/game-engines/unity/SKILL.md"), `---
name: unity-development
description: Unity conventions
metadata:
  version: "1"
---

# Unity
`)
	writeFile(t, filepath.Join(root, "skills/game-engines/unity/references/testing.md"), "# Tests\n")
	writeFile(t, filepath.Join(root, "agents/codex/unity/instructions.md"), "Do the work.\n")
	writeFile(t, filepath.Join(root, "agents/codex/unity/agent.yaml"), `name: Unity Developer
instructionsFile: instructions.md
skills: [unity-development]
multica:
  runtime: desktop
  maxConcurrentTasks: 1
  permission: private
`)

	project, err := Load(filepath.Join(root, "multica.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := project.Skills[0].Name; got != "unity-development" {
		t.Fatalf("skill name = %q", got)
	}
	if got := project.Skills[0].Files[0].Path; got != "references/testing.md" {
		t.Fatalf("skill file path = %q", got)
	}
	if got := project.Agents[0].Instructions; got != "Do the work.\n" {
		t.Fatalf("instructions = %q", got)
	}
}

func TestRejectsUnknownAgentSkill(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), `apiVersion: multica-declarative/v1alpha1
runtimes:
  desktop:
    name: desktop
`)
	writeFile(t, filepath.Join(root, "agents/codex/agent/agent.yaml"), `name: Agent
skills: [missing]
multica:
  runtime: desktop
`)

	_, err := Load(filepath.Join(root, "multica.yaml"))
	if err == nil || !strings.Contains(err.Error(), "undeclared skill") {
		t.Fatalf("expected undeclared skill error, got %v", err)
	}
}

func TestRejectsUnknownWorkspaceField(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), `apiVersion: multica-declarative/v1alpha1
unexpected: true
`)
	writeFile(t, filepath.Join(root, "skills/example/SKILL.md"), `---
name: example
description: Example
---
`)

	_, err := Load(filepath.Join(root, "multica.yaml"))
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("expected strict YAML error, got %v", err)
	}
}

func TestRejectsRemovedRuntimeProfilesField(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), `apiVersion: multica-declarative/v1alpha1
runtimeProfiles: []
`)
	writeFile(t, filepath.Join(root, "skills/example/SKILL.md"), `---
name: example
description: Example
---
`)

	_, err := Load(filepath.Join(root, "multica.yaml"))
	if err == nil || !strings.Contains(err.Error(), "field runtimeProfiles not found") {
		t.Fatalf("expected removed runtimeProfiles field error, got %v", err)
	}
}

func TestRejectsAgentKindField(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), `apiVersion: multica-declarative/v1alpha1
runtimes:
  desktop:
    name: desktop
`)
	writeFile(t, filepath.Join(root, "agents/codex/agent/agent.yaml"), `kind: Prompt
name: Agent
multica:
  runtime: desktop
`)

	_, err := Load(filepath.Join(root, "multica.yaml"))
	if err == nil || !strings.Contains(err.Error(), "field kind not found") {
		t.Fatalf("expected strict agent kind error, got %v", err)
	}
}

func TestRejectsRemovedWorkspaceFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "kind", field: "kind", value: "Workspace"},
		{name: "skills", field: "skills", value: "[]"},
		{name: "agents", field: "agents", value: "[]"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			manifest := "apiVersion: multica-declarative/v1alpha1\n" + test.field + ": " + test.value + "\n"
			writeFile(t, filepath.Join(root, "multica.yaml"), manifest)

			_, err := Load(filepath.Join(root, "multica.yaml"))
			if err == nil || !strings.Contains(err.Error(), "field "+test.field+" not found") {
				t.Fatalf("expected removed %s field error, got %v", test.field, err)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
