# Optional TeamBridge / Mattermost integration

This extension manages the TeamBridge integration in
[`Tr0sT/multica`](https://github.com/Tr0sT/multica) through that fork's
`multica teambridge` commands. `mattermost` is a CLI alias for the same commands;
the declaration directory is called `teambridge`.

The integration is optional. Ordinary export and configurations without any
`teambridge/*/bot.yaml` declarations never invoke extension commands. They
continue to work with the official Multica CLI and upstream server.

All resource operations, account verification and image transfers use the selected
Multica CLI. The reconciler does not make direct HTTP requests, parse Multica
authentication files or access a database.

## CLI requirements

Use a fork CLI that implements `teambridge token export/import`, together with a
fork server supporting its administrative token API and a configured integration.
The ordinary list endpoint remains secret-free. Full exports fail explicitly
when either side lacks the contract or the selected human is not an owner/admin. The exact fork revision exercised
in CI is recorded in
[`integration/multica-teambridge.lock.json`](../integration/multica-teambridge.lock.json).
Ordinary upstream compatibility is tested independently with official CLI 0.6.5,
pinned in [`integration/multica.lock.json`](../integration/multica.lock.json).

Check the selected binary and profile:

```bash
/path/to/fork/multica teambridge --help
/path/to/fork/multica --profile production teambridge list --output json
```

Put this binary on `PATH`, or pass `--multica-bin /path/to/fork/multica` to each
export, plan and apply invocation. No automatic binary switching or HTTP fallback
is performed. An official CLI without these commands produces a clear error
only when the extension is requested; it remains sufficient for core resources.

To build the fork CLI from source, use the Go version required by its
`server/go.mod`:

```bash
git clone https://github.com/Tr0sT/multica.git multica-fork
cd multica-fork/server
go build -o ../bin/multica ./cmd/multica
../bin/multica teambridge --help
```

### CLI contract

Every invocation requests `--output json` and carries the same `--profile` and
`--workspace-id` scope as the core resource operations.

| Command | Purpose |
| --- | --- |
| `teambridge list` | Read the complete public installation list and capability flags |
| `teambridge install --agent UUID --config-file PATH` | Create/reactivate a bot using a private JSON payload |
| `teambridge update UUID --config-file PATH` | Change settings while preserving the stored token and installer |
| `teambridge token export UUID --output-file PATH` | Export the stored token to a new private file; stdout contains safe IDs and `exported: true` |
| `teambridge token import UUID --token-file PATH` | Restore only the token, keeping identity, status, settings, installer and bindings; stdin is also supported |
| `teambridge revoke UUID` | Revoke a declared installation |
| `teambridge verify --config-file PATH` | Check bot and allowed-account identities without modifying them |
| `teambridge image download UUID --output-file PATH` | Download a standalone refusal image and return its metadata |
| `teambridge image upload PATH` | Upload a standalone refusal image and return its new attachment ID |

Payloads and image transfers use a temporary directory with mode `0700` and files
with mode `0600` on POSIX systems. Windows uses the temporary location's filesystem
ACLs. Temporary files are removed after the command finishes, including error
paths. Credentials are never placed in subprocess arguments.

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

The selected CLI and server must support and enable the integration. An absent
command or endpoint, disabled integration, incomplete response, unreadable image or authorization
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
attachmentMode: links
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
| `attachmentMode` | When reported by the fork CLI/server | `none` or `links`; omission leaves the destination unchanged |
| `accessMode` | Yes | `members`, `anyone` or `allowlist` |
| `allowedUsers` | Names and stable account IDs | In-place updates preserve IDs; additions with pinned IDs are verified before granting access |
| Refusal text | Yes | Unicode text up to 4000 characters; empty text selects the server's default reply |
| Refusal images | Actual bytes | Validates, compares and uploads standalone workspace images as needed |
| `status` | Active or revoked | Explicit revoke is supported; reactivation needs a supplied token |
| Bot access token | Exported via the owner/admin human token contract to `credentials/bot-token` | Managed tokens are compared privately and restored without reinstall; absent references or `secrets: omit` leave the destination token unchanged |
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

## Inbound attachments

`attachmentMode` controls how incoming Mattermost files reach the agent:

```yaml
attachmentMode: links
```

- `links` forwards incoming attachment metadata with on-demand links. It does not
  embed or pre-download every attachment into the agent context.
- `none` excludes incoming attachments. Explicit `none` is an actual setting,
  not the same as omitting the key.

The setting is independent of message/thread/channel context and refusal images.
Exports from the current fork preserve its exact value in `bot.yaml` and
`observed.json`. Both full and secretless snapshots manage this non-secret field.
Legacy YAML (or explicit YAML null) leaves it unmanaged: ordinary updates do not
send `attachment_mode`, and reactivation preserves an observed destination value.
New installations without it use the fork server's default.

Older CLIs/servers that do not report this field produce an export warning, and
no default value is invented. Applying an explicit value to an existing bot whose
CLI/server does not report the field fails during preflight, before any workspace
write. Unknown modes and null/invalid API values fail explicitly. Requested writes
are checked against the returned value; a server that ignores the field is not
reported as success. As with other apply errors, an API failure after a request
may leave partial changes. No compatibility patch or HTTP fallback is used.

Use the updated fork CLI **and** server. CI pins fork commit
`6c5856c4a54ffbd296d96b812cb14225fd54e210`, which already implements this setting;
this repository does not modify the fork.

## Credentials and authentication

The selected CLI resolves its own human Multica profile and authentication:

- default: `~/.multica/config.json`;
- `--profile NAME`: `~/.multica/profiles/NAME/config.json`;
- `MULTICA_SERVER_URL`, `MULTICA_TOKEN` and `MULTICA_WORKSPACE_ID` override profile
  values; an explicit `--workspace-id` or workspace-set binding overrides the
  workspace environment value.

The fork CLI keeps the Multica credential on requests to the selected Multica
server and does not send it to image storage. The integration commands reject
daemon/task contexts and task-scoped `mat_` credentials. Server-side workspace
and admin authorization still apply. `multica-declarative` neither reads these
profile files nor implements a separate authentication path. Existing-bot
settings changes use `teambridge update`, preserving the original token and
installer, whose permissions can be relevant to public/allowlist runs.

**Full export includes the stored bot token by default** when `--with-teambridge`
selects the integration. No extra token flag is needed. The exporter calls
`teambridge token export`, retains the exact value in a private file, and writes:

```yaml
accessTokenFile: credentials/bot-token
```

The file is relative to the bot directory, has mode `0600`, and its credentials
directory has mode `0700` on POSIX. The token never appears in `observed.json`,
ordinary YAML, diagnostics, command arguments or plans. **Full snapshots contain
live secrets: do not publish them in public Git.** Backup reads the saved value
without contacting Mattermost, so a Mattermost outage does not prevent export.

`accessTokenEnv` remains supported for manually maintained declarations. Choose
one reference. Declared files/variables are read during full plan/apply even for
existing bots; missing, empty, oversized, insecure or invalid credentials fail
preflight rather than silently becoming unmanaged. `validate` checks declaration
shape and paths without requiring credentials to be provisioned. With neither
reference in a legacy declaration, the destination token is unmanaged.

Existing tokens are compared only in memory. Plan reports the field `accessToken`
without its value, diff or fingerprint. Apply uses `teambridge token import` only
when it differs; it does not reinstall or reactivate the bot, alter the installer,
or replace settings/bindings. Reapplying the same value performs no token write
or connection restart. A changed token is checked against the same bot/server
before any selected workspace is written, and validated again by the server at
restoration. Invalid/revoked tokens and tokens of a different bot are rejected.

Before a fresh install, the reconciler calls `multica teambridge verify` with
the supplied bot token in a private file. The CLI performs read-only TeamBridge
account lookups. It checks that the token belongs to the expected bot
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
images and, in full mode, stored tokens are read and the complete generated snapshot is validated before any
generated paths are replaced. `--all-workspaces` stages and validates every
workspace before replacing the exported set.

A full refresh updates its own generated token files. Each bot's
`.multica-generated.json` records only generated relative paths, never secret
values or fingerprints. A secretless refresh removes those generated token files
and their references. Unowned local files are preserved, including old manually
supplied token files; a collision with a newly generated path fails without
replacing the snapshot. A legacy `accessTokenEnv` is replaced by the exported
`accessTokenFile` reference. Git history and unrelated files are untouched.

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
have their managed tokens restored without activation. They can be reactivated
with an explicit `status: active` plus token. Other settings of a revoked record
cannot be changed, nor can a revoked row be recreated on a fresh installation
without temporarily activating it; those cases fail preflight. Remove a historical record
from the desired snapshot or explicitly choose activation before migration.

This is a **declarative configuration export, not a full Multica backup**. The API
does not expose user-account binding records, chat route mappings,
conversation history, inbound/outbound queues, deduplication state, binding
tokens, WebSocket leases or deployment secrets. Deployment variables such as
`MULTICA_MATTERMOST_SECRET_KEY`, `MULTICA_MATTERMOST_ALLOWED_SERVERS`, application
URL and object storage configuration remain part of the deployment configuration.
Full disaster recovery still requires the application's database/storage and
deployment backup. The exporter reports this boundary instead of claiming these
unavailable values were saved.

## Verification

Unit tests cover CLI arguments and JSON shapes, profile/workspace forwarding,
temporary credential files and cleanup, source-file validation, rollback on
export failure, settings convergence, image reuse/remapping, native account
identity checks and multi-workspace preflight. The fork tests its own HTTP,
authentication, identity-verification and image-transfer implementation.

Two independent real-CLI integration suites use isolated synthetic servers:

```bash
# Official upstream compatibility, including an explicit unsupported extension.
MULTICA_BIN=/path/to/official/multica go test -race -tags=integration ./...

# Fork CLI contract, export/apply round trips and image transfers.
MULTICA_BIN=/path/to/fork/multica \
  make integration-teambridge
```

The second lane builds the pinned fork revision before testing. No live workspace
or real TeamBridge account is used by either suite.
