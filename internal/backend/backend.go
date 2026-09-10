package backend

import "github.com/Tr0sT/multica-declarative/internal/model"

type Backend interface {
	ListSkills() ([]model.Skill, error)
	GetSkill(skillID string) (model.Skill, error)
	CreateSkill(input model.SkillInput) (model.Skill, error)
	UpdateSkill(skillID string, input model.SkillInput) (model.Skill, error)
	UpsertSkillFile(skillID string, input model.SkillFileInput) (model.SkillFile, error)
	DeleteSkillFile(skillID, fileID string) error

	ListAgents() ([]model.Agent, error)
	GetAgent(agentID string) (model.Agent, error)
	ListAgentSkills(agentID string) ([]model.SkillSummary, error)
	CreateAgent(input model.AgentInput) (model.Agent, error)
	UpdateAgent(agentID string, input model.AgentInput) (model.Agent, error)
	SetAgentSkills(agentID string, skillIDs []string) error

	ListRuntimes() ([]model.Runtime, error)
}

type AgentOperations interface {
	GetAgentEnv(agentID string) (map[string]string, error)
	SetAgentEnv(agentID, file string) error
	UploadAgentAvatar(agentID, file string) error
	ArchiveAgent(agentID string) error
	RestoreAgent(agentID string) error
}

type SquadOperations interface {
	ListSquads() ([]model.Squad, error)
	GetSquad(squadID string) (model.Squad, error)
	CreateSquad(input model.SquadInput) (model.Squad, error)
	UpdateSquad(squadID string, input model.SquadInput, fields []string) (model.Squad, error)
	ListSquadMembers(squadID string) ([]model.SquadMember, error)
	AddSquadMember(squadID string, member model.SquadMember) error
	SetSquadMemberRole(squadID string, member model.SquadMember) error
	RemoveSquadMember(squadID string, member model.SquadMember) error
}

// WorkspaceMCPReader exposes metadata only. Stored library entries are write-only
// in Multica and cannot be reconstructed by an exporter.
type WorkspaceMCPReader interface {
	ListWorkspaceMCPServers() ([]model.WorkspaceMCPServer, error)
}

// Optional capabilities keep older mock/backends usable for agent-only projects.
type ProjectOperations interface {
	ListProjects() ([]model.WorkspaceProject, error)
	GetProject(string) (model.WorkspaceProject, error)
	CreateProject(model.ProjectInput) (model.WorkspaceProject, error)
	UpdateProject(string, model.ProjectInput, []string) (model.WorkspaceProject, error)
	ListProjectResources(string) ([]model.ProjectResource, error)
	AddProjectResource(string, model.ProjectResourceSpec) (model.ProjectResource, error)
	UpdateProjectResource(string, string, model.ProjectResourceSpec) error
	RemoveProjectResource(string, string) error
}
type AutopilotOperations interface {
	ListAutopilots() ([]model.Autopilot, error)
	GetAutopilot(string) (model.AutopilotDetail, error)
	CreateAutopilot(model.AutopilotInput) (model.Autopilot, error)
	UpdateAutopilot(string, model.AutopilotInput, []string) error
	AddAutopilotTrigger(string, model.AutopilotTriggerSpec) (model.AutopilotTrigger, error)
	UpdateAutopilotTrigger(string, string, model.AutopilotTriggerSpec) error
	DeleteAutopilotTrigger(string, string) error
}
