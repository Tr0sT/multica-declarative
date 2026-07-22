package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExtendedAgentAndSquad(t *testing.T) {
	root := t.TempDir()
	writeExtendedFile(t, filepath.Join(root, "multica.yaml"), `apiVersion: multica-declarative/v1alpha1
kind: Workspace
skills: [skills/unity]
agents:
  - agents/builder/agent.yaml
  - agents/reviewer/agent.yaml
squads: [squads/unity/squad.yaml]
runtimes:
  desktop:
    name: desktop
    provider: codex
`)
	writeExtendedFile(t, filepath.Join(root, "skills/unity/SKILL.md"), "---\nname: unity\ndescription: Unity\n---\n")
	writeExtendedFile(t, filepath.Join(root, "agents/builder/AGENT.md"), "Build.\n")
	writeExtendedFile(t, filepath.Join(root, "agents/builder/env.json"), `{"TOKEN":"value"}`)
	writeExtendedFile(t, filepath.Join(root, "agents/builder/mcp.json"), `{"mcpServers":{}}`)
	writeExtendedFile(t, filepath.Join(root, "agents/builder/agent.yaml"), `name: Builder
instructionsFile: AGENT.md
skills:
  - name: unity
    enabled: false
multica:
  runtime: desktop
  runtimeConfig:
    sandbox: strict
  permission:
    mode: public_to
    members: [member-1]
  customEnvFile: env.json
  mcpConfigFile: mcp.json
  archived: true
  disabledRuntimeSkills:
    - root: universal
      key: local-unity
  composioToolkitAllowlist: [github]
`)
	writeExtendedFile(t, filepath.Join(root, "agents/reviewer/agent.yaml"), "name: Reviewer\nmultica:\n  runtime: desktop\n")
	writeExtendedFile(t, filepath.Join(root, "squads/unity/SQUAD.md"), "Coordinate.\n")
	writeExtendedFile(t, filepath.Join(root, "squads/unity/squad.yaml"), `kind: Squad
name: Unity Team
leader: Builder
instructionsFile: SQUAD.md
members:
  - type: agent
    agent: Reviewer
    role: reviewer
  - type: member
    id: member-1
    role: observer
`)
	project, err := Load(filepath.Join(root, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	agent := project.Agents[0]
	if !agent.ManageCustomEnv || !agent.ManageMCPConfig || !agent.Archived || agent.PermissionMode != "public_to" {
		t.Fatalf("agent=%#v", agent)
	}
	if len(agent.SkillAssignments) != 1 || agent.SkillAssignments[0].Enabled {
		t.Fatalf("skills=%#v", agent.SkillAssignments)
	}
	if len(project.Squads) != 1 {
		t.Fatalf("project=%#v", project)
	}
}

func TestRejectsTeamInvocationTargets(t *testing.T) {
	root := t.TempDir()
	writeExtendedFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\nkind: Workspace\nagents: [agent.yaml]\nruntimes:\n  r:\n    id: runtime-1\n")
	writeExtendedFile(t, filepath.Join(root, "agent.yaml"), "name: Agent\nmultica:\n  runtime: r\n  permission:\n    mode: public_to\n    teams: [team-1]\n")
	if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectsUnknownNestedAgentField(t *testing.T) {
	root := t.TempDir()
	writeExtendedFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\nkind: Workspace\nagents: [agent.yaml]\nruntimes:\n  r:\n    id: runtime-1\n")
	writeExtendedFile(t, filepath.Join(root, "agent.yaml"), "name: Agent\nmultica:\n  runtime: r\n  permission:\n    mode: private\n    surprise: true\n")
	if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil {
		t.Fatal("expected error")
	}
}

func writeExtendedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
