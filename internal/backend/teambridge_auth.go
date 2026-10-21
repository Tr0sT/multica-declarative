package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type teamBridgeConnection struct {
	baseURL, workspaceID, token string
	client                      *http.Client
}

// The optional adapter follows the same human profile selection as the CLI.
// Integration administration must never inherit the daemon owner's credentials.
func (c *CLI) teamBridgeConnection() (*teamBridgeConnection, error) {
	if teamBridgeTaskContext() {
		return nil, fmt.Errorf("TeamBridge administration is not available inside a Multica daemon task; run from a human admin profile")
	}
	var profile, workspaceID string
	if scope, ok := c.Runner.(scopeRunner); ok {
		profile, workspaceID = scope.profile, scope.workspaceID
	}
	if profile == "." || profile == ".." || filepath.IsAbs(profile) || strings.ContainsAny(profile, `/\\`) {
		return nil, fmt.Errorf("invalid Multica profile name for TeamBridge")
	}
	baseURL := strings.TrimSpace(os.Getenv("MULTICA_SERVER_URL"))
	token := strings.TrimSpace(os.Getenv("MULTICA_TOKEN"))
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(os.Getenv("MULTICA_WORKSPACE_ID"))
	}
	if strings.HasPrefix(token, "mat_") {
		return nil, fmt.Errorf("TeamBridge administration requires a human admin credential, not a task-scoped token")
	}
	if baseURL == "" || token == "" || workspaceID == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot locate the selected Multica profile for TeamBridge")
		}
		configPath := filepath.Join(home, ".multica", "config.json")
		if profile != "" {
			configPath = filepath.Join(home, ".multica", "profiles", profile, "config.json")
		}
		var config struct {
			ServerURL   string `json:"server_url"`
			WorkspaceID string `json:"workspace_id"`
			Token       string `json:"token"`
		}
		file, err := os.Open(configPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot read the selected Multica profile for TeamBridge")
		}
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(file, (1<<20)+1))
			_ = file.Close()
			if readErr != nil || len(data) > 1<<20 || json.Unmarshal(data, &config) != nil {
				return nil, fmt.Errorf("invalid selected Multica profile for TeamBridge")
			}
		}
		if baseURL == "" {
			baseURL = strings.TrimSpace(config.ServerURL)
		}
		if token == "" {
			token = strings.TrimSpace(config.Token)
		}
		if workspaceID == "" {
			workspaceID = strings.TrimSpace(config.WorkspaceID)
		}
	}
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("TeamBridge requires server_url and a human admin token in the selected Multica profile, or MULTICA_SERVER_URL and MULTICA_TOKEN")
	}
	if strings.HasPrefix(token, "mat_") || !teamBridgeHeaderValue(token) {
		return nil, fmt.Errorf("TeamBridge requires a valid human admin credential")
	}
	if workspaceID == "" {
		var workspace struct {
			ID string `json:"id"`
		}
		if err := c.runJSON(&workspace, "workspace", "get", "--output", "json"); err != nil {
			return nil, fmt.Errorf("cannot resolve the selected Multica workspace for TeamBridge; use --workspace-id")
		}
		workspaceID = workspace.ID
	}
	if !teamBridgeUUID.MatchString(workspaceID) {
		return nil, fmt.Errorf("TeamBridge requires a full workspace UUID; select one with --workspace-id")
	}
	normalized, err := teamBridgeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	client := http.Client{}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	if client.Timeout <= 0 {
		client.Timeout = c.Timeout
		if client.Timeout <= 0 {
			client.Timeout = defaultTimeout
		}
	}
	// Never forward credentials or a request body through a redirected API call.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	return &teamBridgeConnection{baseURL: normalized, workspaceID: workspaceID, token: token, client: &client}, nil
}

func teamBridgeTaskContext() bool {
	for _, key := range []string{"MULTICA_AGENT_ID", "MULTICA_TASK_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return true
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, ".multica", "daemon_task_context.json"))
		if err == nil {
			var marker struct {
				ManagedBy string `json:"managed_by"`
			}
			err := json.Unmarshal(data, &marker)
			if err == nil && marker.ManagedBy == "multica-daemon-task" {
				return true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func teamBridgeHeaderValue(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func teamBridgeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("invalid Multica server URL for TeamBridge; use a base URL without credentials, query or fragment")
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("invalid Multica server URL for TeamBridge; HTTP or HTTPS is required")
	}
	if u.Path == "/ws" {
		u.Path = ""
	}
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}
