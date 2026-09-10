#!/usr/bin/env bash
# Optional pinned upstream CLI build with ONLY the missing avatar setter flag.
# Never replaces the installed CLI, server, daemon, configuration or credentials.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
revision=76f59f5f1cd9b6e779d0d34c603407d5d4001bf7 # upstream v0.4.42
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
    -ldflags='-s -w -X main.version=0.4.42+avatar.1 -X main.commit=76f59f5+avatar' \
    -o "$root/bin/multica-avatar" ./cmd/multica
)
cp "$work/LICENSE" "$root/bin/multica-avatar.LICENSE"
cp "$work/NOTICE" "$root/bin/multica-avatar.NOTICE"
printf 'Built %s\n' "$root/bin/multica-avatar"
