package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
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

// The optional integration uses the fork's CLI just like core resources. The
// CLI owns authentication, workspace selection and all network communication.
func (c *CLI) ListTeamBridgeInstallations() ([]model.TeamBridgeInstallation, error) {
	var wire struct {
		Installations    *[]json.RawMessage `json:"installations"`
		Configured       *bool              `json:"configured"`
		InstallSupported *bool              `json:"install_supported"`
	}
	if err := c.teamBridgeJSON(&wire, "list", "--output", "json"); err != nil {
		return nil, err
	}
	if wire.Installations == nil || wire.Configured == nil || wire.InstallSupported == nil {
		return nil, fmt.Errorf("TeamBridge CLI returned an incomplete capability response")
	}
	if !*wire.Configured || !*wire.InstallSupported {
		return nil, fmt.Errorf("TeamBridge integration is disabled or installation is unsupported on this Multica server; configure the fork's integration before using this extension")
	}
	result := make([]model.TeamBridgeInstallation, 0, len(*wire.Installations))
	ids, agents := map[string]bool{}, map[string]bool{}
	workspaceID := c.teamBridgeExplicitWorkspace()
	for _, raw := range *wire.Installations {
		item, err := decodeTeamBridgeInstallation(raw, workspaceID)
		if err != nil {
			return nil, err
		}
		if ids[item.ID] || agents[item.AgentID] {
			return nil, fmt.Errorf("TeamBridge CLI returned duplicate installation or agent identities")
		}
		ids[item.ID], agents[item.AgentID] = true, true
		// When workspace selection belongs to the CLI profile, still reject a
		// response that mixes installations from multiple workspaces.
		workspaceID = item.WorkspaceID
		result = append(result, item)
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
	var raw json.RawMessage
	if err := c.teamBridgeConfigJSON(&raw, body, "install", "--agent", agentID); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	item, err := decodeTeamBridgeInstallation(raw, c.teamBridgeExplicitWorkspace())
	if err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	if item.AgentID != agentID || item.Status != "active" {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge install returned an unexpected agent or inactive installation")
	}
	return item, nil
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
	var raw json.RawMessage
	if err := c.teamBridgeConfigJSON(&raw, body, "update", id); err != nil {
		return err
	}
	item, err := decodeTeamBridgeInstallation(raw, c.teamBridgeExplicitWorkspace())
	if err != nil {
		return err
	}
	if item.ID != id || item.Status != "active" {
		return fmt.Errorf("TeamBridge update returned an unexpected or inactive installation")
	}
	if input.AttachmentMode != nil && (item.AttachmentMode == nil || *item.AttachmentMode != *input.AttachmentMode) {
		return fmt.Errorf("TeamBridge update did not preserve attachmentMode; check fork CLI/server compatibility (the request may have applied other settings)")
	}
	return nil
}

func (c *CLI) RevokeTeamBridgeBot(id string) error {
	if !teamBridgeUUID.MatchString(id) {
		return fmt.Errorf("TeamBridge revoke requires a full installation UUID")
	}
	var response struct {
		ID      string `json:"id"`
		Revoked bool   `json:"revoked"`
	}
	if err := c.teamBridgeJSON(&response, "revoke", id, "--output", "json"); err != nil {
		return err
	}
	if response.ID != id || !response.Revoked {
		return fmt.Errorf("TeamBridge CLI did not confirm the requested revocation")
	}
	return nil
}

// Verification is a CLI read-only operation. Credentials travel in a private
// config file, and the CLI verifies native bot/account IDs before installation.
func (c *CLI) VerifyTeamBridgeBot(input model.TeamBridgeBotInput, expectedBotID string, expectedUsers []model.TeamBridgeAllowedUser) error {
	if err := validateTeamBridgeServerURL(input.ServerURL); err != nil {
		return err
	}
	if !teamBridgeHeaderValue(input.AccessToken) {
		return fmt.Errorf("TeamBridge verification requires a bot token supplied through an environment variable or file")
	}
	if expectedBotID != "" && !teamBridgeUserID.MatchString(expectedBotID) {
		return fmt.Errorf("TeamBridge verification received an invalid expected bot ID")
	}
	verificationInput := input
	verificationInput.DeniedReply = model.TeamBridgeDeniedReply{Mode: "text"}
	settings, err := teamBridgeSettingsBody(verificationInput)
	if err != nil {
		return err
	}
	names, _ := settings["allowed_usernames"].([]string)
	expected := map[string]model.TeamBridgeAllowedUser{}
	for _, user := range expectedUsers {
		user.Username = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(user.Username), "@"))
		if !teamBridgeUsername.MatchString(user.Username) || (user.ID != "" && !teamBridgeUserID.MatchString(user.ID)) {
			return fmt.Errorf("TeamBridge verification received an invalid expected account")
		}
		if _, exists := expected[user.Username]; exists {
			return fmt.Errorf("TeamBridge verification received duplicate expected accounts")
		}
		expected[user.Username] = user
	}
	users := make([]model.TeamBridgeAllowedUser, 0, len(names))
	for _, name := range names {
		user := expected[name]
		user.Username = name
		users = append(users, user)
		delete(expected, name)
	}
	if len(expected) != 0 {
		return fmt.Errorf("TeamBridge expected accounts do not match the requested allowlist")
	}
	body := struct {
		ServerURL         string                        `json:"server_url"`
		AccessToken       string                        `json:"access_token"`
		ExpectedBotUserID string                        `json:"expected_bot_user_id,omitempty"`
		AllowedUsers      []model.TeamBridgeAllowedUser `json:"allowed_users"`
	}{input.ServerURL, strings.TrimSpace(input.AccessToken), expectedBotID, users}
	var response struct {
		Verified bool `json:"verified"`
	}
	if err := c.teamBridgeConfigJSON(&response, body, "verify"); err != nil {
		return err
	}
	if !response.Verified {
		return fmt.Errorf("TeamBridge CLI did not confirm bot/account verification")
	}
	return nil
}

func (c *CLI) teamBridgeExplicitWorkspace() string {
	if scoped, ok := c.Runner.(scopeRunner); ok {
		return scoped.workspaceID
	}
	return ""
}

// Never include command stderr, malformed JSON, or wrapped process errors in
// TeamBridge diagnostics: a CLI/server error can echo a credential or signed URL.
func (c *CLI) teamBridgeJSON(target any, args ...string) error {
	data, err := c.execute(append([]string{"teambridge"}, args...)...)
	if err != nil {
		message := strings.ToLower(err.Error())
		switch {
		case strings.Contains(message, "unknown command"):
			return fmt.Errorf("TeamBridge commands are unavailable in the selected Multica CLI; use --multica-bin with the compatible TeamBridge fork CLI")
		case teamBridgeFailureStatus(message, "401"):
			return fmt.Errorf("TeamBridge CLI authentication failed (HTTP 401); authenticate the selected Multica profile and check the supplied bot token for account verification")
		case teamBridgeFailureStatus(message, "403"):
			return fmt.Errorf("TeamBridge CLI was denied access (HTTP 403); check the selected profile, target workspace and required admin permissions")
		case teamBridgeFailureStatus(message, "404"):
			return fmt.Errorf("TeamBridge API or requested resource is unavailable (HTTP 404); use the compatible Multica fork server and CLI")
		case teamBridgeFailureStatus(message, "409"):
			return fmt.Errorf("TeamBridge installation conflict (HTTP 409); the bot may already be attached to another agent or workspace")
		case strings.Contains(message, "token does not belong to the expected active bot"):
			return fmt.Errorf("TeamBridge token does not belong to the expected active bot; supply the correct bot token before importing")
		case strings.Contains(message, "username does not identify the expected active account"):
			return fmt.Errorf("TeamBridge allowed username no longer identifies the exported account; check renamed or reused usernames before updating the declaration")
		case strings.Contains(message, "could not verify all teambridge accounts"):
			return fmt.Errorf("TeamBridge could not verify every allowed account; check usernames, account status and bot lookup permissions")
		case strings.Contains(message, "integration is disabled or installation is unsupported"):
			return fmt.Errorf("TeamBridge integration is disabled or installation is unsupported on this Multica server; configure the fork's integration before importing")
		case strings.Contains(message, "timed out"):
			return fmt.Errorf("TeamBridge CLI command timed out")
		default:
			return fmt.Errorf("TeamBridge CLI command failed; check fork compatibility, the selected profile, workspace permissions and configuration")
		}
	}
	if len(data) == 0 || len(data) > 8<<20 || json.Unmarshal(data, target) != nil {
		return fmt.Errorf("TeamBridge CLI returned invalid or oversized JSON")
	}
	return nil
}

func teamBridgeFailureStatus(message, status string) bool {
	return strings.Contains(message, "http "+status) || strings.Contains(message, "returned "+status) || strings.Contains(message, "api error ("+status+")")
}

func (c *CLI) teamBridgeConfigJSON(target any, body any, args ...string) error {
	data, err := json.Marshal(body)
	if err != nil || len(data) > 32<<10 {
		return fmt.Errorf("TeamBridge command configuration is invalid or exceeds 32 KiB")
	}
	dir, err := teamBridgeTempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "config.json")
	if err := teamBridgeWritePrivate(file, data); err != nil {
		return err
	}
	args = append(args, "--config-file", file, "--output", "json")
	return c.teamBridgeJSON(target, args...)
}

func teamBridgeTempDir() (string, error) {
	dir, err := os.MkdirTemp("", "multica-teambridge-*")
	if err != nil {
		return "", fmt.Errorf("cannot create private TeamBridge command directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("cannot protect TeamBridge command directory")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("cannot resolve TeamBridge command directory")
	}
	return absolute, nil
}

func teamBridgeWritePrivate(file string, data []byte) error {
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("cannot create private TeamBridge command input")
	}
	_, writeErr := out.Write(data)
	closeErr := out.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("cannot write private TeamBridge command input")
	}
	return nil
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

func teamBridgeSettingsBody(input model.TeamBridgeBotInput) (map[string]any, error) {
	if err := model.ValidateTeamBridgeAttachmentMode(input.AttachmentMode); err != nil {
		return nil, err
	}
	if err := validateTeamBridgeContext(input.Context); err != nil {
		return nil, err
	}
	if err := validateTeamBridgeReply(input.DeniedReply); err != nil {
		return nil, err
	}
	body := map[string]any{"context": input.Context, "access_mode": input.AccessMode, "denied_reply": input.DeniedReply}
	if input.AttachmentMode != nil {
		body["attachment_mode"] = *input.AttachmentMode
	}
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
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned an invalid installation")
	}
	for _, key := range []string{"id", "workspace_id", "agent_id", "server_url", "bot_user_id", "bot_username", "status", "context", "access_mode", "allowed_users", "denied_reply", "installer_user_id", "installed_at", "created_at", "updated_at"} {
		if len(fields[key]) == 0 || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned an installation without required field %s", key)
		}
	}
	var installation model.TeamBridgeInstallation
	if json.Unmarshal(raw, &installation) != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned invalid installation field types")
	}
	if _, present := fields["attachment_mode"]; present && installation.AttachmentMode == nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned null attachment_mode; update the fork CLI/server")
	}
	if err := model.ValidateTeamBridgeAttachmentMode(installation.AttachmentMode); err != nil {
		return model.TeamBridgeInstallation{}, err
	}
	var wireContext struct {
		Mode                  string `json:"mode"`
		LookbackHours         *int   `json:"lookback_hours"`
		ExtendedLookbackHours *int   `json:"extended_lookback_hours"`
	}
	if json.Unmarshal(fields["context"], &wireContext) != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned invalid context settings")
	}
	if wireContext.Mode == "channel_extended" {
		if wireContext.ExtendedLookbackHours == nil || wireContext.LookbackHours != nil {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned incomplete extended history settings")
		}
		installation.Context = model.TeamBridgeContext{Mode: "channel", LookbackHours: *wireContext.ExtendedLookbackHours}
	} else if wireContext.ExtendedLookbackHours != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned ambiguous history settings")
	}
	var reply struct {
		Mode               string   `json:"mode"`
		Text               string   `json:"text"`
		ImageAttachmentID  string   `json:"image_attachment_id"`
		ImageAttachmentIDs []string `json:"image_attachment_ids"`
	}
	if json.Unmarshal(fields["denied_reply"], &reply) != nil {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned invalid denied reply settings")
	}
	if reply.Mode == "image" {
		if reply.ImageAttachmentID == "" || len(reply.ImageAttachmentIDs) != 0 {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned an incomplete single-image reply")
		}
		installation.DeniedReply.Mode = "images"
		installation.DeniedReply.ImageAttachmentIDs = []string{reply.ImageAttachmentID}
	} else if reply.ImageAttachmentID != "" {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned ambiguous denied reply images")
	}
	if !teamBridgeUUID.MatchString(installation.ID) || !teamBridgeUUID.MatchString(installation.AgentID) ||
		!teamBridgeUUID.MatchString(installation.InstallerUserID) || !teamBridgeUUID.MatchString(installation.WorkspaceID) ||
		(workspaceID != "" && installation.WorkspaceID != workspaceID) {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned invalid identities or a different workspace")
	}
	if !teamBridgeUserID.MatchString(installation.BotUserID) || !teamBridgeUsername.MatchString(installation.BotUsername) {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned an invalid bot identity")
	}
	if installation.Status != "active" && installation.Status != "revoked" {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned an unsupported installation status")
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
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned an unsupported access mode")
	}
	if len(installation.AllowedUsers) > 100 {
		return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned more than 100 allowed accounts")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, user := range installation.AllowedUsers {
		if !teamBridgeUserID.MatchString(user.ID) || !teamBridgeUsername.MatchString(user.Username) || ids[user.ID] || names[user.Username] {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned invalid or duplicate allowed accounts")
		}
		ids[user.ID], names[user.Username] = true, true
	}
	for _, timestamp := range []string{installation.InstalledAt, installation.CreatedAt, installation.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			return model.TeamBridgeInstallation{}, fmt.Errorf("TeamBridge CLI returned invalid installation timestamps")
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

type teamBridgeImageMetadata struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        *int64 `json:"size"`
	Path        string `json:"path"`
}

func (c *CLI) DownloadTeamBridgeImage(id string) (model.TeamBridgeImage, error) {
	if !teamBridgeUUID.MatchString(id) {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge image download requires a full attachment UUID")
	}
	dir, err := teamBridgeTempDir()
	if err != nil {
		return model.TeamBridgeImage{}, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "image")
	if err := teamBridgeWritePrivate(file, nil); err != nil {
		return model.TeamBridgeImage{}, err
	}
	var metadata teamBridgeImageMetadata
	if err := c.teamBridgeJSON(&metadata, "image", "download", id, "--output-file", file, "--output", "json"); err != nil {
		return model.TeamBridgeImage{}, err
	}
	if metadata.ID != id || metadata.Path != file || metadata.Size == nil || *metadata.Size <= 0 || *metadata.Size > model.MaxTeamBridgeImageBytes {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge CLI returned invalid image download metadata")
	}
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() != *metadata.Size {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge CLI output is not a regular image file of the expected size")
	}
	// Windows FileMode permission bits do not describe ACL access. Keep the
	// POSIX privacy check where those bits are meaningful.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge CLI image output is not private")
	}
	input, err := os.Open(file)
	if err != nil {
		return model.TeamBridgeImage{}, fmt.Errorf("cannot read TeamBridge CLI image output")
	}
	data, readErr := io.ReadAll(io.LimitReader(input, model.MaxTeamBridgeImageBytes+1))
	closeErr := input.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) != *metadata.Size {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge CLI image output is incomplete or oversized")
	}
	contentType, err := teamBridgeImageType(data)
	if err != nil {
		return model.TeamBridgeImage{}, err
	}
	filename, err := teamBridgeImageFilename(metadata.Filename)
	if err != nil {
		return model.TeamBridgeImage{}, err
	}
	if metadata.ContentType != contentType {
		return model.TeamBridgeImage{}, fmt.Errorf("TeamBridge CLI image content type does not match the file bytes")
	}
	return model.TeamBridgeImage{Filename: filename, ContentType: contentType, Data: data}, nil
}

func (c *CLI) UploadTeamBridgeImage(image model.TeamBridgeImage) (string, error) {
	contentType, err := teamBridgeImageType(image.Data)
	if err != nil {
		return "", err
	}
	if image.ContentType != "" && image.ContentType != contentType {
		return "", fmt.Errorf("TeamBridge image content type does not match its bytes")
	}
	filename, err := teamBridgeImageFilename(image.Filename)
	if err != nil {
		return "", err
	}
	dir, err := teamBridgeTempDir()
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, filename)
	if err := teamBridgeWritePrivate(file, image.Data); err != nil {
		return "", err
	}
	var metadata teamBridgeImageMetadata
	if err := c.teamBridgeJSON(&metadata, "image", "upload", file, "--output", "json"); err != nil {
		return "", err
	}
	if !teamBridgeUUID.MatchString(metadata.ID) || metadata.Size == nil || *metadata.Size != int64(len(image.Data)) || metadata.ContentType != contentType || metadata.Filename != filename {
		return "", fmt.Errorf("TeamBridge CLI returned invalid image upload metadata")
	}
	return metadata.ID, nil
}

func teamBridgeImageFilename(value string) (string, error) {
	filename := path.Base(strings.ReplaceAll(value, `\`, "/"))
	if filename == "" || filename == "." || filename == ".." || filename == "/" || strings.ContainsAny(filename, "\x00\r\n") {
		return "", fmt.Errorf("TeamBridge reply image requires a valid filename")
	}
	return filename, nil
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
