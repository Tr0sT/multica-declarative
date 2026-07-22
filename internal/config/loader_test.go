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
kind: Workspace
skills:
  - skills/unity
agents:
  - agents/unity.yaml
runtimes:
  desktop:
    name: desktop
    provider: codex
`)
	writeFile(t, filepath.Join(root, "skills/unity/SKILL.md"), `---
name: unity-development
description: Unity conventions
metadata:
  version: "1"
---

# Unity
`)
	writeFile(t, filepath.Join(root, "skills/unity/references/testing.md"), "# Tests\n")
	writeFile(t, filepath.Join(root, "agents/instructions.md"), "Do the work.\n")
	writeFile(t, filepath.Join(root, "agents/unity.yaml"), `kind: Prompt
name: Unity Developer
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
kind: Workspace
agents: [agent.yaml]
runtimes:
  desktop:
    name: desktop
`)
	writeFile(t, filepath.Join(root, "agent.yaml"), `kind: Prompt
name: Agent
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
kind: Workspace
unexpected: true
skills: [skills/example]
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
