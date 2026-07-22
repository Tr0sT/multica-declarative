from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class RuntimeSelector:
    id: str | None = None
    name: str | None = None
    custom_name: str | None = None
    provider: str | None = None


@dataclass(frozen=True)
class SkillFileSpec:
    path: str
    source_path: Path
    content: str


@dataclass(frozen=True)
class SkillSpec:
    name: str
    description: str
    content: str
    source_dir: Path
    content_path: Path
    files: tuple[SkillFileSpec, ...] = ()


@dataclass(frozen=True)
class AgentSpec:
    name: str
    description: str
    instructions: str
    model_id: str
    skills: tuple[str, ...]
    runtime_ref: str
    thinking_level: str
    max_concurrent_tasks: int
    permission: str
    custom_args: tuple[str, ...]
    source_path: Path


@dataclass(frozen=True)
class Project:
    workspace_path: Path
    runtime_selectors: dict[str, RuntimeSelector]
    skills: tuple[SkillSpec, ...]
    agents: tuple[AgentSpec, ...]


@dataclass(frozen=True)
class SkillFile:
    id: str
    path: str
    content: str


@dataclass(frozen=True)
class Skill:
    id: str
    name: str
    description: str
    content: str = ""
    files: tuple[SkillFile, ...] = ()


@dataclass(frozen=True)
class InvocationTarget:
    target_type: str
    target_id: str | None = None


@dataclass(frozen=True)
class SkillSummary:
    id: str
    name: str


@dataclass(frozen=True)
class Agent:
    id: str
    name: str
    description: str = ""
    instructions: str = ""
    runtime_id: str = ""
    model: str = ""
    thinking_level: str = ""
    custom_args: tuple[str, ...] = ()
    permission_mode: str = "private"
    invocation_targets: tuple[InvocationTarget, ...] = ()
    max_concurrent_tasks: int = 1
    skills: tuple[SkillSummary, ...] = ()


@dataclass(frozen=True)
class Runtime:
    id: str
    name: str
    custom_name: str = ""
    provider: str = ""
    status: str = ""


@dataclass(frozen=True)
class SkillInput:
    name: str
    description: str
    content_file: Path


@dataclass(frozen=True)
class SkillFileInput:
    path: str
    content_file: Path


@dataclass(frozen=True)
class AgentInput:
    name: str
    description: str
    instructions: str
    runtime_id: str
    model: str
    thinking_level: str
    custom_args: tuple[str, ...]
    permission: str
    max_concurrent_tasks: int


@dataclass(frozen=True)
class Change:
    action: str
    kind: str
    name: str
    fields: tuple[str, ...] = ()


@dataclass
class ApplyResult:
    changes: list[Change] = field(default_factory=list)


def as_string(value: Any) -> str:
    return "" if value is None else str(value)
