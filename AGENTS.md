# Repository instructions

This repository builds a small, predictable declarative reconciler for Multica.

## Architectural constraints

- Treat Multica as a black box.
- Use the official `multica` CLI with `--output json`; never access Multica's database directly.
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
