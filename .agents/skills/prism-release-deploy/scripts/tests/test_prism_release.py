from __future__ import annotations

import importlib.util
import io
import json
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / "prism_release.py"
SPEC = importlib.util.spec_from_file_location("prism_release", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class ReleaseTests(unittest.TestCase):
    def test_parses_release_dry_run(self) -> None:
        output = """Release plan
  Current version : 1.1.2
  Target version  : 1.1.3
  Root tag        : v1.1.3
"""
        self.assertEqual(
            MODULE.parse_release_plan(output),
            {"current_version": "1.1.2", "version": "1.1.3", "tag": "v1.1.3"},
        )

    def test_release_plan_rejects_tag_mismatch(self) -> None:
        with self.assertRaises(MODULE.ReleaseError):
            MODULE.parse_release_plan(
                "Current version: 1.1.2\nTarget version: 1.1.3\nRoot tag: v2.0.0"
            )

    def test_workflow_gate(self) -> None:
        self.assertEqual(MODULE.workflow_gate({}), "pending")
        self.assertEqual(
            MODULE.workflow_gate(
                {
                    "CI": {"status": "completed", "conclusion": "success"},
                    "Docker Image": {"status": "completed", "conclusion": "success"},
                }
            ),
            "success",
        )
        self.assertEqual(
            MODULE.workflow_gate(
                {
                    "CI": {"status": "completed", "conclusion": "failure"},
                    "Docker Image": {"status": "in_progress", "conclusion": None},
                }
            ),
            "failed",
        )
        identities = {
            "CI": {
                "status": "completed",
                "conclusion": "success",
                "event": "push",
                "head_sha": "abc",
                "head_branch": "main",
            },
            "Docker Image": {
                "status": "completed",
                "conclusion": "success",
                "event": "workflow_dispatch",
                "head_sha": "abc",
                "head_branch": "v1.2.3",
            },
        }
        self.assertEqual(
            MODULE.workflow_gate(identities, release_sha="abc", tag="v1.2.3"),
            "failed",
        )

    def test_workflow_selection_skips_wrong_identity_before_correct_run(self) -> None:
        wrong = {
            "name": "CI",
            "id": 1,
            "status": "completed",
            "conclusion": "success",
            "event": "workflow_dispatch",
            "head_sha": "wrong",
            "head_branch": "main",
        }
        correct = {
            "name": "CI",
            "id": 2,
            "status": "completed",
            "conclusion": "success",
            "event": "push",
            "head_sha": "abc",
            "head_branch": "main",
        }
        selected = MODULE.selected_workflows(
            {"workflow_runs": [wrong, correct]}, release_sha="abc", tag="v1.2.3"
        )
        self.assertEqual(selected["CI"]["id"], 2)

    def test_origin_and_arch_normalization(self) -> None:
        self.assertEqual(
            MODULE.parse_origin_slug("git@github.com:coachpo/prism.git"),
            "coachpo/prism",
        )
        self.assertEqual(MODULE.normalized_host_arch("aarch64\n"), "arm64")

    def test_mocked_plan_never_executes_release(self) -> None:
        plan = {
            "action": "plan",
            "current_version": "1.1.2",
            "version": "1.1.3",
            "tag": "v1.1.3",
            "repo_root": "/fixture",
            "head": "abc",
            "branch": "main",
            "dirty": False,
        }
        with (
            patch.object(MODULE, "release_plan", return_value=plan) as release_plan,
            redirect_stdout(io.StringIO()) as output,
        ):
            result = MODULE.main(["plan", "--repo-root", "/fixture", "--spec", "patch"])
        self.assertEqual(result, 0)
        release_plan.assert_called_once()
        self.assertEqual(json.loads(output.getvalue())["tag"], "v1.1.3")


if __name__ == "__main__":
    unittest.main()
