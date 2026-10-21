package reconcile

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

type observedTeamBridge struct {
	actual   model.TeamBridgeInstallation
	change   model.Change
	install  bool
	token    string
	imageIDs []string
}

type teamBridgeState struct {
	ops  backend.TeamBridgeOperations
	bots map[string]observedTeamBridge
}

// Inspect completes all predictable checks before any resource is changed,
// including when the caller preflights an entire workspace set through Plan.
func (r Reconciler) inspectTeamBridge(p model.Project, s *inspection) error {
	if len(p.TeamBridge) == 0 {
		return nil
	}
	ops, ok := r.Backend.(backend.TeamBridgeOperations)
	if !ok {
		return fmt.Errorf("backend does not support the optional TeamBridge integration")
	}
	actual, err := ops.ListTeamBridgeInstallations()
	if err != nil {
		return fmt.Errorf("inspect TeamBridge: %w", err)
	}
	x := &teamBridgeState{ops: ops, bots: map[string]observedTeamBridge{}}
	s.teambridge = x
	byAgent := map[string]model.TeamBridgeInstallation{}
	seenIDs := map[string]bool{}
	for _, a := range actual {
		if a.ID == "" || a.AgentID == "" || seenIDs[a.ID] || byAgent[a.AgentID].ID != "" {
			return fmt.Errorf("TeamBridge list contains missing or duplicate installation/agent identities")
		}
		if a.Status != "active" && a.Status != "revoked" {
			return fmt.Errorf("TeamBridge installation has an unsupported status")
		}
		seenIDs[a.ID] = true
		byAgent[a.AgentID] = a
	}
	for _, d := range p.TeamBridge {
		if _, exists := x.bots[d.Name]; exists {
			return fmt.Errorf("duplicate TeamBridge declaration %q", d.Name)
		}
		_, declared := s.agentChanges[d.Agent]
		if !declared {
			return fmt.Errorf("TeamBridge bot %q references undeclared agent %q", d.Name, d.Agent)
		}
		agentID := s.agents[d.Agent].resource.ID
		a := byAgent[agentID]
		o := observedTeamBridge{actual: a, change: model.Change{Action: Create, Kind: "teambridge", Name: d.Name}}
		if a.ID != "" {
			if canonicalTeamBridgeURL(a.ServerURL) != canonicalTeamBridgeURL(d.ServerURL) || (d.BotUserID != "" && a.BotUserID != d.BotUserID) {
				return fmt.Errorf("TeamBridge bot %q is bound to a different server or bot account; replacement can erase account/chat bindings and must be performed explicitly in Multica", d.Name)
			}
			fields, ids, err := diffTeamBridge(d, a, ops)
			if err != nil {
				return fmt.Errorf("TeamBridge bot %q: %w", d.Name, err)
			}
			o.imageIDs = ids
			if d.Status != a.Status {
				fields = append(fields, "status")
			}
			o.change = makeChange("teambridge", d.Name, fields)
			if a.Status == "revoked" && d.Status == "revoked" && len(fields) != 0 {
				return fmt.Errorf("TeamBridge bot %q is revoked; Multica cannot change its settings without reactivation", d.Name)
			}
		} else {
			if d.Status == "revoked" {
				return fmt.Errorf("TeamBridge bot %q is a revoked source record; Multica cannot recreate it without temporarily activating it; omit the record or explicitly declare it active with a token", d.Name)
			}
			o.imageIDs = make([]string, len(d.DeniedReply.Images))
		}
		o.install = a.ID == "" || (a.Status == "revoked" && d.Status == "active")
		if o.install {
			for _, other := range actual {
				if other.Status == "active" && other.AgentID != agentID && d.BotUserID != "" && other.BotUserID == d.BotUserID && canonicalTeamBridgeURL(other.ServerURL) == canonicalTeamBridgeURL(d.ServerURL) {
					return fmt.Errorf("TeamBridge bot %q is already connected to another agent in this workspace", d.Name)
				}
			}
			for _, spec := range p.Agents {
				if spec.Name == d.Agent && spec.Archived {
					return fmt.Errorf("TeamBridge bot %q cannot be installed for an archived agent", d.Name)
				}
			}
			var err error
			o.token, err = teamBridgeToken(d, p.WithoutSecrets)
			if err != nil {
				return fmt.Errorf("TeamBridge bot %q: %w", d.Name, err)
			}
			in := teamBridgeInput(d, o.imageIDs)
			in.AccessToken = o.token
			if err := ops.VerifyTeamBridgeBot(in, d.BotUserID, d.AllowedUsers); err != nil {
				return fmt.Errorf("TeamBridge bot %q credential/account preflight: %w", d.Name, err)
			}
			if a.ID != "" {
				o.change.Fields = append(o.change.Fields, "reinstall")
			}
		} else if newUsers := newPinnedTeamBridgeUsers(d, a); len(newUsers) > 0 {
			// Existing names retain stable IDs in the fork. New names with an
			// exported ID require verification before granting access.
			token, err := teamBridgeToken(d, p.WithoutSecrets)
			if err != nil {
				return fmt.Errorf("TeamBridge bot %q adds an account with a pinned ID; supply its bot token to verify the account, or explicitly remove the ID to resolve by username: %w", d.Name, err)
			}
			in := teamBridgeInput(d, o.imageIDs)
			in.AccessToken = token
			// Existing grants deliberately retain their saved IDs after username
			// changes. Only newly introduced pinned names need a live lookup.
			in.AllowedUsernames = make([]string, 0, len(newUsers))
			for _, user := range newUsers {
				in.AllowedUsernames = append(in.AllowedUsernames, user.Username)
			}
			if err := ops.VerifyTeamBridgeBot(in, a.BotUserID, newUsers); err != nil {
				return fmt.Errorf("TeamBridge bot %q account preflight: %w", d.Name, err)
			}
		}
		x.bots[d.Name] = o
		s.changes = append(s.changes, o.change)
	}
	return nil
}

func canonicalTeamBridgeURL(value string) string { return strings.TrimRight(value, "/") }

func newPinnedTeamBridgeUsers(d model.TeamBridgeBotSpec, a model.TeamBridgeInstallation) []model.TeamBridgeAllowedUser {
	if d.AccessMode != "allowlist" {
		return nil
	}
	known := map[string]bool{}
	for _, u := range a.AllowedUsers {
		known[u.Username] = true
	}
	var added []model.TeamBridgeAllowedUser
	for _, u := range d.AllowedUsers {
		if u.ID != "" && !known[u.Username] {
			added = append(added, u)
		}
	}
	return added
}

func diffTeamBridge(d model.TeamBridgeBotSpec, a model.TeamBridgeInstallation, ops backend.TeamBridgeOperations) ([]string, []string, error) {
	fields := []string{}
	if d.Context != a.Context {
		fields = append(fields, "context")
	}
	if d.AccessMode != a.AccessMode {
		fields = append(fields, "accessMode")
	}
	wanted, got := []string{}, []string{}
	idsByName := map[string]string{}
	if d.AccessMode == "allowlist" {
		for _, u := range a.AllowedUsers {
			got = append(got, u.Username)
			idsByName[u.Username] = u.ID
		}
		for _, u := range d.AllowedUsers {
			wanted = append(wanted, u.Username)
			if actualID := idsByName[u.Username]; u.ID != "" && actualID != "" && u.ID != actualID {
				return nil, nil, fmt.Errorf("allowed user %q refers to a different account ID; the API cannot replace a pinned identity in place", u.Username)
			}
		}
	}
	slices.Sort(wanted)
	slices.Sort(got)
	if !slices.Equal(wanted, got) {
		fields = append(fields, "allowedUsers")
	}
	imageIDs := make([]string, len(d.DeniedReply.Images))
	replyChanged := d.DeniedReply.Mode != a.DeniedReply.Mode || d.DeniedReply.Text != a.DeniedReply.Text
	if d.DeniedReply.Mode == "images" {
		byContent := map[[32]byte][]string{}
		for _, id := range a.DeniedReply.ImageAttachmentIDs {
			image, err := ops.DownloadTeamBridgeImage(id)
			if err != nil {
				return nil, nil, fmt.Errorf("read current reply image: %w", err)
			}
			hash := sha256.Sum256(image.Data)
			byContent[hash] = append(byContent[hash], id)
		}
		for i, image := range d.DeniedReply.Images {
			hash := sha256.Sum256(image.Data)
			if matches := byContent[hash]; len(matches) > 0 {
				imageIDs[i] = matches[0]
				byContent[hash] = matches[1:]
			}
		}
		if !slices.Equal(imageIDs, a.DeniedReply.ImageAttachmentIDs) {
			replyChanged = true
		}
	}
	if replyChanged {
		fields = append(fields, "deniedReply")
	}
	return fields, imageIDs, nil
}

func teamBridgeInput(d model.TeamBridgeBotSpec, imageIDs []string) model.TeamBridgeBotInput {
	names := []string(nil)
	if d.AccessMode == "allowlist" {
		names = make([]string, 0, len(d.AllowedUsers))
		for _, u := range d.AllowedUsers {
			names = append(names, u.Username)
		}
	}
	return model.TeamBridgeBotInput{
		ServerURL: d.ServerURL, Context: d.Context, AccessMode: d.AccessMode,
		AllowedUsernames: names,
		DeniedReply:      model.TeamBridgeDeniedReply{Mode: d.DeniedReply.Mode, Text: d.DeniedReply.Text, ImageAttachmentIDs: imageIDs},
	}
}

func (r Reconciler) applyTeamBridge(p model.Project, s inspection, agentIDs map[string]string, report func(model.Change)) error {
	if s.teambridge == nil {
		return nil
	}
	ops := s.teambridge.ops
	for _, d := range p.TeamBridge {
		o := s.teambridge.bots[d.Name]
		if o.change.Action == Noop {
			report(o.change)
			continue
		}
		imageIDs := append([]string(nil), o.imageIDs...)
		for i, image := range d.DeniedReply.Images {
			if imageIDs[i] == "" {
				id, err := ops.UploadTeamBridgeImage(image)
				if err != nil {
					return fmt.Errorf("TeamBridge bot %q upload reply image: %w", d.Name, err)
				}
				if id == "" {
					return fmt.Errorf("TeamBridge bot %q image upload returned no attachment ID", d.Name)
				}
				imageIDs[i] = id
			}
		}
		in := teamBridgeInput(d, imageIDs)
		if o.install {
			id := agentIDs[d.Agent]
			if id == "" {
				return fmt.Errorf("TeamBridge bot %q target agent has no ID", d.Name)
			}
			in.AccessToken = o.token
			installed, err := ops.InstallTeamBridgeBot(id, in)
			if err != nil {
				return fmt.Errorf("install TeamBridge bot %q: %w", d.Name, err)
			}
			if err := verifyInstalledTeamBridge(d, id, installed); err != nil {
				if installed.ID != "" && installed.AgentID == id {
					if revokeErr := ops.RevokeTeamBridgeBot(installed.ID); revokeErr != nil {
						return fmt.Errorf("TeamBridge bot %q: %w; could not revoke the inconsistent installation: %w", d.Name, err, revokeErr)
					}
				}
				return fmt.Errorf("TeamBridge bot %q: %w; the inconsistent installation was revoked when identifiable", d.Name, err)
			}
		} else {
			if hasAnyField(o.change.Fields, "context", "accessMode", "allowedUsers", "deniedReply") {
				if err := ops.UpdateTeamBridgeBot(o.actual.ID, in); err != nil {
					return fmt.Errorf("update TeamBridge bot %q: %w", d.Name, err)
				}
			}
			if d.Status == "revoked" && o.actual.Status != "revoked" {
				if err := ops.RevokeTeamBridgeBot(o.actual.ID); err != nil {
					return fmt.Errorf("revoke TeamBridge bot %q: %w", d.Name, err)
				}
			}
		}
		report(o.change)
	}
	return nil
}

func verifyInstalledTeamBridge(d model.TeamBridgeBotSpec, agentID string, a model.TeamBridgeInstallation) error {
	if a.ID == "" || a.AgentID != agentID || a.Status != "active" || canonicalTeamBridgeURL(a.ServerURL) != canonicalTeamBridgeURL(d.ServerURL) || (d.BotUserID != "" && a.BotUserID != d.BotUserID) {
		return fmt.Errorf("server returned an inconsistent bot identity")
	}
	byName := map[string]string{}
	for _, u := range a.AllowedUsers {
		byName[u.Username] = u.ID
	}
	if d.AccessMode != a.AccessMode {
		return fmt.Errorf("server returned inconsistent access settings")
	}
	if d.AccessMode != "allowlist" {
		return nil
	}
	if len(d.AllowedUsers) != len(a.AllowedUsers) {
		return fmt.Errorf("server returned inconsistent allowed accounts")
	}
	for _, u := range d.AllowedUsers {
		id, ok := byName[u.Username]
		if !ok || (u.ID != "" && id != u.ID) {
			return fmt.Errorf("server returned a different allowed account identity")
		}
	}
	return nil
}

func teamBridgeToken(d model.TeamBridgeBotSpec, omit bool) (string, error) {
	if omit || d.WithoutSecrets {
		return "", fmt.Errorf("creating or reactivating a bot requires a token; secrets: omit/--without-secrets leaves credentials unmanaged")
	}
	if d.AccessTokenEnv != "" && d.AccessTokenFile != "" {
		return "", fmt.Errorf("choose accessTokenEnv or accessTokenFile")
	}
	var token string
	switch {
	case d.AccessTokenEnv != "":
		token = os.Getenv(d.AccessTokenEnv)
	case d.AccessTokenFile != "":
		clean := filepath.Clean(d.AccessTokenFile)
		if d.SourceDirectory == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("accessTokenFile must be a contained relative file")
		}
		current := d.SourceDirectory
		for _, part := range strings.Split(clean, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil {
				return "", fmt.Errorf("accessTokenFile is unavailable")
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("accessTokenFile must not traverse symlinks")
			}
		}
		file, err := os.Open(current)
		if err != nil {
			return "", fmt.Errorf("accessTokenFile is unreadable")
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("accessTokenFile must be a regular file")
		}
		data, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
		if err != nil || len(data) > 16*1024 {
			return "", fmt.Errorf("accessTokenFile must contain at most 16 KiB")
		}
		token = string(bytes.TrimSpace(data))
	default:
		return "", fmt.Errorf("the API does not export bot tokens; provide accessTokenEnv or accessTokenFile to install this bot")
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 16*1024 || strings.ContainsAny(token, "\r\n\x00") {
		return "", fmt.Errorf("bot token is missing or invalid; populate its accessTokenEnv/accessTokenFile without including secrets in command arguments")
	}
	return token, nil
}
