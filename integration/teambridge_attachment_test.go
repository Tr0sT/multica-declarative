//go:build integration && teambridgeintegration

package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
	"gopkg.in/yaml.v3"
)

func exportAttachmentProject(t *testing.T, cli *backend.CLI, withoutSecrets bool) (model.Project, string, exporter.Result) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "snapshot")
	result, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, WithTeamBridge: true, WithoutSecrets: withoutSecrets})
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(dir, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return p, dir, result
}

func TestForkCLITeamBridgeAttachmentModeRoundTrip(t *testing.T) {
	for _, withoutSecrets := range []bool{false, true} {
		name := "full"
		if withoutSecrets {
			name = "without-secrets"
		}
		t.Run(name, func(t *testing.T) {
			f, cli := newTeamBridgeFixture(t)
			p, dir, _ := exportAttachmentProject(t, cli, withoutSecrets)
			bot := p.TeamBridge[0]
			if bot.AttachmentMode == nil || *bot.AttachmentMode != "links" {
				t.Fatal("attachment mode not exported")
			}
			observed, err := os.ReadFile(filepath.Join(bot.SourceDirectory, "observed.json"))
			if err != nil || !bytes.Contains(observed, []byte(`"attachment_mode": "links"`)) {
				t.Fatal("observation lost mode", err)
			}
			assertNoop(t, cli, p)
			r := reconcile.Reconciler{Backend: cli}
			for _, mode := range []string{"none", "links", "none"} {
				bot.AttachmentMode = &mode
				data, err := yaml.Marshal(bot)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bot.SourceDirectory, "bot.yaml"), data, 0644); err != nil {
					t.Fatal(err)
				}
				p, err = config.Load(filepath.Join(dir, "multica.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				changes, err := r.Plan(p)
				if err != nil {
					t.Fatal(err)
				}
				last := changes[len(changes)-1]
				if last.Kind != "teambridge" || !reflect.DeepEqual(last.Fields, []string{"attachmentMode"}) {
					t.Fatal(changes)
				}
				before := len(f.mutations())
				if err := r.Apply(p, nil); err != nil {
					t.Fatal(err)
				}
				writes := f.mutations()[before:]
				if len(writes) != 1 || writes[0].method != "PATCH" || writes[0].body["attachment_mode"] != mode {
					t.Fatal(writes)
				}
				if _, present := writes[0].body["access_token"]; present {
					t.Fatal("token changed")
				}
				if len(f.base.mutations()) != 0 {
					t.Fatal("core resources were mutated")
				}
				assertNoop(t, cli, p)
				if err := r.Apply(p, nil); err != nil {
					t.Fatal(err)
				}
				if len(f.mutations()) != before+1 {
					t.Fatal("second apply did not converge")
				}
			}
			// A pre-feature YAML must preserve the actual destination on unrelated edits.
			p.TeamBridge[0].AttachmentMode = nil
			p.TeamBridge[0].Context.LookbackHours = 24
			before := len(f.mutations())
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			writes := f.mutations()[before:]
			if len(writes) != 1 {
				t.Fatal(writes)
			}
			if _, present := writes[0].body["attachment_mode"]; present {
				t.Fatal("omitted setting sent in request")
			}
			f.mu.Lock()
			mode, token := f.installation["attachment_mode"], f.storedBotToken
			f.mu.Unlock()
			if mode != "none" || token != "synthetic-private-bot-token" {
				t.Fatal("legacy import changed destination setting/secret")
			}
		})
	}
}

func TestForkCLITeamBridgeAttachmentModeLegacyAndInvalidServer(t *testing.T) {
	f, cli := newTeamBridgeFixture(t)
	f.mu.Lock()
	delete(f.installation, "attachment_mode")
	f.mu.Unlock()
	p, dir, result := exportAttachmentProject(t, cli, false)
	if p.TeamBridge[0].AttachmentMode != nil || !strings.Contains(strings.Join(result.Warnings, "\n"), "attachmentMode") {
		t.Fatal("unavailable field silently defaulted or lost")
	}
	assertNoop(t, cli, p)
	mode := "links"
	p.TeamBridge[0].AttachmentMode = &mode
	p.Skills[0].Description = "This must not be written"
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err == nil || !strings.Contains(err.Error(), "attachment_mode") {
		t.Fatal(err)
	}
	if len(f.mutations()) != 0 || len(f.base.mutations()) != 0 {
		t.Fatal("preflight left partial changes")
	}
	before := teamBridgeSnapshot(t, dir)
	for _, invalid := range []any{nil, "", "unknown-mode", 123} {
		f.mu.Lock()
		f.installation["attachment_mode"] = invalid
		f.mu.Unlock()
		if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, WithTeamBridge: true, Force: true}); err == nil {
			t.Fatal("invalid API setting accepted")
		}
		if !reflect.DeepEqual(before, teamBridgeSnapshot(t, dir)) {
			t.Fatal("failed refresh replaced snapshot")
		}
	}
}

func TestForkCLITeamBridgeAttachmentModeIgnoredWriteFails(t *testing.T) {
	f, cli := newTeamBridgeFixture(t)
	p, _, _ := exportAttachmentProject(t, cli, true)
	mode := "none"
	p.TeamBridge[0].AttachmentMode = &mode
	f.mu.Lock()
	f.ignoreAttachmentMode = true
	f.mu.Unlock()
	err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil)
	if err == nil || !strings.Contains(err.Error(), "attachmentMode") {
		t.Fatal("ignored write reported as success", err)
	}
	if len(f.mutations()) != 1 {
		t.Fatal("unexpected number of writes")
	}
}

func TestForkCLITeamBridgeAttachmentModeInstallAndReactivate(t *testing.T) {
	for _, reactivate := range []bool{false, true} {
		name := "create-explicit"
		if reactivate {
			name = "reactivate-legacy-omission"
		}
		t.Run(name, func(t *testing.T) {
			f, cli := newTeamBridgeTransportFixture(t, true)
			if reactivate {
				f.mu.Lock()
				f.installation["status"] = "revoked"
				f.mu.Unlock()
			}
			p, _, _ := exportAttachmentProject(t, cli, false)
			t.Setenv(p.TeamBridge[0].AccessTokenEnv, f.storedBotToken)
			wanted := "none"
			if reactivate {
				wanted = "links"
				p.TeamBridge[0].Status = "active"
				p.TeamBridge[0].AttachmentMode = nil
			} else {
				p.TeamBridge[0].AttachmentMode = &wanted
				f.mu.Lock()
				f.installed = false
				f.mu.Unlock()
			}
			r := reconcile.Reconciler{Backend: cli}
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, write := range f.mutations() {
				if strings.Contains(write.path, "/mattermost/install?") {
					found = true
					if write.body["attachment_mode"] != wanted {
						t.Fatal("installation lost requested/preserved setting")
					}
				}
			}
			if !found {
				t.Fatal("installation was not exercised")
			}
			assertNoop(t, cli, p)
			before := len(f.mutations())
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != before {
				t.Fatal("repeated install did not converge")
			}
		})
	}
}
