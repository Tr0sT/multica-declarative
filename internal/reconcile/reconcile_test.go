package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

type fakeBackend struct {
	skills                      []model.Skill
	skill                       model.Skill
	agents                      []model.Agent
	agent                       model.Agent
	runtimes                    []model.Runtime
	agentSkills                 []model.SkillSummary
	squads                      []model.Squad
	squad                       model.Squad
	members                     []model.SquadMember
	env                         map[string]string
	upserted                    []model.SkillFileInput
	deleted                     []string
	assignedIDs                 []string
	updatedSkill, updatedAgent  bool
	archived, restored          bool
	added, removed, roleChanged []model.SquadMember
}

func (f *fakeBackend) ListSkills() ([]model.Skill, error)   { return f.skills, nil }
func (f *fakeBackend) GetSkill(string) (model.Skill, error) { return f.skill, nil }
func (f *fakeBackend) CreateSkill(model.SkillInput) (model.Skill, error) {
	return model.Skill{ID: "created-skill", Name: "unity"}, nil
}
func (f *fakeBackend) UpdateSkill(_ string, _ model.SkillInput) (model.Skill, error) {
	f.updatedSkill = true
	return f.skill, nil
}
func (f *fakeBackend) UpsertSkillFile(_ string, v model.SkillFileInput) (model.SkillFile, error) {
	f.upserted = append(f.upserted, v)
	return model.SkillFile{ID: "new", Path: v.Path}, nil
}
func (f *fakeBackend) DeleteSkillFile(_, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeBackend) ListAgents() ([]model.Agent, error)   { return f.agents, nil }
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
func (f *fakeBackend) ListRuntimes() ([]model.Runtime, error)        { return f.runtimes, nil }
func (f *fakeBackend) GetAgentEnv(string) (map[string]string, error) { return f.env, nil }
func (f *fakeBackend) SetAgentEnv(string, string) error              { return nil }
func (f *fakeBackend) UploadAgentAvatar(string, string) error        { return nil }
func (f *fakeBackend) ArchiveAgent(string) error                     { f.archived = true; return nil }
func (f *fakeBackend) RestoreAgent(string) error                     { f.restored = true; return nil }
func (f *fakeBackend) ListSquads() ([]model.Squad, error)            { return f.squads, nil }
func (f *fakeBackend) GetSquad(string) (model.Squad, error)          { return f.squad, nil }
func (f *fakeBackend) CreateSquad(in model.SquadInput) (model.Squad, error) {
	f.members = []model.SquadMember{{MemberID: in.LeaderID, MemberType: "agent", Role: "leader"}}
	return model.Squad{ID: "sq", Name: in.Name, LeaderID: in.LeaderID}, nil
}
func (f *fakeBackend) UpdateSquad(_ string, _ model.SquadInput, _ []string) (model.Squad, error) {
	return f.squad, nil
}
func (f *fakeBackend) ListSquadMembers(string) ([]model.SquadMember, error) { return f.members, nil }
func (f *fakeBackend) AddSquadMember(_ string, m model.SquadMember) error {
	f.added = append(f.added, m)
	return nil
}
func (f *fakeBackend) SetSquadMemberRole(_ string, m model.SquadMember) error {
	f.roleChanged = append(f.roleChanged, m)
	return nil
}
func (f *fakeBackend) RemoveSquadMember(_ string, m model.SquadMember) error {
	f.removed = append(f.removed, m)
	return nil
}

func TestPlanDetectsUpdates(t *testing.T) {
	b := existingBackend()
	p := exampleProject(t)
	changes, err := (Reconciler{Backend: b}).Plan(p)
	if err != nil {
		t.Fatal(err)
	}
	if changes[0].Action != Update || changes[1].Action != Update {
		t.Fatalf("%#v", changes)
	}
}
func TestFullAgentNoop(t *testing.T) {
	enabled := true
	archived := "now"
	member := "m1"
	b := existingBackend()
	b.agent = model.Agent{ID: "agent-1", Name: "Unity Developer", Description: "desc", Instructions: "work", RuntimeID: "runtime-1", RuntimeConfig: map[string]any{"sandbox": "strict"}, Model: "model", ThinkingLevel: "high", CustomArgs: []string{"--x"}, PermissionMode: "public_to", InvocationTargets: []model.InvocationTarget{{TargetType: "member", TargetID: &member}}, MaxConcurrentTasks: 2, MCPConfig: json.RawMessage(`{"servers":{}}`), ArchivedAt: &archived}
	b.agentSkills = []model.SkillSummary{{ID: "skill-1", Name: "unity", Enabled: &enabled}}
	b.env = map[string]string{"TOKEN": "x"}
	p := exampleProject(t)
	p.Agents[0].Description = "desc"
	p.Agents[0].ManageRuntimeConfig = true
	p.Agents[0].RuntimeConfig = map[string]any{"sandbox": "strict"}
	p.Agents[0].ThinkingLevel = "high"
	p.Agents[0].CustomArgs = []string{"--x"}
	p.Agents[0].MaxConcurrentTasks = 2
	p.Agents[0].PermissionMode = "public_to"
	p.Agents[0].InvocationTargets = []model.InvocationTarget{{TargetType: "member", TargetID: &member}}
	p.Agents[0].Permission = ""
	p.Agents[0].SkillAssignments = []model.AgentSkillSpec{{Name: "unity", Enabled: true}}
	p.Agents[0].ManageMCPConfig = true
	p.Agents[0].MCPConfig = json.RawMessage(`{"servers":{}}`)
	p.Agents[0].ManageCustomEnv = true
	p.Agents[0].CustomEnv = map[string]string{"TOKEN": "x"}
	p.Agents[0].ManageArchived = true
	p.Agents[0].Archived = true
	changes, err := (Reconciler{Backend: b}).Plan(p)
	if err != nil {
		t.Fatal(err)
	}
	if changes[1].Action != Noop {
		t.Fatalf("%#v", changes[1])
	}
}

func TestWorkspacePermissionIgnoresServerWorkspaceID(t *testing.T) {
	workspaceID := "workspace-1"
	desired := model.AgentSpec{
		PermissionMode:    "public_to",
		InvocationTargets: []model.InvocationTarget{{TargetType: "workspace"}},
	}
	actual := model.Agent{
		PermissionMode:    "public_to",
		InvocationTargets: []model.InvocationTarget{{TargetType: "workspace", TargetID: &workspaceID}},
	}

	if !permissionMatches(desired, actual) {
		t.Fatal("workspace target IDs returned by the server must not cause drift")
	}
}

func TestObservedOnlyAgentFieldRejectsApply(t *testing.T) {
	b := existingBackend()
	p := exampleProject(t)
	p.Agents[0].ManageDisabledRuntimeSkills = true
	p.Agents[0].DisabledRuntimeSkills = []model.DisabledRuntimeSkill{{Root: "universal", Key: "x"}}
	err := (Reconciler{Backend: b}).Apply(p, func(model.Change) {})
	if err == nil {
		t.Fatal("expected error")
	}
}
func TestSquadApply(t *testing.T) {
	b := existingBackend()
	p := exampleProject(t)
	p.Squads = []model.SquadSpec{{Name: "Team", Leader: "Unity Developer", Members: []model.SquadMemberSpec{{Type: "member", ID: "human-1", Role: "member"}}}}
	if err := (Reconciler{Backend: b}).Apply(p, func(model.Change) {}); err != nil {
		t.Fatal(err)
	}
	if len(b.added) != 1 {
		t.Fatalf("added=%v", b.added)
	}
}
func TestArchivedAgentIsRestoredForUpdateAndRearchivedWhenUnmanaged(t *testing.T) {
	b := existingBackend()
	archived := "now"
	b.agent.ArchivedAt = &archived
	p := exampleProject(t)
	p.Agents[0].Description = "changed"
	if err := (Reconciler{Backend: b}).Apply(p, func(model.Change) {}); err != nil {
		t.Fatal(err)
	}
	if !b.restored || !b.archived {
		t.Fatalf("restored=%v archived=%v", b.restored, b.archived)
	}
}

func TestApplySynchronizesSkillFiles(t *testing.T) {
	b := existingBackend()
	p := exampleProject(t)
	file := filepath.Join(t.TempDir(), "testing.md")
	os.WriteFile(file, []byte("new"), 0644)
	p.Skills[0].Files = []model.SkillFileSpec{{Path: "testing.md", SourcePath: file, Content: "new"}}
	if err := (Reconciler{Backend: b}).Apply(p, func(model.Change) {}); err != nil {
		t.Fatal(err)
	}
	if len(b.upserted) != 1 || len(b.deleted) != 1 {
		t.Fatal(b.upserted, b.deleted)
	}
}
func existingBackend() *fakeBackend {
	return &fakeBackend{skills: []model.Skill{{ID: "skill-1", Name: "unity"}}, skill: model.Skill{ID: "skill-1", Name: "unity", Description: "old", Content: "old", Files: []model.SkillFile{{ID: "old", Path: "old.md", Content: "old"}}}, agents: []model.Agent{{ID: "agent-1", Name: "Unity Developer"}}, agent: model.Agent{ID: "agent-1", Name: "Unity Developer", RuntimeID: "runtime-1", PermissionMode: "private", MaxConcurrentTasks: 1}, runtimes: []model.Runtime{{ID: "runtime-1", Name: "desktop", Provider: "codex"}}, env: map[string]string{}}
}
func exampleProject(t *testing.T) model.Project {
	t.Helper()
	content := filepath.Join(t.TempDir(), "SKILL.md")
	os.WriteFile(content, []byte("new"), 0644)
	return model.Project{RuntimeSelectors: map[string]model.RuntimeSelector{"desktop": {Name: "desktop", Provider: "codex"}}, Skills: []model.SkillSpec{{Name: "unity", Description: "new", Content: "new", ContentPath: content}}, Agents: []model.AgentSpec{{Name: "Unity Developer", Instructions: "work", ModelID: "model", Skills: []string{"unity"}, RuntimeRef: "desktop", MaxConcurrentTasks: 1, Permission: "private"}}}
}
