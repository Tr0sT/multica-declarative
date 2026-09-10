package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func loadWorkspaceResources(base string, p *model.Project) error {
	dirs, err := discoverResourceDirectories(base, "projects", "project.yaml")
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "project.yaml")
		var d model.WorkspaceProjectSpec
		if err := decodeStrictYAML(path, &d); err != nil {
			return err
		}
		d.Name = strings.TrimSpace(d.Name)
		if d.Status == "" {
			d.Status = "planned"
		}
		d.Description, err = loadTextChoice(path, d.Description, d.DescriptionFile, "description")
		if err != nil {
			return err
		}
		if err = validateWorkspaceProject(d); err != nil {
			return fmt.Errorf("project %q: %w", d.Name, err)
		}
		p.Projects = append(p.Projects, d)
	}
	dirs, err = discoverResourceDirectories(base, "autopilots", "autopilot.yaml")
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "autopilot.yaml")
		var d model.AutopilotSpec
		if err := decodeStrictYAML(path, &d); err != nil {
			return err
		}
		d.Name = strings.TrimSpace(d.Name)
		if d.Status == "" {
			d.Status = "paused"
		}
		d.Description, err = loadTextChoice(path, d.Description, d.DescriptionFile, "description")
		if err != nil {
			return err
		}
		if d.Triggers != nil {
			for i := range *d.Triggers {
				t := &(*d.Triggers)[i]
				if t.Kind == "schedule" && t.Timezone == "" {
					t.Timezone = "UTC"
				}
			}
		}
		if err = validateAutopilot(d); err != nil {
			return fmt.Errorf("autopilot %q: %w", d.Name, err)
		}
		p.Autopilots = append(p.Autopilots, d)
	}
	return nil
}
func validateWorkspaceProject(d model.WorkspaceProjectSpec) error {
	if d.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !slices.Contains([]string{"planned", "in_progress", "paused", "completed", "cancelled"}, d.Status) {
		return fmt.Errorf("invalid status")
	}
	if d.Priority != nil && !slices.Contains([]string{"none", "low", "medium", "high", "urgent"}, *d.Priority) {
		return fmt.Errorf("invalid priority")
	}
	for _, date := range []string{d.StartDate, d.DueDate} {
		if date != "" {
			parsed, err := time.Parse("2006-01-02", date)
			if err != nil || parsed.Format("2006-01-02") != date {
				return fmt.Errorf("dates must be YYYY-MM-DD")
			}
		}
	}
	if d.StartDate != "" && d.DueDate != "" && d.StartDate > d.DueDate {
		return fmt.Errorf("startDate must not be after dueDate")
	}
	if d.Lead != nil {
		l := d.Lead
		switch l.Type {
		case "none":
			if l.Agent != "" || l.ID != "" {
				return fmt.Errorf("lead none cannot contain agent or id")
			}
		case "agent":
			if l.Agent == "" || l.ID != "" {
				return fmt.Errorf("agent lead requires only an agent name")
			}
		case "member":
			if !uuidPattern.MatchString(l.ID) || l.Agent != "" {
				return fmt.Errorf("member lead requires only a member UUID")
			}
		default:
			return fmt.Errorf("lead.type must be agent, member, or none")
		}
	}
	if d.Resources != nil {
		ids := map[string]bool{}
		identities := map[string]bool{}
		for _, r := range *d.Resources {
			ref, _ := json.Marshal(r.Ref)
			key := r.Type + ":" + string(ref)
			if identities[key] {
				return fmt.Errorf("duplicate project resource reference")
			}
			identities[key] = true
			if r.ID != "" {
				if !uuidPattern.MatchString(r.ID) || ids[r.ID] {
					return fmt.Errorf("resource ids must be unique UUIDs")
				}
				ids[r.ID] = true
			}
			if r.Position < 0 {
				return fmt.Errorf("resource position must be nonnegative")
			}
			if err := validateProjectResource(r); err != nil {
				return err
			}
		}
	}
	return nil
}
func validateProjectResource(r model.ProjectResourceSpec) error {
	var allowed []string
	switch r.Type {
	case "github_repo":
		allowed = []string{"url", "ref", "default_branch_hint"}
		raw := r.Ref["url"]
		if raw == "" {
			return fmt.Errorf("github_repo requires ref.url")
		}
		if strings.Contains(raw, "://") {
			u, err := url.Parse(raw)
			if err != nil || u.Host == "" || !slices.Contains([]string{"https", "http", "ssh"}, u.Scheme) {
				return fmt.Errorf("invalid repository URL")
			}
			// Credentials cannot be safely passed via the official CLI --ref argument.
			if u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("repository URLs cannot contain query strings or fragments")
			}
			if u.User != nil && (u.Scheme != "ssh" || strings.Contains(u.User.String(), ":")) {
				return fmt.Errorf("use credential-free repository URLs and runtime authentication")
			}
		} else if !strings.Contains(raw, "@") || !strings.Contains(raw, ":") {
			return fmt.Errorf("invalid SSH repository URL")
		}
	case "local_directory":
		allowed = []string{"local_path", "daemon_id", "label", "execution_mode"}
		path := r.Ref["local_path"]
		if path == "" || (!strings.HasPrefix(path, "/") && !(len(path) > 2 && path[1] == ':') && !strings.HasPrefix(path, `\\`)) {
			return fmt.Errorf("local_directory requires an absolute local_path")
		}
		if !uuidPattern.MatchString(r.Ref["daemon_id"]) {
			return fmt.Errorf("local_directory requires a daemon UUID")
		}
		if m := r.Ref["execution_mode"]; m != "" && m != "in_place" && m != "worktree" {
			return fmt.Errorf("invalid local_directory execution_mode")
		}
	default:
		return fmt.Errorf("resource type must be github_repo or local_directory")
	}
	for k, v := range r.Ref {
		if !slices.Contains(allowed, k) {
			return fmt.Errorf("unsupported project resource ref field %q", k)
		}
		if strings.TrimSpace(v) != v || v == "" {
			return fmt.Errorf("resource ref values must be non-empty and trimmed")
		}
	}
	return nil
}
func validateAutopilot(d model.AutopilotSpec) error {
	if d.Name == "" {
		return fmt.Errorf("name is required")
	}
	if (d.Agent == "") == (d.Squad == "") {
		return fmt.Errorf("specify exactly one of agent or squad")
	}
	if d.Mode != "create_issue" && d.Mode != "run_only" {
		return fmt.Errorf("mode must be create_issue or run_only")
	}
	if d.Status != "active" && d.Status != "paused" {
		return fmt.Errorf("status must be active or paused")
	}
	if strings.Contains(strings.ReplaceAll(d.IssueTitleTemplate, "{{date}}", ""), "{{") {
		return fmt.Errorf("only {{date}} is supported in issueTitleTemplate")
	}
	for _, list := range []*[]string{d.Subscribers, d.Collaborators} {
		if list != nil {
			seen := map[string]bool{}
			for _, id := range *list {
				if !uuidPattern.MatchString(id) || seen[id] {
					return fmt.Errorf("member lists must contain unique UUIDs")
				}
				seen[id] = true
			}
		}
	}
	if d.Triggers != nil {
		ids := map[string]bool{}
		for _, t := range *d.Triggers {
			if t.ID != "" {
				if !uuidPattern.MatchString(t.ID) || ids[t.ID] {
					return fmt.Errorf("trigger ids must be unique UUIDs")
				}
				ids[t.ID] = true
			}
			if !slices.Contains([]string{"schedule", "webhook", "api"}, t.Kind) {
				return fmt.Errorf("invalid trigger kind")
			}
			if t.Kind == "schedule" {
				if strings.TrimSpace(t.Cron) == "" {
					return fmt.Errorf("schedule trigger requires cron")
				}
				if _, err := time.LoadLocation(t.Timezone); err != nil {
					return fmt.Errorf("invalid schedule timezone")
				}
			} else if t.Cron != "" || t.Timezone != "" {
				return fmt.Errorf("cron/timezone only apply to schedule triggers")
			}
			if t.Kind != "webhook" && (t.Provider != nil || t.EventFilters != nil || t.HasSigningSecret != nil) {
				return fmt.Errorf("webhook properties require kind webhook")
			}
			if t.Provider != nil && *t.Provider != "generic" && *t.Provider != "github" {
				return fmt.Errorf("invalid webhook provider")
			}
		}
	}
	return nil
}
func validateWorkspaceReferences(p model.Project) error {
	agents, squads, projects, autopilots := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, a := range p.Agents {
		agents[a.Name] = true
	}
	for _, s := range p.Squads {
		squads[s.Name] = true
	}
	for _, d := range p.Projects {
		if projects[d.Name] {
			return fmt.Errorf("duplicate project name %q", d.Name)
		}
		projects[d.Name] = true
		if d.Lead != nil && d.Lead.Type == "agent" && !agents[d.Lead.Agent] {
			return fmt.Errorf("project %q references undeclared lead agent %q", d.Name, d.Lead.Agent)
		}
	}
	for _, d := range p.Autopilots {
		if autopilots[d.Name] {
			return fmt.Errorf("duplicate autopilot name %q", d.Name)
		}
		autopilots[d.Name] = true
		if d.Agent != "" && !agents[d.Agent] {
			return fmt.Errorf("autopilot %q references undeclared agent %q", d.Name, d.Agent)
		}
		if d.Squad != "" && !squads[d.Squad] {
			return fmt.Errorf("autopilot %q references undeclared squad %q", d.Name, d.Squad)
		}
		if d.Project != "" && !projects[d.Project] {
			return fmt.Errorf("autopilot %q references undeclared project %q", d.Name, d.Project)
		}
	}
	return nil
}
