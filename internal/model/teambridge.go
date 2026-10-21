package model

// TeamBridge is the Mattermost-compatible integration in the Tr0sT Multica fork.
// Its credentials and conversation state are not exposed by the public API.
const MaxTeamBridgeImageBytes = 5 << 20

type TeamBridgeContext struct {
	Mode          string `json:"mode" yaml:"mode"`
	LookbackHours int    `json:"lookback_hours" yaml:"lookbackHours"`
}

type TeamBridgeAllowedUser struct {
	ID       string `json:"id" yaml:"id,omitempty"`
	Username string `json:"username" yaml:"username"`
}

type TeamBridgeImage struct {
	Filename    string
	ContentType string
	Data        []byte
}

type TeamBridgeReplySpec struct {
	Mode       string            `yaml:"mode"`
	Text       string            `yaml:"text,omitempty"`
	ImageFiles []string          `yaml:"imageFiles,omitempty"`
	Images     []TeamBridgeImage `yaml:"-"`
}

type TeamBridgeBotSpec struct {
	Name            string                  `yaml:"name"`
	Agent           string                  `yaml:"agent"`
	ServerURL       string                  `yaml:"serverUrl"`
	BotUserID       string                  `yaml:"botUserId,omitempty"`
	BotUsername     string                  `yaml:"botUsername,omitempty"`
	Status          string                  `yaml:"status"`
	Context         TeamBridgeContext       `yaml:"context"`
	AccessMode      string                  `yaml:"accessMode"`
	AllowedUsers    []TeamBridgeAllowedUser `yaml:"allowedUsers,omitempty"`
	DeniedReply     TeamBridgeReplySpec     `yaml:"deniedReply"`
	AccessTokenEnv  string                  `yaml:"accessTokenEnv,omitempty"`
	AccessTokenFile string                  `yaml:"accessTokenFile,omitempty"`
	WithoutSecrets  bool                    `yaml:"-"`
	SourceDirectory string                  `yaml:"-"`
}

type TeamBridgeDeniedReply struct {
	Mode               string   `json:"mode"`
	Text               string   `json:"text,omitempty"`
	ImageAttachmentIDs []string `json:"image_attachment_ids,omitempty"`
}

// TeamBridgeInstallation holds every public installation field. IDs and audit
// timestamps are exported as source metadata, never assigned on another server.
type TeamBridgeInstallation struct {
	ID              string                  `json:"id"`
	WorkspaceID     string                  `json:"workspace_id"`
	AgentID         string                  `json:"agent_id"`
	ServerURL       string                  `json:"server_url"`
	BotUserID       string                  `json:"bot_user_id"`
	BotUsername     string                  `json:"bot_username"`
	Status          string                  `json:"status"`
	Context         TeamBridgeContext       `json:"context"`
	AccessMode      string                  `json:"access_mode"`
	AllowedUsers    []TeamBridgeAllowedUser `json:"allowed_users"`
	DeniedReply     TeamBridgeDeniedReply   `json:"denied_reply"`
	InstallerUserID string                  `json:"installer_user_id"`
	InstalledAt     string                  `json:"installed_at"`
	CreatedAt       string                  `json:"created_at"`
	UpdatedAt       string                  `json:"updated_at"`
}

type TeamBridgeBotInput struct {
	ServerURL        string
	AccessToken      string
	Context          TeamBridgeContext
	AccessMode       string
	AllowedUsernames []string
	DeniedReply      TeamBridgeDeniedReply
}
