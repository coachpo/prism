#!/usr/bin/env python3
"""Local-only Playwright CLI workflow evidence helper for the Prism matrix.

The helper has two deliberately separate surfaces:

* the workflow owner imports the read-only session primitive for WFL-001,
  WFL-002, and WFL-010.  The primitive never acquires a runner cursor or starts
  services itself; ``run_workflow_cases.py`` owns the clone, mock, backend,
  frontend, browser receipt, deadline, and cleanup as one formal lifecycle.
* ``case-*`` commands keep a named CLI session alive while a human/agent drives
  one WFL-003..WFL-009 case.  They bind that session to a run-scoped disposable
  fixture, persist resumable checkpoints, package a redacted Playwright CLI
  trace, and validate the exact frozen evidence contract without making product
  mutations themselves.

Every browser action is restricted to the literal IPv4 loopback origin frozen
in its private fixture manifest and goes through the bundled playwright-cli
wrapper.  The historical direct ``readonly`` CLI entry point is intentionally
absent so a browser cannot outlive the owner receipt that authorizes it.
"""

from __future__ import annotations

import argparse
import base64
import contextlib
import datetime as dt
import fcntl
import hashlib
import json
import math
import os
import pathlib
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from typing import (
    Any,
    Callable,
    Dict,
    Iterable,
    List,
    Mapping,
    MutableMapping,
    NamedTuple,
    Optional,
    Sequence,
    Tuple,
)

import matrix_runner

DEFAULT_BASE_URL = "http://127.0.0.1:15174"
DEFAULT_WRAPPER = pathlib.Path(__file__).with_name("playwright_cli_matrix.sh").resolve()
PLAYWRIGHT_CLI_ROOT = pathlib.Path("/Users/qingli/.npm/_npx/9b853437c4cd15c0").resolve()
PLAYWRIGHT_CLI_ENTRY = PLAYWRIGHT_CLI_ROOT / "node_modules" / "@playwright" / "cli" / "playwright-cli.js"
PLAYWRIGHT_CLI_LOCK = PLAYWRIGHT_CLI_ROOT / "package-lock.json"
REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
RUN_ID = "20260813T204518Z"
RUN_ROOT = REPO_ROOT / "artifacts" / "evidence" / RUN_ID
RUN_PRIVATE = RUN_ROOT / "private"
MATRIX_PATH = RUN_ROOT / "matrix.json"
MATRIX_SHA256 = "c023da4d4d980094bd01957ec347421fb85622dfa1c95b354d482b0cbf7a95ff"
EXPECTED_BRANCH = "codex/local-test-matrix-20260813T204518Z"
EXPECTED_WORKTREE = pathlib.Path("/private/tmp/prism-local-test-matrix-20260813T204518Z")
CONFIG_FINGERPRINT = "sha256:22f30681ee8a97d3d320eee6e0e77d15234d19f43bb2323beaad62dc3696b955"
TEMPLATE_FINGERPRINT = "4d52589756aa700491ca26a8abcc8424"
SOURCE_DUMP_FINGERPRINT = "sha256:cc785fa1043a58216bf9a0cf97be9c891d9dbd4c594723cd4edb11fd3dcdb464"
ATTEMPT_RE = re.compile(r"^(?:primary|regression-[1-9][0-9]*)-attempt-[1-9][0-9]*$")
SHA40_RE = re.compile(r"^[0-9a-f]{40}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
CASE_DATABASE_PREFIX = "prism_matrix_20260813t204518z_case_"
PROTECTED_DATABASES = {"prism_matrix_gold", "prism_matrix_template", "prism_matrix_runtime"}
READONLY_CASES = ("WFL-001", "WFL-002", "WFL-010")
HELPER_CASES = tuple("WFL-%03d" % number for number in range(3, 10))
SENSITIVE_CASES = {"WFL-003", "WFL-004", "WFL-006", "WFL-008"}

CASE_CHECKPOINTS: Mapping[str, Tuple[str, ...]] = {
    "WFL-003": (
        "fixture_verified",
        "endpoint_created",
        "pricing_created",
        "ban_policy_created",
        "model_created",
        "targets_saved",
        "refresh_and_detail_verified",
        "dependency_protection_verified",
        "reverse_delete_completed",
        "cleanup_verified",
    ),
    "WFL-004": (
        "fixture_verified",
        "proxy_key_created",
        "sensitive_ui_cleared",
        "runtime_request_succeeded",
        "request_detail_verified",
        "audit_verified",
        "key_attribution_verified",
        "proxy_key_revoked",
        "cleanup_verified",
    ),
    "WFL-005": (
        "fixture_verified",
        "failure_injected",
        "failover_observed",
        "routing_health_verified",
        "event_detail_verified",
        "state_reset",
        "primary_recovered",
        "cleanup_verified",
    ),
    "WFL-006": (
        "fixture_verified",
        "disabled_mode_verified",
        "metadata_mode_verified",
        "body_mode_verified",
        "raw_download_verified",
        "secret_scan_passed",
        "settings_restored",
        "cleanup_verified",
    ),
    "WFL-007": (
        "fixture_verified",
        "pricing_verified",
        "usage_verified",
        "cost_recalculated",
        "endpoint_label_snapshot_verified",
        "currency_changed",
        "currency_refresh_verified",
        "currency_restored",
        "cleanup_verified",
    ),
    "WFL-008": (
        "fixture_verified",
        "auth_enabled",
        "unauthenticated_redirects_verified",
        "multi_tab_login_verified",
        "refresh_and_cross_tab_sync_verified",
        "proxy_key_rotated",
        "old_and_new_key_verified",
        "proxy_key_revoked",
        "revoked_key_verified",
        "logged_out",
        "session_storage_cleared",
        "auth_disabled",
        "open_shell_restored",
        "cleanup_verified",
    ),
    "WFL-009": (
        "fixture_verified",
        "preflight_verified",
        "wrong_confirmation_rejected",
        "correct_confirmation_accepted",
        "job_started",
        "job_cancelled",
        "job_restarted",
        "job_completed",
        "completion_status_refreshed",
        "retained_rows_verified",
        "cleanup_verified",
    ),
}

REQUIRED_EVIDENCE: Mapping[str, Tuple[str, ...]] = {
    "WFL-001": (
        "navigation-transcript.json",
        "snapshots-index.json",
        "redirect-grid.json",
        "browser-console.log",
        "trace.zip",
    ),
    "WFL-002": (
        "model-list.snapshot.txt",
        "model-detail-snapshots.json",
        "coverage-diagnostics.json",
        "trace.zip",
    ),
    "WFL-003": (
        "form-snapshots.json",
        "network-transcript.redacted.json",
        "created-resource-ids.json",
        "cleanup-proof.json",
        "trace.zip",
    ),
    "WFL-004": (
        "proxy-key-ui.snapshot.txt",
        "runtime-response.redacted.json",
        "request-detail.snapshot.txt",
        "audit.snapshot.txt",
        "attribution.json",
        "trace.zip",
    ),
    "WFL-005": (
        "incident-request.json",
        "routing-health.snapshot.txt",
        "event-detail.snapshot.txt",
        "reset-response.json",
        "recovery-proof.json",
        "trace.zip",
    ),
    "WFL-006": (
        "settings-snapshots.json",
        "audit-mode-details.json",
        "raw-downloads.redacted.json",
        "secret-scan.txt",
        "trace.zip",
    ),
    "WFL-007": (
        "pricing.snapshot.txt",
        "usage-detail.snapshot.txt",
        "cost-calculation.json",
        "currency-before-after.json",
        "trace.zip",
    ),
    "WFL-008": (
        "auth-transition.json",
        "multi-tab-snapshots.json",
        "key-rotation-grid.redacted.json",
        "session-storage-audit.json",
        "trace.zip",
    ),
    "WFL-009": (
        "preflight.snapshot.txt",
        "confirmation-validation.json",
        "job-timeline.json",
        "retention-result.json",
        "trace.zip",
    ),
    "WFL-010": (
        "viewport-snapshots.json",
        "state-grid.json",
        "accessibility-results.json",
        "browser-console.log",
        "trace.zip",
    ),
}

SNAPSHOT_INDEX_NAMES = {
    "form-snapshots.json",
    "settings-snapshots.json",
    "multi-tab-snapshots.json",
    "model-detail-snapshots.json",
    "viewport-snapshots.json",
}

FORBIDDEN_JSON_KEY_FRAGMENTS = (
    "authorization",
    "cookie",
    "credential",
    "password",
    "secret",
    "token",
    "apikey",
    "endpointkey",
    "header",
    "environment",
    "requestbody",
    "responsebody",
    "rawbody",
    "querycontext",
)

SAFE_TOKEN_METRIC_KEYS = {
    "cachecreationinputtokens",
    "cachereadinputtokens",
    "inputtokens",
    "outputtokens",
    "reasoningtokens",
    "totaltokens",
    "tokencount",
    "usageinputtokens",
    "usageoutputtokens",
}

MAX_TRACE_COMPONENT_BYTES = 64 * 1024 * 1024
MAX_TRACE_UNCOMPRESSED_BYTES = 256 * 1024 * 1024
MAX_TRACE_ARCHIVE_BYTES = 128 * 1024 * 1024

ANSI_RE = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")
SECRET_FIELD_PATTERN = (
    r"(?:preflight[_-]?token|query_context|authorization|cookie|set-cookie|"
    r"api[_-]?key|access[_-]?token|password|credential|endpoint[_-]?key|"
    r"proxy[_-]?key|refresh[_-]?token|secret(?:encryptionkey)?)"
)
SENSITIVE_HEADER_NAME_PATTERN = r"(?:authorization|cookie|set-cookie|x-api-key)"
SECRET_PATTERNS: Tuple[Tuple[re.Pattern[str], str], ...] = (
    (
        re.compile(r'(?i)("' + SECRET_FIELD_PATTERN + r'"\s*:\s*")([^"\r\n]*)(")'),
        r"\1[REDACTED]\3",
    ),
    (
        re.compile(r'(?i)(\\"' + SECRET_FIELD_PATTERN + r'\\"\s*:\s*\\")([^\\"\r\n]*)(\\")'),
        r"\1[REDACTED]\3",
    ),
    (
        re.compile(
            r'(?i)((?:"|\\")name(?:"|\\")\s*:\s*(?:"|\\")'
            + SENSITIVE_HEADER_NAME_PATTERN
            + r'(?:"|\\")\s*,\s*(?:"|\\")value(?:"|\\")\s*:\s*(?:"|\\"))'
            r'([^"\\\r\n]*)((?:"|\\"))'
        ),
        r"\1[REDACTED]\3",
    ),
    (
        re.compile(
            r'(?i)((?:"|\\")value(?:"|\\")\s*:\s*(?:"|\\"))'
            r'([^"\\\r\n]*)((?:"|\\")\s*,\s*(?:"|\\")name(?:"|\\")\s*:\s*'
            r'(?:"|\\")'
            + SENSITIVE_HEADER_NAME_PATTERN
            + r'(?:"|\\"))'
        ),
        r"\1[REDACTED]\3",
    ),
    (re.compile(r"(?i)((?:preflight[_-]?token|query_context|api[_-]?key|access[_-]?token)=)[^&\s\"'\\]+"), r"\1[REDACTED]"),
    (re.compile(r"(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{8,}"), r"\1[REDACTED]"),
    (re.compile(r"(?i)((?:authorization|cookie|set-cookie|api[_-]?key|access[_-]?token)\s*[:=]\s*)\S+"), r"\1[REDACTED]"),
    (re.compile(r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b"), "[REDACTED JWT]"),
    (re.compile(r"\bpm-[0-9a-fA-F]{32}\b"), "pm-[REDACTED]"),
    (re.compile(r"\bsk-[A-Za-z0-9_-]{10,}\b"), "sk-[REDACTED]"),
    (re.compile(r"(?i)postgres(?:ql)?://[^/\s:@]+:[^@\s]+@"), "postgresql://[REDACTED]@"),
    (re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----"), "[REDACTED PRIVATE MATERIAL]"),
)

REMAINING_SECRET_PATTERNS = (
    re.compile(
        r'(?i)(?:"|\\")'
        + SECRET_FIELD_PATTERN
        + r'(?:"|\\")\s*:\s*(?:"|\\")(?!\[REDACTED\])[^"\\\r\n]+'
    ),
    re.compile(
        r'(?i)(?:"|\\")name(?:"|\\")\s*:\s*(?:"|\\")'
        + SENSITIVE_HEADER_NAME_PATTERN
        + r'(?:"|\\")\s*,\s*(?:"|\\")value(?:"|\\")\s*:\s*'
        r'(?:"|\\")(?!\[REDACTED\])[^"\\\r\n]+'
    ),
    re.compile(
        r'(?i)(?:"|\\")value(?:"|\\")\s*:\s*(?:"|\\")(?!\[REDACTED\])'
        r'[^"\\\r\n]+(?:"|\\")\s*,\s*(?:"|\\")name(?:"|\\")\s*:\s*'
        r'(?:"|\\")'
        + SENSITIVE_HEADER_NAME_PATTERN
        + r'(?:"|\\")'
    ),
    re.compile(r'(?i)(?:preflight[_-]?token|query_context|api[_-]?key|access[_-]?token)=(?!\[REDACTED\])[^&\s"\']+'),
    re.compile(r"(?i)\bbearer\s+(?!\[REDACTED\])[A-Za-z0-9._~+/=-]{8,}"),
    re.compile(r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b"),
    re.compile(r"\bpm-[0-9a-fA-F]{32}\b"),
    re.compile(r"\bsk-[A-Za-z0-9_-]{10,}\b"),
    re.compile(r"(?i)postgres(?:ql)?://[^/\s:@]+:[^@\s]+@"),
    re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----"),
)

FATAL_TEXT_MARKERS = (
    "unexpected application error",
    "internal server error",
    "failed to load resource",
    "uncaught error",
    "chunkloaderror",
    "暂时无法确认登录状态",
    "受保护的数据已暂停显示",
    "无法确认当前会话是否仍然有效",
)


COVERAGE_LABELS = {
    "FULL": "\u5b8c\u6574\u8986\u76d6",
    "PARTIAL": "\u90e8\u5206\u8986\u76d6",
    "NONE": "\u4e0d\u517c\u5bb9",
}


class WorkflowError(RuntimeError):
    pass


class ProductAssertionError(WorkflowError):
    """A safe, expected product assertion that can be sealed as exit 1."""

    def __init__(self, code: str) -> None:
        if not re.fullmatch(r"[a-z][a-z0-9_]{2,95}", code):
            code = "readonly_product_assertion_failed"
        super().__init__(code)
        self.code = code


class FormalAttempt(NamedTuple):
    path: pathlib.Path
    result_id: str
    cycle: str
    number: int
    branch_head: str
    config_fingerprint: str
    template_fingerprint: str
    source_dump_fingerprint: str
    database_clone: str
    result_sha256: str
    control_sha256: Tuple[Tuple[str, str], ...]
    harness_sha256: Tuple[Tuple[str, str], ...]


class FormalPreparation(NamedTuple):
    case_id: str
    cycle: str
    branch_head: str
    control_sha256: Tuple[Tuple[str, str], ...]
    harness_sha256: Tuple[Tuple[str, str], ...]


class NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req: Any, fp: Any, code: int, msg: str, headers: Any, newurl: str) -> None:
        return None


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def frozen_workflow_contract() -> Mapping[str, Tuple[str, ...]]:
    if not MATRIX_PATH.is_file() or MATRIX_PATH.is_symlink():
        raise WorkflowError("frozen matrix is missing or unsafe")
    if sha256_file(MATRIX_PATH) != MATRIX_SHA256:
        raise WorkflowError("frozen matrix fingerprint changed")
    value = json.loads(MATRIX_PATH.read_text(encoding="utf-8"))
    if value.get("schema_version") != 1 or value.get("revision") != 2 or value.get("run_id") != RUN_ID:
        raise WorkflowError("frozen matrix identity changed")
    cases = value.get("cases")
    if not isinstance(cases, list):
        raise WorkflowError("frozen matrix cases are invalid")
    contract = {
        str(case.get("id")): tuple(case.get("required_evidence", ()))
        for case in cases
        if isinstance(case, Mapping) and str(case.get("id", "")).startswith("WFL-")
    }
    if contract != dict(REQUIRED_EVIDENCE):
        raise WorkflowError("workflow required_evidence no longer matches the frozen matrix")
    return contract


def validate_base_url(value: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if (
        parsed.scheme != "http"
        or parsed.hostname != "127.0.0.1"
        or parsed.port != 15174
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
    ):
        raise WorkflowError("base URL must be exactly http://127.0.0.1:15174")
    return DEFAULT_BASE_URL


def validate_loopback_origin(value: str) -> str:
    try:
        parsed = urllib.parse.urlsplit(value)
        port = parsed.port
    except ValueError as error:
        raise WorkflowError("browser origin is invalid") from error
    if (
        parsed.scheme != "http"
        or parsed.hostname != "127.0.0.1"
        or port is None
        or not 1024 <= port <= 65535
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in ("", "/")
        or parsed.query
        or parsed.fragment
    ):
        raise WorkflowError("browser origin must be a literal IPv4 loopback HTTP origin")
    return "http://127.0.0.1:%d" % port


def validate_route_path(value: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if (
        parsed.scheme
        or parsed.netloc
        or parsed.fragment
        or not parsed.path.startswith("/")
        or any(ord(character) < 32 for character in value)
    ):
        raise WorkflowError("route path must be a same-origin absolute path")
    return value


def validate_case_id(value: str, allowed: Iterable[str]) -> str:
    if value not in set(allowed):
        raise WorkflowError("unsupported case id: %s" % value)
    return value


def validate_attempt_dir(path: pathlib.Path, case_id: str) -> pathlib.Path:
    absolute = path.absolute()
    for candidate in (absolute, *absolute.parents):
        if candidate.exists() and candidate.is_symlink():
            raise WorkflowError("case directory path contains a symlink")
        if candidate == RUN_ROOT.parent.absolute():
            break
    resolved = path.resolve()
    try:
        relative = resolved.relative_to((RUN_ROOT / "cases" / case_id).resolve())
    except ValueError as error:
        raise WorkflowError("case directory is outside the frozen case root") from error
    if len(relative.parts) != 1 or not ATTEMPT_RE.fullmatch(relative.name):
        raise WorkflowError("case directory is not an exact runner attempt directory")
    return resolved


def path_components_safe(path: pathlib.Path, root: pathlib.Path) -> bool:
    candidate = path.absolute()
    anchor = root.absolute()
    try:
        parts = candidate.relative_to(anchor).parts
    except ValueError:
        return False
    if anchor.is_symlink() or not anchor.is_dir():
        return False
    current = anchor
    for index, part in enumerate(parts):
        current = current / part
        if current.is_symlink():
            return False
        if current.exists() and index < len(parts) - 1 and not current.is_dir():
            return False
    return True


def canonical_runner_timestamp(value: Any) -> bool:
    if not isinstance(value, str) or not re.fullmatch(
        r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z", value
    ):
        return False
    try:
        parsed = dt.datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError:
        return False
    return (
        parsed.tzinfo is not None
        and parsed.utcoffset() == dt.timedelta(0)
        and parsed.isoformat().replace("+00:00", "Z") == value
    )


def type_strict_equal(actual: Any, expected: Any) -> bool:
    if type(actual) is not type(expected):
        return False
    if isinstance(expected, dict):
        return set(actual) == set(expected) and all(
            type_strict_equal(actual[key], expected[key]) for key in expected
        )
    if isinstance(expected, list):
        return len(actual) == len(expected) and all(
            type_strict_equal(left, right)
            for left, right in zip(actual, expected)
        )
    return actual == expected


def read_formal_json(path: pathlib.Path, code: str) -> Dict[str, Any]:
    if path.is_symlink() or not path.is_file():
        raise WorkflowError(code)
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise WorkflowError(code) from error
    if not isinstance(value, dict):
        raise WorkflowError(code)
    return value


def formal_git_facts() -> Tuple[str, str, str]:
    environment = {
        key: value
        for key, value in os.environ.items()
        if key in {"HOME", "LANG", "LC_ALL", "LC_CTYPE", "TZ"}
    }
    environment.update(
        {
            "PATH": "/usr/bin:/bin",
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_CONFIG_GLOBAL": "/dev/null",
            "GIT_CONFIG_SYSTEM": "/dev/null",
            "GIT_OPTIONAL_LOCKS": "0",
            "GIT_TERMINAL_PROMPT": "0",
        }
    )

    def run(*arguments: str) -> str:
        try:
            completed = subprocess.run(
                ["/usr/bin/git", *arguments],
                cwd=str(REPO_ROOT),
                env=environment,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                text=True,
                timeout=10,
                check=False,
            )
        except (OSError, subprocess.SubprocessError) as error:
            raise WorkflowError("formal git fence is unavailable") from error
        if completed.returncode != 0:
            raise WorkflowError("formal git fence rejected the worktree")
        return completed.stdout.strip()

    head = run("rev-parse", "HEAD")
    branch = run("rev-parse", "--abbrev-ref", "HEAD")
    status = run("status", "--porcelain=v2", "--untracked-files=all")
    if not SHA40_RE.fullmatch(head) or branch == "HEAD" or not branch:
        raise WorkflowError("formal git identity is invalid")
    return head, branch, status


def formal_harness_hashes(
    additional_harness_files: Optional[Mapping[str, pathlib.Path]] = None,
) -> Tuple[Tuple[str, str], ...]:
    paths: Dict[str, pathlib.Path] = {
        "workflow_helper": pathlib.Path(__file__).resolve(),
        "matrix_runner": pathlib.Path(matrix_runner.__file__).resolve(),
        "playwright_wrapper": DEFAULT_WRAPPER,
        "playwright_cli_entry": PLAYWRIGHT_CLI_ENTRY,
        "playwright_cli_lock": PLAYWRIGHT_CLI_LOCK,
    }
    for name, path in (additional_harness_files or {}).items():
        if not re.fullmatch(r"[a-z][a-z0-9_]{2,63}", name) or name in paths:
            raise WorkflowError("formal harness inventory is invalid")
        paths[name] = pathlib.Path(path).resolve()
    if any(path.is_symlink() or not path.is_file() for path in paths.values()):
        raise WorkflowError("formal harness input is missing or unsafe")
    return tuple(sorted((name, sha256_file(path)) for name, path in paths.items()))


def validate_runner_allocation(
    attempt: pathlib.Path,
    result: Mapping[str, Any],
) -> None:
    try:
        context = matrix_runner.RunContext(RUN_ROOT)
        grouped, warnings = matrix_runner.scan_results(context)
        cycle = str(result.get("cycle"))
        case_id = str(result.get("case_id"))
        latest = matrix_runner.latest_result(grouped, cycle, case_id)
        events = matrix_runner.read_events(context.events_path)
        missing_events = matrix_runner.missing_result_events(grouped, context.events_path)
        missing_corrections = matrix_runner.missing_correction_events(grouped, context.events_path)
    except (OSError, UnicodeError, json.JSONDecodeError, matrix_runner.RunnerError) as error:
        raise WorkflowError("formal runner allocation is invalid") from error
    if (
        warnings
        or not isinstance(latest, dict)
        or latest.get("result_id") != result.get("result_id")
        or latest.get("status") != "running"
        or matrix_runner.cycle_cursor(context, grouped, cycle) != case_id
        or missing_events
        or missing_corrections
        or context.result_path(cycle, case_id, int(result.get("attempt", 0))).absolute()
        != attempt.absolute() / "result.json"
    ):
        raise WorkflowError("formal runner allocation is invalid")
    result_events = [
        event
        for event in events
        if isinstance(event, dict) and event.get("result_id") == result.get("result_id")
    ]
    started_events = [
        event
        for event in result_events
        if set(event)
        == {
            "schema_version",
            "event_id",
            "at",
            "type",
            "result_id",
            "cycle",
            "case_id",
            "attempt",
        }
        and type(event.get("schema_version")) is int
        and event.get("schema_version") == matrix_runner.SCHEMA_VERSION
        and event.get("type") == "case_started"
        and event.get("cycle") == cycle
        and event.get("case_id") == case_id
        and type(event.get("attempt")) is int
        and event.get("attempt") == result.get("attempt")
        and canonical_runner_timestamp(event.get("at"))
    ]
    try:
        if started_events:
            matrix_runner.validate_uuid_text(started_events[0].get("event_id"), "event_id")
    except matrix_runner.RunnerError as error:
        raise WorkflowError("formal runner start event is invalid") from error
    if len(result_events) != 1 or len(started_events) != 1:
        raise WorkflowError("formal runner start event is invalid")
    checkpoint = read_formal_json(context.checkpoint_path, "formal runner checkpoint is invalid")
    cycles = checkpoint.get("cycles")
    cycle_checkpoint = cycles.get(cycle) if isinstance(cycles, dict) else None
    counts = cycle_checkpoint.get("counts") if isinstance(cycle_checkpoint, dict) else None
    try:
        expected_checkpoint = matrix_runner.build_checkpoint(context, grouped, cycle)
    except matrix_runner.RunnerError as error:
        raise WorkflowError("formal runner checkpoint is invalid") from error
    durable_keys = {
        "schema_version",
        "matrix_sha256",
        "manifest_fingerprints",
        "active_cycle",
        "overall_state",
        "cycles",
    }
    if (
        set(checkpoint) != durable_keys | {"updated_at"}
        or not canonical_runner_timestamp(checkpoint.get("updated_at"))
        or not type_strict_equal(
            {key: checkpoint.get(key) for key in durable_keys},
            {key: expected_checkpoint.get(key) for key in durable_keys},
        )
        or checkpoint.get("matrix_sha256") != context.matrix_hash
        or checkpoint.get("active_cycle") != cycle
        or not isinstance(cycle_checkpoint, dict)
        or cycle_checkpoint.get("next_case_id") != case_id
        or not isinstance(counts, dict)
        or type(counts.get("running")) is not int
        or counts.get("running") != 1
    ):
        raise WorkflowError("formal runner checkpoint is invalid")


def validate_formal_attempt(
    case_id: str,
    attempt_dir: pathlib.Path,
    *,
    require_only_result: bool,
    additional_harness_files: Optional[Mapping[str, pathlib.Path]] = None,
) -> FormalAttempt:
    validate_case_id(case_id, REQUIRED_EVIDENCE)
    if REPO_ROOT.absolute() != EXPECTED_WORKTREE or not path_components_safe(RUN_ROOT, REPO_ROOT):
        raise WorkflowError("formal worktree identity changed")
    attempt = validate_attempt_dir(attempt_dir, case_id)
    if attempt.is_symlink() or not attempt.is_dir() or not path_components_safe(attempt, RUN_ROOT):
        raise WorkflowError("formal attempt path is unsafe")
    matrix = read_formal_json(MATRIX_PATH, "formal matrix is unreadable")
    manifest = read_formal_json(RUN_ROOT / "manifest.json", "formal manifest is unreadable")
    runner_manifest = read_formal_json(
        RUN_ROOT / "runner-manifest.json", "formal runner manifest is unreadable"
    )
    result_path = attempt / "result.json"
    result = read_formal_json(result_path, "formal running result is unreadable")
    if sha256_file(MATRIX_PATH) != MATRIX_SHA256:
        raise WorkflowError("formal matrix fingerprint changed")
    selected = next(
        (
            item
            for item in matrix.get("cases", [])
            if isinstance(item, dict) and item.get("id") == case_id
        ),
        None,
    )
    repository = manifest.get("repository")
    matrix_contract = manifest.get("matrix")
    runner_fingerprints = runner_manifest.get("fingerprints")
    if (
        matrix.get("schema_version") != 1
        or matrix.get("revision") != 2
        or matrix.get("run_id") != RUN_ID
        or not isinstance(selected, dict)
        or tuple(selected.get("required_evidence", ())) != REQUIRED_EVIDENCE[case_id]
        or type(selected.get("timeout")) is not int
        or int(selected.get("timeout", 0)) <= 0
        or manifest.get("run_id") != RUN_ID
        or not isinstance(repository, dict)
        or repository.get("branch") != EXPECTED_BRANCH
        or repository.get("worktree") != str(EXPECTED_WORKTREE)
        or not isinstance(matrix_contract, dict)
        or matrix_contract.get("sha256") != MATRIX_SHA256
        or runner_manifest.get("run_id") != RUN_ID
        or runner_manifest.get("matrix_sha256") != MATRIX_SHA256
        or not isinstance(runner_fingerprints, dict)
        or runner_fingerprints.get("config") != CONFIG_FINGERPRINT
        or runner_fingerprints.get("database_clone") != TEMPLATE_FINGERPRINT
        or runner_fingerprints.get("source_dump") != SOURCE_DUMP_FINGERPRINT
    ):
        raise WorkflowError("formal run manifest identity changed")
    expected_result_keys = {
        "schema_version",
        "result_id",
        "matrix_sha256",
        "cycle",
        "case_id",
        "attempt",
        "status",
        "started_at",
        "finished_at",
        "fingerprints",
        "database_clone",
        "retry_context",
    }
    retry_context = result.get("retry_context")
    fingerprints = result.get("fingerprints")
    if (
        set(result) != expected_result_keys
        or type(result.get("schema_version")) is not int
        or result.get("schema_version") != matrix_runner.SCHEMA_VERSION
        or result.get("matrix_sha256") != MATRIX_SHA256
        or result.get("case_id") != case_id
        or result.get("status") != "running"
        or result.get("finished_at") is not None
        or not canonical_runner_timestamp(result.get("started_at"))
        or type(result.get("attempt")) is not int
        or int(result.get("attempt", 0)) < 1
        or not isinstance(retry_context, dict)
        or set(retry_context) != {"defect_id", "validation_case_ids"}
        or not isinstance(retry_context.get("validation_case_ids"), list)
        or retry_context.get("validation_case_ids")
        != sorted(set(retry_context.get("validation_case_ids", [])))
        or any(
            not isinstance(value, str) or not matrix_runner.CASE_ID_RE.fullmatch(value)
            for value in retry_context.get("validation_case_ids", [])
        )
        or not isinstance(fingerprints, dict)
        or set(fingerprints)
        != {"branch_head", "config", "database_template", "source_dump"}
    ):
        raise WorkflowError("formal running result schema is invalid")
    try:
        matrix_runner.validate_safe_code(retry_context.get("defect_id"), "defect id")
        matrix_runner.validate_uuid_text(result.get("result_id"), "result_id")
        cycle, _ = matrix_runner.validate_cycle(str(result.get("cycle")))
        matrix_runner.assert_no_secret_material(result, "workflow_running_result")
    except matrix_runner.RunnerError as error:
        raise WorkflowError("formal running result schema is invalid") from error
    expected_cycle, attempt_text = attempt.name.rsplit("-attempt-", 1)
    branch_head = fingerprints.get("branch_head")
    database_clone = result.get("database_clone")
    if (
        cycle != expected_cycle
        or result.get("attempt") != int(attempt_text)
        or not isinstance(branch_head, str)
        or not SHA40_RE.fullmatch(branch_head)
        or fingerprints.get("config") != CONFIG_FINGERPRINT
        or fingerprints.get("database_template") != TEMPLATE_FINGERPRINT
        or fingerprints.get("source_dump") != SOURCE_DUMP_FINGERPRINT
        or not isinstance(database_clone, str)
        or not SHA256_RE.fullmatch(database_clone)
    ):
        raise WorkflowError("formal attempt fingerprint mismatch")
    running_paths = []
    for candidate_path in sorted((RUN_ROOT / "cases").glob("*/*/result.json")):
        if not path_components_safe(candidate_path, RUN_ROOT):
            raise WorkflowError("formal result inventory is unsafe")
        candidate = read_formal_json(candidate_path, "formal result inventory is invalid")
        if candidate.get("status") == "running":
            running_paths.append(candidate_path.absolute())
    if running_paths != [result_path.absolute()]:
        raise WorkflowError("formal running attempt is not unique")
    siblings = [
        int(path.parent.name.rsplit("-attempt-", 1)[1])
        for path in attempt.parent.glob("%s-attempt-*/result.json" % cycle)
        if path_components_safe(path, RUN_ROOT) and ATTEMPT_RE.fullmatch(path.parent.name)
    ]
    if not siblings or result.get("attempt") != max(siblings):
        raise WorkflowError("formal attempt is not latest")
    if require_only_result:
        entries = list(attempt.iterdir())
        if len(entries) != 1 or entries[0].name != "result.json" or entries[0].is_symlink():
            raise WorkflowError("formal attempt is not result-only")
    head, branch, status = formal_git_facts()
    if head != branch_head or branch != EXPECTED_BRANCH or status:
        raise WorkflowError("formal HEAD or worktree fence mismatch")
    validate_runner_allocation(attempt, result)
    control_paths = {
        "matrix": MATRIX_PATH,
        "manifest": RUN_ROOT / "manifest.json",
        "runner_manifest": RUN_ROOT / "runner-manifest.json",
        "checkpoint": RUN_ROOT / "checkpoint.json",
        "events": RUN_ROOT / "events.jsonl",
        "running_result": result_path,
    }
    if any(path.is_symlink() or not path.is_file() for path in control_paths.values()):
        raise WorkflowError("formal runner control file is unsafe")
    return FormalAttempt(
        path=attempt,
        result_id=str(result.get("result_id")),
        cycle=cycle,
        number=int(result.get("attempt")),
        branch_head=branch_head,
        config_fingerprint=str(fingerprints.get("config")),
        template_fingerprint=str(fingerprints.get("database_template")),
        source_dump_fingerprint=str(fingerprints.get("source_dump")),
        database_clone=database_clone,
        result_sha256=sha256_file(result_path),
        control_sha256=tuple(sorted((name, sha256_file(path)) for name, path in control_paths.items())),
        harness_sha256=formal_harness_hashes(additional_harness_files),
    )


def validate_formal_preparation(
    case_id: str,
    *,
    additional_harness_files: Optional[Mapping[str, pathlib.Path]] = None,
) -> FormalPreparation:
    validate_case_id(case_id, REQUIRED_EVIDENCE)
    if REPO_ROOT.absolute() != EXPECTED_WORKTREE or not path_components_safe(RUN_ROOT, REPO_ROOT):
        raise WorkflowError("formal worktree identity changed")
    frozen_workflow_contract()
    manifest = read_formal_json(RUN_ROOT / "manifest.json", "formal manifest is unreadable")
    runner_manifest = read_formal_json(
        RUN_ROOT / "runner-manifest.json", "formal runner manifest is unreadable"
    )
    repository = manifest.get("repository")
    matrix_contract = manifest.get("matrix")
    fingerprints = runner_manifest.get("fingerprints")
    if (
        manifest.get("run_id") != RUN_ID
        or not isinstance(repository, dict)
        or repository.get("branch") != EXPECTED_BRANCH
        or repository.get("worktree") != str(EXPECTED_WORKTREE)
        or not isinstance(matrix_contract, dict)
        or matrix_contract.get("sha256") != MATRIX_SHA256
        or runner_manifest.get("run_id") != RUN_ID
        or runner_manifest.get("matrix_sha256") != MATRIX_SHA256
        or not isinstance(fingerprints, dict)
        or fingerprints.get("config") != CONFIG_FINGERPRINT
        or fingerprints.get("database_clone") != TEMPLATE_FINGERPRINT
        or fingerprints.get("source_dump") != SOURCE_DUMP_FINGERPRINT
    ):
        raise WorkflowError("formal run manifest identity changed")
    head, branch, status = formal_git_facts()
    if branch != EXPECTED_BRANCH or status:
        raise WorkflowError("formal HEAD or worktree fence mismatch")
    try:
        context = matrix_runner.RunContext(RUN_ROOT)
        grouped, warnings = matrix_runner.scan_results(context)
        missing_events = matrix_runner.missing_result_events(grouped, context.events_path)
        missing_corrections = matrix_runner.missing_correction_events(grouped, context.events_path)
    except (OSError, UnicodeError, json.JSONDecodeError, matrix_runner.RunnerError) as error:
        raise WorkflowError("formal prepare cursor is invalid") from error
    running = [
        result
        for by_case in grouped.values()
        for attempts in by_case.values()
        for result in attempts
        if isinstance(result, dict) and result.get("status") == "running"
    ]
    checkpoint = read_formal_json(context.checkpoint_path, "formal runner checkpoint is invalid")
    cycle = checkpoint.get("active_cycle")
    if not isinstance(cycle, str):
        raise WorkflowError("formal prepare cursor is invalid")
    try:
        expected_checkpoint = matrix_runner.build_checkpoint(context, grouped, cycle)
        cursor = matrix_runner.cycle_cursor(context, grouped, cycle)
    except matrix_runner.RunnerError as error:
        raise WorkflowError("formal prepare cursor is invalid") from error
    durable_keys = {
        "schema_version",
        "matrix_sha256",
        "manifest_fingerprints",
        "active_cycle",
        "overall_state",
        "cycles",
    }
    cycle_state = checkpoint.get("cycles", {}).get(cycle) if isinstance(checkpoint.get("cycles"), dict) else None
    counts = cycle_state.get("counts") if isinstance(cycle_state, dict) else None
    if (
        warnings
        or missing_events
        or missing_corrections
        or running
        or cursor != case_id
        or set(checkpoint) != durable_keys | {"updated_at"}
        or not canonical_runner_timestamp(checkpoint.get("updated_at"))
        or not type_strict_equal(
            {key: checkpoint.get(key) for key in durable_keys},
            {key: expected_checkpoint.get(key) for key in durable_keys},
        )
        or not isinstance(cycle_state, dict)
        or cycle_state.get("next_case_id") != case_id
        or not isinstance(counts, dict)
        or type(counts.get("running")) is not int
        or counts.get("running") != 0
    ):
        raise WorkflowError("formal prepare cursor is invalid")
    controls = {
        "matrix": MATRIX_PATH,
        "manifest": RUN_ROOT / "manifest.json",
        "runner_manifest": RUN_ROOT / "runner-manifest.json",
        "checkpoint": RUN_ROOT / "checkpoint.json",
        "events": RUN_ROOT / "events.jsonl",
    }
    if any(path.is_symlink() or not path.is_file() for path in controls.values()):
        raise WorkflowError("formal runner control file is unsafe")
    return FormalPreparation(
        case_id=case_id,
        cycle=cycle,
        branch_head=head,
        control_sha256=tuple(sorted((name, sha256_file(path)) for name, path in controls.items())),
        harness_sha256=formal_harness_hashes(additional_harness_files),
    )


def open_runner_lock() -> Any:
    path = RUN_ROOT / ".runner.lock"
    if not path_components_safe(path, RUN_ROOT):
        raise WorkflowError("formal runner lock path is unsafe")
    nofollow = getattr(os, "O_NOFOLLOW", 0)
    if not nofollow:
        raise WorkflowError("formal runner lock cannot reject symlinks")
    flags = os.O_RDWR | os.O_CREAT | nofollow
    if hasattr(os, "O_CLOEXEC"):
        flags |= os.O_CLOEXEC
    try:
        descriptor = os.open(str(path), flags, 0o600)
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise WorkflowError("formal runner lock is not regular")
        os.fchmod(descriptor, 0o600)
        return os.fdopen(descriptor, "r+b")
    except Exception as error:
        if "descriptor" in locals():
            with contextlib.suppress(OSError):
                os.close(descriptor)
        if isinstance(error, WorkflowError):
            raise
        raise WorkflowError("formal runner lock could not be opened") from error


@contextlib.contextmanager
def formal_attempt_lease(
    case_id: str,
    attempt_dir: pathlib.Path,
    *,
    additional_harness_files: Optional[Mapping[str, pathlib.Path]] = None,
) -> Iterable[FormalAttempt]:
    stream = open_runner_lock()
    try:
        try:
            fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise WorkflowError("matrix runner is already active") from error
        before = validate_formal_attempt(
            case_id,
            attempt_dir,
            require_only_result=True,
            additional_harness_files=additional_harness_files,
        )
        try:
            yield before
        finally:
            after = validate_formal_attempt(
                case_id,
                attempt_dir,
                require_only_result=False,
                additional_harness_files=additional_harness_files,
            )
            if before != after:
                raise WorkflowError("formal attempt or runner controls changed during execution")
    finally:
        with contextlib.suppress(OSError):
            fcntl.flock(stream.fileno(), fcntl.LOCK_UN)
        stream.close()


@contextlib.contextmanager
def formal_preparation_lease(
    case_id: str,
    *,
    additional_harness_files: Optional[Mapping[str, pathlib.Path]] = None,
) -> Iterable[FormalPreparation]:
    stream = open_runner_lock()
    try:
        try:
            fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise WorkflowError("matrix runner is already active") from error
        before = validate_formal_preparation(
            case_id,
            additional_harness_files=additional_harness_files,
        )
        try:
            yield before
        finally:
            after = validate_formal_preparation(
                case_id,
                additional_harness_files=additional_harness_files,
            )
            if before != after:
                raise WorkflowError("formal runner controls changed during preparation")
    finally:
        with contextlib.suppress(OSError):
            fcntl.flock(stream.fileno(), fcntl.LOCK_UN)
        stream.close()


def validate_private_path(path: pathlib.Path, *, require_file: bool = True) -> pathlib.Path:
    absolute = path.absolute()
    for candidate in (absolute, *absolute.parents):
        if candidate.exists() and candidate.is_symlink():
            raise WorkflowError("private input path contains a symlink")
        if candidate == RUN_PRIVATE.parent.absolute():
            break
    resolved = path.resolve()
    try:
        resolved.relative_to(RUN_PRIVATE.resolve())
    except ValueError as error:
        raise WorkflowError("private input must stay under the run private directory") from error
    if require_file and not resolved.is_file():
        raise WorkflowError("private input is missing or is a symlink")
    if require_file and resolved.stat().st_mode & 0o077:
        raise WorkflowError("private input permissions must be 0600 or stricter")
    return resolved


def load_fixture_manifest(path: pathlib.Path, case_id: str, base_url: str) -> Dict[str, Any]:
    source = validate_private_path(path)
    if source.stat().st_size > 64 * 1024:
        raise WorkflowError("fixture manifest is too large")
    value = json.loads(source.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise WorkflowError("fixture manifest must be a JSON object")
    assert_safe_json(value, "fixture manifest")
    expected_database = CASE_DATABASE_PREFIX + case_id.lower().replace("-", "_")
    database = value.get("database_clone")
    if database in PROTECTED_DATABASES or database != expected_database:
        raise WorkflowError("fixture manifest does not name the exact disposable case clone")
    identity = value.get("database_clone_identity")
    if not isinstance(identity, str) or not re.fullmatch(r"[0-9a-f]{64}", identity):
        raise WorkflowError("fixture manifest database identity is invalid")
    frontend_origin = validate_loopback_origin(str(value.get("frontend_origin", "")))
    backend_origin = validate_loopback_origin(str(value.get("backend_origin", "")))
    mock_origins = value.get("mock_origins", [])
    if not isinstance(mock_origins, list) or any(not isinstance(item, str) for item in mock_origins):
        raise WorkflowError("fixture manifest mock origins are invalid")
    normalized_mocks = [validate_loopback_origin(item) for item in mock_origins]
    all_origins = [frontend_origin, backend_origin, *normalized_mocks]
    if len(all_origins) != len(set(all_origins)):
        raise WorkflowError("fixture manifest origins must be distinct")
    if case_id != "WFL-009" and not normalized_mocks:
        raise WorkflowError("this workflow fixture requires a loopback mock origin")
    if (
        value.get("schema_version") != 1
        or value.get("run_id") != RUN_ID
        or value.get("case_id") != case_id
        or value.get("fixture_scope") != "case"
        or value.get("disposable") is not True
        or frontend_origin != validate_loopback_origin(base_url)
    ):
        raise WorkflowError("fixture manifest identity or isolation contract is invalid")
    return {
        "path": str(source),
        "sha256": sha256_file(source),
        "database_clone": database,
        "database_clone_identity": identity,
        "frontend_origin": frontend_origin,
        "backend_origin": backend_origin,
        "mock_origins": normalized_mocks,
    }


def load_private_values(path: Optional[pathlib.Path]) -> Tuple[str, ...]:
    if path is None:
        return ()
    source = validate_private_path(path)
    if source.stat().st_size > 64 * 1024:
        raise WorkflowError("private redaction input is too large")
    value = json.loads(source.read_text(encoding="utf-8"))
    if not isinstance(value, list) or not 1 <= len(value) <= 64:
        raise WorkflowError("private redaction input must be a bounded JSON string array")
    result = []
    for item in value:
        if not isinstance(item, str) or not 4 <= len(item.encode("utf-8")) <= 4096:
            raise WorkflowError("private redaction input contains an invalid value")
        result.append(item)
    return tuple(result)


def private_value_variants(values: Sequence[str]) -> Tuple[str, ...]:
    variants = set()
    for value in values:
        raw = value.encode("utf-8")
        variants.update(
            {
                value,
                json.dumps(value, ensure_ascii=False)[1:-1],
                urllib.parse.quote(value, safe=""),
                urllib.parse.quote_plus(value, safe=""),
                base64.b64encode(raw).decode("ascii"),
                base64.urlsafe_b64encode(raw).decode("ascii").rstrip("="),
            }
        )
    return tuple(sorted((item for item in variants if len(item) >= 4), key=len, reverse=True))


def ensure_private_directory(path: pathlib.Path) -> None:
    path.mkdir(parents=True, exist_ok=True)
    os.chmod(path, 0o700)


def atomic_write(path: pathlib.Path, data: bytes) -> None:
    ensure_private_directory(path.parent)
    descriptor, temporary_name = tempfile.mkstemp(prefix=".%s." % path.name, dir=str(path.parent))
    temporary = pathlib.Path(temporary_name)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(str(temporary), str(path))
        os.chmod(path, 0o600)
    except Exception:
        with contextlib.suppress(FileNotFoundError):
            temporary.unlink()
        raise


def write_text(path: pathlib.Path, text: str) -> None:
    atomic_write(path, text.encode("utf-8"))


def write_json(
    path: pathlib.Path,
    value: Any,
    private_values: Sequence[str] = (),
) -> None:
    assert_safe_json(value, "evidence", private_values)
    atomic_write(path, (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode("utf-8"))


def sha256_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        while True:
            chunk = handle.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
    return digest.hexdigest()


def redact_text(value: str, private_values: Sequence[str] = ()) -> str:
    result = ANSI_RE.sub("", value).replace("\x00", "")
    for variant in private_value_variants(private_values):
        result = result.replace(variant, "[REDACTED LOCAL VALUE]")
    for pattern, replacement in SECRET_PATTERNS:
        result = pattern.sub(replacement, result)
    return result


def redacted_trace_bytes(
    path: pathlib.Path,
    private_values: Sequence[str] = (),
    *,
    require_text: bool = False,
) -> Optional[bytes]:
    """Redact a textual trace payload; omit unverifiable binary resources."""
    payload = path.read_bytes()
    if len(payload) > MAX_TRACE_COMPONENT_BYTES:
        raise WorkflowError("trace component is too large: %s" % path.name)
    try:
        text = payload.decode("utf-8")
    except UnicodeDecodeError:
        assert_no_remaining_secret_bytes(payload, "binary trace resource %s" % path.name, private_values)
        if require_text:
            raise WorkflowError("trace metadata is not UTF-8 text: %s" % path.name)
        return None
    redacted = redact_text(text, private_values)
    assert_no_remaining_secret(redacted, "trace payload %s" % path.name, private_values)
    return redacted.encode("utf-8")


def assert_no_remaining_secret_bytes(
    value: bytes,
    location: str,
    private_values: Sequence[str] = (),
) -> None:
    for variant in private_value_variants(private_values):
        if variant.encode("utf-8") in value:
            raise WorkflowError("%s still contains a private local value" % location)
    latin = value.decode("latin-1")
    for pattern in REMAINING_SECRET_PATTERNS:
        if pattern.search(latin):
            raise WorkflowError("%s still appears to contain secret material" % location)


def assert_no_remaining_secret(
    value: str,
    location: str,
    private_values: Sequence[str] = (),
) -> None:
    for variant in private_value_variants(private_values):
        if variant in value:
            raise WorkflowError("%s still contains a private local value" % location)
    for pattern in REMAINING_SECRET_PATTERNS:
        if pattern.search(value):
            raise WorkflowError("%s still appears to contain secret material" % location)


def is_forbidden_json_key(key: Any) -> bool:
    compact = normalized_json_key(key)
    if compact in SAFE_TOKEN_METRIC_KEYS:
        return False
    return any(fragment in compact for fragment in FORBIDDEN_JSON_KEY_FRAGMENTS)


def normalized_json_key(key: Any) -> str:
    return re.sub(r"[^a-z0-9]", "", str(key).lower())


def assert_safe_json(
    value: Any,
    location: str,
    private_values: Sequence[str] = (),
) -> None:
    if isinstance(value, Mapping):
        for key, item in value.items():
            assert_no_remaining_secret(str(key), "%s.<key>" % location, private_values)
            if is_forbidden_json_key(key):
                raise WorkflowError("%s contains forbidden evidence field %r" % (location, key))
            assert_safe_json(item, "%s.%s" % (location, key), private_values)
    elif isinstance(value, (list, tuple)):
        for index, item in enumerate(value):
            assert_safe_json(item, "%s[%d]" % (location, index), private_values)
    elif isinstance(value, str):
        # Evidence is rejected if the original value needs redaction.  Scanning
        # a redacted copy here would prove only that the sanitizer works while
        # still allowing the unmodified secret to be written.
        assert_no_remaining_secret(value, location, private_values)
    elif isinstance(value, float) and not math.isfinite(value):
        raise WorkflowError("%s contains a non-finite JSON number" % location)


def safe_json_projection(value: Any) -> Any:
    """Project API data into evidence-safe JSON without preserving secret fields."""
    if isinstance(value, Mapping):
        result: Dict[str, Any] = {}
        for key, item in value.items():
            if is_forbidden_json_key(key):
                continue
            result[str(key)] = safe_json_projection(item)
        return result
    if isinstance(value, list):
        return [safe_json_projection(item) for item in value]
    if isinstance(value, str):
        return redact_text(value)
    if value is None or isinstance(value, (bool, int, float)):
        return value
    return redact_text(str(value))


def body_has_fatal_marker(text: str) -> List[str]:
    lowered = text.lower()
    return [marker for marker in FATAL_TEXT_MARKERS if marker in lowered]


def unexpected_console_fatal_lines(text: str, allowed_fragments: Sequence[str] = ()) -> List[str]:
    """Return fatal console lines after removing narrowly expected fixture noise."""
    findings = []
    lowered_allowed = tuple(fragment.lower() for fragment in allowed_fragments)
    for line in text.splitlines():
        lowered = line.lower()
        if not any(marker in lowered for marker in FATAL_TEXT_MARKERS):
            continue
        if any(fragment in lowered for fragment in lowered_allowed):
            continue
        findings.append(line.strip())
    return findings


def snapshot_ref_for_name(snapshot_text: str, role: str, names: Sequence[str]) -> str:
    """Resolve a fresh Playwright snapshot ref for an exact accessible name."""
    matches = re.findall(
        r'\b%s\s+"([^"]+)"\s+\[ref=([A-Za-z0-9_-]+)\]' % re.escape(role),
        snapshot_text,
    )
    for wanted in names:
        for actual, reference in matches:
            if actual.strip() == wanted:
                return reference
    raise WorkflowError("fresh snapshot has no %s named %s" % (role, "/".join(names)))


def parse_page_metadata(output: str) -> Tuple[Optional[str], Optional[str]]:
    url_match = re.search(r"^- Page URL:\s*(.+?)\s*$", output, re.MULTILINE)
    title_match = re.search(r"^- Page Title:\s*(.+?)\s*$", output, re.MULTILINE)
    return (url_match.group(1) if url_match else None, title_match.group(1) if title_match else None)


def parse_eval_result(output: str) -> Any:
    marker = "### Result"
    if marker not in output:
        raise WorkflowError("playwright eval did not return a result")
    payload = output.split(marker, 1)[1]
    payload = payload.split("\n### ", 1)[0].strip()
    try:
        decoded = json.loads(payload)
    except json.JSONDecodeError as error:
        raise WorkflowError("playwright eval result is not JSON: %s" % error)
    if isinstance(decoded, str):
        with contextlib.suppress(json.JSONDecodeError):
            return json.loads(decoded)
    return decoded


def inventory(directory: pathlib.Path) -> List[Dict[str, Any]]:
    items = []
    for path in sorted(directory.rglob("*")):
        if path.is_symlink():
            raise WorkflowError("evidence inventory contains a symlink")
        if path.is_file():
            items.append(
                {
                    "path": str(path.relative_to(directory)),
                    "bytes": path.stat().st_size,
                    "sha256": sha256_file(path),
                }
            )
    return items


def discover_local_chromium() -> Optional[pathlib.Path]:
    """Reuse the newest already-installed full Chromium build, without download."""
    configured = os.environ.get("PLAYWRIGHT_BROWSERS_PATH")
    root = pathlib.Path(configured).expanduser() if configured else pathlib.Path.home() / "Library" / "Caches" / "ms-playwright"
    candidates = []
    if root.is_dir():
        for browser_dir in root.glob("chromium-[0-9]*"):
            match = re.search(r"chromium-(\d+)$", browser_dir.name)
            revision = int(match.group(1)) if match else 0
            for executable in browser_dir.glob(
                "chrome-mac-*/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"
            ):
                if executable.is_file() and os.access(executable, os.X_OK):
                    candidates.append((revision, executable.resolve()))
    return max(candidates, default=(0, None), key=lambda item: item[0])[1]


def validate_chromium_executable(path: pathlib.Path) -> pathlib.Path:
    path = path.resolve()
    configured = os.environ.get("PLAYWRIGHT_BROWSERS_PATH")
    cache_root = (
        pathlib.Path(configured).expanduser().resolve()
        if configured
        else (pathlib.Path.home() / "Library" / "Caches" / "ms-playwright").resolve()
    )
    try:
        path.relative_to(cache_root)
    except ValueError as exc:
        raise WorkflowError("Chromium executable escaped the local Playwright cache") from exc
    if path.is_symlink() or not path.is_file() or not os.access(path, os.X_OK):
        raise WorkflowError("pinned Chromium executable is missing or unsafe")
    if path.name != "Google Chrome for Testing" or path.parent.name != "MacOS":
        raise WorkflowError("pinned Chromium executable has an unexpected bundle shape")
    return path


def chromium_bundle_sha256(executable: pathlib.Path) -> str:
    executable = validate_chromium_executable(executable)
    bundle = executable.parents[2]
    if bundle.is_symlink() or not bundle.is_dir() or bundle.suffix != ".app":
        raise WorkflowError("pinned Chromium app bundle is missing or unsafe")
    digest = hashlib.sha256()
    entries = 0
    for path in sorted(bundle.rglob("*"), key=lambda value: value.as_posix()):
        relative = path.relative_to(bundle).as_posix()
        if path.is_symlink():
            try:
                path.resolve().relative_to(bundle.resolve())
            except ValueError as exc:
                raise WorkflowError("Chromium bundle symlink escaped the app bundle") from exc
            digest.update(b"L\0" + relative.encode("utf-8") + b"\0")
            digest.update(os.readlink(path).encode("utf-8") + b"\0")
            entries += 1
        elif path.is_file():
            digest.update(b"F\0" + relative.encode("utf-8") + b"\0")
            with path.open("rb") as handle:
                for chunk in iter(lambda: handle.read(1024 * 1024), b""):
                    digest.update(chunk)
            entries += 1
        elif not path.is_dir():
            raise WorkflowError("Chromium bundle contains an unsupported entry")
    if entries == 0:
        raise WorkflowError("Chromium app bundle is empty")
    return digest.hexdigest()


def playwright_subprocess_environment(source: Optional[Mapping[str, str]] = None) -> Dict[str, str]:
    source = os.environ if source is None else source
    allowed = (
        "PATH",
        "TMPDIR",
        "LANG",
        "LC_ALL",
        "LC_CTYPE",
        "HOME",
        "USER",
        "LOGNAME",
        "SHELL",
        "TZ",
    )
    environment = {
        name: value
        for name in allowed
        if isinstance((value := source.get(name)), str) and value and "\x00" not in value
    }
    if "PATH" not in environment:
        raise WorkflowError("Playwright subprocess PATH is missing")
    environment.update(
        {
            "NO_UPDATE_NOTIFIER": "1",
            "CI": "1",
            "NO_PROXY": "127.0.0.1,localhost",
            "no_proxy": "127.0.0.1,localhost",
        }
    )
    return environment


class PlaywrightCLI:
    def __init__(
        self,
        *,
        session: str,
        scratch_dir: pathlib.Path,
        base_url: str,
        wrapper: pathlib.Path = DEFAULT_WRAPPER,
        redaction_file: Optional[pathlib.Path] = None,
        chromium_executable: Optional[pathlib.Path] = None,
        chromium_bundle_sha256_value: Optional[str] = None,
    ) -> None:
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]{2,62}", session):
            raise WorkflowError("session must be a lowercase alphanumeric/hyphen name")
        self.session = session
        self.scratch_dir = scratch_dir.resolve()
        self.output_dir = self.scratch_dir / "cli-output"
        self.base_url = validate_loopback_origin(base_url)
        self.wrapper = wrapper.resolve()
        if not self.wrapper.is_file():
            raise WorkflowError("playwright CLI wrapper is missing: %s" % self.wrapper)
        ensure_private_directory(self.scratch_dir)
        ensure_private_directory(self.output_dir)
        self.chromium_executable = (
            validate_chromium_executable(chromium_executable)
            if chromium_executable is not None
            else discover_local_chromium()
        )
        if self.chromium_executable is None:
            raise WorkflowError("no already-installed full Chromium build was found")
        if chromium_bundle_sha256_value is not None:
            if not re.fullmatch(r"[0-9a-f]{64}", chromium_bundle_sha256_value):
                raise WorkflowError("pinned Chromium bundle digest is invalid")
            if chromium_bundle_sha256(self.chromium_executable) != chromium_bundle_sha256_value:
                raise WorkflowError("pinned Chromium bundle changed")
        self.redaction_file = validate_private_path(redaction_file) if redaction_file is not None else None
        self.config_path = self.scratch_dir / "playwright-cli.json"
        self._write_config()

    def private_values(self) -> Tuple[str, ...]:
        return load_private_values(getattr(self, "redaction_file", None))

    def _write_config(self) -> None:
        config = {
            "browser": {
                "browserName": "chromium",
                "isolated": True,
                "launchOptions": {
                    "headless": True,
                    "executablePath": str(self.chromium_executable),
                },
                "contextOptions": {"viewport": {"width": 1440, "height": 1000}},
            },
            "outputDir": str(self.output_dir),
            "outputMode": "stdout",
            "console": {"level": "debug"},
            "network": {"allowedOrigins": [self.base_url]},
            "timeouts": {"action": 10000, "navigation": 60000},
        }
        atomic_write(self.config_path, (json.dumps(config, sort_keys=True, indent=2) + "\n").encode("utf-8"))

    def run(
        self,
        *arguments: str,
        timeout: int = 90,
        private_value_environment: Optional[Mapping[str, int]] = None,
    ) -> str:
        private_values = self.private_values()
        private_variants = private_value_variants(private_values)
        for argument in arguments:
            if any(variant and variant in argument for variant in private_variants):
                raise WorkflowError("private value must not be passed in a Playwright CLI argument")
        env = playwright_subprocess_environment()
        for name, index in (private_value_environment or {}).items():
            if not re.fullmatch(r"PRISM_WFL_PRIVATE_[A-Z0-9_]{1,48}", name):
                raise WorkflowError("private Playwright environment name is invalid")
            if isinstance(index, bool) or not isinstance(index, int) or not 0 <= index < len(private_values):
                raise WorkflowError("private Playwright environment index is invalid")
            env[name] = private_values[index]
        process = subprocess.run(
            [str(self.wrapper), "--session=%s" % self.session, *arguments],
            cwd=str(self.scratch_dir),
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=timeout,
            check=False,
        )
        output = redact_text(process.stdout or "", private_values)
        assert_no_remaining_secret(output, "playwright output", private_values)
        if process.returncode != 0:
            raise WorkflowError(
                "playwright CLI failed (%s): %s" % (" ".join(arguments[:2]), output[-3000:].strip())
            )
        return output

    def open_blank(self) -> str:
        return self.run(
            "open",
            "--browser=chromium",
            "--config=%s" % self.config_path,
            timeout=120,
        )

    def close(self, *, strict: bool = False) -> None:
        if strict:
            self.run("close", timeout=60)
            return
        with contextlib.suppress(Exception):
            self.run("close", timeout=60)

    def goto(self, route_path: str, wait_ms: int = 1300) -> str:
        route_path = validate_route_path(route_path)
        output = self.run("goto", self.base_url + route_path, timeout=90)
        if wait_ms:
            self.run(
                "run-code",
                "async (page) => { await page.waitForTimeout(%d); }" % wait_ms,
                timeout=30,
            )
        return output

    def resize(self, width: int, height: int) -> None:
        if width < 320 or height < 480 or width > 2560 or height > 1600:
            raise WorkflowError("viewport is outside the bounded test range")
        self.run("resize", str(width), str(height), timeout=30)

    def snapshot(self, label: str, case_dir: pathlib.Path) -> Dict[str, Any]:
        if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{0,80}", label):
            raise WorkflowError("invalid snapshot label")
        filename = "%s.yml" % label
        output = self.run("snapshot", "--filename=%s" % filename, timeout=60)
        source = self.scratch_dir / filename
        if not source.is_file():
            candidates = sorted(
                (*self.scratch_dir.rglob(filename), *self.output_dir.rglob(filename)),
                key=lambda item: item.stat().st_mtime_ns,
            )
            if not candidates:
                raise WorkflowError("playwright CLI did not create %s" % filename)
            source = candidates[-1]
        private_values = self.private_values()
        content = redact_text(source.read_text(encoding="utf-8", errors="replace"), private_values)
        assert_no_remaining_secret(content, "snapshot %s" % label, private_values)
        target = case_dir / "snapshots" / (label + ".snapshot.txt")
        if target.exists():
            raise WorkflowError("refusing to overwrite snapshot evidence: %s" % target)
        write_text(target, content)
        final_url, title = parse_page_metadata(output)
        if final_url is None:
            eval_output = self.run("eval", "() => window.location.href", timeout=30)
            final_url = str(parse_eval_result(eval_output))
        return {
            "label": label,
            "final_url": redact_text(final_url),
            "title": title or "",
            "snapshot": str(target.relative_to(case_dir)),
            "bytes": target.stat().st_size,
            "sha256": sha256_file(target),
            "fatal_markers": body_has_fatal_marker(content),
        }

    def screenshot(self, label: str, case_dir: pathlib.Path) -> Dict[str, Any]:
        if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{0,80}", label):
            raise WorkflowError("invalid screenshot label")
        filename = "%s.png" % label
        self.run("screenshot", "--filename=%s" % filename, "--full-page", timeout=90)
        source = self.scratch_dir / filename
        if not source.is_file():
            candidates = sorted(
                (*self.scratch_dir.rglob(filename), *self.output_dir.rglob(filename)),
                key=lambda item: item.stat().st_mtime_ns,
            )
            if not candidates:
                raise WorkflowError("playwright CLI did not create %s" % filename)
            source = candidates[-1]
        target = case_dir / "screenshots" / filename
        if target.exists():
            raise WorkflowError("refusing to overwrite screenshot evidence: %s" % target)
        ensure_private_directory(target.parent)
        shutil.copyfile(source, target)
        os.chmod(target, 0o600)
        return {
            "screenshot": str(target.relative_to(case_dir)),
            "bytes": target.stat().st_size,
            "sha256": sha256_file(target),
        }

    def console(self, minimum: str = "error") -> str:
        if minimum not in ("error", "warning", "info", "debug"):
            raise WorkflowError("invalid console level")
        return self.run("console", minimum, timeout=60)

    def trace_start(self) -> int:
        started = time.time_ns()
        self.run("tracing-start", timeout=60)
        return started

    def trace_stop(self, started_ns: int, target: pathlib.Path) -> Dict[str, Any]:
        output = self.run("tracing-stop", timeout=120)
        trace_candidates = sorted(
            (
                item
                for item in self.output_dir.rglob("trace-*.trace")
                if item.is_file() and not item.is_symlink() and item.stat().st_mtime_ns >= started_ns
            ),
            key=lambda item: item.stat().st_mtime_ns,
        )
        if not trace_candidates:
            match = re.search(r"\(([^)]+\.trace)\)", output)
            if match:
                possible = pathlib.Path(match.group(1))
                if not possible.is_absolute():
                    possible = self.scratch_dir / possible
                if possible.is_file() and not possible.is_symlink():
                    trace_candidates = [possible]
        if not trace_candidates:
            raise WorkflowError("trace stop completed but no trace file was found")
        trace_path = trace_candidates[-1]
        network_path = trace_path.with_suffix(".network")
        if not network_path.is_file() or network_path.is_symlink():
            raise WorkflowError("trace network file is missing: %s" % network_path)
        resources_dir = trace_path.parent / "resources"
        if target.exists():
            raise WorkflowError("refusing to overwrite trace evidence: %s" % target)
        raw_directory = self.scratch_dir / "raw-traces"
        validate_private_path(raw_directory, require_file=False)
        ensure_private_directory(raw_directory)
        descriptor, temporary_name = tempfile.mkstemp(prefix=".raw-trace.", suffix=".zip", dir=str(raw_directory))
        os.close(descriptor)
        raw_trace = pathlib.Path(temporary_name)
        os.chmod(raw_trace, 0o600)
        private_values = self.private_values()
        uncompressed_bytes = 0
        try:
            with zipfile.ZipFile(raw_trace, "w", compression=zipfile.ZIP_DEFLATED, allowZip64=True) as archive:
                for source, name in ((trace_path, "trace.trace"), (network_path, "trace.network")):
                    size = source.stat().st_size
                    if size > MAX_TRACE_COMPONENT_BYTES:
                        raise WorkflowError("raw trace metadata is too large")
                    uncompressed_bytes += size
                    archive.write(source, name)
                if resources_dir.is_dir():
                    for resource in sorted(resources_dir.rglob("*")):
                        if resource.is_file() and not resource.is_symlink():
                            size = resource.stat().st_size
                            if size > MAX_TRACE_COMPONENT_BYTES:
                                raise WorkflowError("raw trace resource is too large")
                            uncompressed_bytes += size
                            if uncompressed_bytes > MAX_TRACE_UNCOMPRESSED_BYTES:
                                raise WorkflowError("raw trace is too large")
                            archive.write(
                                resource,
                                "resources/%s" % resource.relative_to(resources_dir).as_posix(),
                            )
            os.chmod(raw_trace, 0o600)
            if raw_trace.stat().st_size > MAX_TRACE_ARCHIVE_BYTES:
                raise WorkflowError("raw trace archive is too large")
            return sanitize_trace_zip(raw_trace, target, private_values)
        finally:
            with contextlib.suppress(FileNotFoundError):
                raw_trace.unlink()
            try:
                purge_raw_trace_components(
                    self.output_dir,
                    trace_path,
                    network_path,
                    resources_dir,
                )
            except Exception:
                with contextlib.suppress(FileNotFoundError):
                    target.unlink()
                raise

    def route_json(self, pattern: str, status: int, body: Any) -> None:
        if not pattern.startswith("**/api/"):
            raise WorkflowError("browser-local fixtures may target only same-origin /api routes")
        self.run(
            "route",
            pattern,
            "--status=%d" % status,
            "--body=%s" % json.dumps(body, separators=(",", ":")),
            "--content-type=application/json",
            timeout=30,
        )

    def unroute(self, pattern: Optional[str] = None) -> None:
        args = ("unroute", pattern) if pattern else ("unroute",)
        self.run(*args, timeout=30)

    def evaluate_json(self, function: str) -> Any:
        return safe_json_projection(parse_eval_result(self.run("eval", function, timeout=60)))


def purge_raw_trace_components(
    output_dir: pathlib.Path,
    trace_path: pathlib.Path,
    network_path: pathlib.Path,
    resources_dir: pathlib.Path,
) -> None:
    """Delete only verified Playwright raw trace material after packaging."""
    root = output_dir.resolve()
    for path in (trace_path, network_path, resources_dir):
        if path.is_symlink():
            raise WorkflowError("raw trace component is a symlink")
        try:
            path.resolve().relative_to(root)
        except ValueError as exc:
            raise WorkflowError("raw trace component escaped its private output directory") from exc
    if resources_dir.exists():
        if not resources_dir.is_dir():
            raise WorkflowError("raw trace resources path is not a directory")
        for entry in resources_dir.rglob("*"):
            if entry.is_symlink():
                raise WorkflowError("raw trace resources contain a symlink")
            try:
                entry.resolve().relative_to(root)
            except ValueError as exc:
                raise WorkflowError("raw trace resource escaped its private output directory") from exc
        shutil.rmtree(resources_dir)
    for path in (trace_path, network_path):
        if path.exists() and not path.is_file():
            raise WorkflowError("raw trace component is not a regular file")
        with contextlib.suppress(FileNotFoundError):
            path.unlink()


def purge_private_scratch_tree(path: pathlib.Path) -> None:
    """Remove one exact run-private browser tree after the CLI is closed."""
    target = validate_private_path(path, require_file=False)
    if not target.exists():
        if target.is_symlink():
            raise WorkflowError("private browser scratch is a dangling symlink")
        return
    if target.is_symlink() or not target.is_dir():
        raise WorkflowError("private browser scratch is unsafe")
    for entry in target.rglob("*"):
        if entry.is_symlink():
            raise WorkflowError("private browser scratch contains a symlink")
        try:
            entry.resolve().relative_to(target.resolve())
        except ValueError as error:
            raise WorkflowError("private browser scratch escaped its owner") from error
    shutil.rmtree(target)
    if target.exists() or target.is_symlink():
        raise WorkflowError("private browser scratch cleanup was incomplete")


def validate_trace_archive(
    path: pathlib.Path,
    private_values: Sequence[str] = (),
    *,
    require_redaction_manifest: bool,
) -> Dict[str, Any]:
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_TRACE_ARCHIVE_BYTES:
        raise WorkflowError("trace archive is missing, unsafe, or too large")
    total_size = 0
    text_entries = 0
    with zipfile.ZipFile(path) as archive:
        infos = archive.infolist()
        names = [info.filename for info in infos]
        if len(names) != len(set(names)):
            raise WorkflowError("trace archive contains duplicate entries")
        if not {"trace.trace", "trace.network"}.issubset(set(names)):
            raise WorkflowError("trace archive is incomplete")
        if require_redaction_manifest and "trace-redaction.json" not in names:
            raise WorkflowError("trace archive has no redaction manifest")
        for info in infos:
            assert_no_remaining_secret(info.filename, "trace archive entry name", private_values)
            entry = pathlib.PurePosixPath(info.filename)
            if (
                entry.is_absolute()
                or ".." in entry.parts
                or info.flag_bits & 0x1
                or info.file_size > MAX_TRACE_COMPONENT_BYTES
                or (info.external_attr >> 16) & 0o170000 == 0o120000
            ):
                raise WorkflowError("trace archive contains an unsafe entry")
            total_size += info.file_size
            if total_size > MAX_TRACE_UNCOMPRESSED_BYTES:
                raise WorkflowError("trace archive expands beyond the safe limit")
            payload = archive.read(info)
            assert_no_remaining_secret_bytes(payload, "trace archive entry", private_values)
            try:
                text = payload.decode("utf-8")
            except UnicodeDecodeError:
                if require_redaction_manifest:
                    raise WorkflowError("sanitized trace contains an unverifiable binary entry")
            else:
                assert_no_remaining_secret(text, "trace archive entry", private_values)
                text_entries += 1
        manifest = None
        if "trace-redaction.json" in names:
            manifest = json.loads(archive.read("trace-redaction.json").decode("utf-8"))
            if (
                not isinstance(manifest, dict)
                or manifest.get("schema_version") != 1
                or manifest.get("binary_resource_policy") != "omitted"
                or manifest.get("sanitizer") != "workflow_playwright_text_only_v1"
            ):
                raise WorkflowError("trace redaction manifest is invalid")
    return {
        "entries": len(names),
        "text_entries": text_entries,
        "uncompressed_bytes": total_size,
        "redaction_manifest": manifest is not None,
    }


def sanitize_trace_zip(
    source: pathlib.Path,
    target: pathlib.Path,
    private_values: Sequence[str],
) -> Dict[str, Any]:
    source = validate_private_path(source)
    if target.exists() or target.is_symlink():
        raise WorkflowError("refusing to overwrite trace evidence")
    if source.stat().st_size > MAX_TRACE_ARCHIVE_BYTES or not zipfile.is_zipfile(source):
        raise WorkflowError("raw trace archive is invalid or too large")
    ensure_private_directory(target.parent)
    descriptor, temporary_name = tempfile.mkstemp(prefix=".%s." % target.name, dir=str(target.parent))
    os.close(descriptor)
    temporary = pathlib.Path(temporary_name)
    retained = 0
    omitted = 0
    total = 0
    try:
        with zipfile.ZipFile(source) as reader, zipfile.ZipFile(
            temporary,
            "w",
            compression=zipfile.ZIP_DEFLATED,
            allowZip64=True,
        ) as writer:
            seen = set()
            for info in reader.infolist():
                assert_no_remaining_secret(info.filename, "raw trace entry name", private_values)
                entry = pathlib.PurePosixPath(info.filename)
                if (
                    info.filename in seen
                    or entry.is_absolute()
                    or ".." in entry.parts
                    or info.flag_bits & 0x1
                    or info.file_size > MAX_TRACE_COMPONENT_BYTES
                    or (info.external_attr >> 16) & 0o170000 == 0o120000
                ):
                    raise WorkflowError("raw trace archive contains an unsafe entry")
                seen.add(info.filename)
                if info.filename == "trace-redaction.json":
                    continue
                payload = reader.read(info)
                total += len(payload)
                if total > MAX_TRACE_UNCOMPRESSED_BYTES:
                    raise WorkflowError("raw trace archive expands beyond the safe limit")
                try:
                    text = payload.decode("utf-8")
                except UnicodeDecodeError:
                    if info.filename in {"trace.trace", "trace.network"}:
                        raise WorkflowError("raw trace metadata is not UTF-8 text")
                    assert_no_remaining_secret_bytes(payload, "raw binary trace entry", private_values)
                    omitted += 1
                    continue
                redacted = redact_text(text, private_values)
                assert_no_remaining_secret(redacted, "raw trace entry", private_values)
                writer.writestr(info.filename, redacted.encode("utf-8"))
                retained += 1
            if not {"trace.trace", "trace.network"}.issubset(seen):
                raise WorkflowError("raw trace archive is incomplete")
            manifest = {
                "schema_version": 1,
                "sanitizer": "workflow_playwright_text_only_v1",
                "binary_resource_policy": "omitted",
                "retained_text_resources": max(0, retained - 2),
                "omitted_binary_resources": omitted,
                "private_value_count": len(private_values),
            }
            writer.writestr(
                "trace-redaction.json",
                (json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8"),
            )
        os.chmod(temporary, 0o600)
        os.replace(str(temporary), str(target))
    finally:
        with contextlib.suppress(FileNotFoundError):
            temporary.unlink()
    try:
        if target.stat().st_size > MAX_TRACE_ARCHIVE_BYTES:
            raise WorkflowError("sanitized trace archive is too large")
        findings = validate_trace_archive(target, private_values, require_redaction_manifest=True)
        return {"path": target.name, "bytes": target.stat().st_size, "sha256": sha256_file(target), **findings}
    except Exception:
        with contextlib.suppress(FileNotFoundError):
            target.unlink()
        raise


def api_get(base_url: str, route_path: str) -> Any:
    # Managed read-only cases use their own private Vite listener instead of
    # the retained runtime's historical 15174 listener.  Literal IPv4 loopback
    # remains mandatory; hostnames, redirects, credentials, and path-bearing
    # origins are still rejected by the shared validator.
    base_url = validate_loopback_origin(base_url)
    route_path = validate_route_path(route_path)
    if not route_path.startswith("/api/"):
        raise WorkflowError("API probes must stay under /api")
    request = urllib.request.Request(base_url + route_path, method="GET")
    opener = urllib.request.build_opener(NoRedirectHandler)
    try:
        with opener.open(request, timeout=15) as response:
            if response.status != 200:
                raise ProductAssertionError("readonly_api_status_unexpected")
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        raise ProductAssertionError("readonly_api_status_unexpected") from error
    except json.JSONDecodeError as error:
        raise ProductAssertionError("readonly_api_json_invalid") from error
    except urllib.error.URLError as error:
        raise WorkflowError("GET %s failed: %s" % (route_path, error))


def select_representative_models(models: Sequence[Mapping[str, Any]]) -> Dict[str, Mapping[str, Any]]:
    representatives: Dict[str, Mapping[str, Any]] = {}
    for model in models:
        mode = model.get("openai_accepted_format")
        model_id = str(model.get("model_id", ""))
        image_operations = model.get("openai_image_operations")
        if mode == "chat_completions_only" and "chat_only" not in representatives:
            representatives["chat_only"] = model
        if mode == "dual_native" and "dual_native" not in representatives:
            representatives["dual_native"] = model
        if (image_operations is not None or "gpt-image" in model_id) and "image_only" not in representatives:
            representatives["image_only"] = model
    missing = sorted({"chat_only", "dual_native", "image_only"} - set(representatives))
    if missing:
        raise ProductAssertionError("readonly_model_operation_shapes_missing")
    return representatives


def snapshot_entry_with_path(cli: PlaywrightCLI, case_dir: pathlib.Path, label: str, path: str, wait_ms: int = 1300) -> Dict[str, Any]:
    cli.goto(path, wait_ms=wait_ms)
    entry = cli.snapshot(label, case_dir)
    entry["requested_path"] = path
    return entry


def readonly_wfl001(cli: PlaywrightCLI, case_dir: pathlib.Path) -> Dict[str, Any]:
    snapshots: List[Dict[str, Any]] = []
    routes = (
        ("home", "/"),
        ("models", "/route/models"),
        ("endpoints", "/route/endpoints"),
        ("ban-policies", "/route/ban-policies"),
        ("pricing", "/route/pricing"),
        ("requests", "/observe/requests"),
        ("routing-health", "/observe/routing-health"),
        ("settings", "/system/settings"),
        ("proxy-keys", "/system/proxy-keys"),
    )
    for label, route in routes:
        snapshots.append(snapshot_entry_with_path(cli, case_dir, label, route))

    redirects = []
    redirect_specs = (
        ("legacy-models", "/models", "/route/models", {}),
        ("legacy-proxy-keys", "/control/proxy-keys", "/system/proxy-keys", {}),
        (
            "legacy-routing-events",
            "/observe?tab=events&preset=6h&event_type=retry_exhausted&metric=cost",
            "/observe/routing-health",
            {"preset": ["6h"], "event_type": ["retry_exhausted"]},
        ),
    )
    for label, requested, expected_path, expected_query in redirect_specs:
        entry = snapshot_entry_with_path(cli, case_dir, label, requested)
        snapshots.append(entry)
        parsed = urllib.parse.urlsplit(entry["final_url"])
        actual_query = urllib.parse.parse_qs(parsed.query)
        kept_expected = all(actual_query.get(key) == value for key, value in expected_query.items())
        redirects.append(
            {
                "label": label,
                "requested_path": requested,
                "expected_path": expected_path,
                "final_path": parsed.path,
                "expected_query": expected_query,
                "actual_query": actual_query,
                "passed": parsed.path == expected_path and kept_expected,
            }
        )

    console_text = cli.console("error")
    write_text(case_dir / "browser-console.log", console_text)
    auth_status = safe_json_projection(api_get(cli.base_url, "/api/auth/status"))
    navigation = {
        "case_id": "WFL-001",
        "recorded_at": utc_now(),
        "browser": "chromium",
        "base_url": cli.base_url,
        "auth_state": auth_status.get("state") if isinstance(auth_status, Mapping) else None,
        "entries": [
            {
                "label": item["label"],
                "requested_path": item["requested_path"],
                "final_url": item["final_url"],
                "title": item["title"],
                "fatal_markers": item["fatal_markers"],
            }
            for item in snapshots
        ],
    }
    snapshot_index = {
        "case_id": "WFL-001",
        "recorded_at": utc_now(),
        "entries": snapshots,
    }
    redirect_grid = {"case_id": "WFL-001", "recorded_at": utc_now(), "entries": redirects}
    write_json(case_dir / "navigation-transcript.json", navigation)
    write_json(case_dir / "snapshots-index.json", snapshot_index)
    write_json(case_dir / "redirect-grid.json", redirect_grid)
    failures = []
    if navigation["auth_state"] != "disabled":
        failures.append("auth state is not disabled")
    expected_paths = {
        "home": "/observe",
        "models": "/route/models",
        "endpoints": "/route/endpoints",
        "ban-policies": "/route/ban-policies",
        "pricing": "/route/pricing",
        "requests": "/observe/requests",
        "routing-health": "/observe/routing-health",
        "settings": "/system/settings",
        "proxy-keys": "/system/proxy-keys",
    }
    for item in navigation["entries"]:
        expected = expected_paths.get(item["label"])
        if expected and urllib.parse.urlsplit(item["final_url"]).path != expected:
            failures.append("%s did not settle on %s" % (item["label"], expected))
    if any(item["fatal_markers"] for item in snapshots):
        failures.append("one or more snapshots contain a fatal marker")
    if not all(item["passed"] for item in redirects):
        failures.append("one or more legacy redirects failed")
    if body_has_fatal_marker(console_text):
        failures.append("browser console contains a fatal marker")
    if failures:
        raise ProductAssertionError("wfl_001_semantic_oracle_failed")
    return {"case_id": "WFL-001", "passed": True, "failures": []}


def diagnostics_projection(value: Mapping[str, Any]) -> Dict[str, Any]:
    return {
        "model_config_id": value.get("model_config_id"),
        "openai_accepted_format": value.get("openai_accepted_format"),
        "strategy": safe_json_projection(value.get("strategy")),
        "accepted_operations": safe_json_projection(value.get("accepted_operations", [])),
        "targets": [
            {
                "access_target_id": item.get("access_target_id"),
                "authored_stage_position": item.get("authored_stage_position"),
                "target_type": item.get("target_type"),
                "coverage": item.get("coverage"),
                "mode_match": item.get("mode_match"),
                "operation_results": safe_json_projection(item.get("operation_results", [])),
            }
            for item in value.get("targets", [])
            if isinstance(item, Mapping)
        ],
        "operation_routes": safe_json_projection(value.get("operation_routes", [])),
        "configuration_warnings": [
            {
                "code": warning.get("code"),
                "severity": warning.get("severity"),
                "message": warning.get("message"),
                "path": warning.get("path"),
                "operation_names": safe_json_projection(warning.get("operation_names", [])),
            }
            for warning in value.get("configuration_warnings", [])
            if isinstance(warning, Mapping)
        ],
    }


def frozen_wfl002_model_groups() -> Dict[str, Tuple[str, ...]]:
    """Return the exact case-sensitive 5+6+1 imported OpenAI model contract."""
    try:
        matrix = json.loads(MATRIX_PATH.read_text(encoding="utf-8"))
        raw = matrix.get("model_groups")
    except (OSError, UnicodeError, json.JSONDecodeError, AttributeError) as error:
        raise WorkflowError("frozen WFL-002 model groups are unavailable") from error
    if not isinstance(raw, Mapping):
        raise WorkflowError("frozen WFL-002 model groups are invalid")
    keys = ("OPENAI_CHAT_ONLY", "OPENAI_DUAL_NATIVE", "OPENAI_IMAGE_ONLY")
    expected_counts = (5, 6, 1)
    result: Dict[str, Tuple[str, ...]] = {}
    for key, expected_count in zip(keys, expected_counts):
        values = raw.get(key)
        if (
            not isinstance(values, list)
            or len(values) != expected_count
            or any(not isinstance(value, str) or not value for value in values)
            or len(set(values)) != len(values)
        ):
            raise WorkflowError("frozen WFL-002 model groups are invalid")
        result[key] = tuple(values)
    flattened = tuple(value for key in keys for value in result[key])
    if len(flattened) != 12 or len(set(flattened)) != 12:
        raise WorkflowError("frozen WFL-002 model groups are invalid")
    return result


def model_inventory_projection(model: Mapping[str, Any]) -> Dict[str, Any]:
    if (
        isinstance(model.get("id"), bool)
        or not isinstance(model.get("id"), int)
        or int(model["id"]) < 1
        or not isinstance(model.get("model_id"), str)
        or not model.get("model_id")
        or not isinstance(model.get("api_family"), str)
        or not isinstance(model.get("is_enabled"), bool)
    ):
        raise ProductAssertionError("wfl_002_model_inventory_shape_invalid")
    targets = model.get("access_targets")
    if not isinstance(targets, list):
        raise ProductAssertionError("wfl_002_model_target_array_invalid")
    projected_targets = []
    for target in targets:
        if not isinstance(target, Mapping):
            raise ProductAssertionError("wfl_002_model_target_array_invalid")
        connection_id = target.get("connection_id")
        if connection_id is None:
            connection_id = target.get("terminal_target_id")
        target_id = target.get("id")
        position = target.get("position")
        target_type = target.get("target_type")
        target_model_id = target.get("target_model_id")
        if (
            isinstance(target_id, bool)
            or not isinstance(target_id, int)
            or target_id < 1
            or isinstance(position, bool)
            or not isinstance(position, int)
            or position < 0
            or target_type not in {"model", "connection"}
            or not isinstance(target.get("is_enabled"), bool)
            or (target_type == "model" and (not isinstance(target_model_id, str) or not target_model_id or connection_id is not None))
            or (target_type == "connection" and (target_model_id is not None or isinstance(connection_id, bool) or not isinstance(connection_id, int) or connection_id < 1))
        ):
            raise ProductAssertionError("wfl_002_model_target_shape_invalid")
        projected_targets.append(
            {
                "id": target_id,
                "target_type": target_type,
                "target_model_id": target_model_id,
                "connection_id": connection_id,
                "position": position,
                "is_enabled": target.get("is_enabled"),
            }
        )
    projected_targets.sort(key=lambda item: (int(item["position"]), int(item["id"])))
    return {
        "id": model.get("id"),
        "model_id": model.get("model_id"),
        "api_family": model.get("api_family"),
        "openai_accepted_format": model.get("openai_accepted_format"),
        "openai_image_operations": model.get("openai_image_operations"),
        "is_enabled": model.get("is_enabled"),
        "access_targets": projected_targets,
    }


def expected_wfl002_capability(model_id: str, groups: Mapping[str, Sequence[str]]) -> Tuple[Optional[str], Optional[str], str]:
    if model_id in groups["OPENAI_CHAT_ONLY"]:
        return "chat_completions_only", None, "chat_only"
    if model_id in groups["OPENAI_DUAL_NATIVE"]:
        return "dual_native", None, "dual_native"
    if model_id in groups["OPENAI_IMAGE_ONLY"]:
        return None, "generations_and_edits", "image_only"
    raise ProductAssertionError("wfl_002_unexpected_model_id")


def readonly_wfl002(
    cli: PlaywrightCLI,
    case_dir: pathlib.Path,
    database_inventory: Optional[Mapping[str, Any]] = None,
) -> Dict[str, Any]:
    if not isinstance(database_inventory, Mapping):
        raise WorkflowError("managed WFL-002 database inventory is required")
    database_models = database_inventory.get("models")
    if not isinstance(database_models, list) or any(not isinstance(item, Mapping) for item in database_models):
        raise WorkflowError("managed WFL-002 database inventory is invalid")
    database_projection = [model_inventory_projection(item) for item in database_models]
    database_projection.sort(key=lambda item: str(item.get("model_id", "")))
    assert_safe_json(database_projection, "wfl_002_database_inventory")

    models_value = api_get(cli.base_url, "/api/models")
    if not isinstance(models_value, list) or not all(isinstance(item, Mapping) for item in models_value):
        raise ProductAssertionError("wfl_002_model_array_invalid")
    all_models: List[Mapping[str, Any]] = list(models_value)
    models = [model for model in all_models if model.get("api_family") == "openai"]
    api_projection = [model_inventory_projection(item) for item in models]
    api_projection.sort(key=lambda item: str(item.get("model_id", "")))
    groups = frozen_wfl002_model_groups()
    expected_ids = tuple(value for key in groups for value in groups[key])
    expected_id_set = set(expected_ids)

    cli.goto("/route/models", wait_ms=1800)
    list_entry = cli.snapshot("model-list", case_dir)
    list_snapshot_path = case_dir / list_entry["snapshot"]
    list_text = list_snapshot_path.read_text(encoding="utf-8")
    write_text(case_dir / "model-list.snapshot.txt", list_text)
    list_rows_value = cli.evaluate_json(
        "() => Array.from(document.querySelectorAll('[data-testid^=\"models-table-row-\"]')).map((row) => ({test_id: row.getAttribute('data-testid'), text: (row.innerText || '').replace(/\\s+/g, ' ').trim()}))"
    )
    if not isinstance(list_rows_value, list) or any(not isinstance(item, Mapping) for item in list_rows_value):
        raise ProductAssertionError("wfl_002_model_list_view_invalid")
    list_rows = [safe_json_projection(item) for item in list_rows_value]
    expected_row_ids = {
        str(model.get("model_id", "")): "models-table-row-%s" % model.get("id")
        for model in models
    }
    rows_by_test_id = {
        str(row.get("test_id", "")): row
        for row in list_rows
        if isinstance(row.get("test_id"), str)
    }
    visible_ids = [
        model_id
        for model_id in expected_ids
        if expected_row_ids.get(model_id) in rows_by_test_id
        and model_id in str(rows_by_test_id[expected_row_ids[model_id]].get("text", ""))
    ]

    detail_entries = []
    diagnostics_entries = []
    models_by_id = {str(model.get("model_id", "")): model for model in models}
    for model_id in expected_ids:
        model = models_by_id.get(model_id)
        if model is None:
            raise ProductAssertionError("wfl_002_exact_model_set_mismatch")
        expected_text_mode, expected_image_mode, role = expected_wfl002_capability(model_id, groups)
        try:
            numeric_id = int(model["id"])
        except (KeyError, TypeError, ValueError) as error:
            raise ProductAssertionError("wfl_002_model_identity_invalid") from error
        if numeric_id < 1:
            raise ProductAssertionError("wfl_002_model_identity_invalid")
        path = "/route/models/%d" % numeric_id
        cli.goto(path, wait_ms=1800)
        snapshot = cli.snapshot("model-detail-%02d" % (expected_ids.index(model_id) + 1), case_dir)
        snapshot_text = (case_dir / snapshot["snapshot"]).read_text(encoding="utf-8")
        diagnostics_value = api_get(cli.base_url, "/api/models/%d/routing-diagnostics" % numeric_id)
        if not isinstance(diagnostics_value, Mapping):
            raise ProductAssertionError("wfl_002_routing_diagnostics_invalid")
        projected = diagnostics_projection(diagnostics_value)
        detail_entries.append(
            {
                "role": role,
                "model_config_id": numeric_id,
                "model_id": model_id,
                "api_family": str(model.get("api_family", "")),
                "openai_accepted_format": model.get("openai_accepted_format"),
                "openai_image_operations": model.get("openai_image_operations"),
                "expected_openai_accepted_format": expected_text_mode,
                "expected_openai_image_operations": expected_image_mode,
                "final_url": snapshot["final_url"],
                "snapshot": snapshot["snapshot"],
                "snapshot_sha256": snapshot["sha256"],
                "model_id_visible": model_id in snapshot_text,
                "fatal_markers": snapshot["fatal_markers"],
                "coverage_labels_present": {
                    state: label in snapshot_text
                    for state, label in COVERAGE_LABELS.items()
                    if any(
                        str(target.get("coverage", "")).upper() == state
                        for target in projected.get("targets", [])
                    )
                },
                "removed_authoring_controls_absent": not any(
                    marker in snapshot_text.lower()
                    for marker in ("translation mode", "operation translation", "exact facade")
                ),
            }
        )
        diagnostics_entries.append(
            {
                "role": role,
                "model_id": model_id,
                "diagnostics": projected,
            }
        )

    cli.goto("/route/models", wait_ms=1000)
    detail_evidence = {
        "case_id": "WFL-002",
        "recorded_at": utc_now(),
        "model_count": len(models),
        "all_api_model_count": len(all_models),
        "expected_model_count": 12,
        "expected_groups": {key: list(value) for key, value in groups.items()},
        "database_models": database_projection,
        "api_models": api_projection,
        "list_rows": list_rows,
        "visible_model_ids": visible_ids,
        "entries": detail_entries,
    }
    coverage_evidence = {
        "case_id": "WFL-002",
        "recorded_at": utc_now(),
        "entries": diagnostics_entries,
        "observed_coverages": sorted(
            {
                str(target.get("coverage", "")).upper()
                for entry in diagnostics_entries
                for target in entry["diagnostics"].get("targets", [])
                if target.get("coverage")
            }
        ),
    }
    write_json(case_dir / "model-detail-snapshots.json", detail_evidence)
    write_json(case_dir / "coverage-diagnostics.json", coverage_evidence)
    failures = []
    actual_ids = {str(item.get("model_id", "")) for item in models}
    if len(models) != 12 or actual_ids != expected_id_set:
        failures.append("API model IDs do not match the exact frozen 5+6+1 set")
    if database_projection != api_projection:
        failures.append("API model and target topology differs from the owned clone")
    for item in api_projection:
        model_id = str(item.get("model_id", ""))
        try:
            expected_text, expected_image, _role = expected_wfl002_capability(model_id, groups)
        except ProductAssertionError:
            failures.append("API model is outside the exact frozen set")
            continue
        if (
            item.get("api_family") != "openai"
            or item.get("openai_accepted_format") != expected_text
            or item.get("openai_image_operations") != expected_image
        ):
            failures.append("API model capability differs from its frozen 5+6+1 group")
    if len(visible_ids) != 12:
        failures.append("model list snapshot does not expose all 12 exact model IDs")
    selected_row_ids = set(expected_row_ids.values())
    if not selected_row_ids <= set(rows_by_test_id) or len(selected_row_ids) != 12:
        failures.append("model list does not expose one structured row for each frozen OpenAI model")
    capability_labels = {
        "chat_only": ("仅 Chat Completions", "Chat Completions"),
        "dual_native": ("双模式", "Dual"),
        "image_only": ("纯图片", "Image"),
    }
    for model_id in expected_ids:
        _text_mode, _image_mode, role = expected_wfl002_capability(model_id, groups)
        matching_row = rows_by_test_id.get(expected_row_ids.get(model_id, ""))
        if matching_row is None or not any(
            label.lower() in str(matching_row.get("text", "")).lower()
            for label in capability_labels[role]
        ):
            failures.append("model list row does not expose the exact model capability")
    if any(item.get("model_id_visible") is not True for item in detail_entries):
        failures.append("one or more model detail views omit the exact model ID")
    missing_coverages = sorted(set(COVERAGE_LABELS) - set(coverage_evidence["observed_coverages"]))
    if missing_coverages:
        failures.append("routing diagnostics are missing coverage states: %s" % ", ".join(missing_coverages))
    if any(not all(item["coverage_labels_present"].values()) for item in detail_entries):
        failures.append("one or more diagnostics coverage states lack a structured UI label")
    if not all(item["removed_authoring_controls_absent"] for item in detail_entries):
        failures.append("removed authoring controls are still visible")
    if any(item["fatal_markers"] for item in [list_entry, *detail_entries]):
        failures.append("one or more model snapshots contain a fatal marker")
    if failures:
        raise ProductAssertionError("wfl_002_semantic_oracle_failed")
    return {"case_id": "WFL-002", "passed": True, "failures": []}


A11Y_AUDIT_JS = r"""() => {
  const visible = (el) => {
    const s = getComputedStyle(el); const r = el.getBoundingClientRect();
    return s.visibility !== 'hidden' && s.display !== 'none' && r.width > 0 && r.height > 0;
  };
  const label = (el) => {
    const by = el.getAttribute('aria-labelledby');
    if (by) return by.split(/\s+/).map(id => document.getElementById(id)?.textContent || '').join(' ').trim();
    if (el.getAttribute('aria-label')) return el.getAttribute('aria-label').trim();
    if (el.labels?.length) return Array.from(el.labels).map(x => x.textContent || '').join(' ').trim();
    return (el.textContent || el.getAttribute('alt') || el.getAttribute('title') || el.getAttribute('placeholder') || '').trim();
  };
  const controls = Array.from(document.querySelectorAll('button,a[href],input,select,textarea,[role="button"],[role="link"]')).filter(visible);
  const missing = controls.filter(el => !label(el)).slice(0, 30).map(el => ({tag: el.tagName.toLowerCase(), role: el.getAttribute('role') || '', test_id: el.getAttribute('data-testid') || ''}));
  const parse = (value) => {
    if (!value) return null;
    let m = value.match(/^rgba?\(([^)]+)\)$/i);
    if (m) { const p = m[1].split(/[ ,/]+/).filter(Boolean).map(Number); return [p[0]/255,p[1]/255,p[2]/255,p.length > 3 ? p[3] : 1]; }
    m = value.match(/^oklch\(([-.\d%]+)\s+([-.\d]+)\s+([-.\d]+)(?:\s*\/\s*([-.\d%]+))?\)$/i);
    if (!m) return null;
    const L = m[1].endsWith('%') ? parseFloat(m[1])/100 : parseFloat(m[1]); const C = parseFloat(m[2]); const h = parseFloat(m[3])*Math.PI/180;
    const a = C*Math.cos(h), b = C*Math.sin(h); const l_ = L + 0.3963377774*a + 0.2158037573*b; const mm_ = L - 0.1055613458*a - 0.0638541728*b; const s_ = L - 0.0894841775*a - 1.291485548*b;
    const l=l_**3, mm=mm_**3, ss=s_**3; const lin=[4.0767416621*l-3.3077115913*mm+0.2309699292*ss,-1.2684380046*l+2.6097574011*mm-0.3413193965*ss,-0.0041960863*l-0.7034186147*mm+1.707614701*ss];
    const enc=x=>Math.max(0,Math.min(1,x<=0.0031308?12.92*x:1.055*Math.pow(x,1/2.4)-0.055)); const alpha=m[4]?(m[4].endsWith('%')?parseFloat(m[4])/100:parseFloat(m[4])):1; return [enc(lin[0]),enc(lin[1]),enc(lin[2]),alpha];
  };
  const lum = c => { const f=x=>x<=0.04045?x/12.92:Math.pow((x+0.055)/1.055,2.4); return 0.2126*f(c[0])+0.7152*f(c[1])+0.0722*f(c[2]); };
  const ratio=(a,b)=>{const x=lum(a),y=lum(b); return (Math.max(x,y)+0.05)/(Math.min(x,y)+0.05);};
  const textElements=[]; const seen=new Set(); const walker=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT); let node;
  while ((node=walker.nextNode()) && textElements.length<900) { const el=node.parentElement; if (!el || seen.has(el) || !node.textContent.trim() || !visible(el)) continue; seen.add(el); textElements.push(el); }
  const contrast=[]; let measured=0, skipped=0;
  for (const el of textElements) { const s=getComputedStyle(el); const fg=parse(s.color); let p=el,bg=null; while(p && !bg){const c=parse(getComputedStyle(p).backgroundColor); if(c && c[3]>0.98) bg=c; p=p.parentElement;} if(!bg) bg=[1,1,1,1]; if(!fg){skipped++;continue;} measured++; const rr=ratio(fg,bg); const large=parseFloat(s.fontSize)>=24 || (parseFloat(s.fontSize)>=18.66 && parseInt(s.fontWeight,10)>=700); const min=large?3:4.5; if(rr<min) contrast.push({ratio:Number(rr.toFixed(2)), minimum:min, severe:rr<3, text:(el.textContent||'').trim().slice(0,60)}); }
  const overflow = controls.filter(el => { const r=el.getBoundingClientRect(); if(r.left>=0 && r.right<=innerWidth) return false; let p=el.parentElement; while(p){ if(p.scrollWidth>p.clientWidth+1 && ['auto','scroll'].includes(getComputedStyle(p).overflowX)) return false; p=p.parentElement;} return true; }).slice(0,30).map(el=>({tag:el.tagName.toLowerCase(), name:label(el).slice(0,80)}));
  return {url:location.href, viewport:{width:innerWidth,height:innerHeight}, language:document.documentElement.lang||'', main_count:document.querySelectorAll('main,[role="main"]').length, heading_count:document.querySelectorAll('h1,h2,h3,[role="heading"]').length, visible_control_count:controls.length, missing_name_count:missing.length, missing_names:missing, inaccessible_control_count:overflow.length, inaccessible_controls:overflow, contrast_measured:measured, contrast_skipped:skipped, contrast_failure_count:contrast.length, severe_contrast_count:contrast.filter(x=>x.severe).length, contrast_failures:contrast.slice(0,40)};
}"""


def capture_a11y(cli: PlaywrightCLI) -> Dict[str, Any]:
    cli.run("press", "Tab", timeout=30)
    focus = cli.evaluate_json(
        "() => ({tag: document.activeElement?.tagName?.toLowerCase() || '', name: (document.activeElement?.getAttribute('aria-label') || document.activeElement?.textContent || '').trim().slice(0, 120)})"
    )
    result = cli.evaluate_json(A11Y_AUDIT_JS)
    if not isinstance(result, MutableMapping):
        raise WorkflowError("accessibility audit did not return an object")
    result["focus_after_tab"] = focus
    return dict(result)


def readonly_wfl010(cli: PlaywrightCLI, case_dir: pathlib.Path) -> Dict[str, Any]:
    models_value = api_get(cli.base_url, "/api/models")
    if not isinstance(models_value, list) or not all(isinstance(item, Mapping) for item in models_value):
        raise ProductAssertionError("wfl_010_model_array_invalid")
    representatives = select_representative_models(models_value)
    viewport_entries: List[Dict[str, Any]] = []
    audits: List[Dict[str, Any]] = []

    for viewport_name, width, height, routes in (
        ("desktop", 1440, 1000, (("models", "/route/models"), ("settings", "/system/settings"))),
        ("narrow", 390, 844, (("models", "/route/models"), ("endpoints", "/route/endpoints"), ("settings", "/system/settings"))),
    ):
        cli.resize(width, height)
        for label, route in routes:
            entry = snapshot_entry_with_path(cli, case_dir, "%s-%s" % (viewport_name, label), route, wait_ms=1500)
            shot = cli.screenshot("%s-%s" % (viewport_name, label), case_dir)
            entry.update({"viewport": viewport_name, "width": width, "height": height, **shot})
            viewport_entries.append(entry)
            audit = capture_a11y(cli)
            audit.update({"viewport": viewport_name, "label": label})
            audits.append(audit)

    state_entries: List[Dict[str, Any]] = []
    cli.resize(1440, 1000)
    model_pattern = "**/api/models"
    try:
        cli.route_json(model_pattern, 200, [])
        entry = snapshot_entry_with_path(cli, case_dir, "state-empty-models", "/route/models", wait_ms=1300)
        entry["semantic"] = cli.evaluate_json(
            "() => ({empty_copy_visible: document.body.innerText.includes('\u8fd8\u6ca1\u6709\u914d\u7f6e\u6a21\u578b') || document.body.innerText.includes('\u6ca1\u6709\u5339\u914d\u7684\u6a21\u578b'), page_visible: Boolean(document.querySelector('[data-testid=\"models-feature-page\"]'))})"
        )
        state_entries.append({"state": "empty", "surface": "models", **entry})
    finally:
        cli.unroute(model_pattern)

    try:
        cli.route_json(model_pattern, 500, {"detail": "matrix synthetic API error"})
        entry = snapshot_entry_with_path(cli, case_dir, "state-error-models", "/route/models", wait_ms=1300)
        entry["semantic"] = cli.evaluate_json(
            "() => ({error_panel_visible: Boolean(document.querySelector('[data-testid=\"models-feature-error\"]')), retry_visible: Array.from(document.querySelectorAll('button')).some(button => ['\u91cd\u8bd5', 'Retry'].includes((button.textContent || '').trim()))})"
        )
        state_entries.append({"state": "error", "surface": "models", **entry})
    finally:
        cli.unroute(model_pattern)

    delay_code = "async (page) => { await page.route('**/api/models', async route => { await page.waitForTimeout(5000); await route.fulfill({status: 200, contentType: 'application/json', body: '[]'}); }); }"
    cli.run("run-code", delay_code, timeout=30)
    try:
        cli.goto("/route/models", wait_ms=0)
        entry = cli.snapshot("state-loading-models", case_dir)
        entry["requested_path"] = "/route/models"
        entry["semantic"] = cli.evaluate_json(
            "() => ({loading_visible: Boolean(document.querySelector('[data-testid=\"models-feature-loading\"]'))})"
        )
        state_entries.append({"state": "loading", "surface": "models", **entry})
    finally:
        cli.run(
            "run-code",
            "async (page) => { await page.unroute('**/api/models'); }",
            timeout=30,
        )

    dashboard_pattern = "**/api/stats/dashboard/now"
    cli.goto("/observe", wait_ms=1800)
    refresh_snapshot = cli.snapshot("state-stale-refresh-action", case_dir)
    refresh_text = (case_dir / refresh_snapshot["snapshot"]).read_text(encoding="utf-8")
    try:
        refresh_ref = snapshot_ref_for_name(refresh_text, "button", ("\u5237\u65b0", "Refresh"))
    except WorkflowError as error:
        raise ProductAssertionError("wfl_010_refresh_control_missing") from error
    try:
        cli.route_json(dashboard_pattern, 503, {"detail": "matrix synthetic delayed stale"})
        cli.run("click", refresh_ref, timeout=30)
        cli.run("run-code", "async (page) => { await page.waitForTimeout(1800); }", timeout=30)
        entry = cli.snapshot("state-stale-observe", case_dir)
        entry["requested_path"] = "/observe"
        entry["refresh_action_snapshot"] = refresh_snapshot["snapshot"]
        entry["refresh_action_snapshot_sha256"] = refresh_snapshot["sha256"]
        entry["semantic"] = cli.evaluate_json(
            "() => ({stale_badge_visible: Boolean(document.querySelector('[data-testid=\"observe-freshness-bar\"]')) && (document.body.innerText.includes('\u4e0a\u6b21\u6210\u529f\u5237\u65b0') || document.body.innerText.includes('last successful refresh')), retained_values_visible: Boolean(document.querySelector('[data-testid=\"now-strip\"]'))})"
        )
        state_entries.append({"state": "stale", "surface": "observe", **entry})
    finally:
        cli.unroute(dashboard_pattern)
        cli.goto("/observe", wait_ms=1300)

    coverage_states = set()
    for role in ("chat_only", "dual_native", "image_only"):
        model = representatives[role]
        try:
            numeric_id = int(model["id"])
        except (KeyError, TypeError, ValueError) as error:
            raise ProductAssertionError("wfl_010_model_identity_invalid") from error
        if numeric_id < 1:
            raise ProductAssertionError("wfl_010_model_identity_invalid")
        entry = snapshot_entry_with_path(
            cli,
            case_dir,
            "state-coverage-%s" % role.replace("_", "-"),
            "/route/models/%d" % numeric_id,
            wait_ms=1600,
        )
        diagnostics = api_get(cli.base_url, "/api/models/%d/routing-diagnostics" % numeric_id)
        projected = diagnostics_projection(diagnostics if isinstance(diagnostics, Mapping) else {})
        observed = sorted(
            {
                str(target.get("coverage", "")).upper()
                for target in projected.get("targets", [])
                if target.get("coverage")
            }
        )
        coverage_states.update(observed)
        snapshot_text = (case_dir / entry["snapshot"]).read_text(encoding="utf-8")
        labels_present = {
            state: COVERAGE_LABELS[state] in snapshot_text
            for state in observed
            if state in COVERAGE_LABELS
        }
        state_entries.append(
            {
                "state": "coverage",
                "surface": "model-detail",
                "role": role,
                "model_id": str(model.get("model_id", "")),
                "observed_coverages": observed,
                "coverage_labels_present": labels_present,
                **entry,
            }
        )

    cli.goto("/route/models", wait_ms=1300)
    console_text = cli.console("warning")
    write_text(case_dir / "browser-console.log", console_text)
    unexpected_console_lines = unexpected_console_fatal_lines(
        console_text,
        (
            "status of 500 (internal server error) @ %s/api/models:" % cli.base_url,
            "status of 503 (service unavailable) @ %s/api/stats/dashboard/now:" % cli.base_url,
            "matrix synthetic api error",
            "matrix synthetic delayed stale",
        ),
    )
    viewport_evidence = {"case_id": "WFL-010", "recorded_at": utc_now(), "entries": viewport_entries}
    state_evidence = {
        "case_id": "WFL-010",
        "recorded_at": utc_now(),
        "entries": state_entries,
        "observed_coverages": sorted(coverage_states),
        "browser_local_fixtures_only": True,
        "unexpected_console_fatal_lines": unexpected_console_lines,
    }
    accessibility_evidence = {
        "case_id": "WFL-010",
        "recorded_at": utc_now(),
        "method": "DOM accessible-name, keyboard-focus, viewport-reachability, and WCAG contrast baseline",
        "entries": audits,
    }
    write_json(case_dir / "viewport-snapshots.json", viewport_evidence)
    write_json(case_dir / "state-grid.json", state_evidence)
    write_json(case_dir / "accessibility-results.json", accessibility_evidence)
    failures = []
    if any(item["fatal_markers"] for item in viewport_entries + state_entries):
        failures.append("one or more state snapshots contain a fatal marker")
    if any(item.get("missing_name_count", 0) for item in audits):
        failures.append("one or more visible controls lack an accessible name")
    if any(item.get("inaccessible_control_count", 0) for item in audits):
        failures.append("one or more narrow-view controls are unreachable")
    if any(item.get("severe_contrast_count", 0) for item in audits):
        failures.append("one or more severe contrast failures were observed")
    if any(
        not bool(item.get("semantic", {}).get("empty_copy_visible"))
        for item in state_entries
        if item["state"] == "empty"
    ):
        failures.append("empty state does not expose its recovery guidance")
    if any(
        not (
            bool(item.get("semantic", {}).get("error_panel_visible"))
            and bool(item.get("semantic", {}).get("retry_visible"))
        )
        for item in state_entries
        if item["state"] == "error"
    ):
        failures.append("error state or its retry control is not visible")
    if any(
        not bool(item.get("semantic", {}).get("loading_visible"))
        for item in state_entries
        if item["state"] == "loading"
    ):
        failures.append("loading state is not visible")
    if any(
        not (
            bool(item.get("semantic", {}).get("stale_badge_visible"))
            and bool(item.get("semantic", {}).get("retained_values_visible"))
        )
        for item in state_entries
        if item["state"] == "stale"
    ):
        failures.append("delayed refresh failure did not retain and label stale data")
    missing_coverages = sorted(set(COVERAGE_LABELS) - coverage_states)
    if missing_coverages:
        failures.append("coverage fixtures are missing states: %s" % ", ".join(missing_coverages))
    if any(
        not all(item.get("coverage_labels_present", {}).values())
        for item in state_entries
        if item["state"] == "coverage"
    ):
        failures.append("one or more coverage states lack a structured UI label")
    if any(not item.get("focus_after_tab", {}).get("name") for item in audits):
        failures.append("keyboard Tab did not reach a named control on one or more pages")
    if unexpected_console_lines:
        failures.append("browser console contains a fatal marker")
    if failures:
        raise ProductAssertionError("wfl_010_semantic_oracle_failed")
    return {"case_id": "WFL-010", "passed": True, "failures": []}


READONLY_HANDLERS = {
    "WFL-001": readonly_wfl001,
    "WFL-002": readonly_wfl002,
    "WFL-010": readonly_wfl010,
}


def run_readonly_session(
    case_id: str,
    case_dir: pathlib.Path,
    base_url: str,
    session: str,
    scratch_dir: pathlib.Path,
    wrapper: pathlib.Path,
    *,
    chromium_executable: pathlib.Path,
    chromium_bundle_sha256_value: str,
    database_inventory: Optional[Mapping[str, Any]] = None,
    lifecycle_callback: Optional[Callable[[str, Mapping[str, Any]], None]] = None,
) -> Dict[str, Any]:
    """Run one read-only browser session already owned by the case owner.

    This primitive deliberately has no formal runner or service authority.  Its
    caller supplies the exact attempt, named session, private scratch path, and
    pinned browser identity, and journals lifecycle transitions through the
    callback.  Scratch is purged only after the trace (when started) is packaged
    and the named session closes successfully, leaving failed cleanup
    reconcilable by the owner receipt.
    """
    validate_case_id(case_id, READONLY_CASES)
    case_dir = validate_attempt_dir(case_dir, case_id)
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{2,62}", session):
        raise WorkflowError("readonly browser session identity is invalid")
    scratch = validate_private_path(scratch_dir, require_file=False)
    if scratch.exists() or scratch.is_symlink():
        raise WorkflowError("readonly browser scratch is not fresh")

    def emit(phase: str, **detail: Any) -> None:
        if lifecycle_callback is not None:
            lifecycle_callback(phase, safe_json_projection(detail))

    cli = PlaywrightCLI(
        session=session,
        scratch_dir=scratch,
        base_url=base_url,
        wrapper=wrapper,
        chromium_executable=chromium_executable,
        chromium_bundle_sha256_value=chromium_bundle_sha256_value,
    )
    emit("constructed", scratch_dir=str(scratch), session=session)
    product_failure: Optional[ProductAssertionError] = None
    trace: Optional[Dict[str, Any]] = None
    result: Optional[Dict[str, Any]] = None
    trace_started: Optional[int] = None
    trace_packaged = False
    closed = False
    try:
        emit("opening")
        cli.open_blank()
        emit("opened")
        emit("trace_starting")
        trace_started = cli.trace_start()
        emit("trace_active", started_ns=trace_started, started_at=utc_now())
        try:
            try:
                if case_id == "WFL-002":
                    result = readonly_wfl002(cli, case_dir, database_inventory)
                else:
                    result = READONLY_HANDLERS[case_id](cli, case_dir)
            except ProductAssertionError as error:
                product_failure = error
        finally:
            emit("trace_stopping")
            trace = cli.trace_stop(trace_started, case_dir / "trace.zip")
            trace_packaged = True
            emit("trace_packaged", trace=trace, ended_at=utc_now())
    finally:
        try:
            emit("closing")
            cli.close(strict=True)
            emit("closed", closed_at=utc_now())
            closed = True
        finally:
            if closed and (trace_started is None or trace_packaged):
                purge_private_scratch_tree(scratch)
                emit("purged", purged_at=utc_now())
    if product_failure is not None:
        result = record_readonly_product_failure(case_id, case_dir, product_failure.code)
    if result is None or trace is None:
        raise WorkflowError("readonly result finalization is incomplete")
    result["trace"] = trace
    validate_readonly_inventory(case_id, case_dir)
    result["evidence"] = inventory(case_dir)
    result["private_scratch_removed"] = not scratch.exists() and not scratch.is_symlink()
    return result


def validate_readonly_inventory(case_id: str, case_dir: pathlib.Path) -> None:
    allowed_top_level = {"result.json", *REQUIRED_EVIDENCE[case_id], "snapshots", "screenshots"}
    for path in case_dir.rglob("*"):
        if path.is_symlink():
            raise WorkflowError("readonly evidence inventory contains a symlink")
        relative = path.relative_to(case_dir)
        if relative.parts[0] not in allowed_top_level:
            raise WorkflowError("readonly evidence inventory contains an unexpected artifact")
        if path.is_dir() and (len(relative.parts) != 1 or relative.name not in {"snapshots", "screenshots"}):
            raise WorkflowError("readonly evidence inventory contains an unexpected directory")
        if path.is_file() and relative.parts[0] == "snapshots" and (
            len(relative.parts) != 2 or not relative.name.endswith(".snapshot.txt")
        ):
            raise WorkflowError("readonly snapshot inventory is invalid")
        if path.is_file() and relative.parts[0] == "screenshots" and (
            len(relative.parts) != 2 or path.suffix != ".png"
        ):
            raise WorkflowError("readonly screenshot inventory is invalid")
        if path.is_file() and path.stat().st_size == 0:
            raise WorkflowError("readonly evidence inventory contains an empty artifact")
        if path.is_file() and relative.parts[0] == "snapshots":
            assert_no_remaining_secret(
                path.read_text(encoding="utf-8", errors="strict"),
                relative.as_posix(),
            )
    for name in REQUIRED_EVIDENCE[case_id]:
        path = case_dir / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
            raise WorkflowError("readonly required evidence is incomplete")
        if path.suffix == ".json":
            try:
                value = json.loads(path.read_text(encoding="utf-8"))
            except (UnicodeError, json.JSONDecodeError) as error:
                raise WorkflowError("readonly JSON evidence is invalid") from error
            if not isinstance(value, dict) or value.get("case_id") != case_id:
                raise WorkflowError("readonly JSON evidence has no exact case binding")
            assert_safe_json(value, name)
        elif path.suffix in {".txt", ".log"}:
            assert_no_remaining_secret(path.read_text(encoding="utf-8", errors="strict"), name)
        elif name == "trace.zip":
            validate_trace_archive(path, require_redaction_manifest=True)


def record_readonly_product_failure(
    case_id: str,
    case_dir: pathlib.Path,
    code: str,
) -> Dict[str, Any]:
    """Fill every missing frozen file with an explicit readonly failure envelope."""
    if not re.fullmatch(r"[a-z][a-z0-9_]{2,95}", code):
        raise WorkflowError("readonly product failure code is invalid")
    created = []
    for name in REQUIRED_EVIDENCE[case_id]:
        target = case_dir / name
        if target.exists() or target.is_symlink():
            continue
        document = failure_evidence_document(case_id, name, code)
        if name == "trace.zip":
            write_failure_trace(target, case_id, code, ())
        elif target.suffix == ".json":
            write_json(target, document)
        elif target.suffix in {".txt", ".log"}:
            write_text(target, json.dumps(document, ensure_ascii=False, sort_keys=True) + "\n")
        else:
            raise WorkflowError("unsupported readonly failure evidence type")
        created.append(name)
    return {
        "case_id": case_id,
        "passed": False,
        "product_failed": True,
        "failure_code": code,
        "failures": [code],
        "failure_evidence_created": created,
    }


def state_path(case_dir: pathlib.Path) -> pathlib.Path:
    return case_dir / ".workflow-helper" / "state.json"


@contextlib.contextmanager
def case_lock(case_dir: pathlib.Path):
    lock_path = case_dir.resolve() / ".workflow-helper" / "case.lock"
    ensure_private_directory(lock_path.parent)
    descriptor = os.open(str(lock_path), os.O_RDWR | os.O_CREAT, 0o600)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise WorkflowError("another command already owns this case helper") from error
        os.fchmod(descriptor, 0o600)
        yield
    finally:
        with contextlib.suppress(OSError):
            fcntl.flock(descriptor, fcntl.LOCK_UN)
        os.close(descriptor)


def save_state(case_dir: pathlib.Path, value: Mapping[str, Any]) -> None:
    atomic_write(state_path(case_dir), (json.dumps(value, sort_keys=True, indent=2) + "\n").encode("utf-8"))


def verify_redaction_seal(case_dir: pathlib.Path, state: Mapping[str, Any]) -> None:
    seal = state.get("redaction_seal")
    if not isinstance(seal, dict) or seal.get("schema_version") != 1:
        raise WorkflowError("private redaction input disappeared before evidence was sealed")
    artifacts = seal.get("artifacts")
    if not isinstance(artifacts, list):
        raise WorkflowError("redaction seal is invalid")
    expected_names = set(REQUIRED_EVIDENCE[str(state["case_id"])])
    sealed_names = set()
    for item in artifacts:
        if not isinstance(item, dict):
            raise WorkflowError("redaction seal is invalid")
        name = item.get("path")
        path = case_dir / str(name)
        if name not in expected_names or path.is_symlink() or not path.is_file():
            raise WorkflowError("sealed evidence is missing or unsafe")
        if item.get("sha256") != sha256_file(path) or item.get("bytes") != path.stat().st_size:
            raise WorkflowError("sealed evidence changed after private values were destroyed")
        sealed_names.add(str(name))
    if sealed_names != expected_names:
        raise WorkflowError("redaction seal does not cover the exact evidence contract")


def private_values_for_state(case_dir: pathlib.Path, state: Mapping[str, Any]) -> Tuple[str, ...]:
    redaction_path = state.get("redaction_file")
    if redaction_path is None:
        return ()
    path = pathlib.Path(str(redaction_path))
    if path.is_file() and not path.is_symlink():
        return load_private_values(path)
    verify_redaction_seal(case_dir, state)
    return ()


def load_state(case_dir: pathlib.Path, *, verify_chromium: bool = True) -> Dict[str, Any]:
    path = state_path(case_dir)
    if not path.is_file():
        raise WorkflowError("case helper is not initialized: %s" % case_dir)
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise WorkflowError("invalid helper state")
    case_id = validate_case_id(str(value.get("case_id", "")), HELPER_CASES)
    if (
        value.get("schema_version") != 2
        or value.get("matrix_sha256") != MATRIX_SHA256
        or pathlib.Path(str(value.get("case_dir", ""))).resolve() != case_dir.resolve()
        or value.get("phase")
        not in {
            "initializing",
            "initialized",
            "trace_starting",
            "trace_active",
            "trace_stopping",
            "trace_packaged",
            "closed",
        }
    ):
        raise WorkflowError("helper state identity is invalid")
    validate_attempt_dir(case_dir, case_id)
    chromium_text = str(value.get("chromium_executable", ""))
    chromium_path = pathlib.Path(chromium_text)
    chromium_digest = str(value.get("chromium_bundle_sha256", ""))
    if not chromium_path.is_absolute() or not re.fullmatch(r"[0-9a-f]{64}", chromium_digest):
        raise WorkflowError("helper Chromium identity is invalid")
    if verify_chromium:
        chromium = validate_chromium_executable(chromium_path)
        if str(chromium) != chromium_text:
            raise WorkflowError("helper Chromium identity is invalid")
    fixture = value.get("fixture")
    if not isinstance(fixture, dict):
        raise WorkflowError("helper state has no fixture binding")
    fixture_path = validate_private_path(pathlib.Path(str(fixture.get("path", ""))))
    if fixture.get("sha256") != sha256_file(fixture_path):
        raise WorkflowError("fixture manifest changed after case initialization")
    redaction_path = value.get("redaction_file")
    if redaction_path is not None:
        private_values_for_state(case_dir, value)
    completed = value.get("completed_checkpoints")
    expected = CASE_CHECKPOINTS[case_id]
    outcome = value.get("outcome", "running")
    if outcome not in {"running", "passed", "product_failed"}:
        raise WorkflowError("helper outcome state is invalid")
    if outcome == "product_failed" and not re.fullmatch(
        r"[a-z0-9][a-z0-9_.-]{2,95}", str(value.get("failure_code", ""))
    ):
        raise WorkflowError("helper product failure state is invalid")
    if not isinstance(completed, list) or tuple(completed) != expected[: len(completed)]:
        raise WorkflowError("helper checkpoint state is invalid")
    records = value.get("checkpoint_records")
    if (
        not isinstance(records, list)
        or [item.get("name") for item in records if isinstance(item, Mapping)] != completed
        or len(records) != len(completed)
        or any(not isinstance(item.get("recorded_at"), str) for item in records if isinstance(item, Mapping))
    ):
        raise WorkflowError("helper checkpoint journal is invalid")
    trace_history = value.get("trace_history", [])
    if not isinstance(trace_history, list) or any(not isinstance(item, Mapping) for item in trace_history):
        raise WorkflowError("trace lifecycle journal is invalid")
    for index, item in enumerate(trace_history):
        expected_event = "started" if index % 2 == 0 else "packaged"
        if (
            item.get("event") != expected_event
            or isinstance(item.get("started_ns"), bool)
            or not isinstance(item.get("started_ns"), int)
            or int(item["started_ns"]) < 1
            or not isinstance(item.get("recorded_at"), str)
        ):
            raise WorkflowError("trace lifecycle journal is invalid")
        if expected_event == "packaged" and item.get("started_ns") != trace_history[index - 1].get("started_ns"):
            raise WorkflowError("trace lifecycle journal is invalid")
    active_started = value.get("trace_started_ns")
    if active_started is None:
        if len(trace_history) % 2 != 0:
            raise WorkflowError("trace lifecycle journal is invalid")
    elif (
        isinstance(active_started, bool)
        or not isinstance(active_started, int)
        or not trace_history
        or trace_history[-1].get("event") != "started"
        or trace_history[-1].get("started_ns") != active_started
    ):
        raise WorkflowError("trace lifecycle journal is invalid")
    return value


def cli_from_state(case_dir: pathlib.Path, wrapper: pathlib.Path) -> Tuple[PlaywrightCLI, Dict[str, Any]]:
    state = load_state(case_dir)
    return (
        PlaywrightCLI(
            session=str(state["session"]),
            scratch_dir=pathlib.Path(str(state["scratch_dir"])),
            base_url=str(state["base_url"]),
            wrapper=wrapper,
            redaction_file=(
                pathlib.Path(str(state["redaction_file"]))
                if state.get("redaction_file") is not None
                else None
            ),
            chromium_executable=pathlib.Path(str(state["chromium_executable"])),
            chromium_bundle_sha256_value=str(state["chromium_bundle_sha256"]),
        ),
        state,
    )


def close_persisted_session(case_dir: pathlib.Path, wrapper: pathlib.Path) -> Dict[str, Any]:
    """Close an existing CLI session without requiring the browser bundle."""
    state = load_state(case_dir, verify_chromium=False)
    session = str(state.get("session", ""))
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{2,62}", session):
        raise WorkflowError("persisted Playwright session identity is invalid")
    wrapper = wrapper.resolve()
    if wrapper.is_symlink() or not wrapper.is_file():
        raise WorkflowError("playwright CLI wrapper is missing")
    scratch = validate_private_path(pathlib.Path(str(state.get("scratch_dir", ""))), require_file=False)
    if not scratch.is_dir() or scratch.is_symlink():
        raise WorkflowError("persisted Playwright scratch directory is invalid")
    private_values = private_values_for_state(case_dir, state)
    environment = playwright_subprocess_environment()
    process = subprocess.run(
        [str(wrapper), "--session=%s" % session, "close"],
        cwd=str(scratch),
        env=environment,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        timeout=60,
        check=False,
    )
    output = redact_text(process.stdout or "", private_values)
    assert_no_remaining_secret(output, "playwright close output", private_values)
    if process.returncode != 0:
        raise WorkflowError("persisted Playwright session close failed")
    return {"case_id": state["case_id"], "closed": True}


def close_named_session(
    session: str,
    scratch_dir: pathlib.Path,
    wrapper: pathlib.Path,
) -> Dict[str, Any]:
    """Strictly close an owner-journaled read-only session for reconciliation."""
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{2,62}", session):
        raise WorkflowError("persisted Playwright session identity is invalid")
    wrapper = wrapper.resolve()
    if wrapper.is_symlink() or not wrapper.is_file():
        raise WorkflowError("playwright CLI wrapper is missing")
    scratch = validate_private_path(scratch_dir, require_file=False)
    if not scratch.is_dir() or scratch.is_symlink():
        raise WorkflowError("persisted Playwright scratch directory is invalid")
    process = subprocess.run(
        [str(wrapper), "--session=%s" % session, "close"],
        cwd=str(scratch),
        env=playwright_subprocess_environment(),
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        timeout=60,
        check=False,
    )
    output = redact_text(process.stdout or "")
    assert_no_remaining_secret(output, "playwright close output")
    if process.returncode != 0:
        raise WorkflowError("persisted Playwright session close failed")
    return {"session": session, "closed": True}


def helper_init(args: argparse.Namespace) -> Dict[str, Any]:
    frozen_workflow_contract()
    case_id = validate_case_id(args.case_id, HELPER_CASES)
    case_dir = validate_attempt_dir(args.case_dir, case_id)
    ensure_private_directory(case_dir)
    if state_path(case_dir).exists():
        raise WorkflowError("case helper is already initialized")
    base_url = validate_loopback_origin(args.base_url)
    fixture = load_fixture_manifest(args.fixture_manifest, case_id, base_url)
    redaction_file = args.private_values_file
    if case_id in SENSITIVE_CASES and redaction_file is None:
        raise WorkflowError("this case requires a private redaction value file")
    if redaction_file is not None:
        load_private_values(redaction_file)
        redaction_file = validate_private_path(redaction_file)
    default_scratch = RUN_PRIVATE / "workflow" / case_id.lower() / case_dir.name / "playwright"
    scratch = validate_private_path(args.scratch_dir or default_scratch, require_file=False)
    ensure_private_directory(scratch)
    session = args.session or ("prism-wfl-helper-%s-%d" % (case_id.lower(), os.getpid()))
    selected_chromium = getattr(args, "chromium_executable", None) or discover_local_chromium()
    if selected_chromium is None:
        raise WorkflowError("no already-installed full Chromium build was found")
    selected_chromium = validate_chromium_executable(pathlib.Path(selected_chromium))
    selected_chromium_digest = getattr(args, "chromium_bundle_sha256", None)
    actual_chromium_digest = chromium_bundle_sha256(selected_chromium)
    if selected_chromium_digest is None:
        selected_chromium_digest = actual_chromium_digest
    if selected_chromium_digest != actual_chromium_digest:
        raise WorkflowError("pinned Chromium bundle changed before helper initialization")
    state = {
        "schema_version": 2,
        "matrix_sha256": MATRIX_SHA256,
        "case_id": case_id,
        "case_dir": str(case_dir),
        "scratch_dir": str(scratch),
        "session": session,
        "base_url": base_url,
        "chromium_executable": str(selected_chromium),
        "chromium_bundle_sha256": selected_chromium_digest,
        "fixture": fixture,
        "redaction_file": str(redaction_file) if redaction_file is not None else None,
        "started_at": utc_now(),
        "phase": "initializing",
        "outcome": "running",
        "failure_code": None,
        "trace_started_ns": None,
        "trace_history": [],
        "sensitive_ui_cleared": False,
        "snapshots": [],
        "completed_checkpoints": ["fixture_verified"],
        "checkpoint_records": [{"name": "fixture_verified", "recorded_at": utc_now()}],
        "resume_count": 0,
    }
    save_state(case_dir, state)
    cli = PlaywrightCLI(
        session=session,
        scratch_dir=scratch,
        base_url=base_url,
        wrapper=args.wrapper,
        redaction_file=redaction_file,
        chromium_executable=selected_chromium,
        chromium_bundle_sha256_value=selected_chromium_digest,
    )
    cli.open_blank()
    state["phase"] = "initialized"
    save_state(case_dir, state)
    return {
        "case_id": case_id,
        "session": session,
        "case_dir": str(case_dir),
        "database_clone": fixture["database_clone"],
        "next_checkpoint": CASE_CHECKPOINTS[case_id][1],
        "ready": True,
    }


def helper_trace_start(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    cli, state = cli_from_state(case_dir, args.wrapper)
    if state.get("trace_started_ns") is not None:
        raise WorkflowError("trace is already active")
    if (case_dir / "trace.zip").exists():
        raise WorkflowError("trace evidence already exists")
    if state["case_id"] in {"WFL-004", "WFL-008"}:
        if not (args.confirmed_dialog_closed or args.sensitive_ui_cleared):
            raise WorkflowError("trace may start only after sensitive UI is cleared")
        if state["case_id"] == "WFL-004" and "sensitive_ui_cleared" not in state["completed_checkpoints"]:
            raise WorkflowError("record the sensitive_ui_cleared checkpoint before tracing")
        state["sensitive_ui_cleared"] = True
    state["trace_started_ns"] = time.time_ns()
    started_at = utc_now()
    history = state.setdefault("trace_history", [])
    if not isinstance(history, list):
        raise WorkflowError("trace lifecycle journal is invalid")
    history.append(
        {
            "event": "started",
            "started_ns": state["trace_started_ns"],
            "recorded_at": started_at,
        }
    )
    state["phase"] = "trace_starting"
    save_state(case_dir, state)
    cli.trace_start()
    state["phase"] = "trace_active"
    save_state(case_dir, state)
    return {
        "case_id": state["case_id"],
        "trace_active": True,
        "started_ns": state["trace_started_ns"],
        "started_at": started_at,
    }


def helper_goto(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    cli, state = cli_from_state(case_dir, args.wrapper)
    output = cli.goto(args.path, wait_ms=args.wait_ms)
    final_url, title = parse_page_metadata(output)
    return {"case_id": state["case_id"], "final_url": final_url or "", "title": title or ""}


def helper_snapshot(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    cli, state = cli_from_state(case_dir, args.wrapper)
    entry = cli.snapshot(args.label, case_dir)
    if args.evidence_name:
        if args.evidence_name not in REQUIRED_EVIDENCE[state["case_id"]]:
            raise WorkflowError("evidence name is not required by %s" % state["case_id"])
        if not args.evidence_name.endswith(".txt"):
            raise WorkflowError("direct snapshot evidence must use a required .txt filename")
        source = case_dir / entry["snapshot"]
        target = case_dir / args.evidence_name
        if target.exists():
            raise WorkflowError("refusing to overwrite evidence: %s" % target)
        write_text(target, source.read_text(encoding="utf-8"))
        entry["evidence_name"] = args.evidence_name
    snapshots = state.setdefault("snapshots", [])
    snapshots.append(entry)
    save_state(case_dir, state)
    return entry


def helper_snapshot_index(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    state = load_state(case_dir)
    if args.evidence_name not in SNAPSHOT_INDEX_NAMES or args.evidence_name not in REQUIRED_EVIDENCE[state["case_id"]]:
        raise WorkflowError("invalid snapshot-index evidence name for %s" % state["case_id"])
    entries = list(state.get("snapshots", []))
    if args.label:
        wanted = set(args.label)
        entries = [entry for entry in entries if entry.get("label") in wanted]
        missing = sorted(wanted - {entry.get("label") for entry in entries})
        if missing:
            raise WorkflowError("unknown snapshot labels: %s" % ", ".join(missing))
    if not entries:
        raise WorkflowError("snapshot index would be empty")
    value = {"case_id": state["case_id"], "recorded_at": utc_now(), "entries": entries}
    target = case_dir / args.evidence_name
    if target.exists():
        raise WorkflowError("refusing to overwrite evidence: %s" % target)
    write_json(target, value)
    return {"case_id": state["case_id"], "evidence_name": args.evidence_name, "entry_count": len(entries)}


def helper_console(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    cli, state = cli_from_state(case_dir, args.wrapper)
    target_name = args.evidence_name or "browser-console.log"
    if target_name not in REQUIRED_EVIDENCE[state["case_id"]]:
        raise WorkflowError("console evidence name is not required by %s" % state["case_id"])
    target = case_dir / target_name
    if target.exists():
        raise WorkflowError("refusing to overwrite evidence: %s" % target)
    output = cli.console(args.minimum)
    write_text(target, output)
    return {"case_id": state["case_id"], "evidence_name": target_name, "fatal_markers": body_has_fatal_marker(output)}


def helper_trace_stop(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    cli, state = cli_from_state(case_dir, args.wrapper)
    started_ns = state.get("trace_started_ns")
    if not isinstance(started_ns, int):
        raise WorkflowError("trace is not active")
    state["phase"] = "trace_stopping"
    save_state(case_dir, state)
    result = cli.trace_stop(started_ns, case_dir / "trace.zip")
    ended_at = utc_now()
    history = state.setdefault("trace_history", [])
    if not isinstance(history, list):
        raise WorkflowError("trace lifecycle journal is invalid")
    history.append(
        {
            "event": "packaged",
            "started_ns": started_ns,
            "recorded_at": ended_at,
            "path": result.get("path"),
            "bytes": result.get("bytes"),
            "sha256": result.get("sha256"),
        }
    )
    state["trace_started_ns"] = None
    state["phase"] = "trace_packaged"
    save_state(case_dir, state)
    return {
        "case_id": state["case_id"],
        "trace": result,
        "started_ns": started_ns,
        "ended_at": ended_at,
    }


def helper_close(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    cli, state = cli_from_state(case_dir, args.wrapper)
    if state.get("trace_started_ns") is not None:
        raise WorkflowError("stop and package the active trace before closing")
    cli.close(strict=True)
    state["closed_at"] = utc_now()
    state["phase"] = "closed"
    state["private_scratch_removed"] = False
    if tuple(state.get("completed_checkpoints", ())) == CASE_CHECKPOINTS[state["case_id"]]:
        state["outcome"] = "passed"
    save_state(case_dir, state)
    purge_private_scratch_tree(cli.scratch_dir)
    state["private_scratch_removed"] = True
    save_state(case_dir, state)
    return {"case_id": state["case_id"], "closed": True}


def helper_checkpoint(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    state = load_state(case_dir)
    case_id = state["case_id"]
    expected = CASE_CHECKPOINTS[case_id]
    completed = list(state["completed_checkpoints"])
    if len(completed) >= len(expected):
        raise WorkflowError("all case checkpoints are already complete")
    next_checkpoint = expected[len(completed)]
    if args.name != next_checkpoint:
        raise WorkflowError("checkpoint is out of order; next is %s" % next_checkpoint)
    completed.append(args.name)
    state["completed_checkpoints"] = completed
    recorded_at = utc_now()
    state.setdefault("checkpoint_records", []).append({"name": args.name, "recorded_at": recorded_at})
    state["last_checkpoint_at"] = recorded_at
    save_state(case_dir, state)
    return {
        "case_id": case_id,
        "recorded": args.name,
        "completed": len(completed),
        "total": len(expected),
        "next_checkpoint": expected[len(completed)] if len(completed) < len(expected) else None,
    }


def reconcile_interrupted_state(case_dir: pathlib.Path, state: Dict[str, Any]) -> List[str]:
    recovered = []
    known = {str(entry.get("snapshot")) for entry in state.get("snapshots", []) if isinstance(entry, Mapping)}
    snapshot_root = case_dir / "snapshots"
    if snapshot_root.is_dir():
        for path in sorted(snapshot_root.glob("*.snapshot.txt")):
            relative = str(path.relative_to(case_dir))
            if relative in known:
                continue
            content = path.read_text(encoding="utf-8", errors="replace")
            assert_no_remaining_secret(content, "recovered snapshot", private_values_for_state(case_dir, state))
            state.setdefault("snapshots", []).append(
                {
                    "label": path.name.removesuffix(".snapshot.txt"),
                    "final_url": "",
                    "title": "",
                    "snapshot": relative,
                    "bytes": path.stat().st_size,
                    "sha256": sha256_file(path),
                    "fatal_markers": body_has_fatal_marker(content),
                    "recovered_after_interruption": True,
                }
            )
            recovered.append(relative)
    trace = case_dir / "trace.zip"
    if trace.is_file() and state.get("phase") != "trace_packaged":
        private_values = private_values_for_state(case_dir, state)
        findings = validate_trace_archive(trace, private_values, require_redaction_manifest=True)
        started_ns = state.get("trace_started_ns")
        history = state.setdefault("trace_history", [])
        if isinstance(started_ns, int) and isinstance(history, list):
            history.append(
                {
                    "event": "packaged",
                    "started_ns": started_ns,
                    "recorded_at": utc_now(),
                    "path": trace.name,
                    "bytes": trace.stat().st_size,
                    "sha256": sha256_file(trace),
                    "recovered_after_interruption": True,
                    "retained_entries": findings.get("retained_entries"),
                }
            )
        state["trace_started_ns"] = None
        state["phase"] = "trace_packaged"
        recovered.append("trace.zip")
    return recovered


def safe_state_projection(state: Mapping[str, Any]) -> Dict[str, Any]:
    case_id = str(state["case_id"])
    expected = CASE_CHECKPOINTS[case_id]
    completed = list(state.get("completed_checkpoints", []))
    return {
        "case_id": case_id,
        "phase": state.get("phase"),
        "session": state.get("session"),
        "database_clone": state.get("fixture", {}).get("database_clone"),
        "fixture_sha256": state.get("fixture", {}).get("sha256"),
        "matrix_sha256": state.get("matrix_sha256"),
        "trace_active": isinstance(state.get("trace_started_ns"), int),
        "snapshot_count": len(state.get("snapshots", [])),
        "completed_checkpoints": completed,
        "next_checkpoint": expected[len(completed)] if len(completed) < len(expected) else None,
        "resume_count": state.get("resume_count", 0),
    }


def helper_status(args: argparse.Namespace) -> Dict[str, Any]:
    return safe_state_projection(load_state(args.case_dir.resolve()))


def helper_resume(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    state = load_state(case_dir)
    if state.get("phase") == "closed":
        raise WorkflowError("closed case helper cannot be resumed")
    recovered = reconcile_interrupted_state(case_dir, state)
    if state.get("phase") == "initializing":
        cli, _ = cli_from_state(case_dir, args.wrapper)
        cli.open_blank()
        state["phase"] = "initialized"
        recovered.append("browser_session")
    elif state.get("phase") == "trace_starting":
        state["phase"] = "trace_active"
        recovered.append("trace_start_state")
    state["resume_count"] = int(state.get("resume_count", 0)) + 1
    state["last_resumed_at"] = utc_now()
    save_state(case_dir, state)
    projection = safe_state_projection(state)
    projection["recovered_artifacts"] = recovered
    return projection


def validate_auxiliary_snapshots(
    case_dir: pathlib.Path,
    state: Mapping[str, Any],
    private_values: Sequence[str],
) -> List[Dict[str, Any]]:
    expected = {}
    for entry in state.get("snapshots", []):
        if not isinstance(entry, Mapping):
            raise WorkflowError("snapshot state is invalid")
        relative = pathlib.PurePosixPath(str(entry.get("snapshot", "")))
        if relative.is_absolute() or ".." in relative.parts or relative.parts[:1] != ("snapshots",):
            raise WorkflowError("snapshot state escapes the case directory")
        expected[relative.as_posix()] = entry
    findings = []
    required = set(REQUIRED_EVIDENCE[str(state["case_id"])])
    observed = set()
    for path in sorted(case_dir.rglob("*")):
        if not path.is_file():
            continue
        relative = path.relative_to(case_dir).as_posix()
        if relative.startswith(".workflow-helper/") or relative in required:
            continue
        if relative == "result.json":
            result_text = path.read_text(encoding="utf-8", errors="strict")
            assert_no_remaining_secret(result_text, relative, private_values)
            continue
        if relative not in expected or path.is_symlink() or path.suffix != ".txt" or path.stat().st_size == 0:
            raise WorkflowError("unexpected or unsafe auxiliary case artifact: %s" % relative)
        text = path.read_text(encoding="utf-8", errors="strict")
        assert_no_remaining_secret(text, relative, private_values)
        entry = expected[relative]
        if entry.get("sha256") != sha256_file(path) or entry.get("bytes") != path.stat().st_size:
            raise WorkflowError("snapshot changed after it was recorded: %s" % relative)
        observed.add(relative)
        findings.append({"path": relative, "bytes": path.stat().st_size, "sha256": sha256_file(path)})
    if observed != set(expected):
        raise WorkflowError("recorded snapshot evidence is missing")
    return findings


def failure_evidence_document(case_id: str, name: str, code: str) -> Dict[str, Any]:
    if not re.fullmatch(r"[a-z0-9][a-z0-9_.-]{2,95}", code):
        raise WorkflowError("product failure code is invalid")
    return {
        "schema_version": 1,
        "case_id": case_id,
        "artifact": name,
        "status": "failed",
        "failure_code": code,
        "assertions": [
            {
                "name": code,
                "passed": False,
                "expected": True,
                "actual": False,
            }
        ],
        "assertion_summary": {"total": 1, "passed": 0, "failed": 1},
    }


def write_failure_trace(
    target: pathlib.Path,
    case_id: str,
    code: str,
    private_values: Sequence[str],
) -> None:
    if target.exists() or target.is_symlink():
        raise WorkflowError("refusing to overwrite trace evidence")
    document = failure_evidence_document(case_id, target.name, code)
    assert_safe_json(document, "failure_trace", private_values)
    payload = (json.dumps(document, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
    descriptor, temporary_name = tempfile.mkstemp(prefix=".failure-trace.", dir=str(target.parent))
    os.close(descriptor)
    temporary = pathlib.Path(temporary_name)
    try:
        with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            archive.writestr("trace.trace", payload)
            archive.writestr("trace.network", payload)
            archive.writestr(
                "trace-redaction.json",
                json.dumps(
                    {
                        "schema_version": 1,
                        "sanitizer": "workflow_playwright_text_only_v1",
                        "binary_resource_policy": "omitted",
                        "retained_text_resources": 0,
                        "omitted_binary_resources": 0,
                        "private_value_count": len(private_values),
                        "failure_envelope": True,
                    },
                    sort_keys=True,
                    separators=(",", ":"),
                )
                + "\n",
            )
        os.chmod(temporary, 0o600)
        os.replace(temporary, target)
        validate_trace_archive(target, private_values, require_redaction_manifest=True)
    except Exception:
        with contextlib.suppress(FileNotFoundError):
            target.unlink()
        raise
    finally:
        with contextlib.suppress(FileNotFoundError):
            temporary.unlink()


def record_product_failure(case_dir: pathlib.Path, code: str) -> Dict[str, Any]:
    """Seal missing frozen artifacts as explicit product-failure envelopes."""
    state = load_state(case_dir)
    case_id = str(state["case_id"])
    if state.get("phase") != "closed" or state.get("trace_started_ns") is not None:
        raise WorkflowError("browser must be closed before recording product failure")
    document_names = []
    private_values = private_values_for_state(case_dir, state)
    for name in REQUIRED_EVIDENCE[case_id]:
        target = case_dir / name
        if target.exists() or target.is_symlink():
            continue
        document = failure_evidence_document(case_id, name, code)
        if name == "trace.zip":
            write_failure_trace(target, case_id, code, private_values)
        elif target.suffix == ".json":
            write_json(target, document, private_values)
        elif target.suffix in {".txt", ".log"}:
            text = json.dumps(document, ensure_ascii=False, sort_keys=True) + "\n"
            assert_no_remaining_secret(text, name, private_values)
            write_text(target, text)
        else:
            raise WorkflowError("unsupported frozen failure evidence type")
        document_names.append(name)
    state = load_state(case_dir)
    state["outcome"] = "product_failed"
    state["failure_code"] = code
    state["failure_recorded_at"] = utc_now()
    save_state(case_dir, state)
    return {"case_id": case_id, "failure_code": code, "created": document_names}


def helper_check(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    state = load_state(case_dir)
    case_id = state["case_id"]
    if args.case_id is not None and args.case_id != case_id:
        raise WorkflowError("explicit case id does not match helper state")
    frozen_workflow_contract()
    private_values = private_values_for_state(case_dir, state)
    auxiliary_findings = validate_auxiliary_snapshots(case_dir, state, private_values)
    missing = [name for name in REQUIRED_EVIDENCE[case_id] if not (case_dir / name).is_file()]
    findings = []
    for name in REQUIRED_EVIDENCE[case_id]:
        path = case_dir / name
        if not path.is_file():
            continue
        if path.is_symlink() or path.stat().st_size == 0:
            raise WorkflowError("required evidence is empty or unsafe: %s" % name)
        if path.suffix == ".json":
            value = json.loads(path.read_text(encoding="utf-8"))
            if not isinstance(value, dict) or value.get("case_id") != case_id:
                raise WorkflowError("JSON evidence has no exact case binding: %s" % name)
            assert_safe_json(value, name, private_values)
        elif path.suffix in (".txt", ".log"):
            text = path.read_text(encoding="utf-8", errors="replace")
            assert_no_remaining_secret(text, name, private_values)
        elif name == "trace.zip":
            validate_trace_archive(path, private_values, require_redaction_manifest=True)
        findings.append({"path": name, "bytes": path.stat().st_size, "sha256": sha256_file(path)})
    checkpoint_complete = tuple(state["completed_checkpoints"]) == CASE_CHECKPOINTS[case_id]
    failure_code = state.get("failure_code")
    product_failed = (
        state.get("outcome") == "product_failed"
        and isinstance(failure_code, str)
        and re.fullmatch(r"[a-z0-9][a-z0-9_.-]{2,95}", failure_code) is not None
    )
    trace_boundary_ok = (
        case_id not in {"WFL-004", "WFL-008"}
        or state.get("sensitive_ui_cleared") is True
        or product_failed
    )
    lifecycle_closed = state.get("phase") == "closed"
    scratch_path = pathlib.Path(str(state.get("scratch_dir", "")))
    private_scratch_removed = (
        scratch_path.is_absolute()
        and not scratch_path.exists()
        and not scratch_path.is_symlink()
    )
    redaction_path = pathlib.Path(str(state["redaction_file"])) if state.get("redaction_file") else None
    private_cleanup_ok = (
        case_id not in SENSITIVE_CASES
        or (
            state.get("redaction_seal") is not None
            and redaction_path is not None
            and not redaction_path.exists()
            and not redaction_path.is_symlink()
        )
    )
    complete = (
        not missing
        and (checkpoint_complete or product_failed)
        and trace_boundary_ok
        and lifecycle_closed
        and private_scratch_removed
        and private_cleanup_ok
    )
    return {
        "case_id": case_id,
        "complete": complete,
        "missing": missing,
        "checkpoint_complete": checkpoint_complete,
        "trace_boundary_ok": trace_boundary_ok,
        "lifecycle_closed": lifecycle_closed,
        "private_scratch_removed": private_scratch_removed,
        "private_cleanup_ok": private_cleanup_ok,
        "product_failed": product_failed,
        "failure_code": failure_code if product_failed else None,
        "evidence": findings,
        "auxiliary_snapshots": auxiliary_findings,
    }


def helper_seal_redaction(args: argparse.Namespace) -> Dict[str, Any]:
    case_dir = args.case_dir.resolve()
    state = load_state(case_dir)
    if state.get("redaction_file") is None:
        raise WorkflowError("case has no private redaction input to seal")
    if state.get("redaction_seal") is not None:
        raise WorkflowError("private redaction evidence is already sealed")
    product_failed = (
        state.get("outcome") == "product_failed"
        and isinstance(state.get("failure_code"), str)
        and re.fullmatch(r"[a-z0-9][a-z0-9_.-]{2,95}", str(state.get("failure_code"))) is not None
    )
    if state.get("phase") != "closed" or (
        tuple(state["completed_checkpoints"]) != CASE_CHECKPOINTS[state["case_id"]]
        and not product_failed
    ):
        raise WorkflowError("close the case and complete every checkpoint before sealing")
    private_values = private_values_for_state(case_dir, state)
    validate_auxiliary_snapshots(case_dir, state, private_values)
    artifacts = []
    for name in REQUIRED_EVIDENCE[state["case_id"]]:
        path = case_dir / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
            raise WorkflowError("cannot seal incomplete evidence")
        if path.suffix == ".json":
            value = json.loads(path.read_text(encoding="utf-8"))
            if not isinstance(value, dict) or value.get("case_id") != state["case_id"]:
                raise WorkflowError("cannot seal unbound JSON evidence")
            assert_safe_json(value, name, private_values)
        elif path.suffix in (".txt", ".log"):
            assert_no_remaining_secret(path.read_text(encoding="utf-8", errors="replace"), name, private_values)
        elif name == "trace.zip":
            validate_trace_archive(path, private_values, require_redaction_manifest=True)
        artifacts.append({"path": name, "bytes": path.stat().st_size, "sha256": sha256_file(path)})
    state["redaction_seal"] = {
        "schema_version": 1,
        "sealed_at": utc_now(),
        "artifacts": artifacts,
        "private_values_may_be_destroyed": True,
    }
    save_state(case_dir, state)
    return {
        "case_id": state["case_id"],
        "sealed": True,
        "artifact_count": len(artifacts),
        "private_values_may_be_destroyed": True,
    }


def sanitize_trace_file(args: argparse.Namespace) -> Dict[str, Any]:
    private_values = load_private_values(args.private_values_file)
    target = args.output.resolve()
    if target.name != "trace.zip" and target.suffix != ".zip":
        raise WorkflowError("sanitized trace output must be a zip file")
    allowed_private = False
    with contextlib.suppress(ValueError):
        target.relative_to(RUN_PRIVATE.resolve())
        allowed_private = True
    allowed_attempt = False
    if args.case_id is not None:
        attempt = validate_attempt_dir(target.parent, args.case_id)
        allowed_attempt = target == attempt / "trace.zip"
    if not allowed_private and not allowed_attempt:
        raise WorkflowError("sanitized trace output must be private or the exact case trace.zip")
    return sanitize_trace_zip(args.input, target, private_values)


def helper_contract(args: argparse.Namespace) -> Dict[str, Any]:
    contract = frozen_workflow_contract()
    case_id = validate_case_id(args.case_id, HELPER_CASES)
    return {
        "case_id": case_id,
        "matrix_sha256": MATRIX_SHA256,
        "required_evidence": list(contract[case_id]),
        "checkpoints": list(CASE_CHECKPOINTS[case_id]),
        "disposable_database": CASE_DATABASE_PREFIX + case_id.lower().replace("-", "_"),
        "sensitive_private_values_required": case_id in SENSITIVE_CASES,
        "single_case_only": True,
    }


def validate_case_command_dir(args: argparse.Namespace) -> pathlib.Path:
    candidate = args.case_dir.resolve()
    if args.operation == "case-init":
        case_id = validate_case_id(args.case_id, HELPER_CASES)
    else:
        path = state_path(candidate)
        if not path.is_file() or path.is_symlink():
            raise WorkflowError("case helper is not initialized")
        value = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(value, dict):
            raise WorkflowError("invalid helper state")
        case_id = validate_case_id(str(value.get("case_id", "")), HELPER_CASES)
        if getattr(args, "case_id", None) is not None and args.case_id != case_id:
            raise WorkflowError("explicit case id does not match helper state")
    return validate_attempt_dir(candidate, case_id)


def sanitize_json_file(args: argparse.Namespace) -> Dict[str, Any]:
    source = args.input.resolve()
    target = args.output.resolve()
    if not source.is_file():
        raise WorkflowError("input JSON is missing")
    value = json.loads(source.read_text(encoding="utf-8"))
    # Do not silently drop ambiguous fields here: callers must author a bounded,
    # safe projection and use this command as the last validation/write gate.
    assert_safe_json(value, "input")
    if target.exists():
        raise WorkflowError("refusing to overwrite output evidence")
    write_json(target, safe_json_projection(value))
    return {"output": str(target), "bytes": target.stat().st_size, "sha256": sha256_file(target)}


def add_common(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    parser.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="operation", required=True)

    contract = subparsers.add_parser("case-contract", help="print one frozen WFL-003..009 case contract")
    contract.add_argument("--case-id", choices=HELPER_CASES, required=True)

    init = subparsers.add_parser("case-init", help="open one fixture-bound helper session for WFL-003..009")
    init.add_argument("--case-id", choices=HELPER_CASES, required=True)
    init.add_argument("--case-dir", type=pathlib.Path, required=True)
    init.add_argument("--fixture-manifest", type=pathlib.Path, required=True)
    init.add_argument("--private-values-file", type=pathlib.Path)
    init.add_argument("--scratch-dir", type=pathlib.Path)
    init.add_argument("--session")
    add_common(init)

    trace_start = subparsers.add_parser("trace-start", help="start a trace in an initialized helper session")
    trace_start.add_argument("--case-dir", type=pathlib.Path, required=True)
    trace_start.add_argument("--confirmed-dialog-closed", action="store_true")
    trace_start.add_argument("--sensitive-ui-cleared", action="store_true")
    trace_start.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)

    goto = subparsers.add_parser("goto", help="navigate the initialized helper session")
    goto.add_argument("--case-dir", type=pathlib.Path, required=True)
    goto.add_argument("--path", required=True)
    goto.add_argument("--wait-ms", type=int, default=1300)
    goto.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)

    snapshot = subparsers.add_parser("snapshot", help="capture and redact a CLI accessibility snapshot")
    snapshot.add_argument("--case-dir", type=pathlib.Path, required=True)
    snapshot.add_argument("--label", required=True)
    snapshot.add_argument("--evidence-name")
    snapshot.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)

    snapshot_index = subparsers.add_parser("snapshot-index", help="build a required JSON snapshot index")
    snapshot_index.add_argument("--case-dir", type=pathlib.Path, required=True)
    snapshot_index.add_argument("--evidence-name", required=True)
    snapshot_index.add_argument("--label", action="append")

    console = subparsers.add_parser("console", help="capture redacted browser console evidence")
    console.add_argument("--case-dir", type=pathlib.Path, required=True)
    console.add_argument("--minimum", choices=("error", "warning", "info", "debug"), default="error")
    console.add_argument("--evidence-name")
    console.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)

    trace_stop = subparsers.add_parser("trace-stop", help="stop and package trace.zip")
    trace_stop.add_argument("--case-dir", type=pathlib.Path, required=True)
    trace_stop.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)

    close = subparsers.add_parser("case-close", help="close only this helper's named browser session")
    close.add_argument("--case-dir", type=pathlib.Path, required=True)
    close.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)

    checkpoint = subparsers.add_parser("case-checkpoint", help="record the next durable case checkpoint")
    checkpoint.add_argument("--case-dir", type=pathlib.Path, required=True)
    checkpoint.add_argument(
        "--name",
        choices=tuple(sorted({name for names in CASE_CHECKPOINTS.values() for name in names})),
        required=True,
    )

    status = subparsers.add_parser("case-status", help="show a secret-free resumable case projection")
    status.add_argument("--case-dir", type=pathlib.Path, required=True)

    resume = subparsers.add_parser("case-resume", help="reconcile immutable artifacts and resume one case")
    resume.add_argument("--case-dir", type=pathlib.Path, required=True)
    resume.add_argument("--wrapper", type=pathlib.Path, default=DEFAULT_WRAPPER)

    seal = subparsers.add_parser(
        "case-seal-redaction",
        help="seal exact evidence before destroying the private redaction value file",
    )
    seal.add_argument("--case-dir", type=pathlib.Path, required=True)

    check = subparsers.add_parser("case-check", help="validate exact evidence inventory and redaction")
    check.add_argument("--case-dir", type=pathlib.Path, required=True)
    check.add_argument("--case-id", choices=HELPER_CASES)

    trace_sanitize = subparsers.add_parser(
        "sanitize-trace",
        help="sanitize one private Playwright trace zip without browser or service access",
    )
    trace_sanitize.add_argument("--input", type=pathlib.Path, required=True)
    trace_sanitize.add_argument("--output", type=pathlib.Path, required=True)
    trace_sanitize.add_argument("--case-id", choices=HELPER_CASES)
    trace_sanitize.add_argument("--private-values-file", type=pathlib.Path)

    sanitize = subparsers.add_parser("sanitize-json", help="validate and atomically write a safe JSON projection")
    sanitize.add_argument("--input", type=pathlib.Path, required=True)
    sanitize.add_argument("--output", type=pathlib.Path, required=True)
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        if args.operation == "case-contract":
            output = helper_contract(args)
        elif args.operation in {
            "case-init",
            "trace-start",
            "goto",
            "snapshot",
            "snapshot-index",
            "console",
            "trace-stop",
            "case-close",
            "case-checkpoint",
            "case-status",
            "case-resume",
            "case-seal-redaction",
            "case-check",
        }:
            handlers = {
                "case-init": helper_init,
                "trace-start": helper_trace_start,
                "goto": helper_goto,
                "snapshot": helper_snapshot,
                "snapshot-index": helper_snapshot_index,
                "console": helper_console,
                "trace-stop": helper_trace_stop,
                "case-close": helper_close,
                "case-checkpoint": helper_checkpoint,
                "case-status": helper_status,
                "case-resume": helper_resume,
                "case-seal-redaction": helper_seal_redaction,
                "case-check": helper_check,
            }
            args.case_dir = validate_case_command_dir(args)
            with case_lock(args.case_dir):
                output = handlers[args.operation](args)
        elif args.operation == "sanitize-trace":
            output = sanitize_trace_file(args)
        elif args.operation == "sanitize-json":
            output = sanitize_json_file(args)
        else:
            raise WorkflowError("unsupported operation")
        print(json.dumps(output, ensure_ascii=False, sort_keys=True, indent=2))
        if args.operation == "case-check" and not output["complete"]:
            return 1
        return 0
    except (WorkflowError, OSError, subprocess.TimeoutExpired, ValueError, zipfile.BadZipFile) as error:
        print("workflow-playwright: %s" % redact_text(str(error)), file=sys.stderr)
        return 2
    except Exception:  # noqa: BLE001 - unexpected harness failures are infrastructure failures
        print("workflow-playwright: internal harness failure", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
