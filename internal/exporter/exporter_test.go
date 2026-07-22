package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

type fakeBackend struct {
	skills      []model.Skill
	agents      []model.Agent
	runtimes    []model.Runtime
	agentSkills map[string][]model.SkillSummary
}

func (f *fakeBackend) ListSkills() ([]model.Skill, error) {
	result := make([]model.Skill, len(f.skills))
	for index, skill := range f.skills {
		result[index] = model.Skill{ID: skill.ID, Name: skill.Name, Description: skill.Description}
	}
	return result, nil
}

func (f *fakeBackend) GetSkill(skillID string) (model.Skill, error) {
	for _, skill := range f.skills {
		if skill.ID == skillID {
			return skill, nil
		}
	}
	return model.Skill{}, os.ErrNotExist
}

func (f *fakeBackend) CreateSkill(model.SkillInput) (model.Skill, error) {
	panic("unexpected mutation")
}

func (f *fakeBackend) UpdateSkill(string, model.SkillInput) (model.Skill, error) {
	panic("unexpected mutation")
}

func (f *fakeBackend) UpsertSkillFile(string, model.SkillFileInput) (model.SkillFile, error) {
	panic("unexpected mutation")
}

func (f *fakeBackend) DeleteSkillFile(string, string) error {
	panic("unexpected mutation")
}

func (f *fakeBackend) ListAgents() ([]model.Agent, error) {
	result := make([]model.Agent, len(f.agents))
	for index, agent := range f.agents {
		result[index] = model.Agent{ID: agent.ID, Name: agent.Name}
	}
	return result, nil
}

func (f *fakeBackend) GetAgent(agentID string) (model.Agent, error) {
	for _, agent := range f.agents {
		if agent.ID == agentID {
			return agent, nil
		}
	}
	return model.Agent{}, os.ErrNotExist
}

func (f *fakeBackend) ListAgentSkills(agentID string) ([]model.SkillSummary, error) {
	return append([]model.SkillSummary(nil), f.agentSkills[agentID]...), nil
}

func (f *fakeBackend) CreateAgent(model.AgentInput) (model.Agent, error) {
	panic("unexpected mutation")
}

func (f *fakeBackend) UpdateAgent(string, model.AgentInput) (model.Agent, error) {
	panic("unexpected mutation")
}

func (f *fakeBackend) SetAgentSkills(string, []string) error {
	panic("unexpected mutation")
}

func (f *fakeBackend) ListRuntimes() ([]model.Runtime, error) {
	return append([]model.Runtime(nil), f.runtimes...), nil
}

func TestExportCreatesRoundTrippableNoopSnapshot(t *testing.T) {
	t.Parallel()
	backend := exampleBackend()
	output := filepath.Join(t.TempDir(), "export")

	result, err := (Exporter{Backend: backend}).Export(Options{OutputDir: output})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if result.Skills != 1 || result.Agents != 1 || result.Runtimes != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}

	project, err := config.Load(filepath.Join(output, "multica.yaml"))
	if err != nil {
		t.Fatalf("load exported project: %v", err)
	}
	if project.Agents[0].Instructions != "Implement the task.\n" {
		t.Fatalf("instructions = %q", project.Agents[0].Instructions)
	}
	if project.RuntimeSelectors[project.Agents[0].RuntimeRef].CustomName != "Main PC" {
		t.Fatalf("runtime selector = %#v", project.RuntimeSelectors)
	}

	changes, err := (reconcile.Reconciler{Backend: backend}).Plan(project)
	if err != nil {
		t.Fatalf("plan exported project: %v", err)
	}
	for _, change := range changes {
		if change.Action != reconcile.Noop {
			t.Fatalf("change = %#v", change)
		}
	}
}

func TestExportGeneratesFrontmatterForLegacySkill(t *testing.T) {
	t.Parallel()
	backend := exampleBackend()
	backend.skills[0].Content = "# Legacy skill\n"
	output := filepath.Join(t.TempDir(), "export")

	result, err := (Exporter{Backend: backend}).Export(Options{OutputDir: output})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	data, err := os.ReadFile(filepath.Join(output, "skills", "unity-development", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") || !strings.Contains(content, "name: unity-development") {
		t.Fatalf("content = %q", content)
	}
}

func TestExportRefusesNonEmptyDirectoryWithoutForce(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	output := filepath.Join(root, "export")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (Exporter{Backend: exampleBackend()}).Export(Options{OutputDir: output})
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(output, "keep.txt")); statErr != nil {
		t.Fatalf("unrelated file was changed: %v", statErr)
	}
}

func TestExportForcePreservesUnrelatedFilesAndReplacesGeneratedPaths(t *testing.T) {
	t.Parallel()
	output := filepath.Join(t.TempDir(), "export")
	if err := os.MkdirAll(filepath.Join(output, "agents", "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "agents", "stale", "agent.yaml"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "README.md"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (Exporter{Backend: exampleBackend()}).Export(Options{OutputDir: output, Force: true})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "README.md")); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "agents", "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale generated path still exists: %v", err)
	}
}

func TestExportRejectsMemberScopedPermissionBeforeWriting(t *testing.T) {
	t.Parallel()
	backend := exampleBackend()
	memberID := "member-1"
	backend.agents[0].PermissionMode = "public_to"
	backend.agents[0].InvocationTargets = []model.InvocationTarget{{TargetType: "member", TargetID: &memberID}}
	output := filepath.Join(t.TempDir(), "export")

	_, err := (Exporter{Backend: backend}).Export(Options{OutputDir: output})
	if err == nil || !strings.Contains(err.Error(), "not representable") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("output should not exist after failed export: %v", statErr)
	}
}

func TestExportRejectsUnsafeSkillFilePath(t *testing.T) {
	t.Parallel()
	backend := exampleBackend()
	backend.skills[0].Files = []model.SkillFile{{ID: "file-1", Path: "../escape.md", Content: "escape"}}

	_, err := (Exporter{Backend: backend}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "export")})
	if err == nil || !strings.Contains(err.Error(), "unsafe file path") {
		t.Fatalf("err = %v", err)
	}
}

func exampleBackend() *fakeBackend {
	workspaceTarget := model.InvocationTarget{TargetType: "workspace"}
	return &fakeBackend{
		skills: []model.Skill{{
			ID:          "skill-1",
			Name:        "unity-development",
			Description: "Unity conventions",
			Content:     "---\nname: unity-development\ndescription: Unity conventions\n---\n\n# Unity\n",
			Files: []model.SkillFile{{
				ID:      "file-1",
				Path:    "references/testing.md",
				Content: "# Testing\n",
			}},
		}},
		agents: []model.Agent{{
			ID:                 "agent-1",
			Name:               "Unity Developer",
			Description:        "Implements Unity tasks.",
			Instructions:       "Implement the task.\n",
			RuntimeID:          "runtime-1",
			Model:              "gpt-5.6",
			ThinkingLevel:      "high",
			CustomArgs:         []string{"--full-auto"},
			PermissionMode:     "public_to",
			InvocationTargets:  []model.InvocationTarget{workspaceTarget},
			MaxConcurrentTasks: 1,
		}},
		runtimes: []model.Runtime{{
			ID:         "runtime-1",
			Name:       "desktop",
			CustomName: "Main PC",
			Provider:   "codex",
			Status:     "online",
		}},
		agentSkills: map[string][]model.SkillSummary{
			"agent-1": {{ID: "skill-1", Name: "unity-development"}},
		},
	}
}
