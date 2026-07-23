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

func TestRejectsMultipleYAMLDocuments(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n---\napiVersion: multica-declarative/v1alpha1\n")

	_, err := Load(filepath.Join(root, "multica.yaml"))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("expected multiple-document error, got %v", err)
	}
}

func TestRejectsResourceSymlinks(t *testing.T) {
	t.Parallel()
	t.Run("declaration marker", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "SKILL.md")
		writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n")
		writeFile(t, outside, "---\nname: escaped\ndescription: Escaped\n---\n")
		if err := os.MkdirAll(filepath.Join(root, "skills", "escaped"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "skills", "escaped", "SKILL.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("expected symlink error, got %v", err)
		}
	})

	t.Run("referenced file", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "AGENT.md")
		writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\nruntimes:\n  local:\n    id: runtime-1\n")
		writeFile(t, filepath.Join(root, "agents", "agent", "agent.yaml"), "name: Agent\ninstructionsFile: AGENT.md\nmultica:\n  runtime: local\n")
		writeFile(t, outside, "escaped\n")
		if err := os.Symlink(outside, filepath.Join(root, "agents", "agent", "AGENT.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("expected symlink error, got %v", err)
		}
	})
}

func TestRejectsNullCustomEnvironment(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\nruntimes:\n  local:\n    id: runtime-1\n")
	writeFile(t, filepath.Join(root, "agents", "agent", "agent.yaml"), "name: Agent\nmultica:\n  runtime: local\n  customEnvFile: custom-env.json\n")
	writeFile(t, filepath.Join(root, "agents", "agent", "custom-env.json"), "null\n")

	_, err := Load(filepath.Join(root, "multica.yaml"))
	if err == nil || !strings.Contains(err.Error(), "must contain a JSON object") {
		t.Fatalf("expected object error, got %v", err)
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
