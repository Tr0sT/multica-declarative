package reconcile

import (
	"github.com/Tr0sT/multica-declarative/internal/model"
	"testing"
)

func TestChildMatchingReservesIDsBeforeFallback(t *testing.T) {
	d := []model.AutopilotTriggerSpec{{Kind: "webhook", Label: "Hook"}, {ID: "keep", Kind: "webhook", Label: "Hook"}}
	a := []model.AutopilotTrigger{{ID: "keep", Kind: "webhook", Label: "Hook"}, {ID: "other", Kind: "webhook", Label: "Hook"}}
	m, err := matchTriggers(d, a)
	if err != nil || m[0] != 1 || m[1] != 0 {
		t.Fatalf("%v %v", m, err)
	}
}
func TestChildMatchingRefusesAmbiguousLabelsAndKindChanges(t *testing.T) {
	d := []model.AutopilotTriggerSpec{{Kind: "schedule", Cron: "new", Label: "Schedule"}}
	a := []model.AutopilotTrigger{{ID: "1", Kind: "schedule", Cron: "old", Label: "Schedule"}, {ID: "2", Kind: "schedule", Cron: "older", Label: "Schedule"}}
	if _, err := matchTriggers(d, a); err == nil {
		t.Fatal("ambiguous child matched")
	}
	d[0].ID = "1"
	d[0].Kind = "webhook"
	if _, err := matchTriggers(d, a); err == nil {
		t.Fatal("kind changed in place")
	}
}
func TestForeignResourceHintStillMatchesRefAfterLabelChange(t *testing.T) {
	ref := map[string]string{"url": "https://github.com/example/game"}
	m, err := matchResources([]model.ProjectResourceSpec{{ID: "source", Type: "github_repo", Ref: ref, Label: "New label"}}, []model.ProjectResource{{ID: "destination", Type: "github_repo", Ref: ref, Label: "Old label"}})
	if err != nil || m[0] != 0 {
		t.Fatalf("would duplicate unique resource ref: %v %v", m, err)
	}
}
