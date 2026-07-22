package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func writeSnapshot(root string, value snapshot) error {
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		return fmt.Errorf("create agents directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		return fmt.Errorf("create skills directory: %w", err)
	}
	if err := writeYAML(filepath.Join(root, "multica.yaml"), value.manifest); err != nil {
		return err
	}

	for _, skill := range value.skills {
		directory := filepath.Join(root, "skills", skill.directory)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create skill directory %s: %w", directory, err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(skill.content), 0o644); err != nil {
			return fmt.Errorf("write skill SKILL.md: %w", err)
		}
		for _, file := range skill.files {
			target := filepath.Join(directory, filepath.FromSlash(file.Path))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create skill file directory: %w", err)
			}
			if err := os.WriteFile(target, []byte(file.Content), 0o644); err != nil {
				return fmt.Errorf("write skill file %s: %w", file.Path, err)
			}
		}
	}

	for _, agent := range value.agents {
		directory := filepath.Join(root, "agents", agent.directory)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create agent directory %s: %w", directory, err)
		}
		if err := os.WriteFile(filepath.Join(directory, "AGENT.md"), []byte(agent.instructions), 0o644); err != nil {
			return fmt.Errorf("write agent instructions: %w", err)
		}
		if err := writeYAML(filepath.Join(directory, "agent.yaml"), agent.document); err != nil {
			return err
		}
	}
	return nil
}

func writeYAML(target string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", target, err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}

func validateTarget(target string, force bool) error {
	if filepath.Dir(target) == target {
		return fmt.Errorf("refusing to export directly into filesystem root %s", target)
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect output directory %s: %w", target, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("output path %s must be a directory, not a file or symlink", target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return fmt.Errorf("read output directory %s: %w", target, err)
	}
	if len(entries) > 0 && !force {
		return fmt.Errorf("output directory %s is not empty; choose another directory or pass --force", target)
	}
	return nil
}

func installSnapshot(staging, target string, force bool) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		if err := os.Rename(staging, target); err != nil {
			return fmt.Errorf("install export at %s: %w", target, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect output directory %s: %w", target, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output path %s is not a directory", target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return fmt.Errorf("read output directory %s: %w", target, err)
	}
	if len(entries) == 0 {
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("replace empty output directory %s: %w", target, err)
		}
		if err := os.Rename(staging, target); err != nil {
			return fmt.Errorf("install export at %s: %w", target, err)
		}
		return nil
	}
	if !force {
		return fmt.Errorf("output directory %s is not empty", target)
	}

	for _, name := range generatedPaths {
		if err := os.RemoveAll(filepath.Join(target, name)); err != nil {
			return fmt.Errorf("remove previous generated path %s: %w", name, err)
		}
	}
	for _, name := range generatedPaths {
		if err := os.Rename(filepath.Join(staging, name), filepath.Join(target, name)); err != nil {
			return fmt.Errorf("install generated path %s: %w", name, err)
		}
	}
	return nil
}
