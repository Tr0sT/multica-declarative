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

type RuntimeProfileOperations interface {
	ListRuntimeProfiles() ([]model.RuntimeProfile, error)
	CreateRuntimeProfile(input model.RuntimeProfileInput) (model.RuntimeProfile, error)
	UpdateRuntimeProfile(profileID string, input model.RuntimeProfileInput, fields []string) (model.RuntimeProfile, error)
}
