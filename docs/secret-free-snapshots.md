# Snapshots without agent secrets

Full export remains the default for backward compatibility. Use the explicit
`--without-secrets` flag to export configuration while keeping agent credentials
out of the generated settings:

```bash
multica-declarative export --output-dir ./snapshot --without-secrets
# Refresh an existing generated snapshot (review the diff afterwards):
multica-declarative export --output-dir ./snapshot --force --without-secrets
```

This mode omits the **entire** `customEnvFile`, `mcpConfigFile`, `runtimeConfig`,
and `customArgs` fields. It never calls `agent env get`, and does not write
`custom-env.json` or `mcp.json`. Runtime configuration and custom arguments are
opaque and can contain credentials under arbitrary keys; omitting only familiar
names such as `token` would not be sufficient. Consequently, even non-secret
settings inside these four fields are omitted. All other supported settings,
including instructions, skills, avatars, projects and autopilots, keep their
existing export behavior.

Agent detail reads may still return MCP/custom-argument values because the
current CLI has no metadata-only agent read. These values are discarded before
snapshot serialization; they are not printed or copied into generated files.
Redacted private MCP values and masked gateway tokens do not prevent this mode
from exporting the other settings. Permissions for other fields are unchanged;
this flag does not bypass authorization or task-scoped authentication.

## Import does not clear or replay credentials

Each exported `agent.yaml` records the policy with the agent itself:

```yaml
name: Reviewer
multica:
  runtime: desktop
  preserveSecrets: true
  maxConcurrentTasks: 1
  permission: private
```

This is **unmanaged state**, not a placeholder credential and not an instruction
to clear anything. Normal commands respect it automatically:

```bash
multica-declarative validate --config ./snapshot/multica.yaml
multica-declarative plan --config ./snapshot/multica.yaml
# Review the entire plan before importing:
multica-declarative apply --config ./snapshot/multica.yaml
```

For this agent, `plan` does not read/compare custom env or compare the other three
secret-bearing settings. `apply` does not call `agent env set` and does not send
`--mcp-config-file`, `--runtime-config`, or `--custom-args`, even when updating
another field. It neither merges nor replays observed secrets, so credentials
rotated between a plan and an apply are not overwritten by stale values.
Explicit local `{}`, `null`, or other secret field contents are ignored while
`preserveSecrets: true` is set. Secret file references are not opened or required.

To import a **full or older snapshot** without its secrets, the same flag is
available for `validate`, `plan`, and `apply`:

```bash
multica-declarative plan --config ./full-snapshot/multica.yaml --without-secrets
multica-declarative apply --config ./full-snapshot/multica.yaml --without-secrets
```

The flag forces preservation for every agent for that invocation. It does not
edit the snapshot. Passing `--without-secrets=false` does not override an agent's
persisted `preserveSecrets: true`; remove that policy explicitly to manage its
secret-bearing settings again. In ordinary full mode, existing behavior remains:
`customEnvFile` with `{}` clears env, `mcpConfigFile` with `null` clears private
MCP, and omitted runtime config/custom arguments still declare empty values.

A **new agent** has no credentials to preserve. It is created without these four
settings; provision them separately before executing it. Import is not a backup
or migration of authentication. Other existing import limits still apply, and an
`active` autopilot snapshot may activate execution as described in
[projects and autopilots](projects-autopilots.md).

## Existing exports and the limits of this mode

A successful `--force --without-secrets` refresh replaces generated resource
directories, removing old secret JSON files and inline runtime/custom-argument
values from those directories. Grouping directories are preserved by resource
name. A failed export leaves the previous snapshot intact, including any secrets
it already held; it is not a scrub operation on failure.

Unrelated files and `.git/` are preserved. This does **not** remove secrets from
Git history, other branches, backups, unrelated directories, or old artifacts.
Use a fresh output directory when creating a shareable snapshot and review its
contents before publication.

This flag is **not a credential scanner**. It does not redact tokens hardcoded in
instructions, skill scripts/supporting files, descriptions, resource URLs or
other free-form text. Those are still exported verbatim. Existing webhook
credentials are excluded in both export modes; new webhook triggers receive new
URLs, and write-only signing secrets cannot be backed up by the CLI. Workspace
MCP library limitations are unchanged.
