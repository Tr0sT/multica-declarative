package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Tr0sT/multica-declarative/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	apiVersion    = "multica-declarative/v1alpha1"
	workspaceKind = "Workspace"
	squadKind     = "Squad"
)

type workspaceDocument struct {
	APIVersion string                     `yaml:"apiVersion"`
	Kind       string                     `yaml:"kind"`
	Skills     []string                   `yaml:"skills"`
	Agents     []string                   `yaml:"agents"`
	Squads     []string                   `yaml:"squads"`
	Runtimes   map[string]runtimeDocument `yaml:"runtimes"`
}

type runtimeDocument struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	CustomName string `yaml:"customName"`
	Provider   string `yaml:"provider"`
}
type modelDoc struct {
	ID string `yaml:"id"`
}

type skillAssignmentDocument struct {
	Name    string `yaml:"name"`
	Enabled *bool  `yaml:"enabled"`
}
type skillAssignmentsDocument []skillAssignmentDocument

func (s *skillAssignmentsDocument) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("skills must be a list")
	}
	for _, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			*s = append(*s, skillAssignmentDocument{Name: item.Value})
		case yaml.MappingNode:
			if err := requireMappingKeys(item, "name", "enabled"); err != nil {
				return err
			}
			var v skillAssignmentDocument
			if err := item.Decode(&v); err != nil {
				return err
			}
			*s = append(*s, v)
		default:
			return fmt.Errorf("skills entries must be names or mappings")
		}
	}
	return nil
}

type permissionDocument struct {
	Mode      string   `yaml:"mode"`
	Workspace bool     `yaml:"workspace"`
	Members   []string `yaml:"members"`
	Teams     []string `yaml:"teams"`
}

func (p *permissionDocument) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		switch strings.TrimSpace(node.Value) {
		case "", "private":
			p.Mode = "private"
		case "workspace":
			p.Mode = "public_to"
			p.Workspace = true
		default:
			return fmt.Errorf("permission must be private, workspace, or a mapping")
		}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("permission must be a scalar or mapping")
	}
	if err := requireMappingKeys(node, "mode", "workspace", "members", "teams"); err != nil {
		return err
	}
	type plain permissionDocument
	var v plain
	if err := node.Decode(&v); err != nil {
		return err
	}
	*p = permissionDocument(v)
	return nil
}

type agentDocument struct {
	Name             string                   `yaml:"name"`
	Description      string                   `yaml:"description"`
	Instructions     string                   `yaml:"instructions"`
	InstructionsFile string                   `yaml:"instructionsFile"`
	Model            modelDoc                 `yaml:"model"`
	Skills           skillAssignmentsDocument `yaml:"skills"`
	Multica          agentMulticaDocument     `yaml:"multica"`
}
type agentMulticaDocument struct {
	Runtime                  string                       `yaml:"runtime"`
	RuntimeConfig            map[string]any               `yaml:"runtimeConfig"`
	ThinkingLevel            string                       `yaml:"thinkingLevel"`
	MaxConcurrentTasks       int                          `yaml:"maxConcurrentTasks"`
	Permission               permissionDocument           `yaml:"permission"`
	CustomArgs               []string                     `yaml:"customArgs"`
	CustomEnvFile            string                       `yaml:"customEnvFile"`
	MCPConfigFile            string                       `yaml:"mcpConfigFile"`
	AvatarFile               string                       `yaml:"avatarFile"`
	Archived                 *bool                        `yaml:"archived"`
	DisabledRuntimeSkills    []model.DisabledRuntimeSkill `yaml:"disabledRuntimeSkills"`
	ComposioToolkitAllowlist []string                     `yaml:"composioToolkitAllowlist"`
}

type squadDocument struct {
	Kind             string                `yaml:"kind"`
	Name             string                `yaml:"name"`
	Description      string                `yaml:"description"`
	Instructions     string                `yaml:"instructions"`
	InstructionsFile string                `yaml:"instructionsFile"`
	Leader           string                `yaml:"leader"`
	AvatarURL        string                `yaml:"avatarUrl"`
	Members          []squadMemberDocument `yaml:"members"`
}
type squadMemberDocument struct {
	Type  string `yaml:"type"`
	Agent string `yaml:"agent"`
	ID    string `yaml:"id"`
	Role  string `yaml:"role"`
}
type skillFrontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Metadata    map[string]any `yaml:"metadata"`
}

func Load(workspacePath string) (model.Project, error) {
	absolute, err := filepath.Abs(workspacePath)
	if err != nil {
		return model.Project{}, fmt.Errorf("resolve workspace manifest: %w", err)
	}
	absolute = filepath.Clean(absolute)
	var doc workspaceDocument
	if err := decodeStrictYAML(absolute, &doc); err != nil {
		return model.Project{}, err
	}
	if doc.APIVersion != apiVersion {
		return model.Project{}, fmt.Errorf("unsupported apiVersion %q; expected %q", doc.APIVersion, apiVersion)
	}
	if doc.Kind != workspaceKind {
		return model.Project{}, fmt.Errorf("unsupported kind %q; expected %q", doc.Kind, workspaceKind)
	}
	base := filepath.Dir(absolute)
	project := model.Project{WorkspacePath: absolute, RuntimeSelectors: map[string]model.RuntimeSelector{}}
	for alias, raw := range doc.Runtimes {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			return project, fmt.Errorf("runtime aliases must be non-empty strings")
		}
		v := model.RuntimeSelector{ID: strings.TrimSpace(raw.ID), Name: strings.TrimSpace(raw.Name), CustomName: strings.TrimSpace(raw.CustomName), Provider: strings.TrimSpace(raw.Provider)}
		if v.ID == "" && v.Name == "" && v.CustomName == "" {
			return project, fmt.Errorf("runtime %q must specify id, name, or customName", alias)
		}
		project.RuntimeSelectors[alias] = v
	}
	for _, item := range doc.Skills {
		p, err := resolvePath(base, item)
		if err != nil {
			return project, err
		}
		v, err := loadSkill(p)
		if err != nil {
			return project, err
		}
		project.Skills = append(project.Skills, v)
	}
	for _, item := range doc.Agents {
		p, err := resolvePath(base, item)
		if err != nil {
			return project, err
		}
		v, err := loadAgent(p)
		if err != nil {
			return project, err
		}
		project.Agents = append(project.Agents, v)
	}
	for _, item := range doc.Squads {
		p, err := resolvePath(base, item)
		if err != nil {
			return project, err
		}
		v, err := loadSquad(p)
		if err != nil {
			return project, err
		}
		project.Squads = append(project.Squads, v)
	}
	if err := validate(project); err != nil {
		return model.Project{}, err
	}
	return project, nil
}

func loadAgent(path string) (model.AgentSpec, error) {
	var d agentDocument
	if err := decodeStrictYAML(path, &d); err != nil {
		return model.AgentSpec{}, err
	}
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return model.AgentSpec{}, fmt.Errorf("%s: name is required", path)
	}
	instructions, err := loadTextChoice(path, d.Instructions, d.InstructionsFile, "instructions")
	if err != nil {
		return model.AgentSpec{}, err
	}
	runtime := strings.TrimSpace(d.Multica.Runtime)
	if runtime == "" {
		return model.AgentSpec{}, fmt.Errorf("%s: multica.runtime is required", path)
	}
	max := d.Multica.MaxConcurrentTasks
	if max == 0 {
		max = 1
	}
	if max < 1 {
		return model.AgentSpec{}, fmt.Errorf("%s: maxConcurrentTasks must be at least 1", path)
	}
	customArgs, err := normalizeStringList(d.Multica.CustomArgs, path+": customArgs")
	if err != nil {
		return model.AgentSpec{}, err
	}
	permissionMode, targets, legacy, err := normalizePermission(d.Multica.Permission, path)
	if err != nil {
		return model.AgentSpec{}, err
	}
	assignments := make([]model.AgentSkillSpec, 0, len(d.Skills))
	skills := make([]string, 0, len(d.Skills))
	seenSkills := map[string]struct{}{}
	for _, raw := range d.Skills {
		name := strings.TrimSpace(raw.Name)
		if name == "" {
			return model.AgentSpec{}, fmt.Errorf("%s: skill name is required", path)
		}
		if _, exists := seenSkills[name]; exists {
			return model.AgentSpec{}, fmt.Errorf("%s: duplicate skill assignment %q", path, name)
		}
		seenSkills[name] = struct{}{}
		enabled := true
		if raw.Enabled != nil {
			enabled = *raw.Enabled
		}
		assignments = append(assignments, model.AgentSkillSpec{Name: name, Enabled: enabled})
		if enabled {
			skills = append(skills, name)
		}
	}
	var customEnv map[string]string
	var customEnvFile string
	manageEnv := strings.TrimSpace(d.Multica.CustomEnvFile) != ""
	if manageEnv {
		customEnvFile, err = resolvePath(filepath.Dir(path), d.Multica.CustomEnvFile)
		if err != nil {
			return model.AgentSpec{}, err
		}
		if err := readJSON(customEnvFile, &customEnv); err != nil {
			return model.AgentSpec{}, fmt.Errorf("%s: customEnvFile: %w", path, err)
		}
	}
	var mcp json.RawMessage
	var mcpFile string
	manageMCP := strings.TrimSpace(d.Multica.MCPConfigFile) != ""
	if manageMCP {
		mcpFile, err = resolvePath(filepath.Dir(path), d.Multica.MCPConfigFile)
		if err != nil {
			return model.AgentSpec{}, err
		}
		data, readErr := os.ReadFile(mcpFile)
		if readErr != nil {
			return model.AgentSpec{}, readErr
		}
		if !json.Valid(data) {
			return model.AgentSpec{}, fmt.Errorf("%s: mcpConfigFile must contain valid JSON", path)
		}
		mcp = append([]byte(nil), data...)
	}
	avatarFile := ""
	if strings.TrimSpace(d.Multica.AvatarFile) != "" {
		avatarFile, err = resolvePath(filepath.Dir(path), d.Multica.AvatarFile)
		if err != nil {
			return model.AgentSpec{}, err
		}
		info, statErr := os.Stat(avatarFile)
		if statErr != nil || !info.Mode().IsRegular() {
			return model.AgentSpec{}, fmt.Errorf("%s: avatarFile must be a regular file", path)
		}
	}
	manageRuntimeConfig := d.Multica.RuntimeConfig != nil
	runtimeConfig := d.Multica.RuntimeConfig
	if runtimeConfig == nil {
		runtimeConfig = map[string]any{}
	}
	disabled := append([]model.DisabledRuntimeSkill(nil), d.Multica.DisabledRuntimeSkills...)
	for i := range disabled {
		disabled[i].RuntimeID = strings.TrimSpace(disabled[i].RuntimeID)
		disabled[i].Provider = strings.TrimSpace(disabled[i].Provider)
		disabled[i].Root = strings.TrimSpace(disabled[i].Root)
		disabled[i].Key = strings.TrimSpace(disabled[i].Key)
		if disabled[i].Root == "" || disabled[i].Key == "" {
			return model.AgentSpec{}, fmt.Errorf("%s: disabledRuntimeSkills entries require root and key", path)
		}
	}
	allowlist, err := normalizeStringList(d.Multica.ComposioToolkitAllowlist, path+": composioToolkitAllowlist")
	if err != nil {
		return model.AgentSpec{}, err
	}
	archived := false
	manageArchived := d.Multica.Archived != nil
	if manageArchived {
		archived = *d.Multica.Archived
	}
	return model.AgentSpec{
		Name: name, Description: strings.TrimSpace(d.Description), Instructions: instructions,
		ModelID: strings.TrimSpace(d.Model.ID), Skills: skills, SkillAssignments: assignments,
		RuntimeRef: runtime, ManageRuntimeConfig: manageRuntimeConfig, RuntimeConfig: runtimeConfig,
		ThinkingLevel: strings.TrimSpace(d.Multica.ThinkingLevel), MaxConcurrentTasks: max,
		Permission: legacy, PermissionMode: permissionMode, InvocationTargets: targets,
		CustomArgs: customArgs, ManageCustomEnv: manageEnv, CustomEnv: customEnv,
		CustomEnvFile: customEnvFile, ManageMCPConfig: manageMCP, MCPConfig: mcp,
		MCPConfigFile: mcpFile, AvatarFile: avatarFile, ManageArchived: manageArchived,
		Archived: archived, ManageDisabledRuntimeSkills: d.Multica.DisabledRuntimeSkills != nil,
		DisabledRuntimeSkills:          disabled,
		ManageComposioToolkitAllowlist: d.Multica.ComposioToolkitAllowlist != nil,
		ComposioToolkitAllowlist:       allowlist, SourcePath: path,
	}, nil
}

func loadSquad(path string) (model.SquadSpec, error) {
	var d squadDocument
	if err := decodeStrictYAML(path, &d); err != nil {
		return model.SquadSpec{}, err
	}
	kind := strings.TrimSpace(d.Kind)
	if kind == "" {
		kind = squadKind
	}
	if kind != squadKind {
		return model.SquadSpec{}, fmt.Errorf("%s: unsupported squad kind %q", path, kind)
	}
	name := strings.TrimSpace(d.Name)
	leader := strings.TrimSpace(d.Leader)
	if name == "" || leader == "" {
		return model.SquadSpec{}, fmt.Errorf("%s: name and leader are required", path)
	}
	instructions, err := loadTextChoice(path, d.Instructions, d.InstructionsFile, "instructions")
	if err != nil {
		return model.SquadSpec{}, err
	}
	members := make([]model.SquadMemberSpec, 0, len(d.Members))
	seenMembers := map[string]struct{}{}
	for _, m := range d.Members {
		typ := strings.TrimSpace(m.Type)
		if typ == "" {
			typ = "agent"
		}
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "member"
		}
		v := model.SquadMemberSpec{Type: typ, Agent: strings.TrimSpace(m.Agent), ID: strings.TrimSpace(m.ID), Role: role}
		if typ == "agent" {
			if v.Agent == "" || v.ID != "" {
				return model.SquadSpec{}, fmt.Errorf("%s: agent squad members require agent and must not set id", path)
			}
		}
		if typ == "member" {
			if v.ID == "" || v.Agent != "" {
				return model.SquadSpec{}, fmt.Errorf("%s: human squad members require id and must not set agent", path)
			}
		}
		if typ != "agent" && typ != "member" {
			return model.SquadSpec{}, fmt.Errorf("%s: member type must be agent or member", path)
		}
		memberKey := typ + ":" + v.Agent + v.ID
		if _, exists := seenMembers[memberKey]; exists {
			return model.SquadSpec{}, fmt.Errorf("%s: duplicate squad member %q", path, memberKey)
		}
		seenMembers[memberKey] = struct{}{}
		members = append(members, v)
	}
	return model.SquadSpec{Name: name, Description: strings.TrimSpace(d.Description), Instructions: instructions, Leader: leader, AvatarURL: strings.TrimSpace(d.AvatarURL), Members: members, SourcePath: path, InstructionsFile: d.InstructionsFile}, nil
}

func loadSkill(directory string) (model.SkillSpec, error) {
	contentPath := filepath.Join(directory, "SKILL.md")
	data, err := os.ReadFile(contentPath)
	if err != nil {
		return model.SkillSpec{}, fmt.Errorf("read %s: %w", contentPath, err)
	}
	if !utf8.Valid(data) {
		return model.SkillSpec{}, fmt.Errorf("%s must be UTF-8", contentPath)
	}
	fm, err := parseFrontmatter(data, contentPath)
	if err != nil {
		return model.SkillSpec{}, err
	}
	fm.Name = strings.TrimSpace(fm.Name)
	fm.Description = strings.TrimSpace(fm.Description)
	if fm.Name == "" || fm.Description == "" {
		return model.SkillSpec{}, fmt.Errorf("%s: frontmatter name and description are required", contentPath)
	}
	skill := model.SkillSpec{Name: fm.Name, Description: fm.Description, Content: string(data), SourceDir: directory, ContentPath: contentPath}
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == directory || path == contentPath || entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported non-regular skill file: %s", path)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if len(b) == 0 || !utf8.Valid(b) {
			return fmt.Errorf("skill file must be non-empty UTF-8: %s", path)
		}
		rel, e := filepath.Rel(directory, path)
		if e != nil {
			return e
		}
		skill.Files = append(skill.Files, model.SkillFileSpec{Path: filepath.ToSlash(rel), SourcePath: path, Content: string(b)})
		return nil
	})
	if err != nil {
		return model.SkillSpec{}, err
	}
	sort.Slice(skill.Files, func(i, j int) bool { return skill.Files[i].Path < skill.Files[j].Path })
	return skill, nil
}

func validate(p model.Project) error {
	if len(p.Skills)+len(p.Agents)+len(p.Squads) == 0 {
		return fmt.Errorf("workspace must declare at least one resource")
	}
	skills := map[string]struct{}{}
	for _, v := range p.Skills {
		if _, ok := skills[v.Name]; ok {
			return fmt.Errorf("duplicate skill name %q", v.Name)
		}
		skills[v.Name] = struct{}{}
	}
	agents := map[string]struct{}{}
	for _, v := range p.Agents {
		if _, ok := agents[v.Name]; ok {
			return fmt.Errorf("duplicate agent name %q", v.Name)
		}
		agents[v.Name] = struct{}{}
		if _, ok := p.RuntimeSelectors[v.RuntimeRef]; !ok {
			return fmt.Errorf("agent %q references unknown runtime %q", v.Name, v.RuntimeRef)
		}
		for _, s := range v.SkillAssignments {
			if _, ok := skills[s.Name]; !ok {
				return fmt.Errorf("agent %q references undeclared skill %q", v.Name, s.Name)
			}
		}
	}
	squads := map[string]struct{}{}
	for _, v := range p.Squads {
		if _, ok := squads[v.Name]; ok {
			return fmt.Errorf("duplicate squad name %q", v.Name)
		}
		squads[v.Name] = struct{}{}
		if _, ok := agents[v.Leader]; !ok {
			return fmt.Errorf("squad %q references undeclared leader agent %q", v.Name, v.Leader)
		}
		for _, m := range v.Members {
			if m.Type == "agent" {
				if _, ok := agents[m.Agent]; !ok {
					return fmt.Errorf("squad %q references undeclared agent %q", v.Name, m.Agent)
				}
			}
		}
	}
	return nil
}

func normalizePermission(p permissionDocument, path string) (string, []model.InvocationTarget, string, error) {
	mode := strings.TrimSpace(p.Mode)
	if mode == "" {
		mode = "private"
	}
	if len(p.Teams) > 0 {
		return "", nil, "", fmt.Errorf("%s: team invocation targets are not supported by the Multica CLI", path)
	}
	members, err := normalizeStringList(p.Members, path+": permission.members")
	if err != nil {
		return "", nil, "", err
	}
	if duplicate := duplicateString(members); duplicate != "" {
		return "", nil, "", fmt.Errorf("%s: duplicate permission member %q", path, duplicate)
	}
	if mode == "private" {
		if p.Workspace || len(members) > 0 {
			return "", nil, "", fmt.Errorf("%s: private permission cannot have targets", path)
		}
		return "private", nil, "private", nil
	}
	if mode != "public_to" {
		return "", nil, "", fmt.Errorf("%s: permission.mode must be private or public_to", path)
	}
	targets := []model.InvocationTarget{}
	if p.Workspace {
		targets = append(targets, model.InvocationTarget{TargetType: "workspace"})
	}
	for _, id := range members {
		idCopy := id
		targets = append(targets, model.InvocationTarget{TargetType: "member", TargetID: &idCopy})
	}
	if len(targets) == 0 {
		return "", nil, "", fmt.Errorf("%s: public_to permission needs workspace or members", path)
	}
	legacy := ""
	if p.Workspace && len(members) == 0 {
		legacy = "workspace"
	}
	return mode, targets, legacy, nil
}

func loadTextChoice(path, inline, file, label string) (string, error) {
	if inline != "" && strings.TrimSpace(file) != "" {
		return "", fmt.Errorf("%s: %s and %sFile are mutually exclusive", path, label, label)
	}
	if strings.TrimSpace(file) == "" {
		return inline, nil
	}
	p, err := resolvePath(filepath.Dir(path), file)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("%s must be UTF-8", p)
	}
	return string(b), nil
}
func readJSON(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}
func parseFrontmatter(data []byte, path string) (skillFrontmatter, error) {
	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	lines := strings.Split(string(normalized), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return skillFrontmatter{}, fmt.Errorf("%s: SKILL.md must start with YAML frontmatter", path)
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
	}
	if closing < 0 {
		return skillFrontmatter{}, fmt.Errorf("%s: frontmatter is not closed", path)
	}
	var v skillFrontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &v); err != nil {
		return v, err
	}
	return v, nil
}
func requireMappingKeys(node *yaml.Node, allowed ...string) error {
	valid := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		valid[key] = struct{}{}
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if _, ok := valid[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	return nil
}

func duplicateString(values []string) string {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return value
		}
		seen[value] = struct{}{}
	}
	return ""
}

func decodeStrictYAML(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
func resolvePath(base, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path must be non-empty")
	}
	p := filepath.Clean(value)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Abs(p)
}
func normalizeStringList(items []string, label string) ([]string, error) {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, v := range items {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, fmt.Errorf("%s contains an empty value", label)
		}
		if _, ok := seen[v]; ok {
			return nil, fmt.Errorf("%s contains duplicate %q", label, v)
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out, nil
}
