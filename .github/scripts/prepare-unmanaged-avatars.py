#!/usr/bin/env python3
"""One-shot source edit on the maintenance branch; removes itself before commit."""
from pathlib import Path
import re


def replace(text, old, new, count=1):
    actual = text.count(old)
    if actual != count:
        raise RuntimeError(f"expected {count} occurrences, got {actual}: {old[:100]!r}")
    return text.replace(old, new)


def cut(text, start, end):
    i = text.index(start)
    j = text.index(end, i + len(start))
    return text[:i] + text[j:]


def drop_function(text, name):
    pattern = r"(?ms)^func (?:\([^\n]*\) )?" + re.escape(name) + r"\([^\n]*\).*?^}\n?"
    text, count = re.subn(pattern, "", text)
    if count != 1:
        raise RuntimeError(f"expected one function {name}, got {count}")
    return text


def edit(path, fn):
    p = Path(path)
    old = p.read_text()
    new = fn(old)
    if old == new:
        raise RuntimeError(f"no edit in {path}")
    p.write_text(new)


def exporter(text):
    text = replace(text, '\t"io"\n', '')
    text = replace(text, '\t"time"\n', '')
    text = re.sub(r'(?m)^\t(?:avatarName|avatar|AvatarFile|AvatarURL)\s+[^\n]*\n', '', text)
    text = cut(text, '\t\tif a.AvatarURL != nil && strings.HasPrefix', '\t\tagents = append(agents, ea)')
    text = cut(text, '\t\t\tavatar := ""\n', '\t\t\tsq := exportedSquad')
    text = replace(text, ', AvatarURL: avatar', '')
    text = drop_function(text, 'downloadAvatar')
    text = drop_function(text, 'avatarExtension')
    return text


edit('internal/exporter/exporter.go', exporter)
edit('internal/exporter/write.go', lambda s: cut(s, '\t\tif len(a.avatar) > 0 {', '\t\tif a.customEnv != nil {'))


def loader(text):
    text = cut(text, '\tif d.Multica.AvatarURL != nil {', '\truntimeConfig := d.Multica.RuntimeConfig')
    text = replace(text, ', AvatarFile: avatarFile, AvatarURL: d.Multica.AvatarURL', '')
    text = replace(text, ', AvatarURL: strings.TrimSpace(d.AvatarURL)', '')
    # Keep the known YAML keys for old snapshots, but never validate or resolve them.
    text = re.sub(r'(?m)^(\tAvatar(?:File|URL)\s+[^\n]*)$', r'\t// Deprecated: accepted for old snapshots but deliberately ignored.\n\1', text)
    return text


edit('internal/config/loader.go', loader)


def reconciler(text):
    text = replace(text, ' || d.AvatarFile != ""', '')
    text = replace(text, ' || (d.AvatarFile != "" && hasAnyField(fields, "avatar"))', '')
    text = replace(text, '\tif err := r.checkAvatarWrites(project, state); err != nil {\n\t\treturn err\n\t}\n', '')
    text = cut(text, '\t\tif d.AvatarURL != nil && (created', '\t\tdesiredArchived := d.Archived')
    text = replace(text, '\t\t\t\tif d.AvatarURL != "" {\n\t\t\t\t\tfields = append(fields, "avatarUrl")\n\t\t\t\t}\n', '')
    text = cut(text, '\tif d.AvatarURL != nil {', '\tif d.Archived != a.Archived()')
    text = drop_function(text, 'avatarDiffers')
    text = cut(text, '\tavatar := ""\n', '\tif !squadMembersMatch')
    text = replace(text, ', AvatarURL: v.AvatarURL', '')
    # These were used only by avatar downloads; retain imports still used elsewhere.
    for name in ('io', 'os', 'time'):
        if not re.search(r'\b' + name + r'\.', text):
            text = replace(text, '\t"' + name + '"\n', '')
    return text


edit('internal/reconcile/reconcile.go', reconciler)


def cli(text):
    text = drop_function(text, 'UploadAgentAvatar')
    text = replace(text, '\t\tcase "avatarUrl":\n\t\t\targs = append(args, "--avatar-url", in.AvatarURL)\n', '')
    return text


edit('internal/backend/cli.go', cli)
edit('internal/backend/backend.go', lambda s: re.sub(r'(?m)^\tUploadAgentAvatar\([^\n]*\n', '', s))


def workspace(text):
    text = replace(text, '\t\tif command == "apply" {\n\t\t\tplans[i], err = controllers[i].Preflight(entry.Project)\n\t\t} else {\n\t\t\tplans[i], err = controllers[i].Plan(entry.Project)\n\t\t}\n', '\t\tplans[i], err = controllers[i].Plan(entry.Project)\n')
    text = replace(text, '\t// Preflight all workspaces, including required write capabilities, before\n\t// the first write. Plan stays usable with a read-only/stock CLI. This is\n\t// not a server transaction: failures/races during apply can still leave\n\t// partial changes.\n', '\t// Inspect all workspaces before the first write. Avatars are unmanaged.\n\t// This is not a server transaction: later API failures or races can still\n\t// leave partial changes.\n')
    return text


edit('internal/app/workspaces.go', workspace)

for path in (
    'compat/multica-avatar.patch',
    'scripts/build-avatar-cli.sh',
    'internal/backend/avatar.go',
    'internal/backend/avatar_test.go',
    'internal/reconcile/avatar.go',
    'internal/reconcile/preflight.go',
    'internal/app/avatar_preflight_test.go',
    'integration/emoji_write_test.go',
):
    Path(path).unlink()
# The validator is no longer part of the managed model.
for path in ('internal/model/avatar.go', 'internal/model/avatar_test.go'):
    if Path(path).exists():
        Path(path).unlink()

edit('Makefile', lambda s: s[:s.index('# Opt-in CLI build for emoji writes;')].rstrip() + '\n')
edit('.github/workflows/ci.yml', lambda s: s[:s.index('      - name: Build opt-in CLI avatar setter patch')].rstrip() + '\n')

Path('internal/config/avatar_test.go').write_text(r'''package config

import (
    "path/filepath"
    "testing"
)

func TestLegacyAvatarFieldsAreAcceptedButIgnored(t *testing.T) {
    for _, fields := range []string{
        "  avatarUrl: \"emoji:👩🏽‍💻\"\n",
        "  avatarUrl: \"emoji:\"\n",
        "  avatarUrl: \"\"\n",
        "  avatarUrl: null\n",
        "  avatarUrl: https://example.invalid/avatar.png\n",
        "  avatarFile: missing.png\n",
        "  avatarFile: /nonexistent/avatar.png\n",
        "  avatarFile: ../../missing.png\n  avatarUrl: \"emoji:old\"\n",
    } {
        t.Run(fields, func(t *testing.T) {
            root := t.TempDir()
            manifest := filepath.Join(root, "multica.yaml")
            writeFile(t, manifest, "apiVersion: multica-declarative/v1alpha1\nruntimes:\n  r: {id: runtime-1}\n")
            writeFile(t, filepath.Join(root, "agents/a/agent.yaml"), "name: A\nmultica:\n  runtime: r\n"+fields)
            writeFile(t, filepath.Join(root, "squads/s/squad.yaml"), "name: S\nleader: A\navatarUrl: https://example.invalid/old.png\n")
            for _, withoutSecrets := range []bool{false, true} {
                p, err := LoadWithOptions(manifest, LoadOptions{WithoutSecrets: withoutSecrets})
                if err != nil { t.Fatal(err) }
                if p.Agents[0].AvatarURL != nil || p.Agents[0].AvatarFile != "" || p.Squads[0].AvatarURL != "" {
                    t.Fatal("legacy avatars became desired state")
                }
            }
        })
    }
}
''')

Path('internal/exporter/avatar_test.go').write_text(r'''package exporter

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
            for i := range b.agents { v := value; b.agents[i].AvatarURL = &v }
            for i := range b.squads { v := value; b.squads[i].AvatarURL = &v }
            out := filepath.Join(t.TempDir(), "snapshot")
            e := Exporter{Backend: b, HTTPClient: &http.Client{Transport: noAvatarDownloads{t}}}
            for _, force := range []bool{false, true} {
                result, err := e.Export(Options{OutputDir: out, Force: force})
                if err != nil { t.Fatal(err) }
                if len(result.Warnings) != 0 { t.Fatal(result.Warnings) }
                err = filepath.WalkDir(out, func(path string, d fs.DirEntry, walkErr error) error {
                    if walkErr != nil { return walkErr }
                    if d.IsDir() { return nil }
                    if strings.HasPrefix(d.Name(), "avatar.") { t.Fatalf("exported avatar file: %s", path) }
                    if d.Name() == "agent.yaml" || d.Name() == "squad.yaml" {
                        data, err := os.ReadFile(path)
                        if err != nil { return err }
                        if strings.Contains(string(data), "avatarUrl:") || strings.Contains(string(data), "avatarFile:") {
                            t.Fatalf("exported avatar field: %s", path)
                        }
                    }
                    return nil
                })
                if err != nil { t.Fatal(err) }
                p, err := config.Load(filepath.Join(out, "multica.yaml"))
                if err != nil { t.Fatal(err) }
                for _, a := range p.Agents {
                    if a.AvatarURL != nil || a.AvatarFile != "" { t.Fatal("avatar in desired state") }
                }
                for _, s := range p.Squads { if s.AvatarURL != "" { t.Fatal("squad avatar in desired state") } }
                changes, err := (reconcile.Reconciler{Backend: b}).Plan(p)
                if err != nil { t.Fatal(err) }
                for _, change := range changes { if change.Action != reconcile.Noop { t.Fatal(change) } }
                // Fake backend panics on every mutation, including archive/restore.
                if err := (reconcile.Reconciler{Backend: b}).Apply(p, nil); err != nil { t.Fatal(err) }
                if !force {
                    // Simulate an old export; refresh must remove generated avatar data.
                    file := filepath.Join(out, "agents/unity-developer/agent.yaml")
                    data, err := os.ReadFile(file)
                    if err != nil { t.Fatal(err) }
                    data = []byte(strings.Replace(string(data), "multica:\n", "multica:\n    avatarUrl: \"emoji:old\"\n    avatarFile: avatar.png\n", 1))
                    if err := os.WriteFile(file, data, 0644); err != nil { t.Fatal(err) }
                    if err := os.WriteFile(filepath.Join(filepath.Dir(file), "avatar.png"), []byte("old-avatar"), 0644); err != nil { t.Fatal(err) }
                    for i := range b.agents { v := "emoji:changed"; b.agents[i].AvatarURL = &v }
                    for i := range b.squads { v := "emoji:changed"; b.squads[i].AvatarURL = &v }
                }
            }
        })
    }
}
''')

Path('internal/reconcile/avatar_test.go').write_text(r'''package reconcile

import (
    "testing"
    "github.com/Tr0sT/multica-declarative/internal/model"
)

func TestLegacyAgentAvatarsCannotTriggerWritesOrUnarchive(t *testing.T) {
    stamp := "2026-01-01T00:00:00Z"
    live, old := "emoji:live", "emoji:"
    b := &fakeBackend{
        agents: []model.Agent{{ID: "a", Name: "A"}},
        agent: model.Agent{ID: "a", Name: "A", RuntimeID: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private", AvatarURL: &live, ArchivedAt: &stamp},
        runtimes: []model.Runtime{{ID: "r"}},
    }
    p := model.Project{RuntimeSelectors: map[string]model.RuntimeSelector{"r": {ID: "r"}}, Agents: []model.AgentSpec{
        {Name: "A", RuntimeRef: "r", RuntimeConfig: map[string]any{}, MaxConcurrentTasks: 1, PermissionMode: "private", AvatarURL: &old, AvatarFile: "/nonexistent/avatar.png", Archived: true},
    }}
    r := Reconciler{Backend: b}
    changes, err := r.Plan(p)
    if err != nil { t.Fatal(err) }
    for _, change := range changes { if change.Action != Noop { t.Fatal(change) } }
    if err := r.Apply(p, nil); err != nil { t.Fatal(err) }
    if b.restored || b.archived || b.updatedAgent || *b.agent.AvatarURL != live { t.Fatal("avatar drift changed archived agent") }
}

func TestSquadAvatarDriftIsUnmanaged(t *testing.T) {
    live := "emoji:live"
    for _, old := range []string{"", "emoji:old", "https://example.invalid/old.png"} {
        desired := model.SquadSpec{Name: "S", Leader: "A", AvatarURL: old}
        actual := model.Squad{ID: "s", Name: "S", LeaderID: "a", AvatarURL: &live}
        agents := map[string]model.Agent{"A": {ID: "a", Name: "A"}}
        members := []model.SquadMember{{MemberID: "a", MemberType: "agent", Role: "leader"}}
        if fields := diffSquad(desired, actual, members, agents); len(fields) != 0 { t.Fatal(fields) }
        if squadInput(desired, "a").AvatarURL != "" { t.Fatal("squad input leaked legacy avatar") }
    }
}
''')

Path('integration/emoji_read_test.go').write_text(r'''//go:build integration

package integration

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/Tr0sT/multica-declarative/internal/config"
    "github.com/Tr0sT/multica-declarative/internal/exporter"
    "github.com/Tr0sT/multica-declarative/internal/reconcile"
)

func TestOfficialCLIAvatarsAreUnmanaged(t *testing.T) {
    for _, value := range []string{"emoji:🌞", "emoji:👩🏽‍💻", "emoji:❤️", "emoji:", "https://example.invalid/avatar.png", ""} {
        t.Run(value, func(t *testing.T) {
            f, cli := newFixture(t)
            setFixtureAvatar(f, value)
            out := filepath.Join(t.TempDir(), "snapshot")
            if _, err := (exporter.Exporter{Backend: cli}).Export(exporter.Options{OutputDir: out}); err != nil { t.Fatal(err) }
            file := filepath.Join(out, "agents/builder/agent.yaml")
            data, err := os.ReadFile(file)
            if err != nil { t.Fatal(err) }
            if strings.Contains(string(data), "avatarUrl:") || strings.Contains(string(data), "avatarFile:") { t.Fatal("avatar exported") }
            // Old declarations remain usable without their avatar files or any CLI patch.
            data = []byte(strings.Replace(string(data), "multica:\n", "multica:\n    avatarUrl: \"emoji:old\"\n    avatarFile: missing-avatar.png\n", 1))
            if err := os.WriteFile(file, data, 0644); err != nil { t.Fatal(err) }
            p, err := config.Load(filepath.Join(out, "multica.yaml"))
            if err != nil { t.Fatal(err) }
            if p.Agents[0].AvatarURL != nil || p.Agents[0].AvatarFile != "" { t.Fatal("legacy fields not ignored") }
            assertNoop(t, cli, p)
            r := reconcile.Reconciler{Backend: cli}
            if err := r.Apply(p, nil); err != nil { t.Fatal(err) }
            if len(f.mutations()) != 0 { t.Fatal("avatar drift triggered a write") }
            // Exercise direct callers too, and ensure real managed changes still work.
            legacy := "emoji:"
            p.Agents[0].AvatarURL = &legacy
            p.Agents[0].AvatarFile = "/nonexistent/avatar.png"
            p.Agents[0].Description = "Changed description"
            p.Skills[0].Description = "Changed skill"
            if err := r.Apply(p, nil); err != nil { t.Fatal(err) }
            if fixtureAvatar(f) != value { t.Fatal("managed update changed live avatar") }
            if len(f.mutations()) < 2 { t.Fatal("managed updates were skipped") }
            assertNoAvatarRequests(t, f)
            assertNoop(t, cli, p)
            before := len(f.mutations())
            if err := r.Apply(p, nil); err != nil { t.Fatal(err) }
            if len(f.mutations()) != before { t.Fatal("second apply did not converge") }
        })
    }
}

func TestOfficialCLICreatesAgentWithoutRestoringLegacyAvatar(t *testing.T) {
    f, cli := newFixture(t)
    setFixtureAvatar(f, "emoji:old")
    p, _ := exportProject(t, cli)
    legacy := "emoji:old"
    p.Agents[0].AvatarURL = &legacy
    p.Agents[0].AvatarFile = "/nonexistent/avatar.png"
    p.Agents[0].ConversationStarters = nil
    f.mu.Lock()
    f.agent, f.assigned = nil, nil
    f.mu.Unlock()
    if err := (reconcile.Reconciler{Backend: cli}).Apply(p, nil); err != nil { t.Fatal(err) }
    assertNoAvatarRequests(t, f)
    assertNoop(t, cli, p)
}

func assertNoAvatarRequests(t *testing.T, f *fixture) {
    t.Helper()
    for _, mutation := range f.mutations() {
        if strings.Contains(mutation.path, "/avatar") { t.Fatal("avatar endpoint called") }
        if _, ok := mutation.body["avatar_url"]; ok { t.Fatal("avatar in mutation payload") }
    }
}
func setFixtureAvatar(f *fixture, value string) {
    f.mu.Lock()
    defer f.mu.Unlock()
    f.agent["avatar_url"] = value
}
func fixtureAvatar(f *fixture) any {
    f.mu.Lock()
    defer f.mu.Unlock()
    return f.agent["avatar_url"]
}
''')

# Keep the old documentation URL useful, but remove the obsolete build recipe.
Path('docs/emoji-avatars.md').write_text('''# Avatars are unmanaged

Agent and squad avatars are intentionally outside the declarative snapshot.
This includes emoji references, image URLs, image files and empty/clear values.
Manage avatars in Multica itself. Project icons are a separate managed field.

- Export does not serialize avatarUrl/avatarFile, download images or write avatar files.
- Plan ignores avatar drift.
- Apply does not set, clear, upload or restore avatars, including during agent
  creation, unrelated updates, archived-agent updates and multi-workspace apply.
- Legacy agent multica.avatarUrl/multica.avatarFile and squad avatarUrl keys are
  accepted but ignored. Referenced avatar files are not resolved or read; missing
  files and old simultaneous URL/file values do not block import.

Use the ordinary official multica CLI. This repository does not patch Multica,
provide a custom avatar CLI, or bypass the CLI with HTTP mutations.

## Updating existing installations

Update and rebuild/install multica-declarative. Remove any old --multica-bin
argument that points to the retired avatar compatibility binary. The default
uses multica from PATH; --multica-bin may still select an ordinary official CLI.
No Multica server or daemon changes are needed.

Old snapshots work without manual edits. On the next export --force, avatar keys
and generated avatar files disappear from the refreshed snapshot; live avatars
remain unchanged. Keep a copy of an old snapshot before refreshing if those local
image files are still needed outside this tool.

Review a fresh plan before applying, especially after a previous partial failure.
Apply remains non-transactional across workspaces, but avatars cannot cause CLI
capability errors because they are no longer part of desired state.

## Tests

The ordinary unit and official-CLI integration suites cover omitted emoji/images,
old snapshots with missing avatar files, create/update/no-op reconciliation,
archived agents, squads, and preservation of live avatars during managed updates.
''')

edit('AGENTS.md', lambda s: replace(s, '- Treat Multica as a black box.\n', '- Treat Multica as a black box; do not patch its CLI, server or daemon.\n- Agent and squad avatars are intentionally unmanaged. Omit them from export and\n  ignore legacy avatarUrl/avatarFile fields without reading files or mutating avatars.\n'))

# Documentation changes are confined to avatar policy and compatibility setup.
def readme(s):
    start = s.index('An optional, pinned CLI source patch')
    end = s.index('\n\n## Architecture', start)
    s = s[:start] + '''Multica is used unmodified through its official CLI. Agent and squad avatars
are intentionally excluded from export, plan and apply. Git stores desired state
and history; Multica remains responsible for runtime behavior.

> Status: early MVP. The declaration format is `v1alpha1` and may change.
>
> Compatibility baseline: **Multica 0.6.2**, released October 13, 2026.
> Existing declarations remain valid. Full skill bodies use the official CLI.
> No custom CLI build or avatar patch is needed. Legacy `avatarUrl`/`avatarFile`
> fields are accepted but ignored, without reading files or changing live avatars.
> See [avatar policy and migration](docs/emoji-avatars.md) and
> [managed resource boundaries](docs/managed-resources.md).
''' + s[end:]
    s = s.replace('  avatars, archived/unbound state, service tier,', '  archived/unbound state, service tier,')
    s = s.replace('squads with leader, instructions, avatar URL, agent/human members, and roles;', 'squads with leader, instructions, agent/human members, and roles;')
    s = s.replace('- the opt-in `multica-avatar` build for creating, changing, or clearing emoji avatars;\n', '')
    s = s.replace('- Go 1.25+ only when building the reconciler from source; the avatar CLI build auto-downloads the upstream-required toolchain.', '- Go 1.25+ only when building the reconciler from source.')
    s = s.replace('For restoring emoji avatars, build and explicitly select the compatibility CLI\nas described in [emoji avatars](docs/emoji-avatars.md).', 'Avatars are not exported or imported. See [migration notes](docs/emoji-avatars.md) for old snapshots.')
    s = s.replace('All selected workspaces are planned and\nchecked for required avatar-write support before any are applied.', 'All selected workspaces are inspected before any are applied.')
    s = s.replace(' Add the explicit `--multica-bin`\nargument shown above when the apply needs to restore emoji avatars.', '')
    s = s.replace('- Multica remains unmodified except for the explicitly selected CLI-only avatar compatibility patch.', '- Multica remains unmodified. Avatars are intentionally unmanaged.')
    return s

edit('README.md', readme)
# Retain resource documentation, but replace obsolete avatar examples/table rows.
def managed(s):
    lines = []
    for line in s.splitlines():
        if line.startswith('  avatarFile:') or line.startswith('  avatarUrl:') or line.startswith('avatarUrl:'):
            continue
        if line.startswith('|') and ('avatar' in line.lower()):
            lines.append('| Avatars (emoji, URL, file) | Unmanaged: not exported; legacy fields are ignored; live avatars are unchanged. |')
            continue
        lines.append(line)
    text = '\n'.join(lines) + '\n'
    text = text.replace('`avatarFile`', '`avatarFile` (deprecated, ignored)')
    text = text.replace('`avatarUrl`', '`avatarUrl` (deprecated, ignored)')
    return text

edit('docs/managed-resources.md', managed)

# Make the preparation infrastructure disappear from the final PR diff.
Path('.github/workflows/prepare-unmanaged-avatars.yml').unlink()
Path(__file__).unlink()
print('Avatar handling and CLI patch removed. Run gofmt and the complete CI suites.')
