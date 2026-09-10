//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestOfficialCLIContracts(t *testing.T) {
	f, cli := newFixture(t)
	raw, err := os.ReadFile("multica.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		out, stderr, err := cli.Runner.Run(context.Background(), cli.Binary, args...)
		if err != nil {
			t.Fatalf("CLI %v: %v %s", args, err, stderr)
		}
		return out
	}
	var version struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(run("version", "--output", "json"), &version); err != nil {
		t.Fatal(err)
	}
	if strings.TrimPrefix(version.Version, "v") != lock.Version {
		t.Fatalf("integration suite requires Multica %s, got %s", lock.Version, version.Version)
	}
	contracts := map[string][]string{
		"skill list":            {"output"},
		"skill get":             {"with-content", "output"},
		"skill create":          {"name", "description", "content-file"},
		"skill update":          {"name", "description", "content-file"},
		"skill files upsert":    {"path", "content-file"},
		"skill files delete":    {},
		"agent list":            {"include-archived", "output"},
		"agent get":             {"output"},
		"agent create":          {"name", "description", "instructions", "runtime-id", "model", "thinking-level", "service-tier", "custom-args", "runtime-config", "mcp-config-file", "max-concurrent-tasks", "permission-mode", "public-to-workspace", "public-to-member"},
		"agent update":          {"name", "description", "instructions", "runtime-id", "model", "thinking-level", "service-tier", "custom-args", "runtime-config", "mcp-config-file", "max-concurrent-tasks", "permission-mode", "public-to-workspace", "public-to-member"},
		"agent skills list":     {"output"},
		"agent skills set":      {"skill-ids"},
		"agent env get":         {"output"},
		"agent env set":         {"custom-env-file"},
		"agent avatar":          {"file"},
		"agent archive":         {},
		"agent restore":         {},
		"runtime list":          {"output"},
		"squad list":            {"output"},
		"squad get":             {"output"},
		"squad create":          {"name", "leader", "description"},
		"squad update":          {"name", "leader", "description", "instructions", "avatar-url"},
		"squad member list":     {},
		"squad member add":      {"member-id", "type", "role"},
		"squad member set-role": {"member-id", "member-type", "role"},
		"squad member remove":   {"member-id", "type"},
		"workspace mcp list":    {"output"},
	}
	flagPattern := regexp.MustCompile(`(?m)^\s+(?:-[a-zA-Z], )?--([a-z][a-z0-9-]*)\b`)
	for command, flags := range contracts {
		t.Run(command, func(t *testing.T) {
			available := map[string]bool{}
			for _, match := range flagPattern.FindAllSubmatch(run(append(strings.Fields(command), "--help")...), -1) {
				available[string(match[1])] = true
			}
			for _, flag := range flags {
				if !available[flag] {
					t.Errorf("missing --%s", flag)
				}
			}
		})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 0 {
		t.Fatal("version/help contracts unexpectedly contacted the API")
	}
}
