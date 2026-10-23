package exporter

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

type exportedTeamBridge struct {
	directory string
	document  model.TeamBridgeBotSpec
	observed  model.TeamBridgeInstallation
	token     string
}

func (e Exporter) readTeamBridge(s *snapshot, agents map[string]string, options Options) error {
	ops, ok := e.Backend.(backend.TeamBridgeOperations)
	if !ok {
		return fmt.Errorf("backend does not support the optional TeamBridge integration")
	}
	items, err := ops.ListTeamBridgeInstallations()
	if err != nil {
		return fmt.Errorf("read TeamBridge installations: %w", err)
	}
	s.teambridgeEnabled = true
	s.warnings = append(s.warnings, "TeamBridge source audit metadata, account/chat bindings, and conversation/runtime state are not restored. This snapshot covers supported declarative configuration, not all deployment data.")
	sort.Slice(items, func(i, j int) bool { return agents[items[i].AgentID] < agents[items[j].AgentID] })
	seen, used := map[string]bool{}, map[string]struct{}{}
	for _, item := range items {
		name := agents[item.AgentID]
		if item.ID == "" || name == "" || seen[name] {
			return fmt.Errorf("TeamBridge installation has a missing/duplicate identity or an agent outside the exported snapshot")
		}
		seen[name] = true
		d := model.TeamBridgeBotSpec{
			Name: name, Agent: name, ServerURL: item.ServerURL, BotUserID: item.BotUserID,
			BotUsername: item.BotUsername, Status: item.Status, Context: item.Context,
			AttachmentMode: item.AttachmentMode,
			AccessMode:     item.AccessMode, AllowedUsers: append([]model.TeamBridgeAllowedUser(nil), item.AllowedUsers...),
			DeniedReply: model.TeamBridgeReplySpec{Mode: item.DeniedReply.Mode, Text: item.DeniedReply.Text},
		}
		if err := model.ValidateTeamBridgeAttachmentMode(item.AttachmentMode); err != nil {
			return fmt.Errorf("TeamBridge bot %q: %w", name, err)
		}
		if item.AttachmentMode == nil {
			s.warnings = append(s.warnings, fmt.Sprintf("TeamBridge bot %q: attachmentMode was not reported by the selected fork CLI/server and is left unmanaged; update both to export this setting", name))
		}
		if item.AccessMode != "allowlist" && len(d.AllowedUsers) > 0 {
			// The fork retains dormant grants when switching away from allowlist,
			// but rejects allowed_usernames on writes in other access modes.
			// Keep these observations in observed.json rather than an invalid or
			// misleading desired-state declaration.
			d.AllowedUsers = nil
			s.warnings = append(s.warnings, fmt.Sprintf("TeamBridge bot %q has dormant allowed-user entries while accessMode is %s; they are preserved in observed.json only and remain unmanaged until accessMode is allowlist", name, item.AccessMode))
		}
		dir := uniqueSlug(name, item.ID, used)
		var token string
		if !options.WithoutSecrets {
			secrets, ok := e.Backend.(backend.TeamBridgeTokenOperations)
			if !ok {
				return fmt.Errorf("TeamBridge token export requires the compatible fork CLI and server; use --without-secrets only for an explicitly incomplete, secretless snapshot")
			}
			token, err = secrets.ExportTeamBridgeToken(item.ID)
			if err != nil {
				return fmt.Errorf("read TeamBridge bot %q token: %w", name, err)
			}
			if !model.ValidTeamBridgeToken(token) {
				return fmt.Errorf("TeamBridge bot %q token is empty or invalid; no snapshot was replaced", name)
			}
			d.AccessTokenFile = teamBridgeGeneratedToken
		}
		for i, id := range item.DeniedReply.ImageAttachmentIDs {
			if id == "" {
				return fmt.Errorf("TeamBridge bot %q has an empty image attachment ID", name)
			}
			image, err := ops.DownloadTeamBridgeImage(id)
			if err != nil {
				return fmt.Errorf("TeamBridge bot %q reply image %d: %w", name, i+1, err)
			}
			if len(image.Data) > model.MaxTeamBridgeImageBytes {
				return fmt.Errorf("TeamBridge bot %q reply image %d exceeds 5 MiB", name, i+1)
			}
			contentType := http.DetectContentType(image.Data)
			ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[contentType]
			if ext == "" {
				return fmt.Errorf("TeamBridge bot %q reply image %d is not a supported image", name, i+1)
			}
			file := fmt.Sprintf("images/%03d%s", i+1, ext)
			image.Filename, image.ContentType = filepath.Base(file), contentType
			d.DeniedReply.ImageFiles = append(d.DeniedReply.ImageFiles, file)
			d.DeniedReply.Images = append(d.DeniedReply.Images, image)
		}
		s.teambridge = append(s.teambridge, exportedTeamBridge{directory: dir, document: d, observed: item, token: token})
	}
	return nil
}

func writeTeamBridge(root string, s snapshot) error {
	if !s.teambridgeEnabled {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(root, "teambridge"), 0755); err != nil {
		return err
	}
	for _, v := range s.teambridge {
		dir := filepath.Join(root, "teambridge", v.directory)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		for i, image := range v.document.DeniedReply.Images {
			path := filepath.Join(dir, filepath.FromSlash(v.document.DeniedReply.ImageFiles[i]))
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(path, image.Data, 0644); err != nil {
				return err
			}
		}
		if v.token != "" {
			credentials := filepath.Join(dir, "credentials")
			if err := os.MkdirAll(credentials, 0700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, teamBridgeGeneratedToken), []byte(v.token), 0600); err != nil {
				return fmt.Errorf("cannot write generated TeamBridge token file")
			}
		}
		if err := writeYAML(filepath.Join(dir, "bot.yaml"), v.document); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(dir, "observed.json"), v.observed); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(dir, teamBridgeGeneratedManifest), teamBridgeOwnership{Version: 1, Files: generatedTeamBridgeFiles(v)}); err != nil {
			return err
		}
	}
	return preserveTeamBridgeFiles(root, s.teambridgePreserved)
}
