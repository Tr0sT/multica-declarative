package reconcile

import "github.com/Tr0sT/multica-declarative/internal/model"

// Preflight plans an apply and checks the CLI capabilities required by its
// writes, without mutating the workspace. Unlike Plan, this rejects changes
// that the selected CLI can observe but cannot perform (for example avatars).
// Apply still re-inspects and checks immediately before writing: this is not
// a transaction, and preflight cannot eliminate later races or API failures.
func (r Reconciler) Preflight(project model.Project) ([]model.Change, error) {
	state, err := r.inspect(project)
	if err != nil {
		return nil, err
	}
	if err := r.checkAvatarWrites(project, state); err != nil {
		return nil, err
	}
	return state.changes, nil
}
