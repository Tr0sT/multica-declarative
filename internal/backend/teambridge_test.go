package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
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

type teamBridgeRunner func(string, []string) ([]byte, []byte, error)

func (r teamBridgeRunner) Run(_ context.Context, binary string, args ...string) ([]byte, []byte, error) {
	return r(binary, args)
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
func teamBridgeListWire(rows ...any) map[string]any {
	if rows == nil {
		rows = []any{}
	}
	return map[string]any{"configured": true, "install_supported": true, "installations": rows}
}
func teamBridgeInput() model.TeamBridgeBotInput {
	return model.TeamBridgeBotInput{ServerURL: "https://teambridge.example.com", Context: model.TeamBridgeContext{Mode: "thread"}, AccessMode: "allowlist", AllowedUsernames: []string{"sergey"}, DeniedReply: model.TeamBridgeDeniedReply{Mode: "text", Text: "Access denied"}}
}
func teamBridgeJSONBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func teamBridgeArg(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}
func teamBridgeAssertPrivate(t *testing.T, file string) {
	t.Helper()
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("not a private regular command file: %v %v", info, err)
	}
	info, err = os.Lstat(filepath.Dir(file))
	if err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
		t.Fatalf("command directory is not private: %v %v", info, err)
	}
}
func teamBridgeAssertRemoved(t *testing.T, file string) {
	t.Helper()
	if file == "" {
		t.Fatal("no command file was captured")
	}
	if _, err := os.Lstat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("temporary command directory was not removed: %v", err)
	}
}

func TestTeamBridgeCLIListNormalizesForkWireAndForwardsScope(t *testing.T) {
	for _, hours := range []int{0, 1440, 2160, 8760} {
		t.Run(fmt.Sprint(hours), func(t *testing.T) {
			row := teamBridgeWire()
			row["context"] = map[string]any{"mode": "channel_extended", "extended_lookback_hours": hours}
			row["denied_reply"] = map[string]any{"mode": "image", "image_attachment_id": tbImage}
			calls := 0
			cli := &CLI{Binary: "fork-multica", Runner: teamBridgeRunner(func(binary string, args []string) ([]byte, []byte, error) {
				calls++
				want := []string{"--profile", "production", "--workspace-id", tbWorkspace, "teambridge", "list", "--output", "json"}
				if binary != "fork-multica" || !reflect.DeepEqual(args, want) {
					t.Fatalf("wrong CLI invocation: %s %v", binary, args)
				}
				return teamBridgeJSONBytes(t, teamBridgeListWire(row)), nil, nil
			})}
			items, err := cli.WithScope("production", tbWorkspace).ListTeamBridgeInstallations()
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(items) != 1 || items[0].Context != (model.TeamBridgeContext{Mode: "channel", LookbackHours: hours}) || items[0].DeniedReply.Mode != "images" || !reflect.DeepEqual(items[0].DeniedReply.ImageAttachmentIDs, []string{tbImage}) || items[0].AllowedUsers[0].ID != tbUser || items[0].InstallerUserID != tbInstaller {
				t.Fatalf("wire fields were lost: %+v", items)
			}
		})
	}
}

func TestTeamBridgeCLIHasNoIndependentAuthenticationOrProfileFallback(t *testing.T) {
	// The runner represents the selected CLI's auth boundary. The declarative
	// adapter must not inspect profiles/env/task markers or send these values.
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"MULTICA_TOKEN", "MULTICA_SERVER_URL", "MULTICA_TASK_CONFIG_ROOT", "MULTICA_AGENT_ID"} {
		t.Setenv(name, "must-not-appear-in-cli-arguments")
	}
	cli := &CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
		if !reflect.DeepEqual(args, []string{"teambridge", "list", "--output", "json"}) {
			t.Fatal(args)
		}
		return teamBridgeJSONBytes(t, teamBridgeListWire()), nil, nil
	})}
	if _, err := cli.ListTeamBridgeInstallations(); err != nil {
		t.Fatal(err)
	}
}

func TestTeamBridgeCLIRejectsIncompleteOrMismatchedResponses(t *testing.T) {
	cases := map[string]func(map[string]any) any{
		"missing capability": func(w map[string]any) any { delete(w, "install_supported"); return w },
		"disabled":           func(w map[string]any) any { w["configured"] = false; return w },
		"null list":          func(w map[string]any) any { w["installations"] = nil; return w },
		"missing field": func(w map[string]any) any {
			row := teamBridgeWire()
			delete(row, "allowed_users")
			w["installations"] = []any{row}
			return w
		},
		"foreign scope": func(w map[string]any) any {
			row := teamBridgeWire()
			row["workspace_id"] = tbAgent
			w["installations"] = []any{row}
			return w
		},
		"invalid workspace": func(w map[string]any) any {
			row := teamBridgeWire()
			row["workspace_id"] = ""
			w["installations"] = []any{row}
			return w
		},
		"unknown context": func(w map[string]any) any {
			row := teamBridgeWire()
			row["context"] = map[string]any{"mode": "future"}
			w["installations"] = []any{row}
			return w
		},
		"missing extended window": func(w map[string]any) any {
			row := teamBridgeWire()
			row["context"] = map[string]any{"mode": "channel_extended"}
			w["installations"] = []any{row}
			return w
		},
		"ambiguous context": func(w map[string]any) any {
			row := teamBridgeWire()
			row["context"] = map[string]any{"mode": "channel_extended", "lookback_hours": 24, "extended_lookback_hours": 0}
			w["installations"] = []any{row}
			return w
		},
		"duplicate rows": func(w map[string]any) any { w["installations"] = []any{teamBridgeWire(), teamBridgeWire()}; return w },
		"null users": func(w map[string]any) any {
			row := teamBridgeWire()
			row["allowed_users"] = nil
			w["installations"] = []any{row}
			return w
		},
		"invalid status": func(w map[string]any) any {
			row := teamBridgeWire()
			row["status"] = "future"
			w["installations"] = []any{row}
			return w
		},
		"invalid timestamp": func(w map[string]any) any {
			row := teamBridgeWire()
			row["created_at"] = "yesterday"
			w["installations"] = []any{row}
			return w
		},
		"invalid image": func(w map[string]any) any {
			row := teamBridgeWire()
			row["denied_reply"] = map[string]any{"mode": "image"}
			w["installations"] = []any{row}
			return w
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{stdout: teamBridgeJSONBytes(t, change(teamBridgeListWire()))}
			if _, err := (&CLI{Runner: runner}).WithScope("", tbWorkspace).ListTeamBridgeInstallations(); err == nil {
				t.Fatal("unsafe or incomplete snapshot accepted")
			}
		})
	}
	first, second := teamBridgeWire(), teamBridgeWire()
	second["id"], second["agent_id"], second["workspace_id"] = tbImage, tbInstaller, tbAgent
	runner := &fakeRunner{stdout: teamBridgeJSONBytes(t, teamBridgeListWire(first, second))}
	if _, err := (&CLI{Runner: runner}).ListTeamBridgeInstallations(); err == nil {
		t.Fatal("mixed-workspace default-profile response accepted")
	}
}

func TestTeamBridgeCLIInstallAndVerifyUsePrivateConfigFiles(t *testing.T) {
	for _, operation := range []string{"install", "verify"} {
		t.Run(operation, func(t *testing.T) {
			var file string
			secret := "synthetic-bot-secret"
			cli := &CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
				if args[0] != "teambridge" || args[1] != operation || teamBridgeArg(args, "--output") != "json" {
					t.Fatal(args)
				}
				if strings.Contains(strings.Join(args, " "), secret) {
					t.Fatal("bot token leaked to argv")
				}
				file = teamBridgeArg(args, "--config-file")
				teamBridgeAssertPrivate(t, file)
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var config map[string]json.RawMessage
				if json.Unmarshal(data, &config) != nil || string(config["access_token"]) != `"synthetic-bot-secret"` || string(config["server_url"]) != `"https://teambridge.example.com"` {
					t.Fatal("config did not preserve required secret/server fields")
				}
				if operation == "install" {
					if teamBridgeArg(args, "--agent") != tbAgent || string(config["access_mode"]) != `"allowlist"` || string(config["allowed_usernames"]) != `["sergey"]` {
						t.Fatal("install contract mismatch")
					}
					return teamBridgeJSONBytes(t, teamBridgeWire()), nil, nil
				}
				var users []model.TeamBridgeAllowedUser
				if json.Unmarshal(config["allowed_users"], &users) != nil || len(users) != 1 || users[0].ID != tbUser || users[0].Username != "sergey" || string(config["expected_bot_user_id"]) != `"`+tbBot+`"` {
					t.Fatal("verify lost pinned account identities")
				}
				if config["context"] != nil || config["denied_reply"] != nil || config["access_mode"] != nil {
					t.Fatal("verify included mutation fields")
				}
				return []byte(`{"verified":true}`), nil, nil
			})}
			input := teamBridgeInput()
			input.AccessToken = secret
			var err error
			if operation == "install" {
				_, err = cli.InstallTeamBridgeBot(tbAgent, input)
			} else {
				err = cli.VerifyTeamBridgeBot(input, tbBot, []model.TeamBridgeAllowedUser{{ID: tbUser, Username: "sergey"}})
			}
			if err != nil {
				t.Fatal(err)
			}
			teamBridgeAssertRemoved(t, file)
		})
	}
}

func TestTeamBridgeCLIUpdatePreservesTokenAndEncodesEmptyAllowlist(t *testing.T) {
	for _, mode := range []string{"members", "anyone", "allowlist"} {
		t.Run(mode, func(t *testing.T) {
			var file string
			cli := &CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
				if !reflect.DeepEqual(args[:3], []string{"teambridge", "update", tbInstall}) {
					t.Fatal(args)
				}
				file = teamBridgeArg(args, "--config-file")
				teamBridgeAssertPrivate(t, file)
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var body map[string]json.RawMessage
				if json.Unmarshal(data, &body) != nil {
					t.Fatal("invalid config")
				}
				for _, key := range []string{"access_token", "server_url", "agent_id", "installer_user_id"} {
					if body[key] != nil {
						t.Fatalf("update included %s", key)
					}
				}
				if mode == "allowlist" && string(body["allowed_usernames"]) != "[]" {
					t.Fatal("empty allowlist must be explicit []")
				}
				if mode != "allowlist" && body["allowed_usernames"] != nil {
					t.Fatal("other access modes must omit usernames")
				}
				row := teamBridgeWire()
				row["access_mode"], row["allowed_users"] = mode, []any{}
				return teamBridgeJSONBytes(t, row), nil, nil
			})}
			input := teamBridgeInput()
			input.AccessMode, input.AllowedUsernames = mode, nil
			if err := cli.UpdateTeamBridgeBot(tbInstall, input); err != nil {
				t.Fatal(err)
			}
			teamBridgeAssertRemoved(t, file)
		})
	}
	input := teamBridgeInput()
	input.AccessToken = "must-not-rotate"
	runner := &fakeRunner{}
	if err := (&CLI{Runner: runner}).UpdateTeamBridgeBot(tbInstall, input); err == nil || len(runner.calls) != 0 {
		t.Fatal("token rotation reached settings update")
	}
}

func TestTeamBridgeCLIConfirmsRevocationAndVerification(t *testing.T) {
	runner := &fakeRunner{stdout: []byte(`{"id":"` + tbInstall + `","revoked":true}`)}
	cli := &CLI{Runner: runner}
	if err := cli.RevokeTeamBridgeBot(tbInstall); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runner.calls[0].args, []string{"teambridge", "revoke", tbInstall, "--output", "json"}) {
		t.Fatal(runner.calls[0].args)
	}
	runner.stdout = []byte(`{"id":"` + tbInstall + `","revoked":false}`)
	if err := cli.RevokeTeamBridgeBot(tbInstall); err == nil {
		t.Fatal("unconfirmed revocation accepted")
	}
	runner.stdout = []byte(`{"verified":false}`)
	input := teamBridgeInput()
	input.AccessToken = "synthetic-secret"
	if err := cli.VerifyTeamBridgeBot(input, tbBot, []model.TeamBridgeAllowedUser{{Username: "sergey"}}); err == nil {
		t.Fatal("unconfirmed verification accepted")
	}
}

func TestTeamBridgeCLICommandFailuresNeverExposeSecretsAndAlwaysCleanUp(t *testing.T) {
	const secret = "sensitive-sentinel-73"
	for _, tc := range []struct {
		name, stderr, stdout, want string
		err                        error
	}{
		{"command error", "request echoed " + secret, "", "command failed", errors.New(secret)},
		{"unsupported CLI", `unknown command "teambridge" for "multica" ` + secret, "", "compatible TeamBridge fork CLI", errors.New(secret)},
		{"unsupported server", "API error (404): " + secret, "", "HTTP 404", errors.New(secret)},
		{"authentication", "teambridge request failed (HTTP 401) " + secret, "", "authentication failed", errors.New(secret)},
		{"permission", "teambridge request failed (HTTP 403) " + secret, "", "admin permissions", errors.New(secret)},
		{"conflict", "teambridge request failed (HTTP 409) " + secret, "", "installation conflict", errors.New(secret)},
		{"wrong bot", "TeamBridge token does not belong to the expected active bot; nothing was changed " + secret, "", "correct bot token", errors.New(secret)},
		{"wrong account", "TeamBridge username does not identify the expected active account; check renamed or reused usernames " + secret, "", "exported account", errors.New(secret)},
		{"malformed JSON", "", secret, "invalid or oversized JSON", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var file string
			cli := &CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
				file = teamBridgeArg(args, "--config-file")
				teamBridgeAssertPrivate(t, file)
				return []byte(tc.stdout), []byte(tc.stderr + " https://objects.example.com/private?signature=signed-url-sentinel"), tc.err
			})}
			input := teamBridgeInput()
			input.AccessToken = secret
			_, err := cli.InstallTeamBridgeBot(tbAgent, input)
			if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "signed-url-sentinel") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wrong/sensitive error: %v", err)
			}
			teamBridgeAssertRemoved(t, file)
		})
	}
}

func TestTeamBridgeCLIImagesUsePrivateFilesAndVerifyBytes(t *testing.T) {
	data := []byte("\x89PNG\r\n\x1a\npayload")
	var downloaded, uploaded string
	cli := &CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
		if args[0] != "teambridge" || args[1] != "image" || teamBridgeArg(args, "--output") != "json" {
			t.Fatal(args)
		}
		switch args[2] {
		case "download":
			if args[3] != tbImage {
				t.Fatal(args)
			}
			downloaded = teamBridgeArg(args, "--output-file")
			teamBridgeAssertPrivate(t, downloaded)
			if err := os.WriteFile(downloaded, data, 0600); err != nil {
				t.Fatal(err)
			}
			return teamBridgeJSONBytes(t, map[string]any{"id": tbImage, "filename": `C:\old\meme.png`, "content_type": "image/png", "size": len(data), "path": downloaded}), nil, nil
		case "upload":
			uploaded = args[3]
			teamBridgeAssertPrivate(t, uploaded)
			bytes, err := os.ReadFile(uploaded)
			if err != nil || !reflect.DeepEqual(bytes, data) || filepath.Base(uploaded) != "meme.png" {
				t.Fatal("wrong upload file")
			}
			return teamBridgeJSONBytes(t, map[string]any{"id": tbImage, "filename": "meme.png", "content_type": "image/png", "size": len(data)}), nil, nil
		default:
			t.Fatal(args)
		}
		return nil, nil, nil
	})}
	image, err := cli.DownloadTeamBridgeImage(tbImage)
	if err != nil || image.Filename != "meme.png" || image.ContentType != "image/png" || !bytes.Equal(image.Data, data) {
		t.Fatalf("download mismatch: %+v %v", image, err)
	}
	teamBridgeAssertRemoved(t, downloaded)
	id, err := cli.UploadTeamBridgeImage(image)
	if err != nil || id != tbImage {
		t.Fatalf("upload mismatch: %s %v", id, err)
	}
	teamBridgeAssertRemoved(t, uploaded)
}

func TestTeamBridgeCLIImageDownloadRejectsUntrustedOutputAndCleansFiles(t *testing.T) {
	for _, kind := range []string{"error", "missing output", "wrong id", "wrong path", "wrong size", "oversize", "wrong type", "unprotected", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "unprotected" && runtime.GOOS == "windows" {
				t.Skip("Windows FileMode permission bits do not describe ACL access")
			}
			var file string
			data := []byte("\x89PNG\r\n\x1a\npayload")
			cli := &CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
				file = teamBridgeArg(args, "--output-file")
				teamBridgeAssertPrivate(t, file)
				if kind == "error" {
					return nil, []byte("signed-private-url"), errors.New("signed-private-url")
				}
				if err := os.WriteFile(file, data, 0600); err != nil {
					t.Fatal(err)
				}
				meta := map[string]any{"id": tbImage, "filename": "image.png", "content_type": "image/png", "size": len(data), "path": file}
				switch kind {
				case "missing output":
					if err := os.Remove(file); err != nil {
						t.Fatal(err)
					}
				case "wrong id":
					meta["id"] = tbInstall
				case "wrong path":
					meta["path"] = "/private/elsewhere"
				case "wrong size":
					meta["size"] = len(data) + 1
				case "oversize":
					meta["size"] = model.MaxTeamBridgeImageBytes + 1
				case "wrong type":
					meta["content_type"] = "image/jpeg"
				case "unprotected":
					if err := os.Chmod(file, 0644); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Remove(file); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(file, 0700); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					outside := filepath.Join(t.TempDir(), "outside.png")
					if err := os.WriteFile(outside, data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(file); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, file); err != nil {
						if runtime.GOOS == "windows" {
							t.Skipf("symlink creation is unavailable: %v", err)
						}
						t.Fatal(err)
					}
				}
				return teamBridgeJSONBytes(t, meta), nil, nil
			})}
			if _, err := cli.DownloadTeamBridgeImage(tbImage); err == nil || strings.Contains(err.Error(), "signed-private-url") {
				t.Fatalf("unsafe output accepted or leaked: %v", err)
			}
			teamBridgeAssertRemoved(t, file)
		})
	}
}

func TestTeamBridgeCLIInvalidInputsDoNotExecuteCommands(t *testing.T) {
	runner := &fakeRunner{}
	cli := &CLI{Runner: runner}
	if _, err := cli.DownloadTeamBridgeImage("not-a-uuid"); err == nil {
		t.Fatal("invalid attachment id accepted")
	}
	if _, err := cli.UploadTeamBridgeImage(model.TeamBridgeImage{Filename: "text.png", Data: []byte("not an image")}); err == nil {
		t.Fatal("non-image accepted")
	}
	if _, err := cli.UploadTeamBridgeImage(model.TeamBridgeImage{Filename: "big.png", Data: bytes.Repeat([]byte("x"), model.MaxTeamBridgeImageBytes+1)}); err == nil {
		t.Fatal("oversized image accepted")
	}
	if err := cli.RevokeTeamBridgeBot("not-a-uuid"); err == nil {
		t.Fatal("invalid revoke id accepted")
	}
	input := teamBridgeInput()
	input.AccessToken = "token\nwith-newline"
	if _, err := cli.InstallTeamBridgeBot(tbAgent, input); err == nil {
		t.Fatal("invalid token accepted")
	}
	input.AccessToken = "synthetic-secret"
	input.ServerURL = "http://unsafe.example.com"
	if err := cli.VerifyTeamBridgeBot(input, tbBot, nil); err == nil {
		t.Fatal("HTTP verification accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid input executed commands: %+v", runner.calls)
	}
}
