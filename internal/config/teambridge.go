package config

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

var teamBridgeUsername = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
var teamBridgeUserID = regexp.MustCompile(`^[a-z0-9]{26}$`)
var teamBridgeEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func loadTeamBridge(base string, p *model.Project, options LoadOptions) error {
	directories, err := discoverResourceDirectories(base, "teambridge", "bot.yaml")
	if err != nil {
		return err
	}
	for _, dir := range directories {
		var d model.TeamBridgeBotSpec
		if err := decodeStrictYAML(filepath.Join(dir, "bot.yaml"), &d); err != nil {
			return err
		}
		d.SourceDirectory, d.WithoutSecrets = dir, options.WithoutSecrets
		if err := normalizeTeamBridge(&d); err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(dir, "bot.yaml"), err)
		}
		if d.WithoutSecrets {
			// Do not even inspect token paths in a secretless declaration.
			d.AccessTokenFile, d.AccessTokenEnv = "", ""
		} else if d.AccessTokenFile != "" {
			path, err := teamBridgeTokenPath(dir, d.AccessTokenFile)
			if err != nil {
				return fmt.Errorf("TeamBridge bot %q accessTokenFile: %w", d.Name, err)
			}
			d.AccessTokenFile = filepath.ToSlash(path)
		}
		seen := map[string]bool{}
		for _, file := range d.DeniedReply.ImageFiles {
			if strings.Contains(file, "\\") {
				return fmt.Errorf("TeamBridge bot %q imageFiles must use portable forward-slash paths", d.Name)
			}
			path, err := resolvePath(dir, filepath.FromSlash(file))
			if err != nil {
				return fmt.Errorf("TeamBridge bot %q image: %w", d.Name, err)
			}
			if seen[path] {
				return fmt.Errorf("TeamBridge bot %q repeats an image file", d.Name)
			}
			seen[path] = true
			image, err := readTeamBridgeImage(path)
			if err != nil {
				return fmt.Errorf("TeamBridge bot %q image: %w", d.Name, err)
			}
			d.DeniedReply.Images = append(d.DeniedReply.Images, image)
		}
		p.TeamBridge = append(p.TeamBridge, d)
	}
	return nil
}

func normalizeTeamBridge(d *model.TeamBridgeBotSpec) error {
	d.Name, d.Agent = strings.TrimSpace(d.Name), strings.TrimSpace(d.Agent)
	if d.Name == "" || d.Agent == "" {
		return fmt.Errorf("TeamBridge name and agent are required")
	}
	u, err := url.Parse(strings.TrimSpace(d.ServerURL))
	if err != nil || u.Hostname() == "" || strings.ToLower(u.Scheme) != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("TeamBridge serverUrl must be an absolute HTTPS URL without credentials, query or fragment")
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if u.Path != "" && u.Path != "/" && path.Clean(u.Path) != strings.TrimRight(u.Path, "/") {
		return fmt.Errorf("TeamBridge serverUrl must not contain noncanonical path segments")
	}
	if u.Port() == "443" {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	u.Path, u.RawPath = strings.TrimRight(u.Path, "/"), ""
	d.ServerURL = u.String()
	if d.BotUserID != "" && !teamBridgeUserID.MatchString(d.BotUserID) {
		return fmt.Errorf("TeamBridge botUserId must be a native 26-character account ID")
	}
	if d.BotUsername != "" && !teamBridgeUsername.MatchString(d.BotUsername) {
		return fmt.Errorf("TeamBridge botUsername must be a canonical username without @")
	}
	if d.Status == "" {
		d.Status = "active"
	}
	if d.Status != "active" && d.Status != "revoked" {
		return fmt.Errorf("TeamBridge status must be active or revoked")
	}
	if d.Context.Mode == "" {
		d.Context.Mode = "message"
	}
	switch d.Context.Mode {
	case "message", "thread":
		if d.Context.LookbackHours != 0 {
			return fmt.Errorf("TeamBridge message/thread context cannot have lookbackHours")
		}
	case "channel":
		switch d.Context.LookbackHours {
		case 0, 1, 6, 24, 72, 168, 720, 1440, 2160, 8760:
		default:
			return fmt.Errorf("TeamBridge channel lookbackHours must be 0, 1, 6, 24, 72, 168, 720, 1440, 2160 or 8760")
		}
	default:
		return fmt.Errorf("TeamBridge context.mode must be message, thread or channel")
	}
	if d.AccessMode == "" {
		d.AccessMode = "members"
	}
	switch d.AccessMode {
	case "members", "anyone":
		if len(d.AllowedUsers) != 0 {
			return fmt.Errorf("TeamBridge allowedUsers requires accessMode allowlist")
		}
	case "allowlist":
		if len(d.AllowedUsers) > 100 {
			return fmt.Errorf("TeamBridge allowedUsers supports at most 100 accounts")
		}
	default:
		return fmt.Errorf("TeamBridge accessMode must be members, anyone or allowlist")
	}
	users, ids := map[string]bool{}, map[string]bool{}
	for i := range d.AllowedUsers {
		user := &d.AllowedUsers[i]
		user.Username = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(user.Username), "@"))
		if !teamBridgeUsername.MatchString(user.Username) || users[user.Username] {
			return fmt.Errorf("TeamBridge allowedUsers contains an invalid or duplicate username")
		}
		if user.ID != "" && (!teamBridgeUserID.MatchString(user.ID) || ids[user.ID]) {
			return fmt.Errorf("TeamBridge allowedUsers contains an invalid or duplicate account ID")
		}
		users[user.Username], ids[user.ID] = true, true
	}
	if d.DeniedReply.Mode == "" {
		d.DeniedReply.Mode = "text"
	}
	switch d.DeniedReply.Mode {
	case "text":
		if len(d.DeniedReply.ImageFiles) != 0 || !utf8.ValidString(d.DeniedReply.Text) || utf8.RuneCountInString(d.DeniedReply.Text) > 4000 {
			return fmt.Errorf("TeamBridge text deniedReply must contain at most 4000 characters and no imageFiles")
		}
	case "images":
		if d.DeniedReply.Text != "" || len(d.DeniedReply.ImageFiles) == 0 {
			return fmt.Errorf("TeamBridge images deniedReply requires imageFiles and no text")
		}
	default:
		return fmt.Errorf("TeamBridge deniedReply.mode must be text or images")
	}
	if !d.WithoutSecrets {
		if d.AccessTokenEnv != "" && d.AccessTokenFile != "" {
			return fmt.Errorf("TeamBridge accessTokenEnv and accessTokenFile are mutually exclusive")
		}
		if d.AccessTokenEnv != "" && !teamBridgeEnvName.MatchString(d.AccessTokenEnv) {
			return fmt.Errorf("TeamBridge accessTokenEnv must be an environment variable name")
		}
	}
	return nil
}

func validateTeamBridgeReferences(p model.Project) error {
	agents := map[string]bool{}
	for _, a := range p.Agents {
		agents[a.Name] = true
	}
	names, bound, bots := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range p.TeamBridge {
		if names[d.Name] || bound[d.Agent] {
			return fmt.Errorf("duplicate TeamBridge name or more than one bot bound to agent %q", d.Agent)
		}
		if !agents[d.Agent] {
			return fmt.Errorf("TeamBridge bot %q references undeclared agent %q", d.Name, d.Agent)
		}
		if d.BotUserID != "" {
			key := strings.TrimPrefix(strings.TrimPrefix(d.ServerURL, "https://"), "http://") + "/" + d.BotUserID
			if bots[key] {
				return fmt.Errorf("TeamBridge bot identity is declared more than once")
			}
			bots[key] = true
		}
		names[d.Name], bound[d.Agent] = true, true
	}
	return nil
}

// Validate the reference without reading a token. Missing files are allowed so
// exported declarations remain valid before credentials are provisioned.
func teamBridgeTokenPath(base, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, "\\") {
		return "", fmt.Errorf("token file must be a non-empty relative path")
	}
	path := filepath.Clean(filepath.Join(base, filepath.FromSlash(value)))
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("token file must stay inside its declaration directory")
	}
	current := base
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return rel, nil
		}
		if err != nil {
			return "", fmt.Errorf("cannot inspect token file reference")
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) || (i == len(parts)-1 && !info.Mode().IsRegular()) {
			return "", fmt.Errorf("token file reference must use ordinary directories and a regular file, without symlinks")
		}
	}
	return rel, nil
}

func readTeamBridgeImage(path string) (model.TeamBridgeImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.TeamBridgeImage{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, model.MaxTeamBridgeImageBytes+1))
	if err != nil {
		return model.TeamBridgeImage{}, err
	}
	if len(data) > model.MaxTeamBridgeImageBytes {
		return model.TeamBridgeImage{}, fmt.Errorf("image exceeds 5 MiB")
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return model.TeamBridgeImage{}, fmt.Errorf("image must contain PNG, JPEG, GIF or WebP bytes")
	}
	return model.TeamBridgeImage{Filename: filepath.Base(path), ContentType: contentType, Data: data}, nil
}
