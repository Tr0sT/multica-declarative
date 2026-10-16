# Avatars are unmanaged

Agent and squad avatars are intentionally outside the declarative snapshot.
This includes emoji references, image URLs, image files and empty/clear values.
Manage avatars in Multica itself. Project icons are a separate managed field.

- Export does not serialize avatarUrl/avatarFile, download images or write avatar files.
- Plan ignores avatar drift.
- Apply does not set, clear, upload or restore avatars, including during agent
  creation, unrelated updates, archived-agent updates and multi-workspace apply.
- Legacy agent multica.avatarUrl/multica.avatarFile and squad avatarUrl keys are
  accepted but ignored. Referenced avatar files are not resolved or read; missing
  files and old simultaneous URL/file values do not block import.

Use the ordinary official multica CLI. This repository does not patch Multica,
provide a custom avatar CLI, or bypass the CLI with HTTP mutations.

## Updating existing installations

Update and rebuild/install multica-declarative. Remove any old --multica-bin
argument that points to the retired avatar compatibility binary. The default
uses multica from PATH; --multica-bin may still select an ordinary official CLI.
No Multica server or daemon changes are needed.

Old snapshots work without manual edits. On the next export --force, avatar keys
and generated avatar files disappear from the refreshed snapshot; live avatars
remain unchanged. Keep a copy of an old snapshot before refreshing if those local
image files are still needed outside this tool.

Review a fresh plan before applying, especially after a previous partial failure.
Apply remains non-transactional across workspaces, but avatars cannot cause CLI
capability errors because they are no longer part of desired state.

## Tests

The ordinary unit and official-CLI integration suites cover omitted emoji/images,
old snapshots with missing avatar files, create/update/no-op reconciliation,
archived agents, squads, and preservation of live avatars during managed updates.
