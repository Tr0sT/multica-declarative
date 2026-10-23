# Export and import without secrets

Full exports retain the existing behavior: custom environment values are written
as `custom-env.json`, per-agent MCP configuration as `mcp.json`, and both files are
referenced by `agent.yaml`. With `--with-teambridge`, real stored bot tokens are
also written to private `credentials/bot-token` files. Full export includes
secrets by default. **Never publish a full snapshot in public Git.** This is a
snapshot of supported declarative resources, not every internal deployment secret
or the complete application database.

## Omit secret-bearing settings

```bash
multica-declarative export --without-secrets --output-dir ./snapshot
multica-declarative validate --config ./snapshot/multica.yaml
multica-declarative plan --config ./snapshot/multica.yaml
# Review the whole plan before importing it.
multica-declarative apply --config ./snapshot/multica.yaml
```

The export does not call `agent env get`. It discards per-agent MCP data returned
by `agent get` (including redacted data) instead of writing it. It omits these
**entire** agent fields, not just values whose keys look like passwords:

- `customEnvFile` and `custom-env.json` (all custom environment variables);
- `mcpConfigFile` and `mcp.json` (all per-agent MCP configuration);
- `runtimeConfig` (including gateway tokens and other provider configuration);
- `customArgs` (CLI arguments can contain arbitrary credentials).

Non-secret settings inside those fields are also omitted. This deliberate
tradeoff avoids guessing which arbitrary keys or positional arguments are secret.
Other agent settings, instructions, skill assignments, squads, projects,
and autopilot configuration continue to be exported normally. Existing webhook
credential exclusions and workspace MCP-library limitations are unchanged.

The manifest records the import policy:

```yaml
apiVersion: multica-declarative/v1alpha1
secrets: omit
# Runtime selectors, when present, follow here.
```

`secrets: omit` means **unmanaged**, not cleared. `validate`, `plan`, and `apply`
automatically honor it, even without `--without-secrets`. They do not open secret
JSON files, do not compare these fields for drift, and do not send these fields
in create/update requests. Existing credentials stay intact even when another
agent setting changes. Missing/empty secret files cannot trigger a clear in this
mode. No placeholders such as `***`, `{}`, or `null` are generated as credentials.

New agents are created without these settings; configure their credentials and
any omitted runtime/MCP options separately before running them. The mode does not
invent credentials or bypass server validation, authentication, or permissions.

## Import an older full snapshot while preserving destination secrets

The flag also works with snapshots that still contain or reference secret files:

```bash
multica-declarative plan --without-secrets --config ./full-snapshot/multica.yaml
multica-declarative apply --without-secrets --config ./full-snapshot/multica.yaml
```

Both commands must use the same policy. Alternatively, add `secrets: omit` to the
manifest to make it persistent. Referenced secret JSON files need not be copied
to the importing machine. These commands do not sanitize or delete local files.

An omitted `secrets` field or explicit `secrets: include` retains full-snapshot
semantics. `--without-secrets` can restrict an `include` manifest, but
`--without-secrets=false` cannot override `secrets: omit`. Unknown policy values
are rejected. Older reconciler builds reject the new manifest field rather than
silently interpreting omitted runtime configuration as an instruction to clear it.

In full mode, omission of `customEnvFile` / `mcpConfigFile` already leaves those
settings unmanaged; explicit files containing `{}` / `null` still clear them.
Omission of `runtimeConfig` / `customArgs` keeps the previous empty-value semantics
unless the new preservation policy is selected. To intentionally manage secrets
again, remove/change `secrets: omit` and supply and review the desired values.

## Refresh and publication boundaries

```bash
multica-declarative export --without-secrets --force --output-dir ./snapshot
```

A successful forced refresh replaces generated resource directories, so old
agent secret files inside them do not remain in the resulting snapshot. As with
normal export, the complete staged snapshot must validate before replacement.
Failed export leaves the previous snapshot intact, which may still contain secrets.
Unrelated files, `.git/`, commit history, backups, and other copies are untouched.
Normal export without the flag can replace this snapshot with a full one again.

**This is not a general-purpose secret scanner.** Credentials manually embedded
in instructions, descriptions, URLs, skill bodies, or supporting scripts/files
are not detected or scrubbed. Review such content and Git history before sharing
or publishing a snapshot. Authentication remains the responsibility of the
selected Multica CLI; this option neither removes task context nor elevates it.

## Optional TeamBridge integration

`export --with-teambridge` includes public bot configuration, reply image bytes
and, by default, exact stored bot tokens. The selected fork CLI and server must
both implement `teambridge token export/import`; token operations require a human
owner/admin of the selected workspace. A token export failure rejects the full
snapshot before replacing existing files.

The token is stored in `teambridge/<bot>/credentials/bot-token` (mode `0600`,
credentials directory `0700` on POSIX), with `accessTokenFile` in `bot.yaml`.
`accessTokenEnv` remains supported for manually maintained declarations. Full
plan compares managed tokens in memory; full apply restores a changed existing
token through `teambridge token import`, or uses it to create/reactivate a bot.
Token-only updates preserve identity, status, settings, installer, history and
bindings. Repeated apply performs no redundant token writes. Absent token
references leave existing credentials unmanaged; an explicit missing/empty
reference fails preflight.

The selected CLI owns Multica authentication and all network requests. Private
temporary files carry credentials and are removed on success or failure. Token
values and their fingerprints never appear in arguments, stdout/stderr or plan
diffs. Stored-token export does not require a live Mattermost connection.

With `--without-secrets` or `secrets: omit`, export never invokes secret commands
or creates token references/files. Import does not read referenced token files,
read their variables, or read/update server tokens. Settings for existing bots
still reconcile. Creating or reactivating a bot fails preflight before any
selected workspace is written because a token is required. A secretless refresh
removes previously generated tokens identified by the bot's generated-path
manifest, while preserving unowned local files and Git history. It is not a
scrubber for credentials in arbitrary user files. See
[TeamBridge authentication and identity checks](teambridge.md).
