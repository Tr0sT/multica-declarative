package backend

import (
	"strings"
	"testing"
)

func TestWorkspaceCollectionsRejectMissingEnvelopes(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"autopilots":[],"total":1}`, `{"autopilots":null,"total":0}`} {
		c := &CLI{Runner: &fakeRunner{stdout: []byte(body)}}
		if _, err := c.ListAutopilots(); err == nil {
			t.Fatalf("incomplete autopilot list accepted: %s", body)
		}
	}
	c := &CLI{Runner: &fakeRunner{stdout: []byte(`null`)}}
	if _, err := c.ListProjects(); err == nil {
		t.Fatal("null project list accepted")
	}
	if _, err := c.ListProjectResources("id"); err == nil {
		t.Fatal("null project resource list accepted")
	}
}
func TestProjectDescriptionCannotSilentlyDisappear(t *testing.T) {
	c := &CLI{Runner: &fakeRunner{stdout: []byte(`{"id":"id","title":"Title","status":"planned"}`)}}
	if _, err := c.GetProject("id"); err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatal(err)
	}
}
func TestMissingTriggerEnabledIsNotInterpretedAsDisabled(t *testing.T) {
	raw := `{"autopilot":{"id":"id","title":"Title","description":null,"status":"active","assignee_type":"agent","assignee_id":"agent","execution_mode":"run_only","project_id":null,"issue_title_template":null,"subscribers":[]},"triggers":[{"id":"trigger","kind":"webhook","provider":"generic","has_signing_secret":false}],"collaborators":[]}`
	c := &CLI{Runner: &fakeRunner{stdout: []byte(raw)}}
	if _, err := c.GetAutopilot("id"); err == nil || !strings.Contains(err.Error(), "enabled") {
		t.Fatal(err)
	}
}

func TestNullEnabledIsNotDisabled(t *testing.T) {
	if err := requireJSONKeys([]byte(`{"enabled":null}`), "enabled"); err == nil {
		t.Fatal("null enabled accepted")
	}
	if err := requireJSONKeys([]byte(`{"enabled":false}`), "enabled"); err != nil {
		t.Fatal(err)
	}
}
