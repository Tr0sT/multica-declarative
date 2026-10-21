# Repository instructions

This repository builds a small, predictable declarative reconciler for Multica.

## Architectural constraints

- Treat Multica as a black box in this repository. Any extension to the fork's CLI
  belongs in the Multica fork and must have its own tests and documented contract.
- Agent and squad avatars are intentionally unmanaged. Omit them from export and
  ignore legacy avatarUrl/avatarFile fields without reading files or mutating avatars.
- Use the `multica` CLI with `--output json` for every resource; never call Multica
  or TeamBridge APIs directly or access Multica's database.
- TeamBridge is an explicitly requested optional fork extension, isolated behind
  `TeamBridgeOperations` and implemented through the fork's `multica teambridge`
  commands. Ordinary exports and declarations without `teambridge/` must continue
  to work with the official CLI and must not invoke extension commands. Preserve
  profile/workspace scope; authentication belongs to the selected Multica CLI.
- Keep desired state in readable files suitable for Git review.
- Preserve Agent Skills as standard `SKILL.md` packages.
- Keep Multica-specific deployment settings separate from portable instructions where practical.
- `export` and `plan` must not mutate Multica.
- Export must validate a complete snapshot before replacing generated files.
- `apply` must be idempotent.
- Do not delete undeclared top-level resources without an explicit ownership/pruning design.
- Never print secret values or pass new secret material through command arguments.
- Keep backend capabilities behind Go interfaces so reconciliation and export remain unit-testable.
- When the official CLI can observe but not mutate a field, preserve and diff it, but fail apply
  explicitly rather than bypassing the CLI.

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
go build ./cmd/multica-declarative
```
