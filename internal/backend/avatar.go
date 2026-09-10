package backend

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

// AgentAvatarReferences is separate from file uploads so existing backends do
// not have to claim support for an operation their CLI cannot perform.
type AgentAvatarReferences interface {
	CheckAgentAvatarURL() error
	SetAgentAvatarURL(agentID, value string) error
}

// CheckAgentAvatarURL is read-only and uses the same runner/authentication context
// as every other CLI command. Upstream 0.4.42 has no avatar reference setter.
func (c *CLI) CheckAgentAvatarURL() error {
	out, err := c.execute("agent", "update", "--help")
	if err != nil {
		return fmt.Errorf("check avatar reference support: %w", err)
	}
	for _, word := range strings.Fields(string(out)) {
		if word == "--avatar-url" {
			return nil
		}
	}
	return fmt.Errorf("installed Multica CLI cannot restore emoji avatars: agent update --avatar-url is missing; build the opt-in CLI with make avatar-cli and use --multica-bin /path/to/bin/multica-avatar (see docs/emoji-avatars.md)")
}

func (c *CLI) SetAgentAvatarURL(id, value string) error {
	if err := model.ValidateAvatarURL(value); err != nil {
		return err
	}
	var result struct {
		AvatarURL json.RawMessage `json:"avatar_url"`
	}
	if err := c.runJSON(&result, "agent", "update", id, "--avatar-url", value, "--output", "json"); err != nil {
		return err
	}
	// A success status alone is insufficient: do not silently accept a CLI or
	// server that ignored the avatar field or returned an incomplete response.
	if len(result.AvatarURL) == 0 {
		return fmt.Errorf("avatar update response omitted avatar_url")
	}
	var actual *string
	if err := json.Unmarshal(result.AvatarURL, &actual); err != nil {
		return fmt.Errorf("avatar update response contains an invalid avatar_url")
	}
	if (actual == nil && value != "") || (actual != nil && *actual != value) {
		return fmt.Errorf("avatar update response did not preserve the requested avatar reference")
	}
	return nil
}
