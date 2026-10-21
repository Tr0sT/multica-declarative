//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
	"gopkg.in/yaml.v3"
)

const (
	teamBridgeInstallationID = "00000000-0000-4000-8000-000000000051"
	teamBridgeAttachmentID   = "00000000-0000-4000-8000-000000000052"
	teamBridgeInstallerID    = "00000000-0000-4000-8000-000000000053"
	teamBridgeBotID          = "abcdefghijklmnopqrstuvwxyz"
	teamBridgeAllowedID      = "zzzzzzzzzzzzzzzzzzzzzzzzzz"
)

// Keep the ordinary resources on the pinned official CLI. Only the optional
// extension uses the HTTP adapter, against the same isolated loopback service.
type teamBridgeFixture struct {
	mu             sync.Mutex
	base           *fixture
	installation   map[string]any
	image          []byte
	requests       []request
	unavailable    bool
	missingImage   bool
	storedBotToken string
}

func newTeamBridgeFixture(t *testing.T) (*teamBridgeFixture, *backend.CLI) {
	t.Helper()
	base, cli := newFixture(t)
	var imageBytes bytes.Buffer
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	picture.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	picture.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 255})
	if err := png.Encode(&imageBytes, picture); err != nil {
		t.Fatal(err)
	}
	f := &teamBridgeFixture{
		base: base, image: imageBytes.Bytes(), storedBotToken: "synthetic-private-bot-token",
		installation: map[string]any{
			"id": teamBridgeInstallationID, "workspace_id": workspaceID, "agent_id": agentID,
			"server_url": "https://teambridge.example.test", "bot_user_id": teamBridgeBotID,
			"bot_username": "fixture-bot", "status": "active",
			"context":           map[string]any{"mode": "channel_extended", "extended_lookback_hours": 8760},
			"access_mode":       "allowlist",
			"allowed_users":     []any{map[string]any{"id": teamBridgeAllowedID, "username": "sergey"}},
			"denied_reply":      map[string]any{"mode": "image", "image_attachment_id": teamBridgeAttachmentID},
			"installer_user_id": teamBridgeInstallerID,
			"installed_at":      "2026-10-21T10:00:00Z", "created_at": "2026-10-21T10:00:00Z",
			"updated_at": "2026-10-21T10:00:00Z",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	home := t.TempDir()
	// Unlike the CLI runner, the optional adapter resolves its human profile in
	// the current process. Isolate both sides explicitly; never inherit an admin
	// profile, a daemon-task marker, a real access token, or proxy configuration.
	t.Chdir(home)
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", workspaceID)
	t.Setenv("MULTICA_TOKEN", "synthetic-integration-token")
	for _, name := range []string{"MULTICA_AGENT_ID", "MULTICA_TASK_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT"} {
		t.Setenv(name, "")
	}
	cli.Runner = isolatedRunner{dir: home, env: []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home,
		"XDG_CACHE_HOME=" + home, "MULTICA_SERVER_URL=" + server.URL,
		"MULTICA_WORKSPACE_ID=" + workspaceID, "MULTICA_TOKEN=synthetic-integration-token",
		"NO_PROXY=127.0.0.1,localhost",
	}}
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	cli.HTTPClient = &http.Client{Transport: transport}
	return f, cli
}

func (f *teamBridgeFixture) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "/mattermost/") && !strings.HasPrefix(r.URL.Path, "/api/attachments/") && r.URL.Path != "/api/upload-file" {
		f.base.serve(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	data := map[string]any{}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "invalid fixture body", http.StatusBadRequest)
			return
		}
	}
	f.requests = append(f.requests, request{r.Method, r.URL.RequestURI(), data})
	if r.Header.Get("Authorization") != "Bearer synthetic-integration-token" || r.Header.Get("X-Workspace-ID") != workspaceID {
		http.Error(w, "unexpected fixture identity", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	respond := func(value any) { _ = json.NewEncoder(w).Encode(value) }
	const installationsPath = "/api/workspaces/" + workspaceID + "/mattermost/installations"
	if f.unavailable && strings.HasPrefix(r.URL.Path, installationsPath) {
		http.NotFound(w, r)
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "GET " + installationsPath:
		respond(map[string]any{"installations": []any{f.installation}, "configured": true, "install_supported": true})
	case "PATCH " + installationsPath + "/" + teamBridgeInstallationID:
		// The fork's settings endpoint cannot rotate credentials or rebind an
		// installation. Reject such writes even if the implementation regresses.
		for _, name := range []string{"access_token", "server_url", "agent_id"} {
			if _, exists := data[name]; exists {
				http.Error(w, "settings write attempted to replace identity or secret", http.StatusBadRequest)
				return
			}
		}
		for _, name := range []string{"context", "access_mode", "denied_reply"} {
			f.installation[name] = data[name]
		}
		respond(f.installation)
	case "GET /api/attachments/" + teamBridgeAttachmentID:
		respond(map[string]any{
			"id": teamBridgeAttachmentID, "workspace_id": workspaceID,
			"filename": "denied.png", "size_bytes": len(f.image),
			"download_url": "/api/attachments/" + teamBridgeAttachmentID + "/download",
		})
	case "GET /api/attachments/" + teamBridgeAttachmentID + "/download":
		if f.missingImage {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(f.image)
	default:
		http.Error(w, "unexpected TeamBridge fixture request", http.StatusInternalServerError)
	}
}

func (f *teamBridgeFixture) mutations() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []request
	for _, value := range f.requests {
		if value.method != http.MethodGet {
			result = append(result, value)
		}
	}
	return result
}

func TestTeamBridgeWithOfficialCLIRoundTrip(t *testing.T) {
	for _, withoutSecrets := range []bool{false, true} {
		name := "with-token-reference"
		if withoutSecrets {
			name = "without-secrets"
		}
		t.Run(name, func(t *testing.T) {
			f, cli := newTeamBridgeFixture(t)
			dir := filepath.Join(t.TempDir(), "snapshot")
			result, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{
				OutputDir: dir, WithTeamBridge: true, WithoutSecrets: withoutSecrets,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.TeamBridge != 1 || !strings.Contains(strings.Join(result.Warnings, "\n"), "tokens") {
				t.Fatalf("unexpected extension result: %#v", result)
			}
			manifest := filepath.Join(dir, "multica.yaml")
			project, err := config.Load(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if len(project.TeamBridge) != 1 {
				t.Fatalf("lost integration declarations: %#v", project.TeamBridge)
			}
			bot := project.TeamBridge[0]
			if bot.Context != (model.TeamBridgeContext{Mode: "channel", LookbackHours: 8760}) ||
				bot.DeniedReply.Mode != "images" || len(bot.DeniedReply.Images) != 1 || !bytes.Equal(bot.DeniedReply.Images[0].Data, f.image) {
				t.Fatalf("extended context or single image was not normalized losslessly: %#v", bot)
			}
			if withoutSecrets != project.WithoutSecrets || (withoutSecrets && bot.AccessTokenEnv != "") || (!withoutSecrets && bot.AccessTokenEnv == "") {
				t.Fatal("secret policy or separate token reference was not preserved")
			}
			if len(bot.AllowedUsers) != 1 || bot.AllowedUsers[0] != (model.TeamBridgeAllowedUser{ID: teamBridgeAllowedID, Username: "sergey"}) {
				t.Fatal("allowed account identity was not preserved")
			}
			for path, data := range teamBridgeSnapshot(t, dir) {
				if bytes.Contains(data, []byte(f.storedBotToken)) {
					t.Fatalf("private bot token appeared in %s", path)
				}
			}
			assertNoop(t, cli, project)
			if err := (reconcile.Reconciler{Backend: cli}).Apply(project, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != 0 || len(f.base.mutations()) != 0 {
				t.Fatal("unchanged exported configuration caused mutations")
			}

			// Exercise the file format again before applying a real settings diff.
			bot.Context.LookbackHours = 24
			encoded, err := yaml.Marshal(bot)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bot.SourceDirectory, "bot.yaml"), encoded, 0644); err != nil {
				t.Fatal(err)
			}
			project, err = config.Load(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := (reconcile.Reconciler{Backend: cli}).Apply(project, nil); err != nil {
				t.Fatal(err)
			}
			writes := f.mutations()
			if len(writes) != 1 || writes[0].method != http.MethodPatch || !strings.HasSuffix(writes[0].path, "/"+teamBridgeInstallationID) {
				t.Fatalf("expected one settings PATCH, without reinstall/upload: %#v", writes)
			}
			if _, exists := writes[0].body["access_token"]; exists || f.storedBotToken != "synthetic-private-bot-token" {
				t.Fatal("settings update replaced the existing bot token")
			}
			if got := writes[0].body["allowed_usernames"]; !reflect.DeepEqual(got, []any{"sergey"}) {
				t.Fatalf("settings update changed allowed usernames: %#v", got)
			}
			assertNoop(t, cli, project)
			if err := (reconcile.Reconciler{Backend: cli}).Apply(project, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != 1 || len(f.base.mutations()) != 0 {
				t.Fatal("reapplying the updated snapshot did not converge")
			}
		})
	}
}

func TestTeamBridgeIsOptInWithOfficialCLI(t *testing.T) {
	f, cli := newTeamBridgeFixture(t)
	f.unavailable = true
	project, _ := exportProject(t, cli)
	assertNoop(t, cli, project)
	if err := (reconcile.Reconciler{Backend: cli}).Apply(project, nil); err != nil {
		t.Fatal(err)
	}
	if len(project.TeamBridge) != 0 || len(f.requests) != 0 || len(f.base.mutations()) != 0 {
		t.Fatal("ordinary upstream export/plan/apply contacted or mutated the optional integration")
	}
}

func TestTeamBridgeFailedRefreshPreservesSnapshot(t *testing.T) {
	for _, missing := range []string{"extension", "image"} {
		t.Run(missing, func(t *testing.T) {
			f, cli := newTeamBridgeFixture(t)
			dir := filepath.Join(t.TempDir(), "snapshot")
			ex := exporter.Exporter{Backend: cli}
			if _, err := ex.Export(exporter.Options{OutputDir: dir, WithTeamBridge: true}); err != nil {
				t.Fatal(err)
			}
			before := teamBridgeSnapshot(t, dir)
			f.mu.Lock()
			f.unavailable, f.missingImage = missing == "extension", missing == "image"
			f.mu.Unlock()
			_, err := ex.Export(exporter.Options{OutputDir: dir, WithTeamBridge: true, Force: true})
			if err == nil || !strings.Contains(err.Error(), "404") {
				t.Fatalf("expected explicit missing %s failure, got %v", missing, err)
			}
			if !reflect.DeepEqual(before, teamBridgeSnapshot(t, dir)) {
				t.Fatal("failed extension export replaced part of the previous snapshot")
			}
			if len(f.mutations()) != 0 || len(f.base.mutations()) != 0 {
				t.Fatal("failed export mutated Multica")
			}
		})
	}
}

func teamBridgeSnapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err == nil {
			result[rel] = data
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return result
}
