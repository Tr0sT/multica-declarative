package reconcile

import (
	"fmt"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
)

func (r Reconciler) checkAvatarWrites(project model.Project, state inspection) error {
	checked := false
	for _, agent := range project.Agents {
		if agent.AvatarURL == nil {
			continue
		}
		if err := model.ValidateAvatarURL(*agent.AvatarURL); err != nil {
			return fmt.Errorf("agent %q: %w", agent.Name, err)
		}
		if agent.AvatarFile != "" {
			return fmt.Errorf("agent %q: avatarUrl and avatarFile are mutually exclusive", agent.Name)
		}
		change := state.agentChanges[agent.Name]
		if change.Action != Create && !hasAnyField(change.Fields, "avatar") {
			continue
		}
		ops, ok := r.Backend.(backend.AgentAvatarReferences)
		if !ok {
			return fmt.Errorf("backend cannot restore avatarUrl for agent %q", agent.Name)
		}
		if !checked {
			if err := ops.CheckAgentAvatarURL(); err != nil {
				return err
			}
			checked = true
		}
	}
	return nil
}
