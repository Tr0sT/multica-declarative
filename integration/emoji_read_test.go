//go:build integration

package integration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func TestOfficialCLIEmojiExportAndNoop(t *testing.T) {
	for _, value := range []string{"emoji:🌞", "emoji:👩🏽‍💻", "emoji:❤️"} {
		t.Run(value, func(t *testing.T) {
			f, cli := newFixture(t)
			setFixtureAvatar(f, value)
			out := filepath.Join(t.TempDir(), "snapshot")
			result, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: out})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Warnings) != 0 {
				t.Fatal(result.Warnings)
			}
			p, err := config.Load(filepath.Join(out, "multica.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if p.Agents[0].AvatarURL == nil || *p.Agents[0].AvatarURL != value {
				t.Fatal("emoji lost")
			}
			assertNoop(t, cli, p)
			if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != 0 {
				t.Fatal("unchanged emoji triggered writes")
			}
		})
	}
}

func TestOfficialCLIEmojiUnsupportedRestoreFailsBeforeAnyWrite(t *testing.T) {
	f, cli := newFixture(t)
	p, _ := exportProject(t, cli)
	value := "emoji:🐝"
	p.Agents[0].AvatarURL = &value
	// Force a skill update that must not occur before the missing CLI capability
	// is detected. The read-only plan may still show the requested avatar drift.
	p.Skills[0].Description = "Changed skill"
	if _, err := (reconcile.Reconciler{Backend: cli}).Plan(p); err != nil {
		t.Fatal(err)
	}
	err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil)
	if err == nil || !strings.Contains(err.Error(), "--avatar-url") {
		t.Fatal(err)
	}
	if len(f.mutations()) != 0 {
		t.Fatal("partial mutation with unsupported CLI")
	}
}

func setFixtureAvatar(f *fixture, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agent["avatar_url"] = value
}
func fixtureAvatar(f *fixture) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agent["avatar_url"]
}
