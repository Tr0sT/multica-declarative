package backend

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

const attachmentTestInstallationID = "00000000-0000-4000-8000-000000000011"

func attachmentTestString(value string) *string { return &value }

func attachmentTestResponse(mode *string) []byte {
	value := map[string]any{
		"id":           attachmentTestInstallationID,
		"workspace_id": "00000000-0000-4000-8000-000000000012",
		"agent_id":     "00000000-0000-4000-8000-000000000013",
		"server_url":   "https://mattermost.example.test",
		"bot_user_id":  "abcdefghijklmnopqrstuvwxyz", "bot_username": "test-bot",
		"status": "active", "context": map[string]any{"mode": "message"},
		"access_mode": "members", "allowed_users": []any{},
		"denied_reply":      map[string]any{"mode": "text"},
		"installer_user_id": "00000000-0000-4000-8000-000000000014",
		"installed_at":      "2026-10-23T00:00:00Z", "created_at": "2026-10-23T00:00:00Z",
		"updated_at": "2026-10-23T00:00:00Z",
	}
	if mode != nil {
		value["attachment_mode"] = *mode
	}
	data, _ := json.Marshal(value)
	return data
}

func attachmentTestInput(mode *string) model.TeamBridgeBotInput {
	return model.TeamBridgeBotInput{
		Context: model.TeamBridgeContext{Mode: "message"}, AttachmentMode: mode,
		AccessMode: "members", DeniedReply: model.TeamBridgeDeniedReply{Mode: "text"},
	}
}

func TestTeamBridgeAttachmentModeWireOmissionAndValidation(t *testing.T) {
	for _, mode := range []*string{nil, attachmentTestString("none"), attachmentTestString("links")} {
		body, err := teamBridgeSettingsBody(attachmentTestInput(mode))
		if err != nil {
			t.Fatal(err)
		}
		got, exists := body["attachment_mode"]
		if exists != (mode != nil) || (mode != nil && got != *mode) {
			t.Fatal("attachment mode omission/value changed", body)
		}
		item, err := decodeTeamBridgeInstallation(attachmentTestResponse(mode), "")
		if err != nil {
			t.Fatal(err)
		}
		if (item.AttachmentMode == nil) != (mode == nil) || (mode != nil && *item.AttachmentMode != *mode) {
			t.Fatal("attachment mode was lost or defaulted")
		}
	}
	for _, invalid := range []string{"", "download", "LINKS", " links "} {
		if _, err := teamBridgeSettingsBody(attachmentTestInput(&invalid)); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	for _, invalid := range []any{nil, "", "download", 17, true, []string{"links"}} {
		var value map[string]any
		if err := json.Unmarshal(attachmentTestResponse(nil), &value); err != nil {
			t.Fatal(err)
		}
		value["attachment_mode"] = invalid
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeTeamBridgeInstallation(data, ""); err == nil {
			t.Fatalf("invalid API value accepted: %#v", invalid)
		}
	}
}

type attachmentConfigRunner struct {
	t        *testing.T
	response []byte
	bodies   []map[string]any
	files    []string
}

func (r *attachmentConfigRunner) Run(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	r.t.Helper()
	for i, arg := range args {
		if arg == "--config-file" && i+1 < len(args) {
			file := args[i+1]
			data, err := os.ReadFile(file)
			if err != nil {
				r.t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				r.t.Fatal(err)
			}
			r.files = append(r.files, file)
			r.bodies = append(r.bodies, body)
		}
	}
	return r.response, nil, nil
}

func TestTeamBridgeAttachmentModeUpdateChecksResponse(t *testing.T) {
	for _, tc := range []struct {
		name             string
		wanted, returned *string
		ok               bool
	}{
		{"legacy-omission", nil, attachmentTestString("links"), true},
		{"legacy-cli", nil, nil, true},
		{"enable", attachmentTestString("links"), attachmentTestString("links"), true},
		{"disable", attachmentTestString("none"), attachmentTestString("none"), true},
		{"ignored-write", attachmentTestString("links"), attachmentTestString("none"), false},
		{"missing-response-field", attachmentTestString("none"), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &attachmentConfigRunner{t: t, response: attachmentTestResponse(tc.returned)}
			cli := &CLI{Runner: runner}
			err := cli.UpdateTeamBridgeBot(attachmentTestInstallationID, attachmentTestInput(tc.wanted))
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected result: %v", err)
			}
			if len(runner.bodies) != 1 {
				t.Fatal("expected one CLI config")
			}
			body := runner.bodies[0]
			got, present := body["attachment_mode"]
			if present != (tc.wanted != nil) || (tc.wanted != nil && got != *tc.wanted) {
				t.Fatal(body)
			}
			if _, present := body["access_token"]; present {
				t.Fatal("update touched token")
			}
			for _, file := range runner.files {
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatal("private input was not removed")
				}
			}
		})
	}
}

func TestTeamBridgeAttachmentModeInstallUsesPrivateConfig(t *testing.T) {
	for _, mode := range []string{"none", "links"} {
		runner := &attachmentConfigRunner{t: t, response: attachmentTestResponse(&mode)}
		input := attachmentTestInput(&mode)
		input.ServerURL, input.AccessToken = "https://mattermost.example.test", "synthetic-token"
		item, err := (&CLI{Runner: runner}).InstallTeamBridgeBot("00000000-0000-4000-8000-000000000013", input)
		if err != nil {
			t.Fatal(err)
		}
		if item.AttachmentMode == nil || *item.AttachmentMode != mode || len(runner.bodies) != 1 || runner.bodies[0]["attachment_mode"] != mode {
			t.Fatal("install did not preserve attachment setting")
		}
	}
}
