package config

import (
	"path/filepath"
	"testing"
)

func TestLoadTeamBridgeAttachmentMode(t *testing.T) {
	for _, tc := range []struct {
		field, wanted string
		managed, valid bool
	}{
		{"", "", false, true},
		{"attachmentMode: null\n", "", false, true},
		{"attachmentMode: none\n", "none", true, true},
		{"attachmentMode: links\n", "links", true, true},
		{"attachmentMode: \"\"\n", "", true, false},
		{"attachmentMode: download\n", "", true, false},
		{"attachmentMode: LINKS\n", "", true, false},
		{"attachmentMode: [links]\n", "", true, false},
	} {
		t.Run(tc.field, func(t *testing.T) {
			root := t.TempDir()
			manifest := filepath.Join(root, "multica.yaml")
			writeFile(t, manifest, "apiVersion: multica-declarative/v1alpha1\nruntimes:\n  r: {id: runtime-1}\n")
			writeFile(t, filepath.Join(root, "agents/a/agent.yaml"), "name: A\nmultica:\n  runtime: r\n")
			writeFile(t, filepath.Join(root, "teambridge/a/bot.yaml"), "name: Bot\nagent: A\nserverUrl: https://mattermost.example.test\n"+tc.field)
			for _, withoutSecrets := range []bool{false, true} {
				p, err := LoadWithOptions(manifest, LoadOptions{WithoutSecrets: withoutSecrets})
				if (err == nil) != tc.valid { t.Fatalf("unexpected validation: %v", err) }
				if !tc.valid { continue }
				if len(p.TeamBridge) != 1 { t.Fatal("bot lost") }
				mode := p.TeamBridge[0].AttachmentMode
				if (mode != nil) != tc.managed || (mode != nil && *mode != tc.wanted) {
					t.Fatal("attachment mode changed or defaulted during load")
				}
			}
		})
	}
}
