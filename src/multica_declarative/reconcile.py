from __future__ import annotations

from collections.abc import Callable, Iterable, Sequence

from .models import (
    Agent,
    AgentInput,
    AgentSpec,
    Change,
    Project,
    Runtime,
    RuntimeSelector,
    Skill,
    SkillFileInput,
    SkillInput,
    SkillSpec,
    SkillSummary,
)
from .multica_cli import Backend

CREATE = "create"
UPDATE = "update"
NOOP = "noop"


class ReconcileError(RuntimeError):
    pass


class Reconciler:
    def __init__(self, backend: Backend) -> None:
        self.backend = backend

    def plan(self, project: Project) -> tuple[Change, ...]:
        remote_skills = self.backend.list_skills()
        remote_agents = self.backend.list_agents()
        runtime_ids = resolve_runtimes(project.runtime_selectors, self.backend.list_runtimes())

        changes: list[Change] = []
        for desired in project.skills:
            matches = _skills_named(remote_skills, desired.name)
            if not matches:
                changes.append(Change(CREATE, "skill", desired.name))
                continue
            if len(matches) > 1:
                raise ReconcileError(f"multiple Multica skills named {desired.name!r}")
            actual = self.backend.get_skill(matches[0].id)
            changes.append(_change("skill", desired.name, _diff_skill(desired, actual)))

        for desired in project.agents:
            matches = _agents_named(remote_agents, desired.name)
            if not matches:
                changes.append(Change(CREATE, "agent", desired.name))
                continue
            if len(matches) > 1:
                raise ReconcileError(
                    f"multiple Multica agents named {desired.name!r}; v1 matches agents by exact name"
                )
            actual = self.backend.get_agent(matches[0].id)
            actual_skills = self.backend.list_agent_skills(actual.id)
            changes.append(
                _change(
                    "agent",
                    desired.name,
                    _diff_agent(desired, runtime_ids[desired.runtime_ref], actual, actual_skills),
                )
            )
        return tuple(changes)

    def apply(self, project: Project, report: Callable[[Change], None]) -> None:
        remote_skills = self.backend.list_skills()
        skill_ids: dict[str, str] = {}

        for desired in project.skills:
            matches = _skills_named(remote_skills, desired.name)
            if not matches:
                actual = self.backend.create_skill(_skill_input(desired))
                actual_files: tuple = ()
                report(Change(CREATE, "skill", desired.name))
            elif len(matches) == 1:
                actual = self.backend.get_skill(matches[0].id)
                actual_files = actual.files
                fields = _diff_skill(desired, actual)
                if any(field != "files" for field in fields):
                    updated = self.backend.update_skill(actual.id, _skill_input(desired))
                    actual = Skill(
                        id=updated.id or actual.id,
                        name=updated.name or actual.name,
                        description=updated.description,
                        content=updated.content,
                        files=actual_files,
                    )
                report(_change("skill", desired.name, fields))
            else:
                raise ReconcileError(f"multiple Multica skills named {desired.name!r}")

            self._sync_skill_files(desired, actual.id, actual_files)
            skill_ids[desired.name] = actual.id

        runtime_ids = resolve_runtimes(project.runtime_selectors, self.backend.list_runtimes())
        remote_agents = self.backend.list_agents()

        for desired in project.agents:
            runtime_id = runtime_ids[desired.runtime_ref]
            input_value = _agent_input(desired, runtime_id)
            matches = _agents_named(remote_agents, desired.name)

            if not matches:
                actual = self.backend.create_agent(input_value)
                actual_skills: tuple[SkillSummary, ...] = ()
                report(Change(CREATE, "agent", desired.name))
            elif len(matches) == 1:
                actual = self.backend.get_agent(matches[0].id)
                actual_skills = self.backend.list_agent_skills(actual.id)
                fields = _diff_agent(desired, runtime_id, actual, actual_skills)
                if any(field != "skills" for field in fields):
                    updated = self.backend.update_agent(actual.id, input_value)
                    actual = Agent(
                        id=updated.id or actual.id,
                        name=updated.name or actual.name,
                        description=updated.description,
                        instructions=updated.instructions,
                        runtime_id=updated.runtime_id,
                        model=updated.model,
                        thinking_level=updated.thinking_level,
                        custom_args=updated.custom_args,
                        permission_mode=updated.permission_mode,
                        invocation_targets=updated.invocation_targets,
                        max_concurrent_tasks=updated.max_concurrent_tasks,
                        skills=actual.skills,
                    )
                report(_change("agent", desired.name, fields))
            else:
                raise ReconcileError(
                    f"multiple Multica agents named {desired.name!r}; v1 matches agents by exact name"
                )

            desired_skill_ids = [skill_ids[name] for name in desired.skills]
            if not matches or _skill_names(actual_skills) != tuple(sorted(desired.skills)):
                self.backend.set_agent_skills(actual.id, desired_skill_ids)

    def _sync_skill_files(
        self, desired: SkillSpec, skill_id: str, actual_files: Sequence
    ) -> None:
        actual_by_path = {item.path: item for item in actual_files}
        desired_paths: set[str] = set()
        for item in desired.files:
            desired_paths.add(item.path)
            existing = actual_by_path.get(item.path)
            if existing is None or existing.content != item.content:
                self.backend.upsert_skill_file(
                    skill_id,
                    SkillFileInput(path=item.path, content_file=item.source_path),
                )

        for item in actual_files:
            if item.path not in desired_paths:
                self.backend.delete_skill_file(skill_id, item.id)


def resolve_runtimes(
    selectors: dict[str, RuntimeSelector], runtimes: Sequence[Runtime]
) -> dict[str, str]:
    resolved: dict[str, str] = {}
    for alias, selector in selectors.items():
        matches = [runtime for runtime in runtimes if _runtime_matches(runtime, selector)]
        if not matches:
            raise ReconcileError(f"runtime selector {alias!r} matched no Multica runtimes")
        if len(matches) > 1:
            raise ReconcileError(
                f"runtime selector {alias!r} matched {len(matches)} Multica runtimes; "
                "make it more specific"
            )
        resolved[alias] = matches[0].id
    return resolved


def format_change(change: Change) -> str:
    prefix = {CREATE: "+", UPDATE: "~", NOOP: "="}[change.action]
    result = f"{prefix} {change.kind:<5} {change.name}"
    if change.fields:
        result += f" [{', '.join(change.fields)}]"
    return result


def _change(kind: str, name: str, fields: Iterable[str]) -> Change:
    field_tuple = tuple(fields)
    return Change(UPDATE if field_tuple else NOOP, kind, name, field_tuple)


def _skill_input(item: SkillSpec) -> SkillInput:
    return SkillInput(
        name=item.name,
        description=item.description,
        content_file=item.content_path,
    )


def _agent_input(item: AgentSpec, runtime_id: str) -> AgentInput:
    return AgentInput(
        name=item.name,
        description=item.description,
        instructions=item.instructions,
        runtime_id=runtime_id,
        model=item.model_id,
        thinking_level=item.thinking_level,
        custom_args=item.custom_args,
        permission=item.permission,
        max_concurrent_tasks=item.max_concurrent_tasks,
    )


def _diff_skill(desired: SkillSpec, actual: Skill) -> tuple[str, ...]:
    fields: list[str] = []
    if desired.description != actual.description:
        fields.append("description")
    if desired.content != actual.content:
        fields.append("content")
    desired_files = {item.path: item.content for item in desired.files}
    actual_files = {item.path: item.content for item in actual.files}
    if desired_files != actual_files:
        fields.append("files")
    return tuple(fields)


def _diff_agent(
    desired: AgentSpec,
    runtime_id: str,
    actual: Agent,
    actual_skills: Sequence[SkillSummary],
) -> tuple[str, ...]:
    fields: list[str] = []
    if desired.description != actual.description:
        fields.append("description")
    if desired.instructions != actual.instructions:
        fields.append("instructions")
    if runtime_id != actual.runtime_id:
        fields.append("runtime")
    if desired.model_id != actual.model:
        fields.append("model")
    if desired.thinking_level != actual.thinking_level:
        fields.append("thinkingLevel")
    if desired.max_concurrent_tasks != actual.max_concurrent_tasks:
        fields.append("maxConcurrentTasks")
    if desired.custom_args != actual.custom_args:
        fields.append("customArgs")
    if not _permission_matches(desired.permission, actual):
        fields.append("permission")
    if tuple(sorted(desired.skills)) != _skill_names(actual_skills):
        fields.append("skills")
    return tuple(fields)


def _permission_matches(permission: str, actual: Agent) -> bool:
    if permission == "private":
        return actual.permission_mode == "private" and not actual.invocation_targets
    return actual.permission_mode == "public_to" and any(
        target.target_type == "workspace" for target in actual.invocation_targets
    )


def _runtime_matches(runtime: Runtime, selector: RuntimeSelector) -> bool:
    return (
        (selector.id is None or runtime.id == selector.id)
        and (selector.name is None or runtime.name == selector.name)
        and (selector.custom_name is None or runtime.custom_name == selector.custom_name)
        and (selector.provider is None or runtime.provider == selector.provider)
    )


def _skills_named(items: Sequence[Skill], name: str) -> tuple[Skill, ...]:
    return tuple(item for item in items if item.name == name)


def _agents_named(items: Sequence[Agent], name: str) -> tuple[Agent, ...]:
    return tuple(item for item in items if item.name == name)


def _skill_names(items: Sequence[SkillSummary]) -> tuple[str, ...]:
    return tuple(sorted(item.name for item in items))
