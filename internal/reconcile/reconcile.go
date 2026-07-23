package reconcile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

const (
	Create = "create"
	Update = "update"
	Noop   = "noop"
)

type Reconciler struct {
	Backend    backend.Backend
	HTTPClient *http.Client
}

type observedAgent struct {
	resource model.Agent
	skills   []model.SkillSummary
}

type observedSquad struct {
	resource model.Squad
	members  []model.SquadMember
}

type inspection struct {
	changes      []model.Change
	skillChanges map[string]model.Change
	agentChanges map[string]model.Change
	squadChanges map[string]model.Change
	skills       map[string]model.Skill
	agents       map[string]observedAgent
	squads       map[string]observedSquad
	runtimeIDs   map[string]string
	squadOps     backend.SquadOperations
}

func (r Reconciler) Plan(project model.Project) ([]model.Change, error) {
	state, err := r.inspect(project)
	if err != nil {
		return nil, err
	}
	return state.changes, nil
}

func (r Reconciler) inspect(project model.Project) (inspection, error) {
	state := inspection{
		skillChanges: map[string]model.Change{},
		agentChanges: map[string]model.Change{},
		squadChanges: map[string]model.Change{},
		skills:       map[string]model.Skill{},
		agents:       map[string]observedAgent{},
		squads:       map[string]observedSquad{},
	}
	if r.Backend == nil {
		return state, fmt.Errorf("reconcile backend is required")
	}
	remoteSkills, err := r.Backend.ListSkills()
	if err != nil {
		return state, err
	}
	remoteAgents, err := r.Backend.ListAgents()
	if err != nil {
		return state, err
	}
	runtimes, err := r.Backend.ListRuntimes()
	if err != nil {
		return state, err
	}
	runtimeIDs, err := ResolveRuntimes(project.RuntimeSelectors, runtimes)
	if err != nil {
		return state, err
	}
	state.runtimeIDs = runtimeIDs
	for _, d := range project.Skills {
		m := skillsNamed(remoteSkills, d.Name)
		var change model.Change
		switch len(m) {
		case 0:
			change = model.Change{Action: Create, Kind: "skill", Name: d.Name}
		case 1:
			a, err := r.Backend.GetSkill(m[0].ID)
			if err != nil {
				return state, err
			}
			if a.ID == "" {
				a.ID = m[0].ID
			}
			if a.ID == "" {
				return state, fmt.Errorf("skill %q has no id", d.Name)
			}
			state.skills[d.Name] = a
			change = makeChange("skill", d.Name, diffSkill(d, a))
		default:
			return state, fmt.Errorf("multiple Multica skills named %q", d.Name)
		}
		state.skillChanges[d.Name] = change
		state.changes = append(state.changes, change)
	}
	agentByName := map[string]model.Agent{}
	for _, d := range project.Agents {
		m := agentsNamed(remoteAgents, d.Name)
		var change model.Change
		switch len(m) {
		case 0:
			if err := validateObservedOnlyOnCreate(d); err != nil {
				return state, err
			}
			if d.ManageCustomEnv || d.AvatarFile != "" || d.Archived {
				if _, ok := r.Backend.(backend.AgentOperations); !ok {
					return state, fmt.Errorf("backend cannot apply auxiliary fields for agent %q", d.Name)
				}
			}
			change = model.Change{Action: Create, Kind: "agent", Name: d.Name}
		case 1:
			a, err := r.Backend.GetAgent(m[0].ID)
			if err != nil {
				return state, err
			}
			if a.ID == "" {
				a.ID = m[0].ID
			}
			if a.ID == "" {
				return state, fmt.Errorf("agent %q has no id", d.Name)
			}
			agentByName[d.Name] = a
			skills, err := r.Backend.ListAgentSkills(a.ID)
			if err != nil {
				return state, err
			}
			fields, err := r.diffAgent(d, runtimeIDs[d.RuntimeRef], a, skills)
			if err != nil {
				return state, err
			}
			if err := validateObservedOnlyChanges(d, a, skills); err != nil {
				return state, err
			}
			if hasAnyField(fields, "customEnv", "avatar", "archived") || (a.Archived() && requiresActiveAgent(fields)) {
				if _, ok := r.Backend.(backend.AgentOperations); !ok {
					return state, fmt.Errorf("backend cannot apply auxiliary fields for agent %q", d.Name)
				}
			}
			state.agents[d.Name] = observedAgent{resource: a, skills: skills}
			change = makeChange("agent", d.Name, fields)
		default:
			return state, fmt.Errorf("multiple Multica agents named %q", d.Name)
		}
		state.agentChanges[d.Name] = change
		state.changes = append(state.changes, change)
	}
	if len(project.Squads) > 0 {
		ops, ok := r.Backend.(backend.SquadOperations)
		if !ok {
			return state, fmt.Errorf("backend does not support squads")
		}
		state.squadOps = ops
		actual, err := ops.ListSquads()
		if err != nil {
			return state, err
		}
		for _, d := range project.Squads {
			m := squadsNamed(actual, d.Name)
			var change model.Change
			switch len(m) {
			case 0:
				change = model.Change{Action: Create, Kind: "squad", Name: d.Name}
			case 1:
				a, err := ops.GetSquad(m[0].ID)
				if err != nil {
					return state, err
				}
				if a.ID == "" {
					a.ID = m[0].ID
				}
				if a.ID == "" {
					return state, fmt.Errorf("squad %q has no id", d.Name)
				}
				members, err := ops.ListSquadMembers(a.ID)
				if err != nil {
					return state, err
				}
				fields := diffSquad(d, a, members, agentByName)
				state.squads[d.Name] = observedSquad{resource: a, members: members}
				change = makeChange("squad", d.Name, fields)
			default:
				return state, fmt.Errorf("multiple squads named %q", d.Name)
			}
			state.squadChanges[d.Name] = change
			state.changes = append(state.changes, change)
		}
	}
	return state, nil
}

func (r Reconciler) Apply(project model.Project, report func(model.Change)) error {
	if report == nil {
		report = func(model.Change) {}
	}
	state, err := r.inspect(project)
	if err != nil {
		return err
	}
	skillIDs := map[string]string{}
	for _, d := range project.Skills {
		change := state.skillChanges[d.Name]
		a, exists := state.skills[d.Name]
		files := append([]model.SkillFile(nil), a.Files...)
		if !exists {
			a, err = r.Backend.CreateSkill(skillInput(d))
			if err != nil {
				return err
			}
		} else {
			if hasNonFileField(change.Fields) {
				updated, e := r.Backend.UpdateSkill(a.ID, skillInput(d))
				if e != nil {
					return e
				}
				if updated.ID != "" {
					a.ID = updated.ID
				}
			}
		}
		if a.ID == "" {
			return fmt.Errorf("skill %q has no id after reconciliation", d.Name)
		}
		if err := r.syncSkillFiles(d, a.ID, files); err != nil {
			return err
		}
		skillIDs[d.Name] = a.ID
		report(change)
	}
	agentIDs := map[string]string{}
	for _, d := range project.Agents {
		change := state.agentChanges[d.Name]
		observed, exists := state.agents[d.Name]
		a := observed.resource
		actualSkills := observed.skills
		created := !exists
		input := agentInput(d, state.runtimeIDs[d.RuntimeRef])
		restoredForUpdate := false
		if created {
			a, err = r.Backend.CreateAgent(input)
			if err != nil {
				return err
			}
		} else {
			if a.Archived() && requiresActiveAgent(change.Fields) {
				ops, ok := r.Backend.(backend.AgentOperations)
				if !ok {
					return fmt.Errorf("backend cannot restore archived agent %q", d.Name)
				}
				if err := ops.RestoreAgent(a.ID); err != nil {
					return err
				}
				a.ArchivedAt = nil
				restoredForUpdate = true
			}
			base := baseAgentFields(change.Fields)
			if len(base) > 0 {
				updated, e := r.Backend.UpdateAgent(a.ID, input)
				if e != nil {
					return e
				}
				if updated.ID != "" {
					a.ID = updated.ID
				}
			}
		}
		if a.ID == "" {
			return fmt.Errorf("agent %q has no id after reconciliation", d.Name)
		}
		desiredEnabled := enabledSkillNames(d.SkillAssignments)
		if created || !equalStrings(sortedSkillNames(actualSkills, true), sortedStrings(desiredEnabled)) {
			ids := []string{}
			for _, name := range desiredEnabled {
				id := skillIDs[name]
				if id == "" {
					return fmt.Errorf("agent %q references skill %q without a resolved id", d.Name, name)
				}
				ids = append(ids, id)
			}
			if err := r.Backend.SetAgentSkills(a.ID, ids); err != nil {
				return err
			}
		}
		if d.ManageCustomEnv {
			if created || hasAnyField(change.Fields, "customEnv") {
				ops := r.Backend.(backend.AgentOperations)
				if err := ops.SetAgentEnv(a.ID, d.CustomEnvFile); err != nil {
					return err
				}
			}
		}
		if d.AvatarFile != "" {
			if created || hasAnyField(change.Fields, "avatar") {
				ops := r.Backend.(backend.AgentOperations)
				if err := ops.UploadAgentAvatar(a.ID, d.AvatarFile); err != nil {
					return err
				}
			}
		}
		desiredArchived := d.Archived
		if desiredArchived != a.Archived() || (desiredArchived && restoredForUpdate) {
			ops, ok := r.Backend.(backend.AgentOperations)
			if !ok {
				return fmt.Errorf("backend cannot change archived state")
			}
			if desiredArchived {
				if err := ops.ArchiveAgent(a.ID); err != nil {
					return err
				}
			} else {
				if err := ops.RestoreAgent(a.ID); err != nil {
					return err
				}
			}
		}
		agentIDs[d.Name] = a.ID
		report(change)
	}
	if len(project.Squads) > 0 {
		ops := state.squadOps
		for _, d := range project.Squads {
			leaderID := agentIDs[d.Leader]
			change := state.squadChanges[d.Name]
			observed, exists := state.squads[d.Name]
			a := observed.resource
			members := observed.members
			if !exists {
				a, err = ops.CreateSquad(squadInput(d, leaderID))
				if err != nil {
					return err
				}
				if a.ID == "" {
					return fmt.Errorf("create squad %q returned no id", d.Name)
				}
				members, err = ops.ListSquadMembers(a.ID)
				if err != nil {
					return err
				}
				fields := []string{}
				if d.Instructions != "" {
					fields = append(fields, "instructions")
				}
				if d.AvatarURL != "" {
					fields = append(fields, "avatarUrl")
				}
				if len(fields) > 0 {
					updated, updateErr := ops.UpdateSquad(a.ID, squadInput(d, leaderID), fields)
					err = updateErr
					if err != nil {
						return err
					}
					if updated.ID != "" {
						a.ID = updated.ID
					}
				}
			} else {
				base := squadBaseFields(change.Fields)
				if len(base) > 0 {
					updated, updateErr := ops.UpdateSquad(a.ID, squadInput(d, leaderID), base)
					err = updateErr
					if err != nil {
						return err
					}
					if updated.ID != "" {
						a.ID = updated.ID
					}
				}
			}
			if err := syncSquadMembers(ops, a.ID, d, agentIDs, members); err != nil {
				return err
			}
			report(change)
		}
	}
	return nil
}

func (r Reconciler) diffAgent(d model.AgentSpec, runtimeID string, a model.Agent, skills []model.SkillSummary) ([]string, error) {
	fields := []string{}
	if d.Description != a.Description {
		fields = append(fields, "description")
	}
	if d.Instructions != a.Instructions {
		fields = append(fields, "instructions")
	}
	if runtimeID != a.RuntimeID {
		fields = append(fields, "runtime")
	}
	if !equalJSON(d.RuntimeConfig, a.RuntimeConfig) {
		fields = append(fields, "runtimeConfig")
	}
	if d.ModelID != a.Model {
		fields = append(fields, "model")
	}
	if d.ThinkingLevel != a.ThinkingLevel {
		fields = append(fields, "thinkingLevel")
	}
	if d.MaxConcurrentTasks != a.MaxConcurrentTasks {
		fields = append(fields, "maxConcurrentTasks")
	}
	if !equalStrings(d.CustomArgs, a.CustomArgs) {
		fields = append(fields, "customArgs")
	}
	if !permissionMatches(d, a) {
		fields = append(fields, "permission")
	}
	if !equalSkillAssignments(d.SkillAssignments, skills) {
		fields = append(fields, "skills")
	}
	if d.ManageMCPConfig {
		if a.MCPConfigRedacted {
			return nil, fmt.Errorf("agent %q MCP config is redacted", d.Name)
		}
		if !equalRawJSON(d.MCPConfig, a.MCPConfig) {
			fields = append(fields, "mcpConfig")
		}
	}
	if d.ManageCustomEnv {
		ops, ok := r.Backend.(backend.AgentOperations)
		if !ok {
			return nil, fmt.Errorf("backend cannot read custom env")
		}
		env, err := ops.GetAgentEnv(a.ID)
		if err != nil {
			return nil, err
		}
		if !equalStringMap(d.CustomEnv, env) {
			fields = append(fields, "customEnv")
		}
	}
	if d.AvatarFile != "" {
		if a.AvatarURL == nil || *a.AvatarURL == "" {
			fields = append(fields, "avatar")
		} else {
			different, err := r.avatarDiffers(d.AvatarFile, *a.AvatarURL)
			if err != nil {
				return nil, err
			}
			if different {
				fields = append(fields, "avatar")
			}
		}
	}
	if d.Archived != a.Archived() {
		fields = append(fields, "archived")
	}
	if !equalDisabled(d.DisabledRuntimeSkills, a.DisabledRuntimeSkills) {
		fields = append(fields, "disabledRuntimeSkills")
	}
	if a.ComposioToolkitAllowlistRedacted {
		return nil, fmt.Errorf("agent %q Composio allowlist is redacted", d.Name)
	}
	if !equalStrings(sortedStrings(d.ComposioToolkitAllowlist), sortedStrings(a.ComposioToolkitAllowlist)) {
		fields = append(fields, "composioToolkitAllowlist")
	}
	return fields, nil
}

func (r Reconciler) avatarDiffers(file, url string) (bool, error) {
	local, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return false, fmt.Errorf("download current avatar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("download current avatar: HTTP %s", resp.Status)
	}
	remote, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return false, err
	}
	return !bytes.Equal(local, remote), nil
}

func syncSquadMembers(ops backend.SquadOperations, id string, d model.SquadSpec, agentIDs map[string]string, actual []model.SquadMember) error {
	desired := map[string]model.SquadMember{}
	leaderID := agentIDs[d.Leader]
	desired["agent:"+leaderID] = model.SquadMember{MemberID: leaderID, MemberType: "agent", Role: "leader"}
	for _, m := range d.Members {
		memberID := m.ID
		if m.Type == "agent" {
			memberID = agentIDs[m.Agent]
		}
		key := m.Type + ":" + memberID
		role := m.Role
		if m.Type == "agent" && memberID == leaderID {
			role = "leader"
		}
		desired[key] = model.SquadMember{MemberID: memberID, MemberType: m.Type, Role: role}
	}
	actualMap := map[string]model.SquadMember{}
	for _, m := range actual {
		actualMap[m.MemberType+":"+m.MemberID] = m
	}
	for _, key := range sortedMapKeys(desired) {
		m := desired[key]
		a, ok := actualMap[key]
		if !ok {
			if err := ops.AddSquadMember(id, m); err != nil {
				return err
			}
		} else if a.Role != m.Role {
			if err := ops.SetSquadMemberRole(id, m); err != nil {
				return err
			}
		}
	}
	for _, key := range sortedMapKeys(actualMap) {
		m := actualMap[key]
		if _, ok := desired[key]; !ok {
			if err := ops.RemoveSquadMember(id, m); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func diffSquad(d model.SquadSpec, a model.Squad, members []model.SquadMember, agents map[string]model.Agent) []string {
	fields := []string{}
	if d.Description != a.Description {
		fields = append(fields, "description")
	}
	if d.Instructions != a.Instructions {
		fields = append(fields, "instructions")
	}
	leaderID := ""
	if v, ok := agents[d.Leader]; ok {
		leaderID = v.ID
	}
	if leaderID == "" || leaderID != a.LeaderID {
		fields = append(fields, "leader")
	}
	avatar := ""
	if a.AvatarURL != nil {
		avatar = *a.AvatarURL
	}
	if d.AvatarURL != avatar {
		fields = append(fields, "avatarUrl")
	}
	if !squadMembersMatch(d, members, agents) {
		fields = append(fields, "members")
	}
	return fields
}
func squadMembersMatch(d model.SquadSpec, actual []model.SquadMember, agents map[string]model.Agent) bool {
	ids := map[string]string{}
	for name, a := range agents {
		ids[name] = a.ID
	}
	desired := map[string]string{}
	leaderID := ids[d.Leader]
	if leaderID != "" {
		desired["agent:"+leaderID] = "leader"
	}
	for _, m := range d.Members {
		id := m.ID
		if m.Type == "agent" {
			id = ids[m.Agent]
		}
		if id == "" {
			return false
		}
		role := m.Role
		if m.Type == "agent" && id == leaderID {
			role = "leader"
		}
		desired[m.Type+":"+id] = role
	}
	got := map[string]string{}
	for _, m := range actual {
		got[m.MemberType+":"+m.MemberID] = m.Role
	}
	return equalStringMap(desired, got)
}

func validateObservedOnlyOnCreate(d model.AgentSpec) error {
	disabled := false
	for _, s := range d.SkillAssignments {
		if !s.Enabled {
			disabled = true
		}
	}
	if disabled || len(d.DisabledRuntimeSkills) > 0 || len(d.ComposioToolkitAllowlist) > 0 {
		return fmt.Errorf("agent %q uses fields that the official Multica CLI can only observe, not create: disabled skills, disabledRuntimeSkills, or composioToolkitAllowlist", d.Name)
	}
	for _, t := range d.InvocationTargets {
		if t.TargetType == "team" {
			return fmt.Errorf("agent %q uses team invocation targets not supported by the CLI", d.Name)
		}
	}
	return nil
}
func validateObservedOnlyChanges(d model.AgentSpec, a model.Agent, skills []model.SkillSummary) error {
	if !equalSkillAssignments(d.SkillAssignments, skills) {
		for _, s := range d.SkillAssignments {
			if !s.Enabled {
				return fmt.Errorf("agent %q disabled skill assignments cannot be changed through the official CLI", d.Name)
			}
		}
	}
	if !equalDisabled(d.DisabledRuntimeSkills, a.DisabledRuntimeSkills) {
		return fmt.Errorf("agent %q disabledRuntimeSkills cannot be changed through the official CLI", d.Name)
	}
	if !equalStrings(sortedStrings(d.ComposioToolkitAllowlist), sortedStrings(a.ComposioToolkitAllowlist)) {
		return fmt.Errorf("agent %q composioToolkitAllowlist cannot be changed through the official CLI", d.Name)
	}
	return nil
}
func diffSkill(d model.SkillSpec, a model.Skill) []string {
	fields := []string{}
	if d.Description != a.Description {
		fields = append(fields, "description")
	}
	if d.Content != a.Content {
		fields = append(fields, "content")
	}
	dm := map[string]string{}
	for _, f := range d.Files {
		dm[f.Path] = f.Content
	}
	am := map[string]string{}
	for _, f := range a.Files {
		am[f.Path] = f.Content
	}
	if !equalStringMap(dm, am) {
		fields = append(fields, "files")
	}
	return fields
}
func permissionMatches(d model.AgentSpec, a model.Agent) bool {
	mode := d.PermissionMode
	if mode == "" {
		mode = "private"
	}
	if mode != a.PermissionMode {
		return false
	}
	return equalTargets(d.InvocationTargets, a.InvocationTargets)
}
func equalTargets(a, b []model.InvocationTarget) bool {
	return equalStrings(targetKeys(a), targetKeys(b))
}
func targetKeys(items []model.InvocationTarget) []string {
	out := []string{}
	for _, t := range items {
		id := ""
		// Multica returns the current workspace ID for workspace targets, while
		// declarations intentionally represent that target as workspace: true.
		// The ID is therefore transport metadata rather than part of the desired
		// identity. Member (and any future scoped) targets still compare by ID.
		if t.TargetType != "workspace" && t.TargetID != nil {
			id = *t.TargetID
		}
		out = append(out, t.TargetType+":"+id)
	}
	sort.Strings(out)
	return out
}
func equalSkillAssignments(d []model.AgentSkillSpec, a []model.SkillSummary) bool {
	dm := map[string]bool{}
	for _, s := range d {
		dm[s.Name] = s.Enabled
	}
	am := map[string]bool{}
	for _, s := range a {
		enabled := true
		if s.Enabled != nil {
			enabled = *s.Enabled
		}
		am[s.Name] = enabled
	}
	if len(dm) != len(am) {
		return false
	}
	for k, v := range dm {
		if am[k] != v {
			return false
		}
	}
	return true
}
func enabledSkillNames(assignments []model.AgentSkillSpec) []string {
	out := []string{}
	for _, s := range assignments {
		if s.Enabled {
			out = append(out, s.Name)
		}
	}
	return out
}
func equalDisabled(a, b []model.DisabledRuntimeSkill) bool {
	ka := []string{}
	kb := []string{}
	for _, v := range a {
		ka = append(ka, disabledKey(v))
	}
	for _, v := range b {
		kb = append(kb, disabledKey(v))
	}
	sort.Strings(ka)
	sort.Strings(kb)
	return equalStrings(ka, kb)
}
func disabledKey(v model.DisabledRuntimeSkill) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}
func equalJSON(a, b any) bool {
	aa, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return equalRawJSON(aa, bb)
}
func equalRawJSON(a, b []byte) bool {
	if len(bytes.TrimSpace(a)) == 0 {
		a = []byte("null")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		b = []byte("null")
	}
	aa, aErr := canonicalJSON(a)
	bb, bErr := canonicalJSON(b)
	if aErr != nil || bErr != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	return bytes.Equal(aa, bb)
}

func canonicalJSON(value []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(decoded)
}

func (r Reconciler) syncSkillFiles(d model.SkillSpec, id string, actual []model.SkillFile) error {
	am := map[string]model.SkillFile{}
	for _, f := range actual {
		am[f.Path] = f
	}
	dm := map[string]struct{}{}
	for _, f := range d.Files {
		dm[f.Path] = struct{}{}
		a, ok := am[f.Path]
		if !ok || a.Content != f.Content {
			if _, err := r.Backend.UpsertSkillFile(id, model.SkillFileInput{Path: f.Path, ContentFile: f.SourcePath}); err != nil {
				return err
			}
		}
	}
	for _, f := range actual {
		if _, ok := dm[f.Path]; !ok {
			if err := r.Backend.DeleteSkillFile(id, f.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func ResolveRuntimes(selectors map[string]model.RuntimeSelector, runtimes []model.Runtime) (map[string]string, error) {
	out := map[string]string{}
	for _, alias := range sortedMapKeys(selectors) {
		s := selectors[alias]
		matches := []model.Runtime{}
		for _, v := range runtimes {
			if (s.ID == "" || v.ID == s.ID) && (s.Name == "" || v.Name == s.Name) && (s.CustomName == "" || v.CustomName == s.CustomName) && (s.Provider == "" || v.Provider == s.Provider) {
				matches = append(matches, v)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("runtime selector %q matched %d Multica runtimes", alias, len(matches))
		}
		out[alias] = matches[0].ID
	}
	return out, nil
}
func FormatChange(c model.Change) string {
	prefix := map[string]string{Create: "+", Update: "~", Noop: "="}[c.Action]
	v := fmt.Sprintf("%s %-15s %s", prefix, c.Kind, c.Name)
	if len(c.Fields) > 0 {
		v += " [" + strings.Join(c.Fields, ", ") + "]"
	}
	return v
}
func makeChange(kind, name string, fields []string) model.Change {
	action := Noop
	if len(fields) > 0 {
		action = Update
	}
	return model.Change{Action: action, Kind: kind, Name: name, Fields: fields}
}
func skillInput(v model.SkillSpec) model.SkillInput {
	return model.SkillInput{Name: v.Name, Description: v.Description, ContentFile: v.ContentPath}
}
func agentInput(v model.AgentSpec, runtimeID string) model.AgentInput {
	return model.AgentInput{
		Name: v.Name, Description: v.Description, Instructions: v.Instructions, RuntimeID: runtimeID,
		RuntimeConfig: v.RuntimeConfig,
		Model:         v.ModelID, ThinkingLevel: v.ThinkingLevel, CustomArgs: append([]string(nil), v.CustomArgs...),
		PermissionMode:     v.PermissionMode,
		InvocationTargets:  append([]model.InvocationTarget(nil), v.InvocationTargets...),
		MaxConcurrentTasks: v.MaxConcurrentTasks, ManageMCPConfig: v.ManageMCPConfig, MCPConfigFile: v.MCPConfigFile,
	}
}
func squadInput(v model.SquadSpec, leaderID string) model.SquadInput {
	return model.SquadInput{Name: v.Name, Description: v.Description, Instructions: v.Instructions, LeaderID: leaderID, AvatarURL: v.AvatarURL}
}
func skillsNamed(items []model.Skill, name string) []model.Skill {
	out := []model.Skill{}
	for _, v := range items {
		if v.Name == name {
			out = append(out, v)
		}
	}
	return out
}
func agentsNamed(items []model.Agent, name string) []model.Agent {
	out := []model.Agent{}
	for _, v := range items {
		if v.Name == name {
			out = append(out, v)
		}
	}
	return out
}
func squadsNamed(items []model.Squad, name string) []model.Squad {
	out := []model.Squad{}
	for _, v := range items {
		if v.Name == name {
			out = append(out, v)
		}
	}
	return out
}
func sortedStrings(v []string) []string {
	out := append([]string(nil), v...)
	sort.Strings(out)
	return out
}
func sortedSkillNames(v []model.SkillSummary, onlyEnabled bool) []string {
	out := []string{}
	for _, s := range v {
		enabled := true
		if s.Enabled != nil {
			enabled = *s.Enabled
		}
		if !onlyEnabled || enabled {
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func hasNonFileField(v []string) bool {
	for _, f := range v {
		if f != "files" {
			return true
		}
	}
	return false
}
func requiresActiveAgent(fields []string) bool {
	for _, field := range fields {
		switch field {
		case "archived", "disabledRuntimeSkills", "composioToolkitAllowlist":
			continue
		default:
			return true
		}
	}
	return false
}

func hasAnyField(fields []string, wanted ...string) bool {
	for _, field := range fields {
		for _, candidate := range wanted {
			if field == candidate {
				return true
			}
		}
	}
	return false
}

func baseAgentFields(v []string) []string {
	out := []string{}
	for _, f := range v {
		switch f {
		case "description", "instructions", "runtime", "runtimeConfig", "model", "thinkingLevel", "maxConcurrentTasks", "customArgs", "permission", "mcpConfig":
			out = append(out, f)
		}
	}
	return out
}
func squadBaseFields(v []string) []string {
	out := []string{}
	for _, f := range v {
		if f != "members" {
			out = append(out, f)
		}
	}
	return out
}
