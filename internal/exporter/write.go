package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func writeSnapshot(root string, v snapshot) error {
	for _, dir := range []string{"agents", "skills", "squads", "runtime-profiles"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			return err
		}
	}
	if err := writeYAML(filepath.Join(root, "multica.yaml"), v.manifest); err != nil {
		return err
	}
	for _, s := range v.skills {
		dir := filepath.Join(root, "skills", s.directory)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(s.content), 0644); err != nil {
			return err
		}
		for _, f := range s.files {
			target := filepath.Join(dir, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(target, []byte(f.Content), 0644); err != nil {
				return err
			}
		}
	}
	for _, a := range v.agents {
		dir := filepath.Join(root, "agents", a.directory)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(a.instructions), 0644); err != nil {
			return err
		}
		if len(a.avatar) > 0 {
			if err := os.WriteFile(filepath.Join(dir, a.avatarName), a.avatar, 0644); err != nil {
				return err
			}
		}
		if err := writeYAML(filepath.Join(dir, "agent.yaml"), a.document); err != nil {
			return err
		}
	}
	for _, s := range v.squads {
		dir := filepath.Join(root, "squads", s.directory)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if s.document.InstructionsFile != "" {
			if err := os.WriteFile(filepath.Join(dir, "SQUAD.md"), []byte(s.instructions), 0644); err != nil {
				return err
			}
		}
		if err := writeYAML(filepath.Join(dir, "squad.yaml"), s.document); err != nil {
			return err
		}
	}
	for _, p := range v.profiles {
		if err := writeYAML(filepath.Join(root, "runtime-profiles", p.directory+".yaml"), p.document); err != nil {
			return err
		}
	}
	return nil
}
func writeYAML(target string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", target, err)
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	return nil
}
func validateTarget(target string, force bool) error {
	if filepath.Dir(target) == target {
		return fmt.Errorf("refusing to export into filesystem root %s", target)
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("output path must be a directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	if len(entries) > 0 && !force {
		return fmt.Errorf("output directory %s is not empty; pass --force", target)
	}
	return nil
}
func installSnapshot(staging, target string, force bool) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return os.Rename(staging, target)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("output path is not a directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		if err := os.Remove(target); err != nil {
			return err
		}
		return os.Rename(staging, target)
	}
	if !force {
		return fmt.Errorf("output directory is not empty")
	}
	for _, name := range generatedPaths {
		if err := os.RemoveAll(filepath.Join(target, name)); err != nil {
			return err
		}
	}
	for _, name := range generatedPaths {
		source := filepath.Join(staging, name)
		if _, err := os.Stat(source); os.IsNotExist(err) {
			continue
		}
		if err := os.Rename(source, filepath.Join(target, name)); err != nil {
			return err
		}
	}
	return nil
}
