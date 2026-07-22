package exporter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func preserveAgentDirectories(target string, agents []exportedAgent) error {
	existing, err := existingAgentDirectories(target)
	if err != nil {
		return err
	}
	for i := range agents {
		if directory, ok := existing[agents[i].document.Name]; ok {
			agents[i].directory = directory
		}
	}

	used := map[string]string{}
	for _, agent := range agents {
		directory := filepath.Clean(agent.directory)
		if directory == "." || filepath.IsAbs(directory) || directory == ".." || strings.HasPrefix(directory, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe export directory %q for agent %q", agent.directory, agent.document.Name)
		}
		if previous, ok := used[directory]; ok {
			return fmt.Errorf("agents %q and %q would export to the same directory %s", previous, agent.document.Name, filepath.Join("agents", directory))
		}
		used[directory] = agent.document.Name
	}
	return nil
}

func existingAgentDirectories(target string) (map[string]string, error) {
	directories := map[string]string{}
	root := filepath.Join(target, "agents")
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return directories, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat existing agents directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("existing agents path must be a directory: %s", root)
	}

	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("existing agents tree must not contain symlinks: %s", current)
		}
		if !entry.IsDir() {
			return nil
		}

		declaration := filepath.Join(current, "agent.yaml")
		declarationInfo, statErr := os.Lstat(declaration)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil
			}
			return fmt.Errorf("stat existing agent declaration %s: %w", declaration, statErr)
		}
		if !declarationInfo.Mode().IsRegular() {
			return fmt.Errorf("existing agent declaration must be a regular file: %s", declaration)
		}

		data, readErr := os.ReadFile(declaration)
		if readErr != nil {
			return fmt.Errorf("read existing agent declaration %s: %w", declaration, readErr)
		}
		var identity struct {
			Name string `yaml:"name"`
		}
		if decodeErr := yaml.Unmarshal(data, &identity); decodeErr != nil {
			return fmt.Errorf("parse existing agent declaration %s: %w", declaration, decodeErr)
		}
		identity.Name = strings.TrimSpace(identity.Name)
		if identity.Name == "" {
			return fmt.Errorf("existing agent declaration %s must contain name", declaration)
		}
		relative, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return fmt.Errorf("resolve existing agent directory %s: %w", current, relErr)
		}
		if previous, ok := directories[identity.Name]; ok {
			return fmt.Errorf("duplicate existing agent name %q in %s and %s", identity.Name, filepath.Join(root, previous), current)
		}
		directories[identity.Name] = relative
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return directories, nil
}

func writeSnapshot(root string, v snapshot) error {
	for _, dir := range []string{"agents", "skills", "squads"} {
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
