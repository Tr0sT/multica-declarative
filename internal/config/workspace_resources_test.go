package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workspaceFiles(t *testing.T, project, autopilot string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n")
	if project != "" {
		writeFile(t, filepath.Join(dir, "projects/group/game/project.yaml"), project)
	}
	if autopilot != "" {
		writeFile(t, filepath.Join(dir, "autopilots/group/nightly/autopilot.yaml"), autopilot)
		writeFile(t, filepath.Join(dir, "agents/builder/agent.yaml"), "name: Builder\nmultica:\n  unbound: true\n")
	}
	return dir
}
func TestProjectOnlyDeclaration(t *testing.T) {
	dir := workspaceFiles(t, "name: Game\ndescriptionFile: PROJECT.md\nresources: []\n", "")
	writeFile(t, filepath.Join(dir, "projects/group/game/PROJECT.md"), "# Details\n\nСодержимое.  \n\n")
	p, err := Load(filepath.Join(dir, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Projects) != 1 || p.Projects[0].Status != "planned" || p.Projects[0].Resources == nil {
		t.Fatal("project-only snapshot not loaded")
	}
	if p.Projects[0].Description != "# Details\n\nСодержимое.  \n\n" {
		t.Fatal("body was modified")
	}
}
func TestAutopilotDefaultsAndEmptyVsOmittedCollections(t *testing.T) {
	dir := workspaceFiles(t, "name: Game\n", "name: Nightly\nagent: Builder\nproject: Game\nmode: run_only\nsubscribers: []\ntriggers:\n  - kind: schedule\n    cron: '0 9 * * *'\n    enabled: false\n")
	p, err := Load(filepath.Join(dir, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	a := p.Autopilots[0]
	if a.Status != "paused" || (*a.Triggers)[0].Timezone != "UTC" || (*a.Triggers)[0].Enabled || a.Subscribers == nil {
		t.Fatal("incorrect safe defaults")
	}
	if p.Projects[0].Resources != nil || a.Collaborators != nil {
		t.Fatal("omission did not stay unmanaged")
	}
}
func TestInvalidWorkspaceResourceDeclarations(t *testing.T) {
	tests := []struct{ name, p, a string }{
		{"unknown-field", "name: Game\nnotAField: true\n", ""},
		{"invalid-status", "name: Game\nstatus: archived\n", ""},
		{"bad-date", "name: Game\nstartDate: 2026-02-30\n", ""},
		{"reversed-dates", "name: Game\nstartDate: 2026-10-01\ndueDate: 2026-09-01\n", ""},
		{"missing-agent", "name: Game\nlead: {type: agent, agent: Missing}\n", ""},
		{"bad-resource", "name: Game\nresources: [{type: unknown, ref: {url: x}}]\n", ""},
		{"secret-url", "name: Game\nresources: [{type: github_repo, ref: {url: 'https://token@github.com/org/repo'}}]\n", ""},
		{"duplicate-ref", "name: Game\nresources: [{type: github_repo, ref: {url: 'https://github.com/a/b'}}, {type: github_repo, ref: {url: 'https://github.com/a/b'}}]\n", ""},
		{"unknown-project", "name: Game\n", "name: Nightly\nagent: Builder\nproject: Missing\nmode: run_only\n"},
		{"two-assignees", "name: Game\n", "name: Nightly\nagent: Builder\nsquad: Team\nmode: run_only\n"},
		{"bad-mode", "name: Game\n", "name: Nightly\nagent: Builder\nmode: forever\n"},
		{"bad-timezone", "name: Game\n", "name: Nightly\nagent: Builder\nmode: run_only\ntriggers: [{kind: schedule, cron: '0 9 * * *', timezone: Unknown/City}]\n"},
		{"webhook-cron", "name: Game\n", "name: Nightly\nagent: Builder\nmode: run_only\ntriggers: [{kind: webhook, cron: '0 9 * * *'}]\n"},
		{"bad-template", "name: Game\n", "name: Nightly\nagent: Builder\nmode: create_issue\nissueTitleTemplate: '{{secret}}'\n"},
		{"description-traversal", "name: Game\ndescriptionFile: ../../../../outside.md\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := workspaceFiles(t, tt.p, tt.a)
			if _, err := Load(filepath.Join(dir, "multica.yaml")); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
func TestNewCollectionsRejectSymlinksAndDuplicateNames(t *testing.T) {
	dir := workspaceFiles(t, "name: Game\n", "")
	writeFile(t, filepath.Join(dir, "projects/other/project.yaml"), "name: Game\n")
	if _, err := Load(filepath.Join(dir, "multica.yaml")); err == nil || !strings.Contains(err.Error(), "duplicate project") {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "projects/other")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("group/game", filepath.Join(dir, "projects/link")); err != nil {
		t.Skip(err)
	}
	if _, err := Load(filepath.Join(dir, "multica.yaml")); err == nil {
		t.Fatal("symlink accepted")
	}
}
