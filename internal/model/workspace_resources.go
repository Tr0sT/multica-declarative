package model

// WorkspaceProjectSpec is distinct from Project, which is the whole declaration.
// Names are logical references; IDs on children are optional in-place identity
// hints and are never sent as IDs when creating resources in another workspace.
type WorkspaceProjectSpec struct {
	Name            string                 `yaml:"name"`
	Description     string                 `yaml:"description,omitempty"`
	DescriptionFile string                 `yaml:"descriptionFile,omitempty"`
	Status          string                 `yaml:"status"`
	Icon            string                 `yaml:"icon,omitempty"`
	Priority        *string                `yaml:"priority,omitempty"`
	Lead            *ProjectLead           `yaml:"lead,omitempty"`
	StartDate       string                 `yaml:"startDate,omitempty"`
	DueDate         string                 `yaml:"dueDate,omitempty"`
	Resources       *[]ProjectResourceSpec `yaml:"resources,omitempty"`
}
type ProjectLead struct {
	Type  string `yaml:"type"`
	Agent string `yaml:"agent,omitempty"`
	ID    string `yaml:"id,omitempty"`
}
type ProjectResourceSpec struct {
	ID       string            `yaml:"id,omitempty"`
	Type     string            `yaml:"type"`
	Ref      map[string]string `yaml:"ref"`
	Label    string            `yaml:"label,omitempty"`
	Position int32             `yaml:"position"`
}
type WorkspaceProject struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	Icon          string `json:"icon"`
	Priority      string `json:"priority"`
	LeadType      string `json:"lead_type"`
	LeadID        string `json:"lead_id"`
	StartDate     string `json:"start_date"`
	DueDate       string `json:"due_date"`
	ResourceCount int    `json:"resource_count"`
}
type ProjectResource struct {
	ID       string            `json:"id"`
	Type     string            `json:"resource_type"`
	Ref      map[string]string `json:"resource_ref"`
	Label    string            `json:"label"`
	Position int32             `json:"position"`
}
type ProjectInput struct {
	Title, Description, Status, Icon, StartDate, DueDate string
	LeadID                                               string
}
type AutopilotSpec struct {
	Name               string                  `yaml:"name"`
	Description        string                  `yaml:"description,omitempty"`
	DescriptionFile    string                  `yaml:"descriptionFile,omitempty"`
	Agent              string                  `yaml:"agent,omitempty"`
	Squad              string                  `yaml:"squad,omitempty"`
	Project            string                  `yaml:"project,omitempty"`
	Mode               string                  `yaml:"mode"`
	Status             string                  `yaml:"status"`
	IssueTitleTemplate string                  `yaml:"issueTitleTemplate,omitempty"`
	Subscribers        *[]string               `yaml:"subscribers,omitempty"`
	Collaborators      *[]string               `yaml:"collaborators,omitempty"`
	Triggers           *[]AutopilotTriggerSpec `yaml:"triggers,omitempty"`
}
type WebhookEventFilter struct {
	Event   string   `json:"event" yaml:"event"`
	Actions []string `json:"actions,omitempty" yaml:"actions,omitempty"`
}
type AutopilotTriggerSpec struct {
	ID       string `yaml:"id,omitempty"`
	Kind     string `yaml:"kind"`
	Enabled  bool   `yaml:"enabled"`
	Cron     string `yaml:"cron,omitempty"`
	Timezone string `yaml:"timezone,omitempty"`
	Label    string `yaml:"label,omitempty"`
	// The current CLI can observe, but not configure these webhook properties.
	Provider         *string               `yaml:"provider,omitempty"`
	HasSigningSecret *bool                 `yaml:"hasSigningSecret,omitempty"`
	EventFilters     *[]WebhookEventFilter `yaml:"eventFilters,omitempty"`
}
type Autopilot struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	AssigneeType       string            `json:"assignee_type"`
	AssigneeID         string            `json:"assignee_id"`
	ProjectID          string            `json:"project_id"`
	Status             string            `json:"status"`
	Mode               string            `json:"execution_mode"`
	IssueTitleTemplate string            `json:"issue_title_template"`
	Subscribers        []AutopilotMember `json:"subscribers"`
}
type AutopilotMember struct {
	Type string `json:"user_type"`
	ID   string `json:"user_id"`
}
type AutopilotTrigger struct {
	ID               string               `json:"id"`
	Kind             string               `json:"kind"`
	Enabled          bool                 `json:"enabled"`
	Cron             string               `json:"cron_expression"`
	Timezone         string               `json:"timezone"`
	Label            string               `json:"label"`
	Provider         string               `json:"provider"`
	HasSigningSecret bool                 `json:"has_signing_secret"`
	EventFilters     []WebhookEventFilter `json:"event_filters"`
	// No webhook tokens, URLs, or signing secrets are decoded into the model.
}
type AutopilotDetail struct {
	Autopilot     Autopilot          `json:"autopilot"`
	Triggers      []AutopilotTrigger `json:"triggers"`
	Collaborators []AutopilotMember  `json:"collaborators"`
}
type AutopilotInput struct {
	Title, Description, AgentID, ProjectID, Status, Mode, IssueTitleTemplate string
	Subscribers                                                              *[]string
}
