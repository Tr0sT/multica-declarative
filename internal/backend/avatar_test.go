package backend

import (
	"slices"
	"strings"
	"testing"
)

func TestAvatarCapabilityCheck(t *testing.T) {
	for _, tc := range []struct {
		help      string
		supported bool
	}{
		{"Flags:\n      --avatar-url string    New avatar reference\n", true},
		{"Flags:\n      --avatar-url-file string    Something else\n", false},
		{"Flags:\n      --file string    Image file\n", false},
	} {
		runner := &fakeRunner{stdout: []byte(tc.help)}
		c := &CLI{Runner: runner}
		err := c.CheckAgentAvatarURL()
		if (err == nil) != tc.supported {
			t.Fatalf("%q: %v", tc.help, err)
		}
		if !slices.Equal(runner.calls[0].args, []string{"agent", "update", "--help"}) {
			t.Fatal(runner.calls)
		}
	}
}

func TestAvatarReferenceSetter(t *testing.T) {
	for _, tc := range []struct {
		value, response string
		valid           bool
	}{
		{"emoji:👩🏽‍💻", `{"avatar_url":"emoji:👩🏽‍💻"}`, true},
		{"", `{"avatar_url":""}`, true}, {"", `{"avatar_url":null}`, true},
		{"emoji:🐝", `{}`, false}, {"emoji:🐝", `{"avatar_url":null}`, false},
		{"emoji:🐝", `{"avatar_url":"emoji:🐧"}`, false},
		{"emoji:🐝", `{"avatar_url":42}`, false},
	} {
		runner := &fakeRunner{stdout: []byte(tc.response)}
		c := &CLI{Runner: runner}
		err := c.SetAgentAvatarURL("a", tc.value)
		if (err == nil) != tc.valid {
			t.Fatalf("%q: %v", tc.response, err)
		}
		want := []string{"agent", "update", "a", "--avatar-url", tc.value, "--output", "json"}
		if !slices.Equal(runner.calls[0].args, want) {
			t.Fatal(runner.calls)
		}
	}
}

func TestInvalidAvatarReferenceNeverExecutes(t *testing.T) {
	runner := &fakeRunner{}
	err := (&CLI{Runner: runner}).SetAgentAvatarURL("a", "emoji:")
	if err == nil || len(runner.calls) != 0 || !strings.Contains(err.Error(), "avatarUrl") {
		t.Fatal(err, runner.calls)
	}
}
