package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"--version"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "0.3.0") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestValidateAllowsFlagsAfterCommand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, "multica.yaml"), `apiVersion: multica-declarative/v1alpha1
kind: Workspace
skills: [skills/example]
`)
	write(t, filepath.Join(root, "skills/example/SKILL.md"), `---
name: example
description: Example
---
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"validate", "--config", filepath.Join(root, "multica.yaml")}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Configuration is valid") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestSplitCommandRecognizesExportAndOutputDir(t *testing.T) {
	t.Parallel()
	command, args, err := splitCommand([]string{"export", "--output-dir", "snapshot", "--force"})
	if err != nil {
		t.Fatalf("splitCommand returned error: %v", err)
	}
	if command != "export" {
		t.Fatalf("command = %q", command)
	}
	want := []string{"--output-dir", "snapshot", "--force"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %v", args)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
