package model

import "encoding/json"

// RuntimeSelector matches one Multica runtime. At least one identity field is required.
type RuntimeSelector struct {
	ID         string
	Name       string
	CustomName string
	Provider   string
}

type SkillFileSpec struct {
	Path       string
	SourcePath string
	Content    string
}

type SkillSpec struct {
	Name        string
	Description string
	Content     string
	SourceDir   string
	ContentPath string
	Files       []SkillFileSpec
}

type InvocationTarget struct {
	TargetType string  `json:"target_type"`
	TargetID   *string `json:"target_id"`
}

type AgentSkillSpec struct {
	Name    string
	Enabled bool
}

type DisabledRuntimeSkill struct {
	RuntimeID string `json:"runtime_id" yaml:"runtimeId"`
	Provider  string `json:"provider" yaml:"provider"`
	Root      string `json:"root" yaml:"root"`
	Key       string `json:"key" yaml:"key"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Plugin    string `json:"plugin,omitempty" yaml:"plugin,omitempty"`
}

type AgentSpec struct {
	Name                           string
	Description                    string
	Instructions                   string
	ModelID                        string
	Skills                         []string // Backward-compatible enabled skill names.
	SkillAssignments               []AgentSkillSpec
	RuntimeRef                     string
	ManageRuntimeConfig            bool
	RuntimeConfig                  map[string]any
	ThinkingLevel                  string
	MaxConcurrentTasks             int
	Permission                     string // Backward-compatible private/workspace value.
	PermissionMode                 string
	InvocationTargets              []InvocationTarget
	CustomArgs                     []string
	ManageCustomEnv                bool
	CustomEnv                      map[string]string
	CustomEnvFile                  string
	ManageMCPConfig                bool
	MCPConfig                      json.RawMessage
	MCPConfigFile                  string
	AvatarFile                     string
	ManageArchived                 bool
	Archived                       bool
	ManageDisabledRuntimeSkills    bool
	DisabledRuntimeSkills          []DisabledRuntimeSkill
	ManageComposioToolkitAllowlist bool
	ComposioToolkitAllowlist       []string
	SourcePath                     string
}

type SquadMemberSpec struct {
	Type  string
	Agent string
	ID    string
	Role  string
}

type SquadSpec struct {
	Name             string
	Description      string
	Instructions     string
	Leader           string
	AvatarURL        string
	Members          []SquadMemberSpec
	SourcePath       string
	InstructionsFile string
}

type RuntimeProfileSpec struct {
	DisplayName    string
	ProtocolFamily string
	CommandName    string
	Description    string
	Enabled        bool
	FixedArgs      []string
	Visibility     string
	SourcePath     string
}

type Project struct {
	WorkspacePath    string
	RuntimeSelectors map[string]RuntimeSelector
	Skills           []SkillSpec
	Agents           []AgentSpec
	Squads           []SquadSpec
	RuntimeProfiles  []RuntimeProfileSpec
}

type SkillFile struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Skill struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Content     string      `json:"content"`
	Files       []SkillFile `json:"files"`
}

type SkillSummary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type Agent struct {
	ID                               string                 `json:"id"`
	Name                             string                 `json:"name"`
	Description                      string                 `json:"description"`
	Instructions                     string                 `json:"instructions"`
	AvatarURL                        *string                `json:"avatar_url"`
	RuntimeID                        string                 `json:"runtime_id"`
	RuntimeConfig                    map[string]any         `json:"runtime_config"`
	Model                            string                 `json:"model"`
	ThinkingLevel                    string                 `json:"thinking_level"`
	CustomArgs                       []string               `json:"custom_args"`
	PermissionMode                   string                 `json:"permission_mode"`
	InvocationTargets                []InvocationTarget     `json:"invocation_targets"`
	MaxConcurrentTasks               int                    `json:"max_concurrent_tasks"`
	MCPConfig                        json.RawMessage        `json:"mcp_config"`
	MCPConfigRedacted                bool                   `json:"mcp_config_redacted"`
	HasCustomEnv                     bool                   `json:"has_custom_env"`
	CustomEnvKeyCount                int                    `json:"custom_env_key_count"`
	ComposioToolkitAllowlist         []string               `json:"composio_toolkit_allowlist"`
	ComposioToolkitAllowlistRedacted bool                   `json:"composio_toolkit_allowlist_redacted"`
	DisabledRuntimeSkills            []DisabledRuntimeSkill `json:"disabled_runtime_skills"`
	Skills                           []SkillSummary         `json:"skills"`
	ArchivedAt                       *string                `json:"archived_at"`
}

func (a Agent) Archived() bool { return a.ArchivedAt != nil && *a.ArchivedAt != "" }

type Runtime struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CustomName string `json:"custom_name"`
	Provider   string `json:"provider"`
	Status     string `json:"status"`
	ProfileID  string `json:"profile_id"`
}

type Squad struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Instructions string  `json:"instructions"`
	LeaderID     string  `json:"leader_id"`
	AvatarURL    *string `json:"avatar_url"`
}

type SquadMember struct {
	MemberID   string `json:"member_id"`
	MemberType string `json:"member_type"`
	Role       string `json:"role"`
}

type RuntimeProfile struct {
	ID             string   `json:"id"`
	DisplayName    string   `json:"display_name"`
	ProtocolFamily string   `json:"protocol_family"`
	CommandName    string   `json:"command_name"`
	Description    *string  `json:"description"`
	FixedArgs      []string `json:"fixed_args"`
	Visibility     string   `json:"visibility"`
	Enabled        bool     `json:"enabled"`
}

type SkillInput struct {
	Name        string
	Description string
	ContentFile string
}

type SkillFileInput struct {
	Path        string
	ContentFile string
}

type AgentInput struct {
	Name                string
	Description         string
	Instructions        string
	RuntimeID           string
	ManageRuntimeConfig bool
	RuntimeConfig       map[string]any
	Model               string
	ThinkingLevel       string
	CustomArgs          []string
	Permission          string // Backward-compatible private/workspace.
	PermissionMode      string
	InvocationTargets   []InvocationTarget
	MaxConcurrentTasks  int
	ManageMCPConfig     bool
	MCPConfigFile       string
}

type SquadInput struct {
	Name         string
	Description  string
	Instructions string
	LeaderID     string
	AvatarURL    string
}

type RuntimeProfileInput struct {
	DisplayName    string
	ProtocolFamily string
	CommandName    string
	Description    string
	Enabled        bool
	FixedArgs      []string
	Visibility     string
}

type Change struct {
	Action string
	Kind   string
	Name   string
	Fields []string
}
