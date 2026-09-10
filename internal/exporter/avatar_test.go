package exporter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

type noAvatarDownloads struct{ t *testing.T }

func (n noAvatarDownloads) RoundTrip(*http.Request) (*http.Response, error) {
	n.t.Error("emoji must not be downloaded")
	return nil, fmt.Errorf("unexpected download")
}

func TestEmojiAvatarExportIsLosslessAndDoesNotDownload(t *testing.T) {
	for _, value := range []string{"emoji:🌞", "emoji:⭐", "emoji:🐝", "emoji:🐧", "emoji:👩🏽‍💻", "emoji:❤️", "emoji:🇳🇱"} {
		t.Run(value, func(t *testing.T) {
			b := exampleBackend()
			for i := range b.agents {
				v := value
				b.agents[i].AvatarURL = &v
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
				p, err := config.Load(filepath.Join(out, "multica.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				for _, a := range p.Agents {
					if a.AvatarURL == nil || *a.AvatarURL != value || a.AvatarFile != "" {
						t.Fatalf("avatar lost: %#v", a)
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
			}
		})
	}
}

func TestImageAvatarExportStillDownloadsFiles(t *testing.T) {
	image := []byte("synthetic-image-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(image)
	}))
	defer server.Close()
	b := exampleBackend()
	for i := range b.agents {
		v := server.URL + "/avatar.png"
		b.agents[i].AvatarURL = &v
	}
	out := filepath.Join(t.TempDir(), "snapshot")
	if _, err := (Exporter{Backend: b}).Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(out, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Agents {
		if a.AvatarURL != nil || a.AvatarFile == "" {
			t.Fatalf("expected file: %#v", a)
		}
		data, err := os.ReadFile(a.AvatarFile)
		if err != nil || string(data) != string(image) {
			t.Fatal("image lost", err)
		}
	}
}

func TestInvalidEmojiDoesNotReplacePreviousSnapshot(t *testing.T) {
	b := exampleBackend()
	out := filepath.Join(t.TempDir(), "snapshot")
	e := Exporter{Backend: b}
	if _, err := e.Export(Options{OutputDir: out}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(out, "agents/unity-developer/agent.yaml")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	value := "emoji:"
	b.agents[0].AvatarURL = &value
	if _, err := e.Export(Options{OutputDir: out, Force: true}); err == nil {
		t.Fatal("expected invalid emoji failure")
	}
	after, err := os.ReadFile(file)
	if err != nil || string(before) != string(after) {
		t.Fatal("previous export replaced", err)
	}
}
