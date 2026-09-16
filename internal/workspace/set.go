// Package workspace routes independent, ordinary declarations to explicit
// workspace IDs. It does not create workspaces or share resource identities.
package workspace

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"gopkg.in/yaml.v3"
)

const APIVersion = "multica-declarative/v1alpha1"

// A key is exactly one portable directory component, not a relative path.
var directoryKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

type Target struct {
	ID string `yaml:"id"`
}

type Manifest struct {
	APIVersion string            `yaml:"apiVersion"`
	Secrets    string            `yaml:"secrets,omitempty"`
	Workspaces map[string]Target `yaml:"workspaces"`
}

type Entry struct {
	Key, ID, ConfigPath string
	Project            model.Project
}

type Set struct {
	ManifestPath string
	Entries      []Entry
}

func ValidKey(key string) bool { return directoryKey.MatchString(key) }

// Read reads only routing metadata, never resource or secret files. A nil
// manifest means a legacy single-workspace declaration, not an empty set.
func Read(path string) (*Manifest, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest must be a regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("parse workspace manifest: %w", err)
	}
	hasSet := false
	if len(node.Content) == 1 && node.Content[0].Kind == yaml.MappingNode {
		fields := node.Content[0].Content
		for i := 0; i < len(fields); i += 2 {
			if fields[i].Value == "workspaces" {
				hasSet = true
			}
		}
	}
	if !hasSet {
		return nil, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode workspace set: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("workspace set must contain exactly one YAML document")
	}
	if manifest.APIVersion != APIVersion {
		return nil, fmt.Errorf("unsupported workspace set apiVersion %q", manifest.APIVersion)
	}
	if manifest.Secrets != "" && manifest.Secrets != "include" && manifest.Secrets != "omit" {
		return nil, fmt.Errorf("secrets must be include or omit")
	}
	if len(manifest.Workspaces) == 0 {
		return nil, fmt.Errorf("workspaces must be a non-empty mapping")
	}
	ids := map[string]string{}
	for _, key := range manifest.Keys() {
		target := manifest.Workspaces[key]
		if !ValidKey(key) {
			return nil, fmt.Errorf("unsafe workspace directory key %q", key)
		}
		if target.ID == "" || strings.ContainsAny(target.ID, " \t\r\n") {
			return nil, fmt.Errorf("workspace %q requires an explicit non-empty id", key)
		}
		if previous, exists := ids[target.ID]; exists {
			return nil, fmt.Errorf("workspaces %q and %q target the same id", previous, key)
		}
		ids[target.ID] = key
	}
	return &manifest, nil
}

func (m *Manifest) Keys() []string {
	keys := make([]string, 0, len(m.Workspaces))
	for key := range m.Workspaces {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Enclosing detects the supported workspaces/<key>/multica.yaml layout. This
// also pins a directly selected child to its parent routing and secrets policy.
func Enclosing(path string) (*Manifest, string, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", "", err
	}
	child := filepath.Dir(absolute)
	collection := filepath.Dir(child)
	if filepath.Base(absolute) != "multica.yaml" || filepath.Base(collection) != "workspaces" {
		return nil, "", "", nil
	}
	parent := filepath.Join(filepath.Dir(collection), "multica.yaml")
	manifest, err := Read(parent)
	if os.IsNotExist(err) {
		return nil, "", "", nil
	}
	if err != nil || manifest == nil {
		return nil, "", "", err
	}
	key := filepath.Base(child)
	if _, ok := manifest.Workspaces[key]; !ok {
		return nil, "", "", fmt.Errorf("workspace directory %q is not declared in %s", key, parent)
	}
	if err := CheckDirectory(filepath.Dir(parent), child); err != nil {
		return nil, "", "", err
	}
	return manifest, parent, key, nil
}

// Load validates every selected child before the caller can contact Multica.
// Each child has its own name/reference namespace and runtime selectors.
func Load(path string, options config.LoadOptions) (*Set, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	manifest, err := Read(absolute)
	if err != nil {
		return nil, err
	}
	selected := ""
	if manifest == nil {
		manifest, absolute, selected, err = Enclosing(absolute)
		if err != nil || manifest == nil {
			return nil, err
		}
	}
	root := filepath.Dir(absolute)
	if err := CheckDirectory(root, filepath.Join(root, "workspaces")); err != nil {
		return nil, err
	}
	if manifest.Secrets == "omit" {
		options.WithoutSecrets = true
	}
	entries, err := os.ReadDir(filepath.Join(root, "workspaces"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("workspace collection contains symlink %q", entry.Name())
		}
		if entry.IsDir() {
			if _, ok := manifest.Workspaces[entry.Name()]; !ok {
				return nil, fmt.Errorf("workspace directory %q is not declared in %s", entry.Name(), absolute)
			}
		}
	}
	set := &Set{ManifestPath: absolute}
	for _, key := range manifest.Keys() {
		if selected != "" && selected != key {
			continue
		}
		directory := filepath.Join(root, "workspaces", key)
		if err := CheckDirectory(root, directory); err != nil {
			return nil, fmt.Errorf("workspace %q: %w", key, err)
		}
		childPath := filepath.Join(directory, "multica.yaml")
		if nested, err := Read(childPath); err != nil {
			return nil, fmt.Errorf("workspace %q: %w", key, err)
		} else if nested != nil {
			return nil, fmt.Errorf("workspace %q must contain a single-workspace manifest, not another set", key)
		}
		project, err := config.LoadWithOptions(childPath, options)
		if err != nil {
			return nil, fmt.Errorf("workspace %q: %w", key, err)
		}
		set.Entries = append(set.Entries, Entry{Key: key, ID: manifest.Workspaces[key].ID, ConfigPath: childPath, Project: project})
	}
	return set, nil
}

// CheckDirectory rejects symlinks and non-directories within the selected root.
func CheckDirectory(root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes workspace root: %s", path)
	}
	current := root
	parts := []string{"."}
	if relative != "." {
		parts = append(parts, strings.Split(relative, string(filepath.Separator))...)
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace path must be a directory, not a symlink: %s", current)
		}
	}
	return nil
}
