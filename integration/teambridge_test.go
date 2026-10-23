//go:build integration && teambridgeintegration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/app"
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
	teamBridgeUploadedID     = "00000000-0000-4000-8000-000000000054"
	teamBridgeBotID          = "abcdefghijklmnopqrstuvwxyz"
	teamBridgeAllowedID      = "zzzzzzzzzzzzzzzzzzzzzzzzzz"
)

// All requests, including the optional extension and image transfer, go through
// the fork CLI subprocess against an isolated loopback service.
type teamBridgeFixture struct {
	mu                   sync.Mutex
	base                 *fixture
	installation         map[string]any
	image                []byte
	requests             []request
	unavailable          bool
	missingImage         bool
	ignoreAttachmentMode bool
	storedBotToken       string
	validBotTokens       map[string]bool
	tokenFailureStatus   int
	installed            bool
	uploaded             []byte
	uploadedName         string
	remoteRequests       []request
}

func newTeamBridgeFixture(t *testing.T) (*teamBridgeFixture, *backend.CLI) {
	return newTeamBridgeTransportFixture(t, false)
}

func newTeamBridgeTransportFixture(t *testing.T, tls bool) (*teamBridgeFixture, *backend.CLI) {
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
		base: base, image: imageBytes.Bytes(), storedBotToken: "synthetic-private-bot-token", installed: true,
		validBotTokens: map[string]bool{"synthetic-private-bot-token": true},
		installation: map[string]any{
			"id": teamBridgeInstallationID, "workspace_id": workspaceID, "agent_id": agentID,
			"server_url": "https://teambridge.example.test", "bot_user_id": teamBridgeBotID,
			"bot_username": "fixture-bot", "status": "active",
			"context":           map[string]any{"mode": "channel_extended", "extended_lookback_hours": 8760},
			"access_mode":       "allowlist",
			"attachment_mode":   "links",
			"allowed_users":     []any{map[string]any{"id": teamBridgeAllowedID, "username": "sergey"}},
			"denied_reply":      map[string]any{"mode": "image", "image_attachment_id": teamBridgeAttachmentID},
			"installer_user_id": teamBridgeInstallerID,
			"installed_at":      "2026-10-21T10:00:00Z", "created_at": "2026-10-21T10:00:00Z",
			"updated_at": "2026-10-21T10:00:00Z",
		},
	}
	var server *httptest.Server
	if tls {
		server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
		f.installation["server_url"] = server.URL
	} else {
		server = httptest.NewServer(http.HandlerFunc(f.serve))
	}
	t.Cleanup(server.Close)
	home := t.TempDir()
	// The CLI owns authentication, workspace selection and daemon guards. Its
	// subprocess never inherits real profiles, task markers or credentials.
	runner := isolatedRunner{dir: home, env: []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home,
		"XDG_CACHE_HOME=" + home, "MULTICA_SERVER_URL=" + server.URL,
		"MULTICA_WORKSPACE_ID=" + workspaceID, "MULTICA_TOKEN=synthetic-integration-token",
		"NO_PROXY=127.0.0.1,localhost",
	}}
	if tls {
		cert := filepath.Join(home, "fixture-ca.pem")
		if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		runner.env = append(runner.env, "SSL_CERT_FILE="+cert, "SSL_CERT_DIR="+home)
	}
	cli.Runner = runner
	return f, cli
}

func (f *teamBridgeFixture) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "/mattermost/") && !strings.HasPrefix(r.URL.Path, "/api/attachments/") && !strings.HasPrefix(r.URL.Path, "/api/v4/") && r.URL.Path != "/api/upload-file" {
		f.base.serve(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/api/v4/") {
		f.serveRemoteAccounts(w, r)
		return
	}
	data := map[string]any{}
	if r.Body != nil && r.ContentLength > 0 && r.URL.Path != "/api/upload-file" {
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
	case "GET " + installationsPath + "/" + teamBridgeInstallationID + "/token":
		if f.tokenFailureStatus != 0 {
			http.Error(w, "synthetic-private-bot-token must never reach diagnostics", f.tokenFailureStatus)
			return
		}
		if !f.installed {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		respond(map[string]any{"installation_id": teamBridgeInstallationID, "workspace_id": workspaceID, "access_token": f.storedBotToken})
	case "PUT " + installationsPath + "/" + teamBridgeInstallationID + "/token":
		token, _ := data["access_token"].(string)
		if !f.installed || !f.validBotTokens[token] {
			http.Error(w, "invalid token for this bot", http.StatusBadRequest)
			return
		}
		updated := f.storedBotToken != token
		f.storedBotToken = token
		respond(map[string]any{"installation_id": teamBridgeInstallationID, "workspace_id": workspaceID, "updated": updated})
	case "GET " + installationsPath:
		installations := []any{}
		if f.installed {
			installations = append(installations, f.installation)
		}
		respond(map[string]any{"installations": installations, "configured": true, "install_supported": true})
	case "POST /api/workspaces/" + workspaceID + "/mattermost/install":
		if r.URL.Query().Get("agent_id") != agentID || data["access_token"] != f.storedBotToken {
			http.Error(w, "unexpected install identity or token", http.StatusBadRequest)
			return
		}
		f.installed = true
		f.installation["status"] = "active"
		for _, name := range []string{"server_url", "context", "access_mode", "denied_reply"} {
			f.installation[name] = data[name]
		}
		if mode, present := data["attachment_mode"]; present && !f.ignoreAttachmentMode {
			f.installation["attachment_mode"] = mode
		}
		respond(f.installation)
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
		if mode, present := data["attachment_mode"]; present && !f.ignoreAttachmentMode {
			f.installation["attachment_mode"] = mode
		}
		respond(f.installation)
	case "POST /api/upload-file":
		if !f.installed && len(f.remoteRequests) < 2 {
			http.Error(w, "installation must verify accounts before uploading", http.StatusBadRequest)
			return
		}
		part, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing upload", http.StatusBadRequest)
			return
		}
		defer part.Close()
		f.uploaded, err = io.ReadAll(part)
		if err != nil {
			http.Error(w, "unreadable upload", http.StatusBadRequest)
			return
		}
		f.uploadedName = header.Filename
		respond(map[string]any{"id": teamBridgeUploadedID, "workspace_id": workspaceID,
			"filename": f.uploadedName, "size_bytes": len(f.uploaded), "content_type": "image/png",
			"url": "/api/attachments/" + teamBridgeUploadedID + "/download"})
	case "GET /api/attachments/" + teamBridgeAttachmentID:
		respond(map[string]any{
			"id": teamBridgeAttachmentID, "workspace_id": workspaceID,
			"filename": "denied.png", "size_bytes": len(f.image),
			"download_url": "/api/attachments/" + teamBridgeAttachmentID + "/download",
		})
	case "GET /api/attachments/" + teamBridgeUploadedID:
		respond(map[string]any{"id": teamBridgeUploadedID, "workspace_id": workspaceID,
			"filename": f.uploadedName, "size_bytes": len(f.uploaded),
			"download_url": "/api/attachments/" + teamBridgeUploadedID + "/download"})
	case "GET /api/attachments/" + teamBridgeAttachmentID + "/download":
		if f.missingImage {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(f.image)
	case "GET /api/attachments/" + teamBridgeUploadedID + "/download":
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(f.uploaded)
	default:
		http.Error(w, "unexpected TeamBridge fixture request", http.StatusInternalServerError)
	}
}

func (f *teamBridgeFixture) serveRemoteAccounts(w http.ResponseWriter, r *http.Request) {
	f.remoteRequests = append(f.remoteRequests, request{method: r.Method, path: r.URL.Path})
	if !f.validBotTokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		http.Error(w, "unexpected bot credential", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v4/users/me":
		_ = json.NewEncoder(w).Encode(map[string]any{"id": teamBridgeBotID, "username": "fixture-bot", "is_bot": true, "delete_at": 0})
	case "POST /api/v4/users/usernames":
		var names []string
		if json.NewDecoder(r.Body).Decode(&names) != nil || !reflect.DeepEqual(names, []string{"sergey"}) {
			http.Error(w, "unexpected account lookup", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": teamBridgeAllowedID, "username": "sergey", "is_bot": false, "delete_at": 0}})
	default:
		http.Error(w, "unexpected remote account operation", http.StatusInternalServerError)
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

func TestForkCLITeamBridgeRoundTrip(t *testing.T) {
	for _, withoutSecrets := range []bool{false, true} {
		name := "with-token-value"
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
			if result.TeamBridge != 1 {
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
			if withoutSecrets != project.WithoutSecrets || bot.AccessTokenEnv != "" || (withoutSecrets && bot.AccessTokenFile != "") || (!withoutSecrets && bot.AccessTokenFile != "credentials/bot-token") {
				t.Fatal("secret policy or exported token file reference was not preserved")
			}
			if len(bot.AllowedUsers) != 1 || bot.AllowedUsers[0] != (model.TeamBridgeAllowedUser{ID: teamBridgeAllowedID, Username: "sergey"}) {
				t.Fatal("allowed account identity was not preserved")
			}
			for path, data := range teamBridgeSnapshot(t, dir) {
				if !withoutSecrets && strings.HasSuffix(filepath.ToSlash(path), "/credentials/bot-token") {
					if string(data) != f.storedBotToken {
						t.Fatal("the exported token value changed")
					}
					info, err := os.Stat(filepath.Join(dir, path))
					if err != nil || info.Mode().Perm() != 0600 {
						t.Fatal("the exported token file is not private")
					}
					continue
				}
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
			if withoutSecrets && teamBridgeTokenRequests(f) != 0 {
				t.Fatal("secretless export/import contacted token operations")
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

func TestForkCLITeamBridgeIsOptIn(t *testing.T) {
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

func TestForkCLITeamBridgeFailedRefreshPreservesSnapshot(t *testing.T) {
	for _, missing := range []string{"extension", "image", "token"} {
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
			if missing == "token" {
				f.tokenFailureStatus = http.StatusNotFound
			}
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

type teamBridgeSecretRunner struct {
	backend.Runner
	t           *testing.T
	secret      string
	configFiles []string
}

func (r *teamBridgeSecretRunner) Run(ctx context.Context, binary string, args ...string) ([]byte, []byte, error) {
	r.t.Helper()
	for i, arg := range args {
		if strings.Contains(arg, r.secret) {
			r.t.Fatal("a bot credential was passed through process arguments")
		}
		if (arg == "--config-file" || arg == "--token-file") && i+1 < len(args) {
			path := args[i+1]
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				r.t.Fatal("the CLI request file is unavailable or not private")
			}
			r.configFiles = append(r.configFiles, path)
		}
	}
	return r.Runner.Run(ctx, binary, args...)
}

func TestForkCLITeamBridgeInstallVerifiesTokenAndTransfersImage(t *testing.T) {
	f, cli := newTeamBridgeTransportFixture(t, true)
	dir := filepath.Join(t.TempDir(), "snapshot")
	if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, WithTeamBridge: true}); err != nil {
		t.Fatal(err)
	}
	project, err := config.Load(filepath.Join(dir, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.installed = false
	f.mu.Unlock()
	runner := &teamBridgeSecretRunner{Runner: cli.Runner, t: t, secret: f.storedBotToken}
	cli.Runner = runner
	r := reconcile.Reconciler{Backend: cli}
	changes, err := r.Plan(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != 0 || len(changes) == 0 || changes[len(changes)-1].Action != reconcile.Create {
		t.Fatal("creation plan did not remain read-only")
	}
	// Apply repeats the read-only credential preflight independently of Plan.
	f.mu.Lock()
	f.remoteRequests = nil
	f.mu.Unlock()
	if err := r.Apply(project, nil); err != nil {
		t.Fatal(err)
	}
	writes := f.mutations()
	if len(writes) != 2 || writes[0].path != "/api/upload-file" || !strings.HasPrefix(writes[1].path, "/api/workspaces/"+workspaceID+"/mattermost/install?") {
		t.Fatalf("expected one upload followed by installation, got %#v", writes)
	}
	if !bytes.Equal(f.uploaded, f.image) {
		t.Fatal("CLI image transfer changed the original bytes")
	}
	if writes[1].body["access_token"] != f.storedBotToken {
		t.Fatal("installation did not receive the separate bot credential")
	}
	f.mu.Lock()
	remoteRequests := append([]request(nil), f.remoteRequests...)
	f.mu.Unlock()
	if !reflect.DeepEqual(remoteRequests, []request{
		{method: http.MethodGet, path: "/api/v4/users/me"},
		{method: http.MethodPost, path: "/api/v4/users/usernames"},
	}) {
		t.Fatalf("installation did not verify bot and allowed account identity before writing: %#v", remoteRequests)
	}
	reply := writes[1].body["denied_reply"].(map[string]any)
	if !reflect.DeepEqual(reply["image_attachment_ids"], []any{teamBridgeUploadedID}) {
		t.Fatal("installation reused a source attachment ID instead of the uploaded image")
	}
	assertNoop(t, cli, project)
	if err := r.Apply(project, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != 2 || len(f.base.mutations()) != 0 {
		t.Fatal("installed bot failed to converge without repeating image upload or token rotation")
	}
	if len(runner.configFiles) < 2 {
		t.Fatal("expected file-based verification and installation requests")
	}
	for _, path := range runner.configFiles {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("a temporary CLI request file survived the command")
		}
	}
}

func TestForkCLITeamBridgeWorkspaceSetPreflightBeforeAnyWrite(t *testing.T) {
	for _, withoutSecrets := range []bool{false, true} {
		name := "token-unavailable"
		if withoutSecrets {
			name = "extension-unavailable"
		}
		t.Run(name, func(t *testing.T) {
			testForkCLITeamBridgeWorkspaceSetPreflight(t, withoutSecrets)
		})
	}
}

func testForkCLITeamBridgeWorkspaceSetPreflight(t *testing.T, withoutSecrets bool) {
	const secondWS = "00000000-0000-4000-8000-000000000104"
	const poisonedWS = "00000000-0000-4000-8000-000000000999"
	f, cli := newTeamBridgeFixture(t)
	f.installation["denied_reply"] = map[string]any{"mode": "text", "text": "No access"}
	var state sync.Mutex
	secondUnavailable := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/workspaces" {
			_ = json.NewEncoder(w).Encode([]backend.Workspace{{ID: workspaceID, Slug: "a", Name: "A"}, {ID: secondWS, Slug: "b", Name: "B"}})
			return
		}
		id := r.Header.Get("X-Workspace-ID")
		if id != workspaceID && id != secondWS {
			t.Errorf("request escaped workspace-set binding: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected workspace scope", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/api/workspaces/"+secondWS+"/mattermost/installations" && r.Method == http.MethodGet {
			state.Lock()
			unavailable := secondUnavailable
			state.Unlock()
			if unavailable && withoutSecrets {
				http.NotFound(w, r)
				return
			}
			f.mu.Lock()
			encoded, _ := json.Marshal(f.installation)
			f.mu.Unlock()
			var bot map[string]any
			_ = json.Unmarshal(encoded, &bot)
			bot["workspace_id"], bot["bot_user_id"] = secondWS, "yyyyyyyyyyyyyyyyyyyyyyyyyy"
			_ = json.NewEncoder(w).Encode(map[string]any{"installations": []any{bot}, "configured": true, "install_supported": true})
			return
		}
		if r.URL.Path == "/api/workspaces/"+secondWS+"/mattermost/installations/"+teamBridgeInstallationID+"/token" && r.Method == http.MethodGet {
			state.Lock()
			unavailable := secondUnavailable
			state.Unlock()
			if unavailable {
				http.Error(w, "token export denied", http.StatusForbidden)
				return
			}
			f.mu.Lock()
			token := f.storedBotToken
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"installation_id": teamBridgeInstallationID, "workspace_id": secondWS, "access_token": token})
			return
		}
		if r.URL.Path == "/api/workspaces/"+secondWS+"/mcp-servers" && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		f.serve(w, r)
	}))
	t.Cleanup(server.Close)
	runner := cli.Runner.(isolatedRunner)
	for i, entry := range runner.env {
		if strings.HasPrefix(entry, "MULTICA_SERVER_URL=") {
			runner.env[i] = "MULTICA_SERVER_URL=" + server.URL
		}
		if strings.HasPrefix(entry, "MULTICA_WORKSPACE_ID=") {
			runner.env[i] = "MULTICA_WORKSPACE_ID=" + poisonedWS
		}
	}
	cli.Runner = runner
	dir := filepath.Join(t.TempDir(), "snapshot")
	ex := exporter.WorkspaceExporter{Catalog: cli, BackendFor: func(id string) backend.Backend { return cli.WithScope("", id) }}
	if result, err := ex.Export(exporter.Options{OutputDir: dir, WithTeamBridge: true, WithoutSecrets: withoutSecrets}); err != nil || result.TeamBridge != 2 {
		t.Fatalf("workspace-set export failed: %#v %v", result, err)
	}
	skillPath := filepath.Join(dir, "a", "skills", "fixture", "SKILL.md")
	body, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, append(body, []byte("\nA planned change in the first workspace.\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	state.Lock()
	secondUnavailable = true
	state.Unlock()
	// Exercise app-level all-workspace preflight with real CLI subprocesses.
	// Allow only synthetic Multica inputs to reach the subprocess environment.
	t.Chdir(runner.dir)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "MULTICA_") {
			t.Setenv(name, "")
		}
	}
	for _, entry := range runner.env {
		name, value, _ := strings.Cut(entry, "=")
		t.Setenv(name, value)
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	var out, errout bytes.Buffer
	code := app.Run([]string{"apply", "--multica-bin", cli.Binary, "--config", filepath.Join(dir, "multica.yaml")}, &out, &errout)
	if code != 1 || !strings.Contains(errout.String(), "preflight") || !strings.Contains(strings.ToLower(errout.String()), "teambridge") {
		t.Fatalf("unexpected workspace-set result: code=%d %s", code, errout.String())
	}
	if len(f.mutations()) != 0 || len(f.base.mutations()) != 0 {
		t.Fatal("an earlier workspace was changed before TeamBridge preflight completed")
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
