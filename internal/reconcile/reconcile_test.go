package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

type fakeBackend struct {
	skills       []model.Skill
	skill        model.Skill
	agents       []model.Agent
	agent        model.Agent
	runtimes     []model.Runtime
	agentSkills  []model.SkillSummary
	upserted     []model.SkillFileInput
	deleted      []string
	assignedIDs  []string
	updatedSkill bool
	updatedAgent bool
}

func (f *fakeBackend) ListSkills() ([]model.Skill, error) { return f.skills, nil }
func (f *fakeBackend) GetSkill(string) (model.Skill, error) { return f.skill, nil }
func (f *fakeBackend) CreateSkill(model.SkillInput) (model.Skill, error) {
	return model.Skill{ID: "created-skill", Name: "unity"}, nil
}
func (f *fakeBackend) UpdateSkill(_ string, _ model.SkillInput) (model.Skill, error) {
	f.updatedSkill = true
	return f.skill, nil
}
func (f *fakeBackend) UpsertSkillFile(_ string, input model.SkillFileInput) (model.SkillFile, error) {
	f.upserted = append(f.upserted, input)
	return model.SkillFile{ID: "new", Path: input.Path}, nil
}
func (f *fakeBackend) DeleteSkillFile(_, fileID string) error {
	f.deleted = append(f.deleted, fileID)
	return nil
}
func (f *fakeBackend) ListAgents() ([]model.Agent, error) { return f.agents, nil }
func (f *fakeBackend) GetAgent(string) (model.Agent, error) { return f.agent, nil }
func (f *fakeBackend) ListAgentSkills(string) ([]model.SkillSummary, error) {
	return f.agentSkills, nil
}
func (f *fakeBackend) CreateAgent(model.AgentInput) (model.Agent, error) {
	return model.Agent{ID: "created-agent", Name: "Unity Developer"}, nil
}
func (f *fakeBackend) UpdateAgent(_ string, _ model.AgentInput) (model.Agent, error) {
	f.updatedAgent = true
	return f.agent, nil
}
func (f *fakeBackend) SetAgentSkills(_ string, ids []string) error {
	f.assignedIDs = append([]string(nil), ids...)
	return nil
}
func (f *fakeBackend) ListRuntimes() ([]model.Runtime, error) { return f.runtimes, nil }

func TestPlanDetectsUpdates(t *testing.T) {
	t.Parallel()
	backend := existingBackend()
	project := exampleProject(t)

	changes, err := (Reconciler{Backend: backend}).Plan(project)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if changes[0].Action != Update || changes[1].Action != Update {
		t.Fatalf("changes = %#v", changes)
	}
	if !hasField(changes[1].Fields, "skills") {
		t.Fatalf("agent fields = %v", changes[1].Fields)
	}
}

func TestPlanNoopWhenEqual(t *testing.T) {
	t.Parallel()
	backend := existingBackend()
	backend.skill = model.Skill{ID: "skill-1", Name: "unity", Description: "new", Content: "new"}
	project := exampleProject(t)
	project.Agents = nil

	changes, err := (Reconciler{Backend: backend}).Plan(project)
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if changes[0].Action != Noop {
		t.Fatalf("change = %#v", changes[0])
	}
}

func TestApplySynchronizesSkillFiles(t *testing.T) {
	t.Parallel()
	backend := existingBackend()
	project := exampleProject(t)
	filePath := filepath.Join(t.TempDir(), "testing.md")
	if err := os.WriteFile(filePath, []byte("new file"), 0o644); err != nil {
		t.Fatal(err)
	}
	project.Skills[0].Files = []model.SkillFileSpec{{Path: "testing.md", SourcePath: filePath, Content: "new file"}}

	err := (Reconciler{Backend: backend}).Apply(project, func(model.Change) {})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(backend.upserted) != 1 || len(backend.deleted) != 1 {
		t.Fatalf("upserted=%v deleted=%v", backend.upserted, backend.deleted)
	}
}

func existingBackend() *fakeBackend {
	return &fakeBackend{
		skills: []model.Skill{{ID: "skill-1", Name: "unity", Description: "old"}},
		skill: model.Skill{
			ID:          "skill-1",
			Name:        "unity",
			Description: "old",
			Content:     "old",
			Files:       []model.SkillFile{{ID: "old-file", Path: "old.md", Content: "old"}},
		},
		agents: []model.Agent{{ID: "agent-1", Name: "Unity Developer"}},
		agent: model.Agent{
			ID:                 "agent-1",
			Name:               "Unity Developer",
			RuntimeID:          "runtime-1",
			PermissionMode:     "private",
			MaxConcurrentTasks: 1,
		},
		runtimes: []model.Runtime{{ID: "runtime-1", Name: "desktop", Provider: "codex"}},
	}
}

func exampleProject(t *testing.T) model.Project {
	t.Helper()
	contentPath := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(contentPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	return model.Project{
		RuntimeSelectors: map[string]model.RuntimeSelector{
			"desktop": {Name: "desktop", Provider: "codex"},
		},
		Skills: []model.SkillSpec{{
			Name:        "unity",
			Description: "new",
			Content:     "new",
			ContentPath: contentPath,
		}},
		Agents: []model.AgentSpec{{
			Name:               "Unity Developer",
			Instructions:       "work",
			ModelID:            "model",
			Skills:             []string{"unity"},
			RuntimeRef:         "desktop",
			MaxConcurrentTasks: 1,
			Permission:         "private",
		}},
	}
}

func hasField(fields []string, expected string) bool {
	for _, field := range fields {
		if field == expected {
			return true
		}
	}
	return false
}
