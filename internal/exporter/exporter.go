package exporter

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

const (
	defaultOutputDir = "multica-export"
	apiVersion       = "multica-declarative/v1alpha1"
)

var generatedPaths = []string{"multica.yaml", "agents", "skills"}

type Options struct {
	OutputDir string
	Force     bool
}

type Result struct {
	OutputDir string
	Skills    int
	Agents    int
	Runtimes  int
	Warnings  []string
}

type Exporter struct {
	Backend backend.Backend
}

type snapshot struct {
	manifest workspaceDocument
	skills   []exportedSkill
	agents   []exportedAgent
	warnings []string
}

type exportedSkill struct {
	directory string
	content   string
	files     []model.SkillFile
}

type exportedAgent struct {
	directory    string
	instructions string
	document     agentDocument
}

type workspaceDocument struct {
	APIVersion string                     `yaml:"apiVersion"`
	Kind       string                     `yaml:"kind"`
	Skills     []string                   `yaml:"skills,omitempty"`
	Agents     []string                   `yaml:"agents,omitempty"`
	Runtimes   map[string]runtimeDocument `yaml:"runtimes,omitempty"`
}

type runtimeDocument struct {
	ID         string `yaml:"id,omitempty"`
	Name       string `yaml:"name,omitempty"`
	CustomName string `yaml:"customName,omitempty"`
	Provider   string `yaml:"provider,omitempty"`
}

type agentDocument struct {
	Kind             string          `yaml:"kind"`
	Name             string          `yaml:"name"`
	Description      string          `yaml:"description,omitempty"`
	InstructionsFile string          `yaml:"instructionsFile"`
	Model            *modelDocument  `yaml:"model,omitempty"`
	Skills           []string        `yaml:"skills,omitempty"`
	Multica          multicaDocument `yaml:"multica"`
}

type modelDocument struct {
	ID string `yaml:"id"`
}

type multicaDocument struct {
	Runtime            string   `yaml:"runtime"`
	ThinkingLevel      string   `yaml:"thinkingLevel,omitempty"`
	MaxConcurrentTasks int      `yaml:"maxConcurrentTasks"`
	Permission         string   `yaml:"permission"`
	CustomArgs         []string `yaml:"customArgs,omitempty"`
}

func (e Exporter) Export(options Options) (Result, error) {
	if e.Backend == nil {
		return Result{}, fmt.Errorf("export backend is required")
	}
	outputDir := strings.TrimSpace(options.OutputDir)
	if outputDir == "" {
		outputDir = defaultOutputDir
	}
	absolute, err := filepath.Abs(outputDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve output directory: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if err := validateTarget(absolute, options.Force); err != nil {
		return Result{}, err
	}

	snapshot, err := e.readSnapshot()
	if err != nil {
		return Result{}, err
	}
	if len(snapshot.skills) == 0 && len(snapshot.agents) == 0 {
		return Result{}, fmt.Errorf("Multica workspace contains no active agents or skills")
	}

	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output parent %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, ".multica-export-*")
	if err != nil {
		return Result{}, fmt.Errorf("create export staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	if err := writeSnapshot(staging, snapshot); err != nil {
		return Result{}, err
	}
	if err := installSnapshot(staging, absolute, options.Force); err != nil {
		return Result{}, err
	}

	return Result{
		OutputDir: absolute,
		Skills:    len(snapshot.skills),
		Agents:    len(snapshot.agents),
		Runtimes:  len(snapshot.manifest.Runtimes),
		Warnings:  snapshot.warnings,
	}, nil
}

func (e Exporter) readSnapshot() (snapshot, error) {
	skillSummaries, err := e.Backend.ListSkills()
	if err != nil {
		return snapshot{}, fmt.Errorf("list Multica skills: %w", err)
	}
	agentSummaries, err := e.Backend.ListAgents()
	if err != nil {
		return snapshot{}, fmt.Errorf("list Multica agents: %w", err)
	}
	runtimes, err := e.Backend.ListRuntimes()
	if err != nil {
		return snapshot{}, fmt.Errorf("list Multica runtimes: %w", err)
	}

	sort.Slice(skillSummaries, func(i, j int) bool {
		if skillSummaries[i].Name == skillSummaries[j].Name {
			return skillSummaries[i].ID < skillSummaries[j].ID
		}
		return skillSummaries[i].Name < skillSummaries[j].Name
	})
	sort.Slice(agentSummaries, func(i, j int) bool {
		if agentSummaries[i].Name == agentSummaries[j].Name {
			return agentSummaries[i].ID < agentSummaries[j].ID
		}
		return agentSummaries[i].Name < agentSummaries[j].Name
	})

	if duplicate := duplicateName(skillSummaries, func(item model.Skill) string { return item.Name }); duplicate != "" {
		return snapshot{}, fmt.Errorf("multiple Multica skills named %q cannot be exported declaratively", duplicate)
	}
	if duplicate := duplicateName(agentSummaries, func(item model.Agent) string { return item.Name }); duplicate != "" {
		return snapshot{}, fmt.Errorf("multiple active Multica agents named %q cannot be exported declaratively", duplicate)
	}

	skillNames := make(map[string]struct{}, len(skillSummaries))
	skillSlugs := make(map[string]struct{}, len(skillSummaries))
	exportedSkills := make([]exportedSkill, 0, len(skillSummaries))
	warnings := make([]string, 0)
	for _, summary := range skillSummaries {
		skill, err := e.Backend.GetSkill(summary.ID)
		if err != nil {
			return snapshot{}, fmt.Errorf("get Multica skill %q: %w", summary.Name, err)
		}
		if strings.TrimSpace(skill.Name) == "" {
			skill.Name = summary.Name
		}
		if strings.TrimSpace(skill.Name) == "" {
			return snapshot{}, fmt.Errorf("Multica skill %q has no name", summary.ID)
		}
		content, changed, err := normalizeSkillContent(skill)
		if err != nil {
			return snapshot{}, fmt.Errorf("prepare skill %q: %w", skill.Name, err)
		}
		if changed {
			warnings = append(warnings, fmt.Sprintf(
				"skill %q had missing or incompatible SKILL.md frontmatter; generated valid frontmatter, so the first plan may update its content",
				skill.Name,
			))
		}
		files, err := validateSkillFiles(skill.Name, skill.Files)
		if err != nil {
			return snapshot{}, err
		}
		directory := uniqueSlug(skill.Name, skill.ID, skillSlugs)
		exportedSkills = append(exportedSkills, exportedSkill{
			directory: directory,
			content:   content,
			files:     files,
		})
		skillNames[skill.Name] = struct{}{}
	}

	detailedAgents := make([]model.Agent, 0, len(agentSummaries))
	agentSkills := make(map[string][]model.SkillSummary, len(agentSummaries))
	for _, summary := range agentSummaries {
		agent, err := e.Backend.GetAgent(summary.ID)
		if err != nil {
			return snapshot{}, fmt.Errorf("get Multica agent %q: %w", summary.Name, err)
		}
		if strings.TrimSpace(agent.Name) == "" {
			agent.Name = summary.Name
		}
		if strings.TrimSpace(agent.Name) == "" {
			return snapshot{}, fmt.Errorf("Multica agent %q has no name", summary.ID)
		}
		permission, err := permissionForExport(agent)
		if err != nil {
			return snapshot{}, fmt.Errorf("agent %q: %w", agent.Name, err)
		}
		_ = permission
		skills, err := e.Backend.ListAgentSkills(agent.ID)
		if err != nil {
			return snapshot{}, fmt.Errorf("list skills for agent %q: %w", agent.Name, err)
		}
		for _, skill := range skills {
			if _, exists := skillNames[skill.Name]; !exists {
				return snapshot{}, fmt.Errorf("agent %q references skill %q which was not returned by skill list", agent.Name, skill.Name)
			}
		}
		sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
		detailedAgents = append(detailedAgents, agent)
		agentSkills[agent.ID] = skills
	}

	runtimeAliases, runtimeDocuments, err := makeRuntimeDocuments(runtimes, detailedAgents)
	if err != nil {
		return snapshot{}, err
	}

	agentSlugs := make(map[string]struct{}, len(detailedAgents))
	exportedAgents := make([]exportedAgent, 0, len(detailedAgents))
	for _, agent := range detailedAgents {
		permission, _ := permissionForExport(agent)
		directory := uniqueSlug(agent.Name, agent.ID, agentSlugs)
		skills := make([]string, 0, len(agentSkills[agent.ID]))
		for _, skill := range agentSkills[agent.ID] {
			skills = append(skills, skill.Name)
		}
		var modelValue *modelDocument
		if agent.Model != "" {
			modelValue = &modelDocument{ID: agent.Model}
		}
		exportedAgents = append(exportedAgents, exportedAgent{
			directory:    directory,
			instructions: agent.Instructions,
			document: agentDocument{
				Kind:             "Prompt",
				Name:             agent.Name,
				Description:      agent.Description,
				InstructionsFile: "AGENT.md",
				Model:            modelValue,
				Skills:           skills,
				Multica: multicaDocument{
					Runtime:            runtimeAliases[agent.RuntimeID],
					ThinkingLevel:      agent.ThinkingLevel,
					MaxConcurrentTasks: normalizedConcurrency(agent.MaxConcurrentTasks),
					Permission:         permission,
					CustomArgs:         append([]string(nil), agent.CustomArgs...),
				},
			},
		})
	}

	manifest := workspaceDocument{
		APIVersion: apiVersion,
		Kind:       "Workspace",
		Runtimes:   runtimeDocuments,
	}
	for _, skill := range exportedSkills {
		manifest.Skills = append(manifest.Skills, path.Join("skills", skill.directory))
	}
	for _, agent := range exportedAgents {
		manifest.Agents = append(manifest.Agents, path.Join("agents", agent.directory, "agent.yaml"))
	}

	return snapshot{
		manifest: manifest,
		skills:   exportedSkills,
		agents:   exportedAgents,
		warnings: warnings,
	}, nil
}
