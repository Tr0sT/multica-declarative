package exporter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const teamBridgeGeneratedToken = "credentials/bot-token"
const teamBridgeGeneratedManifest = ".multica-generated.json"

type teamBridgeOwnership struct {
	Version int      `json:"version"`
	Files   []string `json:"files"`
}

type preservedTeamBridgeFile struct {
	source, relative string
	info             fs.FileInfo
}

// Secretless workspace refresh must not copy old generated token contents into
// its staging tree. Ownership is read from metadata, never from a token file.
func generatedTeamBridgeTokenFiles(root string) (map[string]bool, error) {
	directories, err := existingResourceDirectories(root, "teambridge", "bot.yaml", yamlName)
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	for _, directory := range directories {
		owned, err := existingTeamBridgeGeneratedFiles(filepath.Join(root, "teambridge", directory))
		if err != nil {
			return nil, err
		}
		for _, file := range owned {
			if file == teamBridgeGeneratedToken {
				files[filepath.Join("teambridge", directory, filepath.FromSlash(file))] = true
			}
		}
	}
	return files, nil
}

func generatedTeamBridgeFiles(v exportedTeamBridge) []string {
	files := []string{"bot.yaml", "observed.json"}
	files = append(files, v.document.DeniedReply.ImageFiles...)
	if v.token != "" {
		files = append(files, teamBridgeGeneratedToken)
	}
	return files
}

func preserveTeamBridgeDirectories(target string, s *snapshot) error {
	if !s.teambridgeEnabled {
		if _, err := os.Lstat(filepath.Join(target, "teambridge")); err == nil {
			return fmt.Errorf("output already contains TeamBridge declarations; refresh with --with-teambridge or use a new destination to avoid losing or leaving stale integration settings")
		} else if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := preserveResourceDirectories(target, "teambridge", "bot.yaml", s.teambridge,
		func(v *exportedTeamBridge) (string, *string) { return v.document.Name, &v.directory }, yamlName); err != nil {
		return err
	}
	existing, err := existingResourceDirectories(target, "teambridge", "bot.yaml", yamlName)
	if err != nil {
		return err
	}
	owned, generated := map[string]bool{}, map[string]bool{}
	for _, v := range s.teambridge {
		for _, file := range append(generatedTeamBridgeFiles(v), teamBridgeGeneratedManifest) {
			generated[filepath.Join(v.directory, filepath.FromSlash(file))] = true
		}
	}
	for _, dir := range existing {
		root := filepath.Join(target, "teambridge", dir)
		files, err := existingTeamBridgeGeneratedFiles(root)
		if err != nil {
			return err
		}
		for _, file := range files {
			owned[filepath.Join(dir, filepath.FromSlash(file))] = true
		}
	}
	root := filepath.Join(target, "teambridge")
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("existing TeamBridge tree must not contain symlinks or special files")
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if owned[relative] {
			if info.IsDir() {
				return fmt.Errorf("previously generated TeamBridge file is now a directory")
			}
			return nil
		}
		if generated[relative] {
			return fmt.Errorf("refresh would replace an unowned TeamBridge file or directory; move the conflicting local file before refreshing")
		}
		s.teambridgePreserved = append(s.teambridgePreserved, preservedTeamBridgeFile{current, relative, info})
		return nil
	})
}

// Only generated names can be claimed by the manifest. It contains paths, never
// secret values or fingerprints. Legacy snapshots own public files and images,
// but never claim a manually supplied accessTokenFile.
func existingTeamBridgeGeneratedFiles(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "bot.yaml"))
	if err != nil {
		return nil, err
	}
	var declaration struct {
		DeniedReply struct {
			ImageFiles []string `yaml:"imageFiles"`
		} `yaml:"deniedReply"`
	}
	if yaml.Unmarshal(data, &declaration) != nil {
		return nil, fmt.Errorf("cannot read existing TeamBridge generated-file references")
	}
	files := []string{"bot.yaml", "observed.json"}
	for _, file := range declaration.DeniedReply.ImageFiles {
		if !validTeamBridgeGeneratedImage(file) {
			return nil, fmt.Errorf("existing TeamBridge image reference is not a generated image path; preserve the declaration outside the export tree before refreshing")
		}
		files = append(files, file)
	}
	manifest := filepath.Join(root, teamBridgeGeneratedManifest)
	info, err := os.Lstat(manifest)
	if os.IsNotExist(err) {
		return files, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, fmt.Errorf("invalid TeamBridge generated-file ownership manifest")
	}
	data, err = os.ReadFile(manifest)
	var ownership teamBridgeOwnership
	if err != nil || json.Unmarshal(data, &ownership) != nil || ownership.Version != 1 {
		return nil, fmt.Errorf("invalid TeamBridge generated-file ownership manifest")
	}
	for _, file := range ownership.Files {
		if file != "bot.yaml" && file != "observed.json" && file != teamBridgeGeneratedToken && !validTeamBridgeGeneratedImage(file) {
			return nil, fmt.Errorf("TeamBridge ownership manifest contains an unsupported generated path")
		}
		files = append(files, file)
	}
	return append(files, teamBridgeGeneratedManifest), nil
}

var teamBridgeGeneratedImage = regexp.MustCompile(`^images/[0-9]{3,}\.(png|jpg|gif|webp)$`)

func validTeamBridgeGeneratedImage(file string) bool {
	return path.Clean(file) == file && !strings.Contains(file, "\\") && teamBridgeGeneratedImage.MatchString(file)
}

func preserveTeamBridgeFiles(staging string, files []preservedTeamBridgeFile) error {
	for _, file := range files {
		target := filepath.Join(staging, "teambridge", file.relative)
		if file.info.IsDir() {
			if err := os.MkdirAll(target, file.info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		input, err := os.Open(file.source)
		if err != nil {
			return fmt.Errorf("cannot preserve a local TeamBridge file")
		}
		info, statErr := input.Stat()
		if statErr != nil || !os.SameFile(file.info, info) || !info.Mode().IsRegular() {
			input.Close()
			return fmt.Errorf("local TeamBridge file changed while preparing the snapshot")
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.info.Mode().Perm())
		if err != nil {
			input.Close()
			return fmt.Errorf("cannot preserve a local TeamBridge file without overwriting generated data")
		}
		_, copyErr := io.Copy(output, input)
		if err := errors.Join(copyErr, output.Close(), input.Close()); err != nil {
			return fmt.Errorf("cannot preserve a local TeamBridge file")
		}
	}
	return nil
}
