from __future__ import annotations

import importlib.util
import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / "prism_ops_snapshot.py"
SPEC = importlib.util.spec_from_file_location("prism_ops_snapshot", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class SnapshotTests(unittest.TestCase):
    def test_redacts_secret_bearing_lines(self) -> None:
        value = "healthy\nDATABASE_URL=postgres://secret\nstatus=ok"
        self.assertEqual(
            MODULE.redact_text(value),
            "healthy\n<redacted secret-bearing line>\nstatus=ok",
        )

    def test_parses_common_github_origins(self) -> None:
        self.assertEqual(
            MODULE.parse_origin_slug("git@github.com:coachpo/prism.git"),
            "coachpo/prism",
        )
        self.assertEqual(
            MODULE.parse_origin_slug("https://github.com/coachpo/prism.git"),
            "coachpo/prism",
        )
        self.assertIsNone(MODULE.parse_origin_slug("ssh://example.invalid/prism.git"))
        self.assertEqual(
            MODULE.sanitize_origin("oauth:opaque@github.com:coachpo/prism.git"),
            "github.com:coachpo/prism.git",
        )
        self.assertEqual(
            MODULE.sanitize_origin(
                "https://user:credential@github.com/coachpo/prism.git"
            ),
            "https://github.com/coachpo/prism.git",
        )
        self.assertEqual(
            MODULE.sanitize_origin(
                "https://github.com/coachpo/prism.git?opaque-credential#private"
            ),
            "https://github.com/coachpo/prism.git",
        )

    def test_checks_discovery_and_health(self) -> None:
        snapshot = {
            "repository": {"dirty": False, "versions": {"a": "1", "b": "1"}},
            "remote": {
                "services": {
                    "prism-a": {
                        "app": {
                            "health": "healthy",
                            "restarts": 0,
                            "ports": [{"target": "8080/tcp", "host_port": "8087"}],
                        },
                        "postgres": {"health": "unhealthy", "ports": []},
                        "config": {"sha256": "abc"},
                        "database": {"latest_migration": "000001"},
                    }
                }
            },
        }
        self.assertEqual(
            MODULE.check_snapshot(snapshot, ["prism-a"]),
            ["prism-a: postgres is not healthy"],
        )

    def test_atomic_write_replaces_requested_output(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "nested" / "snapshot.json"
            MODULE.atomic_write(path, "one\n")
            MODULE.atomic_write(path, "two\n")
            self.assertEqual(path.read_text(encoding="utf-8"), "two\n")

    def test_mocked_check_mode_is_read_only_and_reports_snapshot(self) -> None:
        remote = {
            "services": {
                "prism-a": {
                    "app": {
                        "health": "healthy",
                        "restarts": 0,
                        "ports": [{"target": "8080/tcp", "host_port": "8087"}],
                    },
                    "postgres": {"health": "healthy", "ports": []},
                    "config": {"sha256": "abc"},
                    "database": {"latest_migration": "000001"},
                }
            }
        }
        with (
            tempfile.TemporaryDirectory() as temporary,
            patch.object(
                MODULE,
                "git_value",
                side_effect=["origin", "head", "main", "", "origin/main", ""],
            ),
            patch.object(MODULE, "version_surfaces", return_value={"root": "1"}),
            patch.object(MODULE, "github_runs", return_value=[]),
            patch.object(MODULE, "remote_snapshot", return_value=remote) as remote_call,
            redirect_stdout(io.StringIO()) as output,
        ):
            result = MODULE.main(
                [
                    "--repo-root",
                    temporary,
                    "--host",
                    "fixture",
                    "--service",
                    "prism-a",
                    "--check",
                ]
            )
        self.assertEqual(result, 0)
        remote_call.assert_called_once_with("fixture", ["prism-a"])
        self.assertEqual(json.loads(output.getvalue())["remote"], remote)

    def test_capy_check_flags_port_image_and_migration_drift(self) -> None:
        def service(port, image_id, migration, postgres_port=None):
            return {
                "app": {
                    "health": "healthy",
                    "restarts": 0,
                    "image_ref": "ghcr.io/example/prism:v1",
                    "image_id": image_id,
                    "ports": [
                        {"target": "8080/tcp", "host_port": port, "host_ip": "0.0.0.0"}
                    ],
                },
                "postgres": {
                    "health": "healthy",
                    "ports": (
                        [
                            {
                                "target": "5432/tcp",
                                "host_port": postgres_port,
                                "host_ip": "0.0.0.0",
                            }
                        ]
                        if postgres_port
                        else []
                    ),
                },
                "config": {"sha256": "abc"},
                "database": {"latest_migration": migration},
            }

        snapshot = {
            "deployment_host": "capy",
            "repository": {"dirty": False, "versions": {"root": "1"}},
            "remote": {
                "services": {
                    "prism-a": service("8087", "image-a", "000031"),
                    "prism-b": service("8099", "image-b", "000030", "8499"),
                }
            },
        }
        failures = MODULE.check_snapshot(snapshot, ["prism-a", "prism-b"])
        self.assertIn("prism-b: expected public port 8088 is missing", failures)
        self.assertIn("prism-b: expected PostgreSQL port 8432 is missing", failures)
        self.assertIn("capy: prism-a and prism-b image identity differs", failures)
        self.assertIn("capy: prism-a and prism-b migration identity differs", failures)

        snapshot["deployment_host"] = "capy-alias"
        self.assertIn(
            "prism-b: expected public port 8088 is missing",
            MODULE.check_snapshot(snapshot, ["prism-a", "prism-b"]),
        )


if __name__ == "__main__":
    unittest.main()
