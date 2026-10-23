package reconcile

import (
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

func attachmentModeString(value string) *string { return &value }

func TestTeamBridgeAttachmentModeDiff(t *testing.T) {
	for _, tc := range []struct {
		name           string
		wanted, actual *string
		changed, fails bool
	}{
		{"omitted", nil, attachmentModeString("links"), false, false},
		{"legacy", nil, nil, false, false},
		{"same", attachmentModeString("links"), attachmentModeString("links"), false, false},
		{"disable", attachmentModeString("none"), attachmentModeString("links"), true, false},
		{"enable", attachmentModeString("links"), attachmentModeString("none"), true, false},
		{"old-server", attachmentModeString("none"), nil, false, true},
		{"unknown", attachmentModeString("download"), attachmentModeString("links"), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := model.TeamBridgeBotSpec{AttachmentMode: tc.wanted, AccessMode: "members", DeniedReply: model.TeamBridgeReplySpec{Mode: "text"}}
			a := model.TeamBridgeInstallation{AttachmentMode: tc.actual, AccessMode: "members", DeniedReply: model.TeamBridgeDeniedReply{Mode: "text"}}
			fields, _, err := diffTeamBridge(d, a, nil)
			if (err != nil) != tc.fails {
				t.Fatal(err)
			}
			if !tc.fails && ((len(fields) != 0) != tc.changed || (tc.changed && strings.Join(fields, ",") != "attachmentMode")) {
				t.Fatal(fields)
			}
			if in := teamBridgeInput(d, nil); in.AttachmentMode != tc.wanted {
				t.Fatal("input defaulted or lost mode")
			}
		})
	}
}

type attachmentPreflightBackend struct {
	*fakeBackend
	backend.TeamBridgeOperations
	installation model.TeamBridgeInstallation
	updates      int
}

func (b *attachmentPreflightBackend) ListTeamBridgeInstallations() ([]model.TeamBridgeInstallation, error) {
	return []model.TeamBridgeInstallation{b.installation}, nil
}
func (b *attachmentPreflightBackend) UpdateTeamBridgeBot(_ string, in model.TeamBridgeBotInput) error {
	b.updates++
	if in.AttachmentMode != nil {
		b.installation.AttachmentMode = in.AttachmentMode
	}
	return nil
}

func TestMissingAttachmentCapabilityFailsBeforeCoreWrites(t *testing.T) {
	b := &attachmentPreflightBackend{
		fakeBackend: &fakeBackend{
			agents:   []model.Agent{{ID: "a", Name: "A"}},
			agent:    model.Agent{ID: "a", Name: "A", RuntimeID: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private"},
			runtimes: []model.Runtime{{ID: "r"}},
		},
		installation: model.TeamBridgeInstallation{ID: "bot", AgentID: "a", ServerURL: "https://mattermost.example.test", Status: "active", Context: model.TeamBridgeContext{Mode: "message"}, AccessMode: "members", DeniedReply: model.TeamBridgeDeniedReply{Mode: "text"}},
	}
	p := model.Project{
		RuntimeSelectors: map[string]model.RuntimeSelector{"r": {ID: "r"}},
		Agents:           []model.AgentSpec{{Name: "A", Description: "Must not be written", RuntimeRef: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private"}},
		TeamBridge:       []model.TeamBridgeBotSpec{{Name: "Bot", Agent: "A", ServerURL: "https://mattermost.example.test", Status: "active", Context: model.TeamBridgeContext{Mode: "message"}, AccessMode: "members", DeniedReply: model.TeamBridgeReplySpec{Mode: "text"}, AttachmentMode: attachmentModeString("links")}},
	}
	if err := (Reconciler{Backend: b}).Apply(p, nil); err == nil || !strings.Contains(err.Error(), "attachment_mode") {
		t.Fatal(err)
	}
	if b.updatedAgent || b.updates != 0 {
		t.Fatal("missing capability left partial writes")
	}
}

func TestInstalledAttachmentSettingMustMatchWhenManaged(t *testing.T) {
	for _, returned := range []*string{nil, attachmentModeString("none"), attachmentModeString("links")} {
		d := model.TeamBridgeBotSpec{ServerURL: "https://mattermost.example.test", AccessMode: "members", AttachmentMode: attachmentModeString("links")}
		a := model.TeamBridgeInstallation{ID: "bot", AgentID: "a", Status: "active", ServerURL: d.ServerURL, AccessMode: "members", AttachmentMode: returned}
		err := verifyInstalledTeamBridge(d, "a", a)
		if (err == nil) != (returned != nil && *returned == "links") {
			t.Fatal("wrong install mode accepted", err)
		}
		d.AttachmentMode = nil
		if err := verifyInstalledTeamBridge(d, "a", a); err != nil {
			t.Fatal("omitted mode became an assertion", err)
		}
	}
}
