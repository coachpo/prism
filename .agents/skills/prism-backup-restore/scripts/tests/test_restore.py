from __future__ import annotations

import importlib.util
import io
import json
import sys
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
SPEC = importlib.util.spec_from_file_location(
    "prism_restore", SCRIPTS / "prism_restore.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class RestoreTests(unittest.TestCase):
    def test_rewrites_only_database_path(self) -> None:
        original = "postgres://user:pass@example:5432/old_db?sslmode=disable#fragment"
        rewritten = MODULE.rewrite_database_url(original, "prism_restore_20260901")
        self.assertEqual(
            rewritten,
            "postgres://user:pass@example:5432/prism_restore_20260901?sslmode=disable#fragment",
        )

    def test_rejects_relative_or_invalid_urls(self) -> None:
        with self.assertRaises(MODULE.OpsError):
            MODULE.rewrite_database_url("localhost/db", "prism_restore_20260901")
        with self.assertRaises(MODULE.OpsError):
            MODULE.rewrite_database_url("postgres://localhost/db", "bad-name")

    def test_remote_program_never_drops_source_database(self) -> None:
        self.assertNotIn("dropdb", MODULE.REMOTE_RESTORE)
        self.assertNotIn("DROP DATABASE", MODULE.REMOTE_RESTORE)
        self.assertIn("target database already exists", MODULE.REMOTE_RESTORE)

    def test_restore_confirmation_uses_manifest_hash(self) -> None:
        self.assertIn("manifest_sha[:12]", MODULE.REMOTE_RESTORE)
        self.assertIn("confirmation token must equal", MODULE.REMOTE_RESTORE)

    def test_mocked_restore_plan_is_non_mutating(self) -> None:
        planned = {
            "action": "plan",
            "service": "prism-a",
            "target_database": "prism_restore_20260901",
            "confirm_restore": "prism-a:abcdef123456",
        }
        with (
            patch.object(MODULE, "ssh_python", return_value=planned) as ssh_call,
            redirect_stdout(io.StringIO()) as output,
        ):
            result = MODULE.main(
                [
                    "plan",
                    "--host",
                    "fixture",
                    "--service",
                    "prism-a",
                    "--manifest",
                    "/fixture/manifest.json",
                    "--target-database",
                    "prism_restore_20260901",
                ]
            )
        self.assertEqual(result, 0)
        self.assertEqual(json.loads(output.getvalue()), planned)
        self.assertEqual(ssh_call.call_args.args[0], "fixture")

    def test_restore_rejects_relative_manifest_before_ssh(self) -> None:
        with (
            patch.object(MODULE, "ssh_python") as ssh_call,
            self.assertRaises(MODULE.OpsError),
        ):
            MODULE.main(
                [
                    "plan",
                    "--host",
                    "fixture",
                    "--service",
                    "prism-a",
                    "--manifest",
                    "relative/manifest.json",
                ]
            )
        ssh_call.assert_not_called()


if __name__ == "__main__":
    unittest.main()
