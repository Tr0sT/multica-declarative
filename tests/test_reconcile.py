from pathlib import Path

from multica_declarative.models import (
    Agent,
    AgentSpec,
    Project,
    Runtime,
    RuntimeSelector,
    Skill,
    SkillFile,
    SkillFileInput,
    SkillInput,
    SkillSpec,
    SkillSummary,
)
from multica_declarative.reconcile import NOOP, UPDATE, Reconciler


class FakeBackend:
    def __init__(self) -> None:
        self.skills = (Skill(id="skill-1", name="unity", description="old"),)
        self.skill = Skill(
            id="skill-1",
            name="unity",
            description="old",
            content="old",
            files=(SkillFile(id="file-1", path="old.md", content="old"),),
        )
        self.agents = (Agent(id="agent-1", name="Unity Developer"),)
        self.agent = Agent(
            id="agent-1",
            name="Unity Developer",
            runtime_id="runtime-1",
            max_concurrent_tasks=1,
            permission_mode="private",
        )
        self.runtimes = (Runtime(id="runtime-1", name="desktop", provider="codex"),)
        self.agent_skills: tuple[SkillSummary, ...] = ()
        self.upserted: list[tuple[str, SkillFileInput]] = []
        self.deleted: list[tuple[str, str]] = []

    def list_skills(self):
        return self.skills

    def get_skill(self, _skill_id):
        return self.skill

    def create_skill(self, _item: SkillInput):
        raise AssertionError("not expected")

    def update_skill(self, _skill_id: str, _item: SkillInput):
        return self.skill

    def upsert_skill_file(self, skill_id: str, item: SkillFileInput):
        self.upserted.append((skill_id, item))
        return SkillFile(id="new", path=item.path, content=item.content_file.read_text())

    def delete_skill_file(self, skill_id: str, file_id: str):
        self.deleted.append((skill_id, file_id))

    def list_agents(self):
        return self.agents

    def get_agent(self, _agent_id):
        return self.agent

    def list_agent_skills(self, _agent_id):
        return self.agent_skills

    def create_agent(self, _item):
        raise AssertionError("not expected")

    def update_agent(self, _agent_id, _item):
        return self.agent

    def set_agent_skills(self, _agent_id, _skill_ids):
        return None

    def list_runtimes(self):
        return self.runtimes


def test_plan_detects_updates(tmp_path: Path) -> None:
    skill_path = tmp_path / "SKILL.md"
    skill_path.write_text("new", encoding="utf-8")
    project = Project(
        workspace_path=tmp_path / "multica.yaml",
        runtime_selectors={"desktop": RuntimeSelector(name="desktop", provider="codex")},
        skills=(
            SkillSpec(
                name="unity",
                description="new",
                content="new",
                source_dir=tmp_path,
                content_path=skill_path,
            ),
        ),
        agents=(
            AgentSpec(
                name="Unity Developer",
                description="",
                instructions="work",
                model_id="model",
                skills=("unity",),
                runtime_ref="desktop",
                thinking_level="",
                max_concurrent_tasks=1,
                permission="private",
                custom_args=(),
                source_path=tmp_path / "agent.yaml",
            ),
        ),
    )

    changes = Reconciler(FakeBackend()).plan(project)

    assert changes[0].action == UPDATE
    assert changes[1].action == UPDATE
    assert "skills" in changes[1].fields


def test_plan_noop_when_equal(tmp_path: Path) -> None:
    backend = FakeBackend()
    backend.skill = Skill(id="skill-1", name="unity", description="old", content="old")
    project = Project(
        workspace_path=tmp_path / "multica.yaml",
        runtime_selectors={},
        skills=(
            SkillSpec(
                name="unity",
                description="old",
                content="old",
                source_dir=tmp_path,
                content_path=tmp_path / "SKILL.md",
            ),
        ),
        agents=(),
    )

    changes = Reconciler(backend).plan(project)

    assert changes[0].action == NOOP
