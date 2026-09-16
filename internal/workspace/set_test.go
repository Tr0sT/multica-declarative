package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
)

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func makeSet(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, filepath.Join(root, "multica.yaml"), "apiVersion: "+APIVersion+"\nworkspaces:\n  b:\n    id: ws-b\n  a:\n    id: ws-a\n")
	for _, key := range []string{"a", "b"} {
		child := filepath.Join(root, key)
		put(t, filepath.Join(child, "multica.yaml"), "apiVersion: "+APIVersion+"\n")
		put(t, filepath.Join(child, "skills/shared/SKILL.md"), "---\nname: shared\ndescription: Shared conventions.\n---\nContent for "+key+".\n")
		put(t, filepath.Join(child, "agents/builder/agent.yaml"), "name: Builder\nskills: [shared]\nmultica:\n  unbound: true\n")
	}
	return root
}

func TestLoadIndependentNamespacesAndDeterministicOrder(t *testing.T) {
	root := makeSet(t)
	set, err := Load(filepath.Join(root, "multica.yaml"), config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Entries) != 2 || set.Entries[0].Key != "a" || set.Entries[1].ID != "ws-b" {
		t.Fatalf("set=%#v", set)
	}
	for _, entry := range set.Entries {
		if entry.ConfigPath != filepath.Join(root, entry.Key, "multica.yaml") {
			t.Fatalf("workspace is not directly under root: %s", entry.ConfigPath)
		}
		if len(entry.Project.Skills) != 1 || len(entry.Project.Agents) != 1 || entry.Project.Agents[0].Name != "Builder" {
			t.Fatalf("project for %s = %#v", entry.Key, entry.Project)
		}
	}
}

func TestDirectChildKeepsBindingAndRootSecretPolicy(t *testing.T) {
	root := makeSet(t)
	manifest := filepath.Join(root, "multica.yaml")
	data, _ := os.ReadFile(manifest)
	put(t, manifest, string(data)+"secrets: omit\n")
	put(t, filepath.Join(root, "a/agents/builder/agent.yaml"), "name: Builder\nmultica:\n  unbound: true\n  customEnvFile: missing-env.json\n  mcpConfigFile: missing-mcp.json\n")
	for _, path := range []string{manifest, filepath.Join(root, "a/multica.yaml")} {
		set, err := Load(path, config.LoadOptions{WithoutSecrets: false})
		if err != nil {
			t.Fatal(err)
		}
		if set.Entries[0].ID != "ws-a" || !set.Entries[0].Project.WithoutSecrets {
			t.Fatalf("binding or inherited policy lost: %#v", set.Entries[0])
		}
		if path != manifest && len(set.Entries) != 1 {
			t.Fatal("direct child must select exactly one workspace")
		}
	}
}

func TestLegacyManifestStillLoadsAsLegacy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "multica.yaml")
	put(t, path, "apiVersion: "+APIVersion+"\n")
	set, err := Load(path, config.LoadOptions{})
	if err != nil || set != nil {
		t.Fatalf("set=%#v err=%v", set, err)
	}
}

func TestRejectInvalidSetMetadata(t *testing.T) {
	cases := []string{
		"workspaces: null\n",
		"workspaces: {}\n",
		"workspaces: []\n",
		"workspaces:\n  ../escape: {id: ws-a}\n",
		"workspaces:\n  /absolute: {id: ws-a}\n",
		"workspaces:\n  a/b: {id: ws-a}\n",
		"workspaces:\n  A: {id: ws-a}\n",
		"workspaces:\n  a: {}\n",
		"workspaces:\n  a: {id: ' ws-a'}\n",
		"workspaces:\n  a: {id: ws-a, typo: true}\n",
		"workspaces:\n  a: {id: ws-a}\n  b: {id: ws-a}\n",
		"workspaces:\n  a: {id: ws-a}\n  a: {id: ws-b}\n",
		"workspaces:\n  a: {id: ws-a}\nruntimes: {}\n",
		"workspaces:\n  a: {id: ws-a}\nsecrets: wrong\n",
		"workspaces:\n  a: {id: ws-a}\n---\nworkspaces: {}\n",
	}
	for _, content := range cases {
		t.Run(strings.ReplaceAll(content, "\n", "_"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "multica.yaml")
			put(t, path, "apiVersion: "+APIVersion+"\n"+content)
			if _, err := Read(path); err == nil {
				t.Fatalf("accepted invalid workspace set: %s", content)
			}
		})
	}
}

func TestRejectMissingUndeclaredNestedAndSymlinkDirectories(t *testing.T) {
	for _, kind := range []string{"missing", "undeclared", "nested", "symlink", "root-symlink", "manifest-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := makeSet(t)
			child := filepath.Join(root, "a")
			switch kind {
			case "missing":
				if err := os.RemoveAll(child); err != nil {
					t.Fatal(err)
				}
			case "undeclared":
				put(t, filepath.Join(root, "unlisted/multica.yaml"), "apiVersion: "+APIVersion+"\n")
			case "nested":
				put(t, filepath.Join(child, "multica.yaml"), "apiVersion: "+APIVersion+"\nworkspaces:\n  x: {id: ws-x}\n")
			case "symlink":
				if err := os.RemoveAll(child); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), child); err != nil {
					t.Fatal(err)
				}
			case "root-symlink":
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(root, link); err != nil {
					t.Fatal(err)
				}
				root = link
			case "manifest-symlink":
				path := filepath.Join(child, "multica.yaml")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "b/multica.yaml"), path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(filepath.Join(root, "multica.yaml"), config.LoadOptions{}); err == nil {
				t.Fatal("invalid layout accepted")
			}
		})
	}
}

func TestReferencesCannotResolveInAnotherWorkspace(t *testing.T) {
	root := makeSet(t)
	if err := os.RemoveAll(filepath.Join(root, "a/skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(root, "multica.yaml"), config.LoadOptions{}); err == nil {
		t.Fatal("workspace a must not resolve its agent's skill from workspace b")
	}
}

func TestRootLayoutAllowsRepositoryMetadataAndUnrelatedDirectories(t *testing.T) {
	root := makeSet(t)
	for _, path := range []string{".git/HEAD", "notes/readme.md", ".cache/multica.yaml"} {
		put(t, filepath.Join(root, path), "not a declaration")
	}
	for _, path := range []string{"multica.yaml", "a/multica.yaml"} {
		if _, err := Load(filepath.Join(root, path), config.LoadOptions{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDirectUnlistedChildDoesNotFallBackToActiveWorkspace(t *testing.T) {
	root := makeSet(t)
	path := filepath.Join(root, "unlisted/multica.yaml")
	put(t, path, "apiVersion: "+APIVersion+"\n")
	if _, err := Load(path, config.LoadOptions{}); err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("unlisted child must fail, not use the CLI default: %v", err)
	}
}

func TestRootKeysMayUseFormerCollectionAndResourceNames(t *testing.T) {
	for _, key := range []string{"workspaces", "agents", "skills"} {
		t.Run(key, func(t *testing.T) {
			root := t.TempDir()
			put(t, filepath.Join(root, "multica.yaml"), "apiVersion: "+APIVersion+"\nworkspaces:\n  "+key+": {id: ws-test}\n")
			put(t, filepath.Join(root, key, "multica.yaml"), "apiVersion: "+APIVersion+"\n")
			for _, path := range []string{filepath.Join(root, "multica.yaml"), filepath.Join(root, key, "multica.yaml")} {
				set, err := Load(path, config.LoadOptions{})
				if err != nil || len(set.Entries) != 1 || set.Entries[0].ID != "ws-test" {
					t.Fatalf("set=%#v err=%v", set, err)
				}
			}
		})
	}
}
