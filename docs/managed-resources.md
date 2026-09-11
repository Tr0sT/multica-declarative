# Managed Multica resources

This document describes the resources and fields supported by `multica-declarative` v0.5 (tested with Multica 0.4.42).
The official `multica` CLI remains the compatibility boundary. A field is never changed through
direct database access or an undocumented HTTP endpoint.

## Workspace manifest

```yaml
apiVersion: multica-declarative/v1alpha1

runtimes:
  desktop:
    customName: Main PC
    provider: codex
```

Agent, skill, and squad declarations are discovered recursively. Every directory below `agents/`
that contains `agent.yaml` is an agent; `SKILL.md` below `skills/` and `squad.yaml` below `squads/`
define the other resource roots. Directories without a marker are grouping directories, so layouts
such as `agents/codex/builder/agent.yaml`, `skills/unity/testing/SKILL.md`, and
`squads/gameplay/reviewers/squad.yaml` require no manifest entries. Once a marker is found,
discovery does not descend further into that resource directory.

`export --force` preserves the relative directory of every existing skill, agent, and squad it can
match by declaration name. Newly discovered resources are written directly below their collection
directory using a generated slug.

## Agents

An agent may use a compact scalar permission or the expanded form below.

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
  serviceTier: priority
  maxConcurrentTasks: 1

  permission:
    mode: public_to
    workspace: true
    members:
      - 00000000-0000-0000-0000-000000000000

  customArgs:
    - --full-auto

  # Secret values are declarative state stored beside the agent.
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
| runtime, runtimeConfig | yes | yes | Omitting `runtimeConfig` declares an empty object unless `preserveSecrets` is true. |
| model, thinkingLevel, maxConcurrentTasks | yes | yes | Runtime-specific values are passed through, not translated. |
| serviceTier | yes | yes | Omitted = unmanaged; `""` = inherit local Codex configuration; `default` = Standard; other values come from the runtime catalog. |
| conversationStarters | yes | observe-only | Ordered `label`/`prompt` entries. Omitted = unmanaged; explicit `[]` asserts empty. Changes/creation with non-empty starters fail before writes. |
| systemKey | yes | observe-only | Product-managed identity, exported only when present. Cannot be changed or recreated as an ordinary agent. |
| unbound | yes | existing agents only | Explicitly exports an agent whose runtime was removed. Cannot create/detach through this CLI; choosing `runtime` again supports rebinding. |
| customArgs | yes | yes | Exported verbatim in full mode; omitted/unmanaged in secret-free mode. |
| preserveSecrets | yes | yes | `true` leaves custom env, private MCP, runtime config and custom args unmanaged, even during unrelated edits. |
| private/workspace/member invocation permissions | yes | yes | Team targets are rejected because the CLI does not support them. |
| skill assignments | yes | enabled skills only | Disabled assignments are exported and compared, but the CLI cannot change their enabled flag. |
| customEnvFile | yes | yes | Export writes `custom-env.json` beside `agent.yaml`. Use `{}` to clear. |
| mcpConfigFile | yes | yes | Export writes `mcp.json`; export fails if Multica returns a redacted config. A file containing `null` clears it. |
| avatarFile | yes | yes | Image files only; export downloads HTTP(S) images when possible. Mutually exclusive with avatarUrl. |
| avatarUrl | yes | CLI capability required | Lossless `emoji:` reference; omitted = unmanaged, `""` = clear. Stock 0.4.42 lacks the setter; see [emoji avatars](emoji-avatars.md). |
| archived | yes | yes | Omitting `archived` declares an active agent (`false`). |
| disabledRuntimeSkills | yes | observe-only | Apply fails clearly when a change is requested. |
| composioToolkitAllowlist | yes | observe-only | Apply fails clearly when a change is requested. |

Observe-only fields are preserved by `export` and checked during `plan`/`apply` preflight. They are not silently dropped.
Creating an agent that requires a non-empty observe-only field is rejected, because the official CLI
cannot faithfully reproduce it in a different workspace.

Empty `disabledRuntimeSkills` and `composioToolkitAllowlist` values may be omitted; omission declares
an empty list and still participates in drift detection.

### Secret files

`export --without-secrets` omits these files and sets `multica.preserveSecrets: true`.
The policy survives loading and ordinary import. `plan/apply --without-secrets`
forces the same policy for a full snapshot, without reading its secret files.
See [secret-free snapshots](secret-free-snapshots.md) for scope and precautions.
The rules below apply when these fields are managed (the default).

`customEnvFile` must contain a JSON object of string values:

```json
{
  "OPENAI_API_KEY": "..."
}
```

`mcpConfigFile` must contain a JSON object or `null`, matching the official CLI
contract. Export writes both files with local
mode `0600`; they are part of the desired state and should be committed with the agent declaration.
All resource file references must be relative regular files inside their declaration directory;
absolute paths, parent traversal, and symlinks are rejected.

Do not put secret values in `customArgs` or `runtimeConfig`: the official Multica CLI accepts those
fields only as command arguments. Use `customEnvFile` or `mcpConfigFile` for secret-bearing data.

## Squads

```yaml
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

## Multica 0.4.42 compatibility boundaries

The existing `v1alpha1` format remains valid. Newly introduced `serviceTier`,
`conversationStarters`, and `systemKey` fields are unmanaged when absent, so an
older declaration cannot silently clear settings it never described. Export
writes explicit service-tier and conversation-starter values, including empty
ones. A system agent's `system_instructions` are maintained by Multica itself;
only its workspace-owned `instructions` are exported.

An existing agent with no runtime is represented explicitly:

```yaml
name: Disconnected Agent
multica:
  unbound: true
  serviceTier: ""
  conversationStarters: []
  maxConcurrentTasks: 1
  permission: private
```

`unbound: true` and `runtime` are mutually exclusive. This snapshot can be
validated and reconciled against the same unbound agent. To recreate or bind it,
remove `unbound` and provide a runtime selector. Creating a product-managed agent
with `systemKey` still requires Multica to provision that identity.

The CLI can read conversation starters but has no create/update flag for them.
They are kept as ordered `label`/`prompt` entries; at most three, with label and
prompt limits of 80 and 4,000 Unicode code points. A requested difference in
these or other observe-only fields fails preflight before any mutation rather
than pretending it can be applied. Model IDs, thinking levels and service tiers
are runtime-defined; validation does not replace the runtime's model catalog.

Skill reads explicitly request `--with-content`. A missing/null body or file
list is rejected before `plan`, `apply`, or export can treat it as empty data.
Explicitly empty bodies remain distinguishable from missing fields. No external
CLI adapter is needed.

Full export refuses redacted MCP configurations, redacted Composio allowlists, and
masked `runtimeConfig.gateway.token` credentials. The `***` placeholder is not
a usable secret and must not be committed as a replacement credential. Existing
snapshots are not replaced on these failures. Use an appropriately authorized
human profile; do not bypass task-scoped authentication restrictions. Secret-free
mode omits MCP/runtime values, so their redaction does not prevent that export.

Workspace MCP library entries are **write-only**, even for owners; the official
CLI lists only IDs, names and transports. That library and its per-agent
assignments are outside this schema. Export emits a warning whenever the library
is non-empty. Preserve their original configuration separately; this exporter
cannot be used as a complete workspace backup. It also does not manage issues,
chats, task history, or machine/runtime provisioning.

Source contracts (pinned to the tested release):
[skill content](https://github.com/multica-ai/multica/blob/v0.4.42/server/cmd/multica/cmd_skill.go),
[agent flags](https://github.com/multica-ai/multica/blob/v0.4.42/server/cmd/multica/cmd_agent.go),
[agent response and validation](https://github.com/multica-ai/multica/blob/v0.4.42/server/internal/handler/agent.go),
[workspace MCP write-only boundary](https://github.com/multica-ai/multica/blob/v0.4.42/server/cmd/multica/cmd_workspace.go).

## Projects and autopilots

See [projects-autopilots.md](projects-autopilots.md) for the additional resource
collections, CLI compatibility table in prose, and migration safety details.
