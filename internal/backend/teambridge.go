package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

var (
	teamBridgeUUID     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	teamBridgeUserID   = regexp.MustCompile(`^[a-z0-9]{26}$`)
	teamBridgeUsername = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

var _ TeamBridgeOperations = (*CLI)(nil)

func (c *CLI) ListTeamBridgeInstallations() ([]model.TeamBridgeInstallation, error) {
	connection, err := c.teamBridgeConnection()
	if err != nil {
		return nil, err
	}
	return connection.list()
}

func (c *teamBridgeConnection) installationsPath() string {
	return "/api/workspaces/" + c.workspaceID + "/mattermost/installations"
}

func (c *teamBridgeConnection) list() ([]model.TeamBridgeInstallation, error) {
	var wire struct {
		Installations    *[]json.RawMessage `json:"installations"`
		Configured       *bool              `json:"configured"`
		InstallSupported *bool              `json:"install_supported"`
	}
	if err := c.jsonRequest(http.MethodGet, c.installationsPath(), nil, &wire); err != nil {
		return nil, err
	}
	if wire.Installations == nil || wire.Configured == nil || wire.InstallSupported == nil {
		return nil, fmt.Errorf("TeamBridge API returned an incomplete capability response")
	}
	if !*wire.Configured || !*wire.InstallSupported {
		return nil, fmt.Errorf("TeamBridge integration is disabled or installation is unsupported on this Multica server; configure the fork's integration before using this extension")
	}
	result := make([]model.TeamBridgeInstallation, 0, len(*wire.Installations))
	ids, agents := map[string]bool{}, map[string]bool{}
	for _, raw := range *wire.Installations {
		installation, err := decodeTeamBridgeInstallation(raw, c.workspaceID)
		if err != nil {
			return nil, err
		}
		if ids[installation.ID] || agents[installation.AgentID] {
			return nil, fmt.Errorf("TeamBridge API returned duplicate installation or agent identities")
		}
		ids[installation.ID], agents[installation.AgentID] = true, true
		result = append(result, installation)
	}
	return result, nil
}

func (c *CLI) InstallTeamBridgeBot(agentID string, input model.TeamBridgeBotInput) (model.TeamBridgeInstallation, error) {
	if !teamBridgeUUID.MatchString(agentID) {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge install requires a full agent UUID")
	}
	if !teamBridgeHeaderValue(input.AccessToken) {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge install requires a bot access token supplied through an environment variable or file")
	}
	if err := validateTeamBridgeServerURL(input.ServerURL); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	body, err := teamBridgeSettingsBody(input)
	if err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	body["server_url"], body["access_token"] = input.ServerURL, strings.TrimSpace(input.AccessToken)
	connection, err := c.teamBridgeConnection()
	if err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	if _, err := connection.list(); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	var raw json.RawMessage
	endpoint := "/api/workspaces/" + connection.workspaceID + "/mattermost/install?agent_id=" + url.QueryEscape(agentID)
	if err := connection.jsonRequest(http.MethodPost, endpoint, body, &raw); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	installed, err := decodeTeamBridgeInstallation(raw, connection.workspaceID)
	if err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	if installed.AgentID != agentID || installed.Status != "active" {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge install returned an unexpected agent or inactive installation")
	}
	return installed, nil
}

// VerifyTeamBridgeBot performs read-only checks before a fresh installation.
// A saved username must not silently grant access to a different account after
// it has been renamed or reused on the same Mattermost server.
func (c *CLI) VerifyTeamBridgeBot(input model.TeamBridgeBotInput, expectedBotID string, expectedUsers []model.TeamBridgeAllowedUser) error {
	if teamBridgeTaskContext() {
		return fmt.Errorf("TeamBridge administration is not available inside a Multica daemon task")
	}
	if err := validateTeamBridgeServerURL(input.ServerURL); err != nil {
		return err
	}
	if !teamBridgeHeaderValue(input.AccessToken) {
		return fmt.Errorf("TeamBridge verification requires a bot token supplied through an environment variable or file")
	}
	if expectedBotID != "" && !teamBridgeUserID.MatchString(expectedBotID) {
		return fmt.Errorf("TeamBridge verification received an invalid expected bot ID")
	}
	// Images have not been uploaded during preflight, so their target IDs do
	// not exist yet. Only access/context validation is relevant to this lookup.
	verificationInput := input
	verificationInput.DeniedReply = model.TeamBridgeDeniedReply{Mode: "text"}
	settings, err := teamBridgeSettingsBody(verificationInput)
	if err != nil {
		return err
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
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	type user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		IsBot    bool   `json:"is_bot"`
		DeleteAt int64  `json:"delete_at"`
	}
	var me user
	if err := teamBridgeVerificationRequest(&client, input, http.MethodGet, "/users/me", nil, &me); err != nil {
		return err
	}
	if !me.IsBot || me.DeleteAt != 0 || !teamBridgeUserID.MatchString(me.ID) || !teamBridgeUsername.MatchString(me.Username) || (expectedBotID != "" && me.ID != expectedBotID) {
		return fmt.Errorf("TeamBridge token does not belong to the expected active bot account; nothing was changed")
	}
	if input.AccessMode != "allowlist" {
		return nil
	}
	names := settings["allowed_usernames"].([]string)
	if len(names) == 0 {
		if len(expectedUsers) != 0 {
			return fmt.Errorf("TeamBridge expected accounts do not match the requested empty allowlist")
		}
		return nil
	}
	var users []user
	if err := teamBridgeVerificationRequest(&client, input, http.MethodPost, "/users/usernames", names, &users); err != nil {
		return err
	}
	resolved := map[string]string{}
	ids := map[string]bool{}
	for _, account := range users {
		if account.IsBot || account.DeleteAt != 0 || !teamBridgeUserID.MatchString(account.ID) || !teamBridgeUsername.MatchString(account.Username) || resolved[account.Username] != "" || ids[account.ID] {
			return fmt.Errorf("TeamBridge lookup returned invalid or duplicate allowed accounts; nothing was changed")
		}
		resolved[account.Username] = account.ID
		ids[account.ID] = true
	}
	if len(resolved) != len(names) {
		return fmt.Errorf("TeamBridge could not resolve every allowed username; nothing was changed")
	}
	for _, name := range names {
		if resolved[name] == "" {
			return fmt.Errorf("TeamBridge could not resolve every allowed username; nothing was changed")
		}
	}
	for _, expected := range expectedUsers {
		name := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(expected.Username), "@"))
		if resolved[name] == "" || (expected.ID != "" && resolved[name] != expected.ID) {
			return fmt.Errorf("TeamBridge allowed username no longer identifies the exported account; update the declaration after checking the account identity; nothing was changed")
		}
	}
	return nil
}

func teamBridgeVerificationRequest(client *http.Client, input model.TeamBridgeBotInput, method, endpoint string, body any, target any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cannot encode TeamBridge account verification request")
		}
	}
	request, err := http.NewRequest(method, strings.TrimRight(input.ServerURL, "/")+"/api/v4"+endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cannot construct TeamBridge account verification request")
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(input.AccessToken))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("cannot verify TeamBridge accounts; the configured TeamBridge server must be reachable from this machine; nothing was changed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("TeamBridge account verification failed (HTTP %d); nothing was changed", response.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, target) != nil {
		return fmt.Errorf("TeamBridge account verification returned invalid JSON; nothing was changed")
	}
	return nil
}

func (c *CLI) UpdateTeamBridgeBot(id string, input model.TeamBridgeBotInput) error {
	if !teamBridgeUUID.MatchString(id) {
		return fmt.Errorf("TeamBridge update requires a full installation UUID")
	}
	if input.AccessToken != "" {
		return fmt.Errorf("TeamBridge settings updates preserve the existing token; use the explicit installation operation to rotate it")
	}
	body, err := teamBridgeSettingsBody(input)
	if err != nil {
		return err
	}
	connection, err := c.teamBridgeConnection()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := connection.jsonRequest(http.MethodPatch, connection.installationsPath()+"/"+id, body, &raw); err != nil {
		return err
	}
	installation, err := decodeTeamBridgeInstallation(raw, connection.workspaceID)
	if err != nil {
		return err
	}
	if installation.ID != id || installation.Status != "active" {
		return fmt.Errorf("TeamBridge update returned an unexpected or inactive installation")
	}
	return nil
}

func (c *CLI) RevokeTeamBridgeBot(id string) error {
	if !teamBridgeUUID.MatchString(id) {
		return fmt.Errorf("TeamBridge revoke requires a full installation UUID")
	}
	connection, err := c.teamBridgeConnection()
	if err != nil {
		return err
	}
	return connection.jsonRequest(http.MethodDelete, connection.installationsPath()+"/"+id, nil, nil)
}

func teamBridgeSettingsBody(input model.TeamBridgeBotInput) (map[string]any, error) {
	if err := validateTeamBridgeContext(input.Context); err != nil {
		return nil, err
	}
	if err := validateTeamBridgeReply(input.DeniedReply); err != nil {
		return nil, err
	}
	body := map[string]any{"context": input.Context, "access_mode": input.AccessMode, "denied_reply": input.DeniedReply}
	switch input.AccessMode {
	case "members", "anyone":
		if len(input.AllowedUsernames) != 0 {
			return nil, fmt.Errorf("TeamBridge allowed usernames require allowlist access mode")
		}
	case "allowlist":
		if len(input.AllowedUsernames) > 100 {
			return nil, fmt.Errorf("TeamBridge allowlist supports at most 100 accounts")
		}
		names := make([]string, 0, len(input.AllowedUsernames))
		seen := map[string]bool{}
		for _, value := range input.AllowedUsernames {
			name := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "@"))
			if !teamBridgeUsername.MatchString(name) {
				return nil, fmt.Errorf("TeamBridge allowlist contains an invalid username")
			}
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
		body["allowed_usernames"] = names // An explicit [] blocks everyone; null is invalid.
	default:
		return nil, fmt.Errorf("TeamBridge access mode must be members, anyone or allowlist")
	}
	return body, nil
}

func decodeTeamBridgeInstallation(raw json.RawMessage, workspaceID string) (model.TeamBridgeInstallation, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned an invalid installation")
	}
	for _, key := range []string{"id", "workspace_id", "agent_id", "server_url", "bot_user_id", "bot_username", "status", "context", "access_mode", "allowed_users", "denied_reply", "installer_user_id", "installed_at", "created_at", "updated_at"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned an installation without required field %s", key)
		}
	}
	var installation model.TeamBridgeInstallation
	if json.Unmarshal(raw, &installation) != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned invalid installation field types")
	}
	var wireContext struct {
		Mode                  string `json:"mode"`
		LookbackHours         *int   `json:"lookback_hours"`
		ExtendedLookbackHours *int   `json:"extended_lookback_hours"`
	}
	if json.Unmarshal(fields["context"], &wireContext) != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned invalid context settings")
	}
	if wireContext.Mode == "channel_extended" {
		if wireContext.ExtendedLookbackHours == nil || wireContext.LookbackHours != nil {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned incomplete extended history settings")
		}
		installation.Context = model.TeamBridgeContext{Mode: "channel", LookbackHours: *wireContext.ExtendedLookbackHours}
	} else if wireContext.ExtendedLookbackHours != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned ambiguous history settings")
	}
	var reply struct {
		Mode               string   `json:"mode"`
		Text               string   `json:"text"`
		ImageAttachmentID  string   `json:"image_attachment_id"`
		ImageAttachmentIDs []string `json:"image_attachment_ids"`
	}
	if json.Unmarshal(fields["denied_reply"], &reply) != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned invalid denied reply settings")
	}
	if reply.Mode == "image" {
		if reply.ImageAttachmentID == "" || len(reply.ImageAttachmentIDs) != 0 {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned an incomplete single-image reply")
		}
		installation.DeniedReply.Mode = "images"
		installation.DeniedReply.ImageAttachmentIDs = []string{reply.ImageAttachmentID}
	} else if reply.ImageAttachmentID != "" {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned ambiguous denied reply images")
	}
	if !teamBridgeUUID.MatchString(installation.ID) || !teamBridgeUUID.MatchString(installation.AgentID) ||
		!teamBridgeUUID.MatchString(installation.InstallerUserID) || installation.WorkspaceID != workspaceID {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned invalid identities or a different workspace")
	}
	if !teamBridgeUserID.MatchString(installation.BotUserID) || !teamBridgeUsername.MatchString(installation.BotUsername) {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned an invalid bot identity")
	}
	if installation.Status != "active" && installation.Status != "revoked" {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned an unsupported installation status")
	}
	if err := validateTeamBridgeServerURL(installation.ServerURL); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	if err := validateTeamBridgeContext(installation.Context); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	if err := validateTeamBridgeReply(installation.DeniedReply); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	if installation.AccessMode != "members" && installation.AccessMode != "anyone" && installation.AccessMode != "allowlist" {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned an unsupported access mode")
	}
	if len(installation.AllowedUsers) > 100 {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned more than 100 allowed accounts")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, user := range installation.AllowedUsers {
		if !teamBridgeUserID.MatchString(user.ID) || !teamBridgeUsername.MatchString(user.Username) || ids[user.ID] || names[user.Username] {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned invalid or duplicate allowed accounts")
		}
		ids[user.ID], names[user.Username] = true, true
	}
	for _, timestamp := range []string{installation.InstalledAt, installation.CreatedAt, installation.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge API returned invalid installation timestamps")
		}
	}
	return installation, nil
}

func validateTeamBridgeContext(settings model.TeamBridgeContext) error {
	switch settings.Mode {
	case "message", "thread":
		if settings.LookbackHours == 0 {
			return nil
		}
	case "channel":
		switch settings.LookbackHours {
		case 0, 1, 6, 24, 72, 168, 720, 1440, 2160, 8760:
			return nil
		}
	}
	return fmt.Errorf("TeamBridge returned or received unsupported context settings")
}

func validateTeamBridgeReply(reply model.TeamBridgeDeniedReply) error {
	switch reply.Mode {
	case "text":
		if len(reply.ImageAttachmentIDs) == 0 && utf8.ValidString(reply.Text) && utf8.RuneCountInString(reply.Text) <= 4000 {
			return nil
		}
	case "images":
		if reply.Text == "" && len(reply.ImageAttachmentIDs) > 0 {
			seen := map[string]bool{}
			for _, id := range reply.ImageAttachmentIDs {
				if !teamBridgeUUID.MatchString(id) || seen[id] {
					return fmt.Errorf("TeamBridge denied reply contains invalid or duplicate image IDs")
				}
				seen[id] = true
			}
			return nil
		}
	}
	return fmt.Errorf("TeamBridge returned or received unsupported denied reply settings")
}

func validateTeamBridgeServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("TeamBridge bot server must be an HTTPS base URL without credentials, query or fragment")
	}
	return nil
}

func (c *teamBridgeConnection) jsonRequest(method, endpoint string, body any, target any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cannot encode TeamBridge API request")
		}
	}
	request, err := http.NewRequest(method, c.baseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cannot construct TeamBridge API request")
	}
	c.setHeaders(request)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("TeamBridge API request failed; check the Multica server connection")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return teamBridgeHTTPError(response.StatusCode)
	}
	if target == nil {
		if method == http.MethodDelete && response.StatusCode != http.StatusNoContent {
			return fmt.Errorf("TeamBridge revoke returned an unexpected success response; verify the installation status")
		}
		return nil
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 || json.Unmarshal(data, target) != nil {
		return fmt.Errorf("TeamBridge API returned an invalid or oversized JSON response")
	}
	return nil
}

func (c *teamBridgeConnection) setHeaders(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("X-Workspace-ID", c.workspaceID)
	request.Header.Set("X-Client-Platform", "cli")
	request.Header.Set("X-Client-Capabilities", "stable_attachment_urls")
}

func teamBridgeHTTPError(status int) error {
	switch status {
	case http.StatusNotFound:
		return fmt.Errorf("TeamBridge API or requested resource is unavailable (HTTP 404); this feature requires the compatible Multica fork")
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("TeamBridge API denied access (HTTP %d); check the selected profile and workspace admin permissions", status)
	case http.StatusConflict:
		return fmt.Errorf("TeamBridge API reported an installation conflict (HTTP 409); the bot may already belong to another agent or workspace")
	default:
		// Bodies and redirect URLs can echo credentials or signed object URLs.
		return fmt.Errorf("TeamBridge API request failed (HTTP %d)", status)
	}
}

func (c *CLI) DownloadTeamBridgeImage(id string) (model.TeamBridgeImage, error) {
	if !teamBridgeUUID.MatchString(id) {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge image download requires a full attachment UUID")
	}
	connection, err := c.teamBridgeConnection()
	if err != nil {
		return model.TeamBridgeImage{}, err
	}
	var metadata struct {
		ID            string  `json:"id"`
		WorkspaceID   string  `json:"workspace_id"`
		Filename      string  `json:"filename"`
		DownloadURL   string  `json:"download_url"`
		SizeBytes     *int64  `json:"size_bytes"`
		IssueID       *string `json:"issue_id"`
		CommentID     *string `json:"comment_id"`
		ChatSessionID *string `json:"chat_session_id"`
		ChatMessageID *string `json:"chat_message_id"`
		TaskID        *string `json:"task_id"`
	}
	if err := connection.jsonRequest(http.MethodGet, "/api/attachments/"+id, nil, &metadata); err != nil {
		return model.TeamBridgeImage{}, err
	}
	if metadata.ID != id || metadata.WorkspaceID != connection.workspaceID || metadata.SizeBytes == nil || *metadata.SizeBytes < 0 ||
		*metadata.SizeBytes > model.MaxTeamBridgeImageBytes || metadata.DownloadURL == "" || metadata.Filename == "" ||
		metadata.IssueID != nil || metadata.CommentID != nil || metadata.ChatSessionID != nil || metadata.ChatMessageID != nil || metadata.TaskID != nil {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge reply image metadata is invalid or not a standalone image in the selected workspace")
	}
	data, err := connection.downloadImage(metadata.DownloadURL)
	if err != nil {
		return model.TeamBridgeImage{}, err
	}
	if int64(len(data)) != *metadata.SizeBytes {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge reply image download size does not match its metadata")
	}
	image := model.TeamBridgeImage{Filename: path.Base(strings.ReplaceAll(metadata.Filename, `\`, "/")), Data: data}
	if image.Filename == "." || image.Filename == "/" || strings.ContainsAny(image.Filename, "\x00\r\n") {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge reply image has an invalid filename")
	}
	image.ContentType, err = teamBridgeImageType(image.Data)
	return image, err
}

func (c *teamBridgeConnection) downloadImage(rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("TeamBridge attachment has an invalid download URL")
	}
	relative := !u.IsAbs()
	if relative {
		if u.Host != "" || !strings.HasPrefix(rawURL, "/") || strings.HasPrefix(rawURL, "//") {
			return nil, fmt.Errorf("TeamBridge attachment has an invalid relative download URL")
		}
		u, err = url.Parse(c.baseURL + rawURL)
		if err != nil {
			return nil, fmt.Errorf("TeamBridge attachment has an invalid download URL")
		}
	}
	if err := validateTeamBridgeDownloadURL(u, strings.HasPrefix(c.baseURL, "https://")); err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot construct TeamBridge image download")
	}
	if relative {
		c.setHeaders(request)
	}
	client := *c.client
	client.Jar = nil
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many attachment redirects")
		}
		if err := validateTeamBridgeDownloadURL(next.URL, via[len(via)-1].URL.Scheme == "https"); err != nil {
			return err
		}
		// Signed downloads never need Multica credentials after a redirect.
		next.Header.Del("Authorization")
		next.Header.Del("X-Workspace-ID")
		next.Header.Del("Cookie")
		next.Header.Del("Referer")
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("TeamBridge image download failed; check attachment availability")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TeamBridge image download failed (HTTP %d)", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, model.MaxTeamBridgeImageBytes+1))
	if err != nil || len(data) > model.MaxTeamBridgeImageBytes {
		return nil, fmt.Errorf("TeamBridge image download failed or exceeds 5 MiB")
	}
	return data, nil
}

func validateTeamBridgeDownloadURL(u *url.URL, requireHTTPS bool) error {
	if u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (requireHTTPS && u.Scheme != "https") {
		return fmt.Errorf("TeamBridge attachment has an unsafe download URL")
	}
	return nil
}

func (c *CLI) UploadTeamBridgeImage(image model.TeamBridgeImage) (string, error) {
	if _, err := teamBridgeImageType(image.Data); err != nil {
		return "", err
	}
	filename := path.Base(strings.ReplaceAll(image.Filename, `\`, "/"))
	if filename == "" || filename == "." || filename == "/" || strings.ContainsAny(filename, "\x00\r\n") {
		return "", fmt.Errorf("TeamBridge reply image requires a valid filename")
	}
	connection, err := c.teamBridgeConnection()
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("cannot prepare TeamBridge image upload")
	}
	if _, err := part.Write(image.Data); err != nil {
		return "", fmt.Errorf("cannot prepare TeamBridge image upload")
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("cannot prepare TeamBridge image upload")
	}
	request, err := http.NewRequest(http.MethodPost, connection.baseURL+"/api/upload-file", &body)
	if err != nil {
		return "", fmt.Errorf("cannot construct TeamBridge image upload")
	}
	connection.setHeaders(request)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := connection.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("TeamBridge image upload failed; check the Multica server connection")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", teamBridgeHTTPError(response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	var metadata struct {
		ID          string `json:"id"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err != nil || len(data) > 64<<10 || json.Unmarshal(data, &metadata) != nil ||
		!teamBridgeUUID.MatchString(metadata.ID) || metadata.WorkspaceID != connection.workspaceID {
		return "", fmt.Errorf("TeamBridge image upload returned invalid attachment metadata")
	}
	return metadata.ID, nil
}

func teamBridgeImageType(data []byte) (string, error) {
	if len(data) == 0 || len(data) > model.MaxTeamBridgeImageBytes {
		return "", fmt.Errorf("TeamBridge reply images must be non-empty and at most 5 MiB")
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return contentType, nil
	}
	return "", fmt.Errorf("TeamBridge reply images must be PNG, JPEG, GIF or WebP")
}
