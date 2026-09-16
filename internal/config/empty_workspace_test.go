package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyWorkspaceRequiresExplicitLoadOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multica.yaml")
	put := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	put("apiVersion: multica-declarative/v1alpha1\nsecrets: omit\n")
	if _, err := Load(path); err == nil {
		t.Fatal("legacy empty declarations must remain rejected")
	}
	project, err := LoadWithOptions(path, LoadOptions{AllowEmpty: true})
	if err != nil || !project.WithoutSecrets {
		t.Fatalf("explicit empty workspace lost secret policy: %#v, %v", project, err)
	}
	for _, content := range []string{
		"apiVersion: invalid\n",
		"apiVersion: multica-declarative/v1alpha1\ntypo: true\n",
		"apiVersion: multica-declarative/v1alpha1\nsecrets: invalid\n",
		"apiVersion: multica-declarative/v1alpha1\nruntimes:\n  invalid: {}\n",
	} {
		put(content)
		if _, err := LoadWithOptions(path, LoadOptions{AllowEmpty: true}); err == nil {
			t.Fatalf("AllowEmpty bypassed manifest validation: %s", content)
		}
	}
}
