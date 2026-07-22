# Managed Multica resources

This document describes the resources and fields supported by `multica-declarative` v0.4.
The official `multica` CLI remains the compatibility boundary. A field is never changed through
direct database access or an undocumented HTTP endpoint.

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

Agent and skill declarations are discovered recursively. Every directory below `agents/` that
contains `agent.yaml` is an agent; every directory below `skills/` that contains `SKILL.md` is a
skill. Directories without a marker are grouping directories, so layouts such as
`agents/codex/builder/agent.yaml` and `skills/unity/testing/SKILL.md` require no manifest entries.
Once a marker is found, discovery does not descend further into that resource directory.

`export --force` preserves the relative directory of every existing agent it can match by the
`name` in `agent.yaml`. Newly discovered Multica agents are written directly below `agents/` using
their generated slug.

## Agents

An agent may use the compact legacy form or the expanded form below.

```yaml
name: Unity Builder
description: Implements Unity tasks.
instructionsFile: AGENT.md

model:
  id: gpt-5.6

skills:
  - unity-development
  - name: optional-review-checks
    enabled: false

multica:
  runtime: desktop
  runtimeConfig:
    sandbox: strict
  thinkingLevel: high
  maxConcurrentTasks: 1

  permission:
    mode: public_to
    workspace: true
    members:
      - 00000000-0000-0000-0000-000000000000

  customArgs:
    - --full-auto

  # Secret values are read locally and are never emitted by export.
  customEnvFile: custom-env.json
  mcpConfigFile: mcp.json

  avatarFile: avatar.png
  archived: false

  # These fields are observable in the current CLI, but not mutable.
  disabledRuntimeSkills:
    - runtimeId: 00000000-0000-0000-0000-000000000000
      provider: codex
      root: universal
      key: project-global

  composioToolkitAllowlist:
    - github
```

### Agent field support

| Field | Plan/export | Apply | Notes |
|---|---:|---:|---|
| name, description, instructions | yes | yes | Name is currently also the identity key. |
| runtime, runtimeConfig | yes | yes | `runtimeConfig` is managed only when present in YAML. |
| model, thinkingLevel, maxConcurrentTasks | yes | yes | |
| customArgs | yes | yes | Exported verbatim with the rest of the declaration. |
| private/workspace/member invocation permissions | yes | yes | Team targets are rejected because the CLI does not support them. |
| skill assignments | yes | enabled skills only | Disabled assignments are exported and compared, but the CLI cannot change their enabled flag. |
| customEnvFile | yes | yes | Export writes `custom-env.json` beside `agent.yaml`. Use `{}` to clear. |
| mcpConfigFile | yes | yes | Export writes `mcp.json`; export fails if Multica returns a redacted config. A file containing `null` clears it. |
| avatarFile | yes | yes | Export downloads the current avatar when possible. |
| archived | yes | yes | Managed only when explicitly present in YAML. |
| disabledRuntimeSkills | yes | observe-only | Apply fails clearly when a change is requested. |
| composioToolkitAllowlist | yes | observe-only | Apply fails clearly when a change is requested. |

Observe-only fields are preserved by `export` and included in `plan`. They are not silently dropped.
Creating an agent that requires a non-empty observe-only field is rejected, because the official CLI
cannot faithfully reproduce it in a different workspace.

### Secret files

`customEnvFile` must contain a JSON object of string values:

```json
{
  "OPENAI_API_KEY": "..."
}
```

`mcpConfigFile` must contain any valid JSON accepted by Multica. Export writes both files with local
mode `0600`; they are part of the desired state and should be committed with the agent declaration.

## Squads

```yaml
kind: Squad
name: Unity Team
description: Implements and reviews Unity tasks.
instructionsFile: SQUAD.md
leader: Unity Builder
avatarUrl: https://example.invalid/team.png

members:
  - type: agent
    agent: Unity Reviewer
    role: reviewer

  - type: member
    id: 00000000-0000-0000-0000-000000000000
    role: observer
```

The leader and agent members reference agents by declaration name. Human members use their Multica
member UUID. `plan` and `apply` manage description, instructions, leader, avatar URL, member set,
and member roles. A leader is always reconciled as an agent member with role `leader`.
