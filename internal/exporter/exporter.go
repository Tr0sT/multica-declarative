package exporter

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

const (
	defaultOutputDir = "multica-export"
	apiVersion       = "multica-declarative/v1alpha1"
)

var generatedPaths = []string{"multica.yaml", "agents", "skills", "squads", "runtime-profiles"}

type Options struct {
	OutputDir string
	Force     bool
}
type Result struct {
	OutputDir                                         string
	Skills, Agents, Runtimes, Squads, RuntimeProfiles int
	Warnings                                          []string
}
type Exporter struct {
	Backend    backend.Backend
	HTTPClient *http.Client
}
type snapshot struct {
	manifest workspaceDocument
	skills   []exportedSkill
	agents   []exportedAgent
	squads   []exportedSquad
	profiles []exportedProfile
	warnings []string
}
type exportedSkill struct {
	directory, content string
	files              []model.SkillFile
}
type exportedAgent struct {
	directory, instructions string
	document                agentDocument
	avatarName              string
	avatar                  []byte
}
type exportedSquad struct {
	directory, instructions string
	document                squadDocument
}
type exportedProfile struct {
	directory string
	document  runtimeProfileDocument
}

type workspaceDocument struct {
	APIVersion      string                     `yaml:"apiVersion"`
	Kind            string                     `yaml:"kind"`
	Skills          []string                   `yaml:"skills,omitempty"`
	Agents          []string                   `yaml:"agents,omitempty"`
	Squads          []string                   `yaml:"squads,omitempty"`
	RuntimeProfiles []string                   `yaml:"runtimeProfiles,omitempty"`
	Runtimes        map[string]runtimeDocument `yaml:"runtimes,omitempty"`
}
type runtimeDocument struct {
	ID         string `yaml:"id,omitempty"`
	Name       string `yaml:"name,omitempty"`
	CustomName string `yaml:"customName,omitempty"`
	Provider   string `yaml:"provider,omitempty"`
}
type modelDocument struct {
	ID string `yaml:"id"`
}
type skillAssignmentDocument struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
}
type permissionDocument struct {
	Mode      string   `yaml:"mode"`
	Workspace bool     `yaml:"workspace,omitempty"`
	Members   []string `yaml:"members,omitempty"`
}
type agentDocument struct {
	Name             string          `yaml:"name"`
	Description      string          `yaml:"description,omitempty"`
	InstructionsFile string          `yaml:"instructionsFile"`
	Model            *modelDocument  `yaml:"model,omitempty"`
	Skills           []any           `yaml:"skills,omitempty"`
	Multica          multicaDocument `yaml:"multica"`
}
type multicaDocument struct {
	Runtime                  string                       `yaml:"runtime"`
	RuntimeConfig            map[string]any               `yaml:"runtimeConfig,omitempty"`
	ThinkingLevel            string                       `yaml:"thinkingLevel,omitempty"`
	MaxConcurrentTasks       int                          `yaml:"maxConcurrentTasks"`
	Permission               any                          `yaml:"permission"`
	CustomArgs               []string                     `yaml:"customArgs,omitempty"`
	AvatarFile               string                       `yaml:"avatarFile,omitempty"`
	Archived                 bool                         `yaml:"archived,omitempty"`
	DisabledRuntimeSkills    []model.DisabledRuntimeSkill `yaml:"disabledRuntimeSkills,omitempty"`
	ComposioToolkitAllowlist []string                     `yaml:"composioToolkitAllowlist,omitempty"`
}
type squadDocument struct {
	Kind             string                `yaml:"kind"`
	Name             string                `yaml:"name"`
	Description      string                `yaml:"description,omitempty"`
	InstructionsFile string                `yaml:"instructionsFile,omitempty"`
	Leader           string                `yaml:"leader"`
	AvatarURL        string                `yaml:"avatarUrl,omitempty"`
	Members          []squadMemberDocument `yaml:"members,omitempty"`
}
type squadMemberDocument struct {
	Type  string `yaml:"type"`
	Agent string `yaml:"agent,omitempty"`
	ID    string `yaml:"id,omitempty"`
	Role  string `yaml:"role"`
}
type runtimeProfileDocument struct {
	Kind           string   `yaml:"kind"`
	DisplayName    string   `yaml:"displayName"`
	ProtocolFamily string   `yaml:"protocolFamily"`
	CommandName    string   `yaml:"commandName"`
	Description    string   `yaml:"description,omitempty"`
	Enabled        bool     `yaml:"enabled"`
	FixedArgs      []string `yaml:"fixedArgs,omitempty"`
	Visibility     string   `yaml:"visibility"`
}

func (e Exporter) Export(options Options) (Result, error) {
	if e.Backend == nil {
		return Result{}, fmt.Errorf("export backend is required")
	}
	out := strings.TrimSpace(options.OutputDir)
	if out == "" {
		out = defaultOutputDir
	}
	absolute, err := filepath.Abs(out)
	if err != nil {
		return Result{}, err
	}
	absolute = filepath.Clean(absolute)
	if err := validateTarget(absolute, options.Force); err != nil {
		return Result{}, err
	}
	snap, err := e.readSnapshot()
	if err != nil {
		return Result{}, err
	}
	if len(snap.skills)+len(snap.agents)+len(snap.squads)+len(snap.profiles) == 0 {
		return Result{}, fmt.Errorf("Multica workspace contains no exportable resources")
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return Result{}, err
	}
	staging, err := os.MkdirTemp(parent, ".multica-export-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(staging)
	if err := writeSnapshot(staging, snap); err != nil {
		return Result{}, err
	}
	if err := installSnapshot(staging, absolute, options.Force); err != nil {
		return Result{}, err
	}
	return Result{OutputDir: absolute, Skills: len(snap.skills), Agents: len(snap.agents), Runtimes: len(snap.manifest.Runtimes), Squads: len(snap.squads), RuntimeProfiles: len(snap.profiles), Warnings: snap.warnings}, nil
}

func (e Exporter) readSnapshot() (snapshot, error) {
	skillSummaries, err := e.Backend.ListSkills()
	if err != nil {
		return snapshot{}, err
	}
	agentSummaries, err := e.Backend.ListAgents()
	if err != nil {
		return snapshot{}, err
	}
	runtimes, err := e.Backend.ListRuntimes()
	if err != nil {
		return snapshot{}, err
	}
	sort.Slice(skillSummaries, func(i, j int) bool { return skillSummaries[i].Name < skillSummaries[j].Name })
	sort.Slice(agentSummaries, func(i, j int) bool { return agentSummaries[i].Name < agentSummaries[j].Name })
	if d := duplicateName(skillSummaries, func(v model.Skill) string { return v.Name }); d != "" {
		return snapshot{}, fmt.Errorf("duplicate skill %q", d)
	}
	if d := duplicateName(agentSummaries, func(v model.Agent) string { return v.Name }); d != "" {
		return snapshot{}, fmt.Errorf("duplicate agent %q", d)
	}
	warnings := []string{}
	skillNames := map[string]struct{}{}
	usedSkills := map[string]struct{}{}
	skills := []exportedSkill{}
	for _, s := range skillSummaries {
		v, err := e.Backend.GetSkill(s.ID)
		if err != nil {
			return snapshot{}, err
		}
		content, changed, err := normalizeSkillContent(v)
		if err != nil {
			return snapshot{}, err
		}
		if changed {
			warnings = append(warnings, fmt.Sprintf("skill %q frontmatter was normalized", v.Name))
		}
		files, err := validateSkillFiles(v.Name, v.Files)
		if err != nil {
			return snapshot{}, err
		}
		dir := uniqueSlug(v.Name, v.ID, usedSkills)
		skills = append(skills, exportedSkill{directory: dir, content: content, files: files})
		skillNames[v.Name] = struct{}{}
	}
	detailed := []model.Agent{}
	agentSkills := map[string][]model.SkillSummary{}
	for _, s := range agentSummaries {
		a, err := e.Backend.GetAgent(s.ID)
		if err != nil {
			return snapshot{}, err
		}
		if a.Name == "" {
			a.Name = s.Name
		}
		assigned, err := e.Backend.ListAgentSkills(a.ID)
		if err != nil {
			return snapshot{}, err
		}
		for _, sk := range assigned {
			if _, ok := skillNames[sk.Name]; !ok {
				return snapshot{}, fmt.Errorf("agent %q references unknown skill %q", a.Name, sk.Name)
			}
		}
		detailed = append(detailed, a)
		agentSkills[a.ID] = assigned
		if a.HasCustomEnv || a.CustomEnvKeyCount > 0 {
			warnings = append(warnings, fmt.Sprintf("agent %q custom environment is intentionally not exported; add customEnvFile manually", a.Name))
		}
		if len(a.MCPConfig) > 0 && string(a.MCPConfig) != "null" {
			warnings = append(warnings, fmt.Sprintf("agent %q MCP config is intentionally not exported; add mcpConfigFile manually", a.Name))
		}
	}
	aliases, runtimeDocs, err := makeRuntimeDocuments(runtimes, detailed)
	if err != nil {
		return snapshot{}, err
	}
	agentNameByID := map[string]string{}
	for _, a := range detailed {
		agentNameByID[a.ID] = a.Name
	}
	usedAgents := map[string]struct{}{}
	agents := []exportedAgent{}
	for _, a := range detailed {
		permission, err := permissionForExport(a)
		if err != nil {
			return snapshot{}, fmt.Errorf("agent %q: %w", a.Name, err)
		}
		assignments := []any{}
		for _, s := range agentSkills[a.ID] {
			enabled := true
			if s.Enabled != nil {
				enabled = *s.Enabled
			}
			if enabled {
				assignments = append(assignments, s.Name)
			} else {
				assignments = append(assignments, skillAssignmentDocument{Name: s.Name, Enabled: false})
			}
		}
		var md *modelDocument
		if a.Model != "" {
			md = &modelDocument{ID: a.Model}
		}
		dir := uniqueSlug(a.Name, a.ID, usedAgents)
		ea := exportedAgent{directory: dir, instructions: a.Instructions, document: agentDocument{Name: a.Name, Description: a.Description, InstructionsFile: "AGENT.md", Model: md, Skills: assignments, Multica: multicaDocument{Runtime: aliases[a.RuntimeID], RuntimeConfig: a.RuntimeConfig, ThinkingLevel: a.ThinkingLevel, MaxConcurrentTasks: normalizedConcurrency(a.MaxConcurrentTasks), Permission: permission, CustomArgs: append([]string(nil), a.CustomArgs...), Archived: a.Archived(), DisabledRuntimeSkills: append([]model.DisabledRuntimeSkill(nil), a.DisabledRuntimeSkills...), ComposioToolkitAllowlist: append([]string(nil), a.ComposioToolkitAllowlist...)}}}
		if a.AvatarURL != nil && *a.AvatarURL != "" {
			data, name, downloadErr := e.downloadAvatar(*a.AvatarURL)
			if downloadErr != nil {
				warnings = append(warnings, fmt.Sprintf("agent %q avatar was not exported: %v", a.Name, downloadErr))
			} else {
				ea.avatar = data
				ea.avatarName = name
				ea.document.Multica.AvatarFile = name
			}
		}
		agents = append(agents, ea)
	}
	profiles := []exportedProfile{}
	if ops, ok := e.Backend.(backend.RuntimeProfileOperations); ok {
		items, err := ops.ListRuntimeProfiles()
		if err != nil {
			return snapshot{}, err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].DisplayName < items[j].DisplayName })
		used := map[string]struct{}{}
		for _, v := range items {
			desc := ""
			if v.Description != nil {
				desc = *v.Description
			}
			dir := uniqueSlug(v.DisplayName, v.ID, used)
			profiles = append(profiles, exportedProfile{directory: dir, document: runtimeProfileDocument{Kind: "RuntimeProfile", DisplayName: v.DisplayName, ProtocolFamily: v.ProtocolFamily, CommandName: v.CommandName, Description: desc, Enabled: v.Enabled, FixedArgs: append([]string(nil), v.FixedArgs...), Visibility: defaultString(v.Visibility, "workspace")}})
		}
	}
	squads := []exportedSquad{}
	if ops, ok := e.Backend.(backend.SquadOperations); ok {
		items, err := ops.ListSquads()
		if err != nil {
			return snapshot{}, err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		used := map[string]struct{}{}
		for _, summary := range items {
			s, err := ops.GetSquad(summary.ID)
			if err != nil {
				return snapshot{}, err
			}
			leader := agentNameByID[s.LeaderID]
			if leader == "" {
				return snapshot{}, fmt.Errorf("squad %q leader %q is not an exported agent", s.Name, s.LeaderID)
			}
			members, err := ops.ListSquadMembers(s.ID)
			if err != nil {
				return snapshot{}, err
			}
			docs := []squadMemberDocument{}
			for _, m := range members {
				if m.MemberType == "agent" && m.MemberID == s.LeaderID {
					continue
				}
				d := squadMemberDocument{Type: m.MemberType, Role: m.Role}
				if m.MemberType == "agent" {
					d.Agent = agentNameByID[m.MemberID]
					if d.Agent == "" {
						return snapshot{}, fmt.Errorf("squad %q member agent %q is not exported", s.Name, m.MemberID)
					}
				} else {
					d.ID = m.MemberID
				}
				docs = append(docs, d)
			}
			sort.Slice(docs, func(i, j int) bool {
				return docs[i].Type+docs[i].Agent+docs[i].ID < docs[j].Type+docs[j].Agent+docs[j].ID
			})
			dir := uniqueSlug(s.Name, s.ID, used)
			avatar := ""
			if s.AvatarURL != nil {
				avatar = *s.AvatarURL
			}
			sq := exportedSquad{directory: dir, instructions: s.Instructions, document: squadDocument{Kind: "Squad", Name: s.Name, Description: s.Description, Leader: leader, AvatarURL: avatar, Members: docs}}
			if s.Instructions != "" {
				sq.document.InstructionsFile = "SQUAD.md"
			}
			squads = append(squads, sq)
		}
	}
	manifest := workspaceDocument{APIVersion: apiVersion, Kind: "Workspace", Runtimes: runtimeDocs}
	for _, v := range skills {
		manifest.Skills = append(manifest.Skills, path.Join("skills", v.directory))
	}
	for _, v := range agents {
		manifest.Agents = append(manifest.Agents, path.Join("agents", v.directory, "agent.yaml"))
	}
	for _, v := range squads {
		manifest.Squads = append(manifest.Squads, path.Join("squads", v.directory, "squad.yaml"))
	}
	for _, v := range profiles {
		manifest.RuntimeProfiles = append(manifest.RuntimeProfiles, path.Join("runtime-profiles", v.directory+".yaml"))
	}
	return snapshot{manifest: manifest, skills: skills, agents: agents, squads: squads, profiles: profiles, warnings: warnings}, nil
}

func (e Exporter) downloadAvatar(url string) ([]byte, string, error) {
	client := e.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > 5<<20 {
		return nil, "", fmt.Errorf("avatar exceeds 5 MB")
	}
	ext := avatarExtension(resp.Header.Get("Content-Type"), url)
	return data, "avatar" + ext, nil
}
func avatarExtension(contentType, url string) string {
	switch strings.Split(contentType, ";")[0] {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	ext := strings.ToLower(filepath.Ext(strings.Split(url, "?")[0]))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return ext
	}
	return ".png"
}
