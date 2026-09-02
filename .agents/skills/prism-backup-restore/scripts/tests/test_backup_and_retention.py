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

COMMON_PATH = Path(__file__).resolve().parents[1] / "prism_backup_common.py"
SPEC = importlib.util.spec_from_file_location("prism_backup_common", COMMON_PATH)
COMMON = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(COMMON)
sys.path.insert(0, str(COMMON_PATH.parent))

BACKUP_SPEC = importlib.util.spec_from_file_location(
    "prism_backup", COMMON_PATH.parent / "prism_backup.py"
)
BACKUP = importlib.util.module_from_spec(BACKUP_SPEC)
assert BACKUP_SPEC and BACKUP_SPEC.loader
BACKUP_SPEC.loader.exec_module(BACKUP)


class CommonTests(unittest.TestCase):
    def test_confirmation_identifiers_are_strict(self) -> None:
        self.assertEqual(COMMON.validate_service("prism-a"), "prism-a")
        with self.assertRaises(COMMON.OpsError):
            COMMON.validate_service("../prism-a")
        self.assertEqual(
            COMMON.validate_database_name("prism_restore_20260901"),
            "prism_restore_20260901",
        )
        with self.assertRaises(COMMON.OpsError):
            COMMON.validate_database_name("prism-restore")

    def test_payload_encoding_round_trips_without_shell_metacharacters(self) -> None:
        encoded = COMMON.encode_payload({"service": "prism-a", "mode": "quiesced"})
        self.assertRegex(encoded, r"^[A-Za-z0-9_=\-]+$")

    def test_manifest_hash_is_actual_file_hash(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "manifest.json"
            path.write_bytes(b'{"status":"verified"}\n')
            self.assertEqual(
                COMMON.manifest_file_sha(path), COMMON.sha256_bytes(path.read_bytes())
            )

    def test_remote_backup_verifies_from_backup_directory(self) -> None:
        # Regression for validating relative SHA256SUMS from the wrong cwd.
        self.assertIn(
            "file_sha(backup_dir / name)",
            Path(__file__).resolve().parents[1].joinpath("prism_backup.py").read_text(),
        )

    def test_prune_program_rejects_symlinks_and_requires_verified_manifest(
        self,
    ) -> None:
        program = (
            Path(__file__)
            .resolve()
            .parents[1]
            .joinpath("prism_prune_backups.py")
            .read_text()
        )
        self.assertIn("child.is_symlink()", program)
        self.assertIn('manifest.get("status") != "verified"', program)
        self.assertIn("shutil.rmtree(target)", program)

    def test_mocked_backup_plan_requires_no_confirmation_or_writes(self) -> None:
        planned = {
            "action": "plan",
            "service": "prism-a",
            "backup_root": "/fixture",
        }
        with (
            patch.object(BACKUP, "ssh_python", return_value=planned) as ssh_call,
            redirect_stdout(io.StringIO()) as output,
        ):
            result = BACKUP.main(["plan", "--host", "fixture", "--service", "prism-a"])
        self.assertEqual(result, 0)
        self.assertEqual(json.loads(output.getvalue()), planned)
        self.assertEqual(ssh_call.call_args.args[0], "fixture")

    def test_backup_execute_requires_exact_confirmation_before_ssh(self) -> None:
        with (
            patch.object(BACKUP, "ssh_python") as ssh_call,
            self.assertRaises(BACKUP.OpsError),
        ):
            BACKUP.main(["execute", "--host", "fixture", "--service", "prism-a"])
        ssh_call.assert_not_called()

    def test_backup_rejects_relative_remote_root_before_ssh(self) -> None:
        with (
            patch.object(BACKUP, "ssh_python") as ssh_call,
            self.assertRaises(BACKUP.OpsError),
        ):
            BACKUP.main(
                [
                    "plan",
                    "--host",
                    "fixture",
                    "--service",
                    "prism-a",
                    "--backup-root",
                    "relative-backups",
                ]
            )
        ssh_call.assert_not_called()


if __name__ == "__main__":
    unittest.main()
