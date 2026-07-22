from __future__ import annotations

import argparse
import sys
from collections.abc import Sequence

from . import __version__
from .config import ConfigurationError, load_project
from .multica_cli import MulticaCLI, MulticaError
from .reconcile import CREATE, NOOP, UPDATE, ReconcileError, Reconciler, format_change


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="multica-declarative",
        description="Manage Multica agents and skills as code.",
    )
    parser.add_argument(
        "--config",
        default="multica.yaml",
        help="path to the workspace manifest (default: multica.yaml)",
    )
    parser.add_argument(
        "--multica-bin",
        default="multica",
        help="path or name of the Multica CLI binary (default: multica)",
    )
    parser.add_argument("--version", action="version", version=__version__)
    parser.add_argument("command", choices=("validate", "plan", "apply"))
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        project = load_project(args.config)
        if args.command == "validate":
            print(
                "Configuration is valid: "
                f"{len(project.skills)} skill(s), "
                f"{len(project.agents)} agent(s), "
                f"{len(project.runtime_selectors)} runtime selector(s)."
            )
            return 0

        reconciler = Reconciler(MulticaCLI(args.multica_bin))
        if args.command == "plan":
            changes = reconciler.plan(project)
            _print_plan(changes)
            return 0

        reconciler.apply(project, lambda change: print(format_change(change)))
        print("Apply complete.")
        return 0
    except (ConfigurationError, MulticaError, ReconcileError) as exc:
        print(f"{args.command} failed: {exc}", file=sys.stderr)
        return 1


def _print_plan(changes: Sequence) -> None:
    counts = {CREATE: 0, UPDATE: 0, NOOP: 0}
    for change in changes:
        print(format_change(change))
        counts[change.action] += 1
    print(
        f"\nPlan: {counts[CREATE]} to create, "
        f"{counts[UPDATE]} to update, {counts[NOOP]} unchanged."
    )


if __name__ == "__main__":
    raise SystemExit(main())
