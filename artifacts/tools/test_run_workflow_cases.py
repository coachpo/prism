#!/usr/bin/env python3
"""Pure contract tests for the frozen WFL-001..WFL-010 owner.

These tests must never create a database, bind a listener, launch a process,
open a browser, or mutate the matrix runner.  They exercise only validation,
safe projections, and the static command/fixture contract.
"""

from __future__ import annotations

import ast
import contextlib
import importlib.util
import io
import json
import pathlib
import subprocess
import sys
import tempfile
import types
import unittest
from unittest import mock

MODULE_PATH = pathlib.Path(__file__).with_name("run_workflow_cases.py")
SPEC = importlib.util.spec_from_file_location("run_workflow_cases", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class WorkflowCaseOwnerContractTest(unittest.TestCase):
    def test_case_specs_match_frozen_checkpoint_and_evidence_contract(self):
        self.assertEqual(set(MODULE.CASE_SPECS), set(MODULE.ALLOWED_CASES))
        for case_id, case_spec in MODULE.CASE_SPECS.items():
            with self.subTest(case_id=case_id):
                contract = MODULE.case_contract(case_spec)
                self.assertEqual(
                    contract["checkpoints"],
                    list(MODULE.owner_checkpoints(case_id)),
                )
                self.assertEqual(
                    contract["required_evidence"],
                    list(MODULE.workflow.REQUIRED_EVIDENCE[case_id]),
                )
                self.assertEqual(contract["timeout_seconds"], MODULE.CASE_TIMEOUT_SECONDS[case_id])
                self.assertEqual(
                    list(case_spec.scenario_steps),
                    list(MODULE.owner_checkpoints(case_id))[1:],
                )

    def test_disposable_databases_and_ports_are_unique_and_reserved_safe(self):
        databases = [case_spec.database for case_spec in MODULE.CASE_SPECS.values()]
        self.assertEqual(len(databases), len(set(databases)))
        self.assertTrue(all(name.startswith(MODULE.support.CASE_PREFIX) for name in databases))
        ports = [
            port
            for case_spec in MODULE.CASE_SPECS.values()
            for port in (case_spec.backend_port, case_spec.frontend_port, case_spec.mock_port)
            if port is not None
        ]
        self.assertEqual(len(ports), len(set(ports)))
        self.assertFalse(set(ports) & MODULE.RESERVED_PORTS)
        self.assertEqual(
            {(MODULE.CASE_SPECS[case].backend_port, MODULE.CASE_SPECS[case].frontend_port)
             for case in MODULE.ALLOWED_CASES},
            {(18200 + value, 15200 + value) for value in range(1, 11)},
        )

    def test_automated_owner_covers_all_workflows_with_mutation_runner_subset(self):
        expected = {"WFL-%03d" % value for value in range(1, 11)}
        mutation = {"WFL-%03d" % value for value in range(3, 10)}
        self.assertEqual(set(MODULE.AUTOMATED_CASES), expected)
        self.assertEqual(set(MODULE.READONLY_CASES), {"WFL-001", "WFL-002", "WFL-010"})
        self.assertEqual(set(MODULE.CASE_RUNNERS), mutation)
        self.assertTrue(all(callable(MODULE.CASE_RUNNERS[case]) for case in mutation))

    def test_every_generated_workflow_ui_template_is_valid_javascript(self):
        tree = ast.parse(MODULE_PATH.read_text(encoding="utf-8"))

        def render(node):
            if isinstance(node, ast.Constant) and isinstance(node.value, str):
                return node.value
            if isinstance(node, ast.Name):
                value = getattr(MODULE, node.id, None)
                if isinstance(value, str):
                    return value
            if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
                return render(node.left) + render(node.right)
            if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Mod):
                template = render(node.left)
                protected = template.replace("%%", "\x00")
                protected = protected.replace("%s", json.dumps("matrix-static-value"))
                protected = protected.replace("%d", "1")
                return protected.replace("\x00", "%")
            raise AssertionError("unsupported workflow UI template expression at line %s" % node.lineno)

        templates = []
        function_names = {"run_wfl_%03d" % value for value in range(3, 10)}
        for function in tree.body:
            if not isinstance(function, (ast.FunctionDef, ast.AsyncFunctionDef)) or function.name not in function_names:
                continue
            for node in ast.walk(function):
                if (
                    isinstance(node, ast.Call)
                    and isinstance(node.func, ast.Name)
                    and node.func.id == "ui_body"
                ):
                    templates.append((node.lineno, render(node.args[0])))
        self.assertGreaterEqual(len(templates), 30)
        for line, body in sorted(templates):
            with self.subTest(line=line):
                program = MODULE.javascript_action(MODULE.ui_body(body))
                completed = subprocess.run(
                    ["node", "--check", "-"],
                    input="const action = %s;\n" % program,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    check=False,
                )
                self.assertEqual(completed.returncode, 0, completed.stdout)
                for forbidden in (
                    "new URL(",
                    "process.env",
                    "Buffer.",
                    "import('node:",
                    "setTimeout(",
                ):
                    self.assertNotIn(forbidden, program)

    def test_wfl_006_private_actions_never_navigate_after_binding_install(self):
        source = MODULE_PATH.read_text(encoding="utf-8")
        tree = ast.parse(source)
        function = next(
            node
            for node in tree.body
            if isinstance(node, ast.FunctionDef) and node.name == "run_wfl_006"
        )
        private_calls = [
            node
            for node in ast.walk(function)
            if isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and node.func.attr == "run_code"
            and any(keyword.arg == "private_environment" for keyword in node.keywords)
        ]
        self.assertEqual(len(private_calls), 3)
        for call in private_calls:
            segment = ast.get_source_segment(source, call)
            self.assertIsNotNone(segment)
            self.assertNotIn("page.goto(", segment or "")
            self.assertNotIn(".reload(", segment or "")

    def test_self_test_cannot_start_live_dependencies(self):
        forbidden = AssertionError("live dependency invoked by pure self-test")
        with (
            mock.patch.object(MODULE.support, "run_lane", side_effect=forbidden),
            mock.patch.object(MODULE.subprocess, "Popen", side_effect=forbidden),
            mock.patch.object(MODULE.socket, "socket", side_effect=forbidden),
        ):
            result = MODULE.self_test()
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["case_count"], 10)
        self.assertEqual(result["automated_case_count"], 10)
        self.assertEqual(result["mutation_runner_count"], 7)
        self.assertFalse(result["live_services_started"])
        self.assertFalse(result["runner_state_mutated"])

    def test_executable_and_reused_fixture_helpers_are_pinned(self):
        fingerprints = MODULE.pinned_input_fingerprints()
        self.assertTrue(
            {
                "workflow_owner",
                "workflow_helper",
                "local_support",
                "retention_helper",
                "database_lane",
                "playwright_wrapper",
                "playwright_cli_entry",
                "playwright_cli_lock",
                "playwright_cli_runtime_tree",
                "frontend_runtime_tree",
                "frontend_modules_runtime_tree",
                "backend_runtime_tree",
                "go_runtime",
                "chromium_executable",
                "chromium_bundle",
                "git_head",
            }
            <= set(fingerprints)
        )
        self.assertTrue(all(len(value) == 64 for value in fingerprints.values()))
        wrapper = MODULE.workflow.DEFAULT_WRAPPER
        self.assertTrue(wrapper.is_file())
        self.assertTrue(wrapper.stat().st_mode & 0o100)
        wrapper_text = wrapper.read_text(encoding="utf-8")
        self.assertIn(str(MODULE.workflow.PLAYWRIGHT_CLI_ENTRY), wrapper_text)
        self.assertNotIn("npx --yes", wrapper_text)
        package = json.loads(
            (MODULE.workflow.PLAYWRIGHT_CLI_ENTRY.parent / "package.json").read_text(encoding="utf-8")
        )
        self.assertEqual(package["version"], "0.1.18")

    def test_direct_retention_helper_still_fails_closed_for_wfl_009(self):
        with self.assertRaisesRegex(
            MODULE.retention.HarnessError,
            "retention_case_requires_matrix_owner",
        ):
            MODULE.retention.validate_cli_attempt_contract(
                "WFL-009",
                pathlib.Path("/tmp/not-an-attempt"),
                MODULE.ROOT,
            )

    def test_wfl_009_first_job_guard_is_disposable_and_narrow(self):
        sql = MODULE.wfl_009_cancellation_guard_sql()
        self.assertIn("BEFORE INSERT ON management_jobs", sql)
        self.assertIn("NEW.origin = 'manual'", sql)
        self.assertIn("NEW.resource_key = 'request_logs'", sql)
        self.assertIn("matrix_wfl_009_cancel_guard_state", sql)
        self.assertIn("WHERE singleton = TRUE AND deferred_count = 0", sql)
        self.assertNotIn("NOT EXISTS", sql)
        self.assertIn("interval '720 seconds'", sql)
        self.assertNotIn("18080", sql)
        self.assertNotIn("18081", sql)

    def test_service_environment_drops_ambient_database_and_proxy_overrides(self):
        environment = MODULE.sanitized_service_environment(
            {
                "PATH": "/usr/bin:/bin",
                "HOME": "/local/home",
                "DATABASE_URL": "postgres://retained-database",
                "PRISM_CONFIG_PATH": "/retained/config.json",
                "VITE_API_BASE": "https://remote.invalid",
                "HTTP_PROXY": "http://remote.invalid",
                "PGPASSWORD": "ambient-secret",
            }
        )
        self.assertEqual(environment["PATH"], "/usr/bin:/bin")
        self.assertEqual(environment["NO_PROXY"], "127.0.0.1,localhost")
        for forbidden in (
            "DATABASE_URL",
            "PRISM_CONFIG_PATH",
            "VITE_API_BASE",
            "HTTP_PROXY",
            "PGPASSWORD",
        ):
            self.assertNotIn(forbidden, environment)

    def test_whole_case_timeout_arms_and_disarms_frozen_deadline(self):
        with (
            mock.patch.object(MODULE, "frozen_case_timeout", return_value=600),
            mock.patch.object(MODULE.signal, "getitimer", return_value=(0.0, 0.0)),
            mock.patch.object(MODULE.signal, "setitimer") as setitimer,
            mock.patch.object(MODULE.signal, "signal"),
        ):
            with MODULE.enforce_case_timeout("WFL-009") as deadline:
                deadline.enter_cleanup()
        work_arm = setitimer.call_args_list[0].args
        cleanup_arm = setitimer.call_args_list[1].args
        self.assertEqual(work_arm[0], MODULE.signal.ITIMER_REAL)
        self.assertAlmostEqual(work_arm[1], 600.0 - MODULE.CLEANUP_RESERVE_SECONDS, delta=0.1)
        self.assertEqual(cleanup_arm[0], MODULE.signal.ITIMER_REAL)
        self.assertGreater(cleanup_arm[1], work_arm[1])
        self.assertLessEqual(cleanup_arm[1], 600.0)
        self.assertEqual(setitimer.call_args_list[-1].args, (MODULE.signal.ITIMER_REAL, 0.0))

    def test_deadline_distinguishes_work_and_cleanup_expiry(self):
        with mock.patch.object(MODULE, "frozen_case_timeout", return_value=240):
            deadline = MODULE.CaseDeadline("WFL-001")
        with self.assertRaisesRegex(MODULE.CaseDeadlineExpired, "workflow_case_frozen_timeout"):
            deadline._expired(MODULE.signal.SIGALRM, None)
        deadline.phase = "cleanup"
        with self.assertRaisesRegex(MODULE.CaseDeadlineExpired, "workflow_case_cleanup_timeout"):
            deadline._expired(MODULE.signal.SIGALRM, None)

    def test_cleanup_deadline_reentry_cannot_continue_after_final_expiry(self):
        with mock.patch.object(MODULE, "frozen_case_timeout", return_value=240):
            deadline = MODULE.CaseDeadline("WFL-001")
        deadline.phase = "cleanup"
        deadline.final_deadline = 100.0
        with (
            mock.patch.object(MODULE.time, "monotonic", return_value=100.0),
            mock.patch.object(MODULE.signal, "setitimer") as setitimer,
            self.assertRaisesRegex(MODULE.CaseDeadlineExpired, "workflow_case_cleanup_timeout"),
        ):
            deadline.enter_cleanup()
        setitimer.assert_not_called()

    def test_cleanup_transition_rearms_final_timer_before_phase_flip(self):
        with mock.patch.object(MODULE, "frozen_case_timeout", return_value=240):
            deadline = MODULE.CaseDeadline("WFL-001")
        deadline.phase = "work"
        deadline.final_deadline = 200.0
        phases = []
        with mock.patch.object(
            deadline,
            "_arm_until",
            side_effect=lambda _target: phases.append(deadline.phase),
        ):
            deadline.enter_cleanup()
        self.assertEqual(phases, ["work"])
        self.assertEqual(deadline.phase, "cleanup")

    def test_old_work_alarm_during_cleanup_transition_still_runs_work_timeout_path(self):
        with mock.patch.object(MODULE, "frozen_case_timeout", return_value=240):
            deadline = MODULE.CaseDeadline("WFL-001")
        deadline.phase = "work"
        deadline.final_deadline = 200.0
        with (
            mock.patch.object(MODULE.time, "monotonic", return_value=100.0),
            mock.patch.object(MODULE.signal, "setitimer") as setitimer,
            mock.patch.object(
                deadline,
                "_arm_until",
                side_effect=lambda _target: deadline._expired(MODULE.signal.SIGALRM, None),
            ),
            self.assertRaises(MODULE.CaseDeadlineExpired) as raised,
        ):
            deadline.enter_cleanup()
        self.assertEqual(raised.exception.phase, "work")
        self.assertEqual(deadline.phase, "cleanup")
        setitimer.assert_called_once_with(MODULE.signal.ITIMER_REAL, 100.0)

    def test_cleanup_deadline_escapes_service_exception_fanout(self):
        called = []

        class Item:
            def __init__(self, name, failure=None):
                self.name = name
                self.failure = failure

            def close(self):
                called.append(self.name)
                if self.failure is not None:
                    raise self.failure

        group = MODULE.ServiceGroup(MODULE.CASE_SPECS["WFL-001"], {})
        group.items = [Item("later"), Item("deadline", MODULE.CaseDeadlineExpired("cleanup"))]
        with self.assertRaisesRegex(MODULE.CaseDeadlineExpired, "workflow_case_cleanup_timeout"):
            group.close()
        self.assertEqual(called, ["deadline"])

    def test_work_alarm_rearms_absolute_cleanup_deadline_before_unwind(self):
        with mock.patch.object(MODULE, "frozen_case_timeout", return_value=240):
            deadline = MODULE.CaseDeadline("WFL-001")
        deadline.phase = "work"
        deadline.final_deadline = 200.0
        with (
            mock.patch.object(MODULE.time, "monotonic", return_value=100.0),
            mock.patch.object(MODULE.signal, "setitimer") as setitimer,
            self.assertRaisesRegex(MODULE.CaseDeadlineExpired, "workflow_case_frozen_timeout"),
        ):
            deadline._expired(MODULE.signal.SIGALRM, None)
        self.assertEqual(deadline.phase, "cleanup")
        setitimer.assert_called_once_with(MODULE.signal.ITIMER_REAL, 100.0)

    def test_readonly_browser_receipt_reconcile_is_identity_bound(self):
        with tempfile.TemporaryDirectory() as temporary:
            private = pathlib.Path(temporary) / "private"
            lane = private / "workflow-cases" / "wfl_001"
            attempt = pathlib.Path(temporary) / "primary-attempt-1"
            state = {
                "attempt_dir": str(attempt),
                "chromium_executable": "/tmp/chromium",
                "chromium_bundle_sha256": "a" * 64,
            }
            spec = MODULE.CASE_SPECS["WFL-001"]
            with (
                mock.patch.object(MODULE.workflow, "RUN_PRIVATE", private),
                mock.patch.object(MODULE, "lane_dir", return_value=lane),
                mock.patch.object(MODULE, "save_state"),
            ):
                receipt = MODULE.readonly_browser_plan(spec, state)
                scratch = pathlib.Path(receipt["scratch_dir"])
                scratch.mkdir(parents=True)
                (scratch / "planned-only").write_text("safe", encoding="utf-8")
                MODULE.reconcile_readonly_browser(spec, state)
                self.assertEqual(state["readonly_browser"]["phase"], "purged")
                self.assertFalse(scratch.exists())
                tampered = dict(state["readonly_browser"])
                tampered["session"] = "unowned-session"
                state["readonly_browser"] = tampered
                with self.assertRaisesRegex(MODULE.CaseError, "identity_mismatch"):
                    MODULE.validate_readonly_browser_receipt(spec, state)

    def test_readonly_trace_reconcile_records_abandonment_before_purge(self):
        with tempfile.TemporaryDirectory() as temporary:
            private = pathlib.Path(temporary) / "private"
            lane = private / "workflow-cases" / "wfl_001"
            attempt = pathlib.Path(temporary) / "primary-attempt-1"
            state = {
                "attempt_dir": str(attempt),
                "chromium_executable": "/tmp/chromium",
                "chromium_bundle_sha256": "a" * 64,
            }
            spec = MODULE.CASE_SPECS["WFL-001"]
            with (
                mock.patch.object(MODULE.workflow, "RUN_PRIVATE", private),
                mock.patch.object(MODULE, "lane_dir", return_value=lane),
                mock.patch.object(MODULE, "save_state"),
                mock.patch.object(MODULE.workflow, "close_named_session") as close_session,
            ):
                receipt = MODULE.readonly_browser_plan(spec, state)
                scratch = pathlib.Path(receipt["scratch_dir"])
                scratch.mkdir(parents=True)
                callback = MODULE.readonly_lifecycle_callback(spec, state)
                for phase in (
                    "constructed",
                    "opening",
                    "opened",
                    "trace_starting",
                    "trace_active",
                ):
                    callback(phase, {})
                MODULE.reconcile_readonly_browser(spec, state)
            phases = [item["phase"] for item in state["readonly_browser"]["history"]]
            self.assertEqual(
                phases[-4:],
                ["trace_abandoned", "closing", "closed", "purged"],
            )
            abandoned = state["readonly_browser"]["history"][-4]["detail"]
            self.assertEqual(abandoned["reason"], "trace_not_packaged")
            self.assertTrue(abandoned["trace_started_confirmed"])
            close_session.assert_called_once()
            self.assertFalse(scratch.exists())

    def test_readonly_product_failure_reports_only_verified_owner_checkpoints(self):
        spec = MODULE.CASE_SPECS["WFL-001"]
        attempt = pathlib.Path("/tmp/primary-attempt-1")
        deadline = types.SimpleNamespace(
            timeout_seconds=240,
            cleanup_reserve_seconds=90,
            enter_cleanup=mock.Mock(),
        )
        services = types.SimpleNamespace(start=mock.Mock())
        formal = object()
        state = {
            "attempt_dir": str(attempt),
            "chromium_executable": "/tmp/chromium",
            "chromium_bundle_sha256": "a" * 64,
            "scenario_steps": [],
        }
        with (
            mock.patch.object(MODULE.workflow, "validate_attempt_dir", return_value=attempt),
            mock.patch.object(MODULE, "formal_owner_files", return_value={}),
            mock.patch.object(MODULE, "lane_lock", return_value=contextlib.nullcontext()),
            mock.patch.object(
                MODULE,
                "enforce_case_timeout",
                return_value=contextlib.nullcontext(deadline),
            ),
            mock.patch.object(
                MODULE.workflow,
                "formal_attempt_lease",
                return_value=contextlib.nullcontext(formal),
            ),
            mock.patch.object(MODULE, "adopt_prepared_case", return_value=state),
            mock.patch.object(MODULE, "verify_prepared_execution_inputs"),
            mock.patch.object(MODULE, "verify_execution_inputs"),
            mock.patch.object(MODULE, "ServiceGroup", return_value=services),
            mock.patch.object(MODULE, "prepare_scenario_fixture"),
            mock.patch.object(
                MODULE,
                "readonly_browser_plan",
                return_value={"session": "prism-workflow-wfl-001-primary-attempt-1", "scratch_dir": "/tmp/scratch"},
            ),
            mock.patch.object(MODULE.support, "LocalPostgres", return_value=object()),
            mock.patch.object(
                MODULE.workflow,
                "run_readonly_session",
                return_value={
                    "passed": False,
                    "product_failed": True,
                    "failure_code": "wfl_001_semantic_oracle_failed",
                },
            ),
            mock.patch.object(MODULE, "stop_case_services_and_clone"),
            mock.patch.object(MODULE.workflow, "validate_readonly_inventory"),
            mock.patch.object(MODULE, "update_preparation_outcome") as outcome,
            mock.patch.object(MODULE.workflow, "inventory", return_value=[{"path": "result.json"}]),
            mock.patch.object(MODULE, "save_state"),
        ):
            result = MODULE.execute_readonly_case(spec, attempt)
        self.assertEqual(result["status"], "product_failed")
        self.assertEqual(
            result["verified_checkpoints"],
            ["fixture_verified", "cleanup_verified"],
        )
        self.assertEqual(result["checkpoint_count"], 2)
        self.assertNotIn("navigation_verified", result["verified_checkpoints"])
        self.assertEqual(state["failure_checkpoint"], "readonly_semantic_oracle")
        outcome.assert_called_once_with(spec, formal, "product_failed")

    def test_case_backend_build_requires_clean_tracked_worktree(self):
        dirty = types.SimpleNamespace(returncode=0, stdout=b" M backend/runtime.go\n")
        with (
            mock.patch.object(MODULE.subprocess, "run", return_value=dirty),
            self.assertRaisesRegex(MODULE.CaseError, "workflow_tracked_worktree_not_clean"),
        ):
            MODULE.require_clean_worktree()

    def test_stale_process_cleanup_never_signals_unverifiable_group(self):
        with tempfile.TemporaryDirectory() as temporary:
            state_path = pathlib.Path(temporary) / "backend.json"
            state = {
                "schema_version": 1,
                "name": "backend",
                "phase": "started",
                "pid": 44009,
                "pgid": 44009,
                "command_sha256": "a" * 64,
                "arguments_sha256": "b" * 64,
                "started": "Thu Aug 14 10:00:00 2026",
                "owned_port": 18201,
            }
            state_path.write_text(json.dumps(state), encoding="utf-8")
            state_path.chmod(0o600)
            with (
                mock.patch.object(MODULE, "safe_process_identity", return_value=None),
                mock.patch.object(MODULE, "process_group_exists", return_value=True),
                mock.patch.object(MODULE.os, "killpg") as killpg,
                self.assertRaisesRegex(MODULE.CaseError, "workflow_stale_process_identity_unverifiable"),
            ):
                MODULE.stop_stale_owned_process(state_path, "backend")
            killpg.assert_not_called()
            self.assertTrue(state_path.exists())

    def test_launching_receipt_recovers_child_before_listener_bind(self):
        with tempfile.TemporaryDirectory() as temporary:
            state_path = pathlib.Path(temporary) / "backend.json"
            state = {
                "schema_version": 1,
                "name": "backend",
                "phase": "launching",
                "arguments_sha256": "b" * 64,
                "launch_epoch_before": 1000.0,
                "command_markers": ["/tmp/prism-backend"],
                "owned_port": 18201,
            }
            state_path.write_text(json.dumps(state), encoding="utf-8")
            state_path.chmod(0o600)
            alive = {"value": True}

            def kill_group(pgid, signal_value):
                self.assertEqual(pgid, 45009)
                self.assertEqual(signal_value, MODULE.signal.SIGTERM)
                alive["value"] = False

            with (
                mock.patch.object(
                    MODULE,
                    "launching_process_candidates",
                    return_value=[{"pid": 45009, "pgid": 45009}],
                ),
                mock.patch.object(
                    MODULE,
                    "process_group_exists",
                    side_effect=lambda _pgid: alive["value"],
                ),
                mock.patch.object(MODULE.os, "killpg", side_effect=kill_group) as killpg,
                mock.patch.object(MODULE, "port_available") as port_available,
            ):
                MODULE.stop_stale_owned_process(state_path, "backend")
            killpg.assert_called_once()
            port_available.assert_not_called()
            self.assertFalse(state_path.exists())

    def test_launching_receipt_process_scan_is_marker_time_and_group_bound(self):
        started_epoch = 1_723_630_000.0
        started_text = MODULE.time.strftime(
            "%a %b %d %H:%M:%S %Y", MODULE.time.localtime(started_epoch)
        )
        listing = types.SimpleNamespace(
            returncode=0,
            stdout=(
                "45009 45009 Ss %s /tmp/prism-backend --serve\n"
                "45010 45011 Ss %s /tmp/prism-backend --serve\n"
                "45012 45012 Zs %s /tmp/prism-backend --serve\n"
                "45013 45013 Ss %s /tmp/unrelated --serve\n"
            ) % (started_text, started_text, started_text, started_text),
        )
        record = {
            "launch_epoch_before": started_epoch,
            "command_markers": ["/tmp/prism-backend"],
        }
        with mock.patch.object(MODULE.subprocess, "run", return_value=listing):
            candidates = MODULE.launching_process_candidates(record)
        self.assertEqual([item["pid"] for item in candidates], [45009])

    def test_owned_process_post_popen_deadline_cleans_launching_child(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            state_path = root / "backend.json"
            alive = {"value": True}

            class FakeProcess:
                pid = 46009

                def poll(self):
                    return None if alive["value"] else 0

                def wait(self, timeout):
                    alive["value"] = False
                    return 0

            def kill_group(pgid, signal_value):
                self.assertEqual(pgid, FakeProcess.pid)
                self.assertEqual(signal_value, MODULE.signal.SIGTERM)
                alive["value"] = False

            owned = MODULE.OwnedProcess(
                name="backend",
                arguments=(str(root / "prism-backend"),),
                cwd=root,
                environment={},
                log_path=root / "backend.log",
                state_path=state_path,
                owned_port=18201,
            )
            with (
                mock.patch.object(MODULE.subprocess, "Popen", return_value=FakeProcess()),
                mock.patch.object(
                    MODULE.os,
                    "getpgid",
                    side_effect=MODULE.CaseDeadlineExpired("work"),
                ),
                mock.patch.object(
                    MODULE,
                    "process_group_exists",
                    side_effect=lambda _pgid: alive["value"],
                ),
                mock.patch.object(MODULE.os, "killpg", side_effect=kill_group) as killpg,
                mock.patch.object(MODULE, "port_available", return_value=True),
                self.assertRaisesRegex(
                    MODULE.CaseDeadlineExpired,
                    "workflow_case_frozen_timeout",
                ),
            ):
                owned.start()
            killpg.assert_called_once()
            self.assertFalse(alive["value"])
            self.assertFalse(state_path.exists())

    def test_owned_process_popen_interruption_scans_for_unreturned_child(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            state_path = root / "backend.json"
            alive = {"value": True}

            def kill_group(pgid, signal_value):
                self.assertEqual(pgid, 47009)
                self.assertEqual(signal_value, MODULE.signal.SIGTERM)
                alive["value"] = False

            owned = MODULE.OwnedProcess(
                name="backend",
                arguments=(str(root / "prism-backend"),),
                cwd=root,
                environment={},
                log_path=root / "backend.log",
                state_path=state_path,
                owned_port=18201,
            )
            with (
                mock.patch.object(
                    MODULE.subprocess,
                    "Popen",
                    side_effect=MODULE.CaseDeadlineExpired("work"),
                ),
                mock.patch.object(
                    MODULE,
                    "launching_process_candidates",
                    return_value=[{"pid": 47009, "pgid": 47009}],
                ) as candidates,
                mock.patch.object(
                    MODULE,
                    "process_group_exists",
                    side_effect=lambda _pgid: alive["value"],
                ),
                mock.patch.object(MODULE.os, "killpg", side_effect=kill_group) as killpg,
                mock.patch.object(MODULE, "port_available") as port_available,
                self.assertRaisesRegex(
                    MODULE.CaseDeadlineExpired,
                    "workflow_case_frozen_timeout",
                ),
            ):
                owned.start()
            candidates.assert_called_once()
            killpg.assert_called_once()
            port_available.assert_not_called()
            self.assertFalse(alive["value"])
            self.assertFalse(state_path.exists())

    def test_owned_process_close_signals_verified_process_group_and_releases_port(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            state_path = root / "owned.json"
            state_path.write_text("{}", encoding="utf-8")

            alive = {"value": True}

            class FakeProcess:
                pid = 43009

                def poll(self):
                    return None if alive["value"] else 0

                def wait(self, timeout):
                    alive["value"] = False
                    return 0

            def kill_group(pgid, signal_value):
                self.assertEqual(pgid, FakeProcess.pid)
                self.assertEqual(signal_value, MODULE.signal.SIGTERM)
                alive["value"] = False

            owned = MODULE.OwnedProcess(
                name="frontend",
                arguments=("pnpm", "exec", "vite"),
                cwd=root,
                environment={},
                log_path=root / "frontend.log",
                state_path=state_path,
                owned_port=15209,
            )
            owned.process = FakeProcess()
            owned.log_handle = mock.Mock()
            with (
                mock.patch.object(MODULE, "process_group_exists", side_effect=lambda _: alive["value"]),
                mock.patch.object(MODULE.os, "killpg", side_effect=kill_group) as killpg,
                mock.patch.object(MODULE, "port_available", return_value=True),
            ):
                owned.close()
            killpg.assert_called_once()
            owned.log_handle = None
            self.assertFalse(state_path.exists())

    def test_cleanup_browser_close_is_strict_and_purges_exact_private_scratch(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            lane = root / "lane"
            scratch = lane / "playwright"
            scratch.mkdir(parents=True)
            (scratch / "trace-secret.trace").write_text("private", encoding="utf-8")
            attempt = root / "attempt"
            helper_state = attempt / ".workflow-helper" / "state.json"
            helper_state.parent.mkdir(parents=True)
            helper_state.write_text("{}", encoding="utf-8")
            spec = MODULE.CASE_SPECS["WFL-009"]
            state = {
                "case_id": spec.case_id,
                "phase": "trace_active",
                "scratch_dir": str(scratch),
                "base_url": spec.frontend_origin,
                "fixture": {"database_clone": spec.database},
            }
            with (
                mock.patch.object(MODULE, "lane_dir", return_value=lane),
                mock.patch.object(MODULE.workflow, "state_path", return_value=helper_state),
                mock.patch.object(MODULE.workflow, "load_state", return_value=state),
                mock.patch.object(MODULE.workflow, "close_persisted_session") as close_session,
            ):
                MODULE.close_abandoned_browser(spec, attempt)
            close_session.assert_called_once_with(attempt, MODULE.workflow.DEFAULT_WRAPPER)
            self.assertFalse(scratch.exists())

    def test_cleanup_state_allows_product_drift_but_keeps_cleanup_pins(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            state_path = root / "state.json"
            state_path.write_text("{}", encoding="utf-8")
            spec = MODULE.CASE_SPECS["WFL-003"]
            attempt = root / "attempt"
            cleanup_digest = "d" * 64
            chromium_bundle_digest = "c" * 64
            backend_digest = "b" * 64
            backend_binary = root / "removed-backend"
            chromium_executable = root / "chromium"
            chromium_executable.write_bytes(b"fake chromium")
            chromium_executable.chmod(0o700)
            chromium_executable_digest = MODULE.file_sha(chromium_executable)
            chromium_executable.unlink()
            state = {
                "schema_version": MODULE.SCHEMA_VERSION,
                "owner_version": MODULE.OWNER_VERSION,
                "run_id": MODULE.RUN_ID,
                "matrix_sha256": MODULE.workflow.MATRIX_SHA256,
                "case_id": spec.case_id,
                "database": spec.database,
                "ports": {
                    "backend": spec.backend_port,
                    "frontend": spec.frontend_port,
                    "mock": spec.mock_port,
                },
                "attempt_dir": str(attempt),
                "backend_binary": str(backend_binary),
                "chromium_executable": str(chromium_executable),
                "chromium_bundle_sha256": chromium_bundle_digest,
                "input_fingerprints": {
                    "database_lane": cleanup_digest,
                    "backend_binary": backend_digest,
                    "chromium_executable": chromium_executable_digest,
                    "chromium_bundle": chromium_bundle_digest,
                },
            }
            with (
                mock.patch.object(MODULE, "lane_state_path", return_value=state_path),
                mock.patch.object(MODULE, "read_json_0600", return_value=state),
                mock.patch.object(MODULE.workflow, "validate_attempt_dir", return_value=attempt),
                mock.patch.object(
                    MODULE,
                    "cleanup_input_fingerprints",
                    return_value={"database_lane": cleanup_digest},
                ),
                mock.patch.object(
                    MODULE,
                    "pinned_input_fingerprints",
                    side_effect=AssertionError("full execution pins must not gate cleanup"),
                ),
            ):
                loaded = MODULE.load_state(spec, verify_inputs=False)
            self.assertEqual(loaded, state)

    def test_prepared_execution_edge_rechecks_artifacts_clone_and_sources(self):
        spec = MODULE.CASE_SPECS["WFL-003"]
        database = object()
        state = {
            "case_id": spec.case_id,
            "database_clone_fingerprint": "f" * 64,
            "database_clone_identity": "c" * 64,
        }
        events = []
        with (
            mock.patch.object(
                MODULE,
                "validate_prepared_artifacts",
                side_effect=lambda actual_spec, actual_state: events.append(
                    ("artifacts", actual_spec, actual_state)
                ),
            ),
            mock.patch.object(MODULE, "case_database_exists", return_value=True),
            mock.patch.object(MODULE.support, "LocalPostgres", return_value=database),
            mock.patch.object(
                MODULE,
                "physical_database_identity",
                return_value=({"database": spec.database}, "f" * 64),
            ),
            mock.patch.object(MODULE, "exact_database_content_identity", return_value="c" * 64),
            mock.patch.object(
                MODULE,
                "verify_execution_inputs",
                side_effect=lambda actual_state: events.append(("sources", actual_state)),
            ),
        ):
            MODULE.verify_prepared_execution_inputs(spec, state)
        self.assertEqual([item[0] for item in events], ["artifacts", "sources"])

    def test_prepared_execution_edge_rejects_full_content_hash_drift(self):
        spec = MODULE.CASE_SPECS["WFL-003"]
        state = {
            "case_id": spec.case_id,
            "database_clone_fingerprint": "f" * 64,
            "database_clone_identity": "c" * 64,
        }
        with (
            mock.patch.object(MODULE, "validate_prepared_artifacts"),
            mock.patch.object(MODULE, "case_database_exists", return_value=True),
            mock.patch.object(MODULE.support, "LocalPostgres", return_value=object()),
            mock.patch.object(
                MODULE,
                "physical_database_identity",
                return_value=({"database": spec.database}, "f" * 64),
            ),
            mock.patch.object(MODULE, "exact_database_content_identity", return_value="d" * 64),
            mock.patch.object(MODULE, "verify_execution_inputs") as source_check,
            self.assertRaisesRegex(
                MODULE.CaseError,
                "workflow_prepared_clone_identity_changed",
            ),
        ):
            MODULE.verify_prepared_execution_inputs(spec, state)
        source_check.assert_not_called()

    def test_exact_content_identity_delegates_to_full_db_lane_hash(self):
        spec = MODULE.CASE_SPECS["WFL-007"]
        baseline = "a" * 64
        retained_settings_drift = "b" * 64
        outputs = iter(
            (
                "database=%s|content_sha256=%s" % (spec.database, baseline),
                "database=%s|content_sha256=%s"
                % (spec.database, retained_settings_drift),
            )
        )
        with mock.patch.object(
            MODULE.support,
            "run_lane",
            side_effect=lambda *_args, **_kwargs: next(outputs),
        ) as lane:
            before = MODULE.exact_database_content_identity(spec)
            after = MODULE.exact_database_content_identity(spec)
        self.assertEqual(before, baseline)
        self.assertEqual(after, retained_settings_drift)
        self.assertNotEqual(before, after)
        self.assertEqual(lane.call_count, 2)
        for call in lane.call_args_list:
            self.assertEqual(call.args, (["content-hash", spec.database], MODULE.ROOT))
            self.assertEqual(call.kwargs, {"timeout": 120})

    def test_browser_assertion_code_is_structured_as_product_failure(self):
        spec = MODULE.CASE_SPECS["WFL-005"]
        browser = MODULE.BrowserCase(
            spec,
            {"attempt_dir": "/tmp/attempt", "paths": {"private_values": None}},
        )
        browser.cli = types.SimpleNamespace(
            run=mock.Mock(
                return_value=(
                    '### Result\n{"ok":false,"assertion_failure":true,'
                    '"failure_code":"routing_health_row_missing","network_events":[]}\n'
                    "### Ran code"
                )
            )
        )
        browser.capture_snapshot = mock.Mock(return_value={})
        with self.assertRaises(MODULE.CaseError) as raised:
            browser.run_code("routing_health", "async (page) => ({ok: true})")
        self.assertEqual(raised.exception.code, "routing_health_row_missing")
        self.assertTrue(raised.exception.assertion_failure)
        wrapped = browser.cli.run.call_args.args[1]
        self.assertIn("assertion_failure: true", wrapped)
        self.assertIn("return await (", wrapped)
        self.assertEqual(browser.capture_snapshot.call_count, 2)

    def test_browser_harness_code_remains_infrastructure_failure(self):
        spec = MODULE.CASE_SPECS["WFL-005"]
        browser = MODULE.BrowserCase(
            spec,
            {"attempt_dir": "/tmp/attempt", "paths": {"private_values": None}},
        )
        browser.cli = types.SimpleNamespace(
            run=mock.Mock(
                return_value=(
                    '### Result\n{"ok":false,"assertion_failure":true,'
                    '"failure_code":"workflow_origin_mismatch","network_events":[]}\n'
                    "### Ran code"
                )
            )
        )
        browser.capture_snapshot = mock.Mock(return_value={})
        with self.assertRaises(MODULE.CaseError) as raised:
            browser.run_code("routing_health", "async (page) => ({ok: true})")
        self.assertFalse(raised.exception.assertion_failure)

    def test_projection_timeout_is_product_failure_but_mock_fixture_shape_is_infrastructure(self):
        database = types.SimpleNamespace(read_json=mock.Mock())
        with (
            mock.patch.object(MODULE.support, "wait_until", return_value=None),
            self.assertRaises(MODULE.CaseError) as projection_failure,
        ):
            MODULE.wait_request_projection(database, "matrix-projection-timeout", timeout=0.01)
        self.assertEqual(
            projection_failure.exception.code,
            "workflow_request_projection_timeout",
        )
        self.assertTrue(projection_failure.exception.assertion_failure)

        mock_client = types.SimpleNamespace(
            json=mock.Mock(return_value=(200, {"data": "not-a-ledger"}))
        )
        with self.assertRaises(MODULE.CaseError) as fixture_failure:
            MODULE.safe_mock_ledger(mock_client, "matrix-mock-fixture")
        self.assertEqual(
            fixture_failure.exception.code,
            "workflow_mock_ledger_shape_invalid",
        )
        self.assertFalse(fixture_failure.exception.assertion_failure)

    def test_preparation_receipt_exists_before_backend_build(self):
        with tempfile.TemporaryDirectory() as temporary:
            lane = pathlib.Path(temporary) / "lane"
            receipt_path = lane / "preparation.json"
            spec = MODULE.CASE_SPECS["WFL-003"]
            chromium = pathlib.Path(temporary) / "chromium"
            chromium.write_bytes(b"chromium")
            chromium.chmod(0o700)

            def reject_build(_target):
                receipt = json.loads(receipt_path.read_text(encoding="utf-8"))
                self.assertEqual(receipt["state"], "creating")
                raise MODULE.CaseError("synthetic_build_interruption")

            common = {
                "schema_version": MODULE.PREPARATION_SCHEMA_VERSION,
                "owner_version": MODULE.OWNER_VERSION,
                "run_id": MODULE.RUN_ID,
                "matrix_sha256": MODULE.workflow.MATRIX_SHA256,
                "case_id": spec.case_id,
                "database": spec.database,
                "ports": {
                    "backend": spec.backend_port,
                    "frontend": spec.frontend_port,
                    "mock": spec.mock_port,
                },
                "branch": MODULE.workflow.EXPECTED_BRANCH,
                "branch_head": "a" * 40,
                "chromium_executable": str(chromium),
                "preparation_fence": {"owner": "b" * 64},
            }
            with (
                mock.patch.object(MODULE, "lane_dir", return_value=lane),
                mock.patch.object(MODULE, "preparation_receipt_path", return_value=receipt_path),
                mock.patch.object(MODULE, "require_ports_available"),
                mock.patch.object(MODULE, "load_state", return_value=None),
                mock.patch.object(MODULE.workflow, "discover_local_chromium", return_value=chromium),
                mock.patch.object(MODULE.workflow, "validate_chromium_executable", return_value=chromium),
                mock.patch.object(MODULE, "load_preparation_receipt", return_value=None),
                mock.patch.object(MODULE, "preparation_common", return_value=common),
                mock.patch.object(MODULE, "build_case_backend", side_effect=reject_build),
                self.assertRaisesRegex(MODULE.CaseError, "synthetic_build_interruption"),
            ):
                MODULE.prepare_case(spec)
            self.assertTrue(receipt_path.is_file())
            self.assertEqual(json.loads(receipt_path.read_text())["state"], "creating")

    def test_creating_preparation_receipt_recovers_owned_orphans(self):
        with tempfile.TemporaryDirectory() as temporary:
            lane = pathlib.Path(temporary) / "lane"
            binary = lane / "bin" / "prism-backend"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"partial build")
            building = lane / "bin" / ".prism-backend.building"
            building.write_bytes(b"interrupted build")
            config = lane / "config.json"
            config.write_text("{}", encoding="utf-8")
            atomic_config = lane / ".config.json.orphaned"
            atomic_config.write_text("{}", encoding="utf-8")
            receipt = lane / "preparation.json"
            receipt.write_text("{}", encoding="utf-8")
            receipt.chmod(0o600)
            atomic_receipt = lane / ".preparation.json.orphaned"
            atomic_receipt.write_text("{}", encoding="utf-8")
            spec = MODULE.CASE_SPECS["WFL-003"]
            with (
                mock.patch.object(MODULE, "lane_dir", return_value=lane),
                mock.patch.object(MODULE, "preparation_receipt_path", return_value=receipt),
                mock.patch.object(MODULE, "case_database_exists", return_value=False),
                mock.patch.object(MODULE, "assert_case_database_absent"),
            ):
                MODULE.reconcile_creating_preparation(spec, {"state": "creating"})
            self.assertFalse(receipt.exists())
            self.assertFalse(binary.exists())
            self.assertFalse(building.exists())
            self.assertFalse(config.exists())
            self.assertFalse(atomic_config.exists())
            self.assertFalse(atomic_receipt.exists())

    def test_creating_preparation_receipt_reconciles_after_hash_drift(self):
        with tempfile.TemporaryDirectory() as temporary:
            lane = pathlib.Path(temporary) / "lane"
            lane.mkdir()
            receipt_path = lane / "preparation.json"
            spec = MODULE.CASE_SPECS["WFL-003"]
            receipt = {
                "schema_version": MODULE.PREPARATION_SCHEMA_VERSION,
                "owner_version": MODULE.OWNER_VERSION,
                "run_id": MODULE.RUN_ID,
                "matrix_sha256": MODULE.workflow.MATRIX_SHA256,
                "case_id": spec.case_id,
                "database": spec.database,
                "ports": {
                    "backend": spec.backend_port,
                    "frontend": spec.frontend_port,
                    "mock": spec.mock_port,
                },
                "branch": MODULE.workflow.EXPECTED_BRANCH,
                "branch_head": "a" * 40,
                "chromium_executable": str(pathlib.Path(temporary) / "removed-chromium"),
                "preparation_fence": {
                    name: ("c" if name == "workflow_owner" else "b") * 64
                    for name in MODULE.PREPARATION_FENCE_KEYS
                },
                "state": "creating",
                "created_at": "2026-08-14T00:00:00Z",
            }
            receipt_path.write_text(json.dumps(receipt), encoding="utf-8")
            receipt_path.chmod(0o600)
            with mock.patch.object(MODULE, "preparation_receipt_path", return_value=receipt_path):
                loaded = MODULE.load_reconcilable_creating_receipt(spec)
            self.assertEqual(loaded, receipt)

    def test_preparation_handoff_exposes_exact_runner_fingerprint_set(self):
        with tempfile.TemporaryDirectory() as temporary:
            receipt_path = pathlib.Path(temporary) / "preparation.json"
            receipt_path.write_text("{}\n", encoding="utf-8")
            spec = MODULE.CASE_SPECS["WFL-003"]
            receipt = {
                "case_id": spec.case_id,
                "state": "prepared",
                "branch_head": "a" * 40,
                "database": spec.database,
                "database_clone_fingerprint": "b" * 64,
            }
            with mock.patch.object(MODULE, "preparation_receipt_path", return_value=receipt_path):
                handoff = MODULE.preparation_handoff(receipt)
            self.assertEqual(
                handoff["runner_fingerprints"],
                {
                    "branch_head": "a" * 40,
                    "config": MODULE.workflow.CONFIG_FINGERPRINT,
                    "database_template": MODULE.workflow.TEMPLATE_FINGERPRINT,
                    "source_dump": MODULE.workflow.SOURCE_DUMP_FINGERPRINT,
                },
            )
            self.assertEqual(handoff["database_clone"], "b" * 64)

    def test_wfl_008_binds_operator_and_endpoint_values_to_private_indexes(self):
        case_spec = MODULE.CASE_SPECS["WFL-008"]
        self.assertEqual(case_spec.sensitive_value_labels, ("endpoint_key", "operator_password"))
        self.assertEqual(
            tuple(MODULE.workflow.REQUIRED_EVIDENCE["WFL-008"]),
            (
                "auth-transition.json",
                "multi-tab-snapshots.json",
                "key-rotation-grid.redacted.json",
                "session-storage-audit.json",
                "trace.zip",
            ),
        )

    def test_wfl_008_generated_browser_programs_parse_and_evidence_is_safe(self):
        responses = {
            "auth_key_setup": {"key_id": 17, "page_count": 2, "one_time_ui_closed": True, "create_status": 201},
            "auth_enable": {
                "account_status": 200,
                "enable_status": 200,
                "effect_state": "effective",
                "session_action": "clear_and_login",
                "primary_path": "/auth/login",
                "secondary_path": "/auth/login",
                "secondary_redirected": True,
                "storage_event": {"key": "prism.authStateVersion", "kind": "auth_changed", "sequence": 2},
                "account_storage_event_observed": True,
                "account_storage_baseline_sequence": 0,
                "storage_baseline_sequence": 1,
                "storage_receipt_removed": True,
            },
            "multi_tab_login": {"login_status": 200, "primary_path": "/observe", "secondary_path": "/observe"},
            "refresh_cross_tab_sync": {"page_count": 2, "primary_authenticated": True, "secondary_authenticated": True},
            "proxy_key_rotate": {"key_id": 17, "rotate_status": 200, "rotation_count": 1},
            "proxy_key_old_new_probe": {"old_value_status": 401, "new_value_status": 200},
            "proxy_key_revoke": {"delete_status": 200},
            "proxy_key_revoked_probe": {"revoked_value_status": 401},
            "logout_storage_audit": {
                "logout_status": 204,
                "primary_path": "/auth/login",
                "secondary_path": "/auth/login",
                "before": {"local_names": [], "session_names": [], "sensitive_absent": True, "http_state_record_count": 2},
                "after": {"local_names": [], "session_names": [], "sensitive_absent": True, "http_state_record_count": 0},
                "ephemeral_values_destroyed": True,
            },
            "login_for_disable": {"login_status": 200, "sensitive_ui_cleared": True},
            "auth_disable": {
                "disable_status": 200,
                "effect_state": "effective",
                "session_action": "clear_and_continue",
                "effective_mode": "disabled",
                "storage_event": {"key": "prism.authStateVersion", "kind": "auth_changed", "sequence": 2},
                "storage_baseline_sequence": 1,
                "storage_receipt_removed": True,
            },
            "open_shell_restore": {
                "primary_path": "/system/settings",
                "secondary_path": "/observe",
                "primary_open_shell": True,
                "secondary_open_shell": True,
                "http_state_record_count": 0,
            },
        }

        class FakeBrowser:
            def __init__(self):
                self.spec = MODULE.CASE_SPECS["WFL-008"]
                self.state = {"private_value_indexes": {"operator_password": 1}}
                self.programs = []
                self.checkpoints = []
                self.evidence = {}
                self.trace_started = False
                self.trace_receipts = []

            def goto(self, path):
                self.goto_path = path

            def run_code(self, step, code, **kwargs):
                self.programs.append((step, code, kwargs))
                return responses[step]

            def checkpoint(self, name):
                self.checkpoints.append(name)

            def capture_snapshot(self, label):
                return {"label": label, "path": "snapshots/%s.snapshot.txt" % label, "bytes": 1, "sha256": "a" * 64, "fatal_markers": []}

            def start_trace(self, *, sensitive_ui_cleared):
                self.trace_started = sensitive_ui_cleared
                receipt = {
                    "case_id": "WFL-008",
                    "trace_active": True,
                    "started_ns": 1,
                    "started_at": "2026-08-14T00:00:00Z",
                }
                self.trace_receipts.append(receipt)
                return receipt

            def stop_trace(self):
                receipt = {
                    "case_id": "WFL-008",
                    "started_ns": 1,
                    "ended_at": "2026-08-14T00:00:01Z",
                    "trace": {"path": "trace.zip", "bytes": 1, "sha256": "a" * 64},
                }
                self.trace_receipts.append(receipt)
                return receipt

            def write_json(self, name, value):
                document = {"schema_version": 1, "case_id": "WFL-008", **value}
                MODULE.workflow.assert_safe_json(document, name)
                self.evidence[name] = document

        browser = FakeBrowser()
        initial = {"state": "disabled", "login_available": False}
        final = {"state": "disabled", "login_available": False}
        projection = {"status_codes": [200], "proxy_key_ids": [17], "attribution_states": ["identified"]}
        with (
            mock.patch.object(MODULE, "api_object", side_effect=[initial, final]),
            mock.patch.object(
                MODULE,
                "create_runtime_fixture",
                return_value={"runtime_model": "matrix-wfl-008-model"},
            ),
            mock.patch.object(MODULE, "wait_request_projection", return_value=projection),
            mock.patch.object(MODULE, "caller_row_count", return_value=0),
        ):
            MODULE.run_wfl_008(browser, object(), object())

        self.assertTrue(browser.trace_started)
        self.assertEqual(browser.checkpoints, list(browser.spec.scenario_steps[:-1]))
        self.assertEqual(
            set(browser.evidence),
            set(MODULE.workflow.REQUIRED_EVIDENCE["WFL-008"]) - {"trace.zip"},
        )
        for step, program, kwargs in browser.programs:
            with self.subTest(step=step):
                completed = subprocess.run(
                    ["node", "--check", "-"],
                    input="const action = %s;\n" % program,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    check=False,
                )
                self.assertEqual(completed.returncode, 0, completed.stdout)
                self.assertNotIn("Mx-", program)
                if "private_environment" in kwargs:
                    self.assertEqual(
                        kwargs["private_environment"],
                        {"PRISM_WFL_PRIVATE_OPERATOR": 1},
                    )

    def test_wfl_009_generated_browser_programs_parse_and_evidence_is_safe(self):
        cutoff = "2026-08-13T00:00:00Z"
        preflight_cutoff = "2026-08-13T00:00:00.000000Z"
        old_partition = "request_logs_p20260812"
        boundary_partition = "request_logs_p20260813"
        future_partition = "request_logs_p20260814"
        oracle = {
            "dataset": "request_logs",
            "matched_rows_estimate": 14,
            "retained_rows_estimate": 14,
            "whole_partition_count": 1,
            "whole_partition_names": [old_partition],
            "whole_partition_names_preview": [old_partition],
            "boundary_partition_count": 1,
            "boundary_partition_estimate": 13,
            "semantic_actual_matched_rows": 1,
            "semantic_actual_retained_rows": 14,
        }
        previewed_at = MODULE.dt.datetime.now(MODULE.dt.timezone.utc).replace(microsecond=0)
        expires_at = previewed_at + MODULE.dt.timedelta(minutes=5)

        def preflight(preflight_id):
            return {
                "preflight_id": preflight_id,
                "kind": "manual_cleanup",
                "scope": "instance",
                "previewed_at": MODULE.retention.iso(previewed_at),
                "expires_at": MODULE.retention.iso(expires_at),
                "confirmation_keyword": "DELETE",
                "capability_present": True,
                "affected_domains": [{
                    "dataset": "request_logs",
                    "resolved_cutoff": preflight_cutoff,
                    "matched_rows": {"value": "14", "accuracy": "estimated", "method": "partition_metadata"},
                    "retained_rows": {"value": "14", "accuracy": "estimated", "method": "partition_metadata"},
                    "whole_partitions": {
                        "count": "1",
                        "names_preview": [old_partition],
                        "names_total_count": "1",
                        "truncated": False,
                    },
                    "boundary_partitions": [{
                        "name": boundary_partition,
                        "matched_rows": {"value": "13", "accuracy": "estimated", "method": "partition_metadata"},
                    }],
                    "semantic_facts_complete": True,
                    "warnings": ["local fixture warning"],
                }],
            }

        def job(job_id, state, cancel_allowed, *, terminal_disposition=None):
            return {
                "id": job_id,
                "contract_version": 2,
                "dataset": "request_logs",
                "origin": "manual",
                "state": state,
                "terminal_disposition": terminal_disposition,
                "mode": "cutoff",
                "cutoff": cutoff,
                "requested_at": "2026-08-14T10:00:00Z",
                "started_at": None,
                "finished_at": None,
                "attempt_count": 0,
                "cancel_allowed": cancel_allowed,
                "progress": {
                    "accounting_provenance": "v2_exact",
                    "stage": "queued" if state == "queued" else "finished",
                    "visibility_state": "unchanged" if state != "succeeded" else "revoked",
                    "purge_state": "not_started" if state != "succeeded" else "published",
                    "rows_matched_estimate": "14",
                    "rows_matched_accuracy": "estimated",
                    "boundary_rows_deleted": "0",
                    "boundary_batches_completed": "0",
                    "dropped_partition_count": "1" if state == "succeeded" else "0",
                    "dropped_partition_count_accuracy": "exact",
                    "dropped_partition_names_preview": [old_partition] if state == "succeeded" else [],
                    "dropped_partition_names_total_count": "1" if state == "succeeded" else "0",
                    "dropped_rows_estimate": "1" if state == "succeeded" else "0",
                    "dropped_rows_accuracy": "estimated",
                    "last_checkpoint_at": "2026-08-14T10:00:05Z" if state != "queued" else None,
                },
                "error": None,
            }

        cancelled_id = "job_aaaaaaaaaaaaaaaaaaaaaaaa"
        completed_id = "job_bbbbbbbbbbbbbbbbbbbbbbbb"
        cancelled_job = job(cancelled_id, "cancelled", False, terminal_disposition="cancelled")
        completed_job = job(completed_id, "succeeded", False, terminal_disposition="completed")
        cancelled_detail = {
            "job": cancelled_job,
            "terminal_result": {
                "kind": "cancelled",
                "finished_at": "2026-08-14T10:00:01Z",
                "visibility_state": "unchanged",
                "published_epoch": None,
                "published_floor": None,
                "accounting_provenance": "v2_exact",
                "cancellation_scope": "queued_no_data_changed",
                "coherent_outcome": "no_data_changed",
            },
            "checkpoints": [],
            "partitions": [],
            "checkpoint_page_complete": True,
            "partition_page_complete": True,
        }
        required_stages = (
            "purge_running",
            "dropping_partitions",
            "deleting_boundary_rows",
            "publishing_epoch_coverage",
        )
        completed_detail = {
            "job": completed_job,
            "terminal_result": {
                "kind": "succeeded",
                "finished_at": "2026-08-14T10:00:05Z",
                "visibility_state": "revoked",
                "published_epoch": "2",
                "published_floor": cutoff,
                "accounting_provenance": "v2_exact",
                "cancellation_scope": None,
                "coherent_outcome": "published",
            },
            "checkpoints": [
                {
                    "sequence": str(index + 1),
                    "kind": "checkpoint",
                    "stage": stage,
                    "boundary_rows_delta": "0",
                    "dropped_partition_delta": "1" if stage == "dropping_partitions" else "0",
                    "safe_detail_code": None,
                    "recorded_at": "2026-08-14T10:00:05Z",
                }
                for index, stage in enumerate(required_stages)
            ],
            "partitions": [{
                "sequence": "1",
                "partition_name": old_partition,
                "action": "dropped",
                "boundary_rows_deleted": "0",
                "dropped_rows_estimate": "1",
                "dropped_rows_accuracy": "estimated",
                "evidence_at": "2026-08-14T10:00:05Z",
            }],
            "checkpoint_page_complete": True,
            "partition_page_complete": True,
        }
        responses = {
            "retention_preflight_wrong": {
                "preflight_status": 201,
                "preflight": preflight("pf_cancel"),
                "wrong_phrase": "WRONG",
                "confirm_disabled": True,
                "manual_job_post_count": 0,
                "visible_summary": {
                    "dataset": "request_logs",
                    "retention_days": 1,
                    "matched_rows": "14",
                    "retained_rows": "14",
                },
            },
            "retention_start_job": {
                "accepted_status": 202,
                "job": job(cancelled_id, "queued", True),
                "row_visible": True,
            },
            "retention_cancel_job": {
                "cancel_status": 200,
                "refreshed_from_list": True,
                "stale_poll_monotonic": True,
                "cancelled": cancelled_detail,
            },
            "retention_restart_job": {
                "preflight_status": 201,
                "accepted_status": 202,
                "preflight": preflight("pf_complete"),
                "job": job(completed_id, "queued", True),
            },
            "retention_wait_completion": {
                "completed": completed_detail,
                "list_terminal_state": "succeeded",
            },
            "retention_completion_refresh": {
                "refreshed": True,
                "completed": completed_detail,
            },
        }
        baseline_counts = {
            dataset: {"old_rows": 1, "retained_rows": MODULE.WFL_009_VOLUME + 2}
            for dataset in MODULE.retention.MANAGED_DATASETS
        }
        final_counts = dict(baseline_counts)
        final_counts["request_logs"] = {"old_rows": 0, "retained_rows": MODULE.WFL_009_VOLUME + 2}
        baseline_state = {
            "database": MODULE.CASE_SPECS["WFL-009"].database,
            "oid": "9009",
            "marker_counts": baseline_counts,
            "total_counts": {
                dataset: {"before_cutoff": 1, "total": MODULE.WFL_009_VOLUME + 3}
                for dataset in MODULE.retention.MANAGED_DATASETS
            },
            "harness_counts": {
                dataset: 0 for dataset in MODULE.retention.MANAGED_DATASETS
            },
            "partitions": {
                dataset: [
                    {"name": "%s_p20260812" % dataset},
                    {"name": "%s_p20260813" % dataset},
                    {"name": "%s_p20260814" % dataset},
                ]
                for dataset in MODULE.retention.MANAGED_DATASETS
            },
            "purge_states": {dataset: "idle" for dataset in MODULE.retention.MANAGED_DATASETS},
        }
        baseline_state["partitions"]["request_logs"] = [
            {"name": old_partition},
            {"name": boundary_partition},
            {"name": future_partition},
        ]
        final_state = {
            **baseline_state,
            "marker_counts": final_counts,
            "partitions": {
                **baseline_state["partitions"],
                "request_logs": [
                    {"name": boundary_partition},
                    {"name": future_partition},
                ],
            },
            "purge_states": {
                **baseline_state["purge_states"],
                "request_logs": "published",
            },
        }

        class FakeBrowser:
            def __init__(self):
                self.spec = MODULE.CASE_SPECS["WFL-009"]
                database_content_identity = "c" * 64
                self.state = {
                    "database_clone_identity": database_content_identity,
                    "wfl_009_fixture": {
                        "marker": MODULE.WFL_009_MARKER,
                        "caller_prefix": MODULE.WFL_009_CALLER_PREFIX,
                        "volume": MODULE.WFL_009_VOLUME,
                        "inherited_nonterminal_retention_jobs": 0,
                        "cutoff": cutoff,
                        "old_day": "20260812",
                        "boundary_day": "20260813",
                        "future_day": "20260814",
                        "database_oid": "9009",
                        "database_content_identity": database_content_identity,
                        "queued_cancel_guard": {"present": True},
                    }
                }
                self.programs = []
                self.checkpoints = []
                self.evidence = {}
                self.trace_started = False

            def goto(self, path):
                self.goto_path = path

            def start_trace(self):
                self.trace_started = True

            def run_code(self, step, code, **kwargs):
                self.programs.append((step, code, kwargs))
                return responses[step]

            def capture_snapshot(self, label):
                return {"label": label, "path": "snapshots/%s.snapshot.txt" % label, "bytes": 1, "sha256": "a" * 64, "fatal_markers": []}

            def promote_snapshot(self, name, snapshot):
                self.evidence[name] = {"snapshot": snapshot["path"]}

            def checkpoint(self, name):
                self.checkpoints.append(name)

            def write_json(self, name, value):
                document = {"schema_version": 1, "case_id": "WFL-009", **value}
                MODULE.workflow.assert_safe_json(document, name)
                self.evidence[name] = document

        browser = FakeBrowser()
        database = types.SimpleNamespace(mutate_case=mock.Mock())
        settings = {
            "state": "ready",
            "server_now": "2026-08-14T10:00:00Z",
            "configured_logical_cutoffs": {
                dataset: None for dataset in MODULE.retention.MANAGED_DATASETS
            },
        }
        job_projection = {
            "jobs": [cancelled_job, completed_job],
            "bound_manual_request_job_count": 2,
            "bound_consumed_preflight_count": 2,
            "queued_cancel_guard_present": True,
            "guard_state": {"deferred_count": 1, "deferred_job_id": cancelled_id},
            "nonterminal_retention_job_count": 0,
        }
        guard_states = [
            {
                "trigger_count": 1,
                "state_rows": 1,
                "deferred_count": 0,
                "deferred_job_id": None,
                "deferred_job_delay_seconds": None,
                "nonterminal_retention_jobs": 0,
            },
            {
                "trigger_count": 1,
                "state_rows": 1,
                "deferred_count": 1,
                "deferred_job_id": cancelled_id,
                "deferred_job_delay_seconds": MODULE.WFL_009_CANCEL_DEFER_SECONDS,
                "nonterminal_retention_jobs": 1,
            },
            {
                "trigger_count": 1,
                "state_rows": 1,
                "deferred_count": 1,
                "deferred_job_id": cancelled_id,
                "deferred_job_delay_seconds": MODULE.WFL_009_CANCEL_DEFER_SECONDS,
                "nonterminal_retention_jobs": 0,
            },
        ]
        retention_metadata = {
            "settings": {"singleton_key": "global"},
            "policy_resources": [
                {"dataset": dataset} for dataset in MODULE.retention.MANAGED_DATASETS
            ],
            "coverage_read_models": [
                {"dataset": dataset} for dataset in MODULE.retention.MANAGED_DATASETS
            ],
            "audit_fence": {"id": 1},
        }
        with (
            mock.patch.object(MODULE, "wait_wfl_009_retention_disabled", return_value=settings),
            mock.patch.object(MODULE, "assert_wfl_009_runtime_day", return_value=settings),
            mock.patch.object(
                MODULE,
                "wfl_009_state",
                side_effect=[baseline_state, baseline_state, baseline_state, final_state],
            ),
            mock.patch.object(
                MODULE,
                "wfl_009_retention_metadata",
                return_value=retention_metadata,
            ),
            mock.patch.object(MODULE.retention, "preflight_oracle_from_state", return_value=oracle),
            mock.patch.object(MODULE, "wfl_009_guard_state", side_effect=guard_states),
            mock.patch.object(MODULE, "wfl_009_job_database_projection", return_value=job_projection),
        ):
            MODULE.run_wfl_009(browser, object(), database)

        self.assertTrue(browser.trace_started)
        self.assertEqual(browser.checkpoints, list(browser.spec.scenario_steps[:-1]))
        self.assertEqual(
            set(browser.evidence),
            set(MODULE.workflow.REQUIRED_EVIDENCE["WFL-009"]) - {"trace.zip"},
        )
        for step, program, _ in browser.programs:
            with self.subTest(step=step):
                completed = subprocess.run(
                    ["node", "--check", "-"],
                    input="const action = %s;\n" % program,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    check=False,
                )
                self.assertEqual(completed.returncode, 0, completed.stdout)
                self.assertNotIn("fetch(", program.replace("route.fetch(", ""))
        combined_programs = "\n".join(program for _, program, _ in browser.programs)
        self.assertNotIn("getByRole('alertdialog')", combined_programs)
        self.assertIn("loadAllJobDetailEvidence", combined_programs)

    def test_wfl_009_cutoff_comparison_accepts_wire_precision_only(self):
        expected = MODULE.parse_wfl_009_cutoff("2026-08-13T00:00:00Z")
        self.assertTrue(
            MODULE.wfl_009_timestamps_equal("2026-08-13T00:00:00.000000Z", expected)
        )
        self.assertTrue(MODULE.wfl_009_timestamps_equal("2026-08-13T00:00:00Z", expected))
        self.assertFalse(MODULE.wfl_009_timestamps_equal("2026-08-13T00:00:01Z", expected))
        self.assertFalse(MODULE.wfl_009_timestamps_equal("not-a-time", expected))

    def test_wfl_009_preflight_time_is_utc_exact_five_minutes_and_fresh(self):
        previewed = MODULE.dt.datetime.now(MODULE.dt.timezone.utc).replace(microsecond=0)
        valid = {
            "previewed_at": MODULE.retention.iso(previewed),
            "expires_at": MODULE.retention.iso(previewed + MODULE.dt.timedelta(minutes=5)),
        }
        MODULE.assert_wfl_009_preflight_time(valid, minimum_remaining_seconds=30)
        with self.assertRaisesRegex(MODULE.CaseError, "wfl_009_preflight_ttl_mismatch"):
            MODULE.assert_wfl_009_preflight_time(
                {
                    **valid,
                    "expires_at": MODULE.retention.iso(previewed + MODULE.dt.timedelta(minutes=4)),
                }
            )
        with self.assertRaisesRegex(MODULE.CaseError, "wfl_009_preflight_preview_time_invalid"):
            MODULE.assert_wfl_009_preflight_time(
                {**valid, "previewed_at": previewed.isoformat()}
            )

    def test_wfl_009_job_projection_is_bound_to_exact_attempt_jobs(self):
        cancelled_id = "job_aaaaaaaaaaaaaaaaaaaaaaaa"
        completed_id = "job_bbbbbbbbbbbbbbbbbbbbbbbb"
        rows = [
            {
                "id": cancelled_id,
                "state": "cancelled",
                "dataset": "request_logs",
                "origin": "manual",
                "contract_version": 2,
                "preflight_id": "pf_cancel",
                "terminal_disposition": "cancelled",
            },
            {
                "id": completed_id,
                "state": "succeeded",
                "dataset": "request_logs",
                "origin": "manual",
                "contract_version": 2,
                "preflight_id": "pf_complete",
                "terminal_disposition": "completed",
            },
        ]
        database = types.SimpleNamespace(
            read_json=mock.Mock(
                return_value={
                    "jobs": rows,
                    "bound_manual_request_job_count": 2,
                    "bound_consumed_preflight_count": 2,
                    "queued_cancel_guard_present": True,
                    "guard_state": {
                        "deferred_count": 1,
                        "deferred_job_id": cancelled_id,
                    },
                    "nonterminal_retention_job_count": 0,
                }
            )
        )
        projection = MODULE.wfl_009_job_database_projection(
            database,
            cancelled_id,
            completed_id,
        )
        query = database.read_json.call_args.args[0]
        self.assertIn("bound_manual_request_job_count", query)
        self.assertIn("bound_consumed_preflight_count", query)
        self.assertGreaterEqual(query.count("id IN"), 3)
        self.assertEqual(projection["jobs"], rows)

    def test_browser_action_captures_metadata_only_at_exact_origin(self):
        origin = "http://127.0.0.1:18203"
        action = MODULE.action_for_origin(origin, "return {surface: 'contract'};")
        self.assertIn(json.dumps(origin), action)
        self.assertIn("response.request().method()", action)
        self.assertIn("urlPath(response.url())", action)
        self.assertIn("response.status()", action)
        for forbidden in (
            "postData",
            "allHeaders",
            "request().headers",
            "response.body",
            "response.text",
        ):
            self.assertNotIn(forbidden, action)

    def test_minimal_action_executes_in_sparse_pinned_cli_vm_context(self):
        origin = "http://127.0.0.1:18203"
        action = MODULE.action_for_origin(origin, "return {surface: 'vm_contract'};")
        script = r"""
const vm = require('node:vm');
const source = process.argv[1];
const listeners = new Map();
const page = {
  url: () => 'http://127.0.0.1:18203/observe?scope=exact',
  on: (name, listener) => listeners.set(name, listener),
  off: (name, listener) => { if (listeners.get(name) === listener) listeners.delete(name); },
};
const context = vm.createContext({page, __end__: true});
if (vm.runInContext('typeof URL', context) !== 'undefined') throw new Error('sparse_vm_url_present');
const action = vm.runInContext('(' + source + ')', context);
action(page).then((value) => process.stdout.write(JSON.stringify(value))).catch((error) => {
  process.stderr.write(String(error && error.stack || error)); process.exitCode = 1;
});
"""
        completed = subprocess.run(
            ["node", "-e", script, action],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
        )
        self.assertEqual(completed.returncode, 0, completed.stdout)
        self.assertEqual(json.loads(completed.stdout)["surface"], "vm_contract")

    def test_private_file_bridge_never_embeds_value_and_clears_after_action_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            private = pathlib.Path(temporary) / "private"
            values_path = private / "workflow-cases" / "wfl_003" / "private-values.json"
            values_path.parent.mkdir(parents=True)
            private_value = "matrix-private-never-in-program-123456"
            values_path.write_text(json.dumps([private_value]), encoding="utf-8")
            values_path.chmod(0o600)
            state = {
                "attempt_dir": str(pathlib.Path(temporary) / "primary-attempt-1"),
                "paths": {"private_values": str(values_path)},
            }
            browser = MODULE.BrowserCase(MODULE.CASE_SPECS["WFL-003"], state)

            class FakeCLI:
                def __init__(self):
                    self.calls = []

                def run(self, *arguments, **_kwargs):
                    self.calls.append(arguments)
                    if len(self.calls) == 1:
                        return '### Result\n{"loaded": 1}\n'
                    if len(self.calls) == 2:
                        raise MODULE.workflow.WorkflowError("synthetic action failure")
                    return '### Result\n{"cleared": 1, "residue": 0, "unverified": 0}\n'

            cli = FakeCLI()
            browser.cli = cli
            with (
                mock.patch.object(MODULE.workflow, "RUN_PRIVATE", private),
                mock.patch.object(browser, "capture_snapshot"),
                self.assertRaisesRegex(MODULE.workflow.WorkflowError, "synthetic action failure"),
            ):
                browser.run_code(
                    "private_bridge_failure",
                    MODULE.action_for_origin(
                        MODULE.CASE_SPECS["WFL-003"].frontend_origin,
                        "return {surface: 'never_reached'};",
                    ),
                    private_environment={"PRISM_WFL_PRIVATE_ENDPOINT": 0},
                )
            self.assertEqual(len(cli.calls), 3)
            combined = "\n".join(str(argument) for call in cli.calls for argument in call)
            self.assertNotIn(private_value, combined)
            self.assertIn(str(values_path), combined)
            self.assertIn("PRISM_WFL_PRIVATE_ENDPOINT", combined)
            self.assertIn("data-prism-wfl-private-loader", str(cli.calls[-1]))

    def test_private_file_bridge_clears_after_partial_install_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            private = pathlib.Path(temporary) / "private"
            values_path = private / "workflow-cases" / "wfl_003" / "private-values.json"
            values_path.parent.mkdir(parents=True)
            values_path.write_text(json.dumps(["matrix-private-partial-install"]), encoding="utf-8")
            values_path.chmod(0o600)
            browser = MODULE.BrowserCase(
                MODULE.CASE_SPECS["WFL-003"],
                {
                    "attempt_dir": str(pathlib.Path(temporary) / "primary-attempt-1"),
                    "paths": {"private_values": str(values_path)},
                },
            )

            class FakeCLI:
                def __init__(self):
                    self.calls = []

                def run(self, *arguments, **_kwargs):
                    self.calls.append(arguments)
                    if len(self.calls) == 1:
                        raise MODULE.workflow.WorkflowError("partial private install")
                    return '### Result\n{"cleared": 1, "residue": 0, "unverified": 0}\n'

            cli = FakeCLI()
            browser.cli = cli
            with (
                mock.patch.object(MODULE.workflow, "RUN_PRIVATE", private),
                mock.patch.object(browser, "capture_snapshot"),
                self.assertRaisesRegex(MODULE.workflow.WorkflowError, "partial private install"),
            ):
                browser.run_code(
                    "private_partial_install",
                    MODULE.action_for_origin(
                        MODULE.CASE_SPECS["WFL-003"].frontend_origin,
                        "return {surface: 'never_reached'};",
                    ),
                    private_environment={"PRISM_WFL_PRIVATE_ENDPOINT": 0},
                )
            self.assertEqual(len(cli.calls), 2)
            self.assertIn("data-prism-wfl-private-loader", str(cli.calls[1]))

    def test_private_file_bridge_rejects_unverified_or_residual_clear(self):
        browser = MODULE.BrowserCase(
            MODULE.CASE_SPECS["WFL-003"],
            {"attempt_dir": "/tmp/attempt", "paths": {"private_values": None}},
        )
        for receipt in (
            {"cleared": 0, "residue": 0, "unverified": 1},
            {"cleared": 1, "residue": 1, "unverified": 0},
        ):
            with self.subTest(receipt=receipt):
                browser.cli = types.SimpleNamespace(
                    run=mock.Mock(return_value="### Result\n%s\n" % json.dumps(receipt))
                )
                with self.assertRaisesRegex(
                    MODULE.CaseError,
                    "workflow_private_value_binding_clear_failed",
                ):
                    browser._clear_private_bindings()

    def test_action_origin_rejects_non_loopback_or_path_origins(self):
        for invalid in (
            "https://127.0.0.1:18203",
            "http://localhost:18203/path",
            "http://example.com:18203",
            "http://user@127.0.0.1:18203",
        ):
            with self.subTest(invalid=invalid), self.assertRaises(Exception):
                MODULE.action_for_origin(invalid, "return {};")

    def test_request_projection_uses_v2_scoped_status_and_returns_safe_shape(self):
        value = {
            "rows": 2,
            "request_log_id": 71,
            "ingress_request_id": "local-ingress",
            "row_kinds": ["upstream", "upstream"],
            "status_codes": [503, 200],
            "attempt_numbers": [1, 2],
            "attempt_results": ["retryable_failure", "success"],
            "winner_flags": [False, True],
            "terminal_target_ids": [11, 12],
            "endpoint_labels": ["primary", "fallback"],
            "attribution_states": ["anonymous", "anonymous"],
            "proxy_key_ids": [],
        }
        database = types.SimpleNamespace(read_json=mock.Mock(return_value=value))
        projected = MODULE.wait_request_projection(database, "matrix-local", minimum_rows=2)
        query = database.read_json.call_args.args[0]
        self.assertIn("WHEN 'upstream' THEN rl.upstream_status_code", query)
        self.assertIn("WHEN 'planning' THEN rl.gateway_status_code", query)
        self.assertIn("ELSE rl.legacy_status_code", query)
        self.assertNotIn("request_body", query)
        self.assertEqual(projected, value)
        MODULE.workflow.assert_safe_json(projected, "request_projection")

    def test_wfl_002_database_inventory_is_exact_safe_topology_projection(self):
        value = {
            "models": [
                {
                    "id": 1,
                    "model_id": "codex/gpt-image-2",
                    "api_family": "openai",
                    "openai_accepted_format": None,
                    "openai_image_operations": "generations_and_edits",
                    "is_enabled": True,
                    "access_targets": [
                        {
                            "id": 2,
                            "target_type": "connection",
                            "target_model_id": None,
                            "connection_id": 3,
                            "position": 0,
                            "is_enabled": True,
                        }
                    ],
                }
            ]
        }
        database = types.SimpleNamespace(read_json=mock.Mock(return_value=value))
        self.assertEqual(MODULE.wfl_002_database_inventory(database), value)
        query = database.read_json.call_args.args[0]
        for required in (
            "source.model_id",
            "source.openai_accepted_format",
            "source.openai_image_operations",
            "target.target_type",
            "target_model.model_id",
            "target.target_connection_id",
            "target.position",
            "target.is_enabled",
        ):
            self.assertIn(required, query)
        for forbidden in ("api_key", "request_body", "response_body", "custom_headers"):
            self.assertNotIn(forbidden, query)

    def test_audit_projection_renames_sensitive_database_columns_for_evidence(self):
        value = {
            "request_rows": 1,
            "audit_rows": 1,
            "request_log_id": 72,
            "enabled_flags": [True],
            "capture_flags": [True],
            "audit_ids": [73],
            "ingress_payload_stored_flags": [True],
            "result_payload_stored_flags": [True],
            "ingress_capture_states": ["captured"],
            "result_capture_states": ["captured"],
            "ingress_stored_bytes": [12],
            "result_stored_bytes": [16],
            "scrub_sentinel_present": True,
        }
        database = types.SimpleNamespace(read_json=mock.Mock(return_value=value))
        projected = MODULE.wait_audit_projection(
            database,
            "matrix-audit",
            expected_audit_rows=1,
        )
        self.assertEqual(projected, value)
        MODULE.workflow.assert_safe_json(projected, "audit_projection")

    def test_secret_values_are_bound_only_by_label_to_integer_indexes(self):
        case_spec = MODULE.CASE_SPECS["WFL-006"]
        values, indexes = MODULE.build_private_values(case_spec)
        self.assertEqual(set(indexes), set(case_spec.sensitive_value_labels))
        self.assertEqual(sorted(indexes.values()), list(range(len(values))))
        self.assertEqual(len(values), len(set(values)))
        self.assertTrue(all(isinstance(value, str) and len(value) >= 24 for value in values))

    def test_mode_and_currency_validators_reject_before_browser_use(self):
        browser = object()
        with self.assertRaisesRegex(MODULE.CaseError, "workflow_audit_mode_invalid"):
            MODULE.set_openai_audit_mode(browser, "all")
        for code, symbol in (("usd", "$"), ("US", "$"), ("USDD", "$"), ("USD", "abcdef")):
            with self.subTest(code=code, symbol=symbol), self.assertRaisesRegex(
                MODULE.CaseError,
                "workflow_currency_target_invalid",
            ):
                MODULE.run_currency_migration(
                    browser,
                    target_code=code,
                    target_symbol=symbol,
                    step="invalid_currency",
                )

    def test_cli_has_case_singleton_commands_and_no_bulk_runner_command(self):
        parser = MODULE.build_parser()
        for command in ("self-test", "contract", "status", "prepare-case", "run-case", "cleanup-case"):
            with self.subTest(command=command):
                if command == "self-test":
                    parsed = parser.parse_args([command])
                elif command in {"contract", "status", "prepare-case"}:
                    parsed = parser.parse_args([command, "--case-id", "WFL-003"])
                else:
                    parsed = parser.parse_args(
                        [command, "--case-id", "WFL-003", "--attempt-dir", "/tmp/attempt"]
                    )
                self.assertEqual(parsed.command, command)
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            parser.parse_args(["run-all"])

    def test_executable_ast_has_no_matrix_runner_mutation_surface(self):
        source = MODULE_PATH.read_text(encoding="utf-8")
        tree = ast.parse(source)
        imported_modules = {
            alias.name
            for node in ast.walk(tree)
            if isinstance(node, (ast.Import, ast.ImportFrom))
            for alias in node.names
        }
        self.assertNotIn("matrix_runner", imported_modules)
        executable_strings = {
            node.value
            for node in ast.walk(tree)
            if isinstance(node, ast.Constant) and isinstance(node.value, str)
        }
        self.assertNotIn("mark-result", executable_strings)
        self.assertNotIn("advance", executable_strings)
        self.assertNotIn("runner-state.json", executable_strings - {ast.get_docstring(tree)})

    def test_main_reports_wfl_009_as_browser_owned_automated_contract(self):
        with mock.patch("builtins.print") as printer:
            status = MODULE.main(["contract", "--case-id", "WFL-009"])
        self.assertEqual(status, 0)
        payload = json.loads(printer.call_args.args[0])
        self.assertTrue(payload["automated"])
        self.assertEqual(payload["required_evidence"], list(MODULE.workflow.REQUIRED_EVIDENCE["WFL-009"]))

    def test_main_distinguishes_assertion_failures_from_harness_rejections(self):
        with (
            mock.patch.object(
                MODULE,
                "case_contract",
                side_effect=MODULE.CaseError("workflow_assertion_failed", assertion_failure=True),
            ),
            mock.patch("builtins.print"),
        ):
            self.assertEqual(MODULE.main(["contract", "--case-id", "WFL-003"]), 1)
        with (
            mock.patch.object(
                MODULE,
                "case_contract",
                side_effect=MODULE.CaseError("workflow_harness_rejected"),
            ),
            mock.patch("builtins.print"),
        ):
            self.assertEqual(MODULE.main(["contract", "--case-id", "WFL-003"]), 2)
        with (
            mock.patch.object(MODULE, "case_contract", side_effect=RuntimeError("unsafe detail")),
            mock.patch("builtins.print") as printer,
        ):
            self.assertEqual(MODULE.main(["contract", "--case-id", "WFL-003"]), 2)
        self.assertNotIn("unsafe detail", str(printer.call_args))


if __name__ == "__main__":
    unittest.main()
