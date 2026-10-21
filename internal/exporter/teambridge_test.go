package exporter

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/workspace"
)

type teamBridgeExportBackend struct {
	*fakeBackend
	installations []model.TeamBridgeInstallation
	images        map[string]model.TeamBridgeImage
	listErr       error
	imageErr      error
	calls         int
}

var _ backend.TeamBridgeOperations = (*teamBridgeExportBackend)(nil)

func (f *teamBridgeExportBackend) ListTeamBridgeInstallations() ([]model.TeamBridgeInstallation, error) {
	f.calls++
	return append([]model.TeamBridgeInstallation(nil), f.installations...), f.listErr
}
func (f *teamBridgeExportBackend) DownloadTeamBridgeImage(id string) (model.TeamBridgeImage, error) {
	f.calls++
	if f.imageErr != nil {
		return model.TeamBridgeImage{}, f.imageErr
	}
	image, ok := f.images[id]
	if !ok {
		return model.TeamBridgeImage{}, os.ErrNotExist
	}
	return image, nil
}
func (*teamBridgeExportBackend) UploadTeamBridgeImage(model.TeamBridgeImage) (string, error) {
	panic("mutation")
}
func (*teamBridgeExportBackend) VerifyTeamBridgeBot(model.TeamBridgeBotInput, string, []model.TeamBridgeAllowedUser) error {
	panic("export must not read or verify a bot credential")
}
func (*teamBridgeExportBackend) InstallTeamBridgeBot(string, model.TeamBridgeBotInput) (model.TeamBridgeInstallation, error) {
	panic("mutation")
}
func (*teamBridgeExportBackend) UpdateTeamBridgeBot(string, model.TeamBridgeBotInput) error {
	panic("mutation")
}
func (*teamBridgeExportBackend) RevokeTeamBridgeBot(string) error { panic("mutation") }

func teamBridgeExportFixture() *teamBridgeExportBackend {
	return &teamBridgeExportBackend{
		fakeBackend: exampleBackend(),
		installations: []model.TeamBridgeInstallation{{
			ID: "installation-1", WorkspaceID: "workspace-1", AgentID: "agent-1",
			ServerURL: "https://chat.example.com", BotUserID: "aaaaaaaaaaaaaaaaaaaaaaaaaa", BotUsername: "support",
			Status: "active", Context: model.TeamBridgeContext{Mode: "channel", LookbackHours: 8760}, AccessMode: "allowlist",
			AllowedUsers:    []model.TeamBridgeAllowedUser{{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbb", Username: "sergey"}},
			DeniedReply:     model.TeamBridgeDeniedReply{Mode: "images", ImageAttachmentIDs: []string{"image-2", "image-1"}},
			InstallerUserID: "installer-1", InstalledAt: "2026-10-20T12:00:00Z", CreatedAt: "2026-10-19T12:00:00Z", UpdatedAt: "2026-10-21T12:00:00Z",
		}},
		images: map[string]model.TeamBridgeImage{
			"image-1": {Filename: "../../unsafe.png", Data: []byte("\x89PNG\r\n\x1a\nimage-one")},
			"image-2": {Filename: "reply.gif", Data: []byte("GIF89aimage-two")},
		},
	}
}

func TestTeamBridgeExportOptInRoundTrip(t *testing.T) {
	for _, without := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "without-secrets"}[without], func(t *testing.T) {
			b := teamBridgeExportFixture()
			out := filepath.Join(t.TempDir(), "snapshot")
			result, err := (Exporter{Backend: b}).Export(Options{OutputDir: out, WithTeamBridge: true, WithoutSecrets: without})
			if err != nil {
				t.Fatal(err)
			}
			if result.TeamBridge != 1 || b.calls != 3 || !strings.Contains(strings.Join(result.Warnings, " "), "tokens are not exposed") {
				t.Fatalf("incomplete result: %+v calls=%d", result, b.calls)
			}
			p, err := config.Load(filepath.Join(out, "multica.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			d := p.TeamBridge[0]
			if d.Name != "Unity Developer" || d.Agent != "Unity Developer" || d.Context.LookbackHours != 8760 || d.AllowedUsers[0].ID != "bbbbbbbbbbbbbbbbbbbbbbbbbb" || d.WithoutSecrets != without {
				t.Fatalf("settings lost: %+v", d)
			}
			if len(d.DeniedReply.Images) != 2 || !bytes.Equal(d.DeniedReply.Images[0].Data, b.images["image-2"].Data) || !bytes.Equal(d.DeniedReply.Images[1].Data, b.images["image-1"].Data) {
				t.Fatal("image contents/order lost")
			}
			if without && d.AccessTokenEnv != "" || !without && !strings.HasPrefix(d.AccessTokenEnv, "MULTICA_TEAMBRIDGE_SUPPORT_") {
				t.Fatalf("token reference policy incorrect: %q", d.AccessTokenEnv)
			}
			metadata, err := os.ReadFile(filepath.Join(out, "teambridge/unity-developer/observed.json"))
			if err != nil || !bytes.Contains(metadata, []byte(`"installer_user_id": "installer-1"`)) || !bytes.Contains(metadata, []byte(`"image-2"`)) || !bytes.Contains(metadata, []byte(`"updated_at"`)) {
				t.Fatalf("public source metadata missing: %v", err)
			}
		})
	}
}

func TestTeamBridgeExportDisabledMakesNoIntegrationRequests(t *testing.T) {
	b := teamBridgeExportFixture()
	b.listErr = errors.New("this endpoint must not be touched")
	out := filepath.Join(t.TempDir(), "snapshot")
	result, err := (Exporter{Backend: b}).Export(Options{OutputDir: out})
	if err != nil || b.calls != 0 || result.TeamBridge != 0 {
		t.Fatalf("default export touched integration: %v, calls=%d", err, b.calls)
	}
	if _, err := os.Lstat(filepath.Join(out, "teambridge")); !os.IsNotExist(err) {
		t.Fatal("disabled export generated TeamBridge paths")
	}
	if _, err := (Exporter{Backend: exampleBackend()}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "unsupported"), WithTeamBridge: true}); err == nil {
		t.Fatal("opt-in silently accepted unsupported backend")
	}
}

func TestTeamBridgeForceExportPreservesGroupingAndFailsWithoutOptIn(t *testing.T) {
	b := teamBridgeExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	e := Exporter{Backend: b}
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true}); err != nil {
		t.Fatal(err)
	}
	group := filepath.Join(out, "teambridge/nord")
	if err := os.MkdirAll(group, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(out, "teambridge/unity-developer"), filepath.Join(group, "unity-developer")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(group, "unity-developer/bot.yaml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b.calls = 0
	if _, err := e.Export(Options{OutputDir: out, Force: true}); err == nil || !strings.Contains(err.Error(), "--with-teambridge") || b.calls != 0 {
		t.Fatalf("refresh without opt-in was not blocked safely: %v calls=%d", err, b.calls)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed refresh replaced integration files")
	}
	if _, err := e.Export(Options{OutputDir: out, Force: true, WithTeamBridge: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("grouping directory was lost")
	}
}

func TestTeamBridgeImageFailureLeavesEntireSnapshotUntouched(t *testing.T) {
	b := teamBridgeExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	e := Exporter{Backend: b}
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "agents/unity-developer/AGENT.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b.agents[0].Instructions = "would overwrite old instructions"
	b.imageErr = errors.New("synthetic download failure")
	if _, err := e.Export(Options{OutputDir: out, Force: true, WithTeamBridge: true}); err == nil {
		t.Fatal("incomplete snapshot accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("image failure replaced an earlier resource")
	}
}

func TestTeamBridgeWorkspaceSetExportAndSecretlessRefresh(t *testing.T) {
	b := teamBridgeExportFixture()
	e := WorkspaceExporter{Catalog: workspaceCatalog{items: []backend.Workspace{{ID: "ws", Slug: "nord"}}}, BackendFor: func(string) backend.Backend { return b }}
	out := filepath.Join(t.TempDir(), "set")
	result, err := e.Export(Options{OutputDir: out, WithTeamBridge: true})
	if err != nil || result.TeamBridge != 1 {
		t.Fatalf("workspace set export: %+v %v", result, err)
	}
	secretPath := filepath.Join(out, "nord/teambridge/unity-developer/old-token.txt")
	if err := os.WriteFile(secretPath, []byte("old synthetic secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true, Force: true, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
		t.Fatal("secretless refresh retained a previous generated token file")
	}
	set, err := workspace.Load(filepath.Join(out, "multica.yaml"), config.LoadOptions{})
	if err != nil || !set.Entries[0].Project.TeamBridge[0].WithoutSecrets {
		t.Fatalf("workspace secret policy not propagated: %v", err)
	}
	if _, err := e.Export(Options{OutputDir: out, Force: true}); err == nil || !strings.Contains(err.Error(), "--with-teambridge") {
		t.Fatalf("workspace-set refresh without opt-in accepted: %v", err)
	}
}

func TestTeamBridgeTokenReferenceSupportsUnicodeNamesAndDifferentServers(t *testing.T) {
	a := teamBridgeTokenEnv(model.TeamBridgeInstallation{ServerURL: "https://a.example.com", BotUserID: "aaaaaaaaaaaaaaaaaaaaaaaaaa", BotUsername: "бот"})
	b := teamBridgeTokenEnv(model.TeamBridgeInstallation{ServerURL: "https://b.example.com", BotUserID: "aaaaaaaaaaaaaaaaaaaaaaaaaa", BotUsername: "бот"})
	if a == b || strings.ContainsAny(a, "бот") || !strings.HasPrefix(a, "MULTICA_TEAMBRIDGE_") {
		t.Fatalf("invalid or ambiguous token refs: %q %q", a, b)
	}
}

func TestTeamBridgeFullRefreshPreservesTokenEnvAndProtectsLocalTokenFiles(t *testing.T) {
	b := teamBridgeExportFixture()
	e := Exporter{Backend: b}
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "teambridge/unity-developer/bot.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte(teamBridgeTokenEnv(b.installations[0])), []byte("MY_EXISTING_BOT_TOKEN"))
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("accessTokenEnv: MY_EXISTING_BOT_TOKEN")) {
		t.Fatal("custom token environment reference overwritten")
	}
	data = bytes.ReplaceAll(data, []byte("accessTokenEnv: MY_EXISTING_BOT_TOKEN"), []byte("accessTokenFile: token.txt"))
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(filepath.Dir(path), "token.txt")
	if err := os.WriteFile(secretPath, []byte("synthetic-only-local-copy"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true, Force: true}); err == nil || !strings.Contains(err.Error(), "accessTokenFile") {
		t.Fatalf("full refresh would discard a local credential: %v", err)
	}
	secret, err := os.ReadFile(secretPath)
	if err != nil || string(secret) != "synthetic-only-local-copy" {
		t.Fatal("failed full refresh destroyed the local token file")
	}
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true, Force: true, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
		t.Fatal("explicit secretless refresh retained an old token file")
	}
}

func TestTeamBridgeExportRetainsDormantGrantsAsObservationOnly(t *testing.T) {
	b := teamBridgeExportFixture()
	b.installations[0].AccessMode = "members"
	out := filepath.Join(t.TempDir(), "snapshot")
	result, err := (Exporter{Backend: b}).Export(Options{OutputDir: out, WithTeamBridge: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil || len(p.TeamBridge[0].AllowedUsers) != 0 {
		t.Fatalf("dormant grants became an invalid desired setting: %v", err)
	}
	observed, err := os.ReadFile(filepath.Join(out, "teambridge/unity-developer/observed.json"))
	if err != nil || !bytes.Contains(observed, []byte("bbbbbbbbbbbbbbbbbbbbbbbbbb")) || !strings.Contains(strings.Join(result.Warnings, " "), "dormant") {
		t.Fatal("dormant public metadata was silently dropped")
	}
}
