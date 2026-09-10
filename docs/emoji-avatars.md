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

The official **Multica 0.4.42** CLI can read emoji references, but its `agent avatar`
command only uploads image files; `agent update` has no avatar flag. The server
already accepts `avatar_url` on the same update endpoint that the CLI uses.

This repository includes an **opt-in, narrowly scoped source patch** adding
`agent update --avatar-url`. It does not add an HTTP fallback to the reconciler,
change auth, read tokens, remove task-context markers, or update the server or
daemon. It is a patched upstream CLI build, not an official release, and reports
version `0.4.42+avatar.1`.

The build is pinned to upstream commit
`76f59f5f1cd9b6e779d0d34c603407d5d4001bf7` (v0.4.42). The complete patch is
[`compat/multica-avatar.patch`](../compat/multica-avatar.patch).
It adds only a flag and a payload field to the existing update command.

## Build and use

From this repository, on Linux or macOS with Git, Bash, Make and Go installed:

```bash
make build
make avatar-cli
```

The second command downloads the pinned upstream sources and Go dependencies,
applies the patch, and builds `bin/multica-avatar`. Go's toolchain auto-download
selects the upstream-required Go version (1.26.6 for this pin). The normal
installed `multica` binary is not overwritten. License/notice files are placed
beside the built binary.

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

The CLI inherits exactly the same environment/profile and authorization checks
as before. A task-scoped credential error is separate from this avatar bug; this
patch does not circumvent it.

The normal CLI remains sufficient for export, plan, or applying a snapshot whose
emoji avatars already match. Before a create, change or clear, apply checks
`agent update --help` for the setter. An unsupported CLI fails **before any write**,
including skill/project/autopilot changes. A future official CLI implementing
the same flag can be selected instead; remove the compatibility patch after
verifying it with the tests below. No version string is used as a proxy for the
actual setter capability.

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

Tests execute both the stock CLI and the real patched binary against a synthetic
loopback API, never a live workspace. Coverage includes exact Unicode export,
forced refresh, unchanged plans/applies, create/restore/replace/clear, legacy
omission, archived agents, image-file compatibility, CLI capability preflight,
and rejecting an update response that did not preserve the requested reference.
