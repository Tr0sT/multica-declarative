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
	skills       []model.Skill
	agents       []model.Agent
	runtimes     []model.Runtime
	agentSkills  map[string][]model.SkillSummary
	profiles     []model.RuntimeProfile
	squads       []model.Squad
	squadMembers map[string][]model.SquadMember
}

func (f *fakeBackend) ListSkills() ([]model.Skill, error) {
	out := []model.Skill{}
	for _, v := range f.skills {
		out = append(out, model.Skill{ID: v.ID, Name: v.Name, Description: v.Description})
	}
	return out, nil
}
func (f *fakeBackend) GetSkill(id string) (model.Skill, error) {
	for _, v := range f.skills {
		if v.ID == id {
			return v, nil
		}
	}
	return model.Skill{}, os.ErrNotExist
}
func (*fakeBackend) CreateSkill(model.SkillInput) (model.Skill, error)         { panic("mutation") }
func (*fakeBackend) UpdateSkill(string, model.SkillInput) (model.Skill, error) { panic("mutation") }
func (*fakeBackend) UpsertSkillFile(string, model.SkillFileInput) (model.SkillFile, error) {
	panic("mutation")
}
func (*fakeBackend) DeleteSkillFile(string, string) error { panic("mutation") }
func (f *fakeBackend) ListAgents() ([]model.Agent, error) {
	out := []model.Agent{}
	for _, v := range f.agents {
		out = append(out, model.Agent{ID: v.ID, Name: v.Name})
	}
	return out, nil
}
func (f *fakeBackend) GetAgent(id string) (model.Agent, error) {
	for _, v := range f.agents {
		if v.ID == id {
			return v, nil
		}
	}
	return model.Agent{}, os.ErrNotExist
}
func (f *fakeBackend) ListAgentSkills(id string) ([]model.SkillSummary, error) {
	return append([]model.SkillSummary(nil), f.agentSkills[id]...), nil
}
func (*fakeBackend) CreateAgent(model.AgentInput) (model.Agent, error)         { panic("mutation") }
func (*fakeBackend) UpdateAgent(string, model.AgentInput) (model.Agent, error) { panic("mutation") }
func (*fakeBackend) SetAgentSkills(string, []string) error                     { panic("mutation") }
func (f *fakeBackend) ListRuntimes() ([]model.Runtime, error) {
	return append([]model.Runtime(nil), f.runtimes...), nil
}
func (f *fakeBackend) ListRuntimeProfiles() ([]model.RuntimeProfile, error) {
	return append([]model.RuntimeProfile(nil), f.profiles...), nil
}
func (*fakeBackend) CreateRuntimeProfile(model.RuntimeProfileInput) (model.RuntimeProfile, error) {
	panic("mutation")
}
func (*fakeBackend) UpdateRuntimeProfile(string, model.RuntimeProfileInput, []string) (model.RuntimeProfile, error) {
	panic("mutation")
}
func (f *fakeBackend) ListSquads() ([]model.Squad, error) {
	return append([]model.Squad(nil), f.squads...), nil
}
func (f *fakeBackend) GetSquad(id string) (model.Squad, error) {
	for _, v := range f.squads {
		if v.ID == id {
			return v, nil
		}
	}
	return model.Squad{}, os.ErrNotExist
}
func (*fakeBackend) CreateSquad(model.SquadInput) (model.Squad, error) { panic("mutation") }
func (*fakeBackend) UpdateSquad(string, model.SquadInput, []string) (model.Squad, error) {
	panic("mutation")
}
func (f *fakeBackend) ListSquadMembers(id string) ([]model.SquadMember, error) {
	return append([]model.SquadMember(nil), f.squadMembers[id]...), nil
}
func (*fakeBackend) AddSquadMember(string, model.SquadMember) error     { panic("mutation") }
func (*fakeBackend) SetSquadMemberRole(string, model.SquadMember) error { panic("mutation") }
func (*fakeBackend) RemoveSquadMember(string, model.SquadMember) error  { panic("mutation") }

func TestExportCreatesRoundTrippableSnapshot(t *testing.T) {
	b := exampleBackend()
	out := filepath.Join(t.TempDir(), "export")
	result, err := (Exporter{Backend: b}).Export(Options{OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skills != 1 || result.Agents != 2 || result.Squads != 1 || result.RuntimeProfiles != 1 {
		t.Fatalf("%#v", result)
	}
	project, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	changes, err := (reconcile.Reconciler{Backend: b}).Plan(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Action != reconcile.Noop {
			t.Fatalf("%#v", c)
		}
	}
}
func TestExportRepresentsMemberScopedPermission(t *testing.T) {
	b := exampleBackend()
	member := "member-1"
	b.agents[0].PermissionMode = "public_to"
	b.agents[0].InvocationTargets = []model.InvocationTarget{{TargetType: "member", TargetID: &member}}
	out := filepath.Join(t.TempDir(), "export")
	if _, err := (Exporter{Backend: b}).Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	project, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if project.Agents[0].PermissionMode != "public_to" || len(project.Agents[0].InvocationTargets) != 1 {
		t.Fatalf("%#v", project.Agents[0])
	}
}
func TestExportForcePreservesUnrelated(t *testing.T) {
	out := filepath.Join(t.TempDir(), "export")
	os.MkdirAll(filepath.Join(out, "agents", "stale"), 0755)
	os.WriteFile(filepath.Join(out, "README.md"), []byte("keep"), 0644)
	if _, err := (Exporter{Backend: exampleBackend()}).Export(Options{OutputDir: out, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "README.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "agents", "stale")); !os.IsNotExist(err) {
		t.Fatal("stale path remains")
	}
}
func TestExportRejectsUnsafeSkillPath(t *testing.T) {
	b := exampleBackend()
	b.skills[0].Files = []model.SkillFile{{ID: "f", Path: "../escape", Content: "x"}}
	_, err := (Exporter{Backend: b}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "export")})
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatal(err)
	}
}
func exampleBackend() *fakeBackend {
	workspace := model.InvocationTarget{TargetType: "workspace"}
	return &fakeBackend{skills: []model.Skill{{ID: "skill-1", Name: "unity-development", Description: "Unity conventions", Content: "---\nname: unity-development\ndescription: Unity conventions\n---\n", Files: []model.SkillFile{{ID: "f", Path: "references/test.md", Content: "test"}}}}, agents: []model.Agent{{ID: "agent-1", Name: "Unity Developer", Instructions: "work", RuntimeID: "runtime-1", PermissionMode: "public_to", InvocationTargets: []model.InvocationTarget{workspace}, MaxConcurrentTasks: 1}, {ID: "agent-2", Name: "Reviewer", RuntimeID: "runtime-1", PermissionMode: "private", MaxConcurrentTasks: 1}}, runtimes: []model.Runtime{{ID: "runtime-1", Name: "desktop", CustomName: "Main PC", Provider: "codex"}}, agentSkills: map[string][]model.SkillSummary{"agent-1": {{ID: "skill-1", Name: "unity-development"}}}, profiles: []model.RuntimeProfile{{ID: "p", DisplayName: "Wrapper", ProtocolFamily: "codex", CommandName: "wrapper", Enabled: true, Visibility: "workspace"}}, squads: []model.Squad{{ID: "sq", Name: "Team", LeaderID: "agent-1", Instructions: "coordinate"}}, squadMembers: map[string][]model.SquadMember{"sq": {{MemberID: "agent-1", MemberType: "agent", Role: "leader"}, {MemberID: "agent-2", MemberType: "agent", Role: "member"}}}}
}
