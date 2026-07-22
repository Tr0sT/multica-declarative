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
	if code != 0 || !strings.Contains(out.String(), "0.4.0") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errout.String())
	}
}
func TestValidateAllowsFlagsAfterCommand(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\nkind: Workspace\nskills: [skills/example]\n")
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
