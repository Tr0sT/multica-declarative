package reconcile

import (
	"github.com/Tr0sT/multica-declarative/internal/model"
	"testing"
)

func TestLegacyAgentAvatarsCannotTriggerWritesOrUnarchive(t *testing.T) {
	stamp := "2026-01-01T00:00:00Z"
	live, old := "emoji:live", "emoji:"
	b := &fakeBackend{
		agents:   []model.Agent{{ID: "a", Name: "A"}},
		agent:    model.Agent{ID: "a", Name: "A", RuntimeID: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private", AvatarURL: &live, ArchivedAt: &stamp},
		runtimes: []model.Runtime{{ID: "r"}},
	}
	p := model.Project{RuntimeSelectors: map[string]model.RuntimeSelector{"r": {ID: "r"}}, Agents: []model.AgentSpec{
		{Name: "A", RuntimeRef: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private", AvatarURL: &old, AvatarFile: "/nonexistent/avatar.png", Archived: true},
	}}
	r := Reconciler{Backend: b}
	changes, err := r.Plan(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if change.Action != Noop {
			t.Fatal(change)
		}
	}
	if err := r.Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if b.restored || b.archived || b.updatedAgent || *b.agent.AvatarURL != live {
		t.Fatal("avatar drift changed archived agent")
	}
}

func TestSquadAvatarDriftIsUnmanaged(t *testing.T) {
	live := "emoji:live"
	for _, old := range []string{"", "emoji:old", "https://example.invalid/old.png"} {
		desired := model.SquadSpec{Name: "S", Leader: "A", AvatarURL: old}
		actual := model.Squad{ID: "s", Name: "S", LeaderID: "a", AvatarURL: &live}
		agents := map[string]model.Agent{"A": {ID: "a", Name: "A"}}
		members := []model.SquadMember{{MemberID: "a", MemberType: "agent", Role: "leader"}}
		if fields := diffSquad(desired, actual, members, agents); len(fields) != 0 {
			t.Fatal(fields)
		}
		if squadInput(desired, "a").AvatarURL != "" {
			t.Fatal("squad input leaked legacy avatar")
		}
	}
}
