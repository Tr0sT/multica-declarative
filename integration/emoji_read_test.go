//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func TestOfficialCLIAvatarsAreUnmanaged(t *testing.T) {
	for _, value := range []string{"emoji:🌞", "emoji:👩🏽‍💻", "emoji:❤️", "emoji:", "https://example.invalid/avatar.png", ""} {
		t.Run(value, func(t *testing.T) {
			f, cli := newFixture(t)
			setFixtureAvatar(f, value)
			out := filepath.Join(t.TempDir(), "snapshot")
			if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: out}); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(out, "agents/builder/agent.yaml")
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "avatarUrl:") || strings.Contains(string(data), "avatarFile:") {
				t.Fatal("avatar exported")
			}
			// Old declarations remain usable without their avatar files or any CLI patch.
			data = []byte(strings.Replace(string(data), "multica:\n", "multica:\n    avatarUrl: \"emoji:old\"\n    avatarFile: missing-avatar.png\n", 1))
			if err := os.WriteFile(file, data, 0644); err != nil {
				t.Fatal(err)
			}
			p, err := config.Load(filepath.Join(out, "multica.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if p.Agents[0].AvatarURL != nil || p.Agents[0].AvatarFile != "" {
				t.Fatal("legacy fields not ignored")
			}
			assertNoop(t, cli, p)
			r := reconcile.Reconciler{Backend: cli}
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != 0 {
				t.Fatal("avatar drift triggered a write")
			}
			// Exercise direct callers too, and ensure real managed changes still work.
			legacy := "emoji:"
			p.Agents[0].AvatarURL = &legacy
			p.Agents[0].AvatarFile = "/nonexistent/avatar.png"
			p.Agents[0].Description = "Changed description"
			p.Skills[0].Description = "Changed skill"
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if fixtureAvatar(f) != value {
				t.Fatal("managed update changed live avatar")
			}
			if len(f.mutations()) < 2 {
				t.Fatal("managed updates were skipped")
			}
			assertNoAvatarRequests(t, f)
			assertNoop(t, cli, p)
			before := len(f.mutations())
			if err := r.Apply(p, nil); err != nil {
				t.Fatal(err)
			}
			if len(f.mutations()) != before {
				t.Fatal("second apply did not converge")
			}
		})
	}
}

func TestOfficialCLICreatesAgentWithoutRestoringLegacyAvatar(t *testing.T) {
	f, cli := newFixture(t)
	setFixtureAvatar(f, "emoji:old")
	p, _ := exportProject(t, cli)
	legacy := "emoji:old"
	p.Agents[0].AvatarURL = &legacy
	p.Agents[0].AvatarFile = "/nonexistent/avatar.png"
	p.Agents[0].ConversationStarters = nil
	f.mu.Lock()
	f.agent, f.assigned = nil, nil
	f.mu.Unlock()
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoAvatarRequests(t, f)
	assertNoop(t, cli, p)
}

func assertNoAvatarRequests(t *testing.T, f *fixture) {
	t.Helper()
	for _, mutation := range f.mutations() {
		if strings.Contains(mutation.path, "/avatar") {
			t.Fatal("avatar endpoint called")
		}
		if _, ok := mutation.body["avatar_url"]; ok {
			t.Fatal("avatar in mutation payload")
		}
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
