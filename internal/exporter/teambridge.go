package exporter

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"gopkg.in/yaml.v3"
)

type exportedTeamBridge struct {
	directory string
	document  model.TeamBridgeBotSpec
	observed  model.TeamBridgeInstallation
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
	s.warnings = append(s.warnings, "TeamBridge bot tokens are not exposed by the server and cannot be exported; existing tokens are retained on import, while new installations need a separately supplied accessTokenEnv or accessTokenFile. Source audit metadata, account/chat bindings, and conversation/runtime state are not restored.")
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
			AccessMode: item.AccessMode, AllowedUsers: append([]model.TeamBridgeAllowedUser(nil), item.AllowedUsers...),
			DeniedReply: model.TeamBridgeReplySpec{Mode: item.DeniedReply.Mode, Text: item.DeniedReply.Text},
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
		if !options.WithoutSecrets {
			// A reference is portable; there is deliberately no credential value.
			d.AccessTokenEnv = teamBridgeTokenEnv(item)
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
		s.teambridge = append(s.teambridge, exportedTeamBridge{directory: dir, document: d, observed: item})
	}
	return nil
}

func teamBridgeTokenEnv(item model.TeamBridgeInstallation) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, strings.ToUpper(item.BotUsername))
	if name == "" {
		name = "BOT"
	}
	digest := sha256.Sum256([]byte(item.ServerURL + "\n" + item.BotUserID))
	return fmt.Sprintf("MULTICA_TEAMBRIDGE_%s_%X_TOKEN", name, digest[:4])
}

func preserveTeamBridgeDirectories(target string, s *snapshot) error {
	if !s.teambridgeEnabled {
		if _, err := os.Lstat(filepath.Join(target, "teambridge")); err == nil {
			return fmt.Errorf("output already contains TeamBridge declarations; refresh with --with-teambridge or use a new destination to avoid losing or leaving stale integration settings")
		} else if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := preserveResourceDirectories(target, "teambridge", "bot.yaml", s.teambridge,
		func(v *exportedTeamBridge) (string, *string) { return v.document.Name, &v.directory }, yamlName); err != nil {
		return err
	}
	if s.manifest.Secrets == "omit" {
		return nil
	}
	// The server cannot reproduce credentials supplied in local files. Never
	// delete their only local copy during a full refresh, including for a bot
	// that is no longer present on the server. Omission is an explicit opt-out.
	existing, err := existingResourceDirectories(target, "teambridge", "bot.yaml", yamlName)
	if err != nil {
		return err
	}
	refs := map[string]string{}
	for name, dir := range existing {
		data, err := os.ReadFile(filepath.Join(target, "teambridge", dir, "bot.yaml"))
		if err != nil {
			return err
		}
		var reference struct {
			AccessTokenEnv  string `yaml:"accessTokenEnv"`
			AccessTokenFile string `yaml:"accessTokenFile"`
		}
		if err := yaml.Unmarshal(data, &reference); err != nil {
			return fmt.Errorf("read existing TeamBridge credential reference for %q: %w", name, err)
		}
		if reference.AccessTokenFile != "" {
			return fmt.Errorf("TeamBridge bot %q references a local accessTokenFile that the server cannot export; preserve it outside the generated tree and switch to accessTokenEnv before refreshing, or explicitly use --without-secrets to discard local secret files", name)
		}
		refs[name] = reference.AccessTokenEnv
	}
	for i := range s.teambridge {
		if ref := refs[s.teambridge[i].document.Name]; ref != "" {
			s.teambridge[i].document.AccessTokenEnv = ref
		}
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
		if err := writeYAML(filepath.Join(dir, "bot.yaml"), v.document); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(dir, "observed.json"), v.observed); err != nil {
			return err
		}
	}
	return nil
}
