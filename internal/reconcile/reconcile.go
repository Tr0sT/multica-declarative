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

func (r Reconciler) Plan(project model.Project) ([]model.Change, error) {
	changes := []model.Change{}
	if len(project.RuntimeProfiles) > 0 {
		ops, ok := r.Backend.(backend.RuntimeProfileOperations)
		if !ok {
			return nil, fmt.Errorf("backend does not support runtime profiles")
		}
		actual, err := ops.ListRuntimeProfiles()
		if err != nil {
			return nil, err
		}
		for _, d := range project.RuntimeProfiles {
			matches := profilesNamed(actual, d.DisplayName)
			switch len(matches) {
			case 0:
				changes = append(changes, model.Change{Action: Create, Kind: "runtime-profile", Name: d.DisplayName})
			case 1:
				fields := diffRuntimeProfile(d, matches[0])
				changes = append(changes, makeChange("runtime-profile", d.DisplayName, fields))
			default:
				return nil, fmt.Errorf("multiple runtime profiles named %q", d.DisplayName)
			}
		}
	}
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
	for _, d := range project.Skills {
		m := skillsNamed(remoteSkills, d.Name)
		switch len(m) {
		case 0:
			changes = append(changes, model.Change{Action: Create, Kind: "skill", Name: d.Name})
		case 1:
			a, err := r.Backend.GetSkill(m[0].ID)
			if err != nil {
				return nil, err
			}
			changes = append(changes, makeChange("skill", d.Name, diffSkill(d, a)))
		default:
			return nil, fmt.Errorf("multiple Multica skills named %q", d.Name)
		}
	}
	agentByName := map[string]model.Agent{}
	for _, d := range project.Agents {
		m := agentsNamed(remoteAgents, d.Name)
		switch len(m) {
		case 0:
			changes = append(changes, model.Change{Action: Create, Kind: "agent", Name: d.Name})
		case 1:
			a, err := r.Backend.GetAgent(m[0].ID)
			if err != nil {
				return nil, err
			}
			agentByName[d.Name] = a
			skills, err := r.Backend.ListAgentSkills(a.ID)
			if err != nil {
				return nil, err
			}
			fields, err := r.diffAgent(d, runtimeIDs[d.RuntimeRef], a, skills)
			if err != nil {
				return nil, err
			}
			changes = append(changes, makeChange("agent", d.Name, fields))
		default:
			return nil, fmt.Errorf("multiple Multica agents named %q", d.Name)
		}
	}
	if len(project.Squads) > 0 {
		ops, ok := r.Backend.(backend.SquadOperations)
		if !ok {
			return nil, fmt.Errorf("backend does not support squads")
		}
		actual, err := ops.ListSquads()
		if err != nil {
			return nil, err
		}
		for _, d := range project.Squads {
			m := squadsNamed(actual, d.Name)
			switch len(m) {
			case 0:
				changes = append(changes, model.Change{Action: Create, Kind: "squad", Name: d.Name})
			case 1:
				a, err := ops.GetSquad(m[0].ID)
				if err != nil {
					return nil, err
				}
				members, err := ops.ListSquadMembers(a.ID)
				if err != nil {
					return nil, err
				}
				fields := diffSquad(d, a, members, agentByName)
				changes = append(changes, makeChange("squad", d.Name, fields))
			default:
				return nil, fmt.Errorf("multiple squads named %q", d.Name)
			}
		}
	}
	return changes, nil
}

func (r Reconciler) Apply(project model.Project, report func(model.Change)) error {
	if report == nil {
		report = func(model.Change) {}
	}
	if len(project.RuntimeProfiles) > 0 {
		ops, ok := r.Backend.(backend.RuntimeProfileOperations)
		if !ok {
			return fmt.Errorf("backend does not support runtime profiles")
		}
		actual, err := ops.ListRuntimeProfiles()
		if err != nil {
			return err
		}
		for _, d := range project.RuntimeProfiles {
			m := profilesNamed(actual, d.DisplayName)
			switch len(m) {
			case 0:
				if err := validateCreatableProfile(d); err != nil {
					return err
				}
				created, err := ops.CreateRuntimeProfile(profileInput(d))
				if err != nil {
					return err
				}
				if !d.Enabled {
					_, err = ops.UpdateRuntimeProfile(created.ID, profileInput(d), []string{"enabled"})
					if err != nil {
						return err
					}
				}
				report(model.Change{Action: Create, Kind: "runtime-profile", Name: d.DisplayName})
			case 1:
				fields := diffRuntimeProfile(d, m[0])
				if err := validateProfileUpdate(fields, d, m[0]); err != nil {
					return err
				}
				mutable := mutableProfileFields(fields)
				if len(mutable) > 0 {
					if _, err := ops.UpdateRuntimeProfile(m[0].ID, profileInput(d), mutable); err != nil {
						return err
					}
				}
				report(makeChange("runtime-profile", d.DisplayName, fields))
			default:
				return fmt.Errorf("multiple runtime profiles named %q", d.DisplayName)
			}
		}
	}
	remoteSkills, err := r.Backend.ListSkills()
	if err != nil {
		return err
	}
	skillIDs := map[string]string{}
	for _, d := range project.Skills {
		m := skillsNamed(remoteSkills, d.Name)
		var a model.Skill
		var files []model.SkillFile
		switch len(m) {
		case 0:
			a, err = r.Backend.CreateSkill(skillInput(d))
			if err != nil {
				return err
			}
			report(model.Change{Action: Create, Kind: "skill", Name: d.Name})
		case 1:
			a, err = r.Backend.GetSkill(m[0].ID)
			if err != nil {
				return err
			}
			files = append([]model.SkillFile(nil), a.Files...)
			fields := diffSkill(d, a)
			if hasNonFileField(fields) {
				updated, e := r.Backend.UpdateSkill(a.ID, skillInput(d))
				if e != nil {
					return e
				}
				if updated.ID != "" {
					a.ID = updated.ID
				}
			}
			report(makeChange("skill", d.Name, fields))
		default:
			return fmt.Errorf("multiple skills named %q", d.Name)
		}
		if err := r.syncSkillFiles(d, a.ID, files); err != nil {
			return err
		}
		skillIDs[d.Name] = a.ID
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
	agentIDs := map[string]string{}
	for _, d := range project.Agents {
		m := agentsNamed(remoteAgents, d.Name)
		input := agentInput(d, runtimeIDs[d.RuntimeRef])
		var a model.Agent
		var actualSkills []model.SkillSummary
		created := false
		wasArchived := false
		restoredForUpdate := false
		switch len(m) {
		case 0:
			if err := validateObservedOnlyOnCreate(d); err != nil {
				return err
			}
			a, err = r.Backend.CreateAgent(input)
			if err != nil {
				return err
			}
			created = true
			report(model.Change{Action: Create, Kind: "agent", Name: d.Name})
		case 1:
			a, err = r.Backend.GetAgent(m[0].ID)
			if err != nil {
				return err
			}
			actualSkills, err = r.Backend.ListAgentSkills(a.ID)
			if err != nil {
				return err
			}
			fields, err := r.diffAgent(d, runtimeIDs[d.RuntimeRef], a, actualSkills)
			if err != nil {
				return err
			}
			if err := validateObservedOnlyChanges(d, a, actualSkills); err != nil {
				return err
			}
			wasArchived = a.Archived()
			if wasArchived && requiresActiveAgent(fields) {
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
			base := baseAgentFields(fields)
			if len(base) > 0 {
				updated, e := r.Backend.UpdateAgent(a.ID, input)
				if e != nil {
					return e
				}
				if updated.ID != "" {
					a.ID = updated.ID
				}
			}
			report(makeChange("agent", d.Name, fields))
		default:
			return fmt.Errorf("multiple agents named %q", d.Name)
		}
		desiredEnabled := enabledSkillNames(d.SkillAssignments, d.Skills)
		if created || !equalStrings(sortedSkillNames(actualSkills, true), sortedStrings(desiredEnabled)) {
			ids := []string{}
			for _, name := range desiredEnabled {
				ids = append(ids, skillIDs[name])
			}
			if err := r.Backend.SetAgentSkills(a.ID, ids); err != nil {
				return err
			}
		}
		if d.ManageCustomEnv {
			ops, ok := r.Backend.(backend.AgentOperations)
			if !ok {
				return fmt.Errorf("backend cannot manage custom env")
			}
			actualEnv := map[string]string{}
			if !created {
				actualEnv, err = ops.GetAgentEnv(a.ID)
				if err != nil {
					return err
				}
			}
			if created || !equalStringMap(d.CustomEnv, actualEnv) {
				if err := ops.SetAgentEnv(a.ID, d.CustomEnvFile); err != nil {
					return err
				}
			}
		}
		if d.AvatarFile != "" {
			different := created || a.AvatarURL == nil || *a.AvatarURL == ""
			if !different {
				different, err = r.avatarDiffers(d.AvatarFile, *a.AvatarURL)
				if err != nil {
					return err
				}
			}
			if different {
				ops, ok := r.Backend.(backend.AgentOperations)
				if !ok {
					return fmt.Errorf("backend cannot upload avatar")
				}
				if err := ops.UploadAgentAvatar(a.ID, d.AvatarFile); err != nil {
					return err
				}
			}
		}
		desiredArchived := wasArchived
		if created {
			desiredArchived = false
		}
		if d.ManageArchived {
			desiredArchived = d.Archived
		}
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
	}
	if len(project.Squads) > 0 {
		ops, ok := r.Backend.(backend.SquadOperations)
		if !ok {
			return fmt.Errorf("backend does not support squads")
		}
		actual, err := ops.ListSquads()
		if err != nil {
			return err
		}
		for _, d := range project.Squads {
			leaderID := agentIDs[d.Leader]
			m := squadsNamed(actual, d.Name)
			var a model.Squad
			var members []model.SquadMember
			switch len(m) {
			case 0:
				a, err = ops.CreateSquad(squadInput(d, leaderID))
				if err != nil {
					return err
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
					a, err = ops.UpdateSquad(a.ID, squadInput(d, leaderID), fields)
					if err != nil {
						return err
					}
				}
				report(model.Change{Action: Create, Kind: "squad", Name: d.Name})
			case 1:
				a, err = ops.GetSquad(m[0].ID)
				if err != nil {
					return err
				}
				members, err = ops.ListSquadMembers(a.ID)
				if err != nil {
					return err
				}
				fields := diffSquad(d, a, members, agentsByIDName(agentIDs))
				base := squadBaseFields(fields)
				if len(base) > 0 {
					a, err = ops.UpdateSquad(a.ID, squadInput(d, leaderID), base)
					if err != nil {
						return err
					}
				}
				report(makeChange("squad", d.Name, fields))
			default:
				return fmt.Errorf("multiple squads named %q", d.Name)
			}
			if err := syncSquadMembers(ops, a.ID, d, agentIDs, members); err != nil {
				return err
			}
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
	if d.ManageRuntimeConfig && !equalJSON(d.RuntimeConfig, a.RuntimeConfig) {
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
	if !equalSkillAssignments(d.SkillAssignments, d.Skills, skills) {
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
	if d.ManageArchived && d.Archived != a.Archived() {
		fields = append(fields, "archived")
	}
	if d.ManageDisabledRuntimeSkills && !equalDisabled(d.DisabledRuntimeSkills, a.DisabledRuntimeSkills) {
		fields = append(fields, "disabledRuntimeSkills")
	}
	if d.ManageComposioToolkitAllowlist && !equalStrings(sortedStrings(d.ComposioToolkitAllowlist), sortedStrings(a.ComposioToolkitAllowlist)) {
		if a.ComposioToolkitAllowlistRedacted {
			return nil, fmt.Errorf("agent %q Composio allowlist is redacted", d.Name)
		}
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
	for key, m := range desired {
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
	for key, m := range actualMap {
		if _, ok := desired[key]; !ok {
			if err := ops.RemoveSquadMember(id, m); err != nil {
				return err
			}
		}
	}
	return nil
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
	if disabled || (d.ManageDisabledRuntimeSkills && len(d.DisabledRuntimeSkills) > 0) || (d.ManageComposioToolkitAllowlist && len(d.ComposioToolkitAllowlist) > 0) {
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
	if !equalSkillAssignments(d.SkillAssignments, d.Skills, skills) {
		for _, s := range d.SkillAssignments {
			if !s.Enabled {
				return fmt.Errorf("agent %q disabled skill assignments cannot be changed through the official CLI", d.Name)
			}
		}
	}
	if d.ManageDisabledRuntimeSkills && !equalDisabled(d.DisabledRuntimeSkills, a.DisabledRuntimeSkills) {
		return fmt.Errorf("agent %q disabledRuntimeSkills cannot be changed through the official CLI", d.Name)
	}
	if d.ManageComposioToolkitAllowlist && !equalStrings(sortedStrings(d.ComposioToolkitAllowlist), sortedStrings(a.ComposioToolkitAllowlist)) {
		return fmt.Errorf("agent %q composioToolkitAllowlist cannot be changed through the official CLI", d.Name)
	}
	return nil
}
func validateCreatableProfile(d model.RuntimeProfileSpec) error {
	if len(d.FixedArgs) > 0 || d.Visibility != "workspace" {
		return fmt.Errorf("runtime profile %q fixedArgs and non-workspace visibility cannot be created through the official CLI", d.DisplayName)
	}
	return nil
}
func validateProfileUpdate(fields []string, d model.RuntimeProfileSpec, a model.RuntimeProfile) error {
	for _, f := range fields {
		if f == "protocolFamily" {
			return fmt.Errorf("runtime profile %q protocolFamily is immutable", d.DisplayName)
		}
		if f == "fixedArgs" || f == "visibility" {
			return fmt.Errorf("runtime profile %q %s cannot be changed through the official CLI", d.DisplayName, f)
		}
	}
	return nil
}

func diffRuntimeProfile(d model.RuntimeProfileSpec, a model.RuntimeProfile) []string {
	fields := []string{}
	if d.ProtocolFamily != a.ProtocolFamily {
		fields = append(fields, "protocolFamily")
	}
	if d.CommandName != a.CommandName {
		fields = append(fields, "commandName")
	}
	actualDesc := ""
	if a.Description != nil {
		actualDesc = *a.Description
	}
	if d.Description != actualDesc {
		fields = append(fields, "description")
	}
	if d.Enabled != a.Enabled {
		fields = append(fields, "enabled")
	}
	if !equalStrings(d.FixedArgs, a.FixedArgs) {
		fields = append(fields, "fixedArgs")
	}
	if d.Visibility != a.Visibility {
		fields = append(fields, "visibility")
	}
	return fields
}
func mutableProfileFields(fields []string) []string {
	out := []string{}
	for _, f := range fields {
		if f == "commandName" || f == "description" || f == "enabled" || f == "displayName" {
			out = append(out, f)
		}
	}
	return out
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
		if d.Permission == "workspace" {
			mode = "public_to"
		} else {
			mode = "private"
		}
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
		if t.TargetID != nil {
			id = *t.TargetID
		}
		out = append(out, t.TargetType+":"+id)
	}
	sort.Strings(out)
	return out
}
func equalSkillAssignments(d []model.AgentSkillSpec, legacy []string, a []model.SkillSummary) bool {
	if len(d) == 0 {
		d = make([]model.AgentSkillSpec, len(legacy))
		for i, n := range legacy {
			d[i] = model.AgentSkillSpec{Name: n, Enabled: true}
		}
	}
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
func enabledSkillNames(assignments []model.AgentSkillSpec, legacy []string) []string {
	if len(assignments) == 0 {
		return append([]string(nil), legacy...)
	}
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
	return strings.Join([]string{v.RuntimeID, v.Provider, v.Root, v.Key, v.Name, v.Plugin}, "|")
}
func equalJSON(a, b any) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return equalRawJSON(aa, bb)
}
func equalRawJSON(a, b []byte) bool {
	var av, bv any
	if len(bytes.TrimSpace(a)) == 0 {
		a = []byte("null")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		b = []byte("null")
	}
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	aa, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return bytes.Equal(aa, bb)
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
	for alias, s := range selectors {
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
		ManageRuntimeConfig: v.ManageRuntimeConfig, RuntimeConfig: v.RuntimeConfig,
		Model: v.ModelID, ThinkingLevel: v.ThinkingLevel, CustomArgs: append([]string(nil), v.CustomArgs...),
		Permission: v.Permission, PermissionMode: v.PermissionMode,
		InvocationTargets:  append([]model.InvocationTarget(nil), v.InvocationTargets...),
		MaxConcurrentTasks: v.MaxConcurrentTasks, ManageMCPConfig: v.ManageMCPConfig, MCPConfigFile: v.MCPConfigFile,
	}
}
func squadInput(v model.SquadSpec, leaderID string) model.SquadInput {
	return model.SquadInput{Name: v.Name, Description: v.Description, Instructions: v.Instructions, LeaderID: leaderID, AvatarURL: v.AvatarURL}
}
func profileInput(v model.RuntimeProfileSpec) model.RuntimeProfileInput {
	return model.RuntimeProfileInput{DisplayName: v.DisplayName, ProtocolFamily: v.ProtocolFamily, CommandName: v.CommandName, Description: v.Description, Enabled: v.Enabled, FixedArgs: v.FixedArgs, Visibility: v.Visibility}
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
func profilesNamed(items []model.RuntimeProfile, name string) []model.RuntimeProfile {
	out := []model.RuntimeProfile{}
	for _, v := range items {
		if v.DisplayName == name {
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
func agentsByIDName(ids map[string]string) map[string]model.Agent {
	out := map[string]model.Agent{}
	for name, id := range ids {
		out[name] = model.Agent{ID: id, Name: name}
	}
	return out
}
