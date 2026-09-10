# multica-declarative

**Manage Multica agents, skills, squads, projects, and autopilots as code.**

`multica-declarative` is a standalone Go CLI that reads version-controlled YAML and
[Agent Skills](https://agentskills.io/) directories, compares them with a Multica workspace,
and reconciles the difference through the official `multica` CLI.

The reconciler does not access Multica's database or bypass its CLI for HTTP mutations.
An optional, pinned CLI source patch supplies the missing emoji avatar setter in 0.4.42
(see [emoji avatars](docs/emoji-avatars.md)); the server and daemon are not changed. Git stores desired state and history; Multica remains responsible for runtime behavior.

> Status: early MVP. The declaration format is `v1alpha1` and may change.
>
> Compatibility baseline: **Multica 0.4.42**, released September 9, 2026.
> Version 0.5 requests full skill bodies directly through the official CLI;
> no `--multica-bin` compatibility wrapper is needed. Existing declarations remain
> valid. New agent fields are opt-in for older YAML; export preserves them explicitly.
> See [compatibility boundaries](docs/managed-resources.md#multica-0442-compatibility-boundaries)
> for read-only fields and resources that cannot be fully exported.
>
> Emoji avatars now export losslessly as `multica.avatarUrl: "emoji:🌞"`.
> Stock 0.4.42 can export/compare these snapshots, but cannot restore a changed emoji.
> For writes, build the opt-in CLI with `make avatar-cli` and pass
> `--multica-bin /path/to/bin/multica-avatar`. An unsupported write fails before
> any resource mutation. [Setup and migration](docs/emoji-avatars.md).


## Architecture

```text
Git repository
  ├── multica.yaml
  ├── agents/
  ├── skills/
  ├── squads/
  ├── projects/
  └── autopilots/
          │
          ▼
multica-declarative export / validate / plan / apply
          │
          ▼
official multica CLI --output json
          │
          ▼
Multica
```

## Current support

- strict workspace and resource YAML validation;
- recursive agent, skill, squad, project, and autopilot discovery, allowing arbitrary grouping directories;
- standard Agent Skills directories with `SKILL.md` and supporting text files;
- agents with instructions, runtime and runtime config, model, reasoning level, concurrency,
  custom arguments, invocation permissions, skill assignments, custom env files, MCP config files,
  avatars, archived/unbound state, service tier, and observe-only conversation starters/system identity;
- squads with leader, instructions, avatar URL, agent/human members, and roles;
- projects with descriptions, leads, dates, resources, and autopilots with prompts, project/agent references, schedules/webhooks, and subscribers;
- read-only export into round-trippable declarations;
- reviewable `plan` output;
- convergent `apply` through the official CLI.

Existing resources are currently matched by exact name. Renaming remains unsafe until stable external
keys and a state file are added.

## Requirements

- an authenticated **Multica 0.4.42** CLI and compatible server (the tested baseline);
- Go 1.25+ only when building from source.

Verify the Multica profile first:

```bash
multica skill list --output json
multica agent list --output json
multica runtime list --output json
multica squad list --output json
```

## Install

```bash
git clone git@github.com:Tr0sT/multica-declarative.git
cd multica-declarative
go build -o ./bin/multica-declarative ./cmd/multica-declarative
```

Or:

```bash
go install github.com/Tr0sT/multica-declarative/cmd/multica-declarative@latest
```

## Bootstrap an existing workspace

```bash
multica-declarative export --output-dir ./my-workspace
cd my-workspace
multica-declarative validate
multica-declarative plan
```

The exporter is read-only with respect to Multica. It writes agents, skills, squads, runtime
selectors, and agent secrets. Custom environment values are stored as `custom-env.json`; MCP
configuration is stored as `mcp.json`. Both files live beside `agent.yaml`, are referenced from it,
and are intended to be version-controlled with the rest of the declaration. Export fails rather
than writing an MCP configuration that Multica returned in redacted form.

Refreshing is explicit:

```bash
multica-declarative export --output-dir ./my-workspace --force
```

During a refresh, existing skills, agents, and squads are matched by declaration name. Their
relative directories are preserved, including grouping directories such as `agents/main/`,
`agents/vds/`, or `skills/shared/`. A resource not already present in the export tree is created
directly under its collection directory using a generated slug.

`--force` replaces only generated `multica.yaml`, `agents/`, `skills/`, `squads/`, `projects/`, and `autopilots/` paths.
Unrelated files and `.git/` are preserved.

## Commands

```bash
multica-declarative export --output-dir ./snapshot
multica-declarative validate --config ./snapshot/multica.yaml
multica-declarative plan --config ./snapshot/multica.yaml
multica-declarative apply --config ./snapshot/multica.yaml
```

Flags may appear before or after the command. Use `--multica-bin` to select another Multica binary.

### `export`

Reads actual state and creates a complete local snapshot before installing it. Export refuses unsafe
file paths, duplicate identities, unsupported team-scoped invocation permissions, and other lossy
conversions. Legacy skill bodies receive valid Agent Skills frontmatter with a warning.

### `validate`

Loads all local declarations and checks references without contacting Multica.

### `plan`

Reads actual state and reports create/update/no-change operations without mutating Multica.

### `apply`

Creates and updates declared resources. Undeclared top-level resources are never removed. Supporting
files inside a declared skill and squad members are fully reconciled.

## Workspace manifest

```yaml
apiVersion: multica-declarative/v1alpha1

runtimes:
  desktop:
    customName: Main PC
    provider: codex
```

Skills, agents, and squads are discovered recursively under their respective top-level directories;
they are not listed in the manifest. A directory containing `SKILL.md`, `agent.yaml`, or
`squad.yaml` is the corresponding resource root. Parent directories may group resources by runtime
or any other convention. Discovery stops at a resource root, so its supporting subdirectories are
not scanned as separate resources.

A runtime selector may use `id`, `name`, `customName`, `provider`, or a combination. It must resolve
to exactly one runtime.

## Skills

```text
skills/game-engines/unity-development/
├── SKILL.md
├── references/
└── scripts/
```

`SKILL.md` must begin with Agent Skills frontmatter:

```markdown
---
name: unity-development
description: Unity implementation and validation conventions.
---

# Unity development
```

Additional skill files must be non-empty UTF-8 text because the current Multica skill file surface is
text-oriented.

## Projects and autopilots

See [docs/projects-autopilots.md](docs/projects-autopilots.md) for YAML examples,
import/export behavior, safe activation, child identity and webhook/CLI limitations.
Projects and autopilots use the same `export`, `validate`, `plan`, and `apply` commands.
Old declarations that omit these collections leave existing server resources untouched.

## Agents and squads

See [docs/managed-resources.md](docs/managed-resources.md) for complete examples, secret-file rules,
and a field-by-field compatibility table.

Important compatibility boundary:

- fields exposed by official CLI create/update commands are fully reconciled;
- observable server fields without a CLI mutation command are exported and compared, but `apply`
  rejects a requested change instead of bypassing the CLI.

## Safety model

- `export` and `plan` never mutate Multica;
- export validates a complete staging snapshot before replacing generated files;
- non-empty export targets require `--force`;
- undeclared agents, skills, squads, projects, and autopilots are untouched;
- top-level pruning is not implemented;
- secret values are exported to agent JSON files with local mode `0600` and are never printed in plans;
- custom env and MCP values are passed to Multica by file, not embedded in process arguments;
- unsupported or lossy operations fail explicitly.

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
go build -o ./bin/multica-declarative ./cmd/multica-declarative
```

Or:

```bash
make check
make build
```

Integration tests execute the **real official CLI** against a synthetic local HTTP server:

```bash
MULTICA_BIN=/absolute/path/to/multica make integration
```

The binary must match `integration/multica.lock.json`; missing binaries fail the
explicit integration target rather than silently skipping tests. Tests isolate
all authentication and task environment values and never contact a live workspace.
They cover full-content skill reads, byte-preserving export, create/update/no-op
reconciliation, explicit service-tier clearing, legacy omission, unbound edits and
rebinding, and fail-closed behavior for incomplete responses. CI downloads the
pinned CLI, verifies its SHA-256, and runs these tests with the race detector.

The backend is a Go interface. Reconciliation and export are unit-tested without a real workspace;
separate tests verify generated Multica CLI arguments and round-trip YAML behavior.

## Planned work

1. stable resource keys and a Terraform-like state file;
2. incremental/selective export;
3. machine-readable plans and drift/conflict detection;
4. JSON Schema and editor completion;
5. ownership-aware `--prune`;
6. release binaries and explicit CI apply workflows (a Nix flake is already included).

## Principles

- Desired state belongs in Git.
- Multica remains unmodified.
- The official CLI is the compatibility boundary.
- Apply must converge.
- Destructive and lossy behavior must be explicit.
- Secret material is declarative state stored in Git beside its agent; plans, logs, shell history,
  and command arguments must not print its values.
