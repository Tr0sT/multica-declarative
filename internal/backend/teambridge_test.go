package backend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

const (
	tbWorkspace = "11111111-1111-4111-8111-111111111111"
	tbAgent     = "22222222-2222-4222-8222-222222222222"
	tbInstall   = "33333333-3333-4333-8333-333333333333"
	tbImage     = "44444444-4444-4444-8444-444444444444"
	tbInstaller = "55555555-5555-4555-8555-555555555555"
	tbBot       = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	tbUser      = "bbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func setupTeamBridgeTest(t *testing.T) *CLI {
	t.Helper()
	for _, key := range []string{"MULTICA_AGENT_ID", "MULTICA_TASK_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT", "MULTICA_SERVER_URL", "MULTICA_WORKSPACE_ID", "MULTICA_TOKEN"} {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "mul_test-secret")
	t.Setenv("MULTICA_SERVER_URL", "https://multica.example.com")
	t.Setenv("MULTICA_WORKSPACE_ID", tbWorkspace)
	return NewCLI("multica")
}

func teamBridgeWire() map[string]any {
	return map[string]any{
		"id": tbInstall, "workspace_id": tbWorkspace, "agent_id": tbAgent,
		"server_url": "https://teambridge.example.com", "bot_user_id": tbBot, "bot_username": "ask",
		"status": "active", "context": map[string]any{"mode": "thread"}, "access_mode": "allowlist",
		"allowed_users":     []any{map[string]any{"id": tbUser, "username": "sergey"}},
		"denied_reply":      map[string]any{"mode": "text", "text": "Access denied"},
		"installer_user_id": tbInstaller,
		"installed_at":      "2026-10-20T12:00:00Z", "created_at": "2026-10-20T12:00:00Z", "updated_at": "2026-10-21T12:00:00Z",
	}
}

func teamBridgeInput() model.TeamBridgeBotInput {
	return model.TeamBridgeBotInput{
		ServerURL: "https://teambridge.example.com", Context: model.TeamBridgeContext{Mode: "thread"},
		AccessMode: "allowlist", AllowedUsernames: []string{"sergey"},
		DeniedReply: model.TeamBridgeDeniedReply{Mode: "text", Text: "Access denied"},
	}
}

func teamBridgeWriteJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}

func teamBridgeListWire(installations ...any) map[string]any {
	if installations == nil {
		installations = []any{}
	}
	return map[string]any{"configured": true, "install_supported": true, "installations": installations}
}

func TestTeamBridgeListNormalizesForkWireFormats(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	for _, hours := range []int{0, 1440, 2160, 8760} {
		t.Run(fmt.Sprint(hours), func(t *testing.T) {
			wire := teamBridgeWire()
			wire["context"] = map[string]any{"mode": "channel_extended", "extended_lookback_hours": hours}
			wire["denied_reply"] = map[string]any{"mode": "image", "image_attachment_id": tbImage}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/workspaces/"+tbWorkspace+"/mattermost/installations" || r.Header.Get("Authorization") != "Bearer mul_test-secret" || r.Header.Get("X-Workspace-ID") != tbWorkspace {
					t.Error("wrong scoped request")
				}
				teamBridgeWriteJSON(t, w, teamBridgeListWire(wire))
			}))
			defer server.Close()
			t.Setenv("MULTICA_SERVER_URL", server.URL)
			values, err := cli.ListTeamBridgeInstallations()
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 || values[0].Context != (model.TeamBridgeContext{Mode: "channel", LookbackHours: hours}) || values[0].DeniedReply.Mode != "images" || !reflect.DeepEqual(values[0].DeniedReply.ImageAttachmentIDs, []string{tbImage}) || values[0].AllowedUsers[0].ID != tbUser || values[0].InstallerUserID != tbInstaller {
				t.Fatalf("public snapshot did not preserve/normalize the fork fields: %#v", values)
			}
		})
	}
}

func TestTeamBridgeListFailsClosedForUnsupportedOrIncompleteSnapshots(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	cases := map[string]func(map[string]any) any{
		"missing capability":   func(w map[string]any) any { delete(w, "install_supported"); return w },
		"disabled integration": func(w map[string]any) any { w["configured"] = false; return w },
		"unsupported install":  func(w map[string]any) any { w["install_supported"] = false; return w },
		"null installations":   func(w map[string]any) any { w["installations"] = nil; return w },
		"missing public field": func(w map[string]any) any {
			row := teamBridgeWire()
			delete(row, "allowed_users")
			w["installations"] = []any{row}
			return w
		},
		"foreign workspace": func(w map[string]any) any {
			row := teamBridgeWire()
			row["workspace_id"] = tbAgent
			w["installations"] = []any{row}
			return w
		},
		"unknown context": func(w map[string]any) any {
			row := teamBridgeWire()
			row["context"] = map[string]any{"mode": "future"}
			w["installations"] = []any{row}
			return w
		},
		"incomplete extended context": func(w map[string]any) any {
			row := teamBridgeWire()
			row["context"] = map[string]any{"mode": "channel_extended"}
			w["installations"] = []any{row}
			return w
		},
		"ambiguous context": func(w map[string]any) any {
			row := teamBridgeWire()
			row["context"] = map[string]any{"mode": "channel_extended", "extended_lookback_hours": 0, "lookback_hours": 24}
			w["installations"] = []any{row}
			return w
		},
		"unknown access": func(w map[string]any) any {
			row := teamBridgeWire()
			row["access_mode"] = "future"
			w["installations"] = []any{row}
			return w
		},
		"duplicate installations": func(w map[string]any) any { w["installations"] = []any{teamBridgeWire(), teamBridgeWire()}; return w },
		"duplicate users": func(w map[string]any) any {
			row := teamBridgeWire()
			row["allowed_users"] = []any{map[string]any{"id": tbUser, "username": "sergey"}, map[string]any{"id": tbUser, "username": "other"}}
			w["installations"] = []any{row}
			return w
		},
		"null user list": func(w map[string]any) any {
			row := teamBridgeWire()
			row["allowed_users"] = nil
			w["installations"] = []any{row}
			return w
		},
		"unknown denied reply": func(w map[string]any) any {
			row := teamBridgeWire()
			row["denied_reply"] = map[string]any{"mode": "video"}
			w["installations"] = []any{row}
			return w
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { teamBridgeWriteJSON(t, w, change(teamBridgeListWire())) }))
			defer server.Close()
			t.Setenv("MULTICA_SERVER_URL", server.URL)
			if _, err := cli.ListTeamBridgeInstallations(); err == nil {
				t.Fatal("accepted an incomplete or unsupported snapshot")
			}
		})
	}
}

func TestTeamBridgeSettingsPreserveSecretsAndEncodeAccessModes(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	for _, mode := range []string{"members", "anyone", "allowlist"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/api/workspaces/"+tbWorkspace+"/mattermost/installations/"+tbInstall {
					t.Errorf("unexpected update %s %s", r.Method, r.URL.Path)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				for _, forbidden := range []string{"access_token", "server_url", "agent_id", "installer_user_id"} {
					if _, ok := body[forbidden]; ok {
						t.Errorf("PATCH must not contain %s", forbidden)
					}
				}
				if mode == "allowlist" && string(body["allowed_usernames"]) != "[]" {
					t.Error("empty allowlist must be []")
				}
				if mode != "allowlist" && body["allowed_usernames"] != nil {
					t.Error("non-allowlist must omit usernames")
				}
				wire := teamBridgeWire()
				wire["access_mode"] = mode
				wire["allowed_users"] = []any{}
				teamBridgeWriteJSON(t, w, wire)
			}))
			defer server.Close()
			t.Setenv("MULTICA_SERVER_URL", server.URL)
			input := teamBridgeInput()
			input.AccessMode = mode
			input.AllowedUsernames = nil
			if err := cli.UpdateTeamBridgeBot(tbInstall, input); err != nil {
				t.Fatal(err)
			}
		})
	}
	input := teamBridgeInput()
	input.AccessToken = "new-secret"
	if err := cli.UpdateTeamBridgeBot(tbInstall, input); err == nil {
		t.Fatal("PATCH silently ignored token rotation")
	}
}

func TestTeamBridgeInstallAndRevoke(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			teamBridgeWriteJSON(t, w, teamBridgeListWire())
		case http.MethodPost:
			if r.URL.Query().Get("agent_id") != tbAgent || r.URL.Path != "/api/workspaces/"+tbWorkspace+"/mattermost/install" {
				t.Error("wrong installation target")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["access_token"] != "bot-secret" || body["server_url"] != "https://teambridge.example.com" {
				t.Error("installation token/server missing")
			}
			if !reflect.DeepEqual(body["allowed_usernames"], []any{"sergey"}) {
				t.Error("usernames not normalized")
			}
			teamBridgeWriteJSON(t, w, teamBridgeWire())
		case http.MethodDelete:
			if r.URL.Path != "/api/workspaces/"+tbWorkspace+"/mattermost/installations/"+tbInstall {
				t.Error("wrong revoke target")
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	input := teamBridgeInput()
	input.AccessToken = "bot-secret"
	input.AllowedUsernames = []string{" @Sergey ", "sergey"}
	installed, err := cli.InstallTeamBridgeBot(tbAgent, input)
	if err != nil || installed.ID != tbInstall {
		t.Fatalf("install failed: %v", err)
	}
	if err := cli.RevokeTeamBridgeBot(tbInstall); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, []string{"GET", "POST", "DELETE"}) {
		t.Fatalf("operations: %v", methods)
	}
}

func TestTeamBridgeHTTPFailuresNeverExposeBodiesOrFollowAPIRedirects(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	redirected := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected++; t.Error("API redirect followed") }))
	defer destination.Close()
	for _, code := range []int{301, 302, 307, 308, 401, 403, 404, 409, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL+"?token=secret-in-location")
				w.WriteHeader(code)
				_, _ = io.WriteString(w, "mul_test-secret bot-secret secret-in-body")
			}))
			defer server.Close()
			t.Setenv("MULTICA_SERVER_URL", server.URL)
			_, err := cli.ListTeamBridgeInstallations()
			if err == nil {
				t.Fatal("HTTP failure was swallowed")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), destination.URL) {
				t.Fatalf("error leaked credentials/URL: %s", err)
			}
		})
	}
	if redirected != 0 {
		t.Fatal("redirect reached destination")
	}
}

func teamBridgePNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aE4sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTeamBridgeImageDownloadAndUploadArePortableAndScoped(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	data := teamBridgePNG(t)
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Workspace-ID") != "" || r.Header.Get("Cookie") != "" {
			t.Error("Multica credentials leaked to signed storage URL")
		}
		_, _ = w.Write(data)
	}))
	defer download.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mul_test-secret" || r.Header.Get("X-Workspace-ID") != tbWorkspace {
			t.Error("missing scoped API auth")
		}
		switch r.URL.Path {
		case "/api/attachments/" + tbImage:
			teamBridgeWriteJSON(t, w, map[string]any{"id": tbImage, "workspace_id": tbWorkspace, "filename": `C:\old\meme.png`, "size_bytes": len(data), "download_url": download.URL + "?signed=private"})
		case "/api/upload-file":
			if r.Method != http.MethodPost {
				t.Error("wrong upload method")
			}
			if err := r.ParseMultipartForm(6 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if len(r.MultipartForm.Value) != 0 {
				t.Error("standalone upload has task/issue associations")
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			actual, _ := io.ReadAll(file)
			if header.Filename != "meme.png" || !bytes.Equal(actual, data) {
				t.Error("image was not uploaded intact")
			}
			teamBridgeWriteJSON(t, w, map[string]any{"id": tbImage, "workspace_id": tbWorkspace})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	image, err := cli.DownloadTeamBridgeImage(tbImage)
	if err != nil || image.Filename != "meme.png" || image.ContentType != "image/png" || !bytes.Equal(image.Data, data) {
		t.Fatalf("image round trip failed: %v", err)
	}
	id, err := cli.UploadTeamBridgeImage(image)
	if err != nil || id != tbImage {
		t.Fatalf("upload failed: %v", err)
	}
}

func TestTeamBridgeImageRelativeRedirectDropsCredentials(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	data := teamBridgePNG(t)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Workspace-ID") != "" {
			t.Error("redirect leaked scoped headers")
		}
		_, _ = w.Write(data)
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/attachments/"+tbImage {
			teamBridgeWriteJSON(t, w, map[string]any{"id": tbImage, "workspace_id": tbWorkspace, "filename": "meme.png", "size_bytes": len(data), "download_url": "/download"})
			return
		}
		if r.URL.Path != "/download" || r.Header.Get("Authorization") != "Bearer mul_test-secret" {
			t.Error("relative URL missing auth")
		}
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer server.Close()
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	if _, err := cli.DownloadTeamBridgeImage(tbImage); err != nil {
		t.Fatal(err)
	}
}

func TestTeamBridgeImagesRejectOversizedWrongWorkspaceAndNonImages(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	if _, err := cli.UploadTeamBridgeImage(model.TeamBridgeImage{Filename: "fake.png", Data: []byte("text")}); err == nil {
		t.Fatal("uploaded non-image")
	}
	oversized := append(teamBridgePNG(t), make([]byte, model.MaxTeamBridgeImageBytes)...)
	if _, err := cli.UploadTeamBridgeImage(model.TeamBridgeImage{Filename: "big.png", Data: oversized}); err == nil {
		t.Fatal("uploaded oversized image")
	}
	for _, mode := range []string{"wrong workspace", "private attachment", "oversized download", "size mismatch", "non-image"} {
		t.Run(mode, func(t *testing.T) {
			data := teamBridgePNG(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/attachments/"+tbImage {
					metadata := map[string]any{"id": tbImage, "workspace_id": tbWorkspace, "filename": "meme.png", "size_bytes": len(data), "download_url": "/image"}
					if mode == "wrong workspace" {
						metadata["workspace_id"] = tbAgent
					}
					if mode == "private attachment" {
						metadata["chat_session_id"] = tbAgent
					}
					teamBridgeWriteJSON(t, w, metadata)
					return
				}
				switch mode {
				case "oversized download":
					_, _ = w.Write(oversized)
				case "size mismatch":
					_, _ = w.Write(data[:len(data)-1])
				case "non-image":
					_, _ = w.Write(bytes.Repeat([]byte("x"), len(data)))
				default:
					t.Error("invalid metadata was followed by download")
				}
			}))
			defer server.Close()
			t.Setenv("MULTICA_SERVER_URL", server.URL)
			if _, err := cli.DownloadTeamBridgeImage(tbImage); err == nil {
				t.Fatal("accepted unsafe/incomplete image")
			}
		})
	}
}

func TestTeamBridgeVerifyRejectsWrongBotAndReusedUsernameBeforeInstall(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	for _, mode := range []string{"valid", "new images", "wrong bot", "personal token", "reused username", "missing username", "deleted user"} {
		t.Run(mode, func(t *testing.T) {
			var requests []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				if r.Header.Get("Authorization") != "Bearer bot-secret" || r.Header.Get("X-Workspace-ID") != "" {
					t.Error("verification used the wrong credentials")
				}
				switch r.URL.Path {
				case "/api/v4/users/me":
					id, isBot := tbBot, true
					if mode == "wrong bot" {
						id = tbUser
					}
					if mode == "personal token" {
						isBot = false
					}
					teamBridgeWriteJSON(t, w, map[string]any{"id": id, "username": "ask", "is_bot": isBot, "delete_at": 0})
				case "/api/v4/users/usernames":
					if r.Method != http.MethodPost {
						t.Error("username lookup method")
					}
					var names []string
					if err := json.NewDecoder(r.Body).Decode(&names); err != nil {
						t.Error(err)
					}
					if !reflect.DeepEqual(names, []string{"sergey"}) {
						t.Error("lookup names differ")
					}
					users := []any{}
					id, deleted := tbUser, 0
					if mode == "reused username" {
						id = "cccccccccccccccccccccccccc"
					}
					if mode == "deleted user" {
						deleted = 123
					}
					if mode != "missing username" {
						users = append(users, map[string]any{"id": id, "username": "sergey", "is_bot": false, "delete_at": deleted})
					}
					teamBridgeWriteJSON(t, w, users)
				default:
					t.Error("verification made a non-lookup request")
				}
			}))
			defer server.Close()
			cli.HTTPClient = server.Client()
			input := teamBridgeInput()
			input.ServerURL = server.URL
			input.AccessToken = "bot-secret"
			if mode == "new images" {
				input.DeniedReply = model.TeamBridgeDeniedReply{Mode: "images"}
			}
			err := cli.VerifyTeamBridgeBot(input, tbBot, []model.TeamBridgeAllowedUser{{ID: tbUser, Username: "sergey"}})
			if (err == nil) != (mode == "valid" || mode == "new images") {
				t.Fatalf("verification mode %s: %v", mode, err)
			}
			if len(requests) > 2 {
				t.Fatalf("verification unexpectedly mutated: %v", requests)
			}
		})
	}
}

func TestTeamBridgeVerificationRefusesRedirectsAndRedactsErrors(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("verification followed a redirect with a bot token")
	}))
	defer destination.Close()
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusUnauthorized, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL+"?private=bot-secret")
				w.WriteHeader(code)
				_, _ = io.WriteString(w, "bot-secret mul_test-secret")
			}))
			defer server.Close()
			cli.HTTPClient = server.Client()
			input := teamBridgeInput()
			input.ServerURL, input.AccessToken = server.URL, "bot-secret"
			err := cli.VerifyTeamBridgeBot(input, tbBot, nil)
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), destination.URL) {
				t.Fatalf("unsafe verification error: %v", err)
			}
		})
	}
}

func TestTeamBridgeImageDownloadRefusesHTTPSDowngrade(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTPS image download downgraded to HTTP")
	}))
	defer destination.Close()
	for _, mode := range []string{"absolute metadata", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/attachments/"+tbImage {
					downloadURL := destination.URL
					if mode == "redirect" {
						downloadURL = "/download"
					}
					teamBridgeWriteJSON(t, w, map[string]any{"id": tbImage, "workspace_id": tbWorkspace, "filename": "meme.png", "size_bytes": 12, "download_url": downloadURL})
					return
				}
				http.Redirect(w, r, destination.URL, http.StatusFound)
			}))
			defer server.Close()
			cli.HTTPClient = server.Client()
			t.Setenv("MULTICA_SERVER_URL", server.URL)
			if _, err := cli.DownloadTeamBridgeImage(tbImage); err == nil {
				t.Fatal("accepted HTTP downgrade")
			}
		})
	}
}

func TestTeamBridgeProfileAndEnvironmentScope(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	profileDir := filepath.Join(os.Getenv("HOME"), ".multica", "profiles", "production")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "config.json"), []byte(`{"server_url":"https://profile.example.com","workspace_id":"`+tbWorkspace+`","token":"profile-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_TOKEN", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	connection, err := cli.WithScope("production", "").teamBridgeConnection()
	if err != nil {
		t.Fatal(err)
	}
	if connection.baseURL != "https://profile.example.com" || connection.workspaceID != tbWorkspace || connection.token != "profile-secret" {
		t.Fatal("named profile not resolved")
	}
	t.Setenv("MULTICA_SERVER_URL", "wss://override.example.com/ws")
	t.Setenv("MULTICA_TOKEN", " env-secret ")
	t.Setenv("MULTICA_WORKSPACE_ID", tbAgent)
	connection, err = cli.WithScope("production", tbInstaller).teamBridgeConnection()
	if err != nil {
		t.Fatal(err)
	}
	if connection.baseURL != "https://override.example.com" || connection.workspaceID != tbInstaller || connection.token != "env-secret" {
		t.Fatal("explicit scope/environment precedence was not preserved")
	}
	if _, err := cli.WithScope("../production", tbWorkspace).teamBridgeConnection(); err == nil {
		t.Fatal("unsafe profile name accepted")
	}
}

func TestTeamBridgeNeverFallsBackToHumanCredentialsFromTaskContext(t *testing.T) {
	cli := setupTeamBridgeTest(t)
	for _, key := range []string{"MULTICA_AGENT_ID", "MULTICA_TASK_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "present")
			if _, err := cli.teamBridgeConnection(); err == nil {
				t.Fatal("task context inherited human credentials")
			}
		})
	}
	t.Run("task token", func(t *testing.T) {
		t.Setenv("MULTICA_TOKEN", "mat_task-secret")
		if _, err := cli.teamBridgeConnection(); err == nil {
			t.Fatal("task token accepted")
		}
	})
	t.Run("ancestor marker", func(t *testing.T) {
		root := t.TempDir()
		child := filepath.Join(root, "child")
		if err := os.MkdirAll(filepath.Join(root, ".multica"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(child, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".multica", "daemon_task_context.json"), []byte(`{"managed_by":"multica-daemon-task"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(child)
		if _, err := cli.teamBridgeConnection(); err == nil {
			t.Fatal("ancestor task marker ignored")
		}
	})
}
