//go:build integration

// These tests execute the official Multica CLI against a synthetic loopback API.
// They never load a user's profile, task identity, workspace or credentials.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

const (
	skillID     = "00000000-0000-4000-8000-000000000001"
	agentID     = "00000000-0000-4000-8000-000000000002"
	runtimeID   = "00000000-0000-4000-8000-000000000003"
	workspaceID = "00000000-0000-4000-8000-000000000004"
	body        = "---\nname: fixture\ndescription: Synthetic skill.\n---\n\n# Fixture\n\nUnicode: проверка.  \n\n"
	support     = "# Supporting document\n\nTrailing spaces.  \n\n"
)

type isolatedRunner struct {
	env []string
	dir string
}

func (r isolatedRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env, command.Dir = r.env, r.dir
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

type request struct {
	method, path string
	body         map[string]any
}
type fixture struct {
	project, autopilot                    map[string]any
	resources, triggers, collaborators    []map[string]any
	resourceSequence, triggerSequence     int
	failTriggerWrite, incompleteAutopilot bool
	mu                                    sync.Mutex
	skill, agent                          map[string]any
	files                                 []map[string]any
	assigned                              []map[string]any
	env                                   map[string]string
	requests                              []request
	metadataOnly                          bool
}

func newFixture(t *testing.T) (*fixture, *backend.CLI) {
	t.Helper()
	path := os.Getenv("MULTICA_BIN")
	if path == "" {
		t.Fatal("MULTICA_BIN must point to the pinned official CLI; integration tests must not silently skip")
	}
	path, err := exec.LookPath(path)
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		skill:    map[string]any{"id": skillID, "name": "fixture", "description": "Synthetic skill.", "content": body},
		files:    []map[string]any{{"id": "file-1", "path": "references/details.md", "content": support}},
		agent:    map[string]any{"id": agentID, "name": "Builder", "runtime_id": runtimeID, "runtime_config": map[string]any{}, "instructions": "Build carefully.", "description": "Synthetic agent.", "model": "fixture-model", "thinking_level": "xhigh", "service_tier": "priority", "max_concurrent_tasks": 2, "permission_mode": "private", "custom_args": []string{}, "conversation_starters": []model.ConversationStarter{{Label: "Review", Prompt: "Review the changes."}}},
		assigned: []map[string]any{{"id": skillID, "name": "fixture", "enabled": true}},
		env:      map[string]string{},
	}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	home := t.TempDir()
	// Build an allowlist, not a copy of os.Environ. No real MULTICA_* variables,
	// proxies or profiles can redirect a fixture command to a live service.
	runner := isolatedRunner{dir: home, env: []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home,
		"XDG_CACHE_HOME=" + home, "MULTICA_SERVER_URL=" + server.URL,
		"MULTICA_WORKSPACE_ID=" + workspaceID, "MULTICA_TOKEN=synthetic-integration-token",
		"NO_PROXY=127.0.0.1,localhost",
	}}
	cli := backend.NewCLI(path)
	cli.Runner = runner
	return f, cli
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	data := map[string]any{}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "invalid fixture JSON", 400)
			return
		}
	}
	f.requests = append(f.requests, request{r.Method, r.URL.RequestURI(), data})
	respond := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	if f.serveWorkspace(w, r, data) {
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/skills":
		list := []any{}
		if f.skill != nil {
			list = append(list, map[string]any{"id": skillID, "name": f.skill["name"], "description": f.skill["description"]})
		}
		respond(list)
	case "GET /api/skills/" + skillID:
		value := map[string]any{}
		for k, v := range f.skill {
			value[k] = v
		}
		files := []map[string]any{}
		for _, file := range f.files {
			clone := map[string]any{}
			for k, v := range file {
				clone[k] = v
			}
			files = append(files, clone)
		}
		if f.metadataOnly || r.URL.Query().Get("include") != "content" {
			delete(value, "content")
			for _, file := range files {
				delete(file, "content")
			}
		}
		value["files"] = files
		respond(value)
	case "GET /api/agents":
		list := []any{}
		if f.agent != nil {
			list = append(list, f.agent)
		}
		respond(list)
	case "GET /api/agents/" + agentID:
		respond(f.agent)
	case "GET /api/agents/" + agentID + "/skills":
		if f.assigned == nil {
			respond([]any{})
		} else {
			respond(f.assigned)
		}
	case "GET /api/agents/" + agentID + "/env":
		respond(map[string]any{"custom_env": f.env})
	case "GET /api/runtimes":
		respond([]any{map[string]any{"id": runtimeID, "name": "Fixture runtime", "provider": "codex"}})
	case "GET /api/workspaces/" + workspaceID + "/mcp-servers":
		respond([]any{})
	case "GET /api/squads":
		respond([]any{})
	case "POST /api/skills":
		f.skill = data
		f.skill["id"] = skillID
		f.files = []map[string]any{}
		respond(f.skill)
	case "PUT /api/skills/" + skillID:
		for k, v := range data {
			f.skill[k] = v
		}
		respond(f.skill)
	case "POST /api/skills/" + skillID + "/files", "PUT /api/skills/" + skillID + "/files":
		found := false
		for i, file := range f.files {
			if file["path"] == data["path"] {
				data["id"] = file["id"]
				f.files[i] = data
				found = true
			}
		}
		if !found {
			data["id"] = fmt.Sprintf("file-%d", len(f.files)+1)
			f.files = append(f.files, data)
		}
		respond(data)
	case "DELETE /api/skills/" + skillID + "/files/file-1":
		f.files = slices.DeleteFunc(f.files, func(v map[string]any) bool { return v["id"] == "file-1" })
		w.WriteHeader(204)
	case "POST /api/agents":
		f.agent = data
		f.agent["id"] = agentID
		respond(f.agent)
	case "PUT /api/agents/" + agentID:
		for k, v := range data {
			f.agent[k] = v
		}
		respond(f.agent)
	case "PUT /api/agents/" + agentID + "/skills":
		f.assigned = []map[string]any{}
		for _, id := range data["skill_ids"].([]any) {
			f.assigned = append(f.assigned, map[string]any{"id": id, "name": "fixture", "enabled": true})
		}
		respond(f.assigned)
	case "PUT /api/agents/" + agentID + "/env":
		encoded, _ := json.Marshal(data["custom_env"])
		f.env = map[string]string{}
		_ = json.Unmarshal(encoded, &f.env)
		f.agent["has_custom_env"] = len(f.env) > 0
		f.agent["custom_env_key_count"] = len(f.env)
		respond(map[string]any{"custom_env": f.env})
	default:
		http.Error(w, "unexpected fixture request", 500)
	}
}

func exportProject(t *testing.T, cli *backend.CLI) (model.Project, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "snapshot")
	if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir}); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(filepath.Join(dir, "multica.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return p, dir
}
func assertNoop(t *testing.T, cli *backend.CLI, project model.Project) {
	t.Helper()
	changes, err := (reconcile.Reconciler{Backend: cli}).Plan(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if change.Action != reconcile.Noop {
			t.Fatalf("unexpected drift: %#v", change)
		}
	}
}
func (f *fixture) mutations() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []request{}
	for _, req := range f.requests {
		if req.method != "GET" {
			out = append(out, req)
		}
	}
	return out
}

func TestOfficialCLIRoundTrip(t *testing.T) {
	f, cli := newFixture(t)
	// Demonstrate the upstream default using the actual unadapted binary.
	raw, stderr, err := cli.Runner.Run(context.Background(), cli.Binary, "skill", "get", skillID, "--output", "json")
	if err != nil {
		t.Fatalf("%v %s", err, stderr)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if _, exists := metadata["content"]; exists {
		t.Fatal("expected metadata-only default")
	}
	p, dir := exportProject(t, cli)
	for path, want := range map[string]string{"skills/fixture/SKILL.md": body, "skills/fixture/references/details.md": support} {
		got, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || string(got) != want {
			t.Fatalf("file changed during export: %s (%v)", path, err)
		}
	}
	for range 2 {
		assertNoop(t, cli, p)
	}
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != 0 {
		t.Fatal("unchanged snapshot caused writes")
	}
}

func TestOfficialCLIServiceTierClearAndLegacyOmission(t *testing.T) {
	f, cli := newFixture(t)
	p, _ := exportProject(t, cli)
	empty := ""
	p.Agents[0].ServiceTier = &empty
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	requests := f.mutations()
	if len(requests) != 1 || requests[0].body["service_tier"] != "" {
		t.Fatalf("tier clear missing: %#v", requests)
	}
	p.Agents[0].ServiceTier = nil
	p.Agents[0].ConversationStarters = nil
	p.Agents[0].SystemKey = nil
	p.Agents[0].Description = "Changed description."
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	requests = f.mutations()
	for _, field := range []string{"service_tier", "conversation_starters", "system_key"} {
		if _, ok := requests[len(requests)-1].body[field]; ok {
			t.Fatalf("unmanaged field was written: %s", field)
		}
	}
	assertNoop(t, cli, p)
}

func TestOfficialCLIUnboundExportEditAndRebind(t *testing.T) {
	f, cli := newFixture(t)
	f.mu.Lock()
	f.agent["runtime_id"] = ""
	f.mu.Unlock()
	p, _ := exportProject(t, cli)
	if !p.Agents[0].Unbound || len(p.RuntimeSelectors) != 0 {
		t.Fatal("unbound export lost state")
	}
	assertNoop(t, cli, p)
	p.Agents[0].Description = "Edit without a runtime."
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	requests := f.mutations()
	if _, ok := requests[0].body["runtime_id"]; ok {
		t.Fatal("unbound edit sent empty runtime UUID")
	}
	assertNoop(t, cli, p)
	p.RuntimeSelectors = map[string]model.RuntimeSelector{"r": {ID: runtimeID}}
	p.Agents[0].RuntimeRef = "r"
	p.Agents[0].Unbound = false
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
}

func TestOfficialCLIIncompleteReadIsNonDestructive(t *testing.T) {
	f, cli := newFixture(t)
	p, dir := exportProject(t, cli)
	before, err := os.ReadFile(filepath.Join(dir, "skills/fixture/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.metadataOnly = true
	f.mu.Unlock()
	// A pending create before the corrupt read must not make apply partially mutate.
	pending := p.Skills[0]
	pending.Name = "pending-create"
	p.Skills = append([]model.SkillSpec{pending}, p.Skills...)
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err == nil {
		t.Fatal("incomplete read allowed apply")
	}
	if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: dir, Force: true}); err == nil {
		t.Fatal("incomplete read allowed export")
	}
	after, err := os.ReadFile(filepath.Join(dir, "skills/fixture/SKILL.md"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed export changed snapshot")
	}
	if len(f.mutations()) != 0 {
		t.Fatal("failed preflight performed mutations")
	}
}

func TestOfficialCLISkillContentAndFilesConverge(t *testing.T) {
	f, cli := newFixture(t)
	p, dir := exportProject(t, cli)
	p.Skills[0].Content = body + "Additional instructions.\n"
	if err := os.WriteFile(p.Skills[0].ContentPath, []byte(p.Skills[0].Content), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "replacement.md")
	content := strings.Repeat("Unicode: проверка.  \n", 1000)
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	p.Skills[0].Files = []model.SkillFileSpec{{Path: "references/replacement.md", SourcePath: file, Content: content}}
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	before := len(f.mutations())
	if before != 3 {
		t.Fatalf("expected skill update, file upsert and file delete; got %d", before)
	}
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != before {
		t.Fatal("repeated apply wrote unchanged data")
	}
}

func TestOfficialCLICreateAndReapply(t *testing.T) {
	f, cli := newFixture(t)
	p, _ := exportProject(t, cli)
	starters := []model.ConversationStarter{}
	p.Agents[0].ConversationStarters = &starters
	f.mu.Lock()
	f.skill = nil
	f.agent = nil
	f.files = nil
	f.assigned = nil
	f.requests = nil
	f.mu.Unlock()
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	before := len(f.mutations())
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.mutations()) != before {
		t.Fatal("creation did not converge")
	}
}

func TestOfficialCLIReadOnlyStarterChangeFailsBeforeWrites(t *testing.T) {
	f, cli := newFixture(t)
	p, _ := exportProject(t, cli)
	changed := []model.ConversationStarter{{Label: "New", Prompt: "New prompt."}}
	p.Agents[0].ConversationStarters = &changed
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err == nil {
		t.Fatal("unsupported change was silently discarded")
	}
	if len(f.mutations()) != 0 {
		t.Fatal("unsupported change caused partial apply")
	}
}

func TestOfficialCLISecretFilesRoundTripAndClear(t *testing.T) {
	f, cli := newFixture(t)
	f.mu.Lock()
	f.env = map[string]string{"TOKEN": "synthetic-env-secret"}
	f.agent["has_custom_env"] = true
	f.agent["custom_env_key_count"] = 1
	f.agent["mcp_config"] = map[string]any{"mcpServers": map[string]any{"fixture": map[string]any{"command": "fixture", "env": map[string]any{"KEY": "synthetic-mcp-secret"}}}}
	f.mu.Unlock()
	p, _ := exportProject(t, cli)
	a := &p.Agents[0]
	if !a.ManageCustomEnv || !a.ManageMCPConfig || a.CustomEnv["TOKEN"] != "synthetic-env-secret" {
		t.Fatal("secret file references were lost")
	}
	for _, file := range []string{a.CustomEnvFile, a.MCPConfigFile} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe secret-file mode: %v", err)
		}
	}
	assertNoop(t, cli, p)
	a.CustomEnv = map[string]string{}
	if err := os.WriteFile(a.CustomEnvFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	a.MCPConfig = json.RawMessage(`null`)
	if err := os.WriteFile(a.MCPConfigFile, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil {
		t.Fatal(err)
	}
	assertNoop(t, cli, p)
	if len(f.mutations()) != 2 {
		t.Fatal("expected only private MCP update and env clear")
	}
}
