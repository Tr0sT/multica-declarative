package reconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

const (
	Create = "create"
	Update = "update"
	Noop   = "noop"
)

type Reconciler struct {
	Backend backend.Backend
}

func (r Reconciler) Plan(project model.Project) ([]model.Change, error) {
	remoteSkills, err := r.Backend.ListSkills()
	if err != nil {
		return nil, err
	}
	remoteAgents, err := r.Backend.ListAgents()
	if err != nil {
		return nil, err
	}
	runtimes, err := r.Backend.ListRuntimes()
	if err != nil {
		return nil, err
	}
	runtimeIDs, err := ResolveRuntimes(project.RuntimeSelectors, runtimes)
	if err != nil {
		return nil, err
	}

	changes := make([]model.Change, 0, len(project.Skills)+len(project.Agents))
	for _, desired := range project.Skills {
		matches := skillsNamed(remoteSkills, desired.Name)
		switch len(matches) {
		case 0:
			changes = append(changes, model.Change{Action: Create, Kind: "skill", Name: desired.Name})
		case 1:
			actual, err := r.Backend.GetSkill(matches[0].ID)
			if err != nil {
				return nil, err
			}
			changes = append(changes, makeChange("skill", desired.Name, diffSkill(desired, actual)))
		default:
			return nil, fmt.Errorf("multiple Multica skills named %q", desired.Name)
		}
	}

	for _, desired := range project.Agents {
		matches := agentsNamed(remoteAgents, desired.Name)
		switch len(matches) {
		case 0:
			changes = append(changes, model.Change{Action: Create, Kind: "agent", Name: desired.Name})
		case 1:
			actual, err := r.Backend.GetAgent(matches[0].ID)
			if err != nil {
				return nil, err
			}
			actualSkills, err := r.Backend.ListAgentSkills(actual.ID)
			if err != nil {
				return nil, err
			}
			fields := diffAgent(desired, runtimeIDs[desired.RuntimeRef], actual, actualSkills)
			changes = append(changes, makeChange("agent", desired.Name, fields))
		default:
			return nil, fmt.Errorf("multiple Multica agents named %q; v1 matches agents by exact name", desired.Name)
		}
	}
	return changes, nil
}

func (r Reconciler) Apply(project model.Project, report func(model.Change)) error {
	remoteSkills, err := r.Backend.ListSkills()
	if err != nil {
		return err
	}
	skillIDs := make(map[string]string, len(project.Skills))

	for _, desired := range project.Skills {
		matches := skillsNamed(remoteSkills, desired.Name)
		var actual model.Skill
		var actualFiles []model.SkillFile

		switch len(matches) {
		case 0:
			actual, err = r.Backend.CreateSkill(skillInput(desired))
			if err != nil {
				return err
			}
			report(model.Change{Action: Create, Kind: "skill", Name: desired.Name})
		case 1:
			actual, err = r.Backend.GetSkill(matches[0].ID)
			if err != nil {
				return err
			}
			actualFiles = append([]model.SkillFile(nil), actual.Files...)
			fields := diffSkill(desired, actual)
			if hasNonFileField(fields) {
				updated, updateErr := r.Backend.UpdateSkill(actual.ID, skillInput(desired))
				if updateErr != nil {
					return updateErr
				}
				if updated.ID != "" {
					actual.ID = updated.ID
				}
			}
			report(makeChange("skill", desired.Name, fields))
		default:
			return fmt.Errorf("multiple Multica skills named %q", desired.Name)
		}

		if err := r.syncSkillFiles(desired, actual.ID, actualFiles); err != nil {
			return err
		}
		skillIDs[desired.Name] = actual.ID
	}

	runtimes, err := r.Backend.ListRuntimes()
	if err != nil {
		return err
	}
	runtimeIDs, err := ResolveRuntimes(project.RuntimeSelectors, runtimes)
	if err != nil {
		return err
	}
	remoteAgents, err := r.Backend.ListAgents()
	if err != nil {
		return err
	}

	for _, desired := range project.Agents {
		runtimeID := runtimeIDs[desired.RuntimeRef]
		input := agentInput(desired, runtimeID)
		matches := agentsNamed(remoteAgents, desired.Name)
		var actual model.Agent
		var actualSkills []model.SkillSummary

		switch len(matches) {
		case 0:
			actual, err = r.Backend.CreateAgent(input)
			if err != nil {
				return err
			}
			report(model.Change{Action: Create, Kind: "agent", Name: desired.Name})
		case 1:
			actual, err = r.Backend.GetAgent(matches[0].ID)
			if err != nil {
				return err
			}
			actualSkills, err = r.Backend.ListAgentSkills(actual.ID)
			if err != nil {
				return err
			}
			fields := diffAgent(desired, runtimeID, actual, actualSkills)
			if hasNonSkillField(fields) {
				updated, updateErr := r.Backend.UpdateAgent(actual.ID, input)
				if updateErr != nil {
					return updateErr
				}
				if updated.ID != "" {
					actual.ID = updated.ID
				}
			}
			report(makeChange("agent", desired.Name, fields))
		default:
			return fmt.Errorf("multiple Multica agents named %q; v1 matches agents by exact name", desired.Name)
		}

		if len(matches) == 0 || !equalStrings(sortedSkillNames(actualSkills), sortedStrings(desired.Skills)) {
			ids := make([]string, 0, len(desired.Skills))
			for _, name := range desired.Skills {
				ids = append(ids, skillIDs[name])
			}
			if err := r.Backend.SetAgentSkills(actual.ID, ids); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r Reconciler) syncSkillFiles(desired model.SkillSpec, skillID string, actual []model.SkillFile) error {
	actualByPath := make(map[string]model.SkillFile, len(actual))
	for _, item := range actual {
		actualByPath[item.Path] = item
	}
	desiredPaths := make(map[string]struct{}, len(desired.Files))
	for _, item := range desired.Files {
		desiredPaths[item.Path] = struct{}{}
		existing, found := actualByPath[item.Path]
		if !found || existing.Content != item.Content {
			_, err := r.Backend.UpsertSkillFile(skillID, model.SkillFileInput{
				Path:        item.Path,
				ContentFile: item.SourcePath,
			})
			if err != nil {
				return err
			}
		}
	}
	for _, item := range actual {
		if _, found := desiredPaths[item.Path]; !found {
			if err := r.Backend.DeleteSkillFile(skillID, item.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func ResolveRuntimes(selectors map[string]model.RuntimeSelector, runtimes []model.Runtime) (map[string]string, error) {
	result := make(map[string]string, len(selectors))
	for alias, selector := range selectors {
		matches := make([]model.Runtime, 0, 1)
		for _, runtime := range runtimes {
			if runtimeMatches(runtime, selector) {
				matches = append(matches, runtime)
			}
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("runtime selector %q matched no Multica runtimes", alias)
		case 1:
			result[alias] = matches[0].ID
		default:
			return nil, fmt.Errorf("runtime selector %q matched %d Multica runtimes; make it more specific", alias, len(matches))
		}
	}
	return result, nil
}

func FormatChange(change model.Change) string {
	prefix := map[string]string{Create: "+", Update: "~", Noop: "="}[change.Action]
	result := fmt.Sprintf("%s %-5s %s", prefix, change.Kind, change.Name)
	if len(change.Fields) > 0 {
		result += " [" + strings.Join(change.Fields, ", ") + "]"
	}
	return result
}

func diffSkill(desired model.SkillSpec, actual model.Skill) []string {
	var fields []string
	if desired.Description != actual.Description {
		fields = append(fields, "description")
	}
	if desired.Content != actual.Content {
		fields = append(fields, "content")
	}
	desiredFiles := make(map[string]string, len(desired.Files))
	for _, item := range desired.Files {
		desiredFiles[item.Path] = item.Content
	}
	actualFiles := make(map[string]string, len(actual.Files))
	for _, item := range actual.Files {
		actualFiles[item.Path] = item.Content
	}
	if !equalMaps(desiredFiles, actualFiles) {
		fields = append(fields, "files")
	}
	return fields
}

func diffAgent(desired model.AgentSpec, runtimeID string, actual model.Agent, actualSkills []model.SkillSummary) []string {
	var fields []string
	if desired.Description != actual.Description {
		fields = append(fields, "description")
	}
	if desired.Instructions != actual.Instructions {
		fields = append(fields, "instructions")
	}
	if runtimeID != actual.RuntimeID {
		fields = append(fields, "runtime")
	}
	if desired.ModelID != actual.Model {
		fields = append(fields, "model")
	}
	if desired.ThinkingLevel != actual.ThinkingLevel {
		fields = append(fields, "thinkingLevel")
	}
	if desired.MaxConcurrentTasks != actual.MaxConcurrentTasks {
		fields = append(fields, "maxConcurrentTasks")
	}
	if !equalStrings(desired.CustomArgs, actual.CustomArgs) {
		fields = append(fields, "customArgs")
	}
	if !permissionMatches(desired.Permission, actual) {
		fields = append(fields, "permission")
	}
	if !equalStrings(sortedStrings(desired.Skills), sortedSkillNames(actualSkills)) {
		fields = append(fields, "skills")
	}
	return fields
}

func permissionMatches(permission string, actual model.Agent) bool {
	if permission == "private" {
		return actual.PermissionMode == "private" && len(actual.InvocationTargets) == 0
	}
	return actual.PermissionMode == "public_to" &&
		len(actual.InvocationTargets) == 1 &&
		actual.InvocationTargets[0].TargetType == "workspace"
}

func runtimeMatches(runtime model.Runtime, selector model.RuntimeSelector) bool {
	return (selector.ID == "" || runtime.ID == selector.ID) &&
		(selector.Name == "" || runtime.Name == selector.Name) &&
		(selector.CustomName == "" || runtime.CustomName == selector.CustomName) &&
		(selector.Provider == "" || runtime.Provider == selector.Provider)
}

func skillInput(item model.SkillSpec) model.SkillInput {
	return model.SkillInput{Name: item.Name, Description: item.Description, ContentFile: item.ContentPath}
}

func agentInput(item model.AgentSpec, runtimeID string) model.AgentInput {
	return model.AgentInput{
		Name:               item.Name,
		Description:        item.Description,
		Instructions:       item.Instructions,
		RuntimeID:          runtimeID,
		Model:              item.ModelID,
		ThinkingLevel:      item.ThinkingLevel,
		CustomArgs:         append([]string{}, item.CustomArgs...),
		Permission:         item.Permission,
		MaxConcurrentTasks: item.MaxConcurrentTasks,
	}
}

func makeChange(kind, name string, fields []string) model.Change {
	action := Noop
	if len(fields) > 0 {
		action = Update
	}
	return model.Change{Action: action, Kind: kind, Name: name, Fields: fields}
}

func skillsNamed(items []model.Skill, name string) []model.Skill {
	var result []model.Skill
	for _, item := range items {
		if item.Name == name {
			result = append(result, item)
		}
	}
	return result
}

func agentsNamed(items []model.Agent, name string) []model.Agent {
	var result []model.Agent
	for _, item := range items {
		if item.Name == name {
			result = append(result, item)
		}
	}
	return result
}

func sortedStrings(items []string) []string {
	result := append([]string(nil), items...)
	sort.Strings(result)
	return result
}

func sortedSkillNames(items []model.SkillSummary) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	sort.Strings(result)
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func hasNonFileField(fields []string) bool {
	for _, field := range fields {
		if field != "files" {
			return true
		}
	}
	return false
}

func hasNonSkillField(fields []string) bool {
	for _, field := range fields {
		if field != "skills" {
			return true
		}
	}
	return false
}
