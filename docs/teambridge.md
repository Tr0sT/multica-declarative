# Optional TeamBridge / Mattermost integration

This extension manages the TeamBridge integration in
[`Tr0sT/multica`](https://github.com/Tr0sT/multica). Its contract was checked against
fork commit [`2e634e803699ceb659616513275be1258de7cc98`](https://github.com/Tr0sT/multica/tree/2e634e803699ceb659616513275be1258de7cc98)
and the official Multica CLI 0.6.4. The fork calls the integration `mattermost` in
its API; the declaration directory is called `teambridge`.

The integration is optional. Ordinary export and configurations without any
`teambridge/*/bot.yaml` declarations never call the extension's API or read its
authentication configuration. They continue to work with upstream Multica.

Core agents, skills, squads, projects and autopilots still use the official CLI.
Only this extension uses the authenticated public API: the fork has no equivalent
CLI commands, and its attachment CLI cannot upload the standalone images that
refusal replies require. No patched CLI, database access or server change is used.

## Export and apply

```bash
multica-declarative export --with-teambridge --output-dir ./snapshot
multica-declarative validate --config ./snapshot/multica.yaml
multica-declarative plan --config ./snapshot/multica.yaml
multica-declarative apply --config ./snapshot/multica.yaml
```

For all workspaces, combine the existing flags:

```bash
multica-declarative export --all-workspaces --with-teambridge \
  --without-secrets --output-dir ./multica-config
```

`--with-teambridge` is an **export-only** flag. The presence of declared bots opts
plan/apply into managing them. Deleting a local bot declaration leaves the live
bot untouched; undeclared installations are never revoked or removed.

The selected server must support and enable the integration. An absent endpoint,
disabled integration, incomplete response, unreadable image or authorization
failure is an explicit error. A failed export leaves the previous snapshot intact.
Do not interpret an access or network error as an empty integration.

[`examples/teambridge`](../examples/teambridge) is a minimal offline-valid example.
Replace its runtime selector, server and account settings before applying it.

## Files

```text
snapshot/
  multica.yaml
  agents/reviewer/agent.yaml
  teambridge/reviewer/
    bot.yaml
    observed.json
    images/001.png
    images/002.png
```

Bot directories can be nested into groups and keep their paths on a forced
refresh. `observed.json` preserves public source installation IDs, workspace ID,
installer ID, timestamps and settings for inspection. It is not imported as
desired state: assigning another deployment's audit history or foreign IDs would
be incorrect. Signed image download URLs are never persisted.

Example `bot.yaml`:

```yaml
name: NordBotAskHC
agent: NordBotAskHC
serverUrl: https://teambridge.example.com
botUserId: aaaaaaaaaaaaaaaaaaaaaaaaaa
botUsername: nordbotaskhc
status: active
context:
  mode: channel
  lookbackHours: 8760
accessMode: allowlist
allowedUsers:
  - id: bbbbbbbbbbbbbbbbbbbbbbbbbb
    username: sergey
deniedReply:
  mode: images
  imageFiles:
    - images/001.png
    - images/002.png
accessTokenEnv: NORDBOTASKHC_TOKEN
```

The `agent` field references the name of an agent declared in the same workspace.
The reconciler resolves that name to the destination agent ID after agent
creation. Exactly one bot may be declared per agent. A server/account identity
cannot be declared twice in one workspace.

### Managed fields

| Field | Export | Import behavior |
| --- | --- | --- |
| Agent binding | Agent name | Resolves to the destination agent ID; never reuses the source UUID |
| `serverUrl`, `botUserId` | Yes | Identify the server/account; new credentials are checked against the saved account ID |
| `botUsername` | Yes | Observed account name; renaming a TeamBridge account is outside this tool |
| `context` | Yes | Message, thread or channel, including the full supported lookback range |
| `accessMode` | Yes | `members`, `anyone` or `allowlist` |
| `allowedUsers` | Names and stable account IDs | In-place updates preserve IDs; additions with pinned IDs are verified before granting access |
| Refusal text | Yes | Unicode text up to 4000 characters; empty text selects the server's default reply |
| Refusal images | Actual bytes | Validates, compares and uploads standalone workspace images as needed |
| `status` | Active or revoked | Explicit revoke is supported; reactivation needs a supplied token |
| Bot access token | Unavailable from the API | Existing token is preserved; env/file input is required for creation/reactivation |
| Installation/installer IDs and timestamps | `observed.json` | Source observations only, not writable restore fields |

Context modes are `message`, `thread` and `channel`. Message/thread use a zero
lookback. Channel supports **0, 1, 6, 24, 72, 168, 720, 1440, 2160 and 8760 hours**;
zero means no date cutoff. The fork's compatibility response `channel_extended`
is normalized into `channel` with its actual hours before exporting or comparing.

For a text reply:

```yaml
deniedReply:
  mode: text
  text: "Этот бот пока доступен только участникам команды."
```

For an image reply, use `mode: images` and an ordered list of local `imageFiles`.
The fork's legacy single-image response is normalized into this same list.
PNG, JPEG, GIF and WebP are supported, up to 5 MiB each, verified from the actual
bytes. Image files must stay inside the bot declaration directory, without
symlinks. Source attachment IDs are replaced with target IDs when importing into
a different installation. Existing identical bytes are reused, so reapplying or
reordering an image set does not keep uploading duplicate files.

An empty allowlist blocks everyone. `allowedUsers` is only valid for `allowlist`.
Up to 100 usernames are supported. Username normalization removes a leading `@`,
trims whitespace and lowercases the name. Exported native IDs guard against a
username being renamed or subsequently assigned to a different person.

The fork can retain a dormant list after switching to `members` or `anyone`.
Such entries are preserved in `observed.json`, with an export warning, and are
not declared as active grants. The API does not accept an allowlist while those
modes are selected; the reconciler leaves dormant destination entries untouched.

## Credentials and authentication

The adapter reuses the same selected human Multica profile as the CLI:

- default: `~/.multica/config.json`;
- `--profile NAME`: `~/.multica/profiles/NAME/config.json`;
- `MULTICA_SERVER_URL`, `MULTICA_TOKEN` and `MULTICA_WORKSPACE_ID` override profile
  values; an explicit `--workspace-id` or workspace-set binding overrides the
  workspace environment value.

The Multica credential is sent only to the selected Multica server. API redirects
are rejected. Image storage downloads never receive that credential, and signed
URLs or server response bodies are not included in errors. Daemon/task contexts
and task-scoped `mat_` credentials are rejected; the extension never falls back to
the daemon owner's human profile. Server-side workspace and admin authorization
still apply. Existing-bot settings changes use PATCH and preserve the original
installer, whose permissions can be relevant to public/allowlist runs.

**Existing bot tokens cannot be exported.** This is an intentional server API
boundary. A full export writes a deterministic `accessTokenEnv` reference; its
value is not fetched or written. A normal in-place import does not need it.

To create or reactivate a bot, supply the referenced environment variable using
your normal secret-management mechanism, or change the declaration to:

```yaml
accessTokenFile: credentials/bot-token
```

The file contains only the bot token and is relative to the bot directory. Use
either the environment reference or the file reference. Token files may be absent
during `validate` and ordinary in-place apply; their contents are read only when
required. New secret material never appears in subprocess arguments or plans.

Before a fresh install, the reconciler uses the supplied bot token for read-only
TeamBridge account lookups. It checks that the token belongs to the expected bot
and that every pinned allowed username still identifies its saved account ID.
This requires the TeamBridge server to be reachable from the machine running
the reconciler, in addition to Multica's own connectivity. The username lookup
uses Mattermost's read-only `POST /api/v4/users/usernames` endpoint; it does not
create posts or accounts.

For existing installations, unchanged names keep the server's stable grants.
Adding a new name with an exported ID also requires token-backed verification.
A manually added name without an ID explicitly requests normal username
resolution. If the same saved name already has a different pinned ID on the
destination, preflight fails; the tool does not silently transfer that grant.

`--without-secrets` / `secrets: omit` skips both token references without opening
files or reading variables. Existing settings can still be changed. Creating or
reactivating a bot fails preflight under this policy because it requires a token.
To perform that operation, explicitly use a full declaration with a supplied
credential. The policy never clears the destination token.

## Refresh and operational limits

Refresh a snapshot that includes the extension with:

```bash
multica-declarative export --with-teambridge --force --output-dir ./snapshot
```

Omitting the flag when the destination already contains `teambridge/` fails
instead of silently dropping or leaving stale integration settings. All source
images are read and the complete generated snapshot is validated before any
generated paths are replaced. `--all-workspaces` stages and validates every
workspace before replacing the exported set.

A full refresh keeps a manually chosen `accessTokenEnv`. If an existing bot
declaration references `accessTokenFile`, full refresh refuses to discard that
file, even if the bot was removed from the source. Move the credential to your
secret store and use an environment reference before refreshing. Explicit
`--without-secrets` allows replacing generated credential files and dropping
their references; unrelated files and Git history are still outside that cleanup.

Apply preflights every selected workspace before its first write. Credentials,
known capability failures and readable image data are checked at that stage.
Apply is not a server transaction: a later network error or concurrent server
change can leave partial changes, including an uploaded but unused image. A
successful apply converges; repeating it does not reinstall active bots or rotate
their credentials.

Changing the server or bot account of an existing agent can erase account/chat
bindings in the fork. The reconciler therefore rejects that replacement and
requires an explicit change in Multica. An already-active bot cannot be attached
to a second agent/workspace simultaneously; the server enforces this ownership.

Revoked records are exported and compare without writes when unchanged. They can
be reactivated with an explicit `status: active` plus token. The API cannot edit
a revoked record or recreate a revoked row on a fresh installation without
temporarily activating it; those cases fail preflight. Remove a historical record
from the desired snapshot or explicitly choose activation before migration.

This is a **declarative configuration export, not a full Multica backup**. The API
does not expose bot tokens, user-account binding records, chat route mappings,
conversation history, inbound/outbound queues, deduplication state, binding
tokens, WebSocket leases or deployment secrets. Deployment variables such as
`MULTICA_MATTERMOST_SECRET_KEY`, `MULTICA_MATTERMOST_ALLOWED_SERVERS`, application
URL and object storage configuration remain part of the deployment configuration.
Full disaster recovery still requires the application's database/storage and
deployment backup. The exporter reports this boundary instead of claiming these
unavailable values were saved.

## Verification

Unit tests cover API shapes, authentication scope, credential-free signed
downloads, source-file validation, rollback on export failure, settings
convergence, image reuse/remapping, native account identity checks and multi-
workspace preflight. Real CLI integration tests use the pinned official binary
against isolated synthetic servers to verify both ordinary upstream behavior and
the optional fork API path. No live workspace or real TeamBridge account is used.
