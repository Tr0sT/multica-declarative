package backend

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Tr0sT/multica-declarative/internal/model"
)

var _ TeamBridgeTokenOperations = (*CLI)(nil)

func (c *CLI) ExportTeamBridgeToken(id string) (string, error) {
	if !teamBridgeUUID.MatchString(id) {
		return "", fmt.Errorf("TeamBridge token export requires a full installation UUID")
	}
	dir, err := teamBridgeTempDir()
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "bot-token")
	var response struct {
		InstallationID string `json:"installation_id"`
		WorkspaceID    string `json:"workspace_id"`
		Exported       bool   `json:"exported"`
	}
	if err := c.teamBridgeJSON(&response, "token", "export", id, "--output-file", file, "--output", "json"); err != nil {
		return "", err
	}
	if !response.Exported || response.InstallationID != id || !c.teamBridgeTokenWorkspace(response.WorkspaceID) {
		return "", fmt.Errorf("TeamBridge CLI did not confirm token export for the requested installation and workspace")
	}
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() > model.MaxTeamBridgeTokenBytes ||
		(runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return "", fmt.Errorf("TeamBridge CLI did not write a private regular token file of at most 16 KiB")
	}
	f, err := os.Open(file)
	if err != nil {
		return "", fmt.Errorf("cannot read exported TeamBridge token file")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", fmt.Errorf("exported TeamBridge token file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, model.MaxTeamBridgeTokenBytes+1))
	if err != nil || !model.ValidTeamBridgeToken(string(data)) {
		return "", fmt.Errorf("TeamBridge CLI exported an empty, oversized or invalid token")
	}
	return string(data), nil
}

func (c *CLI) ImportTeamBridgeToken(id, token string) error {
	if !teamBridgeUUID.MatchString(id) || !model.ValidTeamBridgeToken(token) {
		return fmt.Errorf("TeamBridge token import requires an installation UUID and a valid token")
	}
	dir, err := teamBridgeTempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "bot-token")
	if err := teamBridgeWritePrivate(file, []byte(token)); err != nil {
		return err
	}
	var response struct {
		InstallationID string `json:"installation_id"`
		WorkspaceID    string `json:"workspace_id"`
		Updated        *bool  `json:"updated"`
	}
	if err := c.teamBridgeJSON(&response, "token", "import", id, "--token-file", file, "--output", "json"); err != nil {
		return err
	}
	if response.Updated == nil || response.InstallationID != id || !c.teamBridgeTokenWorkspace(response.WorkspaceID) {
		return fmt.Errorf("TeamBridge CLI did not confirm token import for the requested installation and workspace")
	}
	return nil
}

func (c *CLI) teamBridgeTokenWorkspace(id string) bool {
	return teamBridgeUUID.MatchString(id) && (c.teamBridgeExplicitWorkspace() == "" || id == c.teamBridgeExplicitWorkspace())
}
