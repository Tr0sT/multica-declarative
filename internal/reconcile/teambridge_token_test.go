package reconcile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestTeamBridgeManagedTokenRestoresExistingActiveAndRevokedWithoutSettingsWrites(t *testing.T) {
	for _, status := range []string{"active", "revoked"} {
		t.Run(status, func(t *testing.T) {
			b, p := teamBridgeFixture(t)
			b.installations[0].Status, p.TeamBridge[0].Status = status, status
			before := b.installations[0]
			const secret = "synthetic-restored-token"
			t.Setenv("TEAMBRIDGE_RESTORE_TOKEN", secret)
			p.TeamBridge[0].AccessTokenEnv = "TEAMBRIDGE_RESTORE_TOKEN"
			r := Reconciler{Backend: b}
			changes, err := r.Plan(p)
			if err != nil {
				t.Fatal(err)
			}
			change := changes[len(changes)-1]
			if change.Action != Update || !reflect.DeepEqual(change.Fields, []string{"accessToken"}) {
				t.Fatal("token drift is not represented safely")
			}
			if strings.Contains(FormatChange(change), secret) || b.tokenWrites != 0 {
				t.Fatal("plan wrote or exposed a token")
			}
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if b.storedToken != secret || b.tokenWrites != 1 || b.installs+b.updates+b.revokes+b.uploads != 0 || !reflect.DeepEqual(before, b.installations[0]) {
				t.Fatal("token restore changed public installation state")
			}
			verified := b.verifications
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			changes, err = r.Plan(p)
			if err != nil || changes[len(changes)-1].Action != Noop || b.tokenWrites != 1 || b.verifications != verified {
				t.Fatal("token round trip did not converge")
			}
		})
	}
}

func TestTeamBridgeManagedMissingAndInvalidTokensFailBeforeAnyWrites(t *testing.T) {
	for _, scenario := range []string{"missing-env", "missing-file", "empty-file", "public-file", "token-read-denied", "invalid-new-token", "foreign-bot"} {
		t.Run(scenario, func(t *testing.T) {
			if runtime.GOOS == "windows" && scenario == "public-file" {
				t.Skip("POSIX permissions")
			}
			b, p := teamBridgeFixture(t)
			p.Skills[0].Description = "must not be written"
			d := &p.TeamBridge[0]
			d.SourceDirectory, d.AccessTokenFile = t.TempDir(), "token"
			const secret = "synthetic-validation-sentinel"
			if scenario != "missing-file" {
				if err := os.WriteFile(filepath.Join(d.SourceDirectory, "token"), []byte(secret), 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "missing-env":
				d.AccessTokenFile, d.AccessTokenEnv = "", "MISSING_SYNTHETIC_TOKEN"
				t.Setenv(d.AccessTokenEnv, "")
			case "empty-file":
				if err := os.WriteFile(filepath.Join(d.SourceDirectory, "token"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "public-file":
				if err := os.Chmod(filepath.Join(d.SourceDirectory, "token"), 0644); err != nil {
					t.Fatal(err)
				}
			case "token-read-denied":
				b.tokenErr = errors.New("HTTP 403 administrative access required")
			case "invalid-new-token", "foreign-bot":
				b.verifyErr = errors.New("token does not belong to the expected active bot")
			}
			if err := (Reconciler{Backend: b}).Apply(p, nil); err == nil || strings.Contains(err.Error(), secret) {
				t.Fatal("known token failure was ignored or disclosed")
			}
			if b.updatedSkill || b.tokenWrites+b.installs+b.updates+b.revokes+b.uploads != 0 || b.storedToken != "synthetic" {
				t.Fatal("preflight failure caused writes")
			}
		})
	}
}

func TestTeamBridgeSecretlessAndLegacyDeclarationsNeverReadTokens(t *testing.T) {
	for _, policy := range []string{"legacy-unmanaged", "workspace-omit", "bot-omit"} {
		t.Run(policy, func(t *testing.T) {
			b, p := teamBridgeFixture(t)
			b.tokenErr = errors.New("must not read token")
			d := &p.TeamBridge[0]
			d.DeniedReply.Text = "safe nonsecret update"
			if policy != "legacy-unmanaged" {
				d.SourceDirectory, d.AccessTokenFile = t.TempDir(), "missing-token"
			}
			p.WithoutSecrets, d.WithoutSecrets = policy == "workspace-omit", policy == "bot-omit"
			r := Reconciler{Backend: b}
			if _, err := r.Plan(p); err != nil {
				t.Fatal(err)
			}
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if b.tokenReads+b.tokenWrites+b.verifications+b.installs != 0 || b.updates != 1 || b.storedToken != "synthetic" {
				t.Fatal("unmanaged token was accessed or changed")
			}
		})
	}
}

func TestTeamBridgeTokenFileRestoresNewBotThenIsIdempotent(t *testing.T) {
	b, p := teamBridgeFixture(t)
	b.installations = nil
	const token = "synthetic-file-install-token"
	d := &p.TeamBridge[0]
	d.SourceDirectory, d.AccessTokenFile = t.TempDir(), "token"
	if err := os.WriteFile(filepath.Join(d.SourceDirectory, "token"), []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	r := Reconciler{Backend: b}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.storedToken != token || b.installs != 1 || b.tokenWrites != 0 {
		t.Fatal("new bot token not restored")
	}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.installs != 1 || b.tokenWrites+b.updates != 0 {
		t.Fatal("second import caused redundant writes")
	}
}
