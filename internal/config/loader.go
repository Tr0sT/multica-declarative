package config

import (
	"bytes"
	"fmt"
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
	agentKind     = "Prompt"
)

type workspaceDocument struct {
	APIVersion string                     `yaml:"apiVersion"`
	Kind       string                     `yaml:"kind"`
	Skills     []string                   `yaml:"skills"`
	Agents     []string                   `yaml:"agents"`
	Runtimes   map[string]runtimeDocument `yaml:"runtimes"`
}

type runtimeDocument struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	CustomName string `yaml:"customName"`
	Provider   string `yaml:"provider"`
}

type agentDocument struct {
	Kind             string   `yaml:"kind"`
	Name             string   `yaml:"name"`
	Description      string   `yaml:"description"`
	Instructions     string   `yaml:"instructions"`
	InstructionsFile string   `yaml:"instructionsFile"`
	Model            modelDoc `yaml:"model"`
	Skills           []string `yaml:"skills"`
	Multica          multiDoc `yaml:"multica"`
}

type modelDoc struct {
	ID string `yaml:"id"`
}

type multiDoc struct {
	Runtime            string   `yaml:"runtime"`
	ThinkingLevel      string   `yaml:"thinkingLevel"`
	MaxConcurrentTasks int      `yaml:"maxConcurrentTasks"`
	Permission         string   `yaml:"permission"`
	CustomArgs         []string `yaml:"customArgs"`
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

	var document workspaceDocument
	if err := decodeStrictYAML(absolute, &document); err != nil {
		return model.Project{}, err
	}
	if document.APIVersion != apiVersion {
		return model.Project{}, fmt.Errorf("unsupported apiVersion %q; expected %q", document.APIVersion, apiVersion)
	}
	if document.Kind != workspaceKind {
		return model.Project{}, fmt.Errorf("unsupported kind %q; expected %q", document.Kind, workspaceKind)
	}

	base := filepath.Dir(absolute)
	project := model.Project{
		WorkspacePath:    absolute,
		RuntimeSelectors: make(map[string]model.RuntimeSelector, len(document.Runtimes)),
	}

	for alias, raw := range document.Runtimes {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			return model.Project{}, fmt.Errorf("runtime aliases must be non-empty strings")
		}
		selector := model.RuntimeSelector{
			ID:         strings.TrimSpace(raw.ID),
			Name:       strings.TrimSpace(raw.Name),
			CustomName: strings.TrimSpace(raw.CustomName),
			Provider:   strings.TrimSpace(raw.Provider),
		}
		if selector.ID == "" && selector.Name == "" && selector.CustomName == "" {
			return model.Project{}, fmt.Errorf("runtime %q must specify id, name, or customName", alias)
		}
		project.RuntimeSelectors[alias] = selector
	}

	for _, item := range document.Skills {
		path, err := resolvePath(base, item)
		if err != nil {
			return model.Project{}, fmt.Errorf("skill path: %w", err)
		}
		skill, err := loadSkill(path)
		if err != nil {
			return model.Project{}, err
		}
		project.Skills = append(project.Skills, skill)
	}

	for _, item := range document.Agents {
		path, err := resolvePath(base, item)
		if err != nil {
			return model.Project{}, fmt.Errorf("agent path: %w", err)
		}
		agent, err := loadAgent(path)
		if err != nil {
			return model.Project{}, err
		}
		project.Agents = append(project.Agents, agent)
	}

	if err := validate(project); err != nil {
		return model.Project{}, err
	}
	return project, nil
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
	frontmatter, err := parseFrontmatter(data, contentPath)
	if err != nil {
		return model.SkillSpec{}, err
	}
	frontmatter.Name = strings.TrimSpace(frontmatter.Name)
	frontmatter.Description = strings.TrimSpace(frontmatter.Description)
	if frontmatter.Name == "" {
		return model.SkillSpec{}, fmt.Errorf("%s: frontmatter.name is required", contentPath)
	}
	if frontmatter.Description == "" {
		return model.SkillSpec{}, fmt.Errorf("%s: frontmatter.description is required", contentPath)
	}

	skill := model.SkillSpec{
		Name:        frontmatter.Name,
		Description: frontmatter.Description,
		Content:     string(data),
		SourceDir:   directory,
		ContentPath: contentPath,
	}

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
		fileData, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if len(fileData) == 0 {
			return fmt.Errorf("skill file must not be empty: %s", path)
		}
		if !utf8.Valid(fileData) {
			return fmt.Errorf("skill file must be UTF-8: %s", path)
		}
		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil {
			return relErr
		}
		skill.Files = append(skill.Files, model.SkillFileSpec{
			Path:       filepath.ToSlash(relative),
			SourcePath: path,
			Content:    string(fileData),
		})
		return nil
	})
	if err != nil {
		return model.SkillSpec{}, fmt.Errorf("walk skill %s: %w", directory, err)
	}
	sort.Slice(skill.Files, func(i, j int) bool { return skill.Files[i].Path < skill.Files[j].Path })
	return skill, nil
}

func loadAgent(path string) (model.AgentSpec, error) {
	var document agentDocument
	if err := decodeStrictYAML(path, &document); err != nil {
		return model.AgentSpec{}, err
	}
	kind := strings.TrimSpace(document.Kind)
	if kind == "" {
		kind = agentKind
	}
	if kind != agentKind {
		return model.AgentSpec{}, fmt.Errorf("%s: unsupported agent kind %q; expected %q", path, kind, agentKind)
	}

	name := strings.TrimSpace(document.Name)
	if name == "" {
		return model.AgentSpec{}, fmt.Errorf("%s: name is required", path)
	}
	if document.Instructions != "" && strings.TrimSpace(document.InstructionsFile) != "" {
		return model.AgentSpec{}, fmt.Errorf("%s: instructions and instructionsFile are mutually exclusive", path)
	}
	instructions := document.Instructions
	if instructionsFile := strings.TrimSpace(document.InstructionsFile); instructionsFile != "" {
		resolved, err := resolvePath(filepath.Dir(path), instructionsFile)
		if err != nil {
			return model.AgentSpec{}, fmt.Errorf("%s: instructionsFile: %w", path, err)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return model.AgentSpec{}, fmt.Errorf("read instructionsFile %s: %w", resolved, err)
		}
		if !utf8.Valid(data) {
			return model.AgentSpec{}, fmt.Errorf("instructionsFile must be UTF-8: %s", resolved)
		}
		instructions = string(data)
	}

	maxConcurrent := document.Multica.MaxConcurrentTasks
	if maxConcurrent == 0 {
		maxConcurrent = 1
	}
	if maxConcurrent < 1 {
		return model.AgentSpec{}, fmt.Errorf("%s: maxConcurrentTasks must be at least 1", path)
	}
	permission := strings.TrimSpace(document.Multica.Permission)
	if permission == "" {
		permission = "private"
	}
	if permission != "private" && permission != "workspace" {
		return model.AgentSpec{}, fmt.Errorf("%s: permission must be private or workspace", path)
	}
	runtimeRef := strings.TrimSpace(document.Multica.Runtime)
	if runtimeRef == "" {
		return model.AgentSpec{}, fmt.Errorf("%s: multica.runtime is required", path)
	}

	skills, err := normalizeStringList(document.Skills, path+": skills")
	if err != nil {
		return model.AgentSpec{}, err
	}
	customArgs, err := normalizeStringList(document.Multica.CustomArgs, path+": multica.customArgs")
	if err != nil {
		return model.AgentSpec{}, err
	}

	return model.AgentSpec{
		Name:               name,
		Description:        strings.TrimSpace(document.Description),
		Instructions:       instructions,
		ModelID:            strings.TrimSpace(document.Model.ID),
		Skills:             skills,
		RuntimeRef:         runtimeRef,
		ThinkingLevel:      strings.TrimSpace(document.Multica.ThinkingLevel),
		MaxConcurrentTasks: maxConcurrent,
		Permission:         permission,
		CustomArgs:         customArgs,
		SourcePath:         path,
	}, nil
}

func validate(project model.Project) error {
	if len(project.Skills) == 0 && len(project.Agents) == 0 {
		return fmt.Errorf("workspace must declare at least one skill or agent")
	}

	skills := make(map[string]struct{}, len(project.Skills))
	for _, skill := range project.Skills {
		if _, exists := skills[skill.Name]; exists {
			return fmt.Errorf("duplicate skill name %q", skill.Name)
		}
		skills[skill.Name] = struct{}{}
	}

	agents := make(map[string]struct{}, len(project.Agents))
	for _, agent := range project.Agents {
		if _, exists := agents[agent.Name]; exists {
			return fmt.Errorf("duplicate agent name %q", agent.Name)
		}
		agents[agent.Name] = struct{}{}
		if _, exists := project.RuntimeSelectors[agent.RuntimeRef]; !exists {
			return fmt.Errorf("agent %q references unknown runtime %q", agent.Name, agent.RuntimeRef)
		}
		for _, skill := range agent.Skills {
			if _, exists := skills[skill]; !exists {
				return fmt.Errorf("agent %q references undeclared skill %q", agent.Name, skill)
			}
		}
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
		return skillFrontmatter{}, fmt.Errorf("%s: frontmatter is not closed with ---", path)
	}
	var result skillFrontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "\n")), &result); err != nil {
		return skillFrontmatter{}, fmt.Errorf("%s: invalid frontmatter: %w", path, err)
	}
	return result, nil
}

func decodeStrictYAML(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func resolvePath(base, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path must be a non-empty string")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func normalizeStringList(values []string, label string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s must contain non-empty strings", label)
		}
		result = append(result, value)
	}
	return result, nil
}
