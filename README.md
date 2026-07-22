# multica-declarative

**Manage Multica agents, skills, and squads as code.**

`multica-declarative` is a standalone Go CLI that reads version-controlled YAML and
[Agent Skills](https://agentskills.io/) directories, compares them with a Multica workspace,
and reconciles the difference through the official `multica` CLI.

The project deliberately does not fork Multica, access its database, or call undocumented HTTP
endpoints. Git stores desired state and history; Multica remains responsible for runtime behavior.

> Status: early MVP. The declaration format is `v1alpha1` and may change.

## Architecture

```text
Git repository
  ├── multica.yaml
  ├── agents/
  ├── skills/
  └── squads/
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
- recursive agent and skill discovery, allowing arbitrary grouping directories;
- standard Agent Skills directories with `SKILL.md` and supporting text files;
- agents with instructions, runtime and runtime config, model, reasoning level, concurrency,
  custom arguments, invocation permissions, skill assignments, custom env files, MCP config files,
  avatars, and archived state;
- squads with leader, instructions, avatar URL, agent/human members, and roles;
- read-only export into round-trippable declarations;
- reviewable `plan` output;
- convergent `apply` through the official CLI.

Existing resources are currently matched by exact name. Renaming remains unsafe until stable external
keys and a state file are added.

## Requirements

- a recent authenticated `multica` CLI;
- Go 1.23+ only when building from source.

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

The exporter is read-only with respect to Multica. It writes agents, skills, squads, and runtime
selectors. Secret values from custom environment variables and MCP config are
intentionally not exported; add local `customEnvFile` and `mcpConfigFile` references manually.

Refreshing is explicit:

```bash
multica-declarative export --output-dir ./my-workspace --force
```

During a refresh, existing `agents/**/agent.yaml` files are matched to Multica agents by `name`.
Their relative directories are preserved, including grouping directories such as `agents/main/`
and `agents/vds/`. An agent not already present in the export tree is created directly under
`agents/<slug>/`.

`--force` replaces only generated `multica.yaml`, `agents/`, `skills/`, and `squads/` paths.
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

squads:
  - squads/unity-team/squad.yaml

runtimes:
  desktop:
    customName: Main PC
    provider: codex
```

Skills and agents are discovered recursively under `skills/` and `agents/`; they are not listed in
the manifest. A directory containing `SKILL.md` is a skill root, and one containing `agent.yaml` is
an agent root. Parent directories may be used to group resources by runtime or any other convention.
Discovery stops at a resource root, so its supporting subdirectories are not scanned as separate
resources.

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
- undeclared agents, skills, and squads are untouched;
- top-level pruning is not implemented;
- secret values are never emitted by export or printed in plans;
- custom env and MCP values are passed to Multica by file, not embedded in process arguments;
- unsupported or lossy operations fail explicitly.

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
go build ./cmd/multica-declarative
```

Or:

```bash
make check
make build
```

The backend is a Go interface. Reconciliation and export are unit-tested without a real workspace;
separate tests verify generated Multica CLI arguments and round-trip YAML behavior.

## Planned work

1. stable resource keys and a Terraform-like state file;
2. incremental/selective export;
3. machine-readable plans and drift/conflict detection;
4. JSON Schema and editor completion;
5. secret-provider integrations such as SOPS/age;
6. ownership-aware `--prune`;
7. release binaries, Nix packaging, and CI apply workflows.

## Principles

- Desired state belongs in Git.
- Multica remains unmodified.
- The official CLI is the compatibility boundary.
- Apply must converge.
- Destructive and lossy behavior must be explicit.
- Secret material must not appear in Git diffs, plans, logs, shell history, or command arguments.
