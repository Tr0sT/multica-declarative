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
func (c *CLI) GetSkill(id string) (model.Skill, error) {
	var v model.Skill
	err := c.runJSON(&v, "skill", "get", id, "--output", "json")
	return v, err
}
func (c *CLI) CreateSkill(in model.SkillInput) (model.Skill, error) {
	args := []string{"skill", "create", "--name", in.Name}
	if in.Description != "" {
		args = append(args, "--description", in.Description)
	}
	args = append(args, "--content-file", in.ContentFile, "--output", "json")
	var v model.Skill
	err := c.runJSON(&v, args...)
	return v, err
}
func (c *CLI) UpdateSkill(id string, in model.SkillInput) (model.Skill, error) {
	var v model.Skill
	err := c.runJSON(&v, "skill", "update", id, "--name", in.Name, "--description", in.Description, "--content-file", in.ContentFile, "--output", "json")
	return v, err
}
func (c *CLI) UpsertSkillFile(id string, in model.SkillFileInput) (model.SkillFile, error) {
	var v model.SkillFile
	err := c.runJSON(&v, "skill", "files", "upsert", id, "--path", in.Path, "--content-file", in.ContentFile, "--output", "json")
	return v, err
}
func (c *CLI) DeleteSkillFile(skillID, fileID string) error {
	return c.run("skill", "files", "delete", skillID, fileID)
}

func (c *CLI) ListAgents() ([]model.Agent, error) {
	var v []model.Agent
	if err := c.runJSON(&v, "agent", "list", "--include-archived", "--output", "json"); err != nil {
		return nil, err
	}
	for i := range v {
		normalizeAgent(&v[i])
	}
	return v, nil
}
func (c *CLI) GetAgent(id string) (model.Agent, error) {
	var v model.Agent
	err := c.runJSON(&v, "agent", "get", id, "--output", "json")
	normalizeAgent(&v)
	return v, err
}
func (c *CLI) ListAgentSkills(id string) ([]model.SkillSummary, error) {
	var v []model.SkillSummary
	if err := c.runJSON(&v, "agent", "skills", "list", id, "--output", "json"); err != nil {
		return nil, err
	}
	return v, nil
}
func (c *CLI) CreateAgent(in model.AgentInput) (model.Agent, error) {
	var v model.Agent
	err := c.runJSON(&v, c.agentArgs([]string{"agent", "create"}, in, false)...)
	normalizeAgent(&v)
	return v, err
}
func (c *CLI) UpdateAgent(id string, in model.AgentInput) (model.Agent, error) {
	var v model.Agent
	err := c.runJSON(&v, c.agentArgs([]string{"agent", "update", id}, in, true)...)
	normalizeAgent(&v)
	return v, err
}
func (c *CLI) SetAgentSkills(id string, ids []string) error {
	var raw json.RawMessage
	return c.runJSON(&raw, "agent", "skills", "set", id, "--skill-ids", strings.Join(ids, ","), "--output", "json")
}
func (c *CLI) ListRuntimes() ([]model.Runtime, error) {
	var v []model.Runtime
	if err := c.runJSON(&v, "runtime", "list", "--output", "json"); err != nil {
		return nil, err
	}
	return v, nil
}

func (c *CLI) GetAgentEnv(id string) (map[string]string, error) {
	var raw struct {
		CustomEnv map[string]string `json:"custom_env"`
	}
	if err := c.runJSON(&raw, "agent", "env", "get", id, "--output", "json"); err != nil {
		return nil, err
	}
	if raw.CustomEnv == nil {
		raw.CustomEnv = map[string]string{}
	}
	return raw.CustomEnv, nil
}
func (c *CLI) SetAgentEnv(id, file string) error {
	var raw json.RawMessage
	return c.runJSON(&raw, "agent", "env", "set", id, "--custom-env-file", file, "--output", "json")
}
func (c *CLI) UploadAgentAvatar(id, file string) error {
	var raw json.RawMessage
	return c.runJSON(&raw, "agent", "avatar", id, "--file", file, "--output", "json")
}
func (c *CLI) ArchiveAgent(id string) error {
	var raw json.RawMessage
	return c.runJSON(&raw, "agent", "archive", id, "--output", "json")
}
func (c *CLI) RestoreAgent(id string) error {
	var raw json.RawMessage
	return c.runJSON(&raw, "agent", "restore", id, "--output", "json")
}

func (c *CLI) ListSquads() ([]model.Squad, error) {
	var v []model.Squad
	if err := c.runJSON(&v, "squad", "list", "--output", "json"); err != nil {
		return nil, err
	}
	return v, nil
}
func (c *CLI) GetSquad(id string) (model.Squad, error) {
	var v model.Squad
	err := c.runJSON(&v, "squad", "get", id, "--output", "json")
	return v, err
}
func (c *CLI) CreateSquad(in model.SquadInput) (model.Squad, error) {
	args := []string{"squad", "create", "--name", in.Name, "--leader", in.LeaderID}
	if in.Description != "" {
		args = append(args, "--description", in.Description)
	}
	args = append(args, "--output", "json")
	var v model.Squad
	err := c.runJSON(&v, args...)
	return v, err
}
func (c *CLI) UpdateSquad(id string, in model.SquadInput, fields []string) (model.Squad, error) {
	args := []string{"squad", "update", id}
	for _, f := range fields {
		switch f {
		case "name":
			args = append(args, "--name", in.Name)
		case "description":
			args = append(args, "--description", in.Description)
		case "instructions":
			args = append(args, "--instructions", in.Instructions)
		case "leader":
			args = append(args, "--leader", in.LeaderID)
		case "avatarUrl":
			args = append(args, "--avatar-url", in.AvatarURL)
		}
	}
	args = append(args, "--output", "json")
	var v model.Squad
	err := c.runJSON(&v, args...)
	return v, err
}
func (c *CLI) ListSquadMembers(id string) ([]model.SquadMember, error) {
	var v []model.SquadMember
	if err := c.runJSON(&v, "squad", "member", "list", id, "--output", "json"); err != nil {
		return nil, err
	}
	return v, nil
}
func (c *CLI) AddSquadMember(id string, m model.SquadMember) error {
	var raw json.RawMessage
	return c.runJSON(&raw, "squad", "member", "add", id, "--member-id", m.MemberID, "--type", m.MemberType, "--role", m.Role, "--output", "json")
}
func (c *CLI) SetSquadMemberRole(id string, m model.SquadMember) error {
	var raw json.RawMessage
	return c.runJSON(&raw, "squad", "member", "set-role", id, "--member-id", m.MemberID, "--member-type", m.MemberType, "--role", m.Role, "--output", "json")
}
func (c *CLI) RemoveSquadMember(id string, m model.SquadMember) error {
	return c.run("squad", "member", "remove", id, "--member-id", m.MemberID, "--type", m.MemberType, "--output", "json")
}

func (c *CLI) ListRuntimeProfiles() ([]model.RuntimeProfile, error) {
	var v []model.RuntimeProfile
	if err := c.runJSON(&v, "runtime", "profile", "list", "--output", "json"); err != nil {
		return nil, err
	}
	for i := range v {
		if v[i].FixedArgs == nil {
			v[i].FixedArgs = []string{}
		}
		if v[i].Visibility == "" {
			v[i].Visibility = "workspace"
		}
	}
	return v, nil
}
func (c *CLI) CreateRuntimeProfile(in model.RuntimeProfileInput) (model.RuntimeProfile, error) {
	args := []string{"runtime", "profile", "create", "--display-name", in.DisplayName, "--protocol-family", in.ProtocolFamily, "--command-name", in.CommandName}
	if in.Description != "" {
		args = append(args, "--description", in.Description)
	}
	args = append(args, "--output", "json")
	var v model.RuntimeProfile
	err := c.runJSON(&v, args...)
	return v, err
}
func (c *CLI) UpdateRuntimeProfile(id string, in model.RuntimeProfileInput, fields []string) (model.RuntimeProfile, error) {
	args := []string{"runtime", "profile", "update", id}
	for _, f := range fields {
		switch f {
		case "displayName":
			args = append(args, "--display-name", in.DisplayName)
		case "commandName":
			args = append(args, "--command-name", in.CommandName)
		case "description":
			args = append(args, "--description", in.Description)
		case "enabled":
			args = append(args, "--enabled="+strconv.FormatBool(in.Enabled))
		}
	}
	args = append(args, "--output", "json")
	var v model.RuntimeProfile
	err := c.runJSON(&v, args...)
	return v, err
}

func (c *CLI) agentArgs(prefix []string, in model.AgentInput, includeClears bool) []string {
	args := append([]string{}, prefix...)
	args = append(args, "--name", in.Name, "--runtime-id", in.RuntimeID)
	if includeClears || in.Description != "" {
		args = append(args, "--description", in.Description)
	}
	if includeClears || in.Instructions != "" {
		args = append(args, "--instructions", in.Instructions)
	}
	if includeClears || in.Model != "" {
		args = append(args, "--model", in.Model)
	}
	if includeClears || in.ThinkingLevel != "" {
		args = append(args, "--thinking-level", in.ThinkingLevel)
	}
	if includeClears || len(in.CustomArgs) > 0 {
		v := in.CustomArgs
		if v == nil {
			v = []string{}
		}
		b, _ := json.Marshal(v)
		args = append(args, "--custom-args", string(b))
	}
	if in.ManageRuntimeConfig {
		v := in.RuntimeConfig
		if v == nil {
			v = map[string]any{}
		}
		b, _ := json.Marshal(v)
		args = append(args, "--runtime-config", string(b))
	}
	if in.ManageMCPConfig {
		args = append(args, "--mcp-config-file", in.MCPConfigFile)
	}
	mode := in.PermissionMode
	if mode == "" {
		if in.Permission == "workspace" {
			mode = "public_to"
		} else {
			mode = "private"
		}
	}
	args = append(args, "--max-concurrent-tasks", strconv.Itoa(in.MaxConcurrentTasks), "--permission-mode", mode)
	if mode == "public_to" {
		targets := in.InvocationTargets
		if len(targets) == 0 && in.Permission == "workspace" {
			targets = []model.InvocationTarget{{TargetType: "workspace"}}
		}
		for _, target := range targets {
			switch target.TargetType {
			case "workspace":
				args = append(args, "--public-to-workspace")
			case "member":
				if target.TargetID != nil {
					args = append(args, "--public-to-member", *target.TargetID)
				}
			}
		}
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
func (c *CLI) run(args ...string) error { _, err := c.execute(args...); return err }
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
func normalizeAgent(v *model.Agent) {
	if v.PermissionMode == "" {
		v.PermissionMode = "private"
	}
	if v.MaxConcurrentTasks == 0 {
		v.MaxConcurrentTasks = 1
	}
	if v.CustomArgs == nil {
		v.CustomArgs = []string{}
	}
	if v.RuntimeConfig == nil {
		v.RuntimeConfig = map[string]any{}
	}
	if v.InvocationTargets == nil {
		v.InvocationTargets = []model.InvocationTarget{}
	}
	if v.Skills == nil {
		v.Skills = []model.SkillSummary{}
	}
	if v.DisabledRuntimeSkills == nil {
		v.DisabledRuntimeSkills = []model.DisabledRuntimeSkill{}
	}
	if v.ComposioToolkitAllowlist == nil {
		v.ComposioToolkitAllowlist = []string{}
	}
}
