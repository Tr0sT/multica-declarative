//go:build integration && teambridgeintegration

package integration

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
	"gopkg.in/yaml.v3"
)

func teamBridgeTokenRequests(f *teamBridgeFixture) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, req := range f.requests {
		if strings.HasSuffix(req.path, "/token") {
			count++
		}
	}
	return count
}

func TestForkCLITeamBridgeTokenRestoresExistingWithoutReinstall(t *testing.T) {
	for _, status := range []string{"active", "revoked"} {
		t.Run(status, func(t *testing.T) {
			f, cli := newTeamBridgeTransportFixture(t, true)
			f.mu.Lock()
			f.installation["status"] = status
			f.mu.Unlock()
			project, _, _ := exportAttachmentProject(t, cli, false)
			f.mu.Lock()
			originalToken := f.storedBotToken
			beforePublic, err := yaml.Marshal(f.installation)
			f.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.storedBotToken = "synthetic-rotated-bot-token"
			f.validBotTokens[f.storedBotToken] = true
			f.requests = nil
			f.mu.Unlock()
			r := reconcile.Reconciler{Backend: cli}
			changes, err := r.Plan(project)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, change := range changes {
				if change.Kind == "teambridge" {
					found = change.Action == reconcile.Update && reflect.DeepEqual(change.Fields, []string{"accessToken"})
				}
			}
			if !found || len(f.mutations()) != 0 || len(f.base.mutations()) != 0 {
				t.Fatal("token plan must report a read-only accessToken change")
			}
			planText := fmt.Sprintf("%+v", changes)
			for _, secret := range []string{originalToken, f.storedBotToken} {
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(secret)))
				if strings.Contains(planText, secret) || strings.Contains(planText, digest) {
					t.Fatal("plan disclosed a token or a token derivative")
				}
			}
			if err := r.Apply(project, nil); err != nil {
				t.Fatal(err)
			}
			writes := f.mutations()
			if len(writes) != 1 || writes[0].method != http.MethodPut || !strings.HasSuffix(writes[0].path, "/token") {
				t.Fatal("restoring a token must issue exactly one dedicated token update")
			}
			if f.storedBotToken != originalToken {
				t.Fatal("apply did not restore the exact exported token")
			}
			f.mu.Lock()
			afterPublic, err := yaml.Marshal(f.installation)
			f.mu.Unlock()
			if err != nil || !bytes.Equal(beforePublic, afterPublic) {
				t.Fatal("token restoration modified installation identity, status, settings or audit metadata")
			}
			assertNoop(t, cli, project)
			if err := r.Apply(project, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != 1 || len(f.base.mutations()) != 0 {
				t.Fatal("reapplying the restored token caused extra writes")
			}
		})
	}
}

func TestForkCLITeamBridgeSecretlessImportDoesNotReadMissingToken(t *testing.T) {
	f, cli := newTeamBridgeFixture(t)
	project, dir, _ := exportAttachmentProject(t, cli, false)
	bot := project.TeamBridge[0]
	if err := os.Remove(filepath.Join(bot.SourceDirectory, bot.AccessTokenFile)); err != nil {
		t.Fatal(err)
	}
	bot.Context.LookbackHours = 24
	data, err := yaml.Marshal(bot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bot.SourceDirectory, "bot.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	project, err = config.LoadWithOptions(filepath.Join(dir, "multica.yaml"), config.LoadOptions{WithoutSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.requests = nil
	f.tokenFailureStatus = http.StatusForbidden
	f.mu.Unlock()
	r := reconcile.Reconciler{Backend: cli}
	if _, err := r.Plan(project); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(project, nil); err != nil {
		t.Fatal(err)
	}
	if teamBridgeTokenRequests(f) != 0 || f.storedBotToken != "synthetic-private-bot-token" {
		t.Fatal("secretless import accessed or changed the server token")
	}
	writes := f.mutations()
	if len(writes) != 1 || writes[0].method != http.MethodPatch {
		t.Fatal("secretless import did not update only the public settings")
	}
	assertNoop(t, cli, project)
}

func TestForkCLITeamBridgeTokenRefreshOwnership(t *testing.T) {
	f, cli := newTeamBridgeFixture(t)
	project, dir, _ := exportAttachmentProject(t, cli, false)
	bot := project.TeamBridge[0]
	tokenPath := filepath.Join(bot.SourceDirectory, bot.AccessTokenFile)
	userPath := filepath.Join(bot.SourceDirectory, "operator-notes.txt")
	userContent := []byte("Keep this local user file during refresh.\n")
	if err := os.WriteFile(userPath, userContent, 0600); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.storedBotToken = "synthetic-refreshed-bot-token"
	f.validBotTokens[f.storedBotToken] = true
	f.mu.Unlock()
	ex := exporter.Exporter{Backend: cli}
	if _, err := ex.Export(exporter.Options{OutputDir: dir, WithTeamBridge: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil || string(data) != f.storedBotToken {
		t.Fatal("full refresh did not replace its generated token")
	}
	beforeReads := teamBridgeTokenRequests(f)
	if _, err := ex.Export(exporter.Options{OutputDir: dir, WithTeamBridge: true, Force: true, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if teamBridgeTokenRequests(f) != beforeReads {
		t.Fatal("secretless refresh invoked a token operation")
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatal("secretless refresh retained a previously generated token")
	}
	data, err = os.ReadFile(userPath)
	if err != nil || !bytes.Equal(data, userContent) {
		t.Fatal("refresh lost an unrelated local user file")
	}
	project, err = config.Load(filepath.Join(dir, "multica.yaml"))
	if err != nil || project.TeamBridge[0].AccessTokenFile != "" || project.TeamBridge[0].AccessTokenEnv != "" {
		t.Fatal("secretless refresh retained a token reference")
	}
}

func TestForkCLITeamBridgeTokenFailureIsSafeAndAtomic(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f, cli := newTeamBridgeFixture(t)
			_, dir, _ := exportAttachmentProject(t, cli, false)
			before := teamBridgeSnapshot(t, dir)
			f.mu.Lock()
			f.tokenFailureStatus = status
			f.mu.Unlock()
			_, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, WithTeamBridge: true, Force: true})
			if err == nil {
				t.Fatal("a full export succeeded without its token")
			}
			if strings.Contains(err.Error(), f.storedBotToken) {
				t.Fatal("an error echoed the secret server response")
			}
			if !reflect.DeepEqual(before, teamBridgeSnapshot(t, dir)) {
				t.Fatal("failed secret export modified the previous snapshot")
			}
		})
	}
}

func TestForkCLITeamBridgeMissingManagedTokenFailsBeforeWrites(t *testing.T) {
	f, cli := newTeamBridgeFixture(t)
	project, _, _ := exportAttachmentProject(t, cli, false)
	bot := project.TeamBridge[0]
	if err := os.Remove(filepath.Join(bot.SourceDirectory, bot.AccessTokenFile)); err != nil {
		t.Fatal(err)
	}
	project.Agents[0].Description += " Changed but must not apply."
	if err := (reconcile.Reconciler{Backend: cli}).Apply(project, nil); err == nil {
		t.Fatal("apply ignored an explicitly managed but missing token file")
	}
	if len(f.mutations()) != 0 || len(f.base.mutations()) != 0 {
		t.Fatal("a resource changed before the missing token was detected")
	}
}
