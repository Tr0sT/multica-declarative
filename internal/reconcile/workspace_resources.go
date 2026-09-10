package reconcile

import (
	"fmt"
	"slices"
	"sort"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

type observedProject struct {
	resource model.WorkspaceProject
	children []model.ProjectResource
	matches  []int
	change   model.Change
}
type observedAutopilot struct {
	detail  model.AutopilotDetail
	matches []int
	change  model.Change
}
type workspaceState struct {
	projects      map[string]observedProject
	autopilots    map[string]observedAutopilot
	projectsOps   backend.ProjectOperations
	autopilotsOps backend.AutopilotOperations
}

// Child IDs are hints for in-place edits, not identities to recreate elsewhere.
// Exact contents permit a repeated import with source IDs into another workspace.
// A unique non-empty label can match a changed child after that first import.
func matchChildren[D, A any](desired []D, actual []A, dID func(D) string, aID func(A) string, exact func(D, A) bool, label func(D, A) bool, sameKind func(D, A) bool) ([]int, error) {
	matches := make([]int, len(desired))
	for i := range matches {
		matches[i] = -1
	}
	used := map[int]bool{}
	seen := map[string]bool{}
	for _, a := range actual {
		id := aID(a)
		if id == "" || seen[id] {
			return nil, fmt.Errorf("child collection contains missing or duplicate IDs")
		}
		seen[id] = true
	}
	for i, d := range desired {
		if dID(d) == "" {
			continue
		}
		for j, a := range actual {
			if dID(d) == aID(a) {
				if used[j] {
					return nil, fmt.Errorf("duplicate desired child ID")
				}
				if !sameKind(d, a) {
					return nil, fmt.Errorf("child kind/type cannot change in place; remove its id to explicitly replace it")
				}
				matches[i] = j
				used[j] = true
				break
			}
		}
	}
	for i, d := range desired {
		if matches[i] >= 0 {
			continue
		}
		for j, a := range actual {
			if !used[j] && exact(d, a) {
				matches[i] = j
				used[j] = true
				break
			}
		}
	}
	for i, d := range desired {
		if matches[i] >= 0 {
			continue
		}
		found := -1
		for j, a := range actual {
			if !used[j] && sameKind(d, a) && label(d, a) {
				if found >= 0 {
					return nil, fmt.Errorf("ambiguous child label; retain its exported id or use unique labels")
				}
				found = j
			}
		}
		if found >= 0 {
			matches[i] = found
			used[found] = true
		}
	}
	return matches, nil
}
func matchResources(d []model.ProjectResourceSpec, a []model.ProjectResource) ([]int, error) {
	return matchChildren(d, a, func(v model.ProjectResourceSpec) string { return v.ID }, func(v model.ProjectResource) string { return v.ID },
		func(d model.ProjectResourceSpec, a model.ProjectResource) bool {
			return d.Type == a.Type && equalJSON(d.Ref, a.Ref)
		},
		func(d model.ProjectResourceSpec, a model.ProjectResource) bool {
			return d.Label != "" && d.Label == a.Label
		},
		func(d model.ProjectResourceSpec, a model.ProjectResource) bool { return d.Type == a.Type })
}
func matchTriggers(d []model.AutopilotTriggerSpec, a []model.AutopilotTrigger) ([]int, error) {
	return matchChildren(d, a, func(v model.AutopilotTriggerSpec) string { return v.ID }, func(v model.AutopilotTrigger) string { return v.ID },
		func(d model.AutopilotTriggerSpec, a model.AutopilotTrigger) bool {
			return d.Kind == a.Kind && d.Cron == a.Cron && d.Timezone == a.Timezone && d.Label == a.Label
		},
		func(d model.AutopilotTriggerSpec, a model.AutopilotTrigger) bool {
			return d.Label != "" && d.Label == a.Label
		},
		func(d model.AutopilotTriggerSpec, a model.AutopilotTrigger) bool { return d.Kind == a.Kind })
}
func membersEqual(desired []string, actual []model.AutopilotMember) bool {
	got := []string{}
	for _, a := range actual {
		if a.Type != "member" {
			return false
		}
		got = append(got, a.ID)
	}
	return equalStrings(sortedStrings(desired), sortedStrings(got))
}
func triggerWritableDiff(d model.AutopilotTriggerSpec, a model.AutopilotTrigger) bool {
	return d.Enabled != a.Enabled || d.Label != a.Label || d.Cron != a.Cron || d.Timezone != a.Timezone
}
func triggerObservedOnly(d model.AutopilotTriggerSpec, a model.AutopilotTrigger, creating bool) error {
	provider := a.Provider
	if provider == "" && d.Kind == "webhook" {
		provider = "generic"
	}
	if creating && d.Kind == "api" {
		return fmt.Errorf("the official CLI cannot create api triggers")
	}
	if d.Provider != nil && *d.Provider != provider {
		return fmt.Errorf("webhook provider cannot be changed through the official CLI")
	}
	if d.HasSigningSecret != nil && *d.HasSigningSecret != a.HasSigningSecret {
		return fmt.Errorf("webhook signing secrets are write-only and cannot be restored through the official CLI")
	}
	if d.EventFilters != nil && !equalJSON(normalizeFilters(*d.EventFilters), normalizeFilters(a.EventFilters)) {
		return fmt.Errorf("webhook event filters cannot be changed through the official CLI")
	}
	return nil
}
func normalizeFilters(v []model.WebhookEventFilter) []model.WebhookEventFilter {
	out := append([]model.WebhookEventFilter{}, v...)
	for i := range out {
		out[i].Actions = sortedStrings(out[i].Actions)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Event < out[j].Event })
	return out
}
func resourceChanged(d model.ProjectResourceSpec, a model.ProjectResource) bool {
	return d.Type != a.Type || !equalJSON(d.Ref, a.Ref) || d.Label != a.Label || d.Position != a.Position
}

func (r Reconciler) inspectWorkspace(p model.Project, s *inspection) error {
	x := &workspaceState{projects: map[string]observedProject{}, autopilots: map[string]observedAutopilot{}}
	s.workspace = x
	agentIDs, squadIDs, projectIDs := map[string]string{}, map[string]string{}, map[string]string{}
	for name, a := range s.agents {
		agentIDs[name] = a.resource.ID
	}
	for name, a := range s.squads {
		squadIDs[name] = a.resource.ID
	}
	if len(p.Projects) > 0 {
		ops, ok := r.Backend.(backend.ProjectOperations)
		if !ok {
			return fmt.Errorf("backend does not support projects")
		}
		x.projectsOps = ops
		remote, err := ops.ListProjects()
		if err != nil {
			return err
		}
		for _, d := range p.Projects {
			o := observedProject{change: model.Change{Action: Create, Kind: "project", Name: d.Name}}
			for _, a := range remote {
				if a.Title == d.Name {
					if o.resource.ID != "" {
						return fmt.Errorf("multiple projects named %q", d.Name)
					}
					if a.ID == "" {
						return fmt.Errorf("project %q has no id", d.Name)
					}
					o.resource = a
				}
			}
			creating := o.resource.ID == ""
			if !creating {
				o.resource, err = ops.GetProject(o.resource.ID)
				if err != nil {
					return err
				}
				a := o.resource
				projectIDs[d.Name] = a.ID
				f := []string{}
				if d.Description != a.Description {
					f = append(f, "description")
				}
				if d.Status != a.Status {
					f = append(f, "status")
				}
				if d.Icon != a.Icon {
					f = append(f, "icon")
				}
				if d.StartDate != a.StartDate {
					f = append(f, "startDate")
				}
				if d.DueDate != a.DueDate {
					f = append(f, "dueDate")
				}
				if d.Priority != nil && *d.Priority != a.Priority {
					return fmt.Errorf("project %q priority cannot be changed through the official CLI", d.Name)
				}
				if d.Lead != nil {
					typ, id := projectLead(d.Lead, agentIDs)
					if typ != a.LeadType || id != a.LeadID || (typ == "agent" && id == "") {
						if typ == "" {
							return fmt.Errorf("project %q lead cannot be cleared through the official CLI", d.Name)
						}
						f = append(f, "lead")
					}
				}
				o.change = makeChange("project", d.Name, f)
			} else if d.Priority != nil && *d.Priority != "none" {
				return fmt.Errorf("project %q priority cannot be set on creation through the official CLI", d.Name)
			}
			if d.Resources != nil {
				if !creating {
					o.children, err = ops.ListProjectResources(o.resource.ID)
					if err != nil {
						return err
					}
					if len(o.children) != o.resource.ResourceCount {
						return fmt.Errorf("project %q resource list is incomplete", d.Name)
					}
				}
				o.matches, err = matchResources(*d.Resources, o.children)
				if err != nil {
					return fmt.Errorf("project %q: %w", d.Name, err)
				}
				changed := len(*d.Resources) != len(o.children)
				for i, rd := range *d.Resources {
					j := o.matches[i]
					if j < 0 || resourceChanged(rd, o.children[j]) {
						changed = true
					}
				}
				if !creating && changed {
					o.change.Fields = append(o.change.Fields, "resources")
					o.change.Action = Update
				}
			}
			x.projects[d.Name] = o
			s.changes = append(s.changes, o.change)
		}
	}
	if len(p.Autopilots) > 0 {
		ops, ok := r.Backend.(backend.AutopilotOperations)
		if !ok {
			return fmt.Errorf("backend does not support autopilots")
		}
		x.autopilotsOps = ops
		remote, err := ops.ListAutopilots()
		if err != nil {
			return err
		}
		for _, d := range p.Autopilots {
			o := observedAutopilot{change: model.Change{Action: Create, Kind: "autopilot", Name: d.Name}}
			for _, a := range remote {
				if a.Title == d.Name {
					if o.detail.Autopilot.ID != "" {
						return fmt.Errorf("multiple autopilots named %q", d.Name)
					}
					if a.ID == "" {
						return fmt.Errorf("autopilot %q has no id", d.Name)
					}
					o.detail.Autopilot = a
				}
			}
			creating := o.detail.Autopilot.ID == ""
			if !creating {
				o.detail, err = ops.GetAutopilot(o.detail.Autopilot.ID)
				if err != nil {
					return err
				}
			}
			a := o.detail.Autopilot
			f := []string{}
			if d.Collaborators != nil && !membersEqual(*d.Collaborators, o.detail.Collaborators) {
				return fmt.Errorf("autopilot %q collaborators cannot be changed through the official CLI", d.Name)
			}
			if d.Squad != "" {
				if creating || a.AssigneeType != "squad" || squadIDs[d.Squad] == "" || squadIDs[d.Squad] != a.AssigneeID {
					return fmt.Errorf("autopilot %q squad assignment cannot be created or changed through the official CLI", d.Name)
				}
			} else if creating || a.AssigneeType != "agent" || agentIDs[d.Agent] == "" || a.AssigneeID != agentIDs[d.Agent] {
				f = append(f, "agent")
			}
			if d.Description != a.Description {
				f = append(f, "description")
			}
			if d.Status != a.Status {
				f = append(f, "status")
			}
			if d.Mode != a.Mode {
				f = append(f, "mode")
			}
			if d.IssueTitleTemplate != a.IssueTitleTemplate {
				f = append(f, "issueTitleTemplate")
			}
			if projectIDs[d.Project] != a.ProjectID || (d.Project != "" && projectIDs[d.Project] == "") {
				f = append(f, "project")
			}
			if d.Subscribers != nil && !membersEqual(*d.Subscribers, a.Subscribers) {
				f = append(f, "subscribers")
			}
			if d.Triggers != nil {
				o.matches, err = matchTriggers(*d.Triggers, o.detail.Triggers)
				if err != nil {
					return fmt.Errorf("autopilot %q: %w", d.Name, err)
				}
				changed := len(*d.Triggers) != len(o.detail.Triggers)
				for i, td := range *d.Triggers {
					j := o.matches[i]
					var actual model.AutopilotTrigger
					if j >= 0 {
						actual = o.detail.Triggers[j]
					}
					if err := triggerObservedOnly(td, actual, j < 0); err != nil {
						return fmt.Errorf("autopilot %q: %w", d.Name, err)
					}
					if j < 0 || triggerWritableDiff(td, actual) {
						changed = true
					}
				}
				if changed {
					f = append(f, "triggers")
				}
			}
			if !creating {
				o.change = makeChange("autopilot", d.Name, f)
			}
			x.autopilots[d.Name] = o
			s.changes = append(s.changes, o.change)
		}
	}
	return nil
}
func projectLead(l *model.ProjectLead, ids map[string]string) (string, string) {
	if l == nil || l.Type == "none" {
		return "", ""
	}
	if l.Type == "agent" {
		return "agent", ids[l.Agent]
	}
	return "member", l.ID
}
func (r Reconciler) applyWorkspace(p model.Project, s inspection, agentIDs map[string]string, report func(model.Change)) error {
	x := s.workspace
	projectIDs := map[string]string{}
	for _, d := range p.Projects {
		o := x.projects[d.Name]
		id := o.resource.ID
		_, lead := projectLead(d.Lead, agentIDs)
		in := model.ProjectInput{Title: d.Name, Description: d.Description, Status: d.Status, Icon: d.Icon, LeadID: lead, StartDate: d.StartDate, DueDate: d.DueDate}
		if id == "" {
			a, err := x.projectsOps.CreateProject(in)
			if err != nil {
				return err
			}
			id = a.ID
		} else {
			fields := slices.DeleteFunc(slices.Clone(o.change.Fields), func(f string) bool { return f == "resources" })
			if len(fields) > 0 {
				if _, err := x.projectsOps.UpdateProject(id, in, fields); err != nil {
					return err
				}
			}
		}
		if id == "" {
			return fmt.Errorf("project %q has no id after reconciliation", d.Name)
		}
		if d.Resources != nil {
			kept := map[int]bool{}
			for i, rd := range *d.Resources {
				j := o.matches[i]
				if j < 0 {
					a, err := x.projectsOps.AddProjectResource(id, rd)
					if err != nil {
						return err
					}
					if a.ID == "" {
						return fmt.Errorf("created project resource has no id")
					}
					if resourceChanged(rd, a) {
						if err := x.projectsOps.UpdateProjectResource(id, a.ID, rd); err != nil {
							return err
						}
					}
				} else {
					kept[j] = true
					if resourceChanged(rd, o.children[j]) {
						if err := x.projectsOps.UpdateProjectResource(id, o.children[j].ID, rd); err != nil {
							return err
						}
					}
				}
			}
			for j, a := range o.children {
				if !kept[j] {
					if err := x.projectsOps.RemoveProjectResource(id, a.ID); err != nil {
						return err
					}
				}
			}
		}
		projectIDs[d.Name] = id
		report(o.change)
	}
	for _, d := range p.Autopilots {
		o := x.autopilots[d.Name]
		if o.change.Action == Noop {
			report(o.change)
			continue
		}
		a := o.detail.Autopilot
		id := a.ID
		in := model.AutopilotInput{Title: d.Name, Description: d.Description, AgentID: agentIDs[d.Agent], ProjectID: projectIDs[d.Project], Status: d.Status, Mode: d.Mode, IssueTitleTemplate: d.IssueTitleTemplate, Subscribers: d.Subscribers}
		creating := id == ""
		if creating {
			v, err := x.autopilotsOps.CreateAutopilot(in)
			if err != nil {
				return err
			}
			id = v.ID
			if id == "" {
				return fmt.Errorf("created autopilot has no id")
			}
			a.Status = "active"
		}
		// Create has no status flag. There are no triggers yet, so pausing before
		// installing them is safe. A failed update intentionally stays paused.
		if a.Status != "paused" {
			if err := x.autopilotsOps.UpdateAutopilot(id, model.AutopilotInput{Status: "paused"}, []string{"status"}); err != nil {
				return err
			}
		}
		if !creating {
			fields := slices.DeleteFunc(slices.Clone(o.change.Fields), func(f string) bool { return f == "status" || f == "triggers" })
			if len(fields) > 0 {
				if err := x.autopilotsOps.UpdateAutopilot(id, in, fields); err != nil {
					return err
				}
			}
		}
		if d.Triggers != nil {
			kept := map[int]bool{}
			for i, td := range *d.Triggers {
				j := o.matches[i]
				if j < 0 {
					t, err := x.autopilotsOps.AddAutopilotTrigger(id, td)
					if err != nil {
						return err
					}
					if t.ID == "" {
						return fmt.Errorf("created trigger has no id")
					}
					if triggerWritableDiff(td, t) {
						if err := x.autopilotsOps.UpdateAutopilotTrigger(id, t.ID, td); err != nil {
							return err
						}
					}
				} else {
					kept[j] = true
					if triggerWritableDiff(td, o.detail.Triggers[j]) {
						if err := x.autopilotsOps.UpdateAutopilotTrigger(id, o.detail.Triggers[j].ID, td); err != nil {
							return err
						}
					}
				}
			}
			for j, t := range o.detail.Triggers {
				if !kept[j] {
					if err := x.autopilotsOps.DeleteAutopilotTrigger(id, t.ID); err != nil {
						return err
					}
				}
			}
		}
		if d.Status == "active" {
			if err := x.autopilotsOps.UpdateAutopilot(id, model.AutopilotInput{Status: "active"}, []string{"status"}); err != nil {
				return err
			}
		}
		report(o.change)
	}
	return nil
}
