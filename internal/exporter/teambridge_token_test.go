package exporter

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
)

func teamBridgeTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(file string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestTeamBridgeTokenExportFailurePreservesCompletePreviousSnapshot(t *testing.T) {
	b := teamBridgeExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	e := Exporter{Backend: b}
	if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true}); err != nil {
		t.Fatal(err)
	}
	before := teamBridgeTree(t, out)
	b.agents[0].Instructions = "must not replace the previous snapshot"
	for _, scenario := range []string{"denied", "missing-command", "empty", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			b.tokenErr, b.token = nil, "synthetic-valid-token"
			switch scenario {
			case "denied":
				b.tokenErr = errors.New("admin access required")
			case "missing-command":
				b.tokenErr = errors.New("token command unsupported")
			case "empty":
				b.token = ""
			case "malformed":
				b.token = "invalid\ntoken"
			}
			if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true, Force: true}); err == nil {
				t.Fatal("incomplete full export accepted")
			}
			after := teamBridgeTree(t, out)
			if len(before) != len(after) {
				t.Fatal("failed export changed snapshot files")
			}
			for file, content := range before {
				if after[file] != content {
					t.Fatalf("failed export changed %s", file)
				}
			}
		})
	}
}

func TestTeamBridgeSecretlessNeverReadsTokensAndPublicArtifactsContainNoToken(t *testing.T) {
	b := teamBridgeExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	result, err := (Exporter{Backend: b}).Export(Options{OutputDir: out, WithTeamBridge: true})
	if err != nil {
		t.Fatal(err)
	}
	for file, content := range teamBridgeTree(t, out) {
		if filepath.ToSlash(file) != "teambridge/unity-developer/"+teamBridgeGeneratedToken && strings.Contains(content, b.token) {
			t.Fatalf("token leaked in %s", file)
		}
	}
	if strings.Contains(strings.Join(result.Warnings, " "), b.token) {
		t.Fatal("token leaked in diagnostics")
	}
	b.tokenCalls = 0
	b.tokenErr = errors.New("token operation must not run")
	if _, err := (Exporter{Backend: b}).Export(Options{OutputDir: out, WithTeamBridge: true, Force: true, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if b.tokenCalls != 0 {
		t.Fatal("secretless export queried token")
	}
	for file, content := range teamBridgeTree(t, out) {
		if strings.Contains(content, b.token) || strings.Contains(content, "accessTokenFile:") || strings.Contains(content, "accessTokenEnv:") {
			t.Fatalf("secretless generated artifact retained credentials: %s", file)
		}
	}
}

func TestTeamBridgeLegacyTokenFilesRemainUnownedDuringRefresh(t *testing.T) {
	for _, omit := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "omit"}[omit], func(t *testing.T) {
			b := teamBridgeExportFixture()
			out := filepath.Join(t.TempDir(), "snapshot")
			e := Exporter{Backend: b}
			if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true}); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(out, "teambridge/unity-developer")
			if err := os.Remove(filepath.Join(root, teamBridgeGeneratedManifest)); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, teamBridgeGeneratedToken)
			const userToken = "synthetic-user-owned-collision"
			if err := os.WriteFile(file, []byte(userToken), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := e.Export(Options{OutputDir: out, WithTeamBridge: true, Force: true, WithoutSecrets: omit})
			if !omit && (err == nil || !strings.Contains(err.Error(), "unowned")) {
				t.Fatal("full refresh silently overwrote a local token")
			}
			if omit && err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != userToken {
				t.Fatal("refresh destroyed an unowned token file")
			}
			if omit {
				data, err := os.ReadFile(filepath.Join(root, "bot.yaml"))
				if err != nil || bytes.Contains(data, []byte("accessToken")) {
					t.Fatal("omit retained a credential reference")
				}
			}
		})
	}
}

type teamBridgeWithoutTokenCapability struct {
	*fakeBackend
	backend.TeamBridgeOperations
}

func TestTeamBridgeFullExportRequiresTokenCapabilityButOmitDoesNot(t *testing.T) {
	b := teamBridgeExportFixture()
	legacy := teamBridgeWithoutTokenCapability{b.fakeBackend, b}
	if _, err := (Exporter{Backend: legacy}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "full"), WithTeamBridge: true}); err == nil || !strings.Contains(err.Error(), "token export") {
		t.Fatal("full export accepted backend without token support")
	}
	if _, err := (Exporter{Backend: legacy}).Export(Options{OutputDir: filepath.Join(t.TempDir(), "omit"), WithTeamBridge: true, WithoutSecrets: true}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretlessWorkspaceStagingExcludesOwnedTokensAndPreservesUnownedFiles(t *testing.T) {
	b := teamBridgeExportFixture()
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := (Exporter{Backend: b}).Export(Options{OutputDir: out, WithTeamBridge: true}); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join("teambridge", "unity-developer", "local-token")
	if err := os.WriteFile(filepath.Join(out, local), []byte("synthetic-unowned-token"), 0600); err != nil {
		t.Fatal(err)
	}
	skip, err := generatedTeamBridgeTokenFiles(out)
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "stage")
	if err := copyWorkspaceTree(out, stage, skip); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stage, "teambridge", "unity-developer", teamBridgeGeneratedToken)); !os.IsNotExist(err) {
		t.Fatal("old managed token copied into secretless staging")
	}
	if data, err := os.ReadFile(filepath.Join(stage, local)); err != nil || string(data) != "synthetic-unowned-token" {
		t.Fatal("unowned file lost during staging")
	}
}

func TestTeamBridgeRefreshRejectsUnsafeOwnershipAndSymlinksWithoutChanges(t *testing.T) {
	for _, scenario := range []string{"escape-manifest", "user-file-claim", "token-symlink", "directory-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			b := teamBridgeExportFixture()
			out := filepath.Join(t.TempDir(), "snapshot")
			e := Exporter{Backend: b}
			if _, err := e.Export(Options{OutputDir: out, WithTeamBridge: true}); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(out, "teambridge/unity-developer")
			manifest := filepath.Join(root, teamBridgeGeneratedManifest)
			switch scenario {
			case "escape-manifest":
				if err := os.WriteFile(manifest, []byte(`{"version":1,"files":["../../outside"]}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "user-file-claim":
				if err := os.WriteFile(manifest, []byte(`{"version":1,"files":["user-file"]}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "token-symlink":
				file := filepath.Join(root, teamBridgeGeneratedToken)
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(manifest, file); err != nil {
					t.Skip("symlinks unavailable")
				}
			case "directory-symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(root, "local-link")); err != nil {
					t.Skip("symlinks unavailable")
				}
			}
			before, err := os.ReadFile(filepath.Join(out, "multica.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Export(Options{OutputDir: out, Force: true, WithTeamBridge: true, WithoutSecrets: true}); err == nil {
				t.Fatal("unsafe existing tree accepted")
			}
			after, err := os.ReadFile(filepath.Join(out, "multica.yaml"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed refresh changed previous snapshot")
			}
		})
	}
}
