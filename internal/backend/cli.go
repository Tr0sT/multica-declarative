package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

const defaultTimeout = 2 * time.Minute

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

type CLI struct {
	Binary  string
	Timeout time.Duration
	Runner  Runner
}

func NewCLI(binary string) *CLI {
	return &CLI{Binary: binary, Timeout: defaultTimeout, Runner: ExecRunner{}}
}

func (c *CLI) ListSkills() ([]model.Skill, error) {
	var result []model.Skill
	if err := c.runJSON(&result, "skill", "list", "--output", "json"); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *CLI) GetSkill(skillID string) (model.Skill, error) {
	var result model.Skill
	err := c.runJSON(&result, "skill", "get", skillID, "--output", "json")
	return result, err
}

func (c *CLI) CreateSkill(input model.SkillInput) (model.Skill, error) {
	args := []string{"skill", "create", "--name", input.Name}
	if input.Description != "" {
		args = append(args, "--description", input.Description)
	}
	args = append(args, "--content-file", input.ContentFile, "--output", "json")
	var result model.Skill
	err := c.runJSON(&result, args...)
	return result, err
}

func (c *CLI) UpdateSkill(skillID string, input model.SkillInput) (model.Skill, error) {
	args := []string{
		"skill", "update", skillID,
		"--name", input.Name,
		"--description", input.Description,
		"--content-file", input.ContentFile,
		"--output", "json",
	}
	var result model.Skill
	err := c.runJSON(&result, args...)
	return result, err
}

func (c *CLI) UpsertSkillFile(skillID string, input model.SkillFileInput) (model.SkillFile, error) {
	var result model.SkillFile
	err := c.runJSON(
		&result,
		"skill", "files", "upsert", skillID,
		"--path", input.Path,
		"--content-file", input.ContentFile,
		"--output", "json",
	)
	return result, err
}

func (c *CLI) DeleteSkillFile(skillID, fileID string) error {
	return c.run("skill", "files", "delete", skillID, fileID)
}

func (c *CLI) ListAgents() ([]model.Agent, error) {
	var result []model.Agent
	if err := c.runJSON(&result, "agent", "list", "--output", "json"); err != nil {
		return nil, err
	}
	normalizeAgents(result)
	return result, nil
}

func (c *CLI) GetAgent(agentID string) (model.Agent, error) {
	var result model.Agent
	err := c.runJSON(&result, "agent", "get", agentID, "--output", "json")
	normalizeAgent(&result)
	return result, err
}

func (c *CLI) ListAgentSkills(agentID string) ([]model.SkillSummary, error) {
	var result []model.SkillSummary
	if err := c.runJSON(&result, "agent", "skills", "list", agentID, "--output", "json"); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *CLI) CreateAgent(input model.AgentInput) (model.Agent, error) {
	var result model.Agent
	err := c.runJSON(&result, c.agentArgs([]string{"agent", "create"}, input, false)...)
	normalizeAgent(&result)
	return result, err
}

func (c *CLI) UpdateAgent(agentID string, input model.AgentInput) (model.Agent, error) {
	var result model.Agent
	err := c.runJSON(&result, c.agentArgs([]string{"agent", "update", agentID}, input, true)...)
	normalizeAgent(&result)
	return result, err
}

func (c *CLI) SetAgentSkills(agentID string, skillIDs []string) error {
	var ignored json.RawMessage
	return c.runJSON(
		&ignored,
		"agent", "skills", "set", agentID,
		"--skill-ids", strings.Join(skillIDs, ","),
		"--output", "json",
	)
}

func (c *CLI) ListRuntimes() ([]model.Runtime, error) {
	var result []model.Runtime
	if err := c.runJSON(&result, "runtime", "list", "--output", "json"); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *CLI) agentArgs(prefix []string, input model.AgentInput, includeClears bool) []string {
	args := append([]string{}, prefix...)
	args = append(args, "--name", input.Name, "--runtime-id", input.RuntimeID)

	if includeClears || input.Description != "" {
		args = append(args, "--description", input.Description)
	}
	if includeClears || input.Instructions != "" {
		args = append(args, "--instructions", input.Instructions)
	}
	if includeClears || input.Model != "" {
		args = append(args, "--model", input.Model)
	}
	if includeClears || input.ThinkingLevel != "" {
		args = append(args, "--thinking-level", input.ThinkingLevel)
	}
	if includeClears || len(input.CustomArgs) > 0 {
		customArgs := input.CustomArgs
		if customArgs == nil {
			customArgs = []string{}
		}
		encoded, _ := json.Marshal(customArgs)
		args = append(args, "--custom-args", string(encoded))
	}

	args = append(args,
		"--max-concurrent-tasks", strconv.Itoa(input.MaxConcurrentTasks),
		"--permission-mode",
	)
	if input.Permission == "workspace" {
		args = append(args, "public_to", "--public-to-workspace")
	} else {
		args = append(args, "private")
	}
	args = append(args, "--output", "json")
	return args
}

func (c *CLI) runJSON(target any, args ...string) error {
	stdout, err := c.execute(args...)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(stdout)) == 0 {
		return fmt.Errorf("%s returned empty output for %s", c.Binary, strings.Join(args, " "))
	}
	if err := json.Unmarshal(stdout, target); err != nil {
		return fmt.Errorf("%s returned invalid JSON for %s: %w", c.Binary, strings.Join(args, " "), err)
	}
	return nil
}

func (c *CLI) run(args ...string) error {
	_, err := c.execute(args...)
	return err
}

func (c *CLI) execute(args ...string) ([]byte, error) {
	binary := c.Binary
	if binary == "" {
		binary = "multica"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runner := c.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stdout, stderr, err := runner.Run(ctx, binary, args...)
	if err == nil {
		return stdout, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%s %s timed out after %s", binary, strings.Join(args, " "), timeout)
	}
	message := fmt.Sprintf("%s %s failed", binary, strings.Join(args, " "))
	if text := strings.TrimSpace(string(stderr)); text != "" {
		message += ":\n" + text
	}
	return nil, fmt.Errorf("%s: %w", message, err)
}

func normalizeAgents(items []model.Agent) {
	for index := range items {
		normalizeAgent(&items[index])
	}
}

func normalizeAgent(item *model.Agent) {
	if item.PermissionMode == "" {
		item.PermissionMode = "private"
	}
	if item.MaxConcurrentTasks == 0 {
		item.MaxConcurrentTasks = 1
	}
	if item.CustomArgs == nil {
		item.CustomArgs = []string{}
	}
	if item.InvocationTargets == nil {
		item.InvocationTargets = []model.InvocationTarget{}
	}
	if item.Skills == nil {
		item.Skills = []model.SkillSummary{}
	}
}
