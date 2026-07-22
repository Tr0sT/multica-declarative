from __future__ import annotations

import json
import subprocess
from collections.abc import Sequence
from typing import Any, Protocol

from .models import (
    Agent,
    AgentInput,
    InvocationTarget,
    Runtime,
    Skill,
    SkillFile,
    SkillFileInput,
    SkillInput,
    SkillSummary,
)


class MulticaError(RuntimeError):
    pass


class Backend(Protocol):
    def list_skills(self) -> tuple[Skill, ...]: ...

    def get_skill(self, skill_id: str) -> Skill: ...

    def create_skill(self, item: SkillInput) -> Skill: ...

    def update_skill(self, skill_id: str, item: SkillInput) -> Skill: ...

    def upsert_skill_file(self, skill_id: str, item: SkillFileInput) -> SkillFile: ...

    def delete_skill_file(self, skill_id: str, file_id: str) -> None: ...

    def list_agents(self) -> tuple[Agent, ...]: ...

    def get_agent(self, agent_id: str) -> Agent: ...

    def list_agent_skills(self, agent_id: str) -> tuple[SkillSummary, ...]: ...

    def create_agent(self, item: AgentInput) -> Agent: ...

    def update_agent(self, agent_id: str, item: AgentInput) -> Agent: ...

    def set_agent_skills(self, agent_id: str, skill_ids: Sequence[str]) -> None: ...

    def list_runtimes(self) -> tuple[Runtime, ...]: ...


class MulticaCLI:
    def __init__(self, binary: str = "multica") -> None:
        self.binary = binary

    def list_skills(self) -> tuple[Skill, ...]:
        raw = self._run_json("skill", "list", "--output", "json")
        return tuple(_parse_skill(item) for item in _expect_list(raw, "skill list"))

    def get_skill(self, skill_id: str) -> Skill:
        raw = self._run_json("skill", "get", skill_id, "--output", "json")
        return _parse_skill(_expect_mapping(raw, "skill get"))

    def create_skill(self, item: SkillInput) -> Skill:
        raw = self._run_json(
            "skill",
            "create",
            "--name",
            item.name,
            "--description",
            item.description,
            "--content-file",
            str(item.content_file),
            "--output",
            "json",
        )
        return _parse_skill(_expect_mapping(raw, "skill create"))

    def update_skill(self, skill_id: str, item: SkillInput) -> Skill:
        raw = self._run_json(
            "skill",
            "update",
            skill_id,
            "--name",
            item.name,
            "--description",
            item.description,
            "--content-file",
            str(item.content_file),
            "--output",
            "json",
        )
        return _parse_skill(_expect_mapping(raw, "skill update"))

    def upsert_skill_file(self, skill_id: str, item: SkillFileInput) -> SkillFile:
        raw = self._run_json(
            "skill",
            "files",
            "upsert",
            skill_id,
            "--path",
            item.path,
            "--content-file",
            str(item.content_file),
            "--output",
            "json",
        )
        return _parse_skill_file(_expect_mapping(raw, "skill file upsert"))

    def delete_skill_file(self, skill_id: str, file_id: str) -> None:
        self._run("skill", "files", "delete", skill_id, file_id)

    def list_agents(self) -> tuple[Agent, ...]:
        raw = self._run_json("agent", "list", "--output", "json")
        return tuple(_parse_agent(item) for item in _expect_list(raw, "agent list"))

    def get_agent(self, agent_id: str) -> Agent:
        raw = self._run_json("agent", "get", agent_id, "--output", "json")
        return _parse_agent(_expect_mapping(raw, "agent get"))

    def list_agent_skills(self, agent_id: str) -> tuple[SkillSummary, ...]:
        raw = self._run_json("agent", "skills", "list", agent_id, "--output", "json")
        return tuple(
            _parse_skill_summary(item) for item in _expect_list(raw, "agent skills list")
        )

    def create_agent(self, item: AgentInput) -> Agent:
        raw = self._run_json(*self._agent_args(("agent", "create"), item))
        return _parse_agent(_expect_mapping(raw, "agent create"))

    def update_agent(self, agent_id: str, item: AgentInput) -> Agent:
        raw = self._run_json(*self._agent_args(("agent", "update", agent_id), item))
        return _parse_agent(_expect_mapping(raw, "agent update"))

    def set_agent_skills(self, agent_id: str, skill_ids: Sequence[str]) -> None:
        self._run_json(
            "agent",
            "skills",
            "set",
            agent_id,
            "--skill-ids",
            ",".join(skill_ids),
            "--output",
            "json",
        )

    def list_runtimes(self) -> tuple[Runtime, ...]:
        raw = self._run_json("runtime", "list", "--output", "json")
        return tuple(_parse_runtime(item) for item in _expect_list(raw, "runtime list"))

    def _agent_args(self, prefix: Sequence[str], item: AgentInput) -> tuple[str, ...]:
        args = [
            *prefix,
            "--name",
            item.name,
            "--description",
            item.description,
            "--instructions",
            item.instructions,
            "--runtime-id",
            item.runtime_id,
            "--model",
            item.model,
            "--thinking-level",
            item.thinking_level,
            "--custom-args",
            json.dumps(list(item.custom_args), separators=(",", ":")),
            "--max-concurrent-tasks",
            str(item.max_concurrent_tasks),
            "--permission-mode",
            "public_to" if item.permission == "workspace" else "private",
        ]
        if item.permission == "workspace":
            args.append("--public-to-workspace")
        args.extend(("--output", "json"))
        return tuple(args)

    def _run_json(self, *args: str) -> Any:
        completed = self._run(*args)
        try:
            return json.loads(completed.stdout)
        except json.JSONDecodeError as exc:
            raise MulticaError(
                f"{self.binary} returned invalid JSON for {' '.join(args)}: {exc}\n"
                f"output: {completed.stdout.strip()}"
            ) from exc

    def _run(self, *args: str) -> subprocess.CompletedProcess[str]:
        try:
            return subprocess.run(
                [self.binary, *args],
                check=True,
                capture_output=True,
                text=True,
                encoding="utf-8",
            )
        except FileNotFoundError as exc:
            raise MulticaError(f"Multica CLI not found: {self.binary}") from exc
        except subprocess.CalledProcessError as exc:
            stderr = (exc.stderr or "").strip()
            message = f"{self.binary} {' '.join(args)} failed with exit code {exc.returncode}"
            if stderr:
                message += f":\n{stderr}"
            raise MulticaError(message) from exc


def _parse_skill(raw: dict[str, Any]) -> Skill:
    return Skill(
        id=_string(raw.get("id")),
        name=_string(raw.get("name")),
        description=_string(raw.get("description")),
        content=_string(raw.get("content")),
        files=tuple(
            _parse_skill_file(item)
            for item in _mapping_list(raw.get("files", []), "skill.files")
        ),
    )


def _parse_skill_file(raw: dict[str, Any]) -> SkillFile:
    return SkillFile(
        id=_string(raw.get("id")),
        path=_string(raw.get("path")),
        content=_string(raw.get("content")),
    )


def _parse_skill_summary(raw: dict[str, Any]) -> SkillSummary:
    return SkillSummary(id=_string(raw.get("id")), name=_string(raw.get("name")))


def _parse_agent(raw: dict[str, Any]) -> Agent:
    return Agent(
        id=_string(raw.get("id")),
        name=_string(raw.get("name")),
        description=_string(raw.get("description")),
        instructions=_string(raw.get("instructions")),
        runtime_id=_string(raw.get("runtime_id")),
        model=_string(raw.get("model")),
        thinking_level=_string(raw.get("thinking_level")),
        custom_args=tuple(_string(item) for item in _expect_list(raw.get("custom_args", []), "custom_args")),
        permission_mode=_string(raw.get("permission_mode")) or "private",
        invocation_targets=tuple(
            InvocationTarget(
                target_type=_string(item.get("target_type")),
                target_id=_nullable_string(item.get("target_id")),
            )
            for item in _mapping_list(raw.get("invocation_targets", []), "invocation_targets")
        ),
        max_concurrent_tasks=_integer(raw.get("max_concurrent_tasks"), default=1),
        skills=tuple(
            _parse_skill_summary(item)
            for item in _mapping_list(raw.get("skills", []), "agent.skills")
        ),
    )


def _parse_runtime(raw: dict[str, Any]) -> Runtime:
    return Runtime(
        id=_string(raw.get("id")),
        name=_string(raw.get("name")),
        custom_name=_string(raw.get("custom_name")),
        provider=_string(raw.get("provider")),
        status=_string(raw.get("status")),
    )


def _expect_mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise MulticaError(f"expected {label} to return an object")
    return value


def _expect_list(value: Any, label: str) -> list[Any]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise MulticaError(f"expected {label} to return a list")
    return value


def _mapping_list(value: Any, label: str) -> list[dict[str, Any]]:
    result = _expect_list(value, label)
    if not all(isinstance(item, dict) for item in result):
        raise MulticaError(f"expected {label} to contain objects")
    return result


def _string(value: Any) -> str:
    return "" if value is None else str(value)


def _nullable_string(value: Any) -> str | None:
    return None if value is None else str(value)


def _integer(value: Any, *, default: int) -> int:
    if value is None:
        return default
    if isinstance(value, bool) or not isinstance(value, int):
        raise MulticaError(f"expected integer, got {type(value).__name__}")
    return value
