package exporter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func preserveSnapshotDirectories(target string, snapshot *snapshot) error {
	if err := preserveResourceDirectories(target, "skills", "SKILL.md", snapshot.skills,
		func(skill *exportedSkill) (string, *string) { return skill.name, &skill.directory }, skillName); err != nil {
		return err
	}
	if err := preserveResourceDirectories(target, "agents", "agent.yaml", snapshot.agents,
		func(agent *exportedAgent) (string, *string) { return agent.document.Name, &agent.directory }, yamlName); err != nil {
		return err
	}
	return preserveResourceDirectories(target, "squads", "squad.yaml", snapshot.squads,
		func(squad *exportedSquad) (string, *string) { return squad.document.Name, &squad.directory }, yamlName)
}

func preserveResourceDirectories[T any](target, collection, marker string, resources []T, location func(*T) (string, *string), parseName func([]byte) (string, error)) error {
	existing, err := existingResourceDirectories(target, collection, marker, parseName)
	if err != nil {
		return err
	}
	used := map[string]string{}
	for index := range resources {
		name, directory := location(&resources[index])
		if existingDirectory, ok := existing[name]; ok {
			*directory = existingDirectory
		}
		clean := filepath.Clean(*directory)
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe export directory %q for %s %q", *directory, collection, name)
		}
		if previous, ok := used[clean]; ok {
			return fmt.Errorf("%s %q and %q would export to the same directory %s", collection, previous, name, filepath.Join(collection, clean))
		}
		*directory = clean
		used[clean] = name
	}
	return nil
}

func existingResourceDirectories(target, collection, marker string, parseName func([]byte) (string, error)) (map[string]string, error) {
	directories := map[string]string{}
	root := filepath.Join(target, collection)
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return directories, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat existing %s directory: %w", collection, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("existing %s path must be a directory: %s", collection, root)
	}

	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("existing %s tree must not contain symlinks: %s", collection, current)
		}
		if !entry.IsDir() {
			return nil
		}

		declaration := filepath.Join(current, marker)
		declarationInfo, statErr := os.Lstat(declaration)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil
			}
			return fmt.Errorf("stat existing %s declaration %s: %w", collection, declaration, statErr)
		}
		if !declarationInfo.Mode().IsRegular() {
			return fmt.Errorf("existing %s declaration must be a regular file: %s", collection, declaration)
		}

		data, readErr := os.ReadFile(declaration)
		if readErr != nil {
			return fmt.Errorf("read existing %s declaration %s: %w", collection, declaration, readErr)
		}
		name, parseErr := parseName(data)
		if parseErr != nil {
			return fmt.Errorf("parse existing %s declaration %s: %w", collection, declaration, parseErr)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("existing %s declaration %s must contain name", collection, declaration)
		}
		relative, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return fmt.Errorf("resolve existing %s directory %s: %w", collection, current, relErr)
		}
		if previous, ok := directories[name]; ok {
			return fmt.Errorf("duplicate existing %s name %q in %s and %s", collection, name, filepath.Join(root, previous), current)
		}
		directories[name] = relative
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return directories, nil
}

func yamlName(data []byte) (string, error) {
	var identity struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &identity); err != nil {
		return "", err
	}
	return identity.Name, nil
}

func skillName(data []byte) (string, error) {
	_, frontmatter, valid := splitFrontmatter(string(data))
	if !valid {
		return "", fmt.Errorf("SKILL.md has invalid frontmatter")
	}
	name, _ := frontmatter["name"].(string)
	return name, nil
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
		if a.customEnv != nil {
			if err := writeJSON(filepath.Join(dir, customEnvFileName), a.customEnv); err != nil {
				return err
			}
		}
		if a.mcpConfig != nil {
			if err := writeJSON(filepath.Join(dir, mcpConfigFileName), a.mcpConfig); err != nil {
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
func writeJSON(target string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", target, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(target, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
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
	if len(entries) > 0 && !force {
		return fmt.Errorf("output directory is not empty")
	}
	backup, err := os.MkdirTemp(filepath.Dir(target), ".multica-backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(backup)

	moved := []string{}
	installed := []string{}
	rollback := func(cause error) error {
		errorsSeen := []error{cause}
		for index := len(installed) - 1; index >= 0; index-- {
			if removeErr := os.RemoveAll(filepath.Join(target, installed[index])); removeErr != nil {
				errorsSeen = append(errorsSeen, fmt.Errorf("remove partially installed %s: %w", installed[index], removeErr))
			}
		}
		for index := len(moved) - 1; index >= 0; index-- {
			name := moved[index]
			if restoreErr := os.Rename(filepath.Join(backup, name), filepath.Join(target, name)); restoreErr != nil {
				errorsSeen = append(errorsSeen, fmt.Errorf("restore previous %s: %w", name, restoreErr))
			}
		}
		return errors.Join(errorsSeen...)
	}

	for _, name := range generatedPaths {
		destination := filepath.Join(target, name)
		if _, statErr := os.Lstat(destination); os.IsNotExist(statErr) {
			continue
		} else if statErr != nil {
			return rollback(statErr)
		}
		if err := os.Rename(destination, filepath.Join(backup, name)); err != nil {
			return rollback(fmt.Errorf("back up existing %s: %w", name, err))
		}
		moved = append(moved, name)
	}
	for _, name := range generatedPaths {
		source := filepath.Join(staging, name)
		if _, statErr := os.Lstat(source); statErr != nil {
			return rollback(fmt.Errorf("stat generated %s: %w", name, statErr))
		}
		if err := os.Rename(source, filepath.Join(target, name)); err != nil {
			return rollback(fmt.Errorf("install generated %s: %w", name, err))
		}
		installed = append(installed, name)
	}
	return nil
}
