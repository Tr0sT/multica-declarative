package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

const teamBridgeBaseYAML = "name: Support Bot\nagent: Agent\nserverUrl: https://chat.example.com\n"

func teamBridgeConfigFixture(t *testing.T, bot string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\n")
	writeFile(t, filepath.Join(root, "agents", "agent", "agent.yaml"), "name: Agent\nmultica:\n  unbound: true\n")
	writeFile(t, filepath.Join(root, "teambridge", "group", "bot", "bot.yaml"), bot)
	return root
}

func TestTeamBridgeLoadPreservesPortableSettingsAndDefersCredentials(t *testing.T) {
	root := teamBridgeConfigFixture(t, teamBridgeBaseYAML+`botUserId: aaaaaaaaaaaaaaaaaaaaaaaaaa
botUsername: support
status: active
context:
  mode: channel
  lookbackHours: 8760
accessMode: allowlist
allowedUsers:
  - id: bbbbbbbbbbbbbbbbbbbbbbbbbb
    username: '@Sergey'
deniedReply:
  mode: images
  imageFiles: [images/reply.png]
accessTokenFile: secrets/not-yet-provisioned.txt
`)
	data := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	image := filepath.Join(root, "teambridge/group/bot/images/reply.png")
	writeFile(t, image, string(data))
	p, err := Load(filepath.Join(root, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	bot := p.TeamBridge[0]
	if bot.Context.Mode != "channel" || bot.Context.LookbackHours != 8760 || bot.AllowedUsers[0].ID != "bbbbbbbbbbbbbbbbbbbbbbbbbb" || bot.AllowedUsers[0].Username != "sergey" {
		t.Fatalf("settings lost: %+v", bot)
	}
	if len(bot.DeniedReply.Images) != 1 || !bytes.Equal(bot.DeniedReply.Images[0].Data, data) || bot.DeniedReply.Images[0].ContentType != "image/png" {
		t.Fatal("image bytes or type lost")
	}
	if bot.AccessTokenFile != "secrets/not-yet-provisioned.txt" || !filepath.IsAbs(bot.SourceDirectory) || bot.WithoutSecrets {
		t.Fatal("token reference was not resolved lazily")
	}
}

func TestTeamBridgeConfigRejectsLossyOrUnsafeDeclarations(t *testing.T) {
	cases := []struct{ name, extra, want string }{
		{"unknown", "invented: true\n", "invented"},
		{"context window", "context: {mode: channel, lookbackHours: 12}\n", "lookbackHours"},
		{"message window", "context: {mode: message, lookbackHours: 24}\n", "message/thread"},
		{"wire context", "context: {mode: channel_extended, lookbackHours: 0}\n", "context.mode"},
		{"unknown status", "status: deleted\n", "status"},
		{"bad access", "accessMode: private\n", "accessMode"},
		{"grant outside allowlist", "allowedUsers: [{username: sergey}]\n", "allowlist"},
		{"duplicate users", "accessMode: allowlist\nallowedUsers: [{username: sergey}, {username: '@Sergey'}]\n", "duplicate username"},
		{"bad identity", "botUserId: unknown\n", "botUserId"},
		{"image without files", "deniedReply: {mode: images}\n", "imageFiles"},
		{"text with files", "deniedReply: {mode: text, imageFiles: [a.png]}\n", "imageFiles"},
		{"token ambiguity", "accessTokenEnv: BOT_TOKEN\naccessTokenFile: token.txt\n", "mutually exclusive"},
		{"invalid env", "accessTokenEnv: '$SECRET'\n", "environment variable"},
		{"token traversal", "accessTokenFile: ../token.txt\n", "inside"},
		{"image traversal", "deniedReply: {mode: images, imageFiles: [../reply.png]}\n", "escapes"},
		{"huge text", "deniedReply: {mode: text, text: '" + strings.Repeat("я", 4001) + "'}\n", "4000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := teamBridgeConfigFixture(t, teamBridgeBaseYAML+tc.extra)
			_, err := Load(filepath.Join(root, "multica.yaml"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
	for _, rawURL := range []string{"http://chat.example.com", "https://name:secret@chat.example.com", "https://chat.example.com?secret=value", "https://chat.example.com/a/../b", "file:///tmp/key"} {
		t.Run(rawURL, func(t *testing.T) {
			root := teamBridgeConfigFixture(t, "name: Bot\nagent: Agent\nserverUrl: "+rawURL+"\n")
			if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil {
				t.Fatal("invalid server URL accepted")
			}
		})
	}
}

func TestTeamBridgeSecretlessPolicyDoesNotInspectTokenReferences(t *testing.T) {
	for _, manifestPolicy := range []bool{false, true} {
		root := teamBridgeConfigFixture(t, teamBridgeBaseYAML+"accessTokenFile: ../../private/token\naccessTokenEnv: '$invalid-and-unused'\n")
		if manifestPolicy {
			writeFile(t, filepath.Join(root, "multica.yaml"), "apiVersion: multica-declarative/v1alpha1\nsecrets: omit\n")
		}
		p, err := LoadWithOptions(filepath.Join(root, "multica.yaml"), LoadOptions{WithoutSecrets: !manifestPolicy})
		if err != nil {
			t.Fatal(err)
		}
		if !p.TeamBridge[0].WithoutSecrets || p.TeamBridge[0].AccessTokenFile != "" || p.TeamBridge[0].AccessTokenEnv != "" {
			t.Fatal("secret fields remained managed")
		}
	}
}

func TestTeamBridgeImagesRejectSymlinksUnsupportedBytesAndOversize(t *testing.T) {
	for _, tc := range []string{"symlink", "text", "oversize"} {
		t.Run(tc, func(t *testing.T) {
			root := teamBridgeConfigFixture(t, teamBridgeBaseYAML+"deniedReply: {mode: images, imageFiles: [reply.png]}\n")
			path := filepath.Join(root, "teambridge/group/bot/reply.png")
			switch tc {
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside.png")
				writeFile(t, outside, "\x89PNG\r\n\x1a\n")
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "text":
				writeFile(t, path, "not an image")
			case "oversize":
				writeFile(t, path, "\x89PNG\r\n\x1a\n"+strings.Repeat("0", model.MaxTeamBridgeImageBytes))
			}
			if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil {
				t.Fatalf("%s image accepted", tc)
			}
		})
	}
}

func TestTeamBridgeReferenceAndIdentityUniqueness(t *testing.T) {
	root := teamBridgeConfigFixture(t, strings.ReplaceAll(teamBridgeBaseYAML, "agent: Agent", "agent: Missing"))
	if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil || !strings.Contains(err.Error(), "undeclared agent") {
		t.Fatalf("missing agent accepted: %v", err)
	}
	root = teamBridgeConfigFixture(t, teamBridgeBaseYAML)
	writeFile(t, filepath.Join(root, "teambridge/other/bot.yaml"), strings.ReplaceAll(teamBridgeBaseYAML, "Support Bot", "Other Bot"))
	if _, err := Load(filepath.Join(root, "multica.yaml")); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("duplicate binding accepted: %v", err)
	}
}
