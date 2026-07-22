# multica-declarative

**Manage Multica agents and skills as code.**

`multica-declarative` is a standalone Go CLI that reads version-controlled YAML and [Agent Skills](https://agentskills.io/) directories, compares them with a Multica workspace, and applies the required changes through the official `multica` CLI.

The project exists because agents and skills configured only through a UI are hard to review, diff, reproduce, and roll back. Git should hold the desired state and its history; Multica should continue to own and run the applied state.

> Status: early MVP. The file format is `v1alpha1` and can change.

## What we are building

```text
Git repository
  ├── multica.yaml
  ├── agents/*.yaml
  └── skills/*/SKILL.md
          │
          ▼
multica-declarative export / validate / plan / apply
          │
          ▼
official multica CLI --output json
          │
          ▼
Multica API and database
```

The controller deliberately does **not**:

- fork or patch Multica;
- read or write Multica's database;
- depend on undocumented HTTP endpoints;
- replace the Multica CLI;
- delete undeclared top-level Multica resources.

Multica is treated as a black box with a supported command-line interface.

## Why Go

The first prototype was written in Python to validate the model quickly. The project is now implemented in Go so users get:

- one self-contained executable;
- no Python installation, virtual environment, or runtime dependencies;
- straightforward Linux, macOS, Windows, and NixOS distribution;
- typed configuration and reconciliation code;
- fast startup and predictable deployment on Multica runtimes.

The YAML parser is the only external Go dependency.

## Intended workflow

1. Bootstrap an existing workspace once with `multica-declarative export`, or start from the example.
2. Edit an agent instruction, model, runtime binding, or skill in Git.
3. Review the normal Git diff.
4. Run `multica-declarative plan` to compare desired and actual state.
5. Run `multica-declarative apply` to reconcile the workspace.
6. Roll back by reverting a Git commit and applying again.

This gives us history, code review, reproducibility, drift visibility, and a path toward CI-driven deployment without moving configuration ownership into Multica's database.

## Current MVP

The current version supports:

- loading and strictly validating a workspace manifest;
- standard Agent Skills directories with `SKILL.md`;
- additional UTF-8 files inside a skill directory;
- declarative prompt agents;
- runtime selection by ID, runtime name, custom name, and provider;
- agent model, instructions, thinking level, concurrency, custom arguments, permissions, and skill assignments;
- read-only `export` of current active agents, all workspace skills, and referenced runtimes;
- `plan` with create/update/no-change output;
- idempotent `apply` through the official Multica CLI;
- full synchronization of files inside managed skills.

Existing skills and agents are matched by exact name. Renaming a managed object is therefore not safe yet. Stable external identities and a local state file are planned before the format is considered stable.

## Requirements

To run a built binary:

- a recent `multica` CLI;
- an authenticated Multica CLI profile.

Run `multica setup` first and verify:

```bash
multica skill list --output json
multica agent list --output json
multica runtime list --output json
```

The controller inherits the Multica CLI environment and profile. It does not store Multica tokens.

Go 1.23 or newer is required only when building from source.

## Install

### Build from source

```bash
git clone git@github.com:Tr0sT/multica-declarative.git
cd multica-declarative
go build -o ./bin/multica-declarative ./cmd/multica-declarative
```

Then either run `./bin/multica-declarative` or place it on `PATH`.

### Install with Go

```bash
go install github.com/Tr0sT/multica-declarative/cmd/multica-declarative@latest
```

Prebuilt release artifacts and Nix packaging are planned.

## Quick start

### Bootstrap an existing Multica workspace

Export the current workspace into a new directory:

```bash
multica-declarative export --output-dir ./my-workspace
cd my-workspace
multica-declarative validate
multica-declarative plan
```

`export` reads Multica through the official CLI and does not mutate the workspace. By default it refuses to write into a non-empty directory. To refresh an existing export while preserving unrelated files in that directory:

```bash
multica-declarative export --output-dir ./my-workspace --force
```

With `--force`, only the generated `multica.yaml`, `agents/`, and `skills/` paths are replaced. Other files such as `README.md` or `.git/` are preserved.

### Start from the example

Copy the example:

```bash
cp -R examples/basic my-workspace
cd my-workspace
```

Adjust the runtime selector in `multica.yaml`, then run:

```bash
multica-declarative validate
multica-declarative plan
multica-declarative apply
```

Flags may be written before or after the command:

```bash
multica-declarative --config ./environments/home/multica.yaml plan
multica-declarative plan --config ./environments/home/multica.yaml
```

Use another Multica binary when necessary:

```bash
multica-declarative plan \
  --multica-bin /run/current-system/sw/bin/multica
```

## Workspace manifest

`multica.yaml` declares which files belong to the desired workspace and how logical runtime aliases map to real Multica runtimes.

```yaml
apiVersion: multica-declarative/v1alpha1
kind: Workspace

skills:
  - skills/unity-development
  - skills/noesis-gui

agents:
  - agents/unity-developer/agent.yaml

runtimes:
  main-desktop:
    customName: Main PC
    provider: codex

  home-vds:
    name: vds
    provider: codex
```

Runtime selectors can use any combination of:

```yaml
runtimes:
  by-id:
    id: 43fca772-0000-0000-0000-000000000000

  by-name:
    name: desktop
    provider: codex

  by-custom-name:
    customName: Main PC
    provider: codex
```

A selector must match exactly one runtime. Zero or multiple matches stop `plan` and `apply` instead of guessing.

Unknown fields in `multica.yaml` and agent YAML files are rejected. This is intentional: a misspelled field must not silently produce a different agent.

## Skills

Skills use the open Agent Skills directory format. Every declared skill directory must contain `SKILL.md` with YAML frontmatter:

```markdown
---
name: unity-development
description: Unity implementation and validation conventions.
metadata:
  version: "1.0.0"
---

# Unity development

Follow the project architecture...
```

Additional files are synchronized into the Multica skill using their path relative to the skill directory:

```text
skills/unity-development/
├── SKILL.md
├── references/
│   └── testing.md
└── scripts/
    └── compile.sh
```

The current Multica skill-file API is text-oriented, so the MVP accepts non-empty UTF-8 files only. Binary assets are not supported yet.

The contents of a declared skill directory are fully managed. A remote file inside that skill which is absent locally is deleted during `apply`.

## Agents

The agent YAML is intentionally small and readable. Its shape is inspired by Microsoft Agent Framework's declarative prompt agents, but this project does not currently claim full Microsoft schema compatibility.

```yaml
kind: Prompt
name: Unity Developer
description: Implements and reviews Unity client tasks.
instructionsFile: AGENT.md

model:
  id: gpt-5.6

skills:
  - unity-development
  - noesis-gui

multica:
  runtime: main-desktop
  thinkingLevel: high
  maxConcurrentTasks: 1
  permission: private
  customArgs:
    - --full-auto
```

Instructions can be inline:

```yaml
instructions: |
  Implement the assigned task directly.
  Compile and test the result.
```

Or stored in a file relative to the agent YAML:

```yaml
instructionsFile: AGENT.md
```

Supported permissions:

- `private`: only the owner can invoke the agent;
- `workspace`: every workspace member can invoke the agent.

## Commands

### `export`

Reads the current Multica workspace and writes a declarative snapshot. It exports all workspace skills, all active non-archived agents, and only the runtimes referenced by those agents.

```bash
multica-declarative export --output-dir ./multica-export
```

Generated layout:

```text
multica-export/
├── multica.yaml
├── agents/
│   └── <agent>/
│       ├── agent.yaml
│       └── AGENT.md
└── skills/
    └── <skill>/
        ├── SKILL.md
        └── ... additional skill files
```

Runtime selectors prefer a unique `customName` and provider, then a unique runtime name and provider, and fall back to the runtime ID when necessary. Directory names are deterministic slugs; declarations retain the exact Multica resource names.

The exporter refuses lossy conversion. For example, member-only or team-only `public_to` invocation permissions are not representable by the current `private|workspace` schema, so export fails before writing files instead of silently changing access. If a legacy skill does not contain compatible Agent Skills frontmatter, valid frontmatter is generated and a warning is printed; the first `plan` may then show a content or description update.

### `validate`

Loads all declarations and checks local consistency without contacting Multica.

```bash
multica-declarative validate
```

### `plan`

Reads actual state through `multica ... --output json` and prints proposed changes without mutating Multica.

```bash
multica-declarative plan
```

Example:

```text
~ skill unity-development [content, files]
= skill noesis-gui
~ agent Unity Developer [model, thinkingLevel, skills]

Plan: 0 to create, 2 to update, 1 unchanged.
```

### `apply`

Creates missing managed resources and updates drifted managed resources.

```bash
multica-declarative apply
```

`apply` is intended to converge: running it again with the same declarations should produce no further changes.

## Safety model

The MVP follows conservative rules:

- `export` is read-only with respect to Multica and validates the complete snapshot before writing;
- export output directories must be empty unless `--force` is explicit;
- only resources explicitly declared in the manifest are managed;
- undeclared Multica skills and agents are untouched;
- no pruning or destructive deletion of top-level resources exists yet;
- skill files are fully managed inside each declared skill;
- runtime selectors must be unambiguous;
- duplicate agents with the same exact name cause an error;
- secrets and custom environment variables are outside the first version;
- CLI failures include stderr but secret values are never introduced by this tool.

## Relationship to existing formats

The project avoids inventing everything from scratch:

- **Skills:** standard Agent Skills `SKILL.md` directories.
- **Agents:** a narrow declarative prompt-agent shape influenced by Microsoft Agent Framework.
- **Multica deployment:** explicit `multica:` fields for runtime binding, reasoning level, concurrency, permissions, and CLI arguments.
- **Execution:** the official Multica CLI is the only backend.

Oracle Agent Spec may become an import/export adapter later, but implementing the entire cross-framework specification is outside the MVP.

## Development

```bash
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/multica-declarative
```

Or:

```bash
make check
make build
```

The backend is represented by a Go interface, so reconciliation is tested without a real Multica workspace. Separate tests verify the generated `multica` CLI arguments.

## Planned work

Likely next steps:

1. stable local resource keys and a Terraform-like state file so objects can be renamed safely;
2. incremental export/update modes and selective resource filters;
3. explicit drift reporting and machine-readable plans;
4. JSON Schema files and editor completion;
5. secret references for `custom_env` and MCP configuration without storing values in Git;
6. optional `--prune` with an explicit ownership model and safety checks;
7. squads, runtime profiles, and other workspace resources;
8. release binaries, Nix packaging, and CI-driven apply workflows.

## Development principles

- Desired state belongs in Git.
- Multica remains unmodified and owns runtime behavior.
- The official CLI is the compatibility boundary.
- Plans must be reviewable.
- Apply must converge instead of producing one-off scripts.
- Destructive behavior must be explicit.
- Secret values must never appear in Git diffs, plans, logs, shell history, or process arguments.
