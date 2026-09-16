package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
	"github.com/Tr0sT/multica-declarative/internal/workspace"
)

func flatExportTarget(output, explicitID string) (id string, omit bool, err error) {
	path := filepath.Join(output, "multica.yaml")
	manifest, err := workspace.Read(path)
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	if manifest != nil {
		return "", false, fmt.Errorf("output is a workspace set; use export --all-workspaces or select a child output directory")
	}
	parent, _, key, err := workspace.Enclosing(path)
	if err != nil {
		return "", false, err
	}
	if parent == nil {
		return explicitID, false, nil
	}
	id = parent.Workspaces[key].ID
	if explicitID != "" && explicitID != id {
		return "", false, fmt.Errorf("--workspace-id conflicts with the output directory's workspace binding")
	}
	return id, parent.Secrets == "omit", nil
}

func runWorkspaceSet(command string, set *workspace.Set, catalog backend.WorkspaceLister, factory func(string) backend.Backend, stdout, stderr io.Writer) error {
	for _, entry := range set.Entries {
		if entry.Project.WithoutSecrets {
			fmt.Fprintf(stderr, "notice: workspace %q secret-bearing agent fields are unmanaged; existing values will not be changed\n", entry.Key)
		}
	}
	if command == "validate" {
		for _, entry := range set.Entries {
			fmt.Fprintf(stdout, "Workspace %s (%s):\n", entry.Key, entry.ID)
			printValidation(stdout, entry.Project)
		}
		return nil
	}
	if command != "plan" && command != "apply" {
		return fmt.Errorf("unsupported workspace-set command %q", command)
	}
	available, err := catalog.ListWorkspaces()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, value := range available {
		if value.ID == "" || ids[value.ID] {
			return fmt.Errorf("workspace list returned missing or duplicate ids")
		}
		ids[value.ID] = true
	}
	for _, entry := range set.Entries {
		if !ids[entry.ID] {
			return fmt.Errorf("workspace %q (%s) is not accessible in the selected Multica profile; no changes applied", entry.Key, entry.ID)
		}
	}
	controllers := make([]reconcile.Reconciler, len(set.Entries))
	plans := make([][]model.Change, len(set.Entries))
	// Preflight all workspaces before the first write. This is not a server
	// transaction: failures/races during apply can still leave partial changes.
	for i, entry := range set.Entries {
		b := factory(entry.ID)
		if b == nil {
			return fmt.Errorf("workspace %q has no backend", entry.Key)
		}
		controllers[i] = reconcile.Reconciler{Backend: b}
		plans[i], err = controllers[i].Plan(entry.Project)
		if err != nil {
			return fmt.Errorf("workspace %q preflight: %w", entry.Key, err)
		}
	}
	for i, entry := range set.Entries {
		fmt.Fprintf(stdout, "\nWorkspace %s (%s):\n", entry.Key, entry.ID)
		if command == "plan" {
			printPlan(stdout, plans[i])
			continue
		}
		if err := controllers[i].Apply(entry.Project, func(c model.Change) {
			fmt.Fprintln(stdout, reconcile.FormatChange(c))
		}); err != nil {
			return fmt.Errorf("workspace %q apply: %w (earlier operations may already have been applied)", entry.Key, err)
		}
	}
	if command == "apply" {
		fmt.Fprintf(stdout, "Apply complete for %d workspace(s).\n", len(set.Entries))
	}
	return nil
}
