package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var out, errout bytes.Buffer
	code := Run([]string{"--version"}, &out, &errout)
	if code != 0 || strings.TrimSpace(out.String()) != Version {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errout.String())
	}
}
func TestValidateAllowsFlagsAfterCommand(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n")
	write(t, filepath.Join(root, "skills/example/SKILL.md"), "---\nname: example\ndescription: Example\n---\n")
	var out, errout bytes.Buffer
	code := Run([]string{"validate", "--config", filepath.Join(root, "multica.yaml")}, &out, &errout)
	if code != 0 || !strings.Contains(out.String(), "Configuration is valid") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errout.String())
	}
}
func TestSplitCommandRecognizesExportAndOutputDir(t *testing.T) {
	command, args, err := splitCommand([]string{"export", "--output-dir", "snapshot", "--force"})
	if err != nil || command != "export" || strings.Join(args, "|") != "--output-dir|snapshot|--force" {
		t.Fatalf("command=%q args=%v err=%v", command, args, err)
	}
}
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWithoutSecretsFlagAndManifestWorkForValidate(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "multica.yaml")
	write(t, manifest, "apiVersion: multica-declarative/v1alpha1\n")
	write(t, filepath.Join(root, "agents/builder/agent.yaml"), "name: Builder\nmultica:\n  unbound: true\n  customEnvFile: missing-env.json\n  mcpConfigFile: missing-mcp.json\n")
	for _, args := range [][]string{
		{"--without-secrets", "validate", "--config", manifest},
		{"validate", "--config", manifest, "--without-secrets"},
	} {
		var out, errout bytes.Buffer
		if code := Run(args, &out, &errout); code != 0 || !strings.Contains(errout.String(), "unmanaged") {
			t.Fatalf("code=%d error=%s", code, errout.String())
		}
	}
	var out, errout bytes.Buffer
	if code := Run([]string{"validate", "--config", manifest}, &out, &errout); code == 0 {
		t.Fatal("normal validation must still reject missing referenced secret files")
	}
	write(t, manifest, "apiVersion: multica-declarative/v1alpha1\nsecrets: omit\n")
	out.Reset()
	errout.Reset()
	if code := Run([]string{"validate", "--without-secrets=false", "--config", manifest}, &out, &errout); code != 0 {
		t.Fatalf("false flag must not override a secret-free manifest: %s", errout.String())
	}
}
