package exporter

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/workspace"
)

type WorkspaceExporter struct {
	Catalog    backend.WorkspaceLister
	BackendFor func(workspaceID string) backend.Backend
}

type WorkspaceResult struct {
	Result
	Workspaces int
}

// Export reads all accessible workspaces. No existing output is replaced until
// every workspace (including empty ones) has been read and validated locally.
func (e WorkspaceExporter) Export(options Options) (WorkspaceResult, error) {
	if e.Catalog == nil || e.BackendFor == nil {
		return WorkspaceResult{}, fmt.Errorf("workspace catalog and scoped backend factory are required")
	}
	out := strings.TrimSpace(options.OutputDir)
	if out == "" {
		out = defaultOutputDir
	}
	absolute, err := filepath.Abs(out)
	if err != nil {
		return WorkspaceResult{}, err
	}
	if err := validateTarget(absolute, options.Force); err != nil {
		return WorkspaceResult{}, err
	}
	previous, err := workspace.Read(filepath.Join(absolute, "multica.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return WorkspaceResult{}, err
	}
	if err == nil && previous == nil {
		return WorkspaceResult{}, fmt.Errorf("output contains a single-workspace manifest; export the workspace set into a different directory")
	}
	if previous == nil {
		if _, err := os.Lstat(filepath.Join(absolute, "workspaces")); !os.IsNotExist(err) {
			return WorkspaceResult{}, fmt.Errorf("refusing to replace workspaces/ without a workspace-set manifest")
		}
	} else if previous.Secrets == "omit" {
		options.WithoutSecrets = true
	}

	available, err := e.Catalog.ListWorkspaces()
	if err != nil {
		return WorkspaceResult{}, err
	}
	if len(available) == 0 {
		return WorkspaceResult{}, fmt.Errorf("no accessible workspaces to export")
	}
	manifest := workspace.Manifest{APIVersion: workspace.APIVersion, Workspaces: map[string]workspace.Target{}}
	if options.WithoutSecrets {
		manifest.Secrets = "omit"
	}
	oldKeys := map[string]string{}
	if previous != nil {
		for key, target := range previous.Workspaces {
			oldKeys[target.ID] = key
		}
	}
	ids, slugs := map[string]bool{}, map[string]bool{}
	for _, value := range available {
		if value.ID == "" || strings.ContainsAny(value.ID, " \t\r\n") || !workspace.ValidKey(value.Slug) {
			return WorkspaceResult{}, fmt.Errorf("workspace list returned an invalid id or directory slug")
		}
		if ids[value.ID] || slugs[value.Slug] {
			return WorkspaceResult{}, fmt.Errorf("workspace list returned duplicate ids or slugs")
		}
		ids[value.ID], slugs[value.Slug] = true, true
		key := value.Slug
		if old, ok := oldKeys[value.ID]; ok {
			key = old
		}
		if _, exists := manifest.Workspaces[key]; exists {
			return WorkspaceResult{}, fmt.Errorf("workspace directory collision for %q", key)
		}
		manifest.Workspaces[key] = workspace.Target{ID: value.ID}
	}
	for id, key := range oldKeys {
		if !ids[id] {
			return WorkspaceResult{}, fmt.Errorf("previously exported workspace %q is no longer accessible; archive its directory and remove its manifest entry explicitly before refreshing", key)
		}
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return WorkspaceResult{}, err
	}
	staging, err := os.MkdirTemp(filepath.Dir(absolute), ".multica-workspace-set-*")
	if err != nil {
		return WorkspaceResult{}, err
	}
	defer os.RemoveAll(staging)
	if err := os.MkdirAll(filepath.Join(staging, "workspaces"), 0755); err != nil {
		return WorkspaceResult{}, err
	}
	if previous != nil {
		// Preserve non-generated per-workspace files and resource grouping. The
		// ordinary installer refreshes only generated paths inside this copy.
		if err := copyWorkspaceTree(filepath.Join(absolute, "workspaces"), filepath.Join(staging, "workspaces")); err != nil {
			return WorkspaceResult{}, err
		}
	}
	result := WorkspaceResult{Result: Result{OutputDir: absolute}, Workspaces: len(available)}
	for _, key := range manifest.Keys() {
		b := e.BackendFor(manifest.Workspaces[key].ID)
		if b == nil {
			return WorkspaceResult{}, fmt.Errorf("workspace %q has no backend", key)
		}
		snap, err := (Exporter{Backend: b}).readSnapshot(options)
		if err != nil {
			return WorkspaceResult{}, fmt.Errorf("workspace %q: %w", key, err)
		}
		target := filepath.Join(staging, "workspaces", key)
		if err := preserveSnapshotDirectories(target, &snap); err != nil {
			return WorkspaceResult{}, fmt.Errorf("workspace %q: %w", key, err)
		}
		childStage, err := os.MkdirTemp(staging, ".workspace-*")
		if err != nil {
			return WorkspaceResult{}, err
		}
		if err := writeSnapshot(childStage, snap); err != nil {
			return WorkspaceResult{}, fmt.Errorf("workspace %q: %w", key, err)
		}
		if err := installSnapshot(childStage, target, true); err != nil {
			return WorkspaceResult{}, fmt.Errorf("workspace %q: %w", key, err)
		}
		if err := os.RemoveAll(childStage); err != nil {
			return WorkspaceResult{}, err
		}
		result.Skills += len(snap.skills)
		result.Agents += len(snap.agents)
		result.Squads += len(snap.squads)
		result.Projects += len(snap.projects)
		result.Autopilots += len(snap.autopilots)
		result.Runtimes += len(snap.manifest.Runtimes)
		for _, warning := range snap.warnings {
			result.Warnings = append(result.Warnings, fmt.Sprintf("workspace %q: %s", key, warning))
		}
	}
	if err := writeYAML(filepath.Join(staging, "multica.yaml"), manifest); err != nil {
		return WorkspaceResult{}, err
	}
	if _, err := workspace.Load(filepath.Join(staging, "multica.yaml"), config.LoadOptions{}); err != nil {
		return WorkspaceResult{}, fmt.Errorf("validate generated workspace set: %w", err)
	}
	if err := installWorkspaceSet(staging, absolute, options.Force); err != nil {
		return WorkspaceResult{}, err
	}
	return result, nil
}

func copyWorkspaceTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("workspace export tree contains a symlink or special file: %s", path)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		return errors.Join(copyErr, output.Close())
	})
}

// Install only the two generated root paths. Keep unrelated root files/.git;
// roll back on ordinary install errors and retain backups if recovery fails.
func installWorkspaceSet(staging, target string, force bool) error {
	if err := validateTarget(target, force); err != nil {
		return err
	}
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return os.Rename(staging, target)
	}
	backup, err := os.MkdirTemp(filepath.Dir(target), ".multica-workspace-backup-*")
	if err != nil {
		return err
	}
	keepBackup := false
	defer func() {
		if !keepBackup {
			_ = os.RemoveAll(backup)
		}
	}()
	var moved, installed []string
	rollback := func(cause error) error {
		errs := []error{cause}
		for i := len(installed) - 1; i >= 0; i-- {
			if err := os.RemoveAll(filepath.Join(target, installed[i])); err != nil {
				errs = append(errs, err)
			}
		}
		for i := len(moved) - 1; i >= 0; i-- {
			if err := os.Rename(filepath.Join(backup, moved[i]), filepath.Join(target, moved[i])); err != nil {
				keepBackup = true
				errs = append(errs, fmt.Errorf("restore %s failed; backup retained at %s: %w", moved[i], backup, err))
			}
		}
		return errors.Join(errs...)
	}
	paths := []string{"multica.yaml", "workspaces"}
	for _, name := range paths {
		destination := filepath.Join(target, name)
		if _, err := os.Lstat(destination); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return rollback(err)
		}
		if err := os.Rename(destination, filepath.Join(backup, name)); err != nil {
			return rollback(err)
		}
		moved = append(moved, name)
	}
	for _, name := range paths {
		if err := os.Rename(filepath.Join(staging, name), filepath.Join(target, name)); err != nil {
			return rollback(err)
		}
		installed = append(installed, name)
	}
	return nil
}
