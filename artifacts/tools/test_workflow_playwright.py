#!/usr/bin/env python3

import importlib.util
import contextlib
import io
import json
import os
import pathlib
import subprocess
import tempfile
import types
import unittest
import zipfile
from unittest import mock

MODULE_PATH = pathlib.Path(__file__).with_name("workflow_playwright.py")
SPEC = importlib.util.spec_from_file_location("workflow_playwright", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


def fake_chromium(root: pathlib.Path) -> tuple[pathlib.Path, pathlib.Path]:
    cache = root / "ms-playwright"
    executable = (
        cache
        / "chromium-1234"
        / "chrome-mac-arm64"
        / "Google Chrome for Testing.app"
        / "Contents"
        / "MacOS"
        / "Google Chrome for Testing"
    )
    executable.parent.mkdir(parents=True)
    executable.write_bytes(b"fake local chromium")
    executable.chmod(0o700)
    return cache, executable.resolve()


class WorkflowPlaywrightUnitTest(unittest.TestCase):
    def test_base_url_is_exact_loopback_frontend(self):
        self.assertEqual(MODULE.validate_base_url("http://127.0.0.1:15174"), MODULE.DEFAULT_BASE_URL)
        for invalid in (
            "http://localhost:15174",
            "http://127.0.0.1:18080",
            "https://127.0.0.1:15174",
            "http://127.0.0.1:15174/route/models",
            "http://user@127.0.0.1:15174",
        ):
            with self.subTest(invalid=invalid), self.assertRaises(MODULE.WorkflowError):
                MODULE.validate_base_url(invalid)

    def test_route_paths_cannot_escape_origin(self):
        self.assertEqual(MODULE.validate_route_path("/route/models?search=x"), "/route/models?search=x")
        for invalid in (
            "route/models",
            "https://example.com/",
            "//example.com/api",
            "/route/models#fragment",
            "/route/models\nnext",
        ):
            with self.subTest(invalid=invalid), self.assertRaises(MODULE.WorkflowError):
                MODULE.validate_route_path(invalid)

    def test_redaction_removes_runtime_values(self):
        raw = (
            "query_context=signed.value&x=1\n"
            "Authorization: Bearer abcdefghijklmnop\n"
            "pm-0123456789abcdef0123456789abcdef\n"
            "postgres://prism:unsafe@127.0.0.1:25432/prism\n"
        )
        redacted = MODULE.redact_text(raw)
        MODULE.assert_no_remaining_secret(redacted, "test")
        self.assertNotIn("signed.value", redacted)
        self.assertNotIn("unsafe", redacted)
        self.assertNotIn("0123456789abcdef0123456789abcdef", redacted)

    def test_redaction_removes_explicit_local_values_and_encodings(self):
        value = "local-fixture-value-123"
        raw = " ".join(
            (
                value,
                MODULE.urllib.parse.quote(value, safe=""),
                MODULE.base64.b64encode(value.encode()).decode(),
            )
        )
        redacted = MODULE.redact_text(raw, (value,))
        MODULE.assert_no_remaining_secret(redacted, "test", (value,))
        self.assertNotIn(value, redacted)

    def test_playwright_cli_rejects_private_values_in_arguments(self):
        cli = object.__new__(MODULE.PlaywrightCLI)
        cli.redaction_file = None
        cli.wrapper = pathlib.Path("/not-invoked")
        cli.scratch_dir = pathlib.Path("/not-invoked")
        cli.private_values = types.MethodType(lambda _self: ("local-private-marker",), cli)
        with self.assertRaisesRegex(MODULE.WorkflowError, "must not be passed"):
            MODULE.PlaywrightCLI.run(cli, "fill", "e1", "local-private-marker")

    def test_playwright_cli_injects_private_values_only_by_indexed_environment(self):
        cli = object.__new__(MODULE.PlaywrightCLI)
        cli.redaction_file = None
        cli.wrapper = pathlib.Path("/wrapper")
        cli.scratch_dir = pathlib.Path("/scratch")
        cli.session = "test-session"
        cli.private_values = types.MethodType(lambda _self: ("local-private-marker",), cli)
        observed = {}

        def fake_run(*args, **kwargs):
            observed["command"] = args[0]
            observed["env"] = kwargs["env"]
            return types.SimpleNamespace(returncode=0, stdout="ok")

        with mock.patch.object(MODULE.subprocess, "run", side_effect=fake_run):
            output = MODULE.PlaywrightCLI.run(
                cli,
                "run-code",
                "async (page) => process.env.PRISM_WFL_PRIVATE_ENDPOINT",
                private_value_environment={"PRISM_WFL_PRIVATE_ENDPOINT": 0},
            )
        self.assertEqual(output, "ok")
        self.assertEqual(observed["env"]["PRISM_WFL_PRIVATE_ENDPOINT"], "local-private-marker")
        self.assertNotIn("local-private-marker", observed["command"])
        with self.assertRaisesRegex(MODULE.WorkflowError, "name is invalid"):
            MODULE.PlaywrightCLI.run(
                cli,
                "run-code",
                "async () => true",
                private_value_environment={"UNSAFE": 0},
            )

    def test_playwright_subprocess_environment_drops_ambient_credentials_and_proxies(self):
        environment = MODULE.playwright_subprocess_environment(
            {
                "PATH": "/usr/bin:/bin",
                "HOME": "/local/home",
                "DATABASE_URL": "postgres://retained",
                "HTTP_PROXY": "http://remote.invalid",
                "OPENAI_API_KEY": "ambient-secret",
                "PRISM_CONFIG_PATH": "/retained/config.json",
            }
        )
        self.assertEqual(environment["PATH"], "/usr/bin:/bin")
        self.assertEqual(environment["NO_PROXY"], "127.0.0.1,localhost")
        for forbidden in ("DATABASE_URL", "HTTP_PROXY", "OPENAI_API_KEY", "PRISM_CONFIG_PATH"):
            self.assertNotIn(forbidden, environment)

    def test_json_evidence_rejects_secret_bearing_field_names(self):
        MODULE.assert_safe_json({"case_id": "WFL-001", "entries": []}, "test")
        for field in ("authorization", "api_key", "query_token", "request_headers", "password_hint", "query_context"):
            with self.subTest(field=field), self.assertRaises(MODULE.WorkflowError):
                MODULE.assert_safe_json({field: "redacted"}, "test")
        MODULE.assert_safe_json(
            {"case_id": "WFL-007", "input_tokens": 12, "output_tokens": 4, "total_tokens": 16},
            "test",
        )

    def test_json_evidence_rejects_original_secret_values_before_write(self):
        secrets = (
            "sk-abcdefghijklmnop",
            "postgres://user:password@127.0.0.1/db",
            "Bearer abcdefghijklmnop",
        )
        for value in secrets:
            with self.subTest(value=value), self.assertRaises(MODULE.WorkflowError):
                MODULE.assert_safe_json({"message": value}, "test")
        marker = "local-private-marker-123456"
        with self.assertRaises(MODULE.WorkflowError):
            MODULE.assert_safe_json({"message": marker}, "test", (marker,))
        with self.assertRaises(MODULE.WorkflowError):
            MODULE.assert_safe_json({"message": ("safe", secrets[0])}, "test")
        with self.assertRaises(MODULE.WorkflowError):
            MODULE.assert_safe_json({"Bearer abcdefghijklmnop": "value"}, "test")
        with self.assertRaises(MODULE.WorkflowError):
            MODULE.assert_safe_json({"value": float("nan")}, "test")
        with tempfile.TemporaryDirectory() as temporary:
            target = pathlib.Path(temporary) / "evidence.json"
            with self.assertRaises(MODULE.WorkflowError):
                MODULE.write_json(target, {"message": secrets[0]})
            self.assertFalse(target.exists())

    def test_safe_projection_drops_secret_bearing_api_fields(self):
        projected = MODULE.safe_json_projection(
            {
                "id": 4,
                "api_key": "pm-0123456789abcdef0123456789abcdef",
                "nested": {"headers": {"Authorization": "Bearer unsafe-value"}, "coverage": "FULL"},
            }
        )
        self.assertEqual(projected, {"id": 4, "nested": {"coverage": "FULL"}})
        MODULE.assert_safe_json(projected, "test")

    def test_representative_model_selection_covers_three_shapes(self):
        models = [
            {"id": 1, "model_id": "deepseek/test", "openai_accepted_format": "chat_completions_only", "openai_image_operations": None},
            {"id": 2, "model_id": "codex/text", "openai_accepted_format": "dual_native", "openai_image_operations": None},
            {"id": 3, "model_id": "codex/gpt-image-2", "openai_accepted_format": None, "openai_image_operations": "generations_and_edits"},
        ]
        selected = MODULE.select_representative_models(models)
        self.assertEqual(set(selected), {"chat_only", "dual_native", "image_only"})
        self.assertEqual(selected["image_only"]["id"], 3)

    def test_required_names_match_matrix(self):
        matrix_path = MODULE_PATH.parents[1] / "evidence" / "20260813T204518Z" / "matrix.json"
        matrix = json.loads(matrix_path.read_text(encoding="utf-8"))
        expected = {
            case["id"]: tuple(case["required_evidence"])
            for case in matrix["cases"]
            if case["id"] in MODULE.REQUIRED_EVIDENCE
        }
        self.assertEqual(dict(MODULE.REQUIRED_EVIDENCE), expected)
        self.assertEqual(set(MODULE.HELPER_CASES), {"WFL-%03d" % value for value in range(3, 10)})
        self.assertEqual(MODULE.frozen_workflow_contract(), expected)

    def test_trace_packaging_creates_viewer_archive(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            cli = object.__new__(MODULE.PlaywrightCLI)
            cli.output_dir = root / "cli-output"
            cli.scratch_dir = root
            trace_dir = cli.output_dir / "traces"
            resources = trace_dir / "resources"
            resources.mkdir(parents=True)
            (trace_dir / "trace-1.trace").write_text(
                'query_context=signed.value&next=1 Authorization: Bearer abcdefghijklmnop '
                r'{\"preflight_token\":\"escaped-preflight-value-123456\"}',
                encoding="utf-8",
            )
            (trace_dir / "trace-1.network").write_text(
                '{"query_context":"signed.value","preflight_token":"preflight-value-123456",'
                '"headers":[{"name":"Cookie","value":"prism_session=session-value-123456"}],'
                '"url":"postgres://prism:unsafe@127.0.0.1/db"}',
                encoding="utf-8",
            )
            (resources / "abc").write_text("pm-0123456789abcdef0123456789abcdef", encoding="utf-8")
            (resources / "binary").write_bytes(b"\x89PNG\r\n\x1a\n\x00\xff")
            cli.run = lambda *args, **kwargs: "Trace recording stopped."
            target = root / "trace.zip"
            with mock.patch.object(MODULE, "RUN_PRIVATE", root):
                result = MODULE.PlaywrightCLI.trace_stop(cli, 0, target)
            self.assertEqual(result["path"], "trace.zip")
            self.assertEqual(list((root / "raw-traces").glob("*.zip")), [])
            self.assertFalse((trace_dir / "trace-1.trace").exists())
            self.assertFalse((trace_dir / "trace-1.network").exists())
            self.assertFalse(resources.exists())
            with zipfile.ZipFile(target) as archive:
                self.assertEqual(
                    set(archive.namelist()),
                    {"trace.trace", "trace.network", "resources/abc", "trace-redaction.json"},
                )
                joined = b"\n".join(archive.read(name) for name in archive.namelist())
                self.assertNotIn(b"signed.value", joined)
                self.assertNotIn(b"unsafe", joined)
                self.assertNotIn(b"0123456789abcdef0123456789abcdef", joined)
                self.assertNotIn(b"preflight-value-123456", joined)
                self.assertNotIn(b"escaped-preflight-value-123456", joined)
                self.assertNotIn(b"session-value-123456", joined)
                manifest = json.loads(archive.read("trace-redaction.json"))
                self.assertEqual(manifest["binary_resource_policy"], "omitted")
                self.assertEqual(manifest["omitted_binary_resources"], 1)

    def test_private_scratch_cleanup_removes_close_regenerated_raw_tree(self):
        with tempfile.TemporaryDirectory() as temporary:
            private = pathlib.Path(temporary) / "private"
            scratch = private / "workflow-readonly" / "wfl-001" / "primary-attempt-1" / "playwright"
            resources = scratch / "cli-output" / "traces" / "resources"
            resources.mkdir(parents=True)
            (resources.parent / "trace-1.network").write_text("raw", encoding="utf-8")
            (resources / "image").write_bytes(b"raw-binary")
            with mock.patch.object(MODULE, "RUN_PRIVATE", private):
                MODULE.purge_private_scratch_tree(scratch)
            self.assertFalse(scratch.exists())

    def test_wfl_002_frozen_groups_are_exact_case_sensitive_five_six_one(self):
        groups = MODULE.frozen_wfl002_model_groups()
        self.assertEqual(
            groups["OPENAI_CHAT_ONLY"],
            (
                "DeepSeek-V4-Flash",
                "deepseek-v4-flash",
                "deepseek-v4-pro",
                "deepseek/deepseek-v4-flash-0731",
                "deepseek/deepseek-v4-pro",
            ),
        )
        self.assertEqual(len(groups["OPENAI_DUAL_NATIVE"]), 6)
        self.assertEqual(groups["OPENAI_IMAGE_ONLY"], ("codex/gpt-image-2",))
        self.assertEqual(len({item for values in groups.values() for item in values}), 12)

    def test_wfl_002_inventory_projection_rejects_boolean_numeric_identity(self):
        model = {
            "id": True,
            "model_id": "unsafe",
            "api_family": "openai",
            "openai_accepted_format": "dual_native",
            "openai_image_operations": None,
            "is_enabled": True,
            "access_targets": [],
        }
        with self.assertRaisesRegex(MODULE.ProductAssertionError, "inventory_shape"):
            MODULE.model_inventory_projection(model)

    def test_formal_attempt_lease_holds_lock_and_rechecks_controls(self):
        with tempfile.TemporaryDirectory() as temporary:
            lock_path = pathlib.Path(temporary) / "runner.lock"
            lock_path.touch()
            before = MODULE.FormalAttempt(
                pathlib.Path(temporary) / "attempt",
                "00000000-0000-4000-8000-000000000001",
                "primary",
                1,
                "a" * 40,
                MODULE.CONFIG_FINGERPRINT,
                MODULE.TEMPLATE_FINGERPRINT,
                MODULE.SOURCE_DUMP_FINGERPRINT,
                "b" * 64,
                "c" * 64,
                (("checkpoint", "d" * 64),),
                (("workflow_helper", "e" * 64),),
            )
            after = before._replace(control_sha256=(("checkpoint", "f" * 64),))
            stream = lock_path.open("r+b")
            with (
                mock.patch.object(MODULE, "open_runner_lock", return_value=stream),
                mock.patch.object(MODULE, "validate_formal_attempt", side_effect=[before, after]) as validate,
                self.assertRaisesRegex(MODULE.WorkflowError, "controls changed"),
            ):
                with MODULE.formal_attempt_lease("WFL-001", before.path):
                    pass
            self.assertEqual(validate.call_count, 2)

    def test_readonly_cli_is_absent_because_owner_controls_lifecycle(self):
        parser = MODULE.build_parser()
        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                parser.parse_args(
                    ["readonly", "--case", "WFL-001", "--case-dir", "/tmp/primary-attempt-1"]
                )

    def test_unexpected_cli_failure_is_infrastructure_exit_two(self):
        with (
            mock.patch.object(MODULE, "helper_contract", side_effect=RuntimeError("unsafe detail")),
            contextlib.redirect_stdout(io.StringIO()),
            contextlib.redirect_stderr(io.StringIO()) as errors,
        ):
            status = MODULE.main(
                [
                    "case-contract",
                    "--case-id",
                    "WFL-003",
                ]
            )
        self.assertEqual(status, 2)
        self.assertNotIn("unsafe detail", errors.getvalue())

    def test_early_readonly_assertion_seals_all_frozen_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            run_root = root / "evidence" / MODULE.RUN_ID
            private = run_root / "private"
            case_dir = run_root / "cases" / "WFL-001" / "primary-attempt-1"
            case_dir.mkdir(parents=True)
            (case_dir / "result.json").write_text("{}\n", encoding="utf-8")
            class FakeCLI:
                def __init__(self, *, scratch_dir, **_kwargs):
                    self.scratch_dir = scratch_dir

                def open_blank(self):
                    self.scratch_dir.mkdir(parents=True)

                def trace_start(self):
                    return 1

                def trace_stop(self, _started, target):
                    MODULE.write_failure_trace(
                        target,
                        "WFL-001",
                        "readonly_api_status_unexpected",
                        (),
                    )
                    return {
                        "path": "trace.zip",
                        "bytes": target.stat().st_size,
                        "sha256": MODULE.sha256_file(target),
                    }

                def close(self, *, strict=False):
                    self.assert_strict = strict
                    raw = self.scratch_dir / "cli-output" / "traces" / "resources"
                    raw.mkdir(parents=True)
                    (raw.parent / "trace-1.network").write_text("raw", encoding="utf-8")

            def fail_immediately(_cli, _case_dir):
                raise MODULE.ProductAssertionError("readonly_api_status_unexpected")

            scratch = private / "workflow-cases" / "wfl-001" / "playwright"
            lifecycle = []
            with (
                mock.patch.object(MODULE, "RUN_ROOT", run_root),
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                mock.patch.object(MODULE, "PlaywrightCLI", FakeCLI),
                mock.patch.dict(MODULE.READONLY_HANDLERS, {"WFL-001": fail_immediately}),
            ):
                result = MODULE.run_readonly_session(
                    "WFL-001",
                    case_dir,
                    MODULE.DEFAULT_BASE_URL,
                    "readonly-test-wfl-001",
                    scratch,
                    pathlib.Path("/not-used"),
                    chromium_executable=pathlib.Path("/not-used-chromium"),
                    chromium_bundle_sha256_value="c" * 64,
                    lifecycle_callback=lambda phase, detail: lifecycle.append((phase, detail)),
                )
                self.assertTrue(result["product_failed"])
                self.assertFalse(result["passed"])
                for name in MODULE.REQUIRED_EVIDENCE["WFL-001"]:
                    self.assertTrue((case_dir / name).is_file(), name)
                self.assertFalse(scratch.exists())
                self.assertEqual(
                    [phase for phase, _detail in lifecycle],
                    [
                        "constructed", "opening", "opened", "trace_starting",
                        "trace_active", "trace_stopping", "trace_packaged",
                        "closing", "closed", "purged",
                    ],
                )
                raw = case_dir / "snapshots" / "raw.network"
                raw.parent.mkdir()
                raw.write_text("raw", encoding="utf-8")
                with self.assertRaises(MODULE.WorkflowError):
                    MODULE.validate_readonly_inventory("WFL-001", case_dir)

    def test_readonly_close_failure_keeps_scratch_for_owner_reconcile(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            run_root = root / "evidence" / MODULE.RUN_ID
            private = run_root / "private"
            case_dir = run_root / "cases" / "WFL-001" / "primary-attempt-1"
            case_dir.mkdir(parents=True)
            (case_dir / "result.json").write_text("{}\n", encoding="utf-8")
            scratch = private / "workflow-cases" / "wfl-001" / "playwright"
            lifecycle = []

            class FakeCLI:
                def __init__(self, *, scratch_dir, **_kwargs):
                    self.scratch_dir = scratch_dir
                    scratch_dir.mkdir(parents=True)

                def open_blank(self):
                    return None

                def trace_start(self):
                    return 1

                def trace_stop(self, _started, target):
                    MODULE.write_failure_trace(target, "WFL-001", "test_trace", ())
                    return {"path": "trace.zip", "bytes": target.stat().st_size, "sha256": MODULE.sha256_file(target)}

                def close(self, *, strict=False):
                    self.assert_strict = strict
                    raise MODULE.WorkflowError("strict close failed")

            with (
                mock.patch.object(MODULE, "RUN_ROOT", run_root),
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                mock.patch.object(MODULE, "PlaywrightCLI", FakeCLI),
                mock.patch.dict(
                    MODULE.READONLY_HANDLERS,
                    {"WFL-001": lambda *_args: {"case_id": "WFL-001", "passed": True}},
                ),
                self.assertRaisesRegex(MODULE.WorkflowError, "strict close failed"),
            ):
                MODULE.run_readonly_session(
                    "WFL-001",
                    case_dir,
                    MODULE.DEFAULT_BASE_URL,
                    "readonly-close-failure",
                    scratch,
                    pathlib.Path("/not-used"),
                    chromium_executable=pathlib.Path("/not-used-chromium"),
                    chromium_bundle_sha256_value="c" * 64,
                    lifecycle_callback=lambda phase, detail: lifecycle.append((phase, detail)),
                )
            self.assertTrue(scratch.is_dir())
            self.assertEqual(lifecycle[-1][0], "closing")
            self.assertNotIn("purged", [phase for phase, _detail in lifecycle])

    def test_formal_attempt_accepts_runner_allocation_and_rejects_extra_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary).resolve()
            repo = root / "repo"
            run_root = repo / "artifacts" / "evidence" / MODULE.RUN_ID
            repo.mkdir()
            source_matrix = json.loads(MODULE.MATRIX_PATH.read_text(encoding="utf-8"))
            source_matrix["cases"] = [
                case for case in source_matrix["cases"] if case["id"] == "WFL-001"
            ]
            matrix_input = root / "matrix.json"
            matrix_input.write_text(
                json.dumps(source_matrix, ensure_ascii=False, sort_keys=True) + "\n",
                encoding="utf-8",
            )
            matrix_sha = MODULE.sha256_file(matrix_input)
            phase_patch = mock.patch.object(MODULE.matrix_runner, "PHASES", ("WFL",))
            phase_patch.start()
            self.addCleanup(phase_patch.stop)
            init_args = [
                "init",
                "--run-dir",
                str(run_root),
                "--matrix",
                str(matrix_input),
                "--fingerprint",
                "branch_head=" + "a" * 40,
                "--fingerprint",
                "config=" + MODULE.CONFIG_FINGERPRINT,
                "--fingerprint",
                "database_clone=" + MODULE.TEMPLATE_FINGERPRINT,
                "--fingerprint",
                "source_dump=" + MODULE.SOURCE_DUMP_FINGERPRINT,
            ]
            with mock.patch("builtins.print") as printer:
                init_status = MODULE.matrix_runner.main(init_args)
            self.assertEqual(init_status, 0, printer.call_args_list)
            runner_manifest_path = run_root / "runner-manifest.json"
            runner_manifest = json.loads(runner_manifest_path.read_text(encoding="utf-8"))
            runner_manifest["fingerprints"]["database_clone"] = runner_manifest[
                "fingerprints"
            ].pop("database_template")
            runner_manifest_path.write_text(
                json.dumps(runner_manifest, sort_keys=True) + "\n", encoding="utf-8"
            )
            (run_root / "manifest.json").write_text(
                json.dumps(
                    {
                        "run_id": MODULE.RUN_ID,
                        "repository": {
                            "branch": MODULE.EXPECTED_BRANCH,
                            "worktree": str(repo),
                        },
                        "matrix": {"sha256": matrix_sha},
                    }
                ),
                encoding="utf-8",
            )
            with mock.patch("builtins.print"):
                self.assertEqual(
                    MODULE.matrix_runner.main(
                        [
                            "run-one",
                            "--run-dir",
                            str(run_root),
                            "--case-id",
                            "WFL-001",
                            "--database-clone",
                            "b" * 64,
                        ]
                    ),
                    0,
                )
            attempt = run_root / "cases" / "WFL-001" / "primary-attempt-1"
            patches = (
                mock.patch.object(MODULE, "REPO_ROOT", repo),
                mock.patch.object(MODULE, "EXPECTED_WORKTREE", repo),
                mock.patch.object(MODULE, "RUN_ROOT", run_root),
                mock.patch.object(MODULE, "RUN_PRIVATE", run_root / "private"),
                mock.patch.object(MODULE, "MATRIX_PATH", run_root / "matrix.json"),
                mock.patch.object(MODULE, "MATRIX_SHA256", matrix_sha),
                mock.patch.object(
                    MODULE,
                    "formal_git_facts",
                    return_value=("a" * 40, MODULE.EXPECTED_BRANCH, ""),
                ),
                mock.patch.object(
                    MODULE,
                    "formal_harness_hashes",
                    return_value=(("workflow_helper", "c" * 64),),
                ),
            )
            with patches[0], patches[1], patches[2], patches[3], patches[4], patches[5], patches[6], patches[7]:
                self.assertTrue(attempt.is_dir())
                self.assertFalse(attempt.is_symlink())
                self.assertTrue(MODULE.path_components_safe(attempt, run_root))
                self.assertEqual(
                    json.loads((run_root / "runner-manifest.json").read_text())["fingerprints"],
                    {
                        "branch_head": "a" * 40,
                        "config": MODULE.CONFIG_FINGERPRINT,
                        "database_clone": MODULE.TEMPLATE_FINGERPRINT,
                        "source_dump": MODULE.SOURCE_DUMP_FINGERPRINT,
                    },
                )
                formal = MODULE.validate_formal_attempt(
                    "WFL-001", attempt, require_only_result=True
                )
                self.assertEqual(formal.database_clone, "b" * 64)
                (attempt / "unexpected.txt").write_text("unexpected", encoding="utf-8")
                with self.assertRaisesRegex(MODULE.WorkflowError, "result-only"):
                    MODULE.validate_formal_attempt(
                        "WFL-001", attempt, require_only_result=True
                    )
                (attempt / "unexpected.txt").unlink()
                checkpoint_path = run_root / "checkpoint.json"
                checkpoint = json.loads(checkpoint_path.read_text(encoding="utf-8"))
                checkpoint["cycles"]["primary"]["counts"]["running"] = True
                checkpoint_path.write_text(
                    json.dumps(checkpoint, sort_keys=True) + "\n", encoding="utf-8"
                )
                with self.assertRaisesRegex(MODULE.WorkflowError, "checkpoint"):
                    MODULE.validate_formal_attempt(
                        "WFL-001", attempt, require_only_result=True
                    )

    def test_snapshot_uses_scratch_relative_cli_filename(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            cli = object.__new__(MODULE.PlaywrightCLI)
            cli.scratch_dir = root / "scratch"
            cli.output_dir = cli.scratch_dir / "cli-output"
            cli.scratch_dir.mkdir()
            cli.output_dir.mkdir()

            def run(*args, **kwargs):
                (cli.scratch_dir / "models.yml").write_text('button "Refresh" [ref=e7]\n', encoding="utf-8")
                return "- Page URL: http://127.0.0.1:15174/route/models\n- Page Title: Prism\n"

            cli.run = run
            case_dir = root / "case"
            case_dir.mkdir()
            entry = MODULE.PlaywrightCLI.snapshot(cli, "models", case_dir)
            self.assertEqual(entry["snapshot"], "snapshots/models.snapshot.txt")
            self.assertTrue((case_dir / entry["snapshot"]).is_file())

    def test_config_is_chromium_and_exact_origin_only(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            cli = object.__new__(MODULE.PlaywrightCLI)
            cli.output_dir = root / "out"
            cli.base_url = MODULE.DEFAULT_BASE_URL
            cli.chromium_executable = pathlib.Path("/installed/chromium")
            cli.config_path = root / "playwright-cli.json"
            MODULE.PlaywrightCLI._write_config(cli)
            config = json.loads(cli.config_path.read_text(encoding="utf-8"))
            self.assertEqual(config["browser"]["browserName"], "chromium")
            self.assertTrue(config["browser"]["isolated"])
            self.assertEqual(config["network"]["allowedOrigins"], [MODULE.DEFAULT_BASE_URL])
            self.assertEqual(config["browser"]["launchOptions"]["executablePath"], "/installed/chromium")

    def test_a11y_program_is_valid_javascript(self):
        process = subprocess.run(
            ["node", "--check", "-"],
            input="const audit = %s;\n" % MODULE.A11Y_AUDIT_JS,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
        )
        self.assertEqual(process.returncode, 0, process.stdout)

    def test_snapshot_ref_and_expected_console_filter(self):
        snapshot = 'button "\u5237\u65b0" [ref=e17]\nbutton "Other" [ref=e18]\n'
        self.assertEqual(MODULE.snapshot_ref_for_name(snapshot, "button", ("\u5237\u65b0",)), "e17")
        console = (
            "[ERROR] Failed to load resource: status of 500 @ /api/models\n"
            "[ERROR] Failed to load resource: status of 503 @ /api/auth/status\n"
        )
        self.assertEqual(
            MODULE.unexpected_console_fatal_lines(console, ("status of 500 @ /api/models",)),
            ["[ERROR] Failed to load resource: status of 503 @ /api/auth/status"],
        )

    def test_page_metadata_and_eval_parsers(self):
        output = (
            "### Page\n"
            "- Page URL: http://127.0.0.1:15174/route/models\n"
            "- Page Title: Prism\n"
            "### Snapshot\n"
        )
        self.assertEqual(
            MODULE.parse_page_metadata(output),
            ("http://127.0.0.1:15174/route/models", "Prism"),
        )
        self.assertEqual(MODULE.parse_eval_result('### Result\n{"ok":true}\n### Ran code'), {"ok": True})

    def test_fixture_manifest_requires_exact_case_clone_and_loopback(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            private = root / "private"
            private.mkdir()
            manifest = private / "fixture.json"
            manifest.write_text(
                json.dumps(
                    {
                        "schema_version": 1,
                        "run_id": MODULE.RUN_ID,
                        "case_id": "WFL-003",
                        "fixture_scope": "case",
                        "disposable": True,
                        "database_clone": "prism_matrix_20260813t204518z_case_wfl_003",
                        "database_clone_identity": "a" * 64,
                        "frontend_origin": "http://127.0.0.1:15174",
                        "backend_origin": "http://127.0.0.1:18103",
                        "mock_origins": ["http://127.0.0.1:18081"],
                    }
                ),
                encoding="utf-8",
            )
            os.chmod(manifest, 0o600)
            with mock.patch.object(MODULE, "RUN_PRIVATE", private):
                binding = MODULE.load_fixture_manifest(manifest, "WFL-003", MODULE.DEFAULT_BASE_URL)
                self.assertEqual(binding["database_clone"], "prism_matrix_20260813t204518z_case_wfl_003")
                unsafe = json.loads(manifest.read_text())
                unsafe["database_clone"] = "prism_matrix_runtime"
                manifest.write_text(json.dumps(unsafe), encoding="utf-8")
                with self.assertRaises(MODULE.WorkflowError):
                    MODULE.load_fixture_manifest(manifest, "WFL-003", MODULE.DEFAULT_BASE_URL)

    def test_trace_zip_sanitizer_omits_binary_and_redacts_private_values(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            private = root / "private"
            private.mkdir()
            source = private / "raw.zip"
            with zipfile.ZipFile(source, "w") as archive:
                archive.writestr(
                    "trace.trace",
                    r'{\"password\":\"local-secret-123\",\"preflight_token\":\"opaque-preflight-123456\"}',
                )
                archive.writestr(
                    "trace.network",
                    'Authorization: Bearer abcdefghijklmnop '
                    '{"name":"Set-Cookie","value":"prism_session=opaque-session-123456"}',
                )
                archive.writestr("resources/image", b"\x89PNG\r\n\x1a\n\x00\xff")
            os.chmod(source, 0o600)
            target = root / "trace.zip"
            with mock.patch.object(MODULE, "RUN_PRIVATE", private):
                result = MODULE.sanitize_trace_zip(source, target, ("local-secret-123",))
            self.assertTrue(result["redaction_manifest"])
            with zipfile.ZipFile(target) as archive:
                self.assertNotIn("resources/image", archive.namelist())
                joined = b"\n".join(archive.read(name) for name in archive.namelist())
                self.assertNotIn(b"local-secret-123", joined)
                self.assertNotIn(b"abcdefghijklmnop", joined)
                self.assertNotIn(b"opaque-preflight-123456", joined)
                self.assertNotIn(b"opaque-session-123456", joined)

    def test_trace_zip_sanitizer_rejects_sensitive_member_names(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            private = root / "private"
            private.mkdir()
            source = private / "raw.zip"
            with zipfile.ZipFile(source, "w") as archive:
                archive.writestr("trace.trace", "{}")
                archive.writestr("trace.network", "{}")
                archive.writestr("resources/preflight_token=opaque-capability-123456", "safe")
            os.chmod(source, 0o600)
            target = root / "trace.zip"
            with (
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                self.assertRaises(MODULE.WorkflowError),
            ):
                MODULE.sanitize_trace_zip(source, target, ())
            self.assertFalse(target.exists())

    def test_trace_zip_sanitizer_removes_rejected_published_target(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            private = root / "private"
            private.mkdir()
            source = private / "raw.zip"
            with zipfile.ZipFile(source, "w") as archive:
                archive.writestr("trace.trace", "{}")
                archive.writestr("trace.network", "{}")
            os.chmod(source, 0o600)
            target = root / "trace.zip"
            with (
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                mock.patch.object(
                    MODULE,
                    "validate_trace_archive",
                    side_effect=MODULE.WorkflowError("rejected_after_publish"),
                ),
                self.assertRaises(MODULE.WorkflowError),
            ):
                MODULE.sanitize_trace_zip(source, target, ())
            self.assertFalse(target.exists())

    def test_resume_recovers_snapshot_and_packaged_trace(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            run_root = root / "evidence" / MODULE.RUN_ID
            private = run_root / "private"
            case_dir = run_root / "cases" / "WFL-003" / "primary-attempt-1"
            private.mkdir(parents=True)
            case_dir.mkdir(parents=True)
            manifest = private / "fixture.json"
            manifest.write_text("{}", encoding="utf-8")
            os.chmod(manifest, 0o600)
            chromium_cache, chromium = fake_chromium(root)
            state = {
                "schema_version": 2,
                "matrix_sha256": MODULE.MATRIX_SHA256,
                "case_id": "WFL-003",
                "case_dir": str(case_dir),
                "scratch_dir": str(private / "scratch"),
                "session": "prism-wfl-helper-wfl-003-test",
                "base_url": MODULE.DEFAULT_BASE_URL,
                "chromium_executable": str(chromium),
                "chromium_bundle_sha256": "c" * 64,
                "fixture": {"path": str(manifest), "sha256": MODULE.sha256_file(manifest), "database_clone": "prism_matrix_20260813t204518z_case_wfl_003"},
                "redaction_file": None,
                "phase": "trace_active",
                "trace_started_ns": 1,
                "trace_history": [{"event": "started", "started_ns": 1, "recorded_at": "2026-08-14T00:00:00Z"}],
                "snapshots": [],
                "completed_checkpoints": ["fixture_verified"],
                "checkpoint_records": [{"name": "fixture_verified", "recorded_at": "2026-08-14T00:00:00Z"}],
                "resume_count": 0,
            }
            MODULE.save_state(case_dir, state)
            snapshots = case_dir / "snapshots"
            snapshots.mkdir()
            (snapshots / "orphan.snapshot.txt").write_text("button Refresh", encoding="utf-8")
            with zipfile.ZipFile(case_dir / "trace.zip", "w") as archive:
                archive.writestr("trace.trace", "{}")
                archive.writestr("trace.network", "{}")
                archive.writestr(
                    "trace-redaction.json",
                    json.dumps(
                        {
                            "schema_version": 1,
                            "sanitizer": "workflow_playwright_text_only_v1",
                            "binary_resource_policy": "omitted",
                        }
                    ),
                )
            args = types.SimpleNamespace(case_dir=case_dir)
            with (
                mock.patch.object(MODULE, "RUN_ROOT", run_root),
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                mock.patch.dict(os.environ, {"PLAYWRIGHT_BROWSERS_PATH": str(chromium_cache)}),
            ):
                output = MODULE.helper_resume(args)
            self.assertEqual(output["phase"], "trace_packaged")
            self.assertEqual(set(output["recovered_artifacts"]), {"snapshots/orphan.snapshot.txt", "trace.zip"})
            self.assertEqual(output["resume_count"], 1)

    def test_cleanup_close_does_not_require_pinned_chromium_bundle_to_remain(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            run_root = root / "evidence" / MODULE.RUN_ID
            private = run_root / "private"
            case_dir = run_root / "cases" / "WFL-003" / "primary-attempt-1"
            scratch = private / "workflow" / "wfl-003" / "playwright"
            scratch.mkdir(parents=True)
            case_dir.mkdir(parents=True)
            manifest = private / "fixture.json"
            manifest.write_text("{}", encoding="utf-8")
            os.chmod(manifest, 0o600)
            missing_chromium = root / "removed-cache" / "Google Chrome for Testing"
            state = {
                "schema_version": 2,
                "matrix_sha256": MODULE.MATRIX_SHA256,
                "case_id": "WFL-003",
                "case_dir": str(case_dir),
                "scratch_dir": str(scratch),
                "session": "prism-wfl-helper-wfl-003-test",
                "base_url": MODULE.DEFAULT_BASE_URL,
                "chromium_executable": str(missing_chromium),
                "chromium_bundle_sha256": "c" * 64,
                "fixture": {
                    "path": str(manifest),
                    "sha256": MODULE.sha256_file(manifest),
                    "database_clone": "prism_matrix_20260813t204518z_case_wfl_003",
                },
                "redaction_file": None,
                "phase": "initialized",
                "trace_started_ns": None,
                "snapshots": [],
                "completed_checkpoints": ["fixture_verified"],
                "checkpoint_records": [
                    {"name": "fixture_verified", "recorded_at": "2026-08-14T00:00:00Z"}
                ],
                "resume_count": 0,
            }
            MODULE.save_state(case_dir, state)
            completed = types.SimpleNamespace(returncode=0, stdout="Session closed")
            with (
                mock.patch.object(MODULE, "RUN_ROOT", run_root),
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                mock.patch.object(MODULE.subprocess, "run", return_value=completed) as run,
            ):
                result = MODULE.close_persisted_session(case_dir, MODULE.DEFAULT_WRAPPER)
            self.assertTrue(result["closed"])
            self.assertFalse(missing_chromium.exists())
            self.assertEqual(run.call_args.args[0][-1], "close")

    def test_helper_close_failure_retains_scratch_for_reconciliation(self):
        with tempfile.TemporaryDirectory() as temporary:
            case_dir = pathlib.Path(temporary) / "primary-attempt-1"
            scratch = pathlib.Path(temporary) / "private" / "playwright"
            case_dir.mkdir()
            scratch.mkdir(parents=True)
            state = {
                "case_id": "WFL-003",
                "trace_started_ns": None,
                "completed_checkpoints": ["fixture_verified"],
            }
            cli = types.SimpleNamespace(
                scratch_dir=scratch,
                close=mock.Mock(side_effect=MODULE.WorkflowError("strict close failed")),
            )
            with (
                mock.patch.object(MODULE, "cli_from_state", return_value=(cli, state)),
                mock.patch.object(MODULE, "purge_private_scratch_tree") as purge,
                self.assertRaisesRegex(MODULE.WorkflowError, "strict close failed"),
            ):
                MODULE.helper_close(
                    types.SimpleNamespace(case_dir=case_dir, wrapper=MODULE.DEFAULT_WRAPPER)
                )
            purge.assert_not_called()
            self.assertTrue(scratch.is_dir())

    def test_case_lock_is_non_reentrant(self):
        with tempfile.TemporaryDirectory() as temporary:
            case_dir = pathlib.Path(temporary)
            with MODULE.case_lock(case_dir):
                with self.assertRaises(MODULE.WorkflowError):
                    with MODULE.case_lock(case_dir):
                        pass

    def test_case_contract_is_single_case_and_has_exact_checkpoints(self):
        output = MODULE.helper_contract(types.SimpleNamespace(case_id="WFL-009"))
        self.assertTrue(output["single_case_only"])
        self.assertEqual(output["required_evidence"], list(MODULE.REQUIRED_EVIDENCE["WFL-009"]))
        self.assertEqual(output["checkpoints"], list(MODULE.CASE_CHECKPOINTS["WFL-009"]))

    def test_sensitive_case_must_seal_then_destroy_private_values(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            run_root = root / "evidence" / MODULE.RUN_ID
            private = run_root / "private"
            case_dir = run_root / "cases" / "WFL-003" / "primary-attempt-1"
            private.mkdir(parents=True)
            case_dir.mkdir(parents=True)
            manifest = private / "fixture.json"
            manifest.write_text("{}", encoding="utf-8")
            values = private / "values.json"
            values.write_text(json.dumps(["local-value-123"]), encoding="utf-8")
            os.chmod(manifest, 0o600)
            os.chmod(values, 0o600)
            chromium_cache, chromium = fake_chromium(root)
            for name in MODULE.REQUIRED_EVIDENCE["WFL-003"]:
                path = case_dir / name
                if path.suffix == ".json":
                    path.write_text(json.dumps({"case_id": "WFL-003", "entries": []}), encoding="utf-8")
            with zipfile.ZipFile(case_dir / "trace.zip", "w") as archive:
                archive.writestr("trace.trace", "{}")
                archive.writestr("trace.network", "{}")
                archive.writestr(
                    "trace-redaction.json",
                    json.dumps(
                        {
                            "schema_version": 1,
                            "sanitizer": "workflow_playwright_text_only_v1",
                            "binary_resource_policy": "omitted",
                        }
                    ),
                )
            state = {
                "schema_version": 2,
                "matrix_sha256": MODULE.MATRIX_SHA256,
                "case_id": "WFL-003",
                "case_dir": str(case_dir),
                "scratch_dir": str(private / "scratch"),
                "session": "prism-wfl-helper-wfl-003-test",
                "base_url": MODULE.DEFAULT_BASE_URL,
                "chromium_executable": str(chromium),
                "chromium_bundle_sha256": "c" * 64,
                "fixture": {
                    "path": str(manifest),
                    "sha256": MODULE.sha256_file(manifest),
                    "database_clone": "prism_matrix_20260813t204518z_case_wfl_003",
                },
                "redaction_file": str(values),
                "phase": "closed",
                "trace_started_ns": None,
                "snapshots": [],
                "completed_checkpoints": list(MODULE.CASE_CHECKPOINTS["WFL-003"]),
                "checkpoint_records": [
                    {"name": name, "recorded_at": "2026-08-14T00:00:00Z"}
                    for name in MODULE.CASE_CHECKPOINTS["WFL-003"]
                ],
                "resume_count": 0,
            }
            MODULE.save_state(case_dir, state)
            args = types.SimpleNamespace(case_dir=case_dir, case_id="WFL-003")
            with (
                mock.patch.object(MODULE, "RUN_ROOT", run_root),
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                mock.patch.dict(os.environ, {"PLAYWRIGHT_BROWSERS_PATH": str(chromium_cache)}),
            ):
                self.assertFalse(MODULE.helper_check(args)["private_cleanup_ok"])
                MODULE.helper_seal_redaction(types.SimpleNamespace(case_dir=case_dir))
                values.unlink()
                checked = MODULE.helper_check(args)
            self.assertTrue(checked["complete"])

    def test_product_failure_envelopes_complete_every_frozen_artifact(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            run_root = root / "evidence" / MODULE.RUN_ID
            private = run_root / "private"
            case_dir = run_root / "cases" / "WFL-005" / "primary-attempt-1"
            private.mkdir(parents=True)
            case_dir.mkdir(parents=True)
            manifest = private / "fixture.json"
            manifest.write_text("{}", encoding="utf-8")
            os.chmod(manifest, 0o600)
            chromium_cache, chromium = fake_chromium(root)
            scratch = private / "workflow" / "wfl-005" / "primary-attempt-1" / "playwright"
            state = {
                "schema_version": 2,
                "matrix_sha256": MODULE.MATRIX_SHA256,
                "case_id": "WFL-005",
                "case_dir": str(case_dir),
                "scratch_dir": str(scratch),
                "session": "prism-wfl-helper-wfl-005-test",
                "base_url": "http://127.0.0.1:15205",
                "chromium_executable": str(chromium),
                "chromium_bundle_sha256": "c" * 64,
                "fixture": {
                    "path": str(manifest),
                    "sha256": MODULE.sha256_file(manifest),
                    "database_clone": "prism_matrix_20260813t204518z_case_wfl_005",
                },
                "redaction_file": None,
                "phase": "closed",
                "outcome": "running",
                "failure_code": None,
                "trace_started_ns": None,
                "snapshots": [],
                "completed_checkpoints": ["fixture_verified"],
                "checkpoint_records": [
                    {"name": "fixture_verified", "recorded_at": "2026-08-14T00:00:00Z"}
                ],
                "resume_count": 0,
            }
            MODULE.save_state(case_dir, state)
            with (
                mock.patch.object(MODULE, "RUN_ROOT", run_root),
                mock.patch.object(MODULE, "RUN_PRIVATE", private),
                mock.patch.dict(os.environ, {"PLAYWRIGHT_BROWSERS_PATH": str(chromium_cache)}),
            ):
                recorded = MODULE.record_product_failure(case_dir, "routing_health_row_missing")
                checked = MODULE.helper_check(
                    types.SimpleNamespace(case_dir=case_dir, case_id="WFL-005")
                )
            self.assertEqual(set(recorded["created"]), set(MODULE.REQUIRED_EVIDENCE["WFL-005"]))
            self.assertTrue(checked["complete"])
            self.assertTrue(checked["product_failed"])
            self.assertEqual(checked["failure_code"], "routing_health_row_missing")


if __name__ == "__main__":
    unittest.main()
