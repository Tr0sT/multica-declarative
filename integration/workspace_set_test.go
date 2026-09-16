//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
	"github.com/Tr0sT/multica-declarative/internal/workspace"
)

func TestRealCLIWorkspaceSetExportUpdateAndIsolation(t *testing.T) {
	const secondWS = "00000000-0000-4000-8000-000000000104"
	const secondSkill = "00000000-0000-4000-8000-000000000101"
	const poisonedWS = "00000000-0000-4000-8000-000000000999"
	_, cli := newFixture(t)
	var mu sync.Mutex
	bodies := map[string]string{
		workspaceID: "---\nname: shared\ndescription: Shared conventions.\n---\nContent a.\n",
		secondWS:    "---\nname: shared\ndescription: Shared conventions.\n---\nContent b.\n",
	}
	skillIDs := map[string]string{workspaceID: skillID, secondWS: secondSkill}
	updates := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		respond := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		if r.Method == "GET" && r.URL.Path == "/api/workspaces" {
			respond([]backend.Workspace{{ID: workspaceID, Slug: "a", Name: "A"}, {ID: secondWS, Slug: "b", Name: "B"}})
			return
		}
		for id, sid := range skillIDs {
			if r.URL.Path == "/api/skills/"+sid {
				if r.Method == "PUT" {
					var data map[string]any
					if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
						t.Error(err)
						http.Error(w, "invalid JSON", 400)
						return
					}
					bodies[id] = data["content"].(string)
					updates[id]++
				} else if r.Method != "GET" {
					t.Errorf("unexpected skill method %s", r.Method)
					http.Error(w, "unexpected method", 500)
					return
				}
				respond(map[string]any{"id": sid, "name": "shared", "description": "Shared conventions.", "content": bodies[id], "files": []any{}})
				return
			}
			if r.Method == "GET" && r.URL.Path == "/api/workspaces/"+id+"/mcp-servers" {
				respond([]any{})
				return
			}
		}
		id := r.URL.Query().Get("workspace_id")
		if _, ok := bodies[id]; !ok {
			t.Errorf("request escaped explicit workspace binding: %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, "wrong workspace", 400)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/skills":
			respond([]any{map[string]any{"id": skillIDs[id], "name": "shared", "description": "Shared conventions."}})
		case "GET /api/agents", "GET /api/runtimes", "GET /api/squads":
			respond([]any{})
		case "GET /api/projects":
			respond(map[string]any{"projects": []any{}})
		case "GET /api/autopilots":
			respond(map[string]any{"autopilots": []any{}, "total": 0})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, "unexpected request", 500)
		}
	}))
	t.Cleanup(server.Close)
	runner := cli.Runner.(isolatedRunner)
	for i, value := range runner.env {
		if strings.HasPrefix(value, "MULTICA_SERVER_URL=") {
			runner.env[i] = "MULTICA_SERVER_URL=" + server.URL
		}
		if strings.HasPrefix(value, "MULTICA_WORKSPACE_ID=") {
			runner.env[i] = "MULTICA_WORKSPACE_ID=" + poisonedWS
		}
	}
	cli.Runner = runner
	ex := exporter.WorkspaceExporter{Catalog: cli, BackendFor: func(id string) backend.Backend { return cli.WithScope("", id) }}
	out := filepath.Join(t.TempDir(), "snapshot")
	result, err := ex.Export(exporter.Options{OutputDir: out, WithoutSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Workspaces != 2 || result.Skills != 2 {
		t.Fatalf("result=%#v", result)
	}
	set, err := workspace.Load(filepath.Join(out, "multica.yaml"), config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range set.Entries {
		assertNoop(t, cli.WithScope("", entry.ID), entry.Project)
	}
	mu.Lock()
	before := len(updates)
	mu.Unlock()
	if before != 0 {
		t.Fatal("export or plan mutated server")
	}
	file := filepath.Join(out, "workspaces/a/skills/shared/SKILL.md")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(string(data), "Content a.", "Changed only a.")), 0644); err != nil {
		t.Fatal(err)
	}
	set, err = workspace.Load(filepath.Join(out, "multica.yaml"), config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		for _, entry := range set.Entries {
			if err := (reconcile.Reconciler{Backend: cli.WithScope("", entry.ID)}).Apply(entry.Project, func(model.Change) {}); err != nil {
				t.Fatal(err)
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if updates[workspaceID] != 1 || updates[secondWS] != 0 || !strings.Contains(bodies[secondWS], "Content b.") {
		t.Fatalf("workspace isolation/convergence failed: updates=%v bodies=%v", updates, bodies)
	}
}
