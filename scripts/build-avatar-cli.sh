#!/usr/bin/env bash
# Optional pinned upstream CLI build with ONLY the missing avatar setter flag.
# Never replaces the installed CLI, server, daemon, configuration or credentials.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version=0.6.2
revision=7ab92d7622ecd81d179a8d83ae2dea74835a5069 # upstream v0.6.2
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$root/bin"
git -C "$work" init --quiet
git -C "$work" fetch --quiet --depth=1 https://github.com/multica-ai/multica.git "$revision"
git -C "$work" checkout --quiet --detach FETCH_HEAD
test "$(git -C "$work" rev-parse HEAD)" = "$revision"
git -C "$work" apply --check "$root/compat/multica-avatar.patch"
git -C "$work" apply "$root/compat/multica-avatar.patch"
(
  cd "$work/server"
  # Go downloads the upstream-required toolchain when necessary. Normal module
  # checksum verification stays enabled; no database or auth changes are made.
  GOTOOLCHAIN=auto CGO_ENABLED=0 go build -mod=readonly -trimpath \
    -ldflags="-s -w -X main.version=${version}+avatar.1 -X main.commit=${revision:0:7}+avatar" \
    -o "$work/multica-avatar" ./cmd/multica
)
# Do not replace a working compatibility binary when building or checking fails.
"$work/multica-avatar" agent update --help | grep -F -- '--avatar-url' >/dev/null
cp "$work/LICENSE" "$root/bin/multica-avatar.LICENSE"
cp "$work/NOTICE" "$root/bin/multica-avatar.NOTICE"
mv "$work/multica-avatar" "$root/bin/multica-avatar"
printf 'Built %s (%s+avatar.1)\n' "$root/bin/multica-avatar" "$version"
printf 'Select it explicitly: multica-declarative --multica-bin "%s" apply --config /path/to/multica.yaml\n' "$root/bin/multica-avatar"
