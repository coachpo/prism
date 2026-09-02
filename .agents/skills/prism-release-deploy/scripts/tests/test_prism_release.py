from __future__ import annotations

import importlib.util
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
SCRIPT = SCRIPTS / "prism_release.py"
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

    def test_recovery_allows_main_to_advance_beyond_release_tag(self) -> None:
        release_sha = "a" * 40
        main_sha = "b" * 40

        def command(argv, *, cwd=None):
            values = tuple(argv)
            responses = {
                ("git", "rev-parse", "HEAD"): main_sha,
                ("git", "branch", "--show-current"): "main",
                ("git", "status", "--porcelain"): "",
                ("git", "rev-parse", "origin/main"): main_sha,
                ("git", "ls-remote", "origin", "refs/heads/main"):
                    f"{main_sha}\trefs/heads/main",
                ("git", "rev-list", "-n1", "v1.1.3"): release_sha,
                ("git", "ls-remote", "--tags", "origin", "refs/tags/v1.1.3"):
                    f"{release_sha}\trefs/tags/v1.1.3",
                ("git", "merge-base", "--is-ancestor", release_sha, main_sha): "",
            }
            return responses[values]

        with (
            patch.object(MODULE, "run", side_effect=command),
            patch.object(
                MODULE,
                "version_surfaces",
                return_value={
                    "root": "1.1.3",
                    "backend": "1.1.3",
                    "frontend": "1.1.3",
                    "frontend_package": "1.1.3",
                },
            ),
        ):
            plan = MODULE.recovery_plan(Path("/fixture"), "1.1.3")
        self.assertEqual(plan["head"], main_sha)
        self.assertEqual(plan["release_sha"], release_sha)

    def test_recover_writes_manifest_without_running_release_helper(self) -> None:
        plan = {
            "action": "recover",
            "current_version": "1.1.3",
            "version": "1.1.3",
            "tag": "v1.1.3",
            "repo_root": "/fixture",
            "head": "b" * 40,
            "release_sha": "a" * 40,
            "branch": "main",
            "dirty": False,
        }
        manifest = {
            "schema_version": 1,
            "status": "published",
            "tag": "v1.1.3",
            "version": "1.1.3",
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "release.json"
            with (
                patch.object(MODULE, "recovery_plan", return_value=plan),
                patch.object(MODULE, "finalize_manifest", return_value=manifest),
                patch.object(MODULE, "release_plan") as release_plan,
                redirect_stdout(io.StringIO()) as output,
            ):
                result = MODULE.main(
                    [
                        "recover",
                        "--repo-root",
                        "/fixture",
                        "--spec",
                        "1.1.3",
                        "--confirm-release",
                        "v1.1.3",
                        "--manifest",
                        str(path),
                    ]
                )
            self.assertEqual(result, 0)
            release_plan.assert_not_called()
            self.assertEqual(json.loads(path.read_text(encoding="utf-8")), manifest)
            self.assertEqual(json.loads(output.getvalue())["status"], "published")


if __name__ == "__main__":
    unittest.main()
