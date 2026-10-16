package exporter

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

type noAvatarDownloads struct{ t *testing.T }

func (n noAvatarDownloads) RoundTrip(*http.Request) (*http.Response, error) {
	n.t.Error("avatars must never be downloaded")
	return nil, fmt.Errorf("unexpected avatar download")
}

func TestAllAvatarsAreOmittedAndRemoteDriftIsIgnored(t *testing.T) {
	for _, value := range []string{"emoji:🌞", "emoji:👩🏽‍💻", "emoji:❤️", "emoji:", "https://example.invalid/avatar.png", "file:///missing", ""} {
		t.Run(value, func(t *testing.T) {
			b := exampleBackend()
			for i := range b.agents {
				v := value
				b.agents[i].AvatarURL = &v
			}
			for i := range b.squads {
				v := value
				b.squads[i].AvatarURL = &v
			}
			out := filepath.Join(t.TempDir(), "snapshot")
			e := Exporter{Backend: b, HTTPClient: &http.Client{Transport: noAvatarDownloads{t}}}
			for _, force := range []bool{false, true} {
				result, err := e.Export(Options{OutputDir: out, Force: force})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Warnings) != 0 {
					t.Fatal(result.Warnings)
				}
				err = filepath.WalkDir(out, func(path string, d fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if d.IsDir() {
						return nil
					}
					if strings.HasPrefix(d.Name(), "avatar.") {
						t.Fatalf("exported avatar file: %s", path)
					}
					if d.Name() == "agent.yaml" || d.Name() == "squad.yaml" {
						data, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						if strings.Contains(string(data), "avatarUrl:") || strings.Contains(string(data), "avatarFile:") {
							t.Fatalf("exported avatar field: %s", path)
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				p, err := config.Load(filepath.Join(out, "multica.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				for _, a := range p.Agents {
					if a.AvatarURL != nil || a.AvatarFile != "" {
						t.Fatal("avatar in desired state")
					}
				}
				for _, s := range p.Squads {
					if s.AvatarURL != "" {
						t.Fatal("squad avatar in desired state")
					}
				}
				changes, err := (reconcile.Reconciler{Backend: b}).Plan(p)
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range changes {
					if change.Action != reconcile.Noop {
						t.Fatal(change)
					}
				}
				// Fake backend panics on every mutation, including archive/restore.
				if err := (reconcile.Reconciler{Backend: b}).Apply(p, nil); err != nil {
					t.Fatal(err)
				}
				if !force {
					// Simulate an old export; refresh must remove generated avatar data.
					file := filepath.Join(out, "agents/unity-developer/agent.yaml")
					data, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					data = []byte(strings.Replace(string(data), "multica:\n", "multica:\n    avatarUrl: \"emoji:old\"\n    avatarFile: avatar.png\n", 1))
					if err := os.WriteFile(file, data, 0644); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(filepath.Dir(file), "avatar.png"), []byte("old-avatar"), 0644); err != nil {
						t.Fatal(err)
					}
					for i := range b.agents {
						v := "emoji:changed"
						b.agents[i].AvatarURL = &v
					}
					for i := range b.squads {
						v := "emoji:changed"
						b.squads[i].AvatarURL = &v
					}
				}
			}
		})
	}
}
