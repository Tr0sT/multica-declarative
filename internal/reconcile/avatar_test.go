package reconcile

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

type rejectAvatarHTTP struct{ t *testing.T }

func (r rejectAvatarHTTP) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Error("must not download emoji")
	return nil, fmt.Errorf("unexpected download")
}

func TestReplacingEmojiWithFileNeedsNoDownload(t *testing.T) {
	r := Reconciler{HTTPClient: &http.Client{Transport: rejectAvatarHTTP{t}}}
	different, err := r.avatarDiffers("unused-until-upload.png", "emoji:🐝")
	if err != nil || !different {
		t.Fatal(different, err)
	}
}

type avatarBackend struct {
	*fakeBackend
	values []string
	checks int
}

func (b *avatarBackend) CheckAgentAvatarURL() error { b.checks++; return nil }
func (b *avatarBackend) SetAgentAvatarURL(id, value string) error {
	b.values = append(b.values, value)
	b.agent.AvatarURL = &value
	return nil
}

func TestArchivedEmojiAvatarIsRestoredUpdatedAndRearchived(t *testing.T) {
	stamp := "2026-01-01T00:00:00Z"
	old, desired := "emoji:🌞", "emoji:🐝"
	b := &avatarBackend{fakeBackend: &fakeBackend{
		agents:   []model.Agent{{ID: "a", Name: "A"}},
		agent:    model.Agent{ID: "a", Name: "A", RuntimeID: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private", AvatarURL: &old, ArchivedAt: &stamp},
		runtimes: []model.Runtime{{ID: "r"}},
	}}
	p := model.Project{RuntimeSelectors: map[string]model.RuntimeSelector{"r": {ID: "r"}}, Agents: []model.AgentSpec{
		{Name: "A", RuntimeRef: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private", AvatarURL: &desired, Archived: true},
	}}
	if err := (Reconciler{Backend: b}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if !b.restored || !b.archived || b.updatedAgent || len(b.values) != 1 || b.values[0] != desired || b.checks != 1 {
		t.Fatalf("incorrect avatar update: %#v %#v", b, b.fakeBackend)
	}
}
