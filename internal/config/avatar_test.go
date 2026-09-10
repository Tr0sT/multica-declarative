package config

import (
	"path/filepath"
	"strconv"
	"testing"
)

func TestAvatarURLDeclaration(t *testing.T) {
	for _, value := range []string{"emoji:🌞", "emoji:👩🏽‍💻", "emoji:❤️", ""} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.yaml")
			writeFile(t, path, "name: Agent\nmultica:\n  runtime: r\n  avatarUrl: "+strconv.Quote(value)+"\n")
			a, err := loadAgent(path)
			if err != nil {
				t.Fatal(err)
			}
			if a.AvatarURL == nil || *a.AvatarURL != value || a.AvatarFile != "" {
				t.Fatalf("avatar changed: %#v", a)
			}
		})
	}
}

func TestAvatarURLOmissionDoesNotManageAvatar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	writeFile(t, path, "name: Agent\nmultica:\n  runtime: r\n")
	a, err := loadAgent(path)
	if err != nil || a.AvatarURL != nil {
		t.Fatalf("%#v %v", a, err)
	}
}

func TestRejectsInvalidOrConflictingAvatarReferences(t *testing.T) {
	for _, fields := range []string{
		`avatarUrl: "emoji:"`, `avatarUrl: "emoji: "`, `avatarUrl: "emoji:🌞\n"`,
		`avatarUrl: "file:/etc/passwd"`, `avatarUrl: "https://example.invalid/pic.png"`,
		"avatarUrl: \"emoji:🌞\"\n  avatarFile: avatar.png", "avatarUrl: \"\"\n  avatarFile: avatar.png",
	} {
		t.Run(fields, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.yaml")
			writeFile(t, path, "name: Agent\nmultica:\n  runtime: r\n  "+fields+"\n")
			if _, err := loadAgent(path); err == nil {
				t.Fatal("invalid avatar accepted")
			}
		})
	}
}
