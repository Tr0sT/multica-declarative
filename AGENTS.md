# Repository instructions

This repository builds a small, predictable declarative reconciler for Multica.

## Architectural constraints

- Treat Multica as a black box.
- Use the official `multica` CLI with `--output json`; do not access Multica's database directly.
- Keep desired state in readable text files suitable for Git review.
- Preserve Agent Skills directories as standard `SKILL.md` packages.
- Keep Multica-specific deployment settings separate from portable agent instructions where practical.
- `plan` must not mutate Multica.
- `apply` must be idempotent.
- Do not delete undeclared remote resources unless an explicit pruning feature is designed and enabled.
- Never print secret values or pass new secret material through command-line arguments.

## Development

Run before submitting changes:

```bash
python -m pip install -e ".[dev]"
ruff check .
pytest
```
