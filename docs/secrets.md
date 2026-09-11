# Export and import without secrets

Full exports retain the existing behavior: custom environment values are written
as `custom-env.json`, per-agent MCP configuration as `mcp.json`, and both files are
referenced by `agent.yaml`. Store full snapshots privately.

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
Other agent settings, instructions, skill assignments, avatars, squads, projects,
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
