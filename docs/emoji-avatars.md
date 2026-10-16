# Emoji avatars

Multica stores emoji avatars as literal references such as `emoji:🌞`, not image
URLs. Export preserves them directly in `agent.yaml`:

```yaml
name: Example Agent
multica:
  runtime: desktop
  avatarUrl: "emoji:🌞"
```

The value is preserved byte-for-byte, including variation selectors, skin-tone
modifiers and joined sequences such as `emoji:👩🏽‍💻`. Export and plan never try to
download it or render it into an image. Existing HTTP(S) image avatars still
export through `avatarFile`.

`avatarUrl` currently accepts an `emoji:` reference or an empty string to clear
an avatar. `avatarUrl` and `avatarFile` are mutually exclusive. Omit both to leave
the remote avatar unmanaged, preserving the meaning of older declarations.
Squad avatar URLs and project icons already have their own CLI-supported fields
and are unchanged by this fix.

## Why restoration also needs a CLI flag

The official **Multica 0.6.2** CLI (released October 13, 2026) can read emoji
references, but its `agent avatar` command still only uploads image files;
`agent update` has no avatar flag. Upgrading the stock CLI from 0.4.42 does not
remove this limitation. The server already accepts `avatar_url` on the same
update endpoint that the CLI uses.

This repository includes an **opt-in, narrowly scoped source patch** adding
`agent update --avatar-url`. It does not add an HTTP fallback to the reconciler,
change auth, read tokens, remove task-context markers, or update the server or
daemon. It is a patched upstream CLI build, not an official release, and reports
version `0.6.2+avatar.1`.

The build is pinned to upstream commit
`7ab92d7622ecd81d179a8d83ae2dea74835a5069` (v0.6.2). The complete patch is
[`compat/multica-avatar.patch`](../compat/multica-avatar.patch).
It adds only a flag and a payload field to the existing update command, plus
mentions the flag in its no-fields error. The v0.6.2 patch preserves the new
conversation-starters handling.

## Build and use

From this repository, on Linux or macOS with Git, Bash, Make and Go installed:

```bash
make build avatar-cli
```

The avatar target downloads the pinned upstream sources and Go dependencies,
applies the patch, and builds `bin/multica-avatar`. Go's toolchain auto-download
selects the upstream-required Go version. The normal installed `multica` binary
is not overwritten, and a failed build/check leaves an existing compatibility
binary in place. License/notice files are placed beside the built binary.

Export itself only needs the updated reconciler and the normal CLI:

```bash
./bin/multica-declarative export --output-dir /path/to/snapshot --force
```

For a restore, from an authorized human shell with the intended Multica profile:

```bash
./bin/multica-declarative plan --config /path/to/snapshot/multica.yaml \
  --multica-bin "$PWD/bin/multica-avatar"

# Review the entire plan before applying it; this can also change autopilots.
./bin/multica-declarative apply --config /path/to/snapshot/multica.yaml \
  --multica-bin "$PWD/bin/multica-avatar"
```

**Building the compatibility CLI does not select it automatically.** Keep the
`--multica-bin` argument in the actual apply command or deployment script. When
running from another directory, use the absolute path printed by `make avatar-cli`.
Use the root manifest for the entire workspace set, or a child's manifest to
select only that workspace. Explicit workspace ID bindings and `secrets: omit`
inheritance are unchanged. Add `--profile <name>` when selecting a named profile.

The CLI inherits exactly the same environment/profile and authorization checks
as before. A task-scoped credential error is separate from this avatar bug; this
patch does not circumvent it.

The normal CLI remains sufficient for export, plan, or applying a snapshot whose
emoji avatars already match. Before a create, change or clear, apply checks
`agent update --help` for the setter. Workspace-set apply performs this check for
**all selected workspaces before any write**, including skill/project/autopilot
changes in earlier workspaces. Ordinary plan intentionally does not require the
setter, so stock CLI can still report drift. A future official CLI implementing
the same flag can be selected instead; remove the compatibility patch after
verifying it with the tests below. No version string is used as a proxy for the
actual setter capability.

## Recovering from `agent update --avatar-url is missing`

Update this repository, rebuild both binaries, and run plan/apply with the
explicit compatibility binary as shown above. Existing snapshots containing
`avatarUrl` do not need to be rewritten or exported again. Do not remove the
field just to suppress the error unless leaving that avatar unmanaged is intended.

Older workspace-set apply could discover this error after changing an earlier
workspace. The message `earlier operations may already have been applied` is a
warning, not proof that a particular resource changed. Review a fresh plan before
retrying. The new preflight detects a missing setter before starting any workspace
writes, but apply is still not a transaction: concurrent changes or API failures
after preflight can leave partial changes. Apply re-reads current state on retry.

## Existing snapshots

Previous exports that warned `unsupported protocol scheme "emoji"` omitted that
avatar. Update the reconciler used by your export script and run export again to
capture the missing values. They cannot be recovered from a snapshot that never
contained them. Alternatively, set a known reference explicitly in `agent.yaml`.

Repeated apply does not reassign an unchanged avatar. Applying a file avatar over
an emoji also avoids attempting to download `emoji:`. Archived agents retain the
existing restore/update/rearchive behavior.

## Tests

```bash
make check
MULTICA_BIN=/path/to/official/multica make integration
MULTICA_BIN=/path/to/official/multica \
  MULTICA_EMOJI_BIN="$PWD/bin/multica-avatar" make integration-emoji
```

The official CLI version and release checksum are pinned in
`integration/multica.lock.json`. Tests execute both the stock v0.6.2 CLI and the
real patched binary against a synthetic loopback API, never a live workspace.
Coverage includes exact Unicode export, forced refresh, unchanged plans/applies,
create/restore/replace/clear, legacy omission, archived agents, image-file
compatibility, CLI capability preflight, and rejecting an update response that
did not preserve the requested reference. Workspace-set unit regressions verify
that an unsupported avatar in a later workspace leaves earlier workspaces
untouched, while plan and applies without avatar writes still work.
