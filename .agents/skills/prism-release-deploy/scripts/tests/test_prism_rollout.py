from __future__ import annotations

import importlib.util
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
SPEC = importlib.util.spec_from_file_location(
    "prism_rollout", SCRIPTS / "prism_rollout.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)


class RolloutTests(unittest.TestCase):
    def test_rollout_token_binds_tag_and_sha(self) -> None:
        self.assertEqual(
            MODULE.rollout_token({"tag": "v1.2.3", "release_sha": "abcdef1234567890"}),
            "v1.2.3@abcdef123456",
        )

    def test_canonical_order_places_a_before_b_and_rejects_b_only(self) -> None:
        self.assertEqual(
            MODULE.canonical_service_order(["prism-b", "prism-a"]),
            ["prism-a", "prism-b"],
        )
        with self.assertRaises(MODULE.RolloutError):
            MODULE.canonical_service_order(["prism-b"])
        with self.assertRaises(MODULE.RolloutError):
            MODULE.canonical_service_order(["prism-a", "prism-a"])

    def test_image_identity_must_match_manifest_and_host(self) -> None:
        manifest = {
            "release_sha": "abc",
            "version": "1.2.3",
            "image": {"manifest_digest": "sha256:123"},
        }
        MODULE.validate_image_identity(
            manifest,
            {
                "manifest_digest": "sha256:123",
                "revision": "abc",
                "version": "1.2.3",
                "os": "linux",
                "architecture": "arm64",
            },
            "arm64",
        )
        with self.assertRaises(MODULE.RolloutError):
            MODULE.validate_image_identity(
                manifest,
                {
                    "manifest_digest": "sha256:bad",
                    "revision": "abc",
                    "version": "1.2.3",
                    "os": "linux",
                    "architecture": "arm64",
                },
                "arm64",
            )

    def test_only_429_and_5xx_retry(self) -> None:
        self.assertTrue(MODULE.should_retry_status(429))
        self.assertTrue(MODULE.should_retry_status(503))
        self.assertFalse(MODULE.should_retry_status(200))
        self.assertFalse(MODULE.should_retry_status(400))

    def test_two_success_with_empty_output_fails_without_retry(self) -> None:
        calls = []

        def fetch():
            calls.append(1)
            return 200, b'{"choices":[{"message":{"content":""}}]}'

        body = MODULE.request_once_with_retry(fetch, sleep=lambda _: None)
        with self.assertRaises(MODULE.RolloutError):
            MODULE.require_chat_nonstream(body)
        self.assertEqual(len(calls), 1)

    def test_transient_failure_retries_once(self) -> None:
        responses = iter([(503, b"temporary"), (200, b"ok")])
        sleeps = []
        self.assertEqual(
            MODULE.request_once_with_retry(
                lambda: next(responses), sleep=sleeps.append
            ),
            b"ok",
        )
        self.assertEqual(sleeps, [10])

    def test_stream_requires_visible_content_and_terminal(self) -> None:
        with self.assertRaises(MODULE.RolloutError):
            MODULE.require_chat_stream(
                b'data: {"choices":[{"delta":{"reasoning_content":"x"}}]}\n\ndata: [DONE]\n'
            )
        MODULE.require_chat_stream(
            b'data: {"choices":[{"delta":{"content":"ok"}}]}\n\ndata: [DONE]\n'
        )

    def test_service_sequence_stops_on_a_failure(self) -> None:
        visited = []

        def handler(service):
            visited.append(service)
            if service == "prism-a":
                raise MODULE.RolloutError("failed")

        with self.assertRaises(MODULE.RolloutError):
            MODULE.run_services(["prism-a", "prism-b"], handler)
        self.assertEqual(visited, ["prism-a"])

    def test_plan_consumes_manifest_without_remote_calls(self) -> None:
        manifest = {
            "schema_version": 1,
            "status": "published",
            "repository": "example/prism",
            "tag": "v1.2.3",
            "version": "1.2.3",
            "release_sha": "abcdef1234567890",
            "image": {
                "repository": "ghcr.io/example/prism",
                "manifest_digest": "sha256:" + "1" * 64,
                "ref": "ghcr.io/example/prism:v1.2.3@sha256:" + "1" * 64,
                "revision": "abcdef1234567890",
                "version": "1.2.3",
            },
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "release.json"
            path.write_text(json.dumps(manifest), encoding="utf-8")
            with redirect_stdout(io.StringIO()) as output:
                result = MODULE.main(["plan", "--manifest", str(path)])
        self.assertEqual(result, 0)
        value = json.loads(output.getvalue())
        self.assertEqual(value["services"], ["prism-a", "prism-b"])
        self.assertEqual(value["confirm_rollout"], "v1.2.3@abcdef123456")

    def test_manifest_binds_repository_tag_and_nonempty_digest(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "release.json"
            value = {
                "schema_version": 1,
                "status": "published",
                "repository": "example/prism",
                "tag": "v1.2.3",
                "version": "1.2.3",
                "release_sha": "abc",
                "image": {
                    "repository": "ghcr.io/example/prism",
                    "manifest_digest": "sha256:" + "1" * 64,
                    "ref": "ghcr.io/other/prism:v9@sha256:" + "1" * 64,
                    "revision": "abc",
                    "version": "1.2.3",
                },
            }
            path.write_text(json.dumps(value), encoding="utf-8")
            with self.assertRaises(MODULE.RolloutError):
                MODULE.load_manifest(path)
            for repository_slug in ("../prism", "./prism", "owner/..", "owner/."):
                value["repository"] = repository_slug
                value["image"]["repository"] = f"ghcr.io/{repository_slug.lower()}"
                value["image"]["ref"] = (
                    f"ghcr.io/{repository_slug.lower()}:v1.2.3@"
                    + value["image"]["manifest_digest"]
                )
                path.write_text(json.dumps(value), encoding="utf-8")
                with self.assertRaises(MODULE.RolloutError):
                    MODULE.load_manifest(path)
            value["repository"] = ""
            value["image"]["repository"] = "ghcr.io/"
            value["image"]["ref"] = (
                "ghcr.io/:v1.2.3@" + value["image"]["manifest_digest"]
            )
            path.write_text(json.dumps(value), encoding="utf-8")
            with self.assertRaises(MODULE.RolloutError):
                MODULE.load_manifest(path)


if __name__ == "__main__":
    unittest.main()
