package backend

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestTeamBridgeTokenCLITransfersPrivateFilesAndPreservesScope(t *testing.T) {
	const secret = "synthetic-exact-bot-secret"
	var exportFile, importFile string
	cli := (&CLI{Binary: "selected-fork", Runner: teamBridgeRunner(func(binary string, args []string) ([]byte, []byte, error) {
		if binary != "selected-fork" || !slices.Equal(args[:4], []string{"--profile", "synthetic-profile", "--workspace-id", tbWorkspace}) || strings.Contains(strings.Join(args, " "), secret) {
			t.Fatal("token command lost scope or exposed a value in arguments")
		}
		if args[4] != "teambridge" || args[5] != "token" || args[7] != tbInstall || teamBridgeArg(args, "--output") != "json" {
			t.Fatal("unexpected token command")
		}
		result := map[string]any{"installation_id": tbInstall, "workspace_id": tbWorkspace}
		switch args[6] {
		case "export":
			exportFile = teamBridgeArg(args, "--output-file")
			if _, err := os.Lstat(exportFile); !os.IsNotExist(err) {
				t.Fatal("CLI output file already exists")
			}
			if err := os.WriteFile(exportFile, []byte(secret), 0600); err != nil {
				t.Fatal(err)
			}
			teamBridgeAssertPrivate(t, exportFile)
			result["exported"] = true
		case "import":
			importFile = teamBridgeArg(args, "--token-file")
			teamBridgeAssertPrivate(t, importFile)
			data, err := os.ReadFile(importFile)
			if err != nil || string(data) != secret {
				t.Fatal("import did not use exact token file")
			}
			result["updated"] = false
		default:
			t.Fatal("unexpected token operation")
		}
		return teamBridgeJSONBytes(t, result), nil, nil
	})}).WithScope("synthetic-profile", tbWorkspace)
	token, err := cli.ExportTeamBridgeToken(tbInstall)
	if err != nil || token != secret {
		t.Fatal("token export failed", err)
	}
	teamBridgeAssertRemoved(t, exportFile)
	if err := cli.ImportTeamBridgeToken(tbInstall, token); err != nil {
		t.Fatal(err)
	}
	teamBridgeAssertRemoved(t, importFile)
}

func TestTeamBridgeTokenCLIRejectsUnsafeOutputsWithoutLeaks(t *testing.T) {
	const secret = "synthetic-output-leak-sentinel"
	for _, scenario := range []string{"error", "json", "wrong-installation", "wrong-workspace", "missing-confirmation", "missing-file", "empty", "oversized", "public", "symlink", "control"} {
		t.Run(scenario, func(t *testing.T) {
			if runtime.GOOS == "windows" && (scenario == "public" || scenario == "symlink") {
				t.Skip("POSIX-specific file protection")
			}
			var file string
			cli := (&CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
				file = teamBridgeArg(args, "--output-file")
				result := map[string]any{"installation_id": tbInstall, "workspace_id": tbWorkspace, "exported": true}
				data, mode := []byte(secret), os.FileMode(0600)
				switch scenario {
				case "error":
					return nil, []byte(secret), errors.New(secret)
				case "json":
					return []byte(secret), nil, nil
				case "wrong-installation":
					result["installation_id"] = tbAgent
				case "wrong-workspace":
					result["workspace_id"] = tbAgent
				case "missing-confirmation":
					delete(result, "exported")
				case "missing-file":
					return teamBridgeJSONBytes(t, result), nil, nil
				case "empty":
					data = nil
				case "oversized":
					data = []byte(strings.Repeat("X", 16385))
				case "public":
					mode = 0644
				case "control":
					data = []byte(secret + "\n")
				case "symlink":
					target := filepath.Join(t.TempDir(), "sentinel")
					if err := os.WriteFile(target, data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, file); err != nil {
						t.Fatal(err)
					}
					return teamBridgeJSONBytes(t, result), nil, nil
				}
				if err := os.WriteFile(file, data, mode); err != nil {
					t.Fatal(err)
				}
				return teamBridgeJSONBytes(t, result), nil, nil
			})}).WithScope("", tbWorkspace)
			if token, err := cli.ExportTeamBridgeToken(tbInstall); err == nil || token != "" || strings.Contains(err.Error(), secret) {
				t.Fatal("unsafe export accepted or disclosed a credential")
			}
			teamBridgeAssertRemoved(t, file)
		})
	}
}

func TestTeamBridgeTokenImportCleansInputsOnMalformedResponseAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var file string
		const secret = "synthetic-import-error-sentinel"
		cli := &CLI{Runner: teamBridgeRunner(func(_ string, args []string) ([]byte, []byte, error) {
			file = teamBridgeArg(args, "--token-file")
			teamBridgeAssertPrivate(t, file)
			if fail {
				return nil, []byte(secret), errors.New(secret)
			}
			return []byte(`{"installation_id":"` + tbInstall + `","workspace_id":"` + tbWorkspace + `","updated":"` + secret + `"}`), nil, nil
		})}
		if err := cli.ImportTeamBridgeToken(tbInstall, secret); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("import accepted failure or leaked secret")
		}
		teamBridgeAssertRemoved(t, file)
	}
}

func TestTeamBridgeTemporaryTokenPathsCanonicalizeSystemAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR is POSIX-specific")
	}
	root := t.TempDir()
	actual, alias := filepath.Join(root, "actual"), filepath.Join(root, "alias")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	dir, err := teamBridgeTempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || dir != resolved || !strings.HasPrefix(dir, actual+string(filepath.Separator)) {
		t.Fatal("temporary path retains a symlinked ancestor")
	}
}
