package exporter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
	"github.com/Tr0sT/multica-declarative/internal/workspace"
)

type workspaceCatalog struct {
	items []backend.Workspace
	err   error
}

func (c workspaceCatalog) ListWorkspaces() ([]backend.Workspace, error) { return c.items, c.err }

func workspaceExportFixture() (WorkspaceExporter, map[string]*fakeBackend) {
	backends := map[string]*fakeBackend{"ws-a": exampleBackend(), "ws-b": exampleBackend(), "ws-empty": {}}
	ex := WorkspaceExporter{
		Catalog:    workspaceCatalog{items: []backend.Workspace{{ID: "ws-b", Slug: "b"}, {ID: "ws-a", Slug: "a"}, {ID: "ws-empty", Slug: "empty"}}},
		BackendFor: func(id string) backend.Backend { return backends[id] },
	}
	return ex, backends
}

func TestWorkspaceSetExportRoundTripsDuplicateNamesAndEmptyWorkspace(t *testing.T) {
	for _, withoutSecrets := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "without-secrets"}[withoutSecrets], func(t *testing.T) {
			ex, backends := workspaceExportFixture()
			out := filepath.Join(t.TempDir(), "snapshot")
			result, err := ex.Export(Options{OutputDir: out, WithoutSecrets: withoutSecrets})
			if err != nil {
				t.Fatal(err)
			}
			if result.Workspaces != 3 || result.Agents != 4 || result.Skills != 2 {
				t.Fatalf("result=%#v", result)
			}
			set, err := workspace.Load(filepath.Join(out, "multica.yaml"), config.LoadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range set.Entries {
				if entry.ConfigPath != filepath.Join(out, entry.Key, "multica.yaml") {
					t.Fatalf("workspace not exported directly under root: %s", entry.ConfigPath)
				}
				if entry.Project.WithoutSecrets != withoutSecrets {
					t.Fatal("secret policy lost")
				}
				changes, err := (reconcile.Reconciler{Backend: backends[entry.ID]}).Plan(entry.Project)
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range changes {
					if change.Action != reconcile.Noop {
						t.Fatalf("workspace %s drift: %#v", entry.Key, change)
					}
				}
			}
			secret := filepath.Join(out, "a/agents/unity-developer/custom-env.json")
			info, statErr := os.Stat(secret)
			if withoutSecrets {
				if !os.IsNotExist(statErr) {
					t.Fatal("secret file was exported")
				}
			} else if statErr != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("secret file mode/error: %v %v", info, statErr)
			}
			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != result.Workspaces+1 {
				t.Fatalf("unexpected root layout: %v", entries)
			}
			if _, err := os.Lstat(filepath.Join(out, "workspaces")); !os.IsNotExist(err) {
				t.Fatal("export created an unnecessary workspaces wrapper")
			}
		})
	}
}

func TestWorkspaceSetForcePreservesGroupsNotesAndGit(t *testing.T) {
	ex, _ := workspaceExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := ex.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(out, "a/agents/unity-developer")
	group := filepath.Join(out, "a/agents/main")
	if err := os.MkdirAll(group, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, filepath.Join(group, "unity-developer")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"README.md", ".git/HEAD", "notes/readme.md", "a/notes.md"} {
		target := filepath.Join(out, path)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("keep"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ex.Export(Options{OutputDir: out}); err == nil {
		t.Fatal("non-empty output requires --force")
	}
	if _, err := ex.Export(Options{OutputDir: out, Force: true}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"README.md", ".git/HEAD", "notes/readme.md", "a/notes.md"} {
		data, err := os.ReadFile(filepath.Join(out, path))
		if err != nil || string(data) != "keep" {
			t.Fatalf("lost %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(group, "unity-developer/agent.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("grouping was flattened")
	}
	if _, err := workspace.Load(filepath.Join(out, "multica.yaml"), config.LoadOptions{}); err != nil {
		t.Fatal(err)
	}
}

type failedWorkspaceBackend struct{ *fakeBackend }

func (b failedWorkspaceBackend) ListSkills() ([]model.Skill, error) {
	return nil, errors.New("synthetic read failure")
}

func TestWorkspaceSetFailureDoesNotReplaceAnyExistingWorkspace(t *testing.T) {
	ex, backends := workspaceExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := ex.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(out, "a/agents/unity-developer/AGENT.md")
	if err := os.WriteFile(marker, []byte("keep old snapshot"), 0644); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ex.BackendFor = func(id string) backend.Backend {
		if id == "ws-b" {
			return failedWorkspaceBackend{backends[id]}
		}
		return backends[id]
	}
	if _, err := ex.Export(Options{OutputDir: out, Force: true}); err == nil {
		t.Fatal("expected read error")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep old snapshot" {
		t.Fatal("earlier workspace replaced before later read failed")
	}
	manifestAfter, err := os.ReadFile(filepath.Join(out, "multica.yaml"))
	if err != nil || string(manifestAfter) != string(manifestBefore) {
		t.Fatal("manifest replaced on failed export")
	}
}

func TestWorkspaceSetExportFailsClosedForInvalidCatalog(t *testing.T) {
	cases := [][]backend.Workspace{
		nil,
		{{ID: "ws-a", Slug: "../escape"}},
		{{ID: "ws-a", Slug: "a"}, {ID: "ws-a", Slug: "b"}},
		{{ID: "ws-a", Slug: "a"}, {ID: "ws-b", Slug: "a"}},
		{{ID: "", Slug: "a"}},
	}
	for _, values := range cases {
		ex := WorkspaceExporter{Catalog: workspaceCatalog{items: values}, BackendFor: func(string) backend.Backend { t.Fatal("invalid catalog must fail before resource reads"); return nil }}
		out := filepath.Join(t.TempDir(), "snapshot")
		if _, err := ex.Export(Options{OutputDir: out}); err == nil {
			t.Fatalf("invalid catalog accepted: %#v", values)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatal("invalid catalog wrote output")
		}
	}
}

func TestWorkspaceSetDoesNotPruneInaccessibleWorkspace(t *testing.T) {
	ex, _ := workspaceExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := ex.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	ex.Catalog = workspaceCatalog{items: []backend.Workspace{{ID: "ws-a", Slug: "a"}}}
	if _, err := ex.Export(Options{OutputDir: out, Force: true}); err == nil || !strings.Contains(err.Error(), "no longer accessible") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "b/multica.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSetForceKeepsOmitPolicyAndRemovesGeneratedSecrets(t *testing.T) {
	ex, _ := workspaceExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := ex.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Export(Options{OutputDir: out, Force: true, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Export(Options{OutputDir: out, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "a/agents/unity-developer/custom-env.json")); !os.IsNotExist(err) {
		t.Fatal("force refresh reintroduced secrets")
	}
}

func TestWorkspaceSetInstallRollback(t *testing.T) {
	root := t.TempDir()
	target, staging := filepath.Join(root, "target"), filepath.Join(root, "staging")
	for _, path := range []string{filepath.Join(target, "a"), filepath.Join(target, "b"), filepath.Join(staging, "a")} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"multica.yaml", "a/keep.txt", "b/keep.txt"} {
		if err := os.WriteFile(filepath.Join(target, path), []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Missing staged b forces a failure after installing the manifest and a.
	for _, path := range []string{"multica.yaml", "a/keep.txt"} {
		if err := os.WriteFile(filepath.Join(staging, path), []byte("new"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := installWorkspaceSet(staging, target, true, []string{"a", "b"}); err == nil {
		t.Fatal("expected failed install")
	}
	for _, path := range []string{"multica.yaml", "a/keep.txt", "b/keep.txt"} {
		data, err := os.ReadFile(filepath.Join(target, path))
		if err != nil || string(data) != "old" {
			t.Fatalf("rollback lost %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(staging, "a")); !os.IsNotExist(err) {
		t.Fatal("test did not reach partial directory installation")
	}
}

func TestWorkspaceSetExportRefusesUnownedRootDirectoryCollision(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "refresh"}[refresh], func(t *testing.T) {
			ex, _ := workspaceExportFixture()
			out := filepath.Join(t.TempDir(), "snapshot")
			if refresh {
				if _, err := ex.Export(Options{OutputDir: out}); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(out, "notes"), 0755); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(out, "notes/keep.txt")
			if err := os.WriteFile(marker, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			catalog := ex.Catalog.(workspaceCatalog)
			catalog.items = append(catalog.items, backend.Workspace{ID: "ws-notes", Slug: "notes"})
			ex.Catalog = catalog
			ex.BackendFor = func(string) backend.Backend {
				t.Fatal("collision must fail before reading workspace resources")
				return nil
			}
			if _, err := ex.Export(Options{OutputDir: out, Force: true}); err == nil || !strings.Contains(err.Error(), "unowned") {
				t.Fatalf("expected unowned-directory conflict, got %v", err)
			}
			data, err := os.ReadFile(marker)
			if err != nil || string(data) != "keep" {
				t.Fatal("unrelated directory was modified")
			}
		})
	}
}

func TestWorkspaceSetRefreshKeepsRenamedRootDirectory(t *testing.T) {
	ex, _ := workspaceExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := ex.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	manifest, err := workspace.Read(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Workspaces["renamed"] = manifest.Workspaces["a"]
	delete(manifest.Workspaces, "a")
	if err := os.Rename(filepath.Join(out, "a"), filepath.Join(out, "renamed")); err != nil {
		t.Fatal(err)
	}
	if err := writeYAML(filepath.Join(out, "multica.yaml"), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Export(Options{OutputDir: out, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "a")); !os.IsNotExist(err) {
		t.Fatal("refresh discarded the directory-to-ID binding")
	}
	set, err := workspace.Load(filepath.Join(out, "renamed/multica.yaml"), config.LoadOptions{})
	if err != nil || len(set.Entries) != 1 || set.Entries[0].ID != "ws-a" {
		t.Fatalf("set=%#v err=%v", set, err)
	}
}
