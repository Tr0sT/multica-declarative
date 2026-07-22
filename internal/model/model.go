package model

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

type AgentSpec struct {
	Name               string
	Description        string
	Instructions       string
	ModelID            string
	Skills             []string
	RuntimeRef         string
	ThinkingLevel      string
	MaxConcurrentTasks int
	Permission         string
	CustomArgs         []string
	SourcePath         string
}

type Project struct {
	WorkspacePath    string
	RuntimeSelectors map[string]RuntimeSelector
	Skills           []SkillSpec
	Agents           []AgentSpec
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

type InvocationTarget struct {
	TargetType string  `json:"target_type"`
	TargetID   *string `json:"target_id"`
}

type SkillSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Agent struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Instructions       string             `json:"instructions"`
	RuntimeID          string             `json:"runtime_id"`
	Model              string             `json:"model"`
	ThinkingLevel      string             `json:"thinking_level"`
	CustomArgs         []string           `json:"custom_args"`
	PermissionMode     string             `json:"permission_mode"`
	InvocationTargets  []InvocationTarget `json:"invocation_targets"`
	MaxConcurrentTasks int                `json:"max_concurrent_tasks"`
	Skills             []SkillSummary     `json:"skills"`
}

type Runtime struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CustomName string `json:"custom_name"`
	Provider   string `json:"provider"`
	Status     string `json:"status"`
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
	Name               string
	Description        string
	Instructions       string
	RuntimeID          string
	Model              string
	ThinkingLevel      string
	CustomArgs         []string
	Permission         string
	MaxConcurrentTasks int
}

type Change struct {
	Action string
	Kind   string
	Name   string
	Fields []string
}
