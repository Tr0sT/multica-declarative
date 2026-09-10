package model

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidateAvatarURL validates the lossless emoji reference form. Image avatars
// continue to use avatarFile; empty explicitly clears an existing reference.
// Do not normalize Unicode or split grapheme sequences.
func ValidateAvatarURL(value string) error {
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "emoji:") || !utf8.ValidString(value) {
		return fmt.Errorf("avatarUrl must be an emoji: reference or an empty string; use avatarFile for images")
	}
	body := strings.TrimPrefix(value, "emoji:")
	if body == "" || strings.IndexFunc(body, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("avatarUrl requires a non-empty emoji: value without whitespace or control characters")
	}
	return nil
}
