package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

type exportedProject struct {
	directory string
	document  model.WorkspaceProjectSpec
}
type exportedAutopilot struct {
	directory string
	document  model.AutopilotSpec
}

func memberIDs(members []model.AutopilotMember) ([]string, error) {
	ids := []string{}
	for _, m := range members {
		if m.Type != "member" || m.ID == "" {
			return nil, fmt.Errorf("unsupported or incomplete autopilot member")
		}
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids, nil
}
func (e Exporter) readWorkspaceResources(s *snapshot, agents map[string]string, squads map[string]string) error {
	projects := map[string]string{}
	if ops, ok := e.Backend.(backend.ProjectOperations); ok {
		items, err := ops.ListProjects()
		if err != nil {
			return err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Title < items[j].Title })
		names, used := map[string]bool{}, map[string]struct{}{}
		for _, item := range items {
			v, err := ops.GetProject(item.ID)
			if err != nil {
				return err
			}
			if v.ID == "" || v.Title == "" || names[v.Title] {
				return fmt.Errorf("incomplete or duplicate project identity")
			}
			names[v.Title] = true
			resources, err := ops.ListProjectResources(v.ID)
			if err != nil {
				return err
			}
			if len(resources) != v.ResourceCount {
				return fmt.Errorf("project %q resource count disagrees with its resource list", v.Title)
			}
			sort.Slice(resources, func(i, j int) bool {
				if resources[i].Position == resources[j].Position {
					return resources[i].ID < resources[j].ID
				}
				return resources[i].Position < resources[j].Position
			})
			refs := []model.ProjectResourceSpec{}
			for _, r := range resources {
				refs = append(refs, model.ProjectResourceSpec{ID: r.ID, Type: r.Type, Ref: r.Ref, Label: r.Label, Position: r.Position})
			}
			lead := &model.ProjectLead{Type: "none"}
			switch v.LeadType {
			case "":
				if v.LeadID != "" {
					return fmt.Errorf("project %q has incomplete lead metadata", v.Title)
				}
			case "agent":
				lead.Type = "agent"
				lead.Agent = agents[v.LeadID]
				if lead.Agent == "" {
					return fmt.Errorf("project %q lead is not an exported agent", v.Title)
				}
			case "member":
				lead.Type = "member"
				lead.ID = v.LeadID
			default:
				return fmt.Errorf("project %q has unsupported lead type", v.Title)
			}
			priority := v.Priority
			d := model.WorkspaceProjectSpec{Name: v.Title, Description: v.Description, DescriptionFile: "PROJECT.md", Status: v.Status, Icon: v.Icon, Priority: &priority, Lead: lead, StartDate: v.StartDate, DueDate: v.DueDate, Resources: &refs}
			s.projects = append(s.projects, exportedProject{uniqueSlug(v.Title, v.ID, used), d})
			projects[v.ID] = v.Title
		}
	}
	if ops, ok := e.Backend.(backend.AutopilotOperations); ok {
		items, err := ops.ListAutopilots()
		if err != nil {
			return err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Title < items[j].Title })
		names, used := map[string]bool{}, map[string]struct{}{}
		for _, item := range items {
			detail, err := ops.GetAutopilot(item.ID)
			if err != nil {
				return err
			}
			v := detail.Autopilot
			if v.ID == "" || v.Title == "" || names[v.Title] {
				return fmt.Errorf("incomplete or duplicate autopilot identity")
			}
			names[v.Title] = true
			d := model.AutopilotSpec{Name: v.Title, Description: v.Description, DescriptionFile: "AUTOPILOT.md", Mode: v.Mode, Status: v.Status, IssueTitleTemplate: v.IssueTitleTemplate}
			switch v.AssigneeType {
			case "agent":
				d.Agent = agents[v.AssigneeID]
				if d.Agent == "" {
					return fmt.Errorf("autopilot %q assignee is not an exported agent", v.Title)
				}
			case "squad":
				d.Squad = squads[v.AssigneeID]
				if d.Squad == "" {
					return fmt.Errorf("autopilot %q assignee is not an exported squad", v.Title)
				}
			default:
				return fmt.Errorf("autopilot %q has unsupported assignee type", v.Title)
			}
			if v.ProjectID != "" {
				d.Project = projects[v.ProjectID]
				if d.Project == "" {
					return fmt.Errorf("autopilot %q references a project not in the snapshot", v.Title)
				}
			}
			subscribers, err := memberIDs(v.Subscribers)
			if err != nil {
				return err
			}
			d.Subscribers = &subscribers
			collaborators, err := memberIDs(detail.Collaborators)
			if err != nil {
				return err
			}
			d.Collaborators = &collaborators
			sort.Slice(detail.Triggers, func(i, j int) bool { return detail.Triggers[i].ID < detail.Triggers[j].ID })
			triggers := []model.AutopilotTriggerSpec{}
			for _, t := range detail.Triggers {
				st := model.AutopilotTriggerSpec{ID: t.ID, Kind: t.Kind, Enabled: t.Enabled, Cron: t.Cron, Timezone: t.Timezone, Label: t.Label}
				if t.Kind == "webhook" {
					provider := t.Provider
					if provider == "" {
						provider = "generic"
					}
					signing := t.HasSigningSecret
					filters := append([]model.WebhookEventFilter{}, t.EventFilters...)
					st.Provider = &provider
					st.HasSigningSecret = &signing
					st.EventFilters = &filters
					s.warnings = append(s.warnings, fmt.Sprintf("autopilot %q webhook credentials are not exportable through the CLI: existing URLs are retained in-place; recreating a trigger generates a new URL; signing secrets are write-only", v.Title))
				}
				triggers = append(triggers, st)
			}
			d.Triggers = &triggers
			s.autopilots = append(s.autopilots, exportedAutopilot{uniqueSlug(v.Title, v.ID, used), d})
		}
	}
	return nil
}
func preserveWorkspaceDirectories(target string, s *snapshot) error {
	if err := preserveResourceDirectories(target, "projects", "project.yaml", s.projects, func(v *exportedProject) (string, *string) { return v.document.Name, &v.directory }, yamlName); err != nil {
		return err
	}
	return preserveResourceDirectories(target, "autopilots", "autopilot.yaml", s.autopilots, func(v *exportedAutopilot) (string, *string) { return v.document.Name, &v.directory }, yamlName)
}
func writeWorkspaceResources(root string, s snapshot) error {
	for _, v := range s.projects {
		dir := filepath.Join(root, "projects", v.directory)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		d := v.document
		if err := os.WriteFile(filepath.Join(dir, "PROJECT.md"), []byte(d.Description), 0644); err != nil {
			return err
		}
		d.Description = ""
		if err := writeYAML(filepath.Join(dir, "project.yaml"), d); err != nil {
			return err
		}
	}
	for _, v := range s.autopilots {
		dir := filepath.Join(root, "autopilots", v.directory)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		d := v.document
		if err := os.WriteFile(filepath.Join(dir, "AUTOPILOT.md"), []byte(d.Description), 0644); err != nil {
			return err
		}
		d.Description = ""
		if err := writeYAML(filepath.Join(dir, "autopilot.yaml"), d); err != nil {
			return err
		}
	}
	return nil
}
