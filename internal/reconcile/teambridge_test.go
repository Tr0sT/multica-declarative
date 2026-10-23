package reconcile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

type teamBridgeMemory struct {
	*fakeBackend
	lastVerification                                          model.TeamBridgeBotInput
	verifiedUsers                                             []model.TeamBridgeAllowedUser
	installations                                             []model.TeamBridgeInstallation
	images                                                    map[string]model.TeamBridgeImage
	reads, uploads, installs, updates, revokes, verifications int
	listErr, verifyErr                                        error
	wrongInstallIdentity                                      bool
	lastInput                                                 model.TeamBridgeBotInput
	storedToken                                               string
	tokenReads, tokenWrites                                   int
	tokenErr                                                  error
}

func (b *teamBridgeMemory) ListTeamBridgeInstallations() ([]model.TeamBridgeInstallation, error) {
	b.reads++
	return b.installations, b.listErr
}
func (b *teamBridgeMemory) DownloadTeamBridgeImage(id string) (model.TeamBridgeImage, error) {
	image, ok := b.images[id]
	if !ok {
		return image, errors.New("missing image")
	}
	return image, nil
}
func (b *teamBridgeMemory) UploadTeamBridgeImage(image model.TeamBridgeImage) (string, error) {
	b.uploads++
	id := fmt.Sprintf("new-image-%d", b.uploads)
	b.images[id] = image
	return id, nil
}
func (b *teamBridgeMemory) VerifyTeamBridgeBot(in model.TeamBridgeBotInput, _ string, users []model.TeamBridgeAllowedUser) error {
	b.verifications++
	b.lastVerification, b.verifiedUsers = in, users
	if in.AccessToken == "" {
		return errors.New("missing verification token")
	}
	return b.verifyErr
}
func (b *teamBridgeMemory) InstallTeamBridgeBot(agentID string, in model.TeamBridgeBotInput) (model.TeamBridgeInstallation, error) {
	b.installs++
	b.lastInput = in
	b.storedToken = in.AccessToken
	a := model.TeamBridgeInstallation{ID: "installed", AgentID: agentID, ServerURL: in.ServerURL, BotUserID: "bot-native-id", Status: "active", Context: in.Context, AccessMode: in.AccessMode, DeniedReply: in.DeniedReply}
	for _, name := range in.AllowedUsernames {
		a.AllowedUsers = append(a.AllowedUsers, model.TeamBridgeAllowedUser{ID: "native-" + name, Username: name})
	}
	if b.wrongInstallIdentity {
		a.BotUserID = "unexpected-bot"
	}
	b.installations = []model.TeamBridgeInstallation{a}
	return a, nil
}
func (b *teamBridgeMemory) UpdateTeamBridgeBot(id string, in model.TeamBridgeBotInput) error {
	b.updates++
	b.lastInput = in
	if in.AccessToken != "" {
		return errors.New("update must not receive credentials")
	}
	for i := range b.installations {
		if b.installations[i].ID != id {
			continue
		}
		a := &b.installations[i]
		a.Context, a.AccessMode, a.DeniedReply = in.Context, in.AccessMode, in.DeniedReply
		previous := map[string]string{}
		for _, u := range a.AllowedUsers {
			previous[u.Username] = u.ID
		}
		if in.AccessMode == "allowlist" {
			a.AllowedUsers = nil
			for _, name := range in.AllowedUsernames {
				uid := previous[name]
				if uid == "" {
					uid = "native-" + name
				}
				a.AllowedUsers = append(a.AllowedUsers, model.TeamBridgeAllowedUser{ID: uid, Username: name})
			}
		}
		return nil
	}
	return errors.New("missing installation")
}
func (b *teamBridgeMemory) RevokeTeamBridgeBot(id string) error {
	b.revokes++
	for i := range b.installations {
		if b.installations[i].ID == id {
			b.installations[i].Status = "revoked"
			return nil
		}
	}
	return errors.New("missing installation")
}

func (b *teamBridgeMemory) ExportTeamBridgeToken(string) (string, error) {
	b.tokenReads++
	return b.storedToken, b.tokenErr
}
func (b *teamBridgeMemory) ImportTeamBridgeToken(_ string, token string) error {
	b.tokenWrites++
	b.storedToken = token
	return nil
}

func teamBridgeFixture(t *testing.T) (*teamBridgeMemory, model.Project) {
	t.Helper()
	fake := existingBackend()
	p := exampleProject(t)
	// Keep core resources converged so any optional integration writes are clear.
	fake.skill.Description, fake.skill.Content, fake.skill.Files = p.Skills[0].Description, p.Skills[0].Content, nil
	fake.agent.Instructions, fake.agent.Model = p.Agents[0].Instructions, p.Agents[0].ModelID
	fake.agentSkills = []model.SkillSummary{{ID: "skill-1", Name: "unity"}}
	d := model.TeamBridgeBotSpec{Name: "Review bot", Agent: "Unity Developer", ServerURL: "https://chat.example.com", BotUserID: "bot-native-id", BotUsername: "review", Status: "active", Context: model.TeamBridgeContext{Mode: "channel", LookbackHours: 8760}, AccessMode: "allowlist", AllowedUsers: []model.TeamBridgeAllowedUser{{ID: "native-alice", Username: "alice"}}, DeniedReply: model.TeamBridgeReplySpec{Mode: "text", Text: "No access"}}
	p.TeamBridge = []model.TeamBridgeBotSpec{d}
	b := &teamBridgeMemory{storedToken: "synthetic", fakeBackend: fake, images: map[string]model.TeamBridgeImage{}, installations: []model.TeamBridgeInstallation{{ID: "installation", AgentID: "agent-1", ServerURL: d.ServerURL, BotUserID: d.BotUserID, BotUsername: d.BotUsername, Status: d.Status, Context: d.Context, AccessMode: d.AccessMode, AllowedUsers: append([]model.TeamBridgeAllowedUser{}, d.AllowedUsers...), DeniedReply: model.TeamBridgeDeniedReply{Mode: d.DeniedReply.Mode, Text: d.DeniedReply.Text}, InstallerUserID: "original-installer"}}}
	return b, p
}

func TestTeamBridgeUnmanagedByDefaultAndOmitIgnoresMissingToken(t *testing.T) {
	b, p := teamBridgeFixture(t)
	plain := p
	plain.TeamBridge = nil
	if _, err := (Reconciler{Backend: b}).Plan(plain); err != nil {
		t.Fatal(err)
	}
	if b.reads != 0 {
		t.Fatal("legacy declaration contacted optional integration")
	}
	p.TeamBridge[0].AccessTokenEnv = ""
	p.TeamBridge[0].AccessTokenFile = "unreadable-token"
	p.WithoutSecrets = true
	if err := (Reconciler{Backend: b}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.installs+b.updates+b.uploads+b.revokes+b.verifications != 0 {
		t.Fatal("no-op accessed credentials or wrote optional state")
	}
}

func TestTeamBridgePatchPreservesSecretsOwnerAndUndeclaredBots(t *testing.T) {
	b, p := teamBridgeFixture(t)
	b.installations = append(b.installations, model.TeamBridgeInstallation{ID: "other", AgentID: "other-agent", Status: "active"})
	p.WithoutSecrets = true
	p.TeamBridge[0].WithoutSecrets = true
	p.TeamBridge[0].Context = model.TeamBridgeContext{Mode: "thread"}
	p.TeamBridge[0].DeniedReply.Text = "Changed refusal"
	p.TeamBridge[0].AllowedUsers = append(p.TeamBridge[0].AllowedUsers, model.TeamBridgeAllowedUser{Username: "bob"})
	r := Reconciler{Backend: b}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.updates != 1 || b.installs+b.revokes+b.verifications != 0 || b.lastInput.AccessToken != "" {
		t.Fatalf("unexpected calls: %+v", b)
	}
	if b.installations[0].InstallerUserID != "original-installer" || b.installations[1].ID != "other" {
		t.Fatal("owner or undeclared installation changed")
	}
}

func TestTeamBridgeCreatePreflightsCredentialsAndRemapsImages(t *testing.T) {
	b, p := teamBridgeFixture(t)
	b.installations = nil
	p.TeamBridge[0].AccessTokenEnv = "TEAMBRIDGE_TEST_TOKEN"
	t.Setenv("TEAMBRIDGE_TEST_TOKEN", "synthetic-bot-token")
	p.TeamBridge[0].DeniedReply = model.TeamBridgeReplySpec{Mode: "images", Images: []model.TeamBridgeImage{{Filename: "a.png", Data: []byte("image-a")}, {Filename: "b.png", Data: []byte("image-b")}}}
	r := Reconciler{Backend: b}
	changes, err := r.Plan(p)
	if err != nil {
		t.Fatal(err)
	}
	if changes[len(changes)-1].Action != Create || b.installs+b.uploads != 0 {
		t.Fatal("plan mutated integration")
	}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.installs != 1 || b.uploads != 2 || len(b.lastInput.DeniedReply.ImageAttachmentIDs) != 2 {
		t.Fatal("new images were not remapped")
	}
	if slices.Equal(b.lastInput.DeniedReply.ImageAttachmentIDs, []string{"a.png", "b.png"}) {
		t.Fatal("local paths used as IDs")
	}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.installs != 1 || b.uploads != 2 || b.updates != 0 {
		t.Fatal("second apply did not converge")
	}
}

func TestTeamBridgeImagesReuseBytesAcrossReorderAndUpdate(t *testing.T) {
	b, p := teamBridgeFixture(t)
	a := model.TeamBridgeImage{Filename: "a.png", Data: []byte("image-a")}
	c := model.TeamBridgeImage{Filename: "c.png", Data: []byte("image-c")}
	b.images["old-a"], b.images["old-b"] = a, model.TeamBridgeImage{Filename: "b.png", Data: []byte("image-b")}
	b.installations[0].DeniedReply = model.TeamBridgeDeniedReply{Mode: "images", ImageAttachmentIDs: []string{"old-a", "old-b"}}
	p.TeamBridge[0].DeniedReply = model.TeamBridgeReplySpec{Mode: "images", Images: []model.TeamBridgeImage{b.images["old-b"], a, c}}
	r := Reconciler{Backend: b}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(b.lastInput.DeniedReply.ImageAttachmentIDs, []string{"old-b", "old-a", "new-image-1"}) || b.uploads != 1 {
		t.Fatal("unchanged image bytes uploaded again or order lost")
	}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.updates != 1 || b.uploads != 1 {
		t.Fatal("image update did not converge")
	}
}

func TestTeamBridgeKnownFailuresPrecedeCoreWrites(t *testing.T) {
	for _, scenario := range []string{"missing-token", "secrets-omit", "unsupported", "bad-credentials", "missing-image", "account-reused"} {
		t.Run(scenario, func(t *testing.T) {
			b, p := teamBridgeFixture(t)
			p.Skills[0].Description = "change core too"
			switch scenario {
			case "missing-token":
				b.installations = nil
				p.TeamBridge[0].AccessTokenEnv = ""
			case "secrets-omit":
				b.installations = nil
				p.WithoutSecrets = true
			case "unsupported":
				b.listErr = errors.New("extension unavailable")
			case "bad-credentials":
				b.installations = nil
				p.TeamBridge[0].AccessTokenEnv = "TEAMBRIDGE_TEST_TOKEN"
				t.Setenv("TEAMBRIDGE_TEST_TOKEN", "synthetic-token")
				b.verifyErr = errors.New("identity verification failed")
			case "missing-image":
				b.installations[0].DeniedReply = model.TeamBridgeDeniedReply{Mode: "images", ImageAttachmentIDs: []string{"gone"}}
				p.TeamBridge[0].DeniedReply = model.TeamBridgeReplySpec{Mode: "images", Images: []model.TeamBridgeImage{{Data: []byte("image")}}}
			case "account-reused":
				p.TeamBridge[0].AllowedUsers[0].ID = "different-account"
			}
			if err := (Reconciler{Backend: b}).Apply(p, nil); err == nil {
				t.Fatal("expected preflight failure")
			}
			if b.updatedSkill || b.installs+b.updates+b.uploads+b.revokes != 0 {
				t.Fatal("writes happened before failed preflight")
			}
		})
	}
}

func TestTeamBridgeRevokedRecordsNeverSilentlyActivate(t *testing.T) {
	b, p := teamBridgeFixture(t)
	p.TeamBridge[0].Status = "revoked"
	b.installations[0].Status = "revoked"
	r := Reconciler{Backend: b}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.installs+b.updates+b.revokes != 0 {
		t.Fatal("revoked record changed")
	}
	p.TeamBridge[0].Context.Mode = "message"
	p.TeamBridge[0].Context.LookbackHours = 0
	if _, err := r.Plan(p); err == nil {
		t.Fatal("editing revoked record would require activation")
	}
	b.installations = nil
	if _, err := r.Plan(p); err == nil {
		t.Fatal("revoked record recreated by activating bot")
	}
}

func TestTeamBridgeExplicitRevokeAndReactivate(t *testing.T) {
	b, p := teamBridgeFixture(t)
	p.TeamBridge[0].Status = "revoked"
	r := Reconciler{Backend: b}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.revokes != 1 || b.installs != 0 {
		t.Fatal("explicit status change not respected")
	}
	p.TeamBridge[0].Status = "active"
	p.TeamBridge[0].AccessTokenEnv = "TEAMBRIDGE_TEST_TOKEN"
	t.Setenv("TEAMBRIDGE_TEST_TOKEN", "synthetic")
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.installs != 1 {
		t.Fatal("reactivation not installed")
	}
}

func TestTeamBridgePinnedNewAccountRequiresVerification(t *testing.T) {
	b, p := teamBridgeFixture(t)
	p.TeamBridge[0].AllowedUsers = append(p.TeamBridge[0].AllowedUsers, model.TeamBridgeAllowedUser{ID: "native-bob", Username: "bob"})
	r := Reconciler{Backend: b}
	if _, err := r.Plan(p); err == nil {
		t.Fatal("new pinned identity accepted without verification")
	}
	p.TeamBridge[0].AccessTokenEnv = "TEAMBRIDGE_TEST_TOKEN"
	t.Setenv("TEAMBRIDGE_TEST_TOKEN", "synthetic")
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.verifications != 1 || b.updates != 1 || b.installs != 0 {
		t.Fatal("expected verified settings patch")
	}
	if !slices.Equal(b.lastVerification.AllowedUsernames, []string{"bob"}) || len(b.verifiedUsers) != 1 || b.verifiedUsers[0].Username != "bob" {
		t.Fatal("existing stable grants must not be re-resolved when adding a new pinned account")
	}
}

func TestTeamBridgeDormantGrantsDoNotCauseDrift(t *testing.T) {
	b, p := teamBridgeFixture(t)
	p.TeamBridge[0].AccessMode = "members"
	p.TeamBridge[0].AllowedUsers = nil
	r := Reconciler{Backend: b}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(b.installations[0].AllowedUsers) != 1 {
		t.Fatal("fixture must preserve dormant grants like the fork")
	}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.updates != 1 {
		t.Fatal("dormant grant caused endless patches")
	}
	p.TeamBridge[0].DeniedReply.Text = "Different refusal"
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.lastInput.AllowedUsernames != nil {
		t.Fatal("non-allowlist patch attempted to send a dormant list")
	}
}

func TestTeamBridgeRejectsIdentityReplacementAndRevokesInconsistentCreate(t *testing.T) {
	b, p := teamBridgeFixture(t)
	p.TeamBridge[0].BotUserID = "different-bot"
	if _, err := (Reconciler{Backend: b}).Plan(p); err == nil {
		t.Fatal("destructive replacement allowed")
	}
	p.TeamBridge[0].BotUserID = "bot-native-id"
	p.TeamBridge[0].AccessTokenEnv = "TEAMBRIDGE_TEST_TOKEN"
	t.Setenv("TEAMBRIDGE_TEST_TOKEN", "synthetic")
	b.installations = nil
	b.wrongInstallIdentity = true
	if err := (Reconciler{Backend: b}).Apply(p, nil); err == nil {
		t.Fatal("inconsistent installation accepted")
	}
	if b.revokes != 1 {
		t.Fatal("inconsistent new bot left active")
	}
}

func TestTeamBridgeTokenFilesArePrivateAndReadOnlyWhenNeeded(t *testing.T) {
	dir := t.TempDir()
	const secret = "private-token-sentinel"
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d := model.TeamBridgeBotSpec{SourceDirectory: dir, AccessTokenFile: "token"}
	if token, err := teamBridgeToken(d, false); err != nil || token != secret {
		t.Fatal("token file not read")
	}
	if _, err := teamBridgeToken(d, true); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("omission policy leaked/read token")
	}
	if err := os.Symlink(filepath.Join(dir, "token"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	d.AccessTokenFile = "link"
	if _, err := teamBridgeToken(d, false); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("symlink token accepted or leaked")
	}
	for _, path := range []string{"../token", filepath.Join(dir, "token"), "."} {
		d.AccessTokenFile = path
		if _, err := teamBridgeToken(d, false); err == nil {
			t.Fatal("escaping token path accepted")
		}
	}
}
