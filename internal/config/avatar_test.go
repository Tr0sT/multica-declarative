package config

import (
	"path/filepath"
	"testing"
)

func TestLegacyAvatarFieldsAreAcceptedButIgnored(t *testing.T) {
	for _, fields := range []string{
		"  avatarUrl: \"emoji:👩🏽‍💻\"\n",
		"  avatarUrl: \"emoji:\"\n",
		"  avatarUrl: \"\"\n",
		"  avatarUrl: null\n",
		"  avatarUrl: https://example.invalid/avatar.png\n",
		"  avatarFile: missing.png\n",
		"  avatarFile: /nonexistent/avatar.png\n",
		"  avatarFile: ../../missing.png\n  avatarUrl: \"emoji:old\"\n",
	} {
		t.Run(fields, func(t *testing.T) {
			root := t.TempDir()
			manifest := filepath.Join(root, "multica.yaml")
			writeFile(t, manifest, "apiVersion: multica-declarative/v1alpha1\nruntimes:\n  r: {id: runtime-1}\n")
			writeFile(t, filepath.Join(root, "agents/a/agent.yaml"), "name: A\nmultica:\n  runtime: r\n"+fields)
			writeFile(t, filepath.Join(root, "squads/s/squad.yaml"), "name: S\nleader: A\navatarUrl: https://example.invalid/old.png\n")
			for _, withoutSecrets := range []bool{false, true} {
				p, err := LoadWithOptions(manifest, LoadOptions{WithoutSecrets: withoutSecrets})
				if err != nil {
					t.Fatal(err)
				}
				if p.Agents[0].AvatarURL != nil || p.Agents[0].AvatarFile != "" || p.Squads[0].AvatarURL != "" {
					t.Fatal("legacy avatars became desired state")
				}
			}
		})
	}
}
