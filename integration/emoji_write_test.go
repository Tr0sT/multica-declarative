//go:build integration && emoji

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func avatarFixture(t *testing.T) (*fixture, *backend.CLI) {
	t.Helper()
	f, cli := newFixture(t)
	binary := os.Getenv("MULTICA_EMOJI_BIN")
	if binary == "" {
		t.Fatal("MULTICA_EMOJI_BIN is required; the write tests must not skip")
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		t.Fatal(err)
	}
	cli.Binary, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.CheckAgentAvatarURL(); err != nil {
		t.Fatal(err)
	}
	return f, cli
}

func TestEmojiAvatarRestoreReplaceClearAndReapply(t *testing.T) {
	f, cli := avatarFixture(t)
	setFixtureAvatar(f, "emoji:🌞")
	p, _ := exportProject(t, cli)
	// Restore from the exported declaration after the remote avatar changes.
	setFixtureAvatar(f, "https://example.invalid/old.png")
	r := reconcile.Reconciler{Backend: cli}
	for _, value := range []string{"emoji:🌞", "emoji:👩🏽‍💻", "emoji:❤️", ""} {
		v := value
		p.Agents[0].AvatarURL = &v
		before := len(f.mutations())
		if err := r.Apply(p, nil); err != nil {
			t.Fatal(err)
		}
		if fixtureAvatar(f) != value {
			t.Fatalf("got %#v, want %q", fixtureAvatar(f), value)
		}
		mutations := f.mutations()[before:]
		if len(mutations) != 1 || mutations[0].method != "PUT" || len(mutations[0].body) != 1 || mutations[0].body["avatar_url"] != value {
			t.Fatalf("avatar-only update affected other fields: %#v", mutations)
		}
		assertNoop(t, cli, p)
		before = len(f.mutations())
		if err := r.Apply(p, nil); err != nil {
			t.Fatal(err)
		}
		if len(f.mutations()) != before {
			t.Fatal("second apply mutated state")
		}
	}
	// Old declarations omit avatarUrl and must leave a live emoji unmanaged.
	p.Agents[0].AvatarURL = nil
	setFixtureAvatar(f, "emoji:🐧")
	before := len(f.mutations())
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != before || fixtureAvatar(f) != "emoji:🐧" {
		t.Fatal("omitted avatar was cleared")
	}
}

func TestEmojiAvatarCreateFromSnapshot(t *testing.T) {
	f, cli := avatarFixture(t)
	setFixtureAvatar(f, "emoji:🐝")
	p, _ := exportProject(t, cli)
	// The fixture's conversation starters are observe-only, not avatar state.
	p.Agents[0].ConversationStarters = nil
	f.mu.Lock()
	f.agent = nil
	f.assigned = nil
	f.mu.Unlock()
	r := reconcile.Reconciler{Backend: cli}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if fixtureAvatar(f) != "emoji:🐝" {
		t.Fatal("new agent lost emoji")
	}
	assertNoop(t, cli, p)
	before := len(f.mutations())
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != before {
		t.Fatal("reapply duplicated or changed agent")
	}
}
