from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml

from .models import AgentSpec, Project, RuntimeSelector, SkillFileSpec, SkillSpec

API_VERSION = "multica-declarative/v1alpha1"
WORKSPACE_KIND = "Workspace"
AGENT_KIND = "Prompt"


class ConfigurationError(ValueError):
    pass


def load_project(workspace_path: str | Path) -> Project:
    path = Path(workspace_path).expanduser().resolve()
    workspace = _load_yaml_mapping(path)

    if workspace.get("apiVersion") != API_VERSION:
        raise ConfigurationError(
            f"unsupported apiVersion {workspace.get('apiVersion')!r}; expected {API_VERSION!r}"
        )
    if workspace.get("kind") != WORKSPACE_KIND:
        raise ConfigurationError(
            f"unsupported kind {workspace.get('kind')!r}; expected {WORKSPACE_KIND!r}"
        )

    base_dir = path.parent
    runtime_selectors = _load_runtime_selectors(workspace.get("runtimes", {}))
    skills = tuple(
        _load_skill(_resolve(base_dir, item))
        for item in _string_list(workspace.get("skills", []), "skills")
    )
    agents = tuple(
        _load_agent(_resolve(base_dir, item))
        for item in _string_list(workspace.get("agents", []), "agents")
    )

    project = Project(
        workspace_path=path,
        runtime_selectors=runtime_selectors,
        skills=skills,
        agents=agents,
    )
    _validate_project(project)
    return project


def _load_runtime_selectors(raw: Any) -> dict[str, RuntimeSelector]:
    if not isinstance(raw, dict):
        raise ConfigurationError("runtimes must be a mapping")

    result: dict[str, RuntimeSelector] = {}
    for alias, value in raw.items():
        if not isinstance(alias, str) or not alias.strip():
            raise ConfigurationError("runtime aliases must be non-empty strings")
        if not isinstance(value, dict):
            raise ConfigurationError(f"runtime {alias!r} must be a mapping")
        selector = RuntimeSelector(
            id=_optional_string(value.get("id")),
            name=_optional_string(value.get("name")),
            custom_name=_optional_string(value.get("customName")),
            provider=_optional_string(value.get("provider")),
        )
        if not (selector.id or selector.name or selector.custom_name):
            raise ConfigurationError(
                f"runtime {alias!r} must specify id, name, or customName"
            )
        result[alias] = selector
    return result


def _load_skill(directory: Path) -> SkillSpec:
    skill_path = directory / "SKILL.md"
    try:
        content = skill_path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ConfigurationError(f"cannot read {skill_path}: {exc}") from exc
    frontmatter = _parse_frontmatter(content, skill_path)

    name = _required_string(frontmatter.get("name"), f"{skill_path}: frontmatter.name")
    description = _required_string(
        frontmatter.get("description"), f"{skill_path}: frontmatter.description"
    )

    files: list[SkillFileSpec] = []
    for source_path in sorted(directory.rglob("*")):
        if source_path == skill_path or source_path.is_dir():
            continue
        if not source_path.is_file():
            raise ConfigurationError(f"unsupported non-regular skill file: {source_path}")
        try:
            file_content = source_path.read_text(encoding="utf-8")
        except UnicodeDecodeError as exc:
            raise ConfigurationError(f"skill file must be UTF-8: {source_path}") from exc
        except OSError as exc:
            raise ConfigurationError(f"cannot read {source_path}: {exc}") from exc
        if not file_content:
            raise ConfigurationError(f"skill file must not be empty: {source_path}")
        files.append(
            SkillFileSpec(
                path=source_path.relative_to(directory).as_posix(),
                source_path=source_path,
                content=file_content,
            )
        )

    return SkillSpec(
        name=name,
        description=description,
        content=content,
        source_dir=directory,
        content_path=skill_path,
        files=tuple(files),
    )


def _load_agent(path: Path) -> AgentSpec:
    raw = _load_yaml_mapping(path)
    kind = _optional_string(raw.get("kind")) or AGENT_KIND
    if kind != AGENT_KIND:
        raise ConfigurationError(
            f"{path}: unsupported agent kind {kind!r}; expected {AGENT_KIND!r}"
        )

    name = _required_string(raw.get("name"), f"{path}: name")
    description = _optional_string(raw.get("description")) or ""

    instructions = _optional_string(raw.get("instructions")) or ""
    instructions_file = _optional_string(raw.get("instructionsFile"))
    if instructions and instructions_file:
        raise ConfigurationError(f"{path}: instructions and instructionsFile are mutually exclusive")
    if instructions_file:
        source = _resolve(path.parent, instructions_file)
        try:
            instructions = source.read_text(encoding="utf-8")
        except OSError as exc:
            raise ConfigurationError(f"cannot read instructionsFile {source}: {exc}") from exc

    model = raw.get("model", {})
    if model is None:
        model = {}
    if not isinstance(model, dict):
        raise ConfigurationError(f"{path}: model must be a mapping")
    model_id = _optional_string(model.get("id")) or ""

    multica = raw.get("multica", {})
    if not isinstance(multica, dict):
        raise ConfigurationError(f"{path}: multica must be a mapping")
    runtime_ref = _required_string(multica.get("runtime"), f"{path}: multica.runtime")
    thinking_level = _optional_string(multica.get("thinkingLevel")) or ""
    max_concurrent_tasks = multica.get("maxConcurrentTasks", 1)
    if not isinstance(max_concurrent_tasks, int) or isinstance(max_concurrent_tasks, bool):
        raise ConfigurationError(f"{path}: maxConcurrentTasks must be an integer")
    if max_concurrent_tasks < 1:
        raise ConfigurationError(f"{path}: maxConcurrentTasks must be at least 1")
    permission = _optional_string(multica.get("permission")) or "private"
    if permission not in {"private", "workspace"}:
        raise ConfigurationError(f"{path}: permission must be private or workspace")

    custom_args = tuple(_string_list(multica.get("customArgs", []), f"{path}: customArgs"))
    skills = tuple(_string_list(raw.get("skills", []), f"{path}: skills"))

    return AgentSpec(
        name=name,
        description=description,
        instructions=instructions,
        model_id=model_id,
        skills=skills,
        runtime_ref=runtime_ref,
        thinking_level=thinking_level,
        max_concurrent_tasks=max_concurrent_tasks,
        permission=permission,
        custom_args=custom_args,
        source_path=path,
    )


def _validate_project(project: Project) -> None:
    if not project.skills and not project.agents:
        raise ConfigurationError("workspace must declare at least one skill or agent")

    skill_names = [skill.name for skill in project.skills]
    duplicates = _duplicates(skill_names)
    if duplicates:
        raise ConfigurationError(f"duplicate skill names: {', '.join(duplicates)}")

    agent_names = [agent.name for agent in project.agents]
    duplicates = _duplicates(agent_names)
    if duplicates:
        raise ConfigurationError(f"duplicate agent names: {', '.join(duplicates)}")

    declared_skills = set(skill_names)
    for agent in project.agents:
        if agent.runtime_ref not in project.runtime_selectors:
            raise ConfigurationError(
                f"agent {agent.name!r} references unknown runtime {agent.runtime_ref!r}"
            )
        missing = sorted(set(agent.skills) - declared_skills)
        if missing:
            raise ConfigurationError(
                f"agent {agent.name!r} references undeclared skills: {', '.join(missing)}"
            )


def _parse_frontmatter(content: str, path: Path) -> dict[str, Any]:
    lines = content.splitlines()
    if not lines or lines[0].strip() != "---":
        raise ConfigurationError(f"{path}: SKILL.md must start with YAML frontmatter")
    try:
        closing = next(i for i, line in enumerate(lines[1:], start=1) if line.strip() == "---")
    except StopIteration as exc:
        raise ConfigurationError(f"{path}: frontmatter is not closed with ---") from exc
    try:
        raw = yaml.safe_load("\n".join(lines[1:closing])) or {}
    except yaml.YAMLError as exc:
        raise ConfigurationError(f"{path}: invalid frontmatter: {exc}") from exc
    if not isinstance(raw, dict):
        raise ConfigurationError(f"{path}: frontmatter must be a mapping")
    return raw


def _load_yaml_mapping(path: Path) -> dict[str, Any]:
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    except OSError as exc:
        raise ConfigurationError(f"cannot read {path}: {exc}") from exc
    except yaml.YAMLError as exc:
        raise ConfigurationError(f"cannot parse {path}: {exc}") from exc
    if not isinstance(raw, dict):
        raise ConfigurationError(f"{path}: document must be a mapping")
    return raw


def _resolve(base: Path, value: str) -> Path:
    path = Path(value).expanduser()
    return path.resolve() if path.is_absolute() else (base / path).resolve()


def _required_string(value: Any, label: str) -> str:
    result = _optional_string(value)
    if not result:
        raise ConfigurationError(f"{label} is required")
    return result


def _optional_string(value: Any) -> str | None:
    if value is None:
        return None
    if not isinstance(value, str):
        raise ConfigurationError(f"expected string, got {type(value).__name__}")
    result = value.strip()
    return result or None


def _string_list(value: Any, label: str) -> list[str]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ConfigurationError(f"{label} must be a list")
    result: list[str] = []
    for item in value:
        if not isinstance(item, str) or not item.strip():
            raise ConfigurationError(f"{label} must contain non-empty strings")
        result.append(item.strip())
    return result


def _duplicates(items: list[str]) -> list[str]:
    seen: set[str] = set()
    duplicates: set[str] = set()
    for item in items:
        if item in seen:
            duplicates.add(item)
        seen.add(item)
    return sorted(duplicates)
