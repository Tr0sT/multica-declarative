from pathlib import Path

import pytest

from multica_declarative.config import ConfigurationError, load_project


def test_load_project(tmp_path: Path) -> None:
    _write(
        tmp_path / "multica.yaml",
        """\
apiVersion: multica-declarative/v1alpha1
kind: Workspace
skills:
  - skills/unity
agents:
  - agents/unity.yaml
runtimes:
  desktop:
    name: desktop
    provider: codex
""",
    )
    _write(
        tmp_path / "skills/unity/SKILL.md",
        """\
---
name: unity-development
description: Unity conventions
---

# Unity
""",
    )
    _write(tmp_path / "skills/unity/references/testing.md", "# Tests\n")
    _write(tmp_path / "agents/instructions.md", "Do the work.\n")
    _write(
        tmp_path / "agents/unity.yaml",
        """\
kind: Prompt
name: Unity Developer
instructionsFile: instructions.md
skills: [unity-development]
multica:
  runtime: desktop
  maxConcurrentTasks: 1
  permission: private
""",
    )

    project = load_project(tmp_path / "multica.yaml")

    assert project.skills[0].name == "unity-development"
    assert project.skills[0].files[0].path == "references/testing.md"
    assert project.agents[0].instructions == "Do the work.\n"


def test_rejects_unknown_agent_skill(tmp_path: Path) -> None:
    _write(
        tmp_path / "multica.yaml",
        """\
apiVersion: multica-declarative/v1alpha1
kind: Workspace
agents: [agent.yaml]
runtimes:
  desktop:
    name: desktop
""",
    )
    _write(
        tmp_path / "agent.yaml",
        """\
kind: Prompt
name: Agent
skills: [missing]
multica:
  runtime: desktop
""",
    )

    with pytest.raises(ConfigurationError, match="undeclared skills"):
        load_project(tmp_path / "multica.yaml")


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
