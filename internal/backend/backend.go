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
