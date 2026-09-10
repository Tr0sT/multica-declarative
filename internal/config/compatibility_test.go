package config

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLoadCurrentAgentFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n")
	writeFile(t, filepath.Join(root, "agents/helper/agent.yaml"), `name: Helper
multica:
  unbound: true
  serviceTier: ""
  systemKey: mika
  conversationStarters:
    - label: Проверить
      prompt: Проверь состояние проекта.
`)
	project, err := Load(filepath.Join(root, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	a := project.Agents[0]
	if !a.Unbound || a.RuntimeRef != "" || a.ServiceTier == nil || *a.ServiceTier != "" || a.SystemKey == nil || *a.SystemKey != "mika" || a.ConversationStarters == nil || len(*a.ConversationStarters) != 1 {
		t.Fatalf("new fields lost: %#v", a)
	}
}

func TestNewFieldsAreUnmanagedWhenOmitted(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "agent.yaml")
	writeFile(t, file, "name: Legacy\nmultica:\n  runtime: r\n")
	a, err := loadAgent(file)
	if err != nil {
		t.Fatal(err)
	}
	if a.ServiceTier != nil || a.ConversationStarters != nil || a.SystemKey != nil || a.Unbound {
		t.Fatal("old declarations changed meaning")
	}
}

func TestRejectsConflictingRuntimeBinding(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.yaml")
	writeFile(t, file, "name: Agent\nmultica:\n  runtime: r\n  unbound: true\n")
	if _, err := loadAgent(file); err == nil {
		t.Fatal("expected binding conflict")
	}
}

func TestConversationStarterValidation(t *testing.T) {
	for _, field := range []string{
		"  conversationStarters: [{label: hi}]\n",
		"  conversationStarters: [{label: hi, prompt: hello, typo: true}]\n",
		"  conversationStarters: [{label: hi, prompt: one}, {label: hi, prompt: two}, {label: hi, prompt: three}, {label: hi, prompt: four}]\n",
		"  conversationStarters: [{label: '" + strings.Repeat("я", 81) + "', prompt: hello}]\n",
	} {
		t.Run(field, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "agent.yaml")
			writeFile(t, file, "name: Agent\nmultica:\n  runtime: r\n"+field)
			if _, err := loadAgent(file); err == nil {
				t.Fatal("expected invalid starter error")
			}
		})
	}
}

func TestMCPConfigMatchesOfficialObjectOrNullContract(t *testing.T) {
	for i, value := range []string{"{}", "null", "[]", "true", "123", `"secret"`, "", "not-json"} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "agent.yaml")
			writeFile(t, file, "name: Agent\nmultica:\n  runtime: r\n  mcpConfigFile: mcp.json\n")
			writeFile(t, filepath.Join(dir, "mcp.json"), value)
			_, err := loadAgent(file)
			valid := value == "{}" || value == "null"
			if (err == nil) != valid {
				t.Fatalf("valid=%v err=%v", valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid config leaked its value")
			}
		})
	}
}
