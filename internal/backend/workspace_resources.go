package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

func (c *CLI) ListProjects() ([]model.WorkspaceProject, error) {
	var v []model.WorkspaceProject
	err := c.runJSON(&v, "project", "list", "--output", "json")
	if err == nil && v == nil {
		err = fmt.Errorf("project list returned an incomplete collection")
	}
	return v, err
}
func (c *CLI) GetProject(id string) (model.WorkspaceProject, error) {
	var v model.WorkspaceProject
	var raw json.RawMessage
	err := c.runJSON(&raw, "project", "get", id, "--output", "json")
	if err == nil {
		err = requireJSONKeys(raw, "id", "title", "description", "status", "priority", "icon", "lead_type", "lead_id", "start_date", "due_date", "resource_count")
	}
	if err == nil {
		err = json.Unmarshal(raw, &v)
	}
	if err == nil && (v.ID != id || v.Title == "" || v.Status == "") {
		err = fmt.Errorf("project get returned incomplete or mismatched metadata")
	}
	return v, err
}
func projectArgs(args []string, in model.ProjectInput, fields []string) []string {
	for _, field := range fields {
		switch field {
		case "title":
			args = append(args, "--title", in.Title)
		case "description":
			args = append(args, "--description", in.Description)
		case "status":
			args = append(args, "--status", in.Status)
		case "icon":
			args = append(args, "--icon", in.Icon)
		case "lead":
			args = append(args, "--lead", in.LeadID)
		case "startDate":
			args = append(args, "--start-date", in.StartDate)
		case "dueDate":
			args = append(args, "--due-date", in.DueDate)
		}
	}
	return append(args, "--output", "json")
}
func (c *CLI) CreateProject(in model.ProjectInput) (model.WorkspaceProject, error) {
	fields := []string{"title", "description", "status", "icon", "startDate", "dueDate"}
	if in.LeadID != "" {
		fields = append(fields, "lead")
	}
	var v model.WorkspaceProject
	err := c.runJSON(&v, projectArgs([]string{"project", "create"}, in, fields)...)
	if err == nil && v.ID == "" {
		err = fmt.Errorf("project create returned no id")
	}
	return v, err
}
func (c *CLI) UpdateProject(id string, in model.ProjectInput, fields []string) (model.WorkspaceProject, error) {
	var v model.WorkspaceProject
	err := c.runJSON(&v, projectArgs([]string{"project", "update", id}, in, fields)...)
	return v, err
}
func (c *CLI) ListProjectResources(id string) ([]model.ProjectResource, error) {
	var v []model.ProjectResource
	var raw []json.RawMessage
	err := c.runJSON(&raw, "project", "resource", "list", id, "--output", "json")
	if err == nil && raw != nil {
		v = []model.ProjectResource{}
		for _, item := range raw {
			if err = requireJSONKeys(item, "id", "resource_type", "resource_ref", "position"); err != nil {
				break
			}
			var resource model.ProjectResource
			if err = json.Unmarshal(item, &resource); err != nil {
				break
			}
			if resource.ID == "" || resource.Type == "" || resource.Ref == nil {
				err = fmt.Errorf("incomplete project resource")
				break
			}
			v = append(v, resource)
		}
	}
	if err == nil && v == nil {
		err = fmt.Errorf("project resource list returned an incomplete collection")
	}
	return v, err
}
func (c *CLI) AddProjectResource(id string, in model.ProjectResourceSpec) (model.ProjectResource, error) {
	ref, err := json.Marshal(in.Ref)
	if err != nil {
		return model.ProjectResource{}, err
	}
	var v model.ProjectResource
	err = c.runJSON(&v, "project", "resource", "add", id, "--type", in.Type, "--ref", string(ref), "--label", in.Label, "--output", "json")
	if err == nil && v.ID == "" {
		err = fmt.Errorf("project resource add returned no id")
	}
	return v, err
}
func (c *CLI) UpdateProjectResource(projectID, id string, in model.ProjectResourceSpec) error {
	ref, err := json.Marshal(in.Ref)
	if err != nil {
		return err
	}
	var v json.RawMessage
	return c.runJSON(&v, "project", "resource", "update", projectID, id, "--ref", string(ref), "--label", in.Label, "--position", strconv.FormatInt(int64(in.Position), 10), "--output", "json")
}
func (c *CLI) RemoveProjectResource(projectID, id string) error {
	return c.run("project", "resource", "remove", projectID, id, "--output", "json")
}

func (c *CLI) ListAutopilots() ([]model.Autopilot, error) {
	var v struct {
		Items []model.Autopilot `json:"autopilots"`
		Total *int              `json:"total"`
	}
	err := c.runJSON(&v, "autopilot", "list", "--output", "json")
	if err == nil && (v.Items == nil || v.Total == nil || *v.Total != len(v.Items)) {
		err = fmt.Errorf("autopilot list returned an incomplete collection")
	}
	return v.Items, err
}
func (c *CLI) GetAutopilot(id string) (model.AutopilotDetail, error) {
	var v model.AutopilotDetail
	var raw struct {
		Autopilot     json.RawMessage         `json:"autopilot"`
		Triggers      []json.RawMessage       `json:"triggers"`
		Collaborators []model.AutopilotMember `json:"collaborators"`
	}
	err := c.runJSON(&raw, "autopilot", "get", id, "--output", "json")
	if err == nil {
		err = requireJSONKeys(raw.Autopilot, "id", "title", "description", "assignee_id", "assignee_type", "project_id", "status", "execution_mode", "issue_title_template", "subscribers")
	}
	if err == nil {
		err = json.Unmarshal(raw.Autopilot, &v.Autopilot)
	}
	if err == nil && raw.Triggers != nil {
		v.Triggers = []model.AutopilotTrigger{}
		for _, item := range raw.Triggers {
			if err = requireJSONKeys(item, "id", "kind", "enabled"); err != nil {
				break
			}
			var trigger model.AutopilotTrigger
			if err = json.Unmarshal(item, &trigger); err != nil {
				break
			}
			if trigger.ID == "" || trigger.Kind == "" {
				err = fmt.Errorf("incomplete autopilot trigger")
				break
			}
			if trigger.Kind == "webhook" {
				err = requireJSONKeys(item, "provider", "has_signing_secret")
			}
			if trigger.Kind == "schedule" {
				err = requireJSONKeys(item, "cron_expression", "timezone")
			}
			if err != nil {
				break
			}
			v.Triggers = append(v.Triggers, trigger)
		}
	}
	v.Collaborators = raw.Collaborators
	if err == nil && (v.Autopilot.ID != id || v.Autopilot.Title == "" || v.Autopilot.Status == "" || v.Autopilot.AssigneeID == "" || v.Autopilot.AssigneeType == "" || v.Autopilot.Mode == "" || v.Triggers == nil || v.Collaborators == nil || v.Autopilot.Subscribers == nil) {
		err = fmt.Errorf("autopilot get returned incomplete or mismatched metadata")
	}
	return v, err
}
func autopilotArgs(args []string, in model.AutopilotInput, fields []string) []string {
	for _, field := range fields {
		switch field {
		case "title":
			args = append(args, "--title", in.Title)
		case "description":
			args = append(args, "--description", in.Description)
		case "agent":
			args = append(args, "--agent", in.AgentID)
		case "project":
			args = append(args, "--project", in.ProjectID)
		case "status":
			args = append(args, "--status", in.Status)
		case "mode":
			args = append(args, "--mode", in.Mode)
		case "issueTitleTemplate":
			args = append(args, "--issue-title-template", in.IssueTitleTemplate)
		case "subscribers":
			if in.Subscribers != nil {
				if len(*in.Subscribers) == 0 {
					args = append(args, "--clear-subscribers")
				}
				for _, id := range *in.Subscribers {
					args = append(args, "--subscriber", id)
				}
			}
		}
	}
	return append(args, "--output", "json")
}
func (c *CLI) CreateAutopilot(in model.AutopilotInput) (model.Autopilot, error) {
	fields := []string{"title", "description", "agent", "mode", "project", "issueTitleTemplate"}
	if in.Subscribers != nil && len(*in.Subscribers) > 0 {
		fields = append(fields, "subscribers")
	}
	var v model.Autopilot
	err := c.runJSON(&v, autopilotArgs([]string{"autopilot", "create"}, in, fields)...)
	if err == nil && v.ID == "" {
		err = fmt.Errorf("autopilot create returned no id")
	}
	return v, err
}
func (c *CLI) UpdateAutopilot(id string, in model.AutopilotInput, fields []string) error {
	var v json.RawMessage
	return c.runJSON(&v, autopilotArgs([]string{"autopilot", "update", id}, in, fields)...)
}
func (c *CLI) AddAutopilotTrigger(id string, in model.AutopilotTriggerSpec) (model.AutopilotTrigger, error) {
	args := []string{"autopilot", "trigger-add", id, "--kind", in.Kind, "--label", in.Label}
	if in.Kind == "schedule" {
		args = append(args, "--cron", in.Cron, "--timezone", in.Timezone)
	}
	var v model.AutopilotTrigger
	err := c.runJSON(&v, append(args, "--output", "json")...)
	if err == nil && v.ID == "" {
		err = fmt.Errorf("autopilot trigger-add returned no id")
	}
	return v, err
}
func (c *CLI) UpdateAutopilotTrigger(autopilotID, id string, in model.AutopilotTriggerSpec) error {
	args := []string{"autopilot", "trigger-update", autopilotID, id, "--enabled=" + strconv.FormatBool(in.Enabled), "--label", in.Label}
	if in.Kind == "schedule" {
		args = append(args, "--cron", in.Cron, "--timezone", in.Timezone)
	}
	var v json.RawMessage
	return c.runJSON(&v, append(args, "--output", "json")...)
}
func (c *CLI) DeleteAutopilotTrigger(autopilotID, id string) error {
	return c.run("autopilot", "trigger-delete", autopilotID, id)
}

// Missing fields must never be interpreted as intentional empty configuration.
// Null remains valid for nullable API fields (descriptions, optional references).
func requireJSONKeys(raw json.RawMessage, keys ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("incomplete workspace resource response")
	}
	for _, key := range keys {
		if value, ok := object[key]; !ok || (bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !slices.Contains([]string{"description", "icon", "lead_type", "lead_id", "start_date", "due_date", "project_id", "issue_title_template", "label", "provider"}, key)) {
			return fmt.Errorf("workspace resource response is missing %s", key)
		}
	}
	return nil
}
