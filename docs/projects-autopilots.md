# Projects and autopilots

Projects and autopilots participate in the same `export`, `validate`, `plan`, and
`apply` workflow as agents, skills, and squads. `apply` is the import command;
there is no separate `import` command. These are **configuration snapshots**, not
backups of issues, run history, comments, timestamps, quotas, or statistics.

## Layout and commands

```text
multica.yaml
agents/...
projects/game/project.yaml
projects/game/PROJECT.md
autopilots/nightly-review/autopilot.yaml
autopilots/nightly-review/AUTOPILOT.md
```

```bash
multica-declarative export --output-dir ./snapshot
multica-declarative validate --config ./snapshot/multica.yaml
multica-declarative plan --config ./snapshot/multica.yaml
# Review the plan before importing:
multica-declarative apply --config ./snapshot/multica.yaml
```

Both collections are discovered recursively. Parent directories may group
resources, and `export --force` preserves those directories by declaration name.
Forced export replaces `projects/` and `autopilots/` along with the previously
generated paths; a complete staged snapshot is validated before any replacement.
Unrelated repository files remain untouched. A workspace containing only projects
is valid.

## Projects

```yaml
name: Game prototype
descriptionFile: PROJECT.md
status: in_progress
icon: "🎮"
lead:
  type: agent
  agent: Builder
startDate: "2026-09-01"
dueDate: "2026-10-01"
resources:
  - type: github_repo
    ref:
      url: https://github.com/example/game
      ref: main
    label: Source
    position: 10
```

`name` maps to the Multica project title and is also its matching key.
`description` can be inline instead of `descriptionFile`; exported descriptions
are written to `PROJECT.md` without trimming or newline conversion. A member lead
uses `{type: member, id: <member-UUID>}`. Export represents no lead as
`{type: none}`. Omitting `lead` leaves it unmanaged on existing projects.

Supported status values are `planned` (default), `in_progress`, `paused`,
`completed`, and `cancelled`. Empty description, icon, start date, and due date
clear the corresponding values. Dates are calendar days, `YYYY-MM-DD`.

Resources support the official `github_repo` and `local_directory` types. A local
resource uses `ref.local_path`, `ref.daemon_id`, and optional `ref.label` and
`ref.execution_mode` (`in_place` or `worktree`). Daemon IDs and filesystem paths
are installation-specific; review them when moving to another workspace. Unknown
ref fields and credential-bearing HTTP URLs are rejected rather than silently
lost or passed as secrets in CLI process arguments. Use runtime Git credentials.

`priority` is exported and compared when present, but the current official CLI
has no priority mutation flag. An unchanged value is safe; changes and creating
a project with a non-default priority fail in preflight. The server's creation
default is `none`. Similarly, setting/changing a lead is supported, but clearing
an existing lead is not supported by this CLI and is rejected before any writes.

## Autopilots

```yaml
name: Nightly review
descriptionFile: AUTOPILOT.md
agent: Builder
project: Game prototype
mode: create_issue
status: paused
issueTitleTemplate: "Review {{date}}"
subscribers: []
triggers:
  - kind: schedule
    enabled: true
    cron: "0 9 * * 1-5"
    timezone: Europe/Berlin
    label: Weekdays
  - kind: webhook
    enabled: false
    label: On demand
```

`name` maps to the autopilot title. The description is its run prompt and is
exported to `AUTOPILOT.md` byte-for-byte. Exactly one of `agent` or `squad` must
reference a declared resource; `project`, when present, references a declared
project by name. IDs are resolved from the target workspace, never copied from
source project/agent identities. Member `subscribers` are UUIDs and must exist in
the target workspace; an empty list explicitly clears subscribers.

`mode` is `create_issue` or `run_only`. `status` is `active` or `paused`; hand-written
declarations default to **paused**. Export preserves the actual status. A schedule
requires `cron`; `timezone` defaults to `UTC`. `enabled` defaults to false when
omitted. The server validates the full cron grammar. Only `{{date}}` interpolation
is supported in issue title templates.

Projects are applied after agents/squads; autopilots are applied last. Changed
autopilots are paused before editing their configuration or triggers, and restored
to the declared status only after those writes succeed. Creation first creates an
untriggered autopilot (the CLI has no create-status flag), then pauses it before
adding triggers. A failed update stays paused. The process is not a server-side
transaction and does not stop runs already in progress. Importing an `active`
autopilot can start scheduled/webhook runs after reactivation; use `paused` for a
staged migration. The tool never calls manual trigger/run or rotates webhook URLs.

### Explicit compatibility boundaries

The implementation uses the official Multica 0.4.42 CLI exclusively for mutations.
It does not bypass missing CLI commands through database access or direct HTTP.

Existing squad-assigned autopilots can be exported, checked and edited without
changing their assignee. The CLI only exposes `--agent`: creating/reassigning an
autopilot to a squad is rejected. `collaborators`, webhook `provider`,
`hasSigningSecret`, and `eventFilters` are also exported as observe-only fields.
Changes to these fields, or recreation that would lose them, fail in preflight.
Omitting an observe-only field leaves it unmanaged; it does not restore that
setting in a different workspace. Existing `api` triggers can be observed and
updated; the CLI cannot create that trigger kind.

**Webhook credentials are not a restorable part of this snapshot.** Existing
triggers retain their tokens/URLs during in-place updates. New webhook triggers
receive new URLs from Multica, so callers must be reconfigured after migration.
Signing secrets are write-only and cannot be exported or restored with this CLI.
Export warns about webhook credentials, never requests `--show-secrets`, and does
not put tokens or URLs into YAML, logs, or plan output. Retrieve new URLs separately
through an authorized human session. Collaborator grant timestamps/creator IDs,
pause reasons, and run history are not configuration and are not copied.

## Identity and collection ownership

Top-level resources are matched by exact name/title. Duplicate names and unresolved
references fail explicitly. Renaming a declaration creates a different resource;
undeclared top-level projects and autopilots are **never deleted**.

`resources`, `triggers`, and `subscribers` are optional managed collections:
omission (or YAML `null`) leaves them untouched; `[]` declares an empty collection.
Export writes explicit collections. Consequently removing an exported resource or
trigger from its parent's list detaches/deletes that child on apply. Deleting a
webhook child invalidates its URL. Review the `resources`/`triggers` changes in the
plan before applying; re-adding that webhook will create a new URL.

Child `id` values in exports are in-place identity hints. Matching reserves existing
IDs first, then matches resource type/ref or trigger kind/schedule/label, then a
unique non-empty label for edited children. This makes repeat imports with source
IDs converge in a different workspace. Ambiguous labels fail instead of guessing.
Keep exported IDs for in-place editing and use stable, unique labels when moving
snapshots. Changing an unmatched, unlabelled trigger can require recreation.

## Verification

CI uses the actual checksum-verified CLI with a synthetic loopback API. It tests
export/re-import, body preservation, named dependencies, foreign child IDs,
no-op reapply, updates and explicit clears, child deletion, omitted collections,
read-only preflight, failed-update pausing, incomplete-read protection, and grouped
forced export. Unit tests cover strict YAML, dates, safe relative files, references,
duplicate resources, child matching, and malformed CLI responses. No live workspace
or production credentials are used.

Upstream contracts: [project CLI](https://github.com/multica-ai/multica/blob/v0.4.42/server/cmd/multica/cmd_project.go)
and [autopilot CLI](https://github.com/multica-ai/multica/blob/v0.4.42/server/cmd/multica/cmd_autopilot.go).
