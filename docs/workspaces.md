# Multiple workspaces

One repository can now contain independent declarations for several **existing
workspaces in one Multica installation**. Each workspace has its own resource
names, runtime selectors, secrets policy, and export directory. Neither export
nor apply changes the interactive CLI's active workspace or affects daemons.

## Export all accessible workspaces

```bash
multica-declarative export --all-workspaces --output-dir ./multica-config
# Use an explicitly selected Multica profile, omitting secret-bearing settings:
multica-declarative export --all-workspaces --profile hustle \
  --without-secrets --output-dir ./multica-config
```

`--all-workspaces` is opt-in. Ordinary `export` keeps the previous single-workspace
behavior and directory format. Full exports still include secret-bearing agent
settings by default; use `--without-secrets` for a shareable snapshot, subject to
the existing warning that free-form instructions/skill files are not scanned for
credentials.

The set export uses the official `multica workspace list --output json` and reads
**every workspace accessible to that CLI identity**, not just the selected one
or the daemon's watch list. Empty workspaces are represented too.

```text
multica-config/
├── multica.yaml
├── hustlecastle/
│   ├── multica.yaml
│   ├── agents/
│   ├── skills/
│   ├── squads/
│   ├── projects/
│   └── autopilots/
└── experiments/
    ├── multica.yaml
    ├── agents/
    ├── skills/
    ├── squads/
    ├── projects/
    └── autopilots/
```

Workspace directories live directly beside the root `multica.yaml`, with no
intermediate collection directory. The root manifest binds each directory to a
workspace ID:

```yaml
apiVersion: multica-declarative/v1alpha1
secrets: omit  # Written when exporting without secrets; otherwise omitted.
workspaces:
  hustlecastle:
    id: 11111111-1111-4111-8111-111111111111
  experiments:
    id: 22222222-2222-4222-8222-222222222222
```

Each key identifies **exactly** `<key>/multica.yaml` relative to the root manifest.
Export initially uses the server workspace slug as the key. Keys are portable,
lowercase directory components (letters, digits, `_`, `-`); paths, `..`, symlinks
in managed paths, duplicate IDs, nested sets, and unknown YAML fields are rejected.
A direct child with a `multica.yaml` must be listed in the root mapping; unrelated
root directories without a manifest and hidden repository metadata such as `.git/`
are ignored. You can rename a directory and its root key together; refresh preserves
the mapping by ID. Workspace IDs, not directory names, decide where writes go.

Child manifests use the unchanged single-workspace format (`apiVersion`,
`runtimes`, optional `secrets`). Existing recursive grouping inside collections
continues to work, for example `agents/main/reviewer/` or `skills/shared/unity/`.
The same resource name can occur in different workspaces without being merged.
References between agents, skills, squads, projects, and autopilots resolve only
inside the corresponding child declaration; there is no cross-workspace sharing.

## Validate, plan, apply

```bash
multica-declarative validate --config ./multica-config/multica.yaml
multica-declarative plan --profile hustle --config ./multica-config/multica.yaml
multica-declarative apply --profile hustle --config ./multica-config/multica.yaml
```

The format is detected automatically; `--all-workspaces` is only an export flag.
Validation is offline. Plan/apply validate the selected children, check that all
target IDs are accessible in the selected profile, and plan **all** selected
workspaces before applying the first one. Output is grouped by workspace.

Each underlying CLI command receives an explicit `--workspace-id`. An inherited
`MULTICA_WORKSPACE_ID`, an interactive `workspace switch`, or an active workspace
in the UI cannot redirect a workspace-set operation. `--profile` is forwarded to
every command, including workspace discovery. Without it, normal Multica CLI
profile/environment resolution applies. Profiles and authentication tokens are
not stored in the declaration.

Select just one workspace with its child manifest:

```bash
multica-declarative plan --profile hustle \
  --config ./multica-config/experiments/multica.yaml
multica-declarative apply --profile hustle \
  --config ./multica-config/experiments/multica.yaml
```

A directly selected child retains its workspace ID **and the root's secrets
policy**. `--workspace-id` cannot override set bindings. It remains available for
legacy standalone declarations:

```bash
multica-declarative export --workspace-id WORKSPACE_UUID --output-dir ./single
multica-declarative plan --workspace-id WORKSPACE_UUID --config ./single/multica.yaml
multica-declarative apply --workspace-id WORKSPACE_UUID --config ./single/multica.yaml
```

As before, standalone snapshots do not record a target. For a legacy snapshot
without an explicit flag, the official CLI resolves the workspace from the
environment or the selected profile default.

## Refresh and safety

```bash
multica-declarative export --all-workspaces --profile hustle \
  --output-dir ./multica-config --force
```

Every workspace is staged and the complete set is validated **before** replacing
existing output. An error while reading workspace B does not install a partially
refreshed workspace A. The installer backs up the root manifest and each bound
workspace directory and rolls back on ordinary installation errors; failed recovery
retains the backup for manual repair. This is not a crash-proof filesystem transaction,
and concurrent exports to the same output directory are not supported.

Refresh preserves `.git/` and unrelated root files/directories, per-workspace notes
outside the generated collections, and grouping directories of existing declarations.
A new workspace slug cannot overwrite an existing root directory that was not bound
in the previous manifest: export fails on that collision even with `--force`.
Within each workspace, generated resource collections retain the existing export
replacement semantics. Root `secrets: omit` remains effective on subsequent set
refreshes even without repeating the flag; generated secret files are removed
when refreshing a full snapshot with `--without-secrets`.

If a previously exported workspace disappears from the accessible list, refresh
fails instead of silently deleting its local snapshot. To intentionally stop
including it, archive its directory outside the export root and remove its root
manifest entry. Undeclared server resources are still never pruned.

Flat export cannot overwrite a set root, even with `--force`. A flat refresh of
a child output directory automatically uses the enclosing set's workspace ID
and root secret policy. Exporting a set into an existing flat snapshot is rejected;
use a different output directory when migrating formats.

Apply is **not** a cross-workspace server transaction. Access/local/preflight
errors stop before writes, but a later API or apply-time compatibility failure
can leave earlier operations applied. Fix the failure, review `plan`, and apply
again; normal per-workspace convergence is unchanged. Do not edit declarations
while a command is running.

## Scope and migration

This feature manages workspace **contents**, not workspace lifecycle, membership,
or runtime registration. It does not create/rename/delete workspaces or provision
daemons/runtimes. Create destination workspaces and register their runtimes first.
To restore into different existing workspaces/another installation, explicitly
change the root IDs and any workspace-specific runtime selectors, human IDs,
permissions and other non-portable references, then review the plan. No name/slug
fallback silently retargets an inaccessible ID.

To migrate existing snapshots without re-exporting, put each existing snapshot
under `<key>/` directly beside the root routing manifest above. No changes to agent,
skill, squad, project, or autopilot declarations are required.

Snapshots from the earlier revision of this PR used an extra `workspaces/` directory.
Move its workspace directories up one level (checking for name collisions first)
and remove the empty wrapper, or export into a new directory. The root mapping and
workspace IDs do not need to change.
