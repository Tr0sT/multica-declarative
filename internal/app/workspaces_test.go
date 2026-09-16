package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/workspace"
)

func appWorkspaceSet(t *testing.T) (string, *workspace.Set) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "multica.yaml")
	write(t, path, "apiVersion: multica-declarative/v1alpha1\nworkspaces:\n  a: {id: ws-a}\n  b: {id: ws-b}\n")
	for _, key := range []string{"a", "b"} {
		write(t, filepath.Join(root, key, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n")
		write(t, filepath.Join(root, key, "skills/shared/SKILL.md"), "---\nname: shared\ndescription: Shared conventions.\n---\nContent "+key+".\n")
	}
	set, err := workspace.Load(path, config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return path, set
}

type appWorkspaceCatalog []backend.Workspace

func (c appWorkspaceCatalog) ListWorkspaces() ([]backend.Workspace, error) { return c, nil }

type workspaceMemoryBackend struct {
	backend.Backend
	id     string
	skills []model.Skill
	writes int
	fail   bool
}

func (b *workspaceMemoryBackend) ListSkills() ([]model.Skill, error) { return b.skills, nil }
func (b *workspaceMemoryBackend) GetSkill(id string) (model.Skill, error) {
	for _, skill := range b.skills {
		if skill.ID == id {
			return skill, nil
		}
	}
	return model.Skill{}, os.ErrNotExist
}
func (b *workspaceMemoryBackend) ListAgents() ([]model.Agent, error) { return nil, nil }
func (b *workspaceMemoryBackend) ListRuntimes() ([]model.Runtime, error) {
	if b.fail {
		return nil, errors.New("synthetic runtime read failure")
	}
	return nil, nil
}
func (b *workspaceMemoryBackend) CreateSkill(in model.SkillInput) (model.Skill, error) {
	content, err := os.ReadFile(in.ContentFile)
	if err != nil {
		return model.Skill{}, err
	}
	b.writes++
	skill := model.Skill{ID: "skill-" + b.id, Name: in.Name, Description: in.Description, Content: string(content)}
	b.skills = append(b.skills, skill)
	return skill, nil
}
func (b *workspaceMemoryBackend) UpdateSkill(id string, in model.SkillInput) (model.Skill, error) {
	content, err := os.ReadFile(in.ContentFile)
	if err != nil {
		return model.Skill{}, err
	}
	for i := range b.skills {
		if b.skills[i].ID == id {
			b.writes++
			b.skills[i].Name, b.skills[i].Description, b.skills[i].Content = in.Name, in.Description, string(content)
			return b.skills[i], nil
		}
	}
	return model.Skill{}, os.ErrNotExist
}

func TestWorkspaceSetValidationIsOfflineAndChildIsPinned(t *testing.T) {
	path, _ := appWorkspaceSet(t)
	for _, selected := range []string{path, filepath.Join(filepath.Dir(path), "b/multica.yaml")} {
		var out, errout bytes.Buffer
		code := Run([]string{"validate", "--config", selected, "--multica-bin", "/nonexistent/multica"}, &out, &errout)
		if code != 0 || !strings.Contains(out.String(), "ws-b") {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), errout.String())
		}
		if selected != path && strings.Contains(out.String(), "ws-a") {
			t.Fatal("child unexpectedly included workspace a")
		}
	}
}

func TestWorkspaceSetApplyKeepsSameNamedSkillsSeparateAndConverges(t *testing.T) {
	_, set := appWorkspaceSet(t)
	a, b := &workspaceMemoryBackend{id: "a"}, &workspaceMemoryBackend{id: "b"}
	factory := func(id string) backend.Backend {
		if id == "ws-a" {
			return a
		}
		if id == "ws-b" {
			return b
		}
		t.Fatalf("unexpected workspace %s", id)
		return nil
	}
	catalog := appWorkspaceCatalog{{ID: "ws-a"}, {ID: "ws-b"}}
	for i := 0; i < 2; i++ {
		var out, errout bytes.Buffer
		if err := runWorkspaceSet("apply", set, catalog, factory, &out, &errout); err != nil {
			t.Fatal(err)
		}
	}
	if a.writes != 1 || b.writes != 1 {
		t.Fatalf("apply did not converge: %d / %d writes", a.writes, b.writes)
	}
	if a.skills[0].Name != b.skills[0].Name || a.skills[0].Content == b.skills[0].Content {
		t.Fatal("workspace resource namespaces were mixed")
	}
}

func TestWorkspaceSetPreflightAndAccessErrorsPreventAllWrites(t *testing.T) {
	_, set := appWorkspaceSet(t)
	for _, kind := range []string{"access", "preflight"} {
		t.Run(kind, func(t *testing.T) {
			a, b := &workspaceMemoryBackend{id: "a"}, &workspaceMemoryBackend{id: "b", fail: true}
			calls := 0
			factory := func(id string) backend.Backend {
				calls++
				if id == "ws-a" {
					return a
				}
				return b
			}
			catalog := appWorkspaceCatalog{{ID: "ws-a"}, {ID: "ws-b"}}
			if kind == "access" {
				catalog = catalog[:1]
			}
			var out, errout bytes.Buffer
			if err := runWorkspaceSet("apply", set, catalog, factory, &out, &errout); err == nil {
				t.Fatal("expected preflight failure")
			}
			if a.writes+b.writes != 0 {
				t.Fatal("mutated an earlier workspace before preflight finished")
			}
			if kind == "access" && calls != 0 {
				t.Fatal("missing workspace should fail before resource reads")
			}
		})
	}
}

func TestWorkspaceScopeFlagsAreValidated(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "--all-workspaces"},
		{"export", "--all-workspaces", "--workspace-id", "ws-a"},
		{"export", "--workspace-id="},
		{"export", "--profile="},
	} {
		var out, errout bytes.Buffer
		if code := Run(args, &out, &errout); code != 2 {
			t.Fatalf("args=%v code=%d err=%s", args, code, errout.String())
		}
	}
	command, args, err := splitCommand([]string{"--profile", "apply", "--workspace-id", "plan", "export"})
	if err != nil || command != "export" || len(args) != 4 {
		t.Fatalf("command=%s args=%v err=%v", command, args, err)
	}
	path, _ := appWorkspaceSet(t)
	var out, errout bytes.Buffer
	if code := Run([]string{"validate", "--config", path, "--workspace-id", "ws-wrong"}, &out, &errout); code != 2 {
		t.Fatal("explicit flag overrode set bindings")
	}
}

func TestFlatExportCannotOverwriteSetOrRetargetChild(t *testing.T) {
	path, _ := appWorkspaceSet(t)
	root := filepath.Dir(path)
	if _, _, err := flatExportTarget(root, ""); err == nil {
		t.Fatal("flat export accepted workspace-set root")
	}
	id, _, err := flatExportTarget(filepath.Join(root, "b"), "")
	if err != nil || id != "ws-b" {
		t.Fatalf("id=%s err=%v", id, err)
	}
	if _, _, err := flatExportTarget(filepath.Join(root, "b"), "ws-a"); err == nil {
		t.Fatal("child export was retargeted")
	}
}
