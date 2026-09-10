//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

const (
	projectID            = "00000000-0000-4000-8000-000000000010"
	autopilotID          = "00000000-0000-4000-8000-000000000011"
	memberID             = "00000000-0000-4000-8000-000000000012"
	resourceID           = "00000000-0000-4000-8000-000000000013"
	scheduleID           = "00000000-0000-4000-8000-000000000014"
	webhookID            = "00000000-0000-4000-8000-000000000015"
	projectDescription   = "# Project\n\nОписание проекта.  \n\n"
	autopilotDescription = "# Instructions\n\nПроверь регрессии.  \n\n"
)

// Called with the shared fixture lock held; only explicitly modelled routes
// succeed. New IDs differ from the source snapshot to exercise remapping.
func (f *fixture) serveWorkspace(w http.ResponseWriter, r *http.Request, data map[string]any) bool {
	respond := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	collection := func(v []map[string]any) []map[string]any {
		if v == nil {
			return []map[string]any{}
		}
		return v
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/projects":
		list := []any{}
		if f.project != nil {
			list = append(list, f.project)
		}
		respond(map[string]any{"projects": list})
		return true
	case "GET /api/projects/" + projectID:
		for _, key := range []string{"description", "icon", "lead_type", "lead_id", "start_date", "due_date"} {
			if _, ok := f.project[key]; !ok {
				f.project[key] = nil
			}
		}
		f.project["resource_count"] = len(f.resources)
		respond(f.project)
		return true
	case "GET /api/projects/" + projectID + "/resources":
		respond(map[string]any{"resources": collection(f.resources)})
		return true
	case "GET /api/autopilots":
		list := []any{}
		if f.autopilot != nil {
			list = append(list, f.autopilot)
		}
		respond(map[string]any{"autopilots": list, "total": len(list)})
		return true
	case "GET /api/autopilots/" + autopilotID:
		for _, key := range []string{"description", "project_id", "issue_title_template"} {
			if _, ok := f.autopilot[key]; !ok {
				f.autopilot[key] = nil
			}
		}
		if f.incompleteAutopilot {
			respond(map[string]any{"autopilot": f.autopilot})
			return true
		}
		respond(map[string]any{"autopilot": f.autopilot, "triggers": collection(f.triggers), "collaborators": collection(f.collaborators)})
		return true
	case "GET /api/workspaces/" + workspaceID + "/members":
		respond([]any{map[string]any{"user_id": memberID, "display_name": "Fixture member", "name": "Fixture member"}})
		return true
	case "POST /api/projects":
		f.project = data
		f.project["id"] = projectID
		f.project["priority"] = "none"
		if _, ok := f.project["status"]; !ok {
			f.project["status"] = "planned"
		}
		respond(f.project)
		return true
	case "PUT /api/projects/" + projectID:
		for k, v := range data {
			f.project[k] = v
		}
		respond(f.project)
		return true
	case "POST /api/projects/" + projectID + "/resources":
		f.resourceSequence++
		data["id"] = fmt.Sprintf("00000000-0000-4000-8000-%012d", 100+f.resourceSequence)
		data["position"] = len(f.resources)
		f.resources = append(f.resources, data)
		respond(data)
		return true
	case "POST /api/autopilots":
		f.autopilot = data
		f.autopilot["id"] = autopilotID
		f.autopilot["status"] = "active"
		f.autopilot["assignee_type"] = "agent"
		if _, ok := f.autopilot["subscribers"]; !ok {
			f.autopilot["subscribers"] = []any{}
		}
		respond(f.autopilot)
		return true
	case "PATCH /api/autopilots/" + autopilotID:
		for k, v := range data {
			f.autopilot[k] = v
		}
		respond(f.autopilot)
		return true
	case "POST /api/autopilots/" + autopilotID + "/triggers":
		if f.failTriggerWrite {
			http.Error(w, "synthetic trigger failure", 500)
			return true
		}
		if f.autopilot["status"] != "paused" {
			http.Error(w, "trigger installed before pausing autopilot", 400)
			return true
		}
		f.triggerSequence++
		data["id"] = fmt.Sprintf("00000000-0000-4000-8000-%012d", 200+f.triggerSequence)
		data["enabled"] = true
		if data["kind"] == "webhook" {
			data["provider"] = "generic"
			data["has_signing_secret"] = false
			data["webhook_token"] = "new-synthetic-webhook-secret"
		}
		f.triggers = append(f.triggers, data)
		respond(data)
		return true
	}
	prefix := "/api/projects/" + projectID + "/resources/"
	if strings.HasPrefix(r.URL.Path, prefix) {
		id := strings.TrimPrefix(r.URL.Path, prefix)
		for i, v := range f.resources {
			if v["id"] != id {
				continue
			}
			switch r.Method {
			case "PUT":
				for k, value := range data {
					v[k] = value
				}
				respond(v)
				return true
			case "DELETE":
				f.resources = slices.Delete(f.resources, i, i+1)
				w.WriteHeader(204)
				return true
			}
		}
	}
	prefix = "/api/autopilots/" + autopilotID + "/triggers/"
	if strings.HasPrefix(r.URL.Path, prefix) {
		id := strings.TrimPrefix(r.URL.Path, prefix)
		for i, v := range f.triggers {
			if v["id"] != id {
				continue
			}
			switch r.Method {
			case "PATCH":
				if f.failTriggerWrite {
					http.Error(w, "synthetic failure", 500)
					return true
				}
				for k, value := range data {
					v[k] = value
				}
				respond(v)
				return true
			case "DELETE":
				f.triggers = slices.Delete(f.triggers, i, i+1)
				w.WriteHeader(204)
				return true
			}
		}
	}
	return false
}
func (f *fixture) withWorkspaceResources() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.project = map[string]any{"id": projectID, "title": "Game prototype", "description": projectDescription, "status": "in_progress", "priority": "none", "icon": "🎮", "lead_type": "agent", "lead_id": agentID, "start_date": "2026-09-01", "due_date": "2026-10-01"}
	f.resources = []map[string]any{{"id": resourceID, "resource_type": "github_repo", "resource_ref": map[string]string{"url": "https://github.com/example/game", "ref": "main"}, "label": "Source", "position": 10}}
	f.autopilot = map[string]any{"id": autopilotID, "title": "Nightly review", "description": autopilotDescription, "assignee_type": "agent", "assignee_id": agentID, "project_id": projectID, "status": "active", "execution_mode": "create_issue", "issue_title_template": "Review {{date}}", "subscribers": []any{map[string]any{"user_type": "member", "user_id": memberID}}}
	f.triggers = []map[string]any{
		{"id": scheduleID, "kind": "schedule", "enabled": true, "cron_expression": "0 9 * * 1-5", "timezone": "Europe/Berlin", "label": "Weekdays"},
		{"id": webhookID, "kind": "webhook", "enabled": false, "label": "On demand", "provider": "generic", "webhook_token": "synthetic-secret-not-for-snapshot", "webhook_url": "https://invalid/secret", "has_signing_secret": false},
	}
}
func TestOfficialCLIProjectAutopilotRoundTrip(t *testing.T) {
	f, cli := newFixture(t)
	f.withWorkspaceResources()
	p, dir := exportProject(t, cli)
	if len(p.Projects) != 1 || len(p.Autopilots) != 1 {
		t.Fatal("missing resource collections")
	}
	if p.Projects[0].Lead.Agent != "Builder" || p.Autopilots[0].Project != "Game prototype" || p.Autopilots[0].Agent != "Builder" {
		t.Fatal("references did not use names")
	}
	for path, expected := range map[string]string{"projects/game-prototype/PROJECT.md": projectDescription, "autopilots/nightly-review/AUTOPILOT.md": autopilotDescription} {
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || string(b) != expected {
			t.Fatalf("body changed: %s, %v", path, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "autopilots/nightly-review/autopilot.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "synthetic-secret") || strings.Contains(string(b), "webhook_url") {
		t.Fatal("export leaked webhook credentials")
	}
	assertNoop(t, cli, p)
	assertNoop(t, cli, p)
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != 0 {
		t.Fatal("no-op reconciliation wrote data")
	}
}
func TestOfficialCLIProjectAutopilotCreateAndReapply(t *testing.T) {
	source, cli := newFixture(t)
	source.withWorkspaceResources()
	p, _ := exportProject(t, cli)
	dest, target := newFixture(t)
	if err := (reconcile.Reconciler{Backend: target}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	dest.mu.Lock()
	if dest.autopilot["project_id"] != projectID || dest.project["lead_id"] != agentID {
		t.Fatal("dependencies not resolved")
	}
	if len(dest.resources) != 1 || len(dest.triggers) != 2 {
		t.Fatal("children not restored")
	}
	if dest.resources[0]["id"] == resourceID {
		t.Fatal("test did not exercise foreign child IDs")
	}
	if dest.autopilot["status"] != "active" {
		t.Fatal("desired status not restored")
	}
	dest.mu.Unlock()
	before := len(dest.mutations())
	assertNoop(t, target, p)
	if err := (reconcile.Reconciler{Backend: target}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(dest.mutations()) != before {
		t.Fatal("second import duplicated or rewrote resources")
	}
}
func TestOfficialCLIWorkspaceEditsClearsAndChildrenConverge(t *testing.T) {
	f, cli := newFixture(t)
	f.withWorkspaceResources()
	p, _ := exportProject(t, cli)
	d := &p.Projects[0]
	d.Description = ""
	d.Icon = ""
	d.StartDate = ""
	d.DueDate = ""
	d.Status = "completed"
	d.Lead = &model.ProjectLead{Type: "member", ID: memberID}
	(*d.Resources)[0].Ref["ref"] = "release"
	(*d.Resources)[0].Position = 5
	(*d.Resources)[0].Label = ""
	ap := &p.Autopilots[0]
	ap.Description = ""
	ap.Project = ""
	ap.Mode = "run_only"
	ap.IssueTitleTemplate = ""
	empty := []string{}
	ap.Subscribers = &empty
	(*ap.Triggers)[0].Cron = "0 12 * * *"
	(*ap.Triggers)[0].Timezone = "UTC"
	(*ap.Triggers)[0].Enabled = false
	// Keep the schedule; explicitly remove the webhook in this managed list.
	*ap.Triggers = (*ap.Triggers)[:1]
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	before := len(f.mutations())
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != before {
		t.Fatal("edits not convergent")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.triggers) != 1 {
		t.Fatal("removed child retained")
	}
}
func TestOfficialCLIWorkspaceReadOnlyPreflight(t *testing.T) {
	for _, field := range []string{"priority", "lead-clear", "collaborators", "provider", "event-filters", "signing-secret"} {
		t.Run(field, func(t *testing.T) {
			f, cli := newFixture(t)
			f.withWorkspaceResources()
			p, _ := exportProject(t, cli)
			p.Agents[0].Description = "Must not be applied"
			switch field {
			case "priority":
				v := "high"
				p.Projects[0].Priority = &v
			case "lead-clear":
				p.Projects[0].Lead = &model.ProjectLead{Type: "none"}
			case "collaborators":
				v := []string{memberID}
				p.Autopilots[0].Collaborators = &v
			case "provider":
				v := "github"
				(*p.Autopilots[0].Triggers)[1].Provider = &v
			case "event-filters":
				v := []model.WebhookEventFilter{{Event: "push"}}
				(*p.Autopilots[0].Triggers)[1].EventFilters = &v
			case "signing-secret":
				v := true
				(*p.Autopilots[0].Triggers)[1].HasSigningSecret = &v
			}
			if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err == nil {
				t.Fatal("unsupported change accepted")
			}
			if len(f.mutations()) != 0 {
				t.Fatal("preflight failure wrote unrelated resources")
			}
		})
	}
}
func TestOfficialCLIIncompleteAutopilotPreservesForcedExport(t *testing.T) {
	f, cli := newFixture(t)
	f.withWorkspaceResources()
	p, dir := exportProject(t, cli)
	path := filepath.Join(dir, "autopilots/nightly-review/AUTOPILOT.md")
	f.mu.Lock()
	f.incompleteAutopilot = true
	f.mu.Unlock()
	if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, Force: true}); err == nil {
		t.Fatal("incomplete export accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != autopilotDescription {
		t.Fatal("failed forced export changed previous snapshot")
	}
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err == nil {
		t.Fatal("incomplete read accepted for apply")
	}
	if len(f.mutations()) != 0 {
		t.Fatal("incomplete read caused writes")
	}
}
func TestOfficialCLIAutopilotFailureRemainsPaused(t *testing.T) {
	f, cli := newFixture(t)
	f.withWorkspaceResources()
	p, _ := exportProject(t, cli)
	(*p.Autopilots[0].Triggers)[0].Cron = "0 12 * * *"
	f.mu.Lock()
	f.failTriggerWrite = true
	f.mu.Unlock()
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err == nil {
		t.Fatal("expected failure")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.autopilot["status"] != "paused" {
		t.Fatal("partially updated autopilot was reactivated")
	}
}
func TestOfficialCLIWorkspaceExportKeepsGrouping(t *testing.T) {
	f, cli := newFixture(t)
	f.withWorkspaceResources()
	_, dir := exportProject(t, cli)
	for _, collection := range []string{"projects", "autopilots"} {
		name := "game-prototype"
		if collection == "autopilots" {
			name = "nightly-review"
		}
		group := filepath.Join(dir, collection, "group")
		if err := os.MkdirAll(group, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, collection, name), filepath.Join(group, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, Force: true}); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(dir, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	if _, err := os.Stat(filepath.Join(dir, "autopilots/group/nightly-review/autopilot.yaml")); err != nil {
		t.Fatal("grouping lost")
	}
}

func TestOfficialCLIOmittedChildCollectionsRemainUnmanaged(t *testing.T) {
	f, cli := newFixture(t)
	f.withWorkspaceResources()
	p, _ := exportProject(t, cli)
	p.Projects[0].Resources = nil
	p.Projects[0].Lead = nil
	p.Projects[0].Priority = nil
	p.Autopilots[0].Triggers = nil
	p.Autopilots[0].Subscribers = nil
	p.Autopilots[0].Collaborators = nil
	assertNoop(t, cli, p)
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != 0 {
		t.Fatal("omission changed child collections")
	}
	emptyResources := []model.ProjectResourceSpec{}
	p.Projects[0].Resources = &emptyResources
	emptyTriggers := []model.AutopilotTriggerSpec{}
	p.Autopilots[0].Triggers = &emptyTriggers
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.resources) != 0 || len(f.triggers) != 0 {
		t.Fatal("explicit empty collections did not remove children")
	}
}
func TestOfficialCLIProjectOnlyExport(t *testing.T) {
	f, cli := newFixture(t)
	f.withWorkspaceResources()
	f.mu.Lock()
	f.agent = nil
	f.skill = nil
	f.autopilot = nil
	f.triggers = nil
	f.project["lead_type"] = nil
	f.project["lead_id"] = nil
	f.mu.Unlock()
	p, _ := exportProject(t, cli)
	if len(p.Agents) != 0 || len(p.Skills) != 0 || len(p.Projects) != 1 || len(p.Autopilots) != 0 {
		t.Fatal("project-only export failed")
	}
	assertNoop(t, cli, p)
}
