package exporter

import (
	"encoding/json"
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
	agentEnvs    map[string]map[string]string
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
func (f *fakeBackend) GetAgentEnv(id string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range f.agentEnvs[id] {
		out[key] = value
	}
	return out, nil
}
func (*fakeBackend) SetAgentEnv(string, string) error       { panic("mutation") }
func (*fakeBackend) UploadAgentAvatar(string, string) error { panic("mutation") }
func (*fakeBackend) ArchiveAgent(string) error              { panic("mutation") }
func (*fakeBackend) RestoreAgent(string) error              { panic("mutation") }
func (f *fakeBackend) ListRuntimes() ([]model.Runtime, error) {
	return append([]model.Runtime(nil), f.runtimes...), nil
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
	if result.Skills != 1 || result.Agents != 2 || result.Squads != 1 {
		t.Fatalf("%#v", result)
	}
	if _, err := os.Stat(filepath.Join(out, "runtime-profiles")); !os.IsNotExist(err) {
		t.Fatalf("removed runtime-profiles directory was created: %v", err)
	}
	manifestYAML, err := os.ReadFile(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"kind:", "skills:", "agents:"} {
		if strings.Contains(string(manifestYAML), removed) {
			t.Fatalf("workspace manifest contains removed %s field:\n%s", removed, manifestYAML)
		}
	}
	agentYAML, err := os.ReadFile(filepath.Join(out, "agents", "unity-developer", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agentYAML), "kind:") {
		t.Fatalf("agent declaration contains redundant kind field:\n%s", agentYAML)
	}
	for _, reference := range []string{"customEnvFile: custom-env.json", "mcpConfigFile: mcp.json"} {
		if !strings.Contains(string(agentYAML), reference) {
			t.Fatalf("agent declaration does not contain %q:\n%s", reference, agentYAML)
		}
	}
	environmentData, err := os.ReadFile(filepath.Join(out, "agents", "unity-developer", customEnvFileName))
	if err != nil {
		t.Fatal(err)
	}
	var environment map[string]string
	if err := json.Unmarshal(environmentData, &environment); err != nil || environment["TOKEN"] != "env-secret" {
		t.Fatalf("exported custom environment is invalid: %v", err)
	}
	mcpData, err := os.ReadFile(filepath.Join(out, "agents", "unity-developer", mcpConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	var mcp map[string]string
	if err := json.Unmarshal(mcpData, &mcp); err != nil || mcp["token"] != "mcp-secret" {
		t.Fatalf("exported MCP config is invalid: %v", err)
	}
	for _, secretFile := range []string{customEnvFileName, mcpConfigFileName} {
		info, err := os.Stat(filepath.Join(out, "agents", "unity-developer", secretFile))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s permissions = %o", secretFile, info.Mode().Perm())
		}
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
	var exported *model.AgentSpec
	for index := range project.Agents {
		if project.Agents[index].Name == "Unity Developer" {
			exported = &project.Agents[index]
			break
		}
	}
	if exported == nil || exported.PermissionMode != "public_to" || len(exported.InvocationTargets) != 1 {
		t.Fatalf("agents = %#v", project.Agents)
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
func TestExportForcePreservesNestedAgentDirectory(t *testing.T) {
	out := filepath.Join(t.TempDir(), "export")
	nested := filepath.Join(out, "agents", "main", "custom-unity")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "agent.yaml"), []byte("name: Unity Developer\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := (Exporter{Backend: exampleBackend()}).Export(Options{OutputDir: out, Force: true}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		filepath.Join(out, "agents", "main", "custom-unity", "agent.yaml"),
		filepath.Join(out, "agents", "reviewer", "agent.yaml"),
	} {
		if _, err := os.Stat(expected); err != nil {
			t.Fatalf("expected exported agent at %s: %v", expected, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "agents", "unity-developer")); !os.IsNotExist(err) {
		t.Fatalf("matched agent was also exported at the default path: %v", err)
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
func TestExportRejectsRedactedMCPConfig(t *testing.T) {
	b := exampleBackend()
	b.agents[0].MCPConfigRedacted = true
	_, err := (Exporter{Backend: b}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "export")})
	if err == nil || !strings.Contains(err.Error(), "MCP config is redacted") {
		t.Fatal(err)
	}
}
func exampleBackend() *fakeBackend {
	workspace := model.InvocationTarget{TargetType: "workspace"}
	return &fakeBackend{skills: []model.Skill{{ID: "skill-1", Name: "unity-development", Description: "Unity conventions", Content: "---\nname: unity-development\ndescription: Unity conventions\n---\n", Files: []model.SkillFile{{ID: "f", Path: "references/test.md", Content: "test"}}}}, agents: []model.Agent{{ID: "agent-1", Name: "Unity Developer", Instructions: "work", RuntimeID: "runtime-1", PermissionMode: "public_to", InvocationTargets: []model.InvocationTarget{workspace}, MaxConcurrentTasks: 1, MCPConfig: json.RawMessage(`{"token":"mcp-secret"}`), HasCustomEnv: true, CustomEnvKeyCount: 1}, {ID: "agent-2", Name: "Reviewer", RuntimeID: "runtime-1", PermissionMode: "private", MaxConcurrentTasks: 1}}, runtimes: []model.Runtime{{ID: "runtime-1", Name: "desktop", CustomName: "Main PC", Provider: "codex"}}, agentSkills: map[string][]model.SkillSummary{"agent-1": {{ID: "skill-1", Name: "unity-development"}}}, agentEnvs: map[string]map[string]string{"agent-1": {"TOKEN": "env-secret"}}, squads: []model.Squad{{ID: "sq", Name: "Team", LeaderID: "agent-1", Instructions: "coordinate"}}, squadMembers: map[string][]model.SquadMember{"sq": {{MemberID: "agent-1", MemberType: "agent", Role: "leader"}, {MemberID: "agent-2", MemberType: "agent", Role: "member"}}}}
}
