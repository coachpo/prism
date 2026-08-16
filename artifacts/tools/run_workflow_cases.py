#!/usr/bin/env python3
"""Run one frozen WFL-001..WFL-010 case in an isolated local fixture.

This owner is deliberately separate from the matrix runner.  It accepts one
exact runner attempt, owns one run-prefixed PostgreSQL clone and three private
loopback listeners, and never advances ``runner-state.json``.  Browser work is
performed only through the bundled Playwright CLI wrapper used by
``workflow_playwright.py``.

The default ``self-test`` and ``contract`` commands are mutation free.
``prepare-case`` builds the pinned backend and clone before formal allocation;
``run-case`` adopts that exact prepared clone, starts its fixture, and executes
the scenario.  Neither path is invoked by self-test or module import.
"""

from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import fcntl
import hashlib
import json
import os
import pathlib
import re
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import urllib.parse
from dataclasses import dataclass
from typing import Any, Callable, Iterable, Mapping, Optional, Sequence

import local_matrix_support as support
import run_retention_case as retention
import workflow_playwright as workflow

RUN_ID = workflow.RUN_ID
ROOT = workflow.REPO_ROOT
RUN_ROOT = workflow.RUN_ROOT
PRIVATE_ROOT = workflow.RUN_PRIVATE / "workflow-cases"
BASE_CONFIG = workflow.RUN_PRIVATE / "config.json"
BACKEND_BINARY = workflow.RUN_PRIVATE / "prism-backend"
MOCK_PROGRAM = ROOT / "artifacts" / "tools" / "mock_provider.py"
FRONTEND_DIR = ROOT / "frontend"
BACKEND_DIR = ROOT / "backend"

SCHEMA_VERSION = 1
PREPARATION_SCHEMA_VERSION = 1
OWNER_VERSION = "1.0.0"
PREPARATION_FENCE_KEYS = frozenset(
    {
        "matrix",
        "base_config",
        "workflow_owner",
        "workflow_helper",
        "local_support",
        "retention_helper",
        "database_lane",
        "playwright_wrapper",
        "playwright_cli_entry",
        "playwright_cli_lock",
        "chromium_executable",
        "backend_runtime_tree",
        "frontend_runtime_tree",
        "frontend_modules_runtime_tree",
        "playwright_cli_runtime_tree",
        "chromium_bundle",
        "git_head",
    }
)
READONLY_CASES = workflow.READONLY_CASES
MUTATION_CASES = workflow.HELPER_CASES
ALLOWED_CASES = tuple("WFL-%03d" % number for number in range(1, 11))
AUTOMATED_CASES = ALLOWED_CASES
RESERVED_PORTS = {15174, 18080, 18081, 25432}
SAFE_STEP_RE = re.compile(r"^[a-z][a-z0-9_]{2,63}$")
SAFE_IDENTIFIER_RE = re.compile(r"^[a-z0-9][a-z0-9._:/-]{0,190}$")
BROWSER_INFRA_FAILURE_CODES = {
    "workflow_origin_mismatch",
    "workflow_result_invalid",
    "endpoint_private_value_missing",
    "payload_marker_missing",
    "operator_private_value_missing",
    "proxy_secret_not_held",
    "proxy_values_missing",
    "proxy_new_value_missing",
    "workflow_private_binding_missing",
}
CASE_TIMEOUT_SECONDS: Mapping[str, int] = {
    "WFL-001": 240,
    "WFL-002": 240,
    "WFL-003": 480,
    "WFL-004": 300,
    "WFL-005": 360,
    "WFL-006": 360,
    "WFL-007": 300,
    "WFL-008": 480,
    "WFL-009": 600,
    "WFL-010": 480,
}
CLEANUP_RESERVE_SECONDS = 90
PROCESS_LAUNCH_WINDOW_SECONDS = 15.0

READONLY_CHECKPOINTS: Mapping[str, tuple[str, ...]] = {
    "WFL-001": (
        "fixture_verified",
        "navigation_verified",
        "cleanup_verified",
    ),
    "WFL-002": (
        "fixture_verified",
        "model_inventory_verified",
        "view_data_verified",
        "coverage_verified",
        "cleanup_verified",
    ),
    "WFL-010": (
        "fixture_verified",
        "viewport_verified",
        "state_grid_verified",
        "accessibility_verified",
        "cleanup_verified",
    ),
}


def owner_checkpoints(case_id: str) -> tuple[str, ...]:
    if case_id in READONLY_CHECKPOINTS:
        return READONLY_CHECKPOINTS[case_id]
    try:
        return tuple(workflow.CASE_CHECKPOINTS[case_id])
    except KeyError as exc:
        raise CaseError("workflow_case_checkpoint_contract_missing") from exc


class CaseError(RuntimeError):
    """Code-only workflow owner failure safe for stdout and runner evidence."""

    def __init__(self, code: str, *, assertion_failure: bool = False) -> None:
        if not re.fullmatch(r"[a-z0-9][a-z0-9_.-]{2,95}", code):
            code = "unsafe_workflow_case_error"
        super().__init__(code)
        self.code = code
        self.assertion_failure = assertion_failure


class CaseDeadlineExpired(BaseException):
    """Signal control flow that cleanup fan-out must never swallow."""

    def __init__(self, phase: str) -> None:
        if phase not in {"work", "cleanup"}:
            raise ValueError("invalid workflow deadline phase")
        self.phase = phase
        self.code = (
            "workflow_case_cleanup_timeout"
            if phase == "cleanup"
            else "workflow_case_frozen_timeout"
        )
        super().__init__(self.code)


@dataclass(frozen=True)
class CaseSpec:
    case_id: str
    slug: str
    backend_port: int
    frontend_port: int
    mock_port: Optional[int]
    sensitive_value_labels: tuple[str, ...]
    scenario_steps: tuple[str, ...]

    @property
    def database(self) -> str:
        return support.case_database(self.slug)

    @property
    def backend_origin(self) -> str:
        return "http://127.0.0.1:%d" % self.backend_port

    @property
    def frontend_origin(self) -> str:
        return "http://127.0.0.1:%d" % self.frontend_port

    @property
    def mock_origin(self) -> Optional[str]:
        return None if self.mock_port is None else "http://127.0.0.1:%d" % self.mock_port


CASE_SPECS: Mapping[str, CaseSpec] = {
    "WFL-001": CaseSpec(
        "WFL-001", "wfl_001", 18201, 15201, 18301,
        (),
        READONLY_CHECKPOINTS["WFL-001"][1:],
    ),
    "WFL-002": CaseSpec(
        "WFL-002", "wfl_002", 18202, 15202, 18302,
        (),
        READONLY_CHECKPOINTS["WFL-002"][1:],
    ),
    "WFL-003": CaseSpec(
        "WFL-003", "wfl_003", 18203, 15203, 18303,
        ("endpoint_key",),
        ("endpoint_created", "pricing_created", "ban_policy_created", "model_created", "targets_saved", "refresh_and_detail_verified", "dependency_protection_verified", "reverse_delete_completed", "cleanup_verified"),
    ),
    "WFL-004": CaseSpec(
        "WFL-004", "wfl_004", 18204, 15204, 18304,
        ("endpoint_key", "payload_marker"),
        ("proxy_key_created", "sensitive_ui_cleared", "runtime_request_succeeded", "request_detail_verified", "audit_verified", "key_attribution_verified", "proxy_key_revoked", "cleanup_verified"),
    ),
    "WFL-005": CaseSpec(
        "WFL-005", "wfl_005", 18205, 15205, 18305,
        ("endpoint_key",),
        ("failure_injected", "failover_observed", "routing_health_verified", "event_detail_verified", "state_reset", "primary_recovered", "cleanup_verified"),
    ),
    "WFL-006": CaseSpec(
        "WFL-006", "wfl_006", 18206, 15206, 18306,
        ("endpoint_key", "payload_marker", "credential_marker"),
        ("disabled_mode_verified", "metadata_mode_verified", "body_mode_verified", "raw_download_verified", "secret_scan_passed", "settings_restored", "cleanup_verified"),
    ),
    "WFL-007": CaseSpec(
        "WFL-007", "wfl_007", 18207, 15207, 18307,
        ("endpoint_key",),
        ("pricing_verified", "usage_verified", "cost_recalculated", "endpoint_label_snapshot_verified", "currency_changed", "currency_refresh_verified", "currency_restored", "cleanup_verified"),
    ),
    "WFL-008": CaseSpec(
        "WFL-008", "wfl_008", 18208, 15208, 18308,
        ("endpoint_key", "operator_password"),
        tuple(workflow.CASE_CHECKPOINTS["WFL-008"])[1:],
    ),
    "WFL-009": CaseSpec(
        "WFL-009", "wfl_009", 18209, 15209, None,
        (),
        tuple(workflow.CASE_CHECKPOINTS["WFL-009"])[1:],
    ),
    "WFL-010": CaseSpec(
        "WFL-010", "wfl_010", 18210, 15210, 18310,
        (),
        READONLY_CHECKPOINTS["WFL-010"][1:],
    ),
}


def utc_now() -> str:
    return workflow.utc_now()


def canonical_bytes(value: Any) -> bytes:
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode("utf-8")


def safe_case(case_id: str) -> CaseSpec:
    normalized = case_id.strip().upper()
    if normalized not in CASE_SPECS:
        raise CaseError("workflow_case_not_allowed")
    return CASE_SPECS[normalized]


def validate_ports(spec: CaseSpec) -> None:
    values = [spec.backend_port, spec.frontend_port]
    if spec.mock_port is not None:
        values.append(spec.mock_port)
    if len(values) != len(set(values)):
        raise CaseError("workflow_case_ports_overlap")
    if any(not 1024 <= value <= 65535 or value in RESERVED_PORTS for value in values):
        raise CaseError("workflow_case_port_not_isolated")


def frozen_case_timeout(case_id: str) -> int:
    workflow.frozen_workflow_contract()
    try:
        matrix = json.loads(workflow.MATRIX_PATH.read_text(encoding="utf-8"))
        values = {
            str(item.get("id")): item.get("timeout")
            for item in matrix.get("cases", [])
            if isinstance(item, Mapping) and str(item.get("id")) in CASE_TIMEOUT_SECONDS
        }
    except (OSError, UnicodeError, json.JSONDecodeError, AttributeError) as exc:
        raise CaseError("workflow_case_timeout_contract_invalid") from exc
    if values != dict(CASE_TIMEOUT_SECONDS):
        raise CaseError("workflow_case_timeout_contract_changed")
    return CASE_TIMEOUT_SECONDS[case_id]


class CaseDeadline:
    """One absolute matrix deadline with a reserved, still-bounded cleanup phase."""

    def __init__(self, case_id: str) -> None:
        self.case_id = case_id
        self.timeout_seconds = frozen_case_timeout(case_id)
        self.cleanup_reserve_seconds = CLEANUP_RESERVE_SECONDS
        if not 0 < self.cleanup_reserve_seconds < self.timeout_seconds:
            raise CaseError("workflow_case_cleanup_reserve_invalid")
        self.started_at = 0.0
        self.final_deadline = 0.0
        self.phase = "work"
        self.previous_handler: Any = None

    def _expired(self, _signum: int, _frame: Any) -> None:
        expired_phase = self.phase
        if expired_phase == "work" and self.final_deadline > 0:
            # Rearm the cleanup reserve inside the signal handler, before any
            # nested finally block (trace packaging, browser close, process
            # stop) can run while unwinding the work-timeout control flow.
            self.phase = "cleanup"
            remaining = self.final_deadline - time.monotonic()
            if remaining <= 0:
                raise CaseDeadlineExpired("cleanup")
            signal.setitimer(signal.ITIMER_REAL, remaining)
        raise CaseDeadlineExpired(expired_phase)

    def _arm_until(self, deadline: float) -> None:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            self._expired(signal.SIGALRM, None)
        signal.setitimer(signal.ITIMER_REAL, remaining)

    def __enter__(self) -> "CaseDeadline":
        if not hasattr(signal, "setitimer") or not hasattr(signal, "ITIMER_REAL"):
            raise CaseError("workflow_case_timeout_runtime_unsupported")
        previous_timer = signal.getitimer(signal.ITIMER_REAL)
        if previous_timer != (0.0, 0.0):
            raise CaseError("workflow_case_timeout_owner_conflict")
        self.previous_handler = signal.getsignal(signal.SIGALRM)
        self.started_at = time.monotonic()
        self.final_deadline = self.started_at + self.timeout_seconds
        self.phase = "work"
        signal.signal(signal.SIGALRM, self._expired)
        self._arm_until(self.final_deadline - self.cleanup_reserve_seconds)
        return self

    def enter_cleanup(self) -> None:
        if self.phase == "cleanup":
            # A SIGALRM raised at the final deadline is caught by the owner's
            # common failure path.  Re-check the same absolute deadline here
            # so that path cannot accidentally continue with an expired,
            # one-shot timer and turn cleanup into an unbounded retry.
            self._arm_until(self.final_deadline)
            return
        if self.phase != "work" or self.final_deadline <= 0:
            raise CaseError("workflow_case_deadline_state_invalid")
        # Replace the earlier work timer while the handler still classifies an
        # in-flight old alarm as a work expiry.  Flipping the phase first would
        # let that alarm masquerade as the absolute cleanup deadline and skip
        # the owner's bounded cleanup path.
        self._arm_until(self.final_deadline)
        self.phase = "cleanup"

    def receipt(self) -> dict[str, Any]:
        return {
            "timeout_seconds": self.timeout_seconds,
            "cleanup_reserve_seconds": self.cleanup_reserve_seconds,
            "phase": self.phase,
            "remaining_seconds": max(0, int(self.final_deadline - time.monotonic())),
        }

    def __exit__(self, _type: Any, _value: Any, _traceback: Any) -> None:
        signal.setitimer(signal.ITIMER_REAL, 0.0)
        signal.signal(signal.SIGALRM, self.previous_handler)
        self.phase = "closed"


def enforce_case_timeout(case_id: str) -> CaseDeadline:
    """Compatibility name for the whole-case deadline context."""
    return CaseDeadline(case_id)


def lane_dir(spec: CaseSpec) -> pathlib.Path:
    return PRIVATE_ROOT / spec.slug


def lane_state_path(spec: CaseSpec) -> pathlib.Path:
    return lane_dir(spec) / "state.json"


def lane_lock_path(spec: CaseSpec) -> pathlib.Path:
    return lane_dir(spec) / "owner.lock"


def preparation_receipt_path(spec: CaseSpec) -> pathlib.Path:
    return lane_dir(spec) / "preparation.json"


@contextlib.contextmanager
def lane_lock(spec: CaseSpec):
    directory = lane_dir(spec)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(directory, 0o700)
    path = lane_lock_path(spec)
    descriptor = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise CaseError("workflow_case_is_already_running") from exc
        yield
    finally:
        fcntl.flock(descriptor, fcntl.LOCK_UN)
        os.close(descriptor)


def read_json_0600(path: pathlib.Path) -> dict[str, Any]:
    try:
        metadata = path.lstat()
    except OSError as exc:
        raise CaseError("workflow_private_state_missing") from exc
    if path.is_symlink() or not stat.S_ISREG(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) & 0o077:
        raise CaseError("workflow_private_state_permissions_invalid")
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise CaseError("workflow_private_state_invalid") from exc
    if not isinstance(value, dict):
        raise CaseError("workflow_private_state_invalid")
    return value


def write_private_json(path: pathlib.Path, value: Any) -> None:
    support.atomic_write_json(path, value)
    os.chmod(path, 0o600)


def file_sha(path: pathlib.Path) -> str:
    try:
        return support.sha256_regular_file(path)
    except support.HarnessError as exc:
        raise CaseError("workflow_pinned_file_invalid") from exc


def deterministic_tree_sha(root: pathlib.Path, paths: Iterable[pathlib.Path]) -> str:
    root = root.resolve()
    digest = hashlib.sha256()
    seen: set[str] = set()
    for path in sorted(paths, key=lambda value: value.as_posix()):
        if path.is_symlink() or not path.is_file():
            raise CaseError("workflow_pinned_tree_entry_invalid")
        resolved = path.resolve()
        try:
            relative = resolved.relative_to(root).as_posix()
        except ValueError as exc:
            raise CaseError("workflow_pinned_tree_entry_escaped") from exc
        if relative in seen:
            continue
        seen.add(relative)
        digest.update(relative.encode("utf-8") + b"\0")
        digest.update(bytes.fromhex(file_sha(resolved)))
    if not seen:
        raise CaseError("workflow_pinned_tree_empty")
    return digest.hexdigest()


def frontend_runtime_tree_sha() -> str:
    paths: list[pathlib.Path] = []
    for directory_name in ("src", "public", "scripts"):
        directory = FRONTEND_DIR / directory_name
        if directory.is_symlink() or not directory.is_dir():
            raise CaseError("workflow_frontend_runtime_tree_invalid")
        paths.extend(path for path in directory.rglob("*") if path.is_file() or path.is_symlink())
    for pattern in (
        ".env*",
        "*.config.ts",
        "*.config.js",
        "tsconfig*.json",
        "*.html",
        "*.json",
        "*.yaml",
        "*.yml",
        "VERSION",
    ):
        paths.extend(path for path in FRONTEND_DIR.glob(pattern) if path.is_file() or path.is_symlink())
    return deterministic_tree_sha(FRONTEND_DIR, paths)


def playwright_cli_runtime_tree_sha() -> str:
    runtime_root = workflow.PLAYWRIGHT_CLI_ROOT
    modules = runtime_root / "node_modules"
    if runtime_root.is_symlink() or modules.is_symlink() or not modules.is_dir():
        raise CaseError("workflow_playwright_runtime_tree_invalid")
    paths = [
        path
        for path in modules.rglob("*")
        if path.is_file() and not path.is_symlink()
    ]
    paths.append(workflow.PLAYWRIGHT_CLI_LOCK)
    return deterministic_tree_sha(runtime_root, paths)


def frontend_modules_runtime_tree_sha() -> str:
    modules = (FRONTEND_DIR / "node_modules").resolve()
    store = modules / ".pnpm"
    if modules.is_symlink() or store.is_symlink() or not store.is_dir():
        raise CaseError("workflow_frontend_modules_tree_invalid")
    paths = [
        path
        for path in store.rglob("*")
        if path.is_file() and not path.is_symlink()
    ]
    return deterministic_tree_sha(modules, paths)


def backend_runtime_tree_sha() -> str:
    paths: list[pathlib.Path] = []
    for path in BACKEND_DIR.rglob("*"):
        relative = path.relative_to(BACKEND_DIR)
        if (
            relative.parts[:1] == ("artifacts",)
            or "__pycache__" in relative.parts
            or path.name == "prism-backend"
            or path.suffix in {".pyc", ".tmp"}
        ):
            continue
        if path.is_file() or path.is_symlink():
            paths.append(path)
    return deterministic_tree_sha(BACKEND_DIR, paths)


def git_head_value() -> str:
    marker = ROOT / ".git"
    try:
        if marker.is_file() and not marker.is_symlink():
            declaration = marker.read_text(encoding="utf-8").strip()
            if not declaration.startswith("gitdir: "):
                raise CaseError("workflow_git_dir_invalid")
            git_dir = pathlib.Path(declaration.removeprefix("gitdir: "))
            if not git_dir.is_absolute():
                git_dir = (ROOT / git_dir).resolve()
        elif marker.is_dir() and not marker.is_symlink():
            git_dir = marker.resolve()
        else:
            raise CaseError("workflow_git_dir_invalid")
        head_text = (git_dir / "HEAD").read_text(encoding="utf-8").strip()
        if head_text.startswith("ref: "):
            ref = head_text.removeprefix("ref: ")
            if not re.fullmatch(r"refs/[A-Za-z0-9._/-]+", ref) or ".." in pathlib.PurePosixPath(ref).parts:
                raise CaseError("workflow_git_head_ref_invalid")
            candidates = [git_dir / ref]
            common_marker = git_dir / "commondir"
            common_dir = git_dir
            if common_marker.is_file() and not common_marker.is_symlink():
                common_value = pathlib.Path(common_marker.read_text(encoding="utf-8").strip())
                common_dir = (git_dir / common_value).resolve() if not common_value.is_absolute() else common_value.resolve()
                candidates.append(common_dir / ref)
            head = ""
            for candidate in candidates:
                if candidate.is_file() and not candidate.is_symlink():
                    head = candidate.read_text(encoding="ascii").strip()
                    break
            if not head:
                packed = common_dir / "packed-refs"
                if packed.is_file() and not packed.is_symlink():
                    for line in packed.read_text(encoding="ascii").splitlines():
                        if line.startswith("#") or line.startswith("^"):
                            continue
                        parts = line.split(" ", 1)
                        if len(parts) == 2 and parts[1] == ref:
                            head = parts[0]
                            break
        else:
            head = head_text
    except (OSError, UnicodeError) as exc:
        raise CaseError("workflow_git_head_unavailable") from exc
    if not re.fullmatch(r"[0-9a-f]{40,64}", head):
        raise CaseError("workflow_git_head_invalid")
    return head


def git_head_sha() -> str:
    return hashlib.sha256(git_head_value().encode("ascii")).hexdigest()


def preparation_fence(chromium_executable: pathlib.Path) -> dict[str, str]:
    chromium = workflow.validate_chromium_executable(chromium_executable)
    paths = {
        "matrix": workflow.MATRIX_PATH,
        "base_config": BASE_CONFIG,
        "workflow_owner": pathlib.Path(__file__).resolve(),
        "workflow_helper": pathlib.Path(workflow.__file__).resolve(),
        "local_support": pathlib.Path(support.__file__).resolve(),
        "retention_helper": pathlib.Path(retention.__file__).resolve(),
        "database_lane": ROOT / "artifacts" / "tools" / "db" / "db_lane.py",
        "playwright_wrapper": workflow.DEFAULT_WRAPPER,
        "playwright_cli_entry": workflow.PLAYWRIGHT_CLI_ENTRY,
        "playwright_cli_lock": workflow.PLAYWRIGHT_CLI_LOCK,
        "chromium_executable": chromium,
    }
    if any(path.is_symlink() or not path.is_file() for path in paths.values()):
        raise CaseError("workflow_preparation_input_missing")
    values = {name: file_sha(path) for name, path in paths.items()}
    values.update(
        {
            "backend_runtime_tree": backend_runtime_tree_sha(),
            "frontend_runtime_tree": frontend_runtime_tree_sha(),
            "frontend_modules_runtime_tree": frontend_modules_runtime_tree_sha(),
            "playwright_cli_runtime_tree": playwright_cli_runtime_tree_sha(),
            "chromium_bundle": workflow.chromium_bundle_sha256(chromium),
            "git_head": git_head_sha(),
        }
    )
    if values["matrix"] != workflow.MATRIX_SHA256:
        raise CaseError("workflow_case_matrix_changed")
    if set(values) != PREPARATION_FENCE_KEYS:
        raise CaseError("workflow_preparation_fence_schema_invalid")
    return values


def preparation_common(
    spec: CaseSpec,
    chromium_executable: pathlib.Path,
) -> dict[str, Any]:
    head = git_head_value()
    return {
        "schema_version": PREPARATION_SCHEMA_VERSION,
        "owner_version": OWNER_VERSION,
        "run_id": RUN_ID,
        "matrix_sha256": workflow.MATRIX_SHA256,
        "case_id": spec.case_id,
        "database": spec.database,
        "ports": {
            "backend": spec.backend_port,
            "frontend": spec.frontend_port,
            "mock": spec.mock_port,
        },
        "branch": workflow.EXPECTED_BRANCH,
        "branch_head": head,
        "chromium_executable": str(chromium_executable),
        "preparation_fence": preparation_fence(chromium_executable),
    }


def load_preparation_receipt(
    spec: CaseSpec,
    *,
    required: bool = True,
) -> Optional[dict[str, Any]]:
    path = preparation_receipt_path(spec)
    if not path.exists():
        if path.is_symlink():
            raise CaseError("workflow_preparation_receipt_invalid")
        if required:
            raise CaseError("workflow_case_not_prepared")
        return None
    receipt = read_json_0600(path)
    chromium_text = str(receipt.get("chromium_executable", ""))
    chromium = pathlib.Path(chromium_text)
    if not chromium.is_absolute():
        raise CaseError("workflow_preparation_receipt_invalid")
    try:
        expected = preparation_common(spec, chromium)
    except workflow.WorkflowError as exc:
        raise CaseError("workflow_preparation_receipt_invalid") from exc
    if any(receipt.get(name) != value for name, value in expected.items()):
        raise CaseError("workflow_preparation_receipt_fence_mismatch")
    state = receipt.get("state")
    if state not in {"creating", "prepared", "adopted", "complete", "product_failed"}:
        raise CaseError("workflow_preparation_receipt_invalid")
    common_keys = set(expected)
    creating_keys = common_keys | {"state", "created_at"}
    prepared_keys = creating_keys | {
        "prepared_at",
        "database_clone_identity",
        "database_clone_fingerprint",
        "database_content_identity",
        "backend_binary",
        "backend_build",
        "chromium_bundle_sha256",
        "paths",
        "generated_artifacts",
        "private_value_indexes",
        "input_fingerprints",
        "allocation",
    }
    adopted_keys = prepared_keys | {"adopted_at"}
    completed_keys = adopted_keys | {"clone_dropped", "finished_at"}
    expected_keys = {
        "creating": creating_keys,
        "prepared": prepared_keys,
        "adopted": adopted_keys,
        "complete": completed_keys,
        "product_failed": completed_keys,
    }[str(state)]
    if set(receipt) != expected_keys:
        raise CaseError("workflow_preparation_receipt_schema_invalid")
    if state != "creating":
        clone_identity = receipt.get("database_clone_identity")
        clone_fingerprint = receipt.get("database_clone_fingerprint")
        if (
            not isinstance(clone_identity, dict)
            or not isinstance(clone_fingerprint, str)
            or not workflow.SHA256_RE.fullmatch(clone_fingerprint)
            or retention.physical_clone_fingerprint(clone_identity) != clone_fingerprint
        ):
            raise CaseError("workflow_preparation_clone_identity_invalid")
        allocation = receipt.get("allocation")
        if state == "prepared" and allocation is not None:
            raise CaseError("workflow_preparation_receipt_schema_invalid")
        if state != "prepared" and (
            not isinstance(allocation, dict)
            or set(allocation) != {"result_id", "cycle", "attempt", "result_sha256"}
            or not isinstance(allocation.get("result_id"), str)
            or not isinstance(allocation.get("cycle"), str)
            or type(allocation.get("attempt")) is not int
            or int(allocation.get("attempt", 0)) < 1
            or not workflow.SHA256_RE.fullmatch(str(allocation.get("result_sha256", "")))
        ):
            raise CaseError("workflow_preparation_receipt_schema_invalid")
        if state in {"complete", "product_failed"} and receipt.get("clone_dropped") is not True:
            raise CaseError("workflow_preparation_receipt_schema_invalid")
    return receipt


def load_reconcilable_creating_receipt(spec: CaseSpec) -> Optional[dict[str, Any]]:
    """Read a creating receipt without requiring its old content hashes to be current.

    A killed preparation can leave an exact case clone or partial backend from
    the prior harness revision. Cleanup is still safe because every target is
    derived from the frozen case spec; requiring current hashes first would
    strand the very receipt intended to authorize that reconciliation.
    """
    path = preparation_receipt_path(spec)
    if not path.exists():
        if path.is_symlink():
            raise CaseError("workflow_preparation_receipt_invalid")
        return None
    receipt = read_json_0600(path)
    if receipt.get("state") != "creating":
        return None
    expected_keys = {
        "schema_version",
        "owner_version",
        "run_id",
        "matrix_sha256",
        "case_id",
        "database",
        "ports",
        "branch",
        "branch_head",
        "chromium_executable",
        "preparation_fence",
        "state",
        "created_at",
    }
    fence = receipt.get("preparation_fence")
    chromium = pathlib.Path(str(receipt.get("chromium_executable", "")))
    if (
        set(receipt) != expected_keys
        or type(receipt.get("schema_version")) is not int
        or receipt.get("schema_version") != PREPARATION_SCHEMA_VERSION
        or receipt.get("owner_version") != OWNER_VERSION
        or receipt.get("run_id") != RUN_ID
        or receipt.get("matrix_sha256") != workflow.MATRIX_SHA256
        or receipt.get("case_id") != spec.case_id
        or receipt.get("database") != spec.database
        or not workflow.type_strict_equal(
            receipt.get("ports"),
            {
                "backend": spec.backend_port,
                "frontend": spec.frontend_port,
                "mock": spec.mock_port,
            },
        )
        or receipt.get("branch") != workflow.EXPECTED_BRANCH
        or not isinstance(receipt.get("branch_head"), str)
        or re.fullmatch(r"[0-9a-f]{40,64}", str(receipt.get("branch_head"))) is None
        or not chromium.is_absolute()
        or not isinstance(fence, dict)
        or set(fence) != PREPARATION_FENCE_KEYS
        or any(
            not isinstance(value, str) or workflow.SHA256_RE.fullmatch(value) is None
            for value in fence.values()
        )
        or not workflow.canonical_runner_timestamp(receipt.get("created_at"))
    ):
        raise CaseError("workflow_preparation_receipt_schema_invalid")
    return receipt


def remove_preparation_artifacts(spec: CaseSpec) -> None:
    directory = lane_dir(spec)
    allowed_files = {
        directory / "bin" / "prism-backend",
        directory / "bin" / ".prism-backend.building",
        directory / "config.json",
        directory / "private-values.json",
        directory / "fixture-manifest.json",
    }
    for target_name in (
        "preparation.json",
        "config.json",
        "private-values.json",
        "fixture-manifest.json",
    ):
        for path in directory.glob(".%s.*" % target_name):
            if path.is_symlink() or not path.is_file():
                raise CaseError("workflow_preparation_artifact_unsafe")
            path.unlink()
    for path in allowed_files:
        if path.is_symlink():
            raise CaseError("workflow_preparation_artifact_unsafe")
        if path.exists():
            if not path.is_file():
                raise CaseError("workflow_preparation_artifact_unsafe")
            path.unlink()
    bin_dir = directory / "bin"
    if bin_dir.exists():
        if bin_dir.is_symlink() or not bin_dir.is_dir() or any(bin_dir.iterdir()):
            raise CaseError("workflow_preparation_artifact_unsafe")
        bin_dir.rmdir()


def reconcile_creating_preparation(spec: CaseSpec, receipt: Mapping[str, Any]) -> None:
    if receipt.get("state") != "creating":
        raise CaseError("workflow_preparation_reconcile_state_invalid")
    process_dir = lane_dir(spec) / "processes"
    if process_dir.exists() or process_dir.is_symlink():
        raise CaseError("workflow_preparation_unexpected_process_state")
    if case_database_exists(spec):
        support.run_lane(["drop-case", spec.database], ROOT)
    assert_case_database_absent(spec)
    remove_preparation_artifacts(spec)
    path = preparation_receipt_path(spec)
    path.unlink()
    if path.exists() or path.is_symlink():
        raise CaseError("workflow_preparation_reconcile_failed")


def require_clean_worktree() -> None:
    try:
        completed = subprocess.run(
            ["git", "status", "--porcelain=v1", "--untracked-files=all"],
            cwd=str(ROOT),
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise CaseError("workflow_git_status_unavailable") from exc
    if completed.returncode != 0:
        raise CaseError("workflow_git_status_unavailable")
    if completed.stdout:
        raise CaseError("workflow_tracked_worktree_not_clean")


def build_case_backend(target: pathlib.Path) -> dict[str, str]:
    require_clean_worktree()
    source_sha = backend_runtime_tree_sha()
    head = git_head_value()
    go_path = shutil.which("go")
    if go_path is None:
        raise CaseError("workflow_go_runtime_missing")
    go_runtime = pathlib.Path(go_path).resolve()
    if not go_runtime.is_file() or go_runtime.is_symlink():
        raise CaseError("workflow_go_runtime_invalid")
    target = target.absolute()
    workflow.validate_private_path(target.parent, require_file=False)
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(target.parent, 0o700)
    temporary = target.with_name("." + target.name + ".building")
    if (
        target.is_symlink()
        or (target.exists() and not target.is_file())
        or temporary.exists()
        or temporary.is_symlink()
    ):
        raise CaseError("workflow_backend_build_target_not_fresh")
    environment = sanitized_service_environment()
    environment.update({"GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local"})
    try:
        completed = subprocess.run(
            [str(go_runtime), "build", "-trimpath", "-o", str(temporary), "./cmd/prism-backend"],
            cwd=str(BACKEND_DIR),
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=180,
            check=False,
        )
        output = workflow.redact_text(completed.stdout.decode("utf-8", errors="replace"))
        workflow.assert_no_remaining_secret(output, "workflow backend build")
        if completed.returncode != 0 or not temporary.is_file() or temporary.is_symlink():
            raise CaseError("workflow_backend_build_failed")
        os.chmod(temporary, 0o700)
        require_clean_worktree()
        if backend_runtime_tree_sha() != source_sha or git_head_value() != head:
            raise CaseError("workflow_backend_source_changed_during_build")
        os.replace(temporary, target)
    except (OSError, subprocess.SubprocessError, workflow.WorkflowError) as exc:
        raise CaseError("workflow_backend_build_failed") from exc
    finally:
        with contextlib.suppress(FileNotFoundError):
            temporary.unlink()
    return {
        "git_head": head,
        "source_tree_sha256": source_sha,
        "binary_sha256": file_sha(target),
        "go_runtime_sha256": file_sha(go_runtime),
    }


def load_state(
    spec: CaseSpec,
    *,
    required: bool = True,
    verify_inputs: bool = True,
) -> Optional[dict[str, Any]]:
    path = lane_state_path(spec)
    if not path.exists():
        if required:
            raise CaseError("workflow_case_not_prepared")
        return None
    state = read_json_0600(path)
    if (
        state.get("schema_version") != SCHEMA_VERSION
        or state.get("owner_version") != OWNER_VERSION
        or state.get("run_id") != RUN_ID
        or state.get("matrix_sha256") != workflow.MATRIX_SHA256
        or state.get("case_id") != spec.case_id
        or state.get("database") != spec.database
        or state.get("ports") != {
            "backend": spec.backend_port,
            "frontend": spec.frontend_port,
            "mock": spec.mock_port,
        }
    ):
        raise CaseError("workflow_case_state_identity_mismatch")
    attempt = workflow.validate_attempt_dir(pathlib.Path(str(state.get("attempt_dir", ""))), spec.case_id)
    if str(attempt) != state.get("attempt_dir"):
        raise CaseError("workflow_case_attempt_identity_mismatch")
    expected_inputs = state.get("input_fingerprints")
    if not isinstance(expected_inputs, dict):
        raise CaseError("workflow_case_input_fingerprints_missing")
    chromium_text = str(state.get("chromium_executable", ""))
    chromium_path = pathlib.Path(chromium_text)
    if (
        not chromium_path.is_absolute()
        or not re.fullmatch(r"[0-9a-f]{64}", str(state.get("chromium_bundle_sha256", "")))
        or not re.fullmatch(r"[0-9a-f]{64}", str(expected_inputs.get("chromium_executable", "")))
        or state.get("chromium_bundle_sha256") != expected_inputs.get("chromium_bundle")
    ):
        raise CaseError("workflow_case_chromium_identity_invalid")
    backend_text = str(state.get("backend_binary", ""))
    backend_path = pathlib.Path(backend_text)
    if not backend_path.is_absolute() or not re.fullmatch(
        r"[0-9a-f]{64}", str(expected_inputs.get("backend_binary", ""))
    ):
        raise CaseError("workflow_case_backend_identity_invalid")
    if verify_inputs:
        formal_allocation = state.get("formal_allocation")
        if (
            not isinstance(formal_allocation, dict)
            or set(formal_allocation) != {"result_id", "cycle", "attempt", "result_sha256"}
            or not isinstance(formal_allocation.get("result_id"), str)
            or not isinstance(formal_allocation.get("cycle"), str)
            or type(formal_allocation.get("attempt")) is not int
            or int(formal_allocation.get("attempt", 0)) < 1
            or not workflow.SHA256_RE.fullmatch(str(formal_allocation.get("result_sha256", "")))
            or not workflow.SHA256_RE.fullmatch(str(state.get("database_clone_fingerprint", "")))
        ):
            raise CaseError("workflow_case_formal_allocation_invalid")
        expected_backend_path = (lane_dir(spec) / "bin" / "prism-backend").absolute()
        if backend_path.absolute() != expected_backend_path:
            raise CaseError("workflow_case_backend_identity_invalid")
        try:
            chromium_executable = workflow.validate_chromium_executable(chromium_path)
        except workflow.WorkflowError as exc:
            raise CaseError("workflow_case_chromium_identity_invalid") from exc
        if str(chromium_executable) != chromium_text or file_sha(chromium_executable) != expected_inputs.get("chromium_executable"):
            raise CaseError("workflow_case_chromium_identity_invalid")
        if not backend_path.is_file() or backend_path.is_symlink() or file_sha(backend_path) != expected_inputs.get("backend_binary"):
            raise CaseError("workflow_case_backend_identity_invalid")
        actual_inputs = pinned_input_fingerprints(chromium_executable, backend_path)
        if expected_inputs != actual_inputs:
            raise CaseError("workflow_case_inputs_changed")
    else:
        cleanup_inputs = cleanup_input_fingerprints()
        if any(expected_inputs.get(name) != digest for name, digest in cleanup_inputs.items()):
            raise CaseError("workflow_cleanup_inputs_changed")
    return state


def save_state(spec: CaseSpec, state: Mapping[str, Any]) -> None:
    write_private_json(lane_state_path(spec), dict(state))


def pinned_input_fingerprints(
    chromium_executable: Optional[pathlib.Path] = None,
    backend_binary: Optional[pathlib.Path] = None,
) -> dict[str, str]:
    node_path = shutil.which("node")
    pnpm_path = shutil.which("pnpm")
    python_path = shutil.which("python3")
    go_path = shutil.which("go")
    if node_path is None or pnpm_path is None or python_path is None or go_path is None:
        raise CaseError("workflow_command_runtime_missing")
    chromium_executable = chromium_executable or workflow.discover_local_chromium()
    if chromium_executable is None:
        raise CaseError("workflow_chromium_runtime_missing")
    chromium_executable = workflow.validate_chromium_executable(chromium_executable)
    backend_binary = (backend_binary or BACKEND_BINARY).resolve()
    required = {
        "matrix": workflow.MATRIX_PATH,
        "base_config": BASE_CONFIG,
        "backend_binary": backend_binary,
        "mock_program": MOCK_PROGRAM,
        "frontend_package": FRONTEND_DIR / "package.json",
        "frontend_lock": FRONTEND_DIR / "pnpm-lock.yaml",
        "workflow_owner": pathlib.Path(__file__).resolve(),
        "workflow_helper": pathlib.Path(workflow.__file__).resolve(),
        "local_support": pathlib.Path(support.__file__).resolve(),
        "retention_helper": pathlib.Path(retention.__file__).resolve(),
        "database_lane": ROOT / "artifacts" / "tools" / "db" / "db_lane.py",
        "playwright_wrapper": workflow.DEFAULT_WRAPPER,
        "playwright_cli_entry": workflow.PLAYWRIGHT_CLI_ENTRY,
        "playwright_cli_lock": workflow.PLAYWRIGHT_CLI_LOCK,
        "node_runtime": pathlib.Path(node_path).resolve(),
        "pnpm_runtime": pathlib.Path(pnpm_path).resolve(),
        "python_runtime": pathlib.Path(python_path).resolve(),
        "python_executable": pathlib.Path(sys.executable).resolve(),
        "go_runtime": pathlib.Path(go_path).resolve(),
        "frontend_modules_manifest": (FRONTEND_DIR / "node_modules" / ".modules.yaml").resolve(),
        "frontend_modules_lock": (FRONTEND_DIR / "node_modules" / ".pnpm" / "lock.yaml").resolve(),
        "vite_executable": (FRONTEND_DIR / "node_modules" / ".bin" / "vite").resolve(),
        "chromium_executable": chromium_executable,
    }
    missing = [name for name, path in required.items() if not path.is_file() or path.is_symlink()]
    if missing:
        raise CaseError("workflow_case_pinned_input_missing")
    values = {name: file_sha(path) for name, path in required.items()}
    values["frontend_runtime_tree"] = frontend_runtime_tree_sha()
    values["frontend_modules_runtime_tree"] = frontend_modules_runtime_tree_sha()
    values["backend_runtime_tree"] = backend_runtime_tree_sha()
    values["playwright_cli_runtime_tree"] = playwright_cli_runtime_tree_sha()
    values["chromium_bundle"] = workflow.chromium_bundle_sha256(chromium_executable)
    values["git_head"] = git_head_sha()
    if values["matrix"] != workflow.MATRIX_SHA256:
        raise CaseError("workflow_case_matrix_changed")
    return values


def verify_execution_inputs(state: Mapping[str, Any]) -> None:
    expected = state.get("input_fingerprints")
    if not isinstance(expected, Mapping):
        raise CaseError("workflow_case_input_fingerprints_missing")
    try:
        chromium = workflow.validate_chromium_executable(
            pathlib.Path(str(state.get("chromium_executable", "")))
        )
    except workflow.WorkflowError as exc:
        raise CaseError("workflow_case_chromium_identity_invalid") from exc
    backend_binary = pathlib.Path(str(state.get("backend_binary", "")))
    spec = safe_case(str(state.get("case_id", "")))
    if (
        not backend_binary.is_absolute()
        or backend_binary.absolute() != (lane_dir(spec) / "bin" / "prism-backend").absolute()
    ):
        raise CaseError("workflow_case_backend_identity_invalid")
    if dict(expected) != pinned_input_fingerprints(chromium, backend_binary):
        raise CaseError("workflow_case_inputs_changed")


def cleanup_input_fingerprints() -> dict[str, str]:
    runtime_paths = {
        "local_support": pathlib.Path(support.__file__).resolve(),
        "database_lane": ROOT / "artifacts" / "tools" / "db" / "db_lane.py",
        "playwright_wrapper": workflow.DEFAULT_WRAPPER,
        "playwright_cli_entry": workflow.PLAYWRIGHT_CLI_ENTRY,
        "playwright_cli_lock": workflow.PLAYWRIGHT_CLI_LOCK,
    }
    for name, command in (
        ("node_runtime", "node"),
        ("python_runtime", "python3"),
    ):
        path = shutil.which(command)
        if path is None:
            raise CaseError("workflow_cleanup_runtime_missing")
        runtime_paths[name] = pathlib.Path(path).resolve()
    if any(not path.is_file() or path.is_symlink() for path in runtime_paths.values()):
        raise CaseError("workflow_cleanup_pinned_input_missing")
    values = {name: file_sha(path) for name, path in runtime_paths.items()}
    values["playwright_cli_runtime_tree"] = playwright_cli_runtime_tree_sha()
    return values


def port_available(port: int) -> bool:
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(("127.0.0.1", port))
        return True
    except OSError:
        return False
    finally:
        sock.close()


def require_ports_available(spec: CaseSpec) -> None:
    validate_ports(spec)
    values = [spec.backend_port, spec.frontend_port]
    if spec.mock_port is not None:
        values.append(spec.mock_port)
    if any(not port_available(port) for port in values):
        raise CaseError("workflow_case_listener_port_unavailable")


def safe_process_identity(pid: int) -> Optional[dict[str, Any]]:
    if pid <= 1:
        return None
    try:
        pgid = os.getpgid(pid)
        command = subprocess.run(
            ["ps", "-p", str(pid), "-o", "command="],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=3,
            check=False,
        )
        started = subprocess.run(
            ["ps", "-p", str(pid), "-o", "lstart="],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=3,
            check=False,
        )
        status = subprocess.run(
            ["ps", "-p", str(pid), "-o", "stat="],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=3,
            check=False,
        )
    except (OSError, ProcessLookupError, subprocess.SubprocessError):
        return None
    command_text = command.stdout.strip()
    if (
        command.returncode != 0
        or started.returncode != 0
        or status.returncode != 0
        or not command_text
        or not started.stdout.strip()
        or status.stdout.strip().upper().startswith("Z")
    ):
        return None
    return {
        "pid": pid,
        "pgid": pgid,
        "command_sha256": hashlib.sha256(command_text.encode("utf-8")).hexdigest(),
        "command_text": command_text,
        "started": started.stdout.strip(),
    }


def process_command_markers(name: str, arguments: Sequence[str], owned_port: int) -> list[str]:
    if name == "mock" and len(arguments) >= 2:
        markers = [str(pathlib.Path(arguments[1]).absolute()), "--port", str(owned_port)]
    elif name == "backend" and arguments:
        markers = [str(pathlib.Path(arguments[0]).absolute())]
    elif name == "frontend":
        markers = ["vite", "--port", str(owned_port), "--strictPort"]
    else:
        raise CaseError("workflow_process_command_invalid")
    if any(not marker or "\x00" in marker or "\n" in marker for marker in markers):
        raise CaseError("workflow_process_command_invalid")
    return markers


def spawned_receipt_matches(record: Mapping[str, Any], current: Mapping[str, Any]) -> bool:
    markers = record.get("command_markers")
    command_text = current.get("command_text")
    before = record.get("launch_epoch_before")
    after = record.get("launch_epoch_after")
    try:
        started_epoch = time.mktime(time.strptime(str(current.get("started")), "%a %b %d %H:%M:%S %Y"))
    except (TypeError, ValueError, OverflowError):
        return False
    return bool(
        isinstance(markers, list)
        and markers
        and all(isinstance(marker, str) and marker in str(command_text) for marker in markers)
        and not isinstance(before, bool)
        and isinstance(before, (int, float))
        and not isinstance(after, bool)
        and isinstance(after, (int, float))
        and float(before) <= float(after)
        and float(before) - 2.0 <= started_epoch <= float(after) + 2.0
    )


def launching_process_candidates(record: Mapping[str, Any]) -> list[dict[str, Any]]:
    """Find only self-led process groups born in this receipt's launch window."""
    markers = record.get("command_markers")
    before = record.get("launch_epoch_before")
    if (
        not isinstance(markers, list)
        or not markers
        or any(not isinstance(marker, str) or not marker for marker in markers)
        or isinstance(before, bool)
        or not isinstance(before, (int, float))
    ):
        raise CaseError("workflow_process_identity_mismatch")
    try:
        listing = subprocess.run(
            ["ps", "-axo", "pid=,pgid=,stat=,lstart=,command="],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=5,
            check=False,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise CaseError("workflow_stale_process_identity_unverifiable") from exc
    if listing.returncode != 0:
        raise CaseError("workflow_stale_process_identity_unverifiable")
    candidates: list[dict[str, Any]] = []
    for line in listing.stdout.splitlines():
        fields = line.strip().split(None, 8)
        if len(fields) != 9:
            continue
        try:
            pid = int(fields[0])
            pgid = int(fields[1])
            started_text = " ".join(fields[3:8])
            started_epoch = time.mktime(time.strptime(started_text, "%a %b %d %H:%M:%S %Y"))
        except (TypeError, ValueError, OverflowError):
            continue
        status = fields[2].upper()
        command_text = fields[8]
        if (
            pid <= 1
            or pgid != pid
            or status.startswith("Z")
            or not all(marker in command_text for marker in markers)
            or not float(before) - 2.0
            <= started_epoch
            <= float(before) + PROCESS_LAUNCH_WINDOW_SECONDS
        ):
            continue
        candidates.append(
            {
                "pid": pid,
                "pgid": pgid,
                "command_sha256": hashlib.sha256(command_text.encode("utf-8")).hexdigest(),
                "command_text": command_text,
                "started": started_text,
            }
        )
    return candidates


def process_group_exists(pgid: int) -> bool:
    if isinstance(pgid, bool) or not isinstance(pgid, int) or pgid <= 1:
        raise CaseError("workflow_process_group_identity_invalid")
    try:
        os.killpg(pgid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


class OwnedProcess:
    def __init__(
        self,
        *,
        name: str,
        arguments: Sequence[str],
        cwd: pathlib.Path,
        environment: Mapping[str, str],
        log_path: pathlib.Path,
        state_path: pathlib.Path,
        owned_port: int,
    ) -> None:
        self.name = name
        self.arguments = tuple(arguments)
        self.cwd = cwd
        self.environment = dict(environment)
        self.log_path = log_path
        self.state_path = state_path
        self.owned_port = owned_port
        self.process: Optional[subprocess.Popen[bytes]] = None
        self.log_handle: Optional[Any] = None

    def start(self) -> None:
        if not self.arguments or any("\x00" in item or "\n" in item for item in self.arguments):
            raise CaseError("workflow_process_command_invalid")
        if self.state_path.exists() or self.state_path.is_symlink():
            raise CaseError("workflow_stale_process_requires_cleanup")
        self.log_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.log_handle = self.log_path.open("ab", buffering=0)
        os.chmod(self.log_path, 0o600)
        arguments_sha256 = hashlib.sha256("\0".join(self.arguments).encode()).hexdigest()
        command_markers = process_command_markers(self.name, self.arguments, self.owned_port)
        launch_epoch_before = time.time()
        write_private_json(
            self.state_path,
            {
                "schema_version": 1,
                "name": self.name,
                "phase": "launching",
                "arguments_sha256": arguments_sha256,
                "owned_port": self.owned_port,
                "launching_at": utc_now(),
                "launch_epoch_before": launch_epoch_before,
                "command_markers": command_markers,
            },
        )
        try:
            self.process = subprocess.Popen(
                list(self.arguments),
                cwd=str(self.cwd),
                env=self.environment,
                stdin=subprocess.DEVNULL,
                stdout=self.log_handle,
                stderr=subprocess.STDOUT,
                start_new_session=True,
            )
            pgid = os.getpgid(self.process.pid)
            launch_epoch_after = time.time()
            write_private_json(
                self.state_path,
                {
                    "schema_version": 1,
                    "name": self.name,
                    "phase": "spawned",
                    "pid": self.process.pid,
                    "pgid": pgid,
                    "arguments_sha256": arguments_sha256,
                    "owned_port": self.owned_port,
                    "spawned_at": utc_now(),
                    "launch_epoch_before": launch_epoch_before,
                    "launch_epoch_after": launch_epoch_after,
                    "command_markers": command_markers,
                },
            )
            identity = safe_process_identity(self.process.pid)
            if (
                identity is None
                or identity["pgid"] != self.process.pid
                or not spawned_receipt_matches(
                    {
                        "launch_epoch_before": launch_epoch_before,
                        "launch_epoch_after": launch_epoch_after,
                        "command_markers": command_markers,
                    },
                    identity,
                )
            ):
                raise CaseError("workflow_process_identity_failed")
            identity.pop("command_text", None)
            identity.update(
                {
                    "schema_version": 1,
                    "name": self.name,
                    "phase": "started",
                    "arguments_sha256": arguments_sha256,
                    "owned_port": self.owned_port,
                }
            )
            write_private_json(self.state_path, identity)
        except BaseException as exc:
            cleanup_failure: Optional[Exception] = None
            try:
                self.close()
            except Exception as error:  # noqa: BLE001 - retain launch receipt for reconciliation
                cleanup_failure = error
            if cleanup_failure is None and self.state_path.exists():
                try:
                    # Popen may be interrupted after fork/posix_spawn created a
                    # child but before Python returned the object to STORE_ATTR.
                    # Reconcile the launching receipt by marker/time identity;
                    # listener availability alone cannot prove no child exists.
                    stop_stale_owned_process(self.state_path, self.name)
                except Exception as error:  # noqa: BLE001 - retain receipt on ambiguity
                    cleanup_failure = error
            if isinstance(exc, OSError):
                if cleanup_failure is not None:
                    raise CaseError("workflow_process_start_cleanup_failed") from cleanup_failure
                raise CaseError("workflow_process_start_failed") from exc
            if cleanup_failure is not None and isinstance(exc, Exception):
                raise CaseError("workflow_process_start_cleanup_failed") from cleanup_failure
            raise

    def close(self) -> None:
        process = self.process
        self.process = None
        failure: Optional[Exception] = None
        try:
            if process is not None:
                pgid = process.pid
                for signal_value, timeout in ((signal.SIGTERM, 12.0), (signal.SIGKILL, 5.0)):
                    if not process_group_exists(pgid):
                        break
                    try:
                        os.killpg(pgid, signal_value)
                    except ProcessLookupError:
                        break
                    except PermissionError as exc:
                        raise CaseError("workflow_process_group_stop_denied") from exc
                    deadline = time.monotonic() + timeout
                    while time.monotonic() < deadline:
                        process.poll()
                        if not process_group_exists(pgid):
                            break
                        time.sleep(0.05)
                process.poll()
                if process_group_exists(pgid):
                    raise CaseError("workflow_process_group_stop_failed")
                if process.poll() is None:
                    process.wait(timeout=1)
                port_deadline = time.monotonic() + 5.0
                while time.monotonic() < port_deadline and not port_available(self.owned_port):
                    time.sleep(0.05)
                if not port_available(self.owned_port):
                    raise CaseError("workflow_owned_port_not_released")
        except Exception as exc:  # noqa: BLE001 - preserve the owned-process cleanup failure
            failure = exc
        finally:
            if self.log_handle is not None:
                self.log_handle.close()
                self.log_handle = None
        if failure is not None:
            raise failure
        if process is not None:
            with contextlib.suppress(FileNotFoundError):
                self.state_path.unlink()


def stop_stale_owned_process(path: pathlib.Path, name: str) -> None:
    if not path.exists():
        return
    record = read_json_0600(path)
    phase = record.get("phase")
    owned_port = record.get("owned_port")
    if (
        record.get("schema_version") != 1
        or record.get("name") != name
        or phase not in {"launching", "spawned", "started"}
        or isinstance(owned_port, bool)
        or not isinstance(owned_port, int)
        or not 1024 <= owned_port <= 65535
    ):
        raise CaseError("workflow_process_identity_mismatch")
    if phase == "launching":
        candidates = launching_process_candidates(record)
        if len(candidates) > 1:
            raise CaseError("workflow_stale_process_identity_unverifiable")
        if not candidates:
            if not port_available(owned_port):
                raise CaseError("workflow_stale_process_identity_unverifiable")
            path.unlink()
            return
        current = candidates[0]
        pid = int(current["pid"])
        pgid = int(current["pgid"])
        if not process_group_exists(pgid):
            path.unlink()
            return
    else:
        try:
            pid = int(record["pid"])
            pgid = int(record["pgid"])
        except (KeyError, TypeError, ValueError) as exc:
            raise CaseError("workflow_process_identity_mismatch") from exc
        current = safe_process_identity(pid)
        if pgid != pid:
            raise CaseError("workflow_process_identity_mismatch")
    if phase == "spawned":
        if current is None and not process_group_exists(pgid):
            path.unlink()
            return
        if current is None or not spawned_receipt_matches(record, current):
            raise CaseError("workflow_stale_process_identity_unverifiable")
    elif phase == "started":
        if current is not None:
            for field in ("pid", "pgid", "command_sha256", "started"):
                if current.get(field) != record.get(field):
                    raise CaseError("workflow_process_identity_mismatch")
        elif not process_group_exists(pgid):
            path.unlink()
            return
        else:
            # A numeric PGID is not sufficient authority to signal a process.  If
            # the recorded leader identity is gone, keep the journal and require
            # manual inspection instead of risking a reused/unrelated group.
            raise CaseError("workflow_stale_process_identity_unverifiable")
    for signal_value, timeout in ((signal.SIGTERM, 10.0), (signal.SIGKILL, 4.0)):
        try:
            os.killpg(pgid, signal_value)
        except ProcessLookupError:
            path.unlink()
            return
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline and process_group_exists(pgid):
            time.sleep(0.05)
        if not process_group_exists(pgid):
            path.unlink()
            return
    raise CaseError("workflow_stale_process_stop_failed")


def exact_database_content_identity(spec: CaseSpec) -> str:
    """Hash every public table row and schema object through the DB lane."""
    try:
        output = support.run_lane(
            ["content-hash", spec.database],
            ROOT,
            timeout=120,
        )
    except support.HarnessError as exc:
        raise CaseError("workflow_database_content_identity_unavailable") from exc
    matched = re.fullmatch(
        "database=%s\\|content_sha256=([0-9a-f]{64})" % re.escape(spec.database),
        output.strip(),
    )
    if matched is None:
        raise CaseError("workflow_database_content_identity_invalid")
    return matched.group(1)


def physical_database_identity(
    spec: CaseSpec,
    database: support.LocalPostgres,
) -> tuple[dict[str, Any], str]:
    try:
        identity = retention.as_dict(database.read_json(retention.physical_clone_identity_sql()))
        fingerprint = retention.physical_clone_fingerprint(identity)
    except (support.HarnessError, ValueError, TypeError) as exc:
        raise CaseError("workflow_case_clone_identity_unavailable") from exc
    if (
        identity.get("database") != spec.database
        or identity.get("allow_connections") is not True
        or identity.get("is_template") is not False
    ):
        raise CaseError("workflow_case_clone_identity_invalid")
    return dict(identity), fingerprint


def build_private_values(spec: CaseSpec) -> tuple[list[str], dict[str, int]]:
    values: list[str] = []
    indexes: dict[str, int] = {}
    for label in spec.sensitive_value_labels:
        indexes[label] = len(values)
        if label == "endpoint_key":
            values.append("matrix-endpoint-" + secrets.token_urlsafe(28))
        elif label == "operator_password":
            values.append("Mx-" + secrets.token_urlsafe(30))
        else:
            values.append("matrix-%s-%s" % (label.replace("_", "-"), secrets.token_urlsafe(24)))
    return values, indexes


def preparation_handoff(receipt: Mapping[str, Any]) -> dict[str, Any]:
    return {
        "schema_version": PREPARATION_SCHEMA_VERSION,
        "run_id": RUN_ID,
        "case_id": receipt.get("case_id"),
        "state": receipt.get("state"),
        "branch_head": receipt.get("branch_head"),
        "runner_fingerprints": {
            "branch_head": receipt.get("branch_head"),
            "config": workflow.CONFIG_FINGERPRINT,
            "database_template": workflow.TEMPLATE_FINGERPRINT,
            "source_dump": workflow.SOURCE_DUMP_FINGERPRINT,
        },
        "database": receipt.get("database"),
        "database_clone": receipt.get("database_clone_fingerprint"),
        "preparation_receipt_sha256": file_sha(
            preparation_receipt_path(safe_case(str(receipt.get("case_id", ""))))
        ),
    }


def generated_artifact_receipt(path: pathlib.Path) -> dict[str, Any]:
    source = workflow.validate_private_path(path)
    metadata = source.stat()
    if source.is_symlink() or not stat.S_ISREG(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) != 0o600:
        raise CaseError("workflow_prepared_artifact_mode_invalid")
    return {
        "path": str(source),
        "sha256": file_sha(source),
        "bytes": metadata.st_size,
        "mode": "0600",
    }


def validate_prepared_artifacts(spec: CaseSpec, receipt: Mapping[str, Any]) -> None:
    """Rebind generated lane inputs before any owned service can consume them."""
    directory = lane_dir(spec).resolve()
    expected_paths: dict[str, Optional[pathlib.Path]] = {
        "config": directory / "config.json",
        "manifest": directory / "fixture-manifest.json",
        "private_values": directory / "private-values.json" if spec.sensitive_value_labels else None,
    }
    paths = receipt.get("paths")
    artifacts = receipt.get("generated_artifacts")
    indexes = receipt.get("private_value_indexes")
    if (
        not isinstance(paths, dict)
        or set(paths) != set(expected_paths)
        or not isinstance(artifacts, dict)
        or set(artifacts) != set(expected_paths)
        or not isinstance(indexes, dict)
        or indexes != {label: index for index, label in enumerate(spec.sensitive_value_labels)}
    ):
        raise CaseError("workflow_prepared_artifact_receipt_invalid")
    for name, expected_path in expected_paths.items():
        path_value = paths.get(name)
        artifact = artifacts.get(name)
        if expected_path is None:
            unexpected = directory / "private-values.json"
            if path_value is not None or artifact is not None or unexpected.exists() or unexpected.is_symlink():
                raise CaseError("workflow_prepared_artifact_receipt_invalid")
            continue
        if path_value != str(expected_path) or not isinstance(artifact, dict):
            raise CaseError("workflow_prepared_artifact_receipt_invalid")
        if set(artifact) != {"path", "sha256", "bytes", "mode"} or artifact.get("path") != str(expected_path):
            raise CaseError("workflow_prepared_artifact_receipt_invalid")
        try:
            current = generated_artifact_receipt(expected_path)
        except workflow.WorkflowError as exc:
            raise CaseError("workflow_prepared_artifact_invalid") from exc
        if not workflow.type_strict_equal(artifact, current):
            raise CaseError("workflow_prepared_artifact_changed")

    config = support.read_private_json(expected_paths["config"] or pathlib.Path())
    expected_config = support.read_private_json(BASE_CONFIG)
    try:
        parsed = urllib.parse.urlsplit(str(expected_config["database"]["url"]))
        expected_config["database"]["url"] = urllib.parse.urlunsplit(
            (parsed.scheme, parsed.netloc, "/" + spec.database, parsed.query, "")
        )
    except (KeyError, TypeError, ValueError) as exc:
        raise CaseError("workflow_prepared_config_invalid") from exc
    expected_config.setdefault("server", {})["host"] = "127.0.0.1"
    expected_config["server"]["port"] = spec.backend_port
    expected_config.setdefault("http", {})["corsAllowedOrigins"] = [spec.frontend_origin]
    expected_config.setdefault("telemetry", {})["enabled"] = False
    expected_config.setdefault("alerting", {})["webhookUrl"] = ""
    expected_config.setdefault("mail", {})["enabled"] = False
    if not workflow.type_strict_equal(config, expected_config):
        raise CaseError("workflow_prepared_config_binding_invalid")

    manifest_path = expected_paths["manifest"]
    if manifest_path is None:
        raise CaseError("workflow_prepared_manifest_invalid")
    manifest = support.read_private_json(manifest_path)
    expected_manifest = {
        "schema_version": 1,
        "run_id": RUN_ID,
        "case_id": spec.case_id,
        "fixture_scope": "case",
        "disposable": True,
        "database_clone": spec.database,
        "database_clone_identity": receipt.get("database_clone_fingerprint"),
        "chromium_executable": receipt.get("chromium_executable"),
        "chromium_bundle_sha256": receipt.get("chromium_bundle_sha256"),
        "frontend_origin": spec.frontend_origin,
        "backend_origin": spec.backend_origin,
        "mock_origins": [spec.mock_origin] if spec.mock_origin is not None else [],
    }
    if not workflow.type_strict_equal(manifest, expected_manifest):
        raise CaseError("workflow_prepared_manifest_binding_invalid")
    try:
        workflow.load_fixture_manifest(manifest_path, spec.case_id, spec.frontend_origin)
    except workflow.WorkflowError as exc:
        raise CaseError("workflow_prepared_manifest_invalid") from exc

    values_path = expected_paths["private_values"]
    if values_path is not None:
        try:
            values = workflow.load_private_values(values_path)
        except workflow.WorkflowError as exc:
            raise CaseError("workflow_prepared_private_values_invalid") from exc
        if len(values) != len(spec.sensitive_value_labels):
            raise CaseError("workflow_prepared_private_values_invalid")


def validate_live_prepared_clone(spec: CaseSpec, receipt: Mapping[str, Any]) -> None:
    validate_prepared_artifacts(spec, receipt)
    if not case_database_exists(spec):
        raise CaseError("workflow_prepared_clone_missing")
    database = support.LocalPostgres(spec.database)
    identity, fingerprint = physical_database_identity(spec, database)
    if (
        receipt.get("database_clone_identity") != identity
        or receipt.get("database_clone_fingerprint") != fingerprint
        or receipt.get("database_content_identity") != exact_database_content_identity(spec)
    ):
        raise CaseError("workflow_prepared_clone_identity_changed")


def verify_prepared_execution_inputs(spec: CaseSpec, state: Mapping[str, Any]) -> None:
    """Recheck the adopted handoff immediately before fixture or service use."""
    validate_prepared_artifacts(spec, state)
    if not case_database_exists(spec):
        raise CaseError("workflow_prepared_clone_missing")
    database = support.LocalPostgres(spec.database)
    _, fingerprint = physical_database_identity(spec, database)
    if (
        state.get("database_clone_fingerprint") != fingerprint
        or state.get("database_clone_identity") != exact_database_content_identity(spec)
    ):
        raise CaseError("workflow_prepared_clone_identity_changed")
    verify_execution_inputs(state)


def prepare_case(spec: CaseSpec) -> dict[str, Any]:
    require_ports_available(spec)
    if load_state(spec, required=False) is not None:
        raise CaseError("workflow_case_already_prepared")
    chromium_executable = workflow.discover_local_chromium()
    if chromium_executable is None:
        raise CaseError("workflow_chromium_runtime_missing")
    chromium_executable = workflow.validate_chromium_executable(chromium_executable)
    private_dir = lane_dir(spec)
    private_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(private_dir, 0o700)
    creating_receipt = load_reconcilable_creating_receipt(spec)
    if creating_receipt is not None:
        reconcile_creating_preparation(spec, creating_receipt)
        receipt = None
    else:
        receipt = load_preparation_receipt(spec, required=False)
        if receipt is not None and receipt.get("state") == "prepared":
            validate_live_prepared_clone(spec, receipt)
            return preparation_handoff(receipt)
        if receipt is not None:
            raise CaseError("workflow_preparation_already_adopted")
    common = preparation_common(spec, chromium_executable)
    creating = {
        **common,
        "state": "creating",
        "created_at": utc_now(),
    }
    write_private_json(preparation_receipt_path(spec), creating)
    backend_binary = private_dir / "bin" / "prism-backend"
    clone_created = False
    try:
        backend_build = build_case_backend(backend_binary)
        fingerprints = pinned_input_fingerprints(chromium_executable, backend_binary)
        if (
            backend_build.get("binary_sha256") != fingerprints.get("backend_binary")
            or backend_build.get("source_tree_sha256") != fingerprints.get("backend_runtime_tree")
        ):
            raise CaseError("workflow_backend_build_provenance_mismatch")
        support.run_lane(["create-case", spec.slug], ROOT)
        clone_created = True
    except support.HarnessError as exc:
        raise CaseError("workflow_case_database_create_failed") from exc
    try:
        database = support.LocalPostgres(spec.database)
        clone_identity, clone_fingerprint = physical_database_identity(spec, database)
        if spec.mock_port is not None:
            database.mutate_case(
                "UPDATE endpoints SET base_url = regexp_replace(base_url, "
                "'^http://127[.]0[.]0[.]1:[0-9]+', "
                + support.sql_literal(spec.mock_origin or "")
                + ")"
            )
        support.run_lane(["gate", spec.database], ROOT)
        content_identity = exact_database_content_identity(spec)

        config_path = private_dir / "config.json"
        support.case_config(BASE_CONFIG, config_path, spec.database, spec.backend_port)
        config = support.read_private_json(config_path)
        config.setdefault("http", {})["corsAllowedOrigins"] = [spec.frontend_origin]
        config.setdefault("telemetry", {})["enabled"] = False
        config.setdefault("alerting", {})["webhookUrl"] = ""
        config.setdefault("mail", {})["enabled"] = False
        support.atomic_write_json(config_path, config)

        private_values, indexes = build_private_values(spec)
        values_path = private_dir / "private-values.json"
        if private_values:
            support.atomic_write_json(values_path, private_values)
        elif values_path.exists():
            raise CaseError("unexpected_private_values_file")

        manifest_path = private_dir / "fixture-manifest.json"
        manifest = {
            "schema_version": 1,
            "run_id": RUN_ID,
            "case_id": spec.case_id,
            "fixture_scope": "case",
            "disposable": True,
            "database_clone": spec.database,
            "database_clone_identity": clone_fingerprint,
            "chromium_executable": str(chromium_executable),
            "chromium_bundle_sha256": fingerprints["chromium_bundle"],
            "frontend_origin": spec.frontend_origin,
            "backend_origin": spec.backend_origin,
            "mock_origins": [spec.mock_origin] if spec.mock_origin is not None else [],
        }
        support.atomic_write_json(manifest_path, manifest)
        workflow.load_fixture_manifest(manifest_path, spec.case_id, spec.frontend_origin)

        prepared = {
            **common,
            "state": "prepared",
            "created_at": creating["created_at"],
            "prepared_at": utc_now(),
            "database_clone_identity": clone_identity,
            "database_clone_fingerprint": clone_fingerprint,
            "database_content_identity": content_identity,
            "backend_binary": str(backend_binary),
            "backend_build": backend_build,
            "chromium_bundle_sha256": fingerprints["chromium_bundle"],
            "paths": {
                "config": str(config_path),
                "manifest": str(manifest_path),
                "private_values": str(values_path) if private_values else None,
            },
            "generated_artifacts": {
                "config": generated_artifact_receipt(config_path),
                "manifest": generated_artifact_receipt(manifest_path),
                "private_values": (
                    generated_artifact_receipt(values_path) if private_values else None
                ),
            },
            "private_value_indexes": indexes,
            "input_fingerprints": fingerprints,
            "allocation": None,
        }
        write_private_json(preparation_receipt_path(spec), prepared)
        validate_live_prepared_clone(spec, prepared)
        return preparation_handoff(prepared)
    except Exception:
        cleaned = False
        try:
            if clone_created and case_database_exists(spec):
                support.run_lane(["drop-case", spec.database], ROOT)
            assert_case_database_absent(spec)
            remove_preparation_artifacts(spec)
            cleaned = True
        except Exception:
            cleaned = False
        if cleaned:
            with contextlib.suppress(FileNotFoundError):
                preparation_receipt_path(spec).unlink()
        raise


def adopt_prepared_case(
    spec: CaseSpec,
    attempt: pathlib.Path,
    formal: workflow.FormalAttempt,
) -> dict[str, Any]:
    receipt = load_preparation_receipt(spec)
    if receipt is None or receipt.get("state") not in {"prepared", "adopted"}:
        raise CaseError("workflow_prepared_receipt_not_adoptable")
    validate_live_prepared_clone(spec, receipt)
    expected_allocation = {
        "result_id": formal.result_id,
        "cycle": formal.cycle,
        "attempt": formal.number,
        "result_sha256": formal.result_sha256,
    }
    if (
        receipt.get("branch_head") != formal.branch_head
        or receipt.get("database_clone_fingerprint") != formal.database_clone
        or formal.config_fingerprint != "sha256:" + file_sha(BASE_CONFIG)
    ):
        raise CaseError("workflow_formal_preparation_binding_mismatch")
    existing = load_state(spec, required=False)
    if existing is not None:
        if (
            existing.get("phase") != "adopting"
            or existing.get("attempt_dir") != str(attempt)
            or existing.get("formal_allocation") != expected_allocation
        ):
            raise CaseError("workflow_case_already_prepared")
        state = existing
    else:
        state = {
            "schema_version": SCHEMA_VERSION,
            "owner_version": OWNER_VERSION,
            "run_id": RUN_ID,
            "matrix_sha256": workflow.MATRIX_SHA256,
            "case_id": spec.case_id,
            "attempt_dir": str(attempt),
            "database": spec.database,
            "database_clone_identity": receipt.get("database_content_identity"),
            "database_clone_fingerprint": receipt.get("database_clone_fingerprint"),
            "backend_binary": receipt.get("backend_binary"),
            "backend_build": receipt.get("backend_build"),
            "chromium_executable": receipt.get("chromium_executable"),
            "chromium_bundle_sha256": receipt.get("chromium_bundle_sha256"),
            "ports": receipt.get("ports"),
            "paths": receipt.get("paths"),
            "generated_artifacts": receipt.get("generated_artifacts"),
            "private_value_indexes": receipt.get("private_value_indexes"),
            "input_fingerprints": receipt.get("input_fingerprints"),
            "formal_allocation": expected_allocation,
            "phase": "adopting",
            "scenario_steps": [],
            "processes": {},
            "prepared_at": receipt.get("prepared_at"),
            "adoption_started_at": utc_now(),
        }
        save_state(spec, state)
    bound_receipt = dict(receipt)
    if receipt.get("state") == "prepared":
        if receipt.get("allocation") is not None:
            raise CaseError("workflow_prepared_receipt_allocation_invalid")
        bound_receipt["state"] = "adopted"
        bound_receipt["allocation"] = expected_allocation
        bound_receipt["adopted_at"] = utc_now()
        write_private_json(preparation_receipt_path(spec), bound_receipt)
    elif receipt.get("allocation") != expected_allocation:
        raise CaseError("workflow_prepared_receipt_allocation_mismatch")
    state["phase"] = "prepared"
    state["adopted_at"] = bound_receipt.get("adopted_at")
    save_state(spec, state)
    return state


WFL_009_MARKER = "matrix-wfl-009-retention"
WFL_009_CALLER_PREFIX = WFL_009_MARKER + "-"
WFL_009_VOLUME = 12
# Keep the queued-cancellation fixture safely deferred past the frozen whole-
# case timeout.  If the owner times out, the backend is stopped before this
# clone-local guard can make the job runnable.
WFL_009_CANCEL_DEFER_SECONDS = CASE_TIMEOUT_SECONDS["WFL-009"] + 120


def wfl_009_cancellation_guard_sql() -> str:
    """Defer only the first UI-created manual job in the disposable clone.

    The product worker polls every five seconds and a manual purge becomes
    intentionally non-cancellable as soon as it acquires its execution fence.
    A clone-local one-row guard state makes the queued-cancellation branch
    independent from inherited terminal history.  Its compare-and-set update
    is transactional with the first insert; the second UI-created job uses the
    product's ordinary schedule and runs to completion.
    """
    return f"""
DROP TRIGGER IF EXISTS matrix_wfl_009_defer_first_manual_job ON management_jobs;
DROP FUNCTION IF EXISTS matrix_wfl_009_defer_first_manual_job();
DROP TABLE IF EXISTS matrix_wfl_009_cancel_guard_state;
CREATE TABLE matrix_wfl_009_cancel_guard_state (
    singleton boolean PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    deferred_count integer NOT NULL DEFAULT 0 CHECK (deferred_count IN (0, 1)),
    deferred_job_id text,
    deferred_at timestamptz,
    CHECK (
        (deferred_count = 0 AND deferred_job_id IS NULL AND deferred_at IS NULL)
        OR (deferred_count = 1 AND deferred_job_id IS NOT NULL AND deferred_at IS NOT NULL)
    )
);
INSERT INTO matrix_wfl_009_cancel_guard_state (singleton) VALUES (TRUE);
CREATE FUNCTION matrix_wfl_009_defer_first_manual_job()
RETURNS trigger
LANGUAGE plpgsql
AS $guard$
DECLARE
    should_defer boolean := FALSE;
BEGIN
    IF NEW.type = 'log_retention'
       AND NEW.contract_version = 2
       AND NEW.origin = 'manual'
       AND NEW.resource_key = 'request_logs'
    THEN
        UPDATE matrix_wfl_009_cancel_guard_state
        SET deferred_count = 1,
            deferred_job_id = NEW.id,
            deferred_at = clock_timestamp()
        WHERE singleton = TRUE AND deferred_count = 0
        RETURNING TRUE INTO should_defer;
        IF COALESCE(should_defer, FALSE) THEN
            NEW.next_attempt_at := clock_timestamp() + interval '{WFL_009_CANCEL_DEFER_SECONDS} seconds';
        END IF;
    END IF;
    RETURN NEW;
END
$guard$;
CREATE TRIGGER matrix_wfl_009_defer_first_manual_job
BEFORE INSERT ON management_jobs
FOR EACH ROW EXECUTE FUNCTION matrix_wfl_009_defer_first_manual_job();
"""


def prepare_wfl_009_fixture(spec: CaseSpec, state: dict[str, Any]) -> None:
    """Seed WFL-009 before backend startup, inside its exact disposable DB."""
    if spec.case_id != "WFL-009" or state.get("phase") != "prepared":
        raise CaseError("wfl_009_fixture_prepare_state_invalid")
    database = support.LocalPostgres(spec.database)
    identity = retention.as_dict(database.read_json(retention.partition_days_sql()))
    if identity.get("database") != spec.database or not identity.get("oid"):
        raise CaseError("wfl_009_clone_identity_invalid")
    purge_states = retention.as_dict(identity.get("purge_states"))
    if set(purge_states) != set(retention.MANAGED_DATASETS) or any(
        value not in {"idle", "published", "rolled_back"}
        for value in purge_states.values()
    ):
        raise CaseError("wfl_009_clone_purge_state_invalid")

    clock = retention.as_dict(
        database.read_json(
            """
SELECT jsonb_build_object(
    'server_now', to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
)
"""
        )
    )
    try:
        server_now = dt.datetime.strptime(str(clock.get("server_now")), "%Y-%m-%dT%H:%M:%SZ").replace(
            tzinfo=dt.timezone.utc
        )
    except (TypeError, ValueError) as exc:
        raise CaseError("wfl_009_database_clock_invalid") from exc
    today = server_now.replace(hour=0, minute=0, second=0, microsecond=0)
    cutoff = today - dt.timedelta(days=1)
    old_day = cutoff - dt.timedelta(days=1)
    boundary_day = cutoff
    future_day = cutoff + dt.timedelta(days=1)
    available_days = {str(value) for value in retention.as_list(identity.get("days"))}
    required_days = {
        old_day.strftime("%Y%m%d"),
        boundary_day.strftime("%Y%m%d"),
        future_day.strftime("%Y%m%d"),
    }
    if not required_days <= available_days:
        raise CaseError("wfl_009_required_partition_days_missing")

    inherited_jobs = retention.as_dict(
        database.read_json(
            """
SELECT jsonb_build_object('nonterminal_retention_jobs', count(*))
FROM management_jobs
WHERE type = 'log_retention' AND state IN ('queued', 'running', 'cancel_requested')
"""
        )
    )
    if int(inherited_jobs.get("nonterminal_retention_jobs") or 0) != 0:
        raise CaseError("wfl_009_inherited_nonterminal_retention_job")

    database.mutate_case(
        retention.setup_fixture_sql(
            WFL_009_MARKER,
            old_day,
            boundary_day,
            future_day,
            WFL_009_VOLUME,
        )
        + "\n"
        + wfl_009_cancellation_guard_sql(),
        timeout=180,
    )
    baseline = retention.as_dict(
        database.read_json(
            retention.state_sql(WFL_009_MARKER, cutoff, WFL_009_CALLER_PREFIX)
        )
    )
    if baseline.get("database") != spec.database or str(baseline.get("oid")) != str(identity.get("oid")):
        raise CaseError("wfl_009_seeded_clone_identity_changed")
    expected_counts = {"old_rows": 1, "retained_rows": WFL_009_VOLUME + 2}
    marker_counts = retention.as_dict(baseline.get("marker_counts"))
    for dataset in retention.MANAGED_DATASETS:
        actual = retention.as_dict(marker_counts.get(dataset))
        if any(int(actual.get(key, -1)) != value for key, value in expected_counts.items()):
            raise CaseError("wfl_009_fixture_marker_counts_invalid")
    guard = retention.as_dict(
        database.read_json(
            """
SELECT jsonb_build_object(
    'trigger_count', (
        SELECT count(*) FROM pg_trigger
        WHERE tgname = 'matrix_wfl_009_defer_first_manual_job' AND NOT tgisinternal
    ),
    'function_count', (
        SELECT count(*) FROM pg_proc
        WHERE proname = 'matrix_wfl_009_defer_first_manual_job' AND pronargs = 0
    ),
    'state_rows', (SELECT count(*) FROM matrix_wfl_009_cancel_guard_state),
    'deferred_count', (SELECT deferred_count FROM matrix_wfl_009_cancel_guard_state WHERE singleton),
    'deferred_job_id', (SELECT deferred_job_id FROM matrix_wfl_009_cancel_guard_state WHERE singleton)
)
"""
        )
    )
    if (
        int(guard.get("trigger_count") or 0) != 1
        or int(guard.get("function_count") or 0) != 1
        or int(guard.get("state_rows") or 0) != 1
        or int(guard.get("deferred_count") or 0) != 0
        or guard.get("deferred_job_id") is not None
    ):
        raise CaseError("wfl_009_cancellation_guard_missing")

    seeded_identity = exact_database_content_identity(spec)
    manifest_path = pathlib.Path(str(retention.as_dict(state.get("paths")).get("manifest")))
    manifest = support.read_private_json(manifest_path)
    if (
        manifest.get("case_id") != spec.case_id
        or manifest.get("database_clone") != spec.database
        or manifest.get("frontend_origin") != spec.frontend_origin
        or manifest.get("backend_origin") != spec.backend_origin
    ):
        raise CaseError("wfl_009_fixture_manifest_binding_invalid")
    manifest["database_clone_identity"] = seeded_identity
    support.atomic_write_json(manifest_path, manifest)
    workflow.load_fixture_manifest(manifest_path, spec.case_id, spec.frontend_origin)
    state["database_clone_identity"] = seeded_identity

    state["wfl_009_fixture"] = {
        "schema_version": 1,
        "marker": WFL_009_MARKER,
        "caller_prefix": WFL_009_CALLER_PREFIX,
        "volume": WFL_009_VOLUME,
        "cutoff": retention.iso(cutoff),
        "old_day": old_day.strftime("%Y%m%d"),
        "boundary_day": boundary_day.strftime("%Y%m%d"),
        "future_day": future_day.strftime("%Y%m%d"),
        "database_oid": str(identity.get("oid")),
        "database_content_identity": seeded_identity,
        "expected_marker_counts": expected_counts,
        "inherited_nonterminal_retention_jobs": 0,
        "queued_cancel_guard": {
            "scope": "first_manual_request_logs_job",
            "defer_seconds": WFL_009_CANCEL_DEFER_SECONDS,
            "present": True,
        },
    }
    state["phase"] = "fixture_seeded"
    state["fixture_seeded_at"] = utc_now()
    save_state(spec, state)


def prepare_scenario_fixture(spec: CaseSpec, state: dict[str, Any]) -> None:
    if spec.case_id == "WFL-009":
        prepare_wfl_009_fixture(spec, state)


def wfl_002_database_inventory(database: support.LocalPostgres) -> dict[str, Any]:
    """Project the exact safe model/target topology from the owned clone."""
    value = retention.as_dict(
        database.read_json(
            """
SELECT jsonb_build_object(
    'models', COALESCE(jsonb_agg(model_row ORDER BY model_row->>'model_id'), '[]'::jsonb)
)
FROM (
    SELECT jsonb_build_object(
        'id', source.id,
        'model_id', source.model_id,
        'api_family', source.api_family,
        'openai_accepted_format', source.openai_accepted_format,
        'openai_image_operations', source.openai_image_operations,
        'is_enabled', source.is_enabled,
        'access_targets', COALESCE((
            SELECT jsonb_agg(
                jsonb_build_object(
                    'id', target.id,
                    'target_type', target.target_type,
                    'target_model_id', target_model.model_id,
                    'connection_id', target.target_connection_id,
                    'position', target.position,
                    'is_enabled', target.is_enabled
                ) ORDER BY target.position, target.id
            )
            FROM model_access_targets AS target
            LEFT JOIN model_configs AS target_model
              ON target_model.id = target.target_model_config_id
             AND target_model.profile_id = target.profile_id
            WHERE target.source_model_config_id = source.id
              AND target.profile_id = source.profile_id
        ), '[]'::jsonb)
    ) AS model_row
    FROM model_configs AS source
    WHERE source.profile_id = 1 AND source.api_family = 'openai'
) AS inventory;
"""
        )
    )
    models = value.get("models")
    if not isinstance(models, list) or any(not isinstance(item, Mapping) for item in models):
        raise CaseError("wfl_002_database_inventory_invalid")
    workflow.assert_safe_json(value, "wfl_002_database_inventory")
    return value


def wait_http_ready(
    origin: str,
    path: str,
    predicate: Callable[[support.HTTPResult], bool],
    timeout: float = 40.0,
    *,
    headers: Optional[Mapping[str, str]] = None,
) -> None:
    client = support.LocalHTTP(origin, timeout=2.0)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            response = client.request("GET", path, timeout=2.0, headers=headers)
            if predicate(response):
                return
        except support.HarnessError:
            pass
        time.sleep(0.2)
    raise CaseError("workflow_service_readiness_timeout")


def sanitized_service_environment(source: Optional[Mapping[str, str]] = None) -> dict[str, str]:
    """Build a minimal child environment with no ambient Prism/DB override."""
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
        raise CaseError("workflow_service_path_missing")
    environment.update(
        {
            "CI": "1",
            "NO_UPDATE_NOTIFIER": "1",
            "NO_PROXY": "127.0.0.1,localhost",
            "no_proxy": "127.0.0.1,localhost",
        }
    )
    return environment


class ServiceGroup:
    def __init__(self, spec: CaseSpec, state: dict[str, Any]) -> None:
        self.spec = spec
        self.state = state
        self.items: list[OwnedProcess] = []

    def _journal(self, item: OwnedProcess, phase: str) -> None:
        if phase not in {"started", "stopped"}:
            raise CaseError("workflow_service_lifecycle_phase_invalid")
        processes = self.state.setdefault("processes", {})
        history = self.state.setdefault("service_lifecycle", [])
        if not isinstance(processes, dict) or not isinstance(history, list):
            raise CaseError("workflow_service_lifecycle_state_invalid")
        if phase == "started":
            receipt = read_json_0600(item.state_path)
            if (
                receipt.get("schema_version") != 1
                or receipt.get("phase") != "started"
                or receipt.get("name") != item.name
                or receipt.get("owned_port") != item.owned_port
            ):
                raise CaseError("workflow_service_lifecycle_receipt_invalid")
            projection = {
                key: receipt.get(key)
                for key in ("name", "pid", "pgid", "command_sha256", "arguments_sha256", "started")
            }
            projection["receipt_path"] = str(item.state_path)
            projection["owned_port"] = item.owned_port
            projection["phase"] = "started"
            processes[item.name] = projection
        else:
            current = processes.get(item.name)
            if not isinstance(current, dict) or current.get("phase") != "started":
                raise CaseError("workflow_service_lifecycle_state_invalid")
            processes[item.name] = {**current, "phase": "stopped", "stopped_at": utc_now()}
        history.append({"name": item.name, "phase": phase, "recorded_at": utc_now()})
        save_state(self.spec, self.state)

    def start(self) -> None:
        private_dir = lane_dir(self.spec)
        processes_dir = private_dir / "processes"
        processes_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        for name in ("frontend", "backend", "mock"):
            stop_stale_owned_process(processes_dir / (name + ".json"), name)
        require_ports_available(self.spec)

        base_environment = sanitized_service_environment()

        if self.spec.mock_port is not None:
            mock = OwnedProcess(
                name="mock",
                arguments=(
                    sys.executable,
                    str(MOCK_PROGRAM),
                    "--host", "127.0.0.1",
                    "--port", str(self.spec.mock_port),
                    "--ledger-limit", "4096",
                ),
                cwd=ROOT,
                environment=base_environment,
                log_path=private_dir / "mock.log",
                state_path=processes_dir / "mock.json",
                owned_port=self.spec.mock_port,
            )
            mock.start()
            self.items.append(mock)
            self._journal(mock, "started")
            wait_http_ready(
                self.spec.mock_origin or "",
                "/__mock__/health",
                lambda response: response.status == 200
                and isinstance(response.json(), dict)
                and response.json().get("loopback_only") is True
                and response.json().get("outbound_enabled") is False,
            )

        backend_environment = dict(base_environment)
        backend_environment["PRISM_CONFIG_PATH"] = str(self.state["paths"]["config"])
        backend = OwnedProcess(
            name="backend",
            arguments=(str(self.state["backend_binary"]),),
            cwd=BACKEND_DIR,
            environment=backend_environment,
            log_path=private_dir / "backend.log",
            state_path=processes_dir / "backend.json",
            owned_port=self.spec.backend_port,
        )
        backend.start()
        self.items.append(backend)
        self._journal(backend, "started")
        wait_http_ready(
            self.spec.backend_origin,
            "/health",
            lambda response: response.status == 200
            and isinstance(response.json(), dict)
            and response.json().get("readiness") == "ready"
            and response.json().get("startup") == "complete",
        )

        if shutil.which("pnpm") is None:
            raise CaseError("workflow_pnpm_missing")
        frontend_environment = dict(base_environment)
        frontend_environment["PRISM_VITE_PROXY_ENABLED"] = "1"
        frontend_environment["PRISM_VITE_PROXY_TARGET"] = self.spec.backend_origin
        frontend = OwnedProcess(
            name="frontend",
            arguments=(
                "pnpm", "exec", "vite",
                "--host", "127.0.0.1",
                "--port", str(self.spec.frontend_port),
                "--strictPort",
            ),
            cwd=FRONTEND_DIR,
            environment=frontend_environment,
            log_path=private_dir / "frontend.log",
            state_path=processes_dir / "frontend.json",
            owned_port=self.spec.frontend_port,
        )
        frontend.start()
        self.items.append(frontend)
        self._journal(frontend, "started")
        wait_http_ready(
            self.spec.frontend_origin,
            "/",
            lambda response: response.status == 200 and len(response.body) > 0,
            headers={"Accept": "text/html"},
        )
        self.state["phase"] = "services_ready"
        self.state["services_ready_at"] = utc_now()
        save_state(self.spec, self.state)

    def close(self) -> None:
        failure: Optional[Exception] = None
        for item in reversed(self.items):
            try:
                item.close()
                self._journal(item, "stopped")
            except Exception as exc:  # noqa: BLE001 - cleanup must attempt every owned child
                failure = failure or exc
        self.items.clear()
        if failure is not None:
            raise failure


def helper_namespace(**values: Any) -> argparse.Namespace:
    defaults = {
        "wrapper": workflow.DEFAULT_WRAPPER,
        "case_id": None,
        "confirmed_dialog_closed": False,
        "sensitive_ui_cleared": False,
    }
    defaults.update(values)
    return argparse.Namespace(**defaults)


class BrowserCase:
    def __init__(self, spec: CaseSpec, state: dict[str, Any]) -> None:
        self.spec = spec
        self.state = state
        self.case_dir = pathlib.Path(state["attempt_dir"])
        self.network: list[dict[str, Any]] = []
        self.forms: list[dict[str, Any]] = []
        self.settings: list[dict[str, Any]] = []
        self.cli: Optional[workflow.PlaywrightCLI] = None
        self.trace_active = False
        self.trace_receipts: list[dict[str, Any]] = []

    def initialize(self) -> None:
        helper_state = workflow.state_path(self.case_dir)
        if helper_state.exists():
            raise CaseError("workflow_attempt_helper_already_initialized")
        private_values_path = self.state["paths"].get("private_values")
        args = helper_namespace(
            case_id=self.spec.case_id,
            case_dir=self.case_dir,
            fixture_manifest=pathlib.Path(self.state["paths"]["manifest"]),
            private_values_file=pathlib.Path(private_values_path) if private_values_path else None,
            scratch_dir=lane_dir(self.spec) / "playwright",
            session="prism-workflow-%s-%s" % (self.spec.slug.replace("_", "-"), self.case_dir.name),
            base_url=self.spec.frontend_origin,
            chromium_executable=pathlib.Path(str(self.state["chromium_executable"])),
            chromium_bundle_sha256=str(self.state["chromium_bundle_sha256"]),
        )
        workflow.helper_init(args)
        self.cli, _ = workflow.cli_from_state(self.case_dir, workflow.DEFAULT_WRAPPER)
        self.state["phase"] = "browser_ready"
        self.state["browser_ready_at"] = utc_now()
        save_state(self.spec, self.state)

    def _require_cli(self) -> workflow.PlaywrightCLI:
        if self.cli is None:
            raise CaseError("workflow_browser_not_initialized")
        return self.cli

    def private_values(self) -> tuple[str, ...]:
        path_text = self.state["paths"].get("private_values")
        return workflow.load_private_values(pathlib.Path(path_text) if path_text else None)

    def _install_private_bindings(self, bindings: Mapping[str, int]) -> None:
        path_text = self.state["paths"].get("private_values")
        if not isinstance(path_text, str):
            raise CaseError("workflow_private_value_binding_missing")
        values_path = workflow.validate_private_path(pathlib.Path(path_text))
        values = workflow.load_private_values(values_path)
        normalized: dict[str, int] = {}
        for name, index in bindings.items():
            if (
                not re.fullmatch(r"PRISM_WFL_PRIVATE_[A-Z0-9_]{1,48}", name)
                or isinstance(index, bool)
                or not isinstance(index, int)
                or not 0 <= index < len(values)
            ):
                raise CaseError("workflow_private_value_binding_invalid")
            normalized[name] = index
        if not normalized:
            raise CaseError("workflow_private_value_binding_invalid")
        code = """async (page) => {
  const selector = 'input[data-prism-wfl-private-loader="true"]';
  const filePath = %s;
  const bindings = %s;
  let loaded = 0;
  for (const candidate of page.context().pages()) {
    await candidate.evaluate((wanted) => {
      document.querySelectorAll(wanted).forEach((element) => element.remove());
      const input = document.createElement('input');
      input.type = 'file';
      input.setAttribute('data-prism-wfl-private-loader', 'true');
      input.style.display = 'none';
      document.documentElement.appendChild(input);
    }, selector);
    await candidate.locator(selector).setInputFiles(filePath);
    const installed = await candidate.evaluate(async ({wanted, bindings}) => {
      const input = document.querySelector(wanted);
      const file = input?.files?.[0];
      if (!file) return false;
      let values;
      try {
        values = JSON.parse(await file.text());
      } finally {
        input.value = '';
        input.remove();
      }
      if (!Array.isArray(values)) return false;
      const projected = {};
      for (const [name, index] of Object.entries(bindings)) {
        const value = values[index];
        if (typeof value !== 'string' || value.length === 0) return false;
        projected[name] = value;
      }
      Object.defineProperty(window, '__prismWflPrivate', {
        value: Object.freeze(projected), configurable: true, enumerable: false,
      });
      return true;
    }, {wanted: selector, bindings});
    if (!installed) throw new Error('workflow_private_binding_install_failed');
    loaded += 1;
  }
  if (loaded < 1) throw new Error('workflow_private_binding_page_missing');
  return {loaded};
}""" % (json.dumps(str(values_path)), json.dumps(normalized, sort_keys=True))
        output = self._require_cli().run("run-code", code, timeout=60)
        receipt = workflow.parse_eval_result(output)
        loaded = receipt.get("loaded") if isinstance(receipt, Mapping) else None
        # The exact page count is intentionally not persisted, but at least
        # one page must have received the sealed binding.
        if isinstance(loaded, bool) or not isinstance(loaded, int) or loaded < 1:
            raise CaseError("workflow_private_value_binding_install_failed")

    def _clear_private_bindings(self) -> None:
        code = """async (page) => {
  let cleared = 0;
  let residue = 0;
  let unverified = 0;
  for (const candidate of page.context().pages()) {
    const result = await candidate.evaluate(() => {
      const present = Object.prototype.hasOwnProperty.call(window, '__prismWflPrivate');
      Reflect.deleteProperty(window, '__prismWflPrivate');
      for (const input of document.querySelectorAll('input[data-prism-wfl-private-loader="true"]')) {
        input.value = '';
        input.remove();
      }
      return {
        removed: present,
        residue: Object.prototype.hasOwnProperty.call(window, '__prismWflPrivate')
          || document.querySelector('input[data-prism-wfl-private-loader="true"]') !== null,
      };
    }).catch(() => ({removed: false, residue: false, unverified: !candidate.isClosed()}));
    if (result.removed) cleared += 1;
    if (result.residue) residue += 1;
    if (result.unverified) unverified += 1;
  }
  return {cleared, residue, unverified};
}"""
        output = self._require_cli().run("run-code", code, timeout=60)
        receipt = workflow.parse_eval_result(output)
        if (
            not isinstance(receipt, Mapping)
            or receipt.get("residue") != 0
            or receipt.get("unverified") != 0
        ):
            raise CaseError("workflow_private_value_binding_clear_failed")

    def capture_snapshot(self, label: str, *, group: Optional[str] = None) -> dict[str, Any]:
        cli = self._require_cli()
        entry = cli.snapshot(label, self.case_dir)
        helper_state = workflow.load_state(self.case_dir)
        helper_state["snapshots"].append(entry)
        workflow.save_state(self.case_dir, helper_state)
        projection = {
            "label": label,
            "path": entry["snapshot"],
            "bytes": entry["bytes"],
            "sha256": entry["sha256"],
            "fatal_markers": entry["fatal_markers"],
        }
        if group == "form":
            self.forms.append(projection)
        elif group == "settings":
            self.settings.append(projection)
        return projection

    def goto(self, path: str) -> None:
        self._require_cli().goto(path, wait_ms=1200)

    def start_trace(self, *, sensitive_ui_cleared: bool = False) -> dict[str, Any]:
        if self.trace_active:
            raise CaseError("workflow_trace_already_active")
        receipt = workflow.helper_trace_start(
            helper_namespace(
                case_dir=self.case_dir,
                sensitive_ui_cleared=sensitive_ui_cleared,
                confirmed_dialog_closed=sensitive_ui_cleared,
            )
        )
        self.trace_active = True
        projected = workflow.safe_json_projection(receipt)
        if not isinstance(projected, dict):
            raise CaseError("workflow_trace_receipt_invalid")
        self.trace_receipts.append(projected)
        return projected

    def stop_trace(self) -> dict[str, Any]:
        if not self.trace_active:
            raise CaseError("workflow_trace_not_active")
        receipt = workflow.helper_trace_stop(helper_namespace(case_dir=self.case_dir))
        self.trace_active = False
        projected = workflow.safe_json_projection(receipt)
        if not isinstance(projected, dict):
            raise CaseError("workflow_trace_receipt_invalid")
        self.trace_receipts.append(projected)
        return projected

    def checkpoint(self, name: str) -> None:
        if name not in self.spec.scenario_steps:
            raise CaseError("workflow_scenario_checkpoint_not_owned")
        workflow.helper_checkpoint(helper_namespace(case_dir=self.case_dir, name=name))
        self.state["scenario_steps"].append({"name": name, "recorded_at": utc_now()})
        save_state(self.spec, self.state)

    def run_code(
        self,
        step: str,
        code: str,
        *,
        private_environment: Optional[Mapping[str, int]] = None,
        timeout: int = 120,
    ) -> dict[str, Any]:
        if not SAFE_STEP_RE.fullmatch(step):
            raise CaseError("workflow_scenario_step_name_invalid")
        self.capture_snapshot("%s-before" % step)
        wrapped_code = """async (page) => {
  try {
    return await (%s)(page);
  } catch (error) {
    const code = String(error && error.message || '');
    if (/^[a-z][a-z0-9_]{2,95}$/.test(code)) {
      return {ok: false, assertion_failure: true, failure_code: code, network_events: []};
    }
    throw error;
  }
}""" % code
        try:
            if private_environment:
                self._install_private_bindings(private_environment)
            output = self._require_cli().run(
                "run-code",
                wrapped_code,
                timeout=timeout,
            )
        finally:
            if private_environment:
                self._clear_private_bindings()
        value = workflow.parse_eval_result(output)
        projected = workflow.safe_json_projection(value)
        if not isinstance(projected, dict):
            raise CaseError("workflow_browser_result_shape_invalid")
        workflow.assert_safe_json(projected, step, self.private_values())
        events = projected.pop("network_events", [])
        if not isinstance(events, list):
            raise CaseError("workflow_browser_network_shape_invalid")
        for item in events:
            if not isinstance(item, dict):
                raise CaseError("workflow_browser_network_shape_invalid")
            item = dict(item)
            item["step"] = step
            workflow.assert_safe_json(item, "network", self.private_values())
            self.network.append(item)
        if projected.get("ok") is not True:
            failure_code = projected.get("failure_code")
            if (
                projected.get("assertion_failure") is not True
                or not isinstance(failure_code, str)
                or not SAFE_STEP_RE.fullmatch(failure_code)
            ):
                raise CaseError("workflow_browser_result_shape_invalid")
            self.capture_snapshot("%s-failed" % step)
            raise CaseError(
                failure_code,
                assertion_failure=failure_code not in BROWSER_INFRA_FAILURE_CODES,
            )
        self.capture_snapshot("%s-after" % step)
        return projected

    def write_json(self, name: str, value: Mapping[str, Any]) -> None:
        if name not in workflow.REQUIRED_EVIDENCE[self.spec.case_id]:
            raise CaseError("workflow_evidence_name_not_required")
        path = self.case_dir / name
        if path.exists() or path.is_symlink():
            raise CaseError("workflow_evidence_overwrite_refused")
        document = {"schema_version": 1, "case_id": self.spec.case_id, **dict(value)}
        private_values = self.private_values()
        workflow.assert_safe_json(document, name, private_values)
        workflow.write_json(path, document, private_values)

    def write_text(self, name: str, text: str) -> None:
        if name not in workflow.REQUIRED_EVIDENCE[self.spec.case_id]:
            raise CaseError("workflow_evidence_name_not_required")
        path = self.case_dir / name
        if path.exists() or path.is_symlink():
            raise CaseError("workflow_evidence_overwrite_refused")
        values = workflow.load_private_values(
            pathlib.Path(self.state["paths"]["private_values"])
            if self.state["paths"].get("private_values")
            else None
        )
        redacted = workflow.redact_text(text, values)
        workflow.assert_no_remaining_secret(redacted, name, values)
        workflow.write_text(path, redacted)

    def promote_snapshot(self, name: str, snapshot: Mapping[str, Any]) -> None:
        relative_text = snapshot.get("path")
        if not isinstance(relative_text, str):
            raise CaseError("workflow_snapshot_evidence_path_invalid")
        relative = pathlib.Path(relative_text)
        if relative.is_absolute() or not relative.parts or relative.parts[0] != "snapshots" or ".." in relative.parts:
            raise CaseError("workflow_snapshot_evidence_path_invalid")
        source = self.case_dir / relative
        try:
            source.resolve(strict=True).relative_to(self.case_dir.resolve(strict=True))
        except (FileNotFoundError, ValueError) as exc:
            raise CaseError("workflow_snapshot_evidence_path_invalid") from exc
        if source.is_symlink() or not source.is_file():
            raise CaseError("workflow_snapshot_evidence_path_invalid")
        if workflow.sha256_file(source) != snapshot.get("sha256"):
            raise CaseError("workflow_snapshot_evidence_digest_mismatch")
        if snapshot.get("fatal_markers"):
            raise CaseError("workflow_snapshot_evidence_fatal_marker", assertion_failure=True)
        self.write_text(name, source.read_text(encoding="utf-8"))

    def close(self) -> None:
        workflow.helper_close(helper_namespace(case_dir=self.case_dir))


READONLY_BROWSER_TRANSITIONS: Mapping[str, frozenset[str]] = {
    "planned": frozenset({"constructed", "purged"}),
    "constructed": frozenset({"opening", "purged"}),
    "opening": frozenset({"opened", "closing"}),
    "opened": frozenset({"trace_starting", "closing"}),
    "trace_starting": frozenset({"trace_active", "trace_abandoned"}),
    "trace_active": frozenset({"trace_stopping", "trace_abandoned"}),
    "trace_stopping": frozenset({"trace_packaged", "trace_abandoned"}),
    "trace_packaged": frozenset({"closing"}),
    "trace_abandoned": frozenset({"closing"}),
    "closing": frozenset({"closed"}),
    "closed": frozenset({"purged"}),
    "purged": frozenset(),
}


def readonly_browser_plan(spec: CaseSpec, state: dict[str, Any]) -> dict[str, Any]:
    if spec.case_id not in READONLY_CASES:
        raise CaseError("workflow_readonly_browser_case_invalid")
    session = "prism-workflow-%s-%s" % (
        spec.slug.replace("_", "-"), pathlib.Path(state["attempt_dir"]).name
    )
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{2,62}", session):
        raise CaseError("workflow_readonly_browser_session_invalid")
    scratch = (lane_dir(spec) / "playwright").resolve()
    workflow.validate_private_path(scratch, require_file=False)
    if scratch.exists() or scratch.is_symlink():
        raise CaseError("workflow_readonly_browser_scratch_not_fresh")
    receipt = {
        "schema_version": 1,
        "case_id": spec.case_id,
        "attempt_dir": state["attempt_dir"],
        "session": session,
        "scratch_dir": str(scratch),
        "base_url": spec.frontend_origin,
        "wrapper": str(workflow.DEFAULT_WRAPPER),
        "chromium_executable": state["chromium_executable"],
        "chromium_bundle_sha256": state["chromium_bundle_sha256"],
        "phase": "planned",
        "history": [{"phase": "planned", "recorded_at": utc_now()}],
    }
    state["readonly_browser"] = receipt
    save_state(spec, state)
    return receipt


def validate_readonly_browser_receipt(
    spec: CaseSpec,
    state: Mapping[str, Any],
) -> dict[str, Any]:
    value = state.get("readonly_browser")
    if not isinstance(value, dict):
        raise CaseError("workflow_readonly_browser_receipt_missing")
    expected_scratch = (lane_dir(spec) / "playwright").resolve()
    scratch_path = pathlib.Path(str(value.get("scratch_dir", "")))
    expected_session = "prism-workflow-%s-%s" % (
        spec.slug.replace("_", "-"), pathlib.Path(str(state.get("attempt_dir", ""))).name
    )
    if (
        value.get("schema_version") != 1
        or value.get("case_id") != spec.case_id
        or value.get("attempt_dir") != state.get("attempt_dir")
        or value.get("base_url") != spec.frontend_origin
        or value.get("session") != expected_session
        or not re.fullmatch(r"[a-z0-9][a-z0-9-]{2,62}", str(value.get("session", "")))
        or not scratch_path.is_absolute()
        or scratch_path.resolve() != expected_scratch
        or scratch_path.is_symlink()
        or value.get("wrapper") != str(workflow.DEFAULT_WRAPPER)
        or value.get("chromium_executable") != state.get("chromium_executable")
        or value.get("chromium_bundle_sha256") != state.get("chromium_bundle_sha256")
        or value.get("phase") not in READONLY_BROWSER_TRANSITIONS
        or not isinstance(value.get("history"), list)
    ):
        raise CaseError("workflow_readonly_browser_identity_mismatch")
    phases = [item.get("phase") for item in value["history"] if isinstance(item, Mapping)]
    if (
        len(phases) != len(value["history"])
        or not phases
        or phases[0] != "planned"
        or phases[-1] != value.get("phase")
    ):
        raise CaseError("workflow_readonly_browser_history_invalid")
    for previous, current in zip(phases, phases[1:]):
        if current not in READONLY_BROWSER_TRANSITIONS.get(str(previous), frozenset()):
            raise CaseError("workflow_readonly_browser_history_invalid")
    return value


def readonly_lifecycle_callback(
    spec: CaseSpec,
    state: dict[str, Any],
) -> Callable[[str, Mapping[str, Any]], None]:
    def record(phase: str, detail: Mapping[str, Any]) -> None:
        receipt = validate_readonly_browser_receipt(spec, state)
        previous = str(receipt["phase"])
        if phase not in READONLY_BROWSER_TRANSITIONS[previous]:
            raise CaseError("workflow_readonly_browser_transition_invalid")
        workflow.assert_safe_json(detail, "readonly_browser_lifecycle")
        receipt["phase"] = phase
        receipt["history"].append(
            {"phase": phase, "recorded_at": utc_now(), "detail": dict(detail)}
        )
        state["readonly_browser"] = receipt
        save_state(spec, state)

    return record


def reconcile_readonly_browser(spec: CaseSpec, state: dict[str, Any]) -> None:
    receipt = validate_readonly_browser_receipt(spec, state)
    phase = str(receipt["phase"])
    scratch = pathlib.Path(str(receipt["scratch_dir"]))
    if phase == "purged":
        if scratch.exists() or scratch.is_symlink():
            raise CaseError("workflow_readonly_browser_purge_mismatch")
        return
    if phase in {"planned", "constructed"}:
        if scratch.exists():
            if not scratch.is_dir() or scratch.is_symlink():
                raise CaseError("workflow_cleanup_browser_scratch_invalid")
            workflow.purge_private_scratch_tree(scratch)
        receipt["phase"] = "purged"
        receipt["history"].append(
            {"phase": "purged", "recorded_at": utc_now(), "detail": {"never_opened": True}}
        )
        state["readonly_browser"] = receipt
        save_state(spec, state)
        return
    if phase in {"trace_starting", "trace_active", "trace_stopping"}:
        trace_path = pathlib.Path(str(receipt["attempt_dir"])) / "trace.zip"
        packaged = False
        if trace_path.is_file() and not trace_path.is_symlink():
            try:
                workflow.validate_trace_archive(
                    trace_path,
                    (),
                    require_redaction_manifest=True,
                )
                packaged = True
            except workflow.WorkflowError:
                packaged = False
        if packaged:
            recovered_transitions = {
                "trace_starting": ("trace_active", "trace_stopping", "trace_packaged"),
                "trace_active": ("trace_stopping", "trace_packaged"),
                "trace_stopping": ("trace_packaged",),
            }[phase]
            for recovered_phase in recovered_transitions:
                detail: dict[str, Any] = {"reconciled": True}
                if recovered_phase == "trace_packaged":
                    detail.update(
                        {
                            "path": "trace.zip",
                            "bytes": trace_path.stat().st_size,
                            "sha256": workflow.sha256_file(trace_path),
                        }
                    )
                receipt["phase"] = recovered_phase
                receipt["history"].append(
                    {
                        "phase": recovered_phase,
                        "recorded_at": utc_now(),
                        "detail": detail,
                    }
                )
        else:
            receipt["phase"] = "trace_abandoned"
            receipt["history"].append(
                {
                    "phase": "trace_abandoned",
                    "recorded_at": utc_now(),
                    "detail": {
                        "reason": "trace_not_packaged",
                        "reconciled": True,
                        "trace_started_confirmed": phase in {"trace_active", "trace_stopping"},
                        "candidate_present": trace_path.exists() or trace_path.is_symlink(),
                    },
                }
            )
        state["readonly_browser"] = receipt
        save_state(spec, state)
        phase = str(receipt["phase"])
    if phase != "closed":
        if not scratch.is_dir() or scratch.is_symlink():
            raise CaseError("workflow_cleanup_browser_scratch_invalid")
        if phase != "closing":
            if "closing" not in READONLY_BROWSER_TRANSITIONS[phase]:
                raise CaseError("workflow_readonly_browser_transition_invalid")
            receipt["phase"] = "closing"
            receipt["history"].append(
                {"phase": "closing", "recorded_at": utc_now(), "detail": {"reconciled": True}}
            )
            state["readonly_browser"] = receipt
            save_state(spec, state)
        try:
            workflow.close_named_session(
                str(receipt["session"]), scratch, pathlib.Path(str(receipt["wrapper"]))
            )
        except workflow.WorkflowError as exc:
            raise CaseError("workflow_cleanup_browser_close_failed") from exc
        receipt["phase"] = "closed"
        receipt["history"].append(
            {"phase": "closed", "recorded_at": utc_now(), "detail": {"reconciled": True}}
        )
        state["readonly_browser"] = receipt
        save_state(spec, state)
    if scratch.exists():
        if not scratch.is_dir() or scratch.is_symlink():
            raise CaseError("workflow_cleanup_browser_scratch_invalid")
        workflow.purge_private_scratch_tree(scratch)
    receipt["phase"] = "purged"
    receipt["history"].append(
        {"phase": "purged", "recorded_at": utc_now(), "detail": {"reconciled": True}}
    )
    state["readonly_browser"] = receipt
    save_state(spec, state)


def javascript_action(body: str) -> str:
    """Wrap a constant UI action with same-origin, safe network telemetry."""
    return """async (page) => {
  const expectedOrigin = %s;
  const urlParts = (value) => {
    const text = String(value);
    const scheme = text.indexOf('://');
    if (scheme < 0) {
      const stop = [text.indexOf('?'), text.indexOf('#')].filter((item) => item >= 0);
      return {origin: '', pathname: text.slice(0, stop.length ? Math.min(...stop) : text.length)};
    }
    const firstSlash = text.indexOf('/', scheme + 3);
    const origin = firstSlash < 0 ? text : text.slice(0, firstSlash);
    const suffix = firstSlash < 0 ? '/' : text.slice(firstSlash);
    const stop = [suffix.indexOf('?'), suffix.indexOf('#')].filter((item) => item >= 0);
    return {origin, pathname: suffix.slice(0, stop.length ? Math.min(...stop) : suffix.length)};
  };
  const urlOrigin = (value) => urlParts(value).origin;
  const urlPath = (value) => urlParts(value).pathname;
  if (urlOrigin(page.url()) !== expectedOrigin) throw new Error('workflow_origin_mismatch');
  const networkEvents = [];
  const listener = (response) => {
    if (urlOrigin(response.url()) !== expectedOrigin) return;
    const path = urlPath(response.url());
    if (!path.startsWith('/api/') && !path.startsWith('/v1')) return;
    networkEvents.push({method: response.request().method(), path, status: response.status()});
  };
  page.on('response', listener);
  let result;
  try {
    result = await (async () => {
      %s
    })();
  } finally {
    page.off('response', listener);
  }
  if (result === null || typeof result !== 'object' || Array.isArray(result)) throw new Error('workflow_result_invalid');
  return {ok: true, ...result, network_events: networkEvents.slice(-64)};
}""" % (json.dumps("http://127.0.0.1:0"), body)


def action_for_origin(origin: str, body: str) -> str:
    if workflow.validate_loopback_origin(origin) != origin:
        raise CaseError("workflow_browser_origin_invalid")
    template = javascript_action(body)
    return template.replace(json.dumps("http://127.0.0.1:0"), json.dumps(origin), 1)


def api_object(client: support.LocalHTTP, path: str) -> dict[str, Any]:
    _, value = client.json("GET", path, expected=(200,))
    if not isinstance(value, dict):
        raise CaseError("workflow_api_object_expected")
    return value


def api_list(client: support.LocalHTTP, path: str) -> list[dict[str, Any]]:
    _, value = client.json("GET", path, expected=(200,))
    if isinstance(value, dict) and isinstance(value.get("data"), list):
        value = value["data"]
    if not isinstance(value, list) or any(not isinstance(item, dict) for item in value):
        raise CaseError("workflow_api_list_expected")
    return value


def find_named(items: Iterable[Mapping[str, Any]], field: str, value: str) -> dict[str, Any]:
    matches = [dict(item) for item in items if item.get(field) == value]
    if len(matches) != 1:
        raise CaseError("workflow_fixture_named_item_not_unique")
    return matches[0]


def safe_id(value: Any) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise CaseError("workflow_fixture_id_invalid")
    return value


def post_object(
    client: support.LocalHTTP,
    path: str,
    body: Mapping[str, Any],
    *,
    expected: Sequence[int] = (200, 201),
) -> dict[str, Any]:
    _, value = client.json("POST", path, body=body, expected=expected)
    if not isinstance(value, dict):
        raise CaseError("workflow_api_object_expected")
    return value


def private_value(browser: BrowserCase, label: str) -> str:
    path_text = browser.state["paths"].get("private_values")
    index = browser.state["private_value_indexes"].get(label)
    if not isinstance(path_text, str) or isinstance(index, bool) or not isinstance(index, int):
        raise CaseError("workflow_private_value_binding_missing")
    values = workflow.load_private_values(pathlib.Path(path_text))
    if not 0 <= index < len(values):
        raise CaseError("workflow_private_value_binding_invalid")
    return values[index]


def create_runtime_fixture(
    browser: BrowserCase,
    client: support.LocalHTTP,
    *,
    prefix: str,
    with_pricing: bool = False,
    with_fallback: bool = False,
    until_reset_failure: bool = False,
    with_image_generation: bool = False,
) -> dict[str, Any]:
    """Create deterministic setup resources through management APIs.

    These writes are setup, not workflow evidence.  Every resource is namespaced
    to the case clone; raw endpoint material comes from the private redaction
    file and is never returned in this metadata projection.
    """
    endpoint_key = private_value(browser, "endpoint_key")
    strategy = post_object(
        client,
        "/api/loadbalance/strategies",
        {
            "name": prefix + "-strategy",
            "legacy_strategy_type": "fill-first",
            "failure_status_codes": [500, 502, 503, 504],
            "retry_base_delay_ms": 0,
            "retry_backoff_multiplier": 1.0,
            "retry_jitter_ratio": 0.0,
            "retry_max_delay_ms": 1,
            "cycle_retry_attempt_limit": 1 if until_reset_failure else 3,
            "ban_mode": "until_reset" if until_reset_failure else "off",
            "ban_cumulative_retry_attempt_threshold": 1 if until_reset_failure else 0,
            "ban_duration_seconds": 0,
        },
    )
    endpoint = post_object(
        client,
        "/api/endpoints",
        {"name": prefix + "-primary", "base_url": browser.spec.mock_origin, "api_key": endpoint_key},
    )
    pricing: Optional[dict[str, Any]] = None
    if with_pricing:
        pricing = post_object(
            client,
            "/api/pricing-templates",
            {
                "name": prefix + "-pricing",
                "description": "deterministic local workflow pricing",
                "input_price": "1.000000",
                "output_price": "2.000000",
                "cached_input_price": "0.500000",
                "cache_creation_price": "0.750000",
                "reasoning_price": "3.000000",
            },
        )
    initial_target: dict[str, Any] = {
        "endpoint_id": safe_id(endpoint.get("id")),
        "name": prefix + "-terminal-primary",
        "is_active": True,
        "openai_text_capability": "dual_native",
    }
    if with_image_generation:
        initial_target["openai_image_capability"] = "generations"
    if pricing is not None:
        initial_target["pricing_template_id"] = safe_id(pricing.get("id"))
    envelope = post_object(
        client,
        "/api/models",
        {
            "api_family": "openai",
            "model_id": prefix + "-model",
            "display_name": prefix + " model",
            "openai_accepted_format": "dual_native",
            "openai_image_operations": "generations" if with_image_generation else None,
            "loadbalance_strategy_id": safe_id(strategy.get("id")),
            "is_enabled": True,
            "initial_terminal_target": initial_target,
        },
    )
    model = envelope.get("model")
    if not isinstance(model, dict):
        raise CaseError("workflow_fixture_model_create_shape_invalid")
    access_targets = model.get("access_targets")
    if not isinstance(access_targets, list):
        raise CaseError("workflow_fixture_initial_target_shape_invalid")
    primary_targets = [
        item
        for item in access_targets
        if isinstance(item, dict)
        and item.get("target_type") == "connection"
        and item.get("connection_id") is not None
    ]
    if len(primary_targets) != 1:
        raise CaseError("workflow_fixture_initial_target_shape_invalid")
    primary_target = primary_targets[0]
    result: dict[str, Any] = {
        "strategy_id": safe_id(strategy.get("id")),
        "endpoint_id": safe_id(endpoint.get("id")),
        "model_id": safe_id(model.get("id")),
        "runtime_model": prefix + "-model",
        "pricing_id": safe_id(pricing.get("id")) if pricing is not None else None,
        "endpoint_label": prefix + "-primary",
        "primary_access_target_id": safe_id(primary_target.get("id")),
        "primary_connection_id": safe_id(primary_target.get("connection_id")),
        "primary_terminal_label": prefix + "-terminal-primary",
    }
    if with_fallback:
        fallback_endpoint = post_object(
            client,
            "/api/endpoints",
            {"name": prefix + "-fallback", "base_url": browser.spec.mock_origin, "api_key": endpoint_key},
        )
        connection_envelope = post_object(
            client,
            "/api/models/%d/connections" % result["model_id"],
            {
                "endpoint_id": safe_id(fallback_endpoint.get("id")),
                "name": prefix + "-terminal-fallback",
                "is_active": True,
                "openai_text_capability": "dual_native",
                "pricing_template_id": result["pricing_id"],
            },
        )
        connection = connection_envelope.get("connection")
        if not isinstance(connection, dict):
            raise CaseError("workflow_fixture_connection_create_shape_invalid")
        result["fallback_endpoint_id"] = safe_id(fallback_endpoint.get("id"))
        result["fallback_connection_id"] = safe_id(connection.get("id"))
        result["fallback_endpoint_label"] = prefix + "-fallback"
        result["fallback_terminal_label"] = prefix + "-terminal-fallback"
    return result


def ui_body(action: str) -> str:
    """Install bounded locator helpers before one visible UI action."""
    helpers = r"""
const expectVisible = async (locator, code) => {
  await locator.first().waitFor({state: 'visible', timeout: 15000}).catch(() => { throw new Error(code); });
  return locator.first();
};
const clickButton = async (root, pattern, code) => {
  const button = await expectVisible(root.getByRole('button', {name: pattern}), code);
  await button.click();
};
const fillLabel = async (root, pattern, value, code) => {
  const input = await expectVisible(root.getByLabel(pattern), code);
  await input.fill(value);
};
const fillPrivateLocator = async (locator, binding, code) => {
  const input = await expectVisible(locator, code);
  const applied = await input.evaluate((element, name) => {
    const value = window.__prismWflPrivate?.[name];
    if (typeof value !== 'string' || value.length === 0) return false;
    const prototype = element instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(prototype, 'value')?.set;
    if (typeof setter !== 'function') return false;
    setter.call(element, value);
    element.dispatchEvent(new Event('input', {bubbles: true}));
    element.dispatchEvent(new Event('change', {bubbles: true}));
    return true;
  }, binding);
  if (!applied) throw new Error('workflow_private_binding_missing');
};
const fillPrivateLabel = async (root, pattern, binding, code) => {
  await fillPrivateLocator(root.getByLabel(pattern), binding, code);
};
const chooseLabel = async (root, labelPattern, optionPattern, code) => {
  const select = await expectVisible(root.getByLabel(labelPattern), code + '_select');
  await select.click();
  const option = await expectVisible(page.getByRole('option', {name: optionPattern}), code + '_option');
  await option.click();
};
const responseFor = (method, pathPattern) => page.waitForResponse((response) => {
  return response.request().method() === method && pathPattern.test(urlPath(response.url()));
}, {timeout: 20000});
"""
    return helpers + "\n" + action


def wfl_003_target_projection(value: Any) -> list[dict[str, Any]]:
    """Project the exact mixed-target persistence fields shared by API and DB."""
    def projection_id(raw: Any) -> int:
        if isinstance(raw, bool) or not isinstance(raw, int) or raw <= 0:
            raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
        return raw

    if not isinstance(value, list):
        raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
    result: list[dict[str, Any]] = []
    for raw in value:
        if not isinstance(raw, Mapping):
            raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
        target_id = projection_id(raw.get("id"))
        target_type = raw.get("target_type")
        position = raw.get("position")
        is_enabled = raw.get("is_enabled")
        if target_type not in {"model", "connection"}:
            raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
        if isinstance(position, bool) or not isinstance(position, int) or position < 0:
            raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
        if not isinstance(is_enabled, bool):
            raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
        connection = raw.get("connection")
        if target_type == "model":
            target_model_id = raw.get("target_model_id")
            if not isinstance(target_model_id, str) or not target_model_id:
                raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
            target_model = raw.get("target_model")
            if not isinstance(target_model, Mapping) or target_model.get("model_id") != target_model_id:
                raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
            display_name = target_model.get("display_name")
            if display_name is not None and not isinstance(display_name, str):
                raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
            target_model_label: Optional[str] = display_name.strip() if isinstance(display_name, str) and display_name.strip() else target_model_id
            connection_id: Optional[int] = None
            connection_name: Optional[str] = None
            pricing_template_id: Optional[int] = None
        else:
            target_model_id = None
            target_model_label = None
            connection_id = projection_id(raw.get("connection_id"))
            if not isinstance(connection, Mapping):
                raise CaseError("wfl_003_target_projection_shape_invalid", assertion_failure=True)
            connection_name_value = connection.get("name")
            connection_name = connection_name_value if isinstance(connection_name_value, str) else None
            pricing_value = connection.get("pricing_template_id")
            pricing_template_id = projection_id(pricing_value) if pricing_value is not None else None
        result.append(
            {
                "id": target_id,
                "target_type": target_type,
                "target_model_id": target_model_id,
                "target_model_label": target_model_label,
                "connection_id": connection_id,
                "position": position,
                "is_enabled": is_enabled,
                "connection_name": connection_name,
                "pricing_template_id": pricing_template_id,
            }
        )
    return result


def wfl_003_database_targets(
    database: support.LocalPostgres,
    model_config_id: int,
) -> list[dict[str, Any]]:
    value = database.read_json(
        """
SELECT json_build_object(
  'targets', coalesce(json_agg(json_build_object(
    'id', mat.id,
    'target_type', mat.target_type,
    'target_model_id', target_model.model_id,
    'target_model', CASE WHEN target_model.id IS NULL THEN NULL ELSE json_build_object(
      'model_id', target_model.model_id,
      'display_name', target_model.display_name
    ) END,
    'connection_id', mat.target_connection_id,
    'position', mat.position,
    'is_enabled', mat.is_enabled,
    'connection', CASE WHEN terminal.id IS NULL THEN NULL ELSE json_build_object(
      'name', terminal.name,
      'pricing_template_id', terminal.pricing_template_id
    ) END
  ) ORDER BY mat.position, mat.id), '[]'::json)
)
FROM model_access_targets mat
LEFT JOIN model_configs target_model ON target_model.id = mat.target_model_config_id
LEFT JOIN connections terminal ON terminal.id = mat.target_connection_id
WHERE mat.source_model_config_id = %d;
""" % model_config_id
    )
    return wfl_003_target_projection(value.get("targets"))


def run_wfl_003(browser: BrowserCase, client: support.LocalHTTP, database: support.LocalPostgres) -> None:
    spec = browser.spec
    origin = spec.frontend_origin
    endpoint_name = "matrix-wfl-003-endpoint"
    pricing_name = "matrix-wfl-003-pricing"
    ban_name = "matrix-wfl-003-ban"
    model_id = "matrix/wfl-003-model"
    model_name = "Matrix WFL 003 Model"
    terminal_name = "matrix-wfl-003-terminal"
    target_model_id = "codex/gpt-5.5"
    endpoint_key_index = int(browser.state["private_value_indexes"]["endpoint_key"])

    browser.start_trace()
    browser.goto("/route/endpoints")
    endpoint_action = browser.run_code(
        "endpoint_create",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('endpoints-feature-page').waitFor({state: 'visible', timeout: 15000});
await clickButton(page, /添加端点|Add endpoint/i, 'endpoint_add_missing');
const dialog = await expectVisible(page.getByRole('dialog', {name: /新建端点|New endpoint/i}), 'endpoint_dialog_missing');
let invalidPostCount = 0;
const invalidRequestListener = (request) => {
  if (request.method() === 'POST' && urlPath(request.url()) === '/api/endpoints') invalidPostCount += 1;
};
page.on('request', invalidRequestListener);
await clickButton(dialog, /仅保存|Save only/i, 'endpoint_save_missing');
await page.waitForTimeout(250);
page.off('request', invalidRequestListener);
const validationErrors = await dialog.locator('[data-slot="form-message"]:visible').count();
if (invalidPostCount !== 0) throw new Error('endpoint_invalid_form_posted');
if (validationErrors < 2) throw new Error('endpoint_frontend_validation_missing');
await fillLabel(dialog, /^名称$|^Name$/i, %s, 'endpoint_name_missing');
await fillLabel(dialog, /基础 URL|Base URL/i, %s, 'endpoint_url_missing');
await fillPrivateLabel(dialog, /API 密钥|API Key/i, 'PRISM_WFL_PRIVATE_ENDPOINT', 'endpoint_key_missing');
const responsePromise = responseFor('POST', /^\/api\/endpoints$/);
await clickButton(dialog, /仅保存|Save only/i, 'endpoint_save_missing');
const response = await responsePromise;
if (![200, 201].includes(response.status())) throw new Error('endpoint_create_status');
await expectVisible(page.getByText(%s, {exact: true}), 'endpoint_row_missing');
return {surface: 'endpoint', visible_name: %s, create_status: response.status(), validation: {blocked_post_count: invalidPostCount, visible_error_count: validationErrors}};
""" % tuple(json.dumps(value) for value in (endpoint_name, spec.mock_origin, endpoint_name, endpoint_name)))),
        private_environment={"PRISM_WFL_PRIVATE_ENDPOINT": endpoint_key_index},
    )
    endpoint = find_named(api_list(client, "/api/endpoints"), "name", endpoint_name)
    endpoint_id = safe_id(endpoint.get("id"))
    browser.capture_snapshot("endpoint-created-form", group="form")
    browser.checkpoint("endpoint_created")

    browser.goto("/route/pricing")
    pricing_action = browser.run_code(
        "pricing_create",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('pricing-feature-page').waitFor({state: 'visible', timeout: 15000});
await clickButton(page, /新增模板|Add template/i, 'pricing_add_missing');
const dialog = await expectVisible(page.getByRole('dialog', {name: /新增价格模板|Add pricing template/i}), 'pricing_dialog_missing');
let invalidPostCount = 0;
const invalidRequestListener = (request) => {
  if (request.method() === 'POST' && urlPath(request.url()) === '/api/pricing-templates') invalidPostCount += 1;
};
page.on('request', invalidRequestListener);
await clickButton(dialog, /保存模板|Save template/i, 'pricing_save_missing');
await page.waitForTimeout(250);
page.off('request', invalidRequestListener);
const validationErrors = await dialog.locator('[data-slot="form-message"]:visible').count();
if (invalidPostCount !== 0) throw new Error('pricing_invalid_form_posted');
if (validationErrors < 3) throw new Error('pricing_frontend_validation_missing');
await fillLabel(dialog, /^名称$|^Name$/i, %s, 'pricing_name_missing');
await fillLabel(dialog, /输入价格|Input price/i, '1.25', 'pricing_input_missing');
await fillLabel(dialog, /输出价格|Output price/i, '2.50', 'pricing_output_missing');
await fillLabel(dialog, /缓存输入价格|Cached input price/i, '0.50', 'pricing_cached_missing');
await fillLabel(dialog, /缓存创建价格|Cache creation price/i, '0.75', 'pricing_cache_create_missing');
await fillLabel(dialog, /推理价格|Reasoning price/i, '3.00', 'pricing_reasoning_missing');
const responsePromise = responseFor('POST', /^\/api\/pricing-templates$/);
await clickButton(dialog, /保存模板|Save template/i, 'pricing_save_missing');
const response = await responsePromise;
if (![200, 201].includes(response.status())) throw new Error('pricing_create_status');
await expectVisible(page.getByText(%s, {exact: true}), 'pricing_row_missing');
return {surface: 'pricing', visible_name: %s, create_status: response.status(), validation: {blocked_post_count: invalidPostCount, visible_error_count: validationErrors}};
""" % tuple(json.dumps(value) for value in (pricing_name, pricing_name, pricing_name)))),
    )
    pricing = find_named(api_list(client, "/api/pricing-templates"), "name", pricing_name)
    pricing_id = safe_id(pricing.get("id"))
    browser.capture_snapshot("pricing-created-form", group="form")
    browser.checkpoint("pricing_created")

    browser.goto("/route/ban-policies")
    browser.run_code(
        "ban_policy_create",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('ban-policies-feature-page').waitFor({state: 'visible', timeout: 15000});
await clickButton(page, /新建策略|Add strategy/i, 'ban_add_missing');
const dialog = await expectVisible(page.getByRole('dialog', {name: /新建路由策略|New routing strategy/i}), 'ban_dialog_missing');
await fillLabel(dialog, /^名称$|^Name$/i, %s, 'ban_name_missing');
const responsePromise = responseFor('POST', /^\/api\/loadbalance\/strategies$/);
await clickButton(dialog, /保存策略|Save strategy/i, 'ban_save_missing');
const response = await responsePromise;
if (![200, 201].includes(response.status())) throw new Error('ban_create_status');
await expectVisible(page.getByText(%s, {exact: true}), 'ban_row_missing');
return {surface: 'ban_policy', visible_name: %s, create_status: response.status()};
""" % tuple(json.dumps(value) for value in (ban_name, ban_name, ban_name)))),
    )
    ban = find_named(api_list(client, "/api/loadbalance/strategies"), "name", ban_name)
    ban_id = safe_id(ban.get("id"))
    browser.capture_snapshot("ban-policy-created-form", group="form")
    browser.checkpoint("ban_policy_created")

    browser.goto("/route/models")
    model_action = browser.run_code(
        "model_create",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('models-feature-page').waitFor({state: 'visible', timeout: 15000});
await clickButton(page, /新建模型|New model/i, 'model_add_missing');
const dialog = await expectVisible(page.getByTestId('create-model-dialog'), 'model_dialog_missing');
let invalidPostCount = 0;
const invalidRequestListener = (request) => {
  if (request.method() === 'POST' && urlPath(request.url()) === '/api/models') invalidPostCount += 1;
};
page.on('request', invalidRequestListener);
await clickButton(dialog, /创建并启用|Create and enable/i, 'model_submit_missing');
const validationAlert = await expectVisible(dialog.getByRole('alert'), 'model_frontend_validation_missing');
await page.waitForTimeout(250);
page.off('request', invalidRequestListener);
const validationText = (await validationAlert.innerText()).trim();
if (invalidPostCount !== 0) throw new Error('model_invalid_form_posted');
if (!/模型 ID|Model ID/i.test(validationText)) throw new Error('model_frontend_validation_copy_missing');
await fillLabel(dialog, /模型 ID|Model ID/i, %s, 'model_id_missing');
await fillLabel(dialog, /显示名称|Display name/i, %s, 'model_display_missing');
await chooseLabel(dialog, /路由策略|Routing strategy/i, new RegExp(%s, 'i'), 'model_strategy_missing');
await chooseLabel(dialog, /^端点$|^Endpoint$/i, new RegExp(%s, 'i'), 'model_endpoint_missing');
await fillLabel(dialog, /终端目标名称|Terminal target name/i, %s, 'model_target_name_missing');
const responsePromise = responseFor('POST', /^\/api\/models$/);
await clickButton(dialog, /创建并启用|Create and enable/i, 'model_submit_missing');
const response = await responsePromise;
if (![200, 201].includes(response.status())) throw new Error('model_create_status');
await expectVisible(page.getByText(%s, {exact: true}), 'model_row_missing');
return {surface: 'model', visible_name: %s, create_status: response.status(), validation: {blocked_post_count: invalidPostCount, alert_visible: true, model_id_error_visible: true}};
""" % tuple(json.dumps(value) for value in (model_id, model_name, re.escape(ban_name), re.escape(endpoint_name), terminal_name, model_name, model_name)))),
    )
    model = find_named(api_list(client, "/api/models"), "model_id", model_id)
    model_numeric_id = safe_id(model.get("id"))
    browser.capture_snapshot("model-created-form", group="form")
    browser.checkpoint("model_created")

    browser.goto("/route/models/%d" % model_numeric_id)
    target_action = browser.run_code(
        "targets_configure",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('model-detail-feature-page').waitFor({state: 'visible', timeout: 15000});
const editor = await expectVisible(page.getByTestId('access-targets-editor'), 'targets_editor_missing');
const terminalRow = await expectVisible(editor.getByRole('row').filter({hasText: %s}), 'terminal_row_missing');
await clickButton(terminalRow, new RegExp('编辑.*' + %s, 'i'), 'terminal_edit_missing');
const targetDialog = await expectVisible(page.getByRole('dialog', {name: /编辑终端目标|Edit terminal target/i}), 'terminal_dialog_missing');
await chooseLabel(targetDialog, /价格模板|Pricing template/i, new RegExp(%s, 'i'), 'terminal_pricing_missing');
let responsePromise = responseFor('PATCH', new RegExp('^/api/models/[0-9]+/connections/[0-9]+$'));
await clickButton(targetDialog, /保存终端目标|Save terminal target/i, 'terminal_save_missing');
let response = await responsePromise;
if (response.status() !== 200) throw new Error('terminal_update_status');
await chooseLabel(editor, /选择目标模型|Select target model/i, new RegExp(%s, 'i'), 'model_target_option_missing');
responsePromise = responseFor('POST', new RegExp('^/api/models/[0-9]+/targets$'));
await clickButton(editor, /添加目标|Add target/i, 'model_target_add_missing');
response = await responsePromise;
if (![200, 201].includes(response.status())) throw new Error('model_target_create_status');
const rows = editor.locator('tbody tr');
if (await rows.count() !== 2) throw new Error('mixed_target_count');
await rows.nth(1).dragTo(rows.nth(0));
const orderResponses = [];
const orderResponseListener = (candidate) => {
  const path = urlPath(candidate.url());
  if (candidate.request().method() === 'PATCH' && /^\/api\/models\/[0-9]+\/targets\/[0-9]+\/position$/.test(path)) {
    orderResponses.push({path, status: candidate.status()});
  }
};
page.on('response', orderResponseListener);
const saveOrder = await expectVisible(editor.getByRole('button', {name: /保存顺序|Save order/i}), 'target_order_save_missing');
await saveOrder.click();
await saveOrder.waitFor({state: 'detached', timeout: 30000}).catch(() => { throw new Error('target_order_save_not_settled'); });
page.off('response', orderResponseListener);
if (orderResponses.length < 1 || orderResponses.some((item) => item.status !== 200)) throw new Error('target_order_save_request_missing');
const persistedRows = editor.locator('tbody tr');
const rowIds = await persistedRows.evaluateAll((items) => items.map((item) => item.getAttribute('data-testid')));
const rowTexts = await persistedRows.evaluateAll((items) => items.map((item) => (item.textContent || '').replace(/\s+/g, ' ').trim()));
const positions = await persistedRows.evaluateAll((items) => items.map((item) => (item.querySelector('td')?.textContent || '').trim()));
return {surface: 'targets', target_count: 2, pricing_visible: await editor.getByText(%s, {exact: false}).count() > 0, order_responses: orderResponses, row_ids: rowIds, row_texts: rowTexts, positions};
""" % tuple(json.dumps(value) for value in (terminal_name, re.escape(terminal_name), re.escape(pricing_name), re.escape(target_model_id), pricing_name)))),
        timeout=180,
    )
    refreshed_model = api_object(client, "/api/models/%d" % model_numeric_id)
    api_targets = wfl_003_target_projection(refreshed_model.get("access_targets"))
    database_targets = wfl_003_database_targets(database, model_numeric_id)
    model_targets = [item for item in api_targets if item["target_type"] == "model"]
    terminal_targets = [item for item in api_targets if item["target_type"] == "connection"]
    if len(model_targets) != 1 or len(terminal_targets) != 1:
        raise CaseError("wfl_003_targets_not_persisted", assertion_failure=True)
    imported_target_model = find_named(api_list(client, "/api/models"), "model_id", target_model_id)
    imported_target_display_name = imported_target_model.get("display_name")
    if imported_target_display_name is not None and not isinstance(imported_target_display_name, str):
        raise CaseError("wfl_003_target_model_label_invalid", assertion_failure=True)
    target_model_label = (
        imported_target_display_name.strip()
        if isinstance(imported_target_display_name, str) and imported_target_display_name.strip()
        else target_model_id
    )
    expected_targets = [
        {
            "id": model_targets[0]["id"],
            "target_type": "model",
            "target_model_id": target_model_id,
            "target_model_label": target_model_label,
            "connection_id": None,
            "position": 0,
            "is_enabled": True,
            "connection_name": None,
            "pricing_template_id": None,
        },
        {
            "id": terminal_targets[0]["id"],
            "target_type": "connection",
            "target_model_id": None,
            "target_model_label": None,
            "connection_id": terminal_targets[0]["connection_id"],
            "position": 1,
            "is_enabled": True,
            "connection_name": terminal_name,
            "pricing_template_id": pricing_id,
        },
    ]
    expected_row_ids = ["access-target-%d" % item["id"] for item in expected_targets]
    if (
        api_targets != expected_targets
        or database_targets != expected_targets
        or target_action.get("row_ids") != expected_row_ids
        or target_action.get("positions") != ["1", "2"]
        or target_action.get("pricing_visible") is not True
    ):
        raise CaseError("wfl_003_target_order_persistence_mismatch", assertion_failure=True)
    browser.capture_snapshot("targets-saved-form", group="form")
    browser.checkpoint("targets_saved")

    browser._require_cli().run("reload", timeout=90)
    reloaded_ui = browser.run_code(
        "targets_reload_verify",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('model-detail-feature-page').waitFor({state: 'visible', timeout: 15000});
const editor = await expectVisible(page.getByTestId('access-targets-editor'), 'targets_editor_missing_after_reload');
const expectedIds = %s;
const expectedNames = %s;
const rows = editor.locator('tbody tr');
if (await rows.count() !== expectedIds.length) throw new Error('target_reload_count_mismatch');
const rowIds = await rows.evaluateAll((items) => items.map((item) => item.getAttribute('data-testid')));
if (JSON.stringify(rowIds) !== JSON.stringify(expectedIds)) throw new Error('target_reload_order_mismatch');
const rowTexts = await rows.evaluateAll((items) => items.map((item) => (item.textContent || '').replace(/\s+/g, ' ').trim()));
const positions = await rows.evaluateAll((items) => items.map((item) => (item.querySelector('td')?.textContent || '').trim()));
if (positions[0] !== '1' || positions[1] !== '2') throw new Error('target_reload_position_mismatch');
if (!rowTexts[0].includes(expectedNames[0]) || !rowTexts[1].includes(expectedNames[1]) || !rowTexts[1].includes(expectedNames[2])) {
  throw new Error('target_reload_relation_mismatch');
}
return {surface: 'targets', row_ids: rowIds, row_texts: rowTexts, positions, pricing_relation_visible: true};
""" % (json.dumps(expected_row_ids), json.dumps([target_model_label, terminal_name, pricing_name])))),
        timeout=90,
    )
    reloaded_model = api_object(client, "/api/models/%d" % model_numeric_id)
    reloaded_api_targets = wfl_003_target_projection(reloaded_model.get("access_targets"))
    reloaded_database_targets = wfl_003_database_targets(database, model_numeric_id)
    if reloaded_api_targets != expected_targets or reloaded_database_targets != expected_targets:
        raise CaseError("wfl_003_target_reload_persistence_mismatch", assertion_failure=True)
    detail_snapshot = browser.capture_snapshot("targets-refreshed-detail", group="form")
    detail_text = (browser.case_dir / detail_snapshot["path"]).read_text(encoding="utf-8")
    if terminal_name not in detail_text or target_model_label not in detail_text:
        raise CaseError("wfl_003_detail_refresh_mismatch", assertion_failure=True)
    target_persistence = {
        "expected": expected_targets,
        "ui_after_save": target_action,
        "api_after_save": api_targets,
        "database_after_save": database_targets,
        "ui_after_reload": reloaded_ui,
        "api_after_reload": reloaded_api_targets,
        "database_after_reload": reloaded_database_targets,
        "exact_match": True,
    }
    browser.checkpoint("refresh_and_detail_verified")

    browser.goto("/route/endpoints")
    dependency = browser.run_code(
        "endpoint_dependency_guard",
        action_for_origin(origin, ui_body("""
await page.getByTestId('endpoints-feature-page').waitFor({state: 'visible', timeout: 15000});
const row = await expectVisible(page.getByTestId(%s), 'endpoint_dependency_row_missing');
const deleteButton = row.getByRole('button', {name: /确定要删除|Delete endpoint/i});
await deleteButton.click();
await expectVisible(page.getByTestId('delete-blocked-heading'), 'endpoint_dependency_guard_missing');
const blockers = await expectVisible(page.getByTestId('delete-blockers'), 'endpoint_blockers_missing');
const blockerText = await blockers.innerText();
if (!blockerText.includes(%s)) throw new Error('endpoint_blocker_target_missing');
await clickButton(page.getByRole('dialog'), /取消|Cancel|关闭|Close/i, 'endpoint_dependency_close_missing');
return {surface: 'endpoint_dependency', blocked: true, referenced_target_visible: true};
""" % (json.dumps("endpoint-row-%d" % endpoint_id), json.dumps(terminal_name)))),
    )
    if dependency.get("blocked") is not True:
        raise CaseError("wfl_003_dependency_guard_failed", assertion_failure=True)
    browser.checkpoint("dependency_protection_verified")

    # Destructive UI cleanup is deliberately one ordered action so a partial
    # failure leaves the disposable clone for diagnosis instead of silently
    # falling through to a direct database cleanup.
    browser.goto("/route/models/%d" % model_numeric_id)
    browser.run_code(
        "reverse_delete",
        action_for_origin(origin, ui_body("""
const editor = await expectVisible(page.getByTestId('access-targets-editor'), 'cleanup_editor_missing');
const removeTargetRow = async (testId) => {
  const row = await expectVisible(editor.getByTestId(testId), 'cleanup_target_row_missing');
  const more = row.getByRole('button', {name: /更多操作|More actions/i});
  await more.click();
  const remove = await expectVisible(page.getByRole('menuitem', {name: /删除目标|移除目标|Remove target/i}), 'cleanup_target_remove_missing');
  const responsePromise = responseFor('DELETE', new RegExp('^/api/models/[0-9]+/targets/[0-9]+$'));
  await remove.click();
  const response = await responsePromise;
  if (response.status() !== 200) throw new Error('cleanup_target_delete_status');
};
await removeTargetRow(%s);
await removeTargetRow(%s);
await page.goto(expectedOrigin + '/route/models');
await page.getByTestId('models-feature-page').waitFor({state: 'visible', timeout: 15000});
const modelRow = await expectVisible(page.getByTestId(%s), 'cleanup_model_row_missing');
await modelRow.getByRole('button', {name: /查看模型详情|View model details/i}).click();
await expectVisible(page.getByRole('menuitem', {name: /^删除$|^Delete$/i}), 'cleanup_model_menu_delete_missing').then((item) => item.click());
let responsePromise = responseFor('DELETE', new RegExp('^/api/models/[0-9]+$'));
await clickButton(page.getByRole('dialog'), /^删除$|^Delete$/i, 'cleanup_model_confirm_missing');
let response = await responsePromise;
if (![200, 204].includes(response.status())) throw new Error('cleanup_model_delete_status');
await page.goto(expectedOrigin + '/route/pricing');
await page.getByTestId('pricing-feature-page').waitFor({state: 'visible', timeout: 15000});
const pricingRow = await expectVisible(page.getByTestId(%s), 'cleanup_pricing_row_missing');
await pricingRow.getByRole('button', {name: /^操作$|^Actions$/i}).click();
await expectVisible(page.getByRole('menuitem', {name: /^删除$|^Delete$/i}), 'cleanup_pricing_menu_delete_missing').then((item) => item.click());
responsePromise = responseFor('DELETE', new RegExp('^/api/pricing-templates/[0-9]+$'));
await clickButton(page.getByRole('dialog'), /^删除$|^Delete$/i, 'cleanup_pricing_confirm_missing');
response = await responsePromise;
if (![200, 204].includes(response.status())) throw new Error('cleanup_pricing_delete_status');
await page.goto(expectedOrigin + '/route/ban-policies');
await page.getByTestId('ban-policies-feature-page').waitFor({state: 'visible', timeout: 15000});
const banRow = await expectVisible(page.getByTestId(%s), 'cleanup_ban_row_missing');
await clickButton(banRow, /^删除$|^Delete$/i, 'cleanup_ban_delete_missing');
responsePromise = responseFor('DELETE', new RegExp('^/api/loadbalance/strategies/[0-9]+$'));
await clickButton(page.getByRole('dialog'), /^删除$|^Delete$/i, 'cleanup_ban_confirm_missing');
response = await responsePromise;
if (![200, 204].includes(response.status())) throw new Error('cleanup_ban_delete_status');
await page.goto(expectedOrigin + '/route/endpoints');
await page.getByTestId('endpoints-feature-page').waitFor({state: 'visible', timeout: 15000});
const endpointRow = await expectVisible(page.getByTestId(%s), 'cleanup_endpoint_row_missing');
await endpointRow.getByRole('button', {name: /确定要删除|Delete endpoint/i}).click();
await expectVisible(page.getByTestId('delete-endpoint-confirm'), 'cleanup_endpoint_confirm_missing');
responsePromise = responseFor('DELETE', new RegExp('^/api/endpoints/[0-9]+$'));
await page.getByTestId('delete-endpoint-confirm').click();
response = await responsePromise;
if (![200, 204].includes(response.status())) throw new Error('cleanup_endpoint_delete_status');
return {surface: 'reverse_delete', deleted: ['model_targets', 'model', 'pricing', 'ban_policy', 'endpoint']};
""" % tuple(json.dumps(value) for value in (
            expected_row_ids[0],
            expected_row_ids[1],
            "models-table-row-%d" % model_numeric_id,
            "pricing-template-row-%d" % pricing_id,
            "strategy-row-%d" % ban_id,
            "endpoint-row-%d" % endpoint_id,
        )))),
        timeout=240,
    )
    browser.checkpoint("reverse_delete_completed")

    cleanup = database.read_json(
        """
SELECT json_build_object(
  'endpoint_rows', (SELECT count(*) FROM endpoints WHERE name = %s),
  'pricing_rows', (SELECT count(*) FROM pricing_templates WHERE name = %s),
  'ban_rows', (SELECT count(*) FROM loadbalance_strategies WHERE name = %s),
  'model_rows', (SELECT count(*) FROM model_configs WHERE model_id = %s),
  'connection_rows', (SELECT count(*) FROM connections WHERE name = %s)
);
""" % tuple(support.sql_literal(value) for value in (endpoint_name, pricing_name, ban_name, model_id, terminal_name))
    )
    if any(cleanup.get(field) != 0 for field in ("endpoint_rows", "pricing_rows", "ban_rows", "model_rows", "connection_rows")):
        raise CaseError("wfl_003_cleanup_residue", assertion_failure=True)
    browser.write_json(
        "form-snapshots.json",
        {
            "snapshots": browser.forms,
            "frontend_validation": {
                "endpoint": endpoint_action.get("validation"),
                "pricing": pricing_action.get("validation"),
                "model": model_action.get("validation"),
                "all_invalid_submissions_blocked_before_post": True,
            },
            "target_persistence": target_persistence,
            "passed": True,
        },
    )
    browser.write_json("network-transcript.redacted.json", {"events": browser.network, "passed": True})
    browser.write_json(
        "created-resource-ids.json",
        {
            "resources": {
                "endpoint": endpoint_id,
                "pricing": pricing_id,
                "ban_policy": ban_id,
                "model": model_numeric_id,
                "target_count": 2,
                "target_row_ids_in_runtime_order": [item["id"] for item in expected_targets],
                "terminal_connection_id": expected_targets[1]["connection_id"],
            },
            "target_persistence": target_persistence,
            "passed": True,
        },
    )
    browser.write_json(
        "cleanup-proof.json",
        {"dependency_guard": dependency, "residual_counts": cleanup, "reverse_order": ["targets", "model", "pricing", "ban_policy", "endpoint"], "passed": True},
    )


def wait_request_projection(
    database: support.LocalPostgres,
    caller_id: str,
    timeout: float = 20.0,
    *,
    minimum_rows: int = 1,
) -> dict[str, Any]:
    if not SAFE_IDENTIFIER_RE.fullmatch(caller_id):
        raise CaseError("workflow_caller_id_invalid")
    caller = support.sql_literal(caller_id)

    def read() -> Optional[dict[str, Any]]:
        value = database.read_json(
            """
SELECT json_build_object(
  'rows', count(*),
  'request_log_id', min(rl.id),
  'request_log_ids', coalesce(json_agg(rl.id ORDER BY rl.attempt_number NULLS LAST, rl.id), '[]'::json),
  'ingress_request_id', min(rl.ingress_request_id::text),
  'model_ids', coalesce(json_agg(rl.model_id ORDER BY rl.attempt_number NULLS LAST, rl.id), '[]'::json),
  'row_kinds', coalesce(json_agg(rl.row_kind ORDER BY rl.attempt_number NULLS LAST, rl.id), '[]'::json),
  'status_codes', coalesce(json_agg(
      CASE rl.row_kind
        WHEN 'upstream' THEN rl.upstream_status_code
        WHEN 'planning' THEN rl.gateway_status_code
        WHEN 'admission' THEN rl.gateway_status_code
        ELSE rl.legacy_status_code
      END
      ORDER BY rl.attempt_number NULLS LAST, rl.id
    ) FILTER (WHERE COALESCE(rl.upstream_status_code, rl.gateway_status_code, rl.legacy_status_code) IS NOT NULL), '[]'::json),
  'attempt_numbers', coalesce(json_agg(rl.attempt_number ORDER BY rl.attempt_number, rl.id) FILTER (WHERE rl.attempt_number IS NOT NULL), '[]'::json),
  'attempt_results', coalesce(json_agg(rl.attempt_result ORDER BY rl.attempt_number, rl.id) FILTER (WHERE rl.attempt_result IS NOT NULL), '[]'::json),
  'winner_flags', coalesce(json_agg(rl.is_winner ORDER BY rl.attempt_number, rl.id) FILTER (WHERE rl.is_winner IS NOT NULL), '[]'::json),
  'terminal_target_ids', coalesce(json_agg(rl.selected_terminal_target_id ORDER BY rl.attempt_number, rl.id) FILTER (WHERE rl.selected_terminal_target_id IS NOT NULL), '[]'::json),
  'terminal_target_labels', coalesce(json_agg(terminal.name ORDER BY rl.attempt_number, rl.id) FILTER (WHERE terminal.name IS NOT NULL), '[]'::json),
  'endpoint_labels', coalesce(json_agg(ep.name ORDER BY rl.attempt_number, rl.id) FILTER (WHERE ep.name IS NOT NULL), '[]'::json),
  'attribution_states', coalesce(json_agg(rl.proxy_api_key_attribution_state ORDER BY rl.attempt_number NULLS LAST, rl.id) FILTER (WHERE rl.proxy_api_key_attribution_state IS NOT NULL), '[]'::json),
  'proxy_key_ids', coalesce(json_agg(rl.proxy_api_key_id_snapshot ORDER BY rl.attempt_number NULLS LAST, rl.id) FILTER (WHERE rl.proxy_api_key_id_snapshot IS NOT NULL), '[]'::json),
  'proxy_key_names', coalesce(json_agg(rl.proxy_api_key_name_snapshot ORDER BY rl.attempt_number NULLS LAST, rl.id) FILTER (WHERE rl.proxy_api_key_name_snapshot IS NOT NULL), '[]'::json)
) FROM request_logs rl
LEFT JOIN endpoints ep ON ep.id = rl.endpoint_id
LEFT JOIN connections terminal ON terminal.id = rl.selected_terminal_target_id
WHERE rl.caller_request_id = %s;
""" % caller
        )
        rows = value.get("rows")
        if isinstance(rows, bool) or not isinstance(rows, int) or rows < 0:
            raise CaseError("workflow_request_projection_shape_invalid", assertion_failure=True)
        return value if rows >= minimum_rows else None

    result = support.wait_until(read, timeout, interval=0.2)
    if not isinstance(result, dict):
        raise CaseError("workflow_request_projection_timeout", assertion_failure=True)
    return result


def wait_database_object(
    database: support.LocalPostgres,
    query: str,
    predicate: Callable[[Mapping[str, Any]], bool],
    *,
    timeout: float = 20.0,
) -> dict[str, Any]:
    def read() -> Optional[dict[str, Any]]:
        value = database.read_json(query)
        if not isinstance(value, dict):
            raise CaseError("workflow_database_projection_shape_invalid", assertion_failure=True)
        try:
            return value if predicate(value) else None
        except (KeyError, TypeError, ValueError) as exc:
            raise CaseError(
                "workflow_database_projection_shape_invalid",
                assertion_failure=True,
            ) from exc

    value = support.wait_until(read, timeout, interval=0.2)
    if not isinstance(value, dict):
        raise CaseError("workflow_database_projection_timeout", assertion_failure=True)
    return value


def wait_finalized_request(
    database: support.LocalPostgres,
    ingress_request_id: str,
    *,
    expected_status: int,
    expected_attempt_count: int,
    expected_terminal_target_id: int,
    expected_endpoint_label: str,
    expected_failover: bool,
    timeout: float = 20.0,
) -> dict[str, Any]:
    if not re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", ingress_request_id):
        raise CaseError("workflow_ingress_request_id_invalid")
    ingress = support.sql_literal(ingress_request_id)
    query = """
SELECT json_build_object(
  'rows', count(*),
  'status_codes', coalesce(json_agg(status_code ORDER BY id), '[]'::json),
  'attempt_counts', coalesce(json_agg(attempt_count ORDER BY id), '[]'::json),
  'expected_request_log_row_counts', coalesce(json_agg(expected_request_log_row_count ORDER BY id), '[]'::json),
  'final_attempt_numbers', coalesce(json_agg(final_attempt_number ORDER BY id), '[]'::json),
  'terminal_target_ids', coalesce(json_agg(selected_terminal_target_id ORDER BY id), '[]'::json),
  'endpoint_labels', coalesce(json_agg(endpoint_label_snapshot ORDER BY id), '[]'::json),
  'failover_flags', coalesce(json_agg(failover_occurred ORDER BY id), '[]'::json),
  'success_flags', coalesce(json_agg(success_flag ORDER BY id), '[]'::json),
  'routing_evidence_complete', coalesce(bool_and(routing_evidence_complete), false)
)
FROM usage_request_events
WHERE ingress_request_id = %s;
""" % ingress
    return wait_database_object(
        database,
        query,
        lambda item: (
            item.get("rows") == 1
            and item.get("status_codes") == [expected_status]
            and item.get("attempt_counts") == [expected_attempt_count]
            and item.get("expected_request_log_row_counts") == [expected_attempt_count]
            and item.get("final_attempt_numbers") == [expected_attempt_count]
            and item.get("terminal_target_ids") == [expected_terminal_target_id]
            and item.get("endpoint_labels") == [expected_endpoint_label]
            and item.get("failover_flags") == [expected_failover]
            and item.get("success_flags") == [True]
            and item.get("routing_evidence_complete") is True
        ),
        timeout=timeout,
    )


def inspect_request_chain_ui(
    browser: BrowserCase,
    *,
    step: str,
    projection: Mapping[str, Any],
    expected_status_codes: Sequence[int],
    expected_winner_flags: Sequence[bool],
    expected_final_target: str,
    expected_endpoint: str,
    open_request_log_id: Optional[int] = None,
    detail_expected_texts: Sequence[str] = (),
) -> dict[str, Any]:
    """Assert the server-owned ingress chain through shipped test IDs."""
    ingress_request_id = projection.get("ingress_request_id")
    request_log_ids = projection.get("request_log_ids")
    if (
        not isinstance(ingress_request_id, str)
        or not re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", ingress_request_id)
        or not isinstance(request_log_ids, list)
        or len(request_log_ids) != len(expected_status_codes)
        or len(request_log_ids) != len(expected_winner_flags)
        or not request_log_ids
    ):
        raise CaseError("workflow_request_chain_identity_invalid", assertion_failure=True)
    if any(isinstance(value, bool) or not isinstance(value, int) or value <= 0 for value in request_log_ids):
        raise CaseError("workflow_request_chain_identity_invalid", assertion_failure=True)
    normalized_ids = [int(value) for value in request_log_ids]
    if open_request_log_id is not None and open_request_log_id not in normalized_ids:
        raise CaseError("workflow_request_chain_detail_identity_invalid", assertion_failure=True)
    if not expected_final_target or not expected_endpoint or any(not value for value in detail_expected_texts):
        raise CaseError("workflow_request_chain_expectation_invalid")
    browser.goto(
        "/observe/requests?view=ingress_chains&time_range=all&ingress_request_id="
        + ingress_request_id
    )
    return browser.run_code(
        step,
        action_for_origin(browser.spec.frontend_origin, ui_body(r"""
await page.getByTestId('ingress-chains-table').waitFor({state: 'visible', timeout: 20000});
const ingressId = %s;
const expectedIds = %s;
const expectedStatuses = %s;
const expectedWinners = %s;
const expectedAttempts = expectedIds.map((_, index) => index + 1);
const summary = await expectVisible(page.getByTestId('chain-summary-' + ingressId), 'request_chain_summary_missing');
const summaryCells = summary.getByRole('cell');
if (await summaryCells.count() < 7) throw new Error('request_chain_summary_shape_invalid');
const summaryStatus = (await summaryCells.nth(2).innerText()).replace(/\s+/g, ' ').trim();
const summaryTarget = (await summaryCells.nth(4).innerText()).replace(/\s+/g, ' ').trim();
const summaryEndpoint = (await summaryCells.nth(5).innerText()).replace(/\s+/g, ' ').trim();
const summaryAttempts = (await summaryCells.nth(6).innerText()).trim();
if (!(await summaryCells.nth(2).getByText(String(expectedStatuses.at(-1)), {exact: true}).count())) throw new Error('request_chain_final_status_mismatch');
if (summaryTarget !== %s) throw new Error('request_chain_final_target_mismatch');
if (summaryEndpoint !== %s) throw new Error('request_chain_endpoint_mismatch');
if (summaryAttempts !== String(expectedIds.length)) throw new Error('request_chain_attempt_count_mismatch');
const toggle = summary.locator('button').first();
if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
const chain = await expectVisible(page.getByTestId('chain-' + ingressId), 'request_chain_rows_missing');
const actualRowCount = await chain.locator('[data-testid^="chain-row-"]').count();
if (actualRowCount !== expectedIds.length) throw new Error('request_chain_row_count_mismatch');
const rows = [];
for (let index = 0; index < expectedIds.length; index += 1) {
  const id = String(expectedIds[index]);
  const row = await expectVisible(chain.getByTestId('chain-row-' + id), 'request_chain_row_missing');
  const text = (await row.innerText()).replace(/\s+/g, ' ').trim();
  if (!(await row.getByText(String(expectedStatuses[index]), {exact: true}).count())) throw new Error('request_chain_row_status_mismatch');
  if (!(await row.getByText(new RegExp('^(?:尝试|Attempt)\\s*' + expectedAttempts[index] + '$', 'i')).count())) throw new Error('request_chain_row_attempt_mismatch');
  if (!(await row.getByText(/^(?:上游尝试|Upstream)$/i).count())) throw new Error('request_chain_row_kind_mismatch');
  const winnerVisible = (await row.getByText(/^(?:胜出|Winner)$/i).count()) > 0;
  if (winnerVisible !== expectedWinners[index]) throw new Error('request_chain_row_winner_mismatch');
  rows.push({request_log_id: expectedIds[index], status_code: expectedStatuses[index], attempt_number: expectedAttempts[index], winner_visible: winnerVisible, row_kind: 'upstream', text});
}
let detailOpened = false;
const openId = %s;
const detailTexts = %s;
if (openId !== null) {
  await chain.getByTestId('chain-row-' + String(openId)).click();
  const detail = await expectVisible(page.getByTestId('request-log-detail-sheet'), 'request_chain_detail_missing');
  await expectVisible(detail.getByTestId('request-log-summary-strip'), 'request_chain_detail_summary_missing');
  await expectVisible(detail.getByTestId('request-log-overview-grid'), 'request_chain_detail_overview_missing');
  const detailText = await detail.innerText();
  for (const expectedText of detailTexts) {
    if (!detailText.includes(expectedText)) throw new Error('request_chain_detail_attribution_mismatch');
  }
  detailOpened = true;
}
return {surface: 'request_chain', ingress_request_id: ingressId, summary: {final_status: expectedStatuses.at(-1), status_text: summaryStatus, final_target: summaryTarget, endpoint: summaryEndpoint, attempt_count: Number(summaryAttempts)}, rows, exact_row_count: true, detail_opened: detailOpened};
""" % (
            json.dumps(ingress_request_id),
            json.dumps(normalized_ids),
            json.dumps(list(expected_status_codes)),
            json.dumps(list(expected_winner_flags)),
            json.dumps(expected_final_target),
            json.dumps(expected_endpoint),
            json.dumps(open_request_log_id),
            json.dumps(list(detail_expected_texts)),
        ))),
        timeout=120,
    )


def wait_current_state(
    client: support.LocalHTTP,
    terminal_target_id: int,
    expected_state: str,
    *,
    timeout: float = 20.0,
) -> dict[str, Any]:
    def read() -> Optional[dict[str, Any]]:
        response = api_object(
            client,
            "/api/loadbalance/current-state?terminal_target_id=%d&limit=50" % terminal_target_id,
        )
        items = response.get("items")
        if not isinstance(items, list):
            raise CaseError("workflow_current_state_shape_invalid", assertion_failure=True)
        for item in items:
            if not isinstance(item, dict):
                raise CaseError("workflow_current_state_shape_invalid", assertion_failure=True)
            terminal = item.get("terminal_target")
            if isinstance(terminal, dict) and terminal.get("id") == terminal_target_id and item.get("state") == expected_state:
                return item
        return None

    value = support.wait_until(read, timeout, interval=0.2)
    if not isinstance(value, dict):
        raise CaseError("workflow_current_state_timeout", assertion_failure=True)
    return value


def safe_mock_ledger(mock: support.LocalHTTP, caller_id: str) -> list[dict[str, Any]]:
    if not SAFE_IDENTIFIER_RE.fullmatch(caller_id):
        raise CaseError("workflow_mock_request_id_invalid")
    _, value = mock.json(
        "GET",
        "/__mock__/ledger?after=0&limit=100&request_id=" + caller_id,
        expected=(200,),
    )
    rows = value.get("data") if isinstance(value, dict) else None
    if not isinstance(rows, list):
        raise CaseError("workflow_mock_ledger_shape_invalid")
    result: list[dict[str, Any]] = []
    for row in rows:
        if not isinstance(row, dict):
            raise CaseError("workflow_mock_ledger_shape_invalid")
        result.append(
            {
                "sequence": row.get("sequence"),
                "request_id": row.get("request_id"),
                "operation": row.get("operation"),
                "planned_status": row.get("planned_status"),
                "outcome": row.get("outcome"),
                "body_sha256": row.get("body_sha256"),
            }
        )
    workflow.assert_safe_json(result, "mock_ledger")
    return result


def wait_audit_projection(
    database: support.LocalPostgres,
    caller_id: str,
    *,
    expected_audit_rows: int,
    timeout: float = 20.0,
) -> dict[str, Any]:
    if not SAFE_IDENTIFIER_RE.fullmatch(caller_id):
        raise CaseError("workflow_caller_id_invalid")
    caller = support.sql_literal(caller_id)
    query = """
WITH selected_requests AS (
  SELECT id, created_at, audit_enabled_at_request, audit_capture_bodies_at_request
  FROM request_logs
  WHERE caller_request_id = %s
), selected_audits AS (
  SELECT a.*
  FROM audit_logs a
  JOIN selected_requests r
    ON r.id = a.request_log_id
   AND r.created_at = a.request_log_created_at
)
SELECT json_build_object(
  'request_rows', (SELECT count(*) FROM selected_requests),
  'audit_rows', (SELECT count(*) FROM selected_audits),
  'request_log_id', (SELECT min(id) FROM selected_requests),
  'enabled_flags', coalesce((SELECT json_agg(audit_enabled_at_request ORDER BY id) FROM selected_requests), '[]'::json),
  'capture_flags', coalesce((SELECT json_agg(audit_capture_bodies_at_request ORDER BY id) FROM selected_requests), '[]'::json),
  'audit_ids', coalesce((SELECT json_agg(id ORDER BY id) FROM selected_audits), '[]'::json),
  'ingress_payload_stored_flags', coalesce((SELECT json_agg(request_body_stored ORDER BY id) FROM selected_audits), '[]'::json),
  'result_payload_stored_flags', coalesce((SELECT json_agg(response_body_stored ORDER BY id) FROM selected_audits), '[]'::json),
  'ingress_capture_states', coalesce((SELECT json_agg(request_body_capture_status ORDER BY id) FROM selected_audits), '[]'::json),
  'result_capture_states', coalesce((SELECT json_agg(response_body_capture_status ORDER BY id) FROM selected_audits), '[]'::json),
  'ingress_stored_bytes', coalesce((SELECT json_agg(coalesce(request_body_bytes_stored, 0) ORDER BY id) FROM selected_audits), '[]'::json),
  'result_stored_bytes', coalesce((SELECT json_agg(coalesce(response_body_bytes_stored, 0) ORDER BY id) FROM selected_audits), '[]'::json),
  'scrub_sentinel_present', coalesce((SELECT bool_and(request_headers::text LIKE '%%[REDACTED]%%') FROM selected_audits), false)
);
""" % caller

    return wait_database_object(
        database,
        query,
        lambda item: int(item.get("request_rows", 0)) >= 1 and int(item.get("audit_rows", -1)) == expected_audit_rows,
        timeout=timeout,
    )


def audit_marker_absence_projection(
    database: support.LocalPostgres,
    private_markers: Sequence[str],
) -> dict[str, Any]:
    """Prove synthetic private values are absent from runtime persistence.

    The markers are used only inside the read-only database predicate.  The
    retained projection contains aggregate counts and stable surface names;
    it never contains a marker or its encoded representation.
    """
    if len(private_markers) != 2 or any(not isinstance(value, str) or not value for value in private_markers):
        raise CaseError("wfl_006_database_marker_input_invalid")
    encoded_literals = ", ".join(
        "(decode(%s, 'hex'))" % support.sql_literal(value.encode("utf-8").hex())
        for value in private_markers
    )
    projection = database.read_json(
        """
WITH markers(marker) AS (
  VALUES %s
), surfaces(surface) AS (
  VALUES
    ('request_logs.row_json'),
    ('audit_logs.row_json'),
    ('audit_logs.request_body'),
    ('audit_logs.response_body'),
    ('usage_request_events.row_json'),
    ('runtime_telemetry_outbox.row_json'),
    ('loadbalance_events.row_json')
), haystacks(surface, payload) AS (
  SELECT 'request_logs.row_json', convert_to(to_jsonb(row_value)::text, 'UTF8')
  FROM request_logs AS row_value
  UNION ALL
  SELECT 'audit_logs.row_json', convert_to(
    (to_jsonb(row_value) - 'request_body' - 'response_body')::text,
    'UTF8'
  )
  FROM audit_logs AS row_value
  UNION ALL
  SELECT 'audit_logs.request_body', request_body
  FROM audit_logs
  WHERE request_body IS NOT NULL
  UNION ALL
  SELECT 'audit_logs.response_body', response_body
  FROM audit_logs
  WHERE response_body IS NOT NULL
  UNION ALL
  SELECT 'usage_request_events.row_json', convert_to(to_jsonb(row_value)::text, 'UTF8')
  FROM usage_request_events AS row_value
  UNION ALL
  SELECT 'runtime_telemetry_outbox.row_json', convert_to(to_jsonb(row_value)::text, 'UTF8')
  FROM runtime_telemetry_outbox AS row_value
  UNION ALL
  SELECT 'loadbalance_events.row_json', convert_to(to_jsonb(row_value)::text, 'UTF8')
  FROM loadbalance_events AS row_value
), surface_counts AS (
  SELECT
    surfaces.surface,
    count(haystacks.payload) AS scanned_values,
    count(haystacks.payload) FILTER (
      WHERE EXISTS (
        SELECT 1 FROM markers
        WHERE position(markers.marker IN haystacks.payload) > 0
      )
    ) AS matches
  FROM surfaces
  LEFT JOIN haystacks USING (surface)
  GROUP BY surfaces.surface
)
SELECT json_build_object(
  'scope', 'workflow_clone_runtime_persistence',
  'marker_value_count', (SELECT count(*) FROM markers),
  'table_row_counts', json_build_object(
    'request_logs', (SELECT count(*) FROM request_logs),
    'audit_logs', (SELECT count(*) FROM audit_logs),
    'usage_request_events', (SELECT count(*) FROM usage_request_events),
    'runtime_telemetry_outbox', (SELECT count(*) FROM runtime_telemetry_outbox),
    'loadbalance_events', (SELECT count(*) FROM loadbalance_events)
  ),
  'scanned_values', (SELECT coalesce(sum(scanned_values), 0) FROM surface_counts),
  'surface_matches', (
    SELECT json_object_agg(surface, matches ORDER BY surface)
    FROM surface_counts
  ),
  'total_matches', (SELECT coalesce(sum(matches), 0) FROM surface_counts)
);
""" % encoded_literals
    )
    surface_matches = projection.get("surface_matches")
    if (
        projection.get("scope") != "workflow_clone_runtime_persistence"
        or projection.get("marker_value_count") != 2
        or not isinstance(projection.get("table_row_counts"), dict)
        or isinstance(projection.get("scanned_values"), bool)
        or not isinstance(projection.get("scanned_values"), int)
        or projection["scanned_values"] < 1
        or not isinstance(surface_matches, dict)
        or set(surface_matches) != {
            "request_logs.row_json",
            "audit_logs.row_json",
            "audit_logs.request_body",
            "audit_logs.response_body",
            "usage_request_events.row_json",
            "runtime_telemetry_outbox.row_json",
            "loadbalance_events.row_json",
        }
        or any(isinstance(value, bool) or not isinstance(value, int) or value != 0 for value in surface_matches.values())
        or projection.get("total_matches") != 0
    ):
        raise CaseError("wfl_006_database_marker_absence_failed", assertion_failure=True)
    workflow.assert_safe_json(projection, "wfl_006_database_marker_absence")
    return projection


def original_openai_audit_mode(settings: Mapping[str, Any]) -> str:
    policies = settings.get("policies")
    if not isinstance(policies, list):
        raise CaseError("workflow_audit_settings_shape_invalid", assertion_failure=True)
    matches = [item for item in policies if isinstance(item, dict) and item.get("family") == "openai"]
    if len(matches) != 1 or matches[0].get("mode") not in {"disabled", "metadata_only", "body_capture"}:
        raise CaseError("workflow_audit_settings_shape_invalid", assertion_failure=True)
    return str(matches[0]["mode"])


def set_openai_audit_mode(browser: BrowserCase, mode: str, *, step_suffix: str = "") -> dict[str, Any]:
    if mode not in {"disabled", "metadata_only", "body_capture"}:
        raise CaseError("workflow_audit_mode_invalid")
    if step_suffix and not re.fullmatch(r"_[a-z0-9_]{2,24}", step_suffix):
        raise CaseError("workflow_audit_mode_step_invalid")
    origin = browser.spec.frontend_origin
    return browser.run_code(
        "audit_mode_" + mode + step_suffix,
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('audit-api-family-card').waitFor({state: 'visible', timeout: 15000});
const enabled = await expectVisible(page.getByRole('switch', {name: /OpenAI.*(?:启用审计|Enable audit)/i}), 'audit_enabled_switch_missing');
const capture = await expectVisible(page.getByRole('switch', {name: /OpenAI.*(?:捕获正文|Capture bodies)/i}), 'audit_capture_switch_missing');
const desired = %s;
let changed = false;
let enabledNow = await enabled.isChecked();
if (desired === 'disabled' && enabledNow) {
  await enabled.click(); changed = true; await page.waitForTimeout(100);
} else if (desired !== 'disabled' && !enabledNow) {
  await enabled.click(); changed = true; await page.waitForTimeout(100);
}
enabledNow = await enabled.isChecked();
if (desired !== 'disabled') {
  const captureNow = await capture.isChecked();
  const desiredCapture = desired === 'body_capture';
  if (captureNow !== desiredCapture) {
    await capture.click(); changed = true; await page.waitForTimeout(100);
  }
}
let status = null;
if (changed) {
  const save = await expectVisible(page.getByRole('button', {name: /保存更改|Save changes/i}), 'settings_save_missing');
  const responsePromise = responseFor('PUT', /^\/api\/settings\/audit$/);
  await save.click();
  const response = await responsePromise;
  status = response.status();
  if (status !== 200) throw new Error('audit_settings_save_status');
}
const finalEnabled = await enabled.isChecked();
const finalCapture = await capture.isChecked();
const actual = !finalEnabled ? 'disabled' : finalCapture ? 'body_capture' : 'metadata_only';
if (actual !== desired) throw new Error('audit_mode_not_applied');
return {surface: 'settings_audit', requested_mode: desired, actual_mode: actual, changed, save_status: status};
""" % json.dumps(mode))),
    )


def wait_usage_projection(
    database: support.LocalPostgres,
    caller_id: str,
    *,
    timeout: float = 20.0,
) -> dict[str, Any]:
    if not SAFE_IDENTIFIER_RE.fullmatch(caller_id):
        raise CaseError("workflow_caller_id_invalid")
    caller = support.sql_literal(caller_id)
    query = """
WITH ingress AS (
  SELECT DISTINCT ingress_request_id
  FROM request_logs
  WHERE caller_request_id = %s AND ingress_request_id IS NOT NULL
)
SELECT json_build_object(
  'rows', count(*),
  'usage_event_id', min(u.id),
  'ingress_request_id', min(u.ingress_request_id::text),
  'model_id', min(u.model_id),
  'endpoint_id', min(u.endpoint_id),
  'status_code', min(u.status_code),
  'success', bool_and(u.success_flag),
  'pricing_statuses', coalesce(json_agg(u.pricing_status ORDER BY u.id), '[]'::json),
  'pricing_trust', coalesce(json_agg(u.pricing_evidence_trust ORDER BY u.id), '[]'::json),
  'input_tokens', min(u.input_tokens),
  'output_tokens', min(u.output_tokens),
  'total_tokens', min(u.total_tokens),
  'cache_read_input_tokens', min(u.cache_read_input_tokens),
  'cache_creation_input_tokens', min(u.cache_creation_input_tokens),
  'reasoning_tokens', min(u.reasoning_tokens),
  'input_cost_micros', min(u.input_cost_micros),
  'output_cost_micros', min(u.output_cost_micros),
  'cache_read_input_cost_micros', min(u.cache_read_input_cost_micros),
  'cache_creation_input_cost_micros', min(u.cache_creation_input_cost_micros),
  'reasoning_cost_micros', min(u.reasoning_cost_micros),
  'total_cost_original_micros', min(u.total_cost_original_micros),
  'total_cost_user_currency_micros', min(u.total_cost_user_currency_micros),
  'currency_code_original', min(u.currency_code_original),
  'report_currency_code', min(u.report_currency_code),
  'report_currency_symbol', min(u.report_currency_symbol),
  'reporting_currency_epoch', min(u.reporting_currency_epoch),
  'fx_rate_used', min(u.fx_rate_used),
  'fx_rate_source', min(u.fx_rate_source),
  'endpoint_label_snapshot', min(u.endpoint_label_snapshot),
  'pricing_template_id_used', min(u.pricing_template_id_used),
  'pricing_template_name_snapshot', min(u.pricing_template_name_snapshot),
  'pricing_template_revision_id_used', min(u.pricing_template_revision_id_used),
  'pricing_snapshot_unit', min(u.pricing_snapshot_unit),
  'pricing_snapshot_input', min(u.pricing_snapshot_input),
  'pricing_snapshot_output', min(u.pricing_snapshot_output),
  'pricing_snapshot_cache_read_input', min(u.pricing_snapshot_cache_read_input),
  'pricing_snapshot_cache_creation_input', min(u.pricing_snapshot_cache_creation_input),
  'pricing_snapshot_reasoning', min(u.pricing_snapshot_reasoning)
)
FROM usage_request_events u
JOIN ingress i USING (ingress_request_id);
""" % caller
    return wait_database_object(
        database,
        query,
        lambda item: int(item.get("rows", 0)) == 1 and item.get("pricing_statuses") == ["priced"],
        timeout=timeout,
    )


def costing_projection(client: support.LocalHTTP) -> dict[str, Any]:
    value = api_object(client, "/api/settings/costing")
    result = {
        "report_currency_code": value.get("report_currency_code"),
        "report_currency_symbol": value.get("report_currency_symbol"),
        "reporting_currency_epoch": value.get("reporting_currency_epoch"),
        "expected_updated_at": value.get("expected_updated_at"),
    }
    if (
        not isinstance(result["report_currency_code"], str)
        or not isinstance(result["report_currency_symbol"], str)
        or isinstance(result["reporting_currency_epoch"], bool)
        or not isinstance(result["reporting_currency_epoch"], int)
        or result["reporting_currency_epoch"] < 1
        or not isinstance(result["expected_updated_at"], str)
    ):
        raise CaseError("workflow_costing_settings_shape_invalid", assertion_failure=True)
    return result


def run_currency_migration(
    browser: BrowserCase,
    *,
    target_code: str,
    target_symbol: str,
    step: str,
) -> dict[str, Any]:
    if not re.fullmatch(r"[A-Z]{3}", target_code) or not target_symbol or len(target_symbol) > 5:
        raise CaseError("workflow_currency_target_invalid")
    origin = browser.spec.frontend_origin
    return browser.run_code(
        step,
        action_for_origin(origin, ui_body(r"""
await clickButton(page, /迁移报告货币|Migrate reporting currency/i, 'currency_migration_button_missing');
const dialog = await expectVisible(page.getByRole('alertdialog'), 'currency_migration_dialog_missing');
await dialog.locator('input[name="target_currency_code"]').fill(%s);
await dialog.locator('input[name="target_currency_symbol"]').fill(%s);
await clickButton(dialog, /预览影响|Preview impact/i, 'currency_preview_missing');
const commit = await expectVisible(dialog.getByRole('button', {name: /确认迁移|Confirm migration/i}), 'currency_commit_missing');
const responsePromise = responseFor('POST', /^\/api\/settings\/costing\/currency-migrations\/commit$/);
await commit.click();
const response = await responsePromise;
if (response.status() !== 200) throw new Error('currency_commit_status');
await dialog.waitFor({state: 'hidden', timeout: 30000});
await page.waitForTimeout(500);
const body = await page.locator('body').innerText();
if (!body.includes(%s)) throw new Error('currency_target_not_visible');
return {surface: 'settings_currency', target_code: %s, target_symbol: %s, commit_status: response.status(), dialog_closed: true};
""" % tuple(json.dumps(value) for value in (target_code, target_symbol, target_code, target_code, target_symbol)))),
        timeout=240,
    )


def run_wfl_004(browser: BrowserCase, client: support.LocalHTTP, database: support.LocalPostgres) -> None:
    origin = browser.spec.frontend_origin
    fixture = create_runtime_fixture(browser, client, prefix="matrix-wfl-004")
    key_name = "matrix-wfl-004-proxy"
    caller_id = "matrix-wfl-004-runtime"
    payload_marker_index = int(browser.state["private_value_indexes"]["payload_marker"])

    browser.goto("/system/proxy-keys")
    browser.run_code(
        "proxy_key_create",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('proxy-keys-feature-page').waitFor({state: 'visible', timeout: 15000});
await clickButton(page, /发放密钥|Issue key/i, 'proxy_issue_missing');
const sheet = await expectVisible(page.getByTestId('proxy-key-issue-sheet'), 'proxy_issue_sheet_missing');
await fillLabel(sheet, /^名称$|^Name$/i, %s, 'proxy_name_missing');
const responsePromise = responseFor('POST', /^\/api\/settings\/auth\/proxy-keys$/);
await clickButton(sheet, /创建密钥|Create key/i, 'proxy_create_missing');
const response = await responsePromise;
if (response.status() !== 201) throw new Error('proxy_create_status');
const dialog = await expectVisible(page.getByRole('dialog', {name: /新密钥|New secret/i}), 'proxy_secret_dialog_missing');
const secretPanel = await expectVisible(dialog.getByTestId('proxy-key-secret'), 'proxy_secret_missing');
const raw = (await secretPanel.locator('p').first().innerText()).trim();
if (!/^pm-[A-Za-z0-9_-]{20,}$/.test(raw)) throw new Error('proxy_secret_shape');
Object.defineProperty(window, '__prismWflProxyKey', {value: raw, configurable: true, writable: true});
await dialog.getByLabel(/我已安全保存|saved this key/i).check();
await clickButton(dialog, /完成并关闭|Finish/i, 'proxy_finish_missing');
await dialog.waitFor({state: 'hidden', timeout: 10000});
return {surface: 'proxy_key', create_status: response.status(), one_time_dialog_closed: true, raw_value_returned: false};
""" % json.dumps(key_name))),
    )
    keys_response = api_object(client, "/api/settings/auth/proxy-keys")
    key_items = keys_response.get("items")
    if not isinstance(key_items, list):
        raise CaseError("wfl_004_proxy_key_list_shape_invalid", assertion_failure=True)
    key_item = find_named((item for item in key_items if isinstance(item, dict)), "name", key_name)
    proxy_key_id = safe_id(key_item.get("id"))
    proxy_snapshot = browser.capture_snapshot("proxy-key-created-ui")
    browser.write_text("proxy-key-ui.snapshot.txt", (browser.case_dir / proxy_snapshot["path"]).read_text(encoding="utf-8"))
    browser.checkpoint("proxy_key_created")
    browser.checkpoint("sensitive_ui_cleared")
    browser.start_trace(sensitive_ui_cleared=True)

    runtime = browser.run_code(
        "proxy_runtime_request",
        action_for_origin(origin, ui_body("""
const response = await page.evaluate(async ({callerId, model, binding}) => {
  const raw = window.__prismWflProxyKey;
  const payloadMarker = window.__prismWflPrivate?.[binding];
  if (typeof raw !== 'string' || !raw.startsWith('pm-')) throw new Error('proxy_secret_not_held');
  if (typeof payloadMarker !== 'string' || payloadMarker.length === 0) throw new Error('payload_marker_missing');
  const result = await fetch('/v1/responses', {
    method: 'POST',
    headers: {'content-type': 'application/json', 'authorization': 'Bearer ' + raw, 'x-request-id': callerId, 'x-prism-mock-request-id': callerId},
    body: JSON.stringify({model, input: payloadMarker, stream: false}),
  });
  const bytes = new Uint8Array(await result.arrayBuffer());
  const digest = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))).map((value) => value.toString(16).padStart(2, '0')).join('');
  delete window.__prismWflProxyKey;
  return {status: result.status, bytes: bytes.byteLength, sha256: digest, raw_value_destroyed: typeof window.__prismWflProxyKey === 'undefined'};
}, {callerId: %s, model: %s, binding: 'PRISM_WFL_PRIVATE_PAYLOAD'});
if (response.status !== 200) throw new Error('proxy_runtime_status');
return {surface: 'runtime', http_status: response.status, response_bytes: response.bytes, response_sha256: response.sha256, caller_id: %s, raw_value_destroyed: response.raw_value_destroyed};
""" % tuple(json.dumps(value) for value in (caller_id, fixture["runtime_model"], caller_id)))),
        private_environment={"PRISM_WFL_PRIVATE_PAYLOAD": payload_marker_index},
    )
    browser.write_json("runtime-response.redacted.json", {"request": runtime, "passed": True})
    browser.checkpoint("runtime_request_succeeded")

    projection = wait_request_projection(database, caller_id)
    request_log_id = safe_id(projection.get("request_log_id"))
    primary_connection_id = safe_id(fixture.get("primary_connection_id"))
    exact_projection = (
        projection.get("rows") == 1
        and projection.get("request_log_ids") == [request_log_id]
        and projection.get("model_ids") == [fixture["runtime_model"]]
        and projection.get("row_kinds") == ["upstream"]
        and projection.get("status_codes") == [200]
        and projection.get("attempt_numbers") == [1]
        and projection.get("attempt_results") == ["completed"]
        and projection.get("winner_flags") == [True]
        and projection.get("terminal_target_ids") == [primary_connection_id]
        and projection.get("terminal_target_labels") == [fixture["primary_terminal_label"]]
        and projection.get("endpoint_labels") == [fixture["endpoint_label"]]
        and projection.get("attribution_states") == ["identified"]
        and projection.get("proxy_key_ids") == [proxy_key_id]
        and projection.get("proxy_key_names") == [key_name]
    )
    if not exact_projection:
        raise CaseError("wfl_004_request_projection_mismatch", assertion_failure=True)
    finalized = wait_finalized_request(
        database,
        str(projection.get("ingress_request_id")),
        expected_status=200,
        expected_attempt_count=1,
        expected_terminal_target_id=primary_connection_id,
        expected_endpoint_label=str(fixture["endpoint_label"]),
        expected_failover=False,
    )
    chain_ui = inspect_request_chain_ui(
        browser,
        step="request_chain_inspect",
        projection=projection,
        expected_status_codes=(200,),
        expected_winner_flags=(True,),
        expected_final_target=str(fixture["primary_terminal_label"]),
        expected_endpoint=str(fixture["endpoint_label"]),
        open_request_log_id=request_log_id,
        detail_expected_texts=(key_name, fixture["runtime_model"]),
    )
    request_snapshot = browser.capture_snapshot("proxy-request-detail")
    request_text = (browser.case_dir / request_snapshot["path"]).read_text(encoding="utf-8")
    browser.write_text("request-detail.snapshot.txt", request_text)
    browser.checkpoint("request_detail_verified")

    browser.goto("/observe/requests/%d/audit" % request_log_id)
    browser.run_code(
        "audit_open",
        action_for_origin(origin, ui_body("""
await page.getByRole('heading', {name: /请求.*审计|Request.*audit/i}).waitFor({state: 'visible', timeout: 15000});
const text = await page.locator('body').innerText();
if (!/审计记录|Audit record/i.test(text)) throw new Error('audit_record_missing');
return {surface: 'audit', request_log_id: %d, audit_record_visible: true};
""" % request_log_id)),
    )
    audit_snapshot = browser.capture_snapshot("proxy-request-audit")
    browser.write_text("audit.snapshot.txt", (browser.case_dir / audit_snapshot["path"]).read_text(encoding="utf-8"))
    browser.checkpoint("audit_verified")

    browser.write_json(
        "attribution.json",
        {
            "proxy_key_id": proxy_key_id,
            "caller_id": caller_id,
            "request_projection": projection,
            "finalized_request": finalized,
            "ui_chain": chain_ui,
            "expected": {
                "request_log_id": request_log_id,
                "attempt_number": 1,
                "attempt_result": "completed",
                "winner": True,
                "terminal_target_id": primary_connection_id,
                "proxy_key_attribution_state": "identified",
            },
            "exact_match": True,
            "passed": True,
        },
    )
    browser.checkpoint("key_attribution_verified")

    browser.goto("/system/proxy-keys")
    browser.run_code(
        "proxy_key_delete",
        action_for_origin(origin, ui_body("""
await page.getByTestId('proxy-keys-feature-page').waitFor({state: 'visible', timeout: 15000});
const row = await expectVisible(page.getByRole('row').filter({hasText: %s}), 'proxy_key_row_missing');
await row.getByRole('button', {name: new RegExp('代理密钥.*' + %s + '.*更多操作|Proxy key.*more actions', 'i')}).click();
await expectVisible(page.getByRole('menuitem', {name: /删除密钥|Delete key/i}), 'proxy_delete_menu_missing').then((item) => item.click());
const responsePromise = responseFor('DELETE', new RegExp('^/api/settings/auth/proxy-keys/[0-9]+$'));
await clickButton(page.getByRole('dialog'), /删除密钥|Delete key/i, 'proxy_delete_confirm_missing');
const response = await responsePromise;
if (![200, 204].includes(response.status())) throw new Error('proxy_delete_status');
return {surface: 'proxy_key', deleted: true, delete_status: response.status()};
""" % (json.dumps(key_name), json.dumps(re.escape(key_name))))),
    )
    browser.checkpoint("proxy_key_revoked")
    fresh_keys = api_object(client, "/api/settings/auth/proxy-keys").get("items")
    if not isinstance(fresh_keys, list) or any(isinstance(item, dict) and item.get("id") == proxy_key_id for item in fresh_keys):
        raise CaseError("wfl_004_proxy_key_cleanup_failed", assertion_failure=True)


def run_wfl_005(browser: BrowserCase, client: support.LocalHTTP, database: support.LocalPostgres) -> None:
    origin = browser.spec.frontend_origin
    mock_origin = browser.spec.mock_origin
    if mock_origin is None:
        raise CaseError("wfl_005_mock_origin_missing")
    fixture = create_runtime_fixture(
        browser,
        client,
        prefix="matrix-wfl-005",
        with_fallback=True,
        until_reset_failure=True,
    )
    connections = database.read_json(
        """
SELECT json_build_object(
  'primary_id', max(id) FILTER (WHERE name = 'matrix-wfl-005-terminal-primary'),
  'fallback_id', max(id) FILTER (WHERE name = 'matrix-wfl-005-terminal-fallback')
) FROM connections
WHERE name IN ('matrix-wfl-005-terminal-primary', 'matrix-wfl-005-terminal-fallback');
"""
    )
    primary_id = safe_id(connections.get("primary_id"))
    fallback_id = safe_id(connections.get("fallback_id"))
    if (
        primary_id != fixture.get("primary_connection_id")
        or fallback_id != fixture.get("fallback_connection_id")
    ):
        raise CaseError("wfl_005_fallback_identity_mismatch")

    mock = support.LocalHTTP(mock_origin)
    incident_caller = "matrix-wfl-005-incident"
    _, installed = mock.json(
        "POST",
        "/__mock__/scripts",
        body={
            "request_id": incident_caller,
            "behaviors": [{"status": 503}, {}],
            "repeat_last": True,
        },
        expected=(200,),
    )
    if not isinstance(installed, dict) or installed.get("stored") is not True:
        raise CaseError("wfl_005_failure_script_install_failed")
    browser.start_trace()
    browser.checkpoint("failure_injected")

    incident_http = client.request(
        "POST",
        "/v1/responses",
        body={"model": fixture["runtime_model"], "input": "deterministic local failover probe", "stream": False},
        headers={"X-Request-ID": incident_caller, "X-Prism-Mock-Request-ID": incident_caller},
    )
    if incident_http.status != 200:
        raise CaseError("wfl_005_incident_request_failed", assertion_failure=True)
    projection = wait_request_projection(database, incident_caller, minimum_rows=2)
    ledger = safe_mock_ledger(mock, incident_caller)
    expected_chain = (
        projection.get("rows") == 2
        and isinstance(projection.get("request_log_ids"), list)
        and len(projection["request_log_ids"]) == 2
        and projection.get("request_log_id") == projection["request_log_ids"][0]
        and projection.get("model_ids") == [fixture["runtime_model"], fixture["runtime_model"]]
        and projection.get("row_kinds") == ["upstream", "upstream"]
        and projection.get("status_codes") == [503, 200]
        and projection.get("attempt_numbers") == [1, 2]
        and projection.get("attempt_results") == ["http_error", "completed"]
        and projection.get("terminal_target_ids") == [primary_id, fallback_id]
        and projection.get("terminal_target_labels") == [fixture["primary_terminal_label"], fixture["fallback_terminal_label"]]
        and projection.get("endpoint_labels") == [fixture["endpoint_label"], fixture["fallback_endpoint_label"]]
        and projection.get("winner_flags") == [False, True]
        and projection.get("attribution_states") == ["none", "none"]
        and projection.get("proxy_key_ids") == []
        and projection.get("proxy_key_names") == []
        and len(ledger) == 2
        and all(row.get("request_id") == incident_caller for row in ledger)
        and [row.get("planned_status") for row in ledger] == [503, 200]
    )
    if not expected_chain:
        raise CaseError("wfl_005_failover_chain_mismatch", assertion_failure=True)
    incident_finalized = wait_finalized_request(
        database,
        str(projection.get("ingress_request_id")),
        expected_status=200,
        expected_attempt_count=2,
        expected_terminal_target_id=fallback_id,
        expected_endpoint_label=str(fixture["fallback_endpoint_label"]),
        expected_failover=True,
    )
    incident_chain_ui = inspect_request_chain_ui(
        browser,
        step="incident_chain_inspect",
        projection=projection,
        expected_status_codes=(503, 200),
        expected_winner_flags=(False, True),
        expected_final_target=str(fixture["fallback_terminal_label"]),
        expected_endpoint=str(fixture["fallback_endpoint_label"]),
    )
    browser.write_json(
        "incident-request.json",
        {
            "caller_id": incident_caller,
            "http_status": incident_http.status,
            "response_bytes": len(incident_http.body),
            "response_sha256": hashlib.sha256(incident_http.body).hexdigest(),
            "request_projection": projection,
            "finalized_request": incident_finalized,
            "ui_chain": incident_chain_ui,
            "mock_ledger": ledger,
            "primary_terminal_target_id": primary_id,
            "fallback_terminal_target_id": fallback_id,
            "expected_attempt_chain": [
                {"attempt_number": 1, "status_code": 503, "attempt_result": "http_error", "winner": False, "terminal_target_id": primary_id},
                {"attempt_number": 2, "status_code": 200, "attempt_result": "completed", "winner": True, "terminal_target_id": fallback_id},
            ],
            "exact_match": True,
            "passed": True,
        },
    )
    browser.checkpoint("failover_observed")

    event = wait_database_object(
        database,
        """
SELECT json_build_object(
  'event_id', id,
  'connection_id', connection_id,
  'event_type', event_type,
  'failure_kind', failure_kind,
  'cycle_retry_attempts', cycle_retry_attempts,
  'cumulative_retry_attempts', cumulative_retry_attempts,
  'ban_mode', ban_mode,
  'policy_cycle_retry_attempt_limit', policy_cycle_retry_attempt_limit,
  'policy_ban_cumulative_retry_attempt_threshold', policy_ban_cumulative_retry_attempt_threshold
)
FROM loadbalance_events
WHERE connection_id = %d AND event_type = 'banned'
ORDER BY created_at DESC, id DESC
LIMIT 1;
""" % primary_id,
        lambda item: item.get("connection_id") == primary_id and item.get("event_type") == "banned",
    )
    event_id = safe_id(event.get("event_id"))
    banned_state = wait_current_state(client, primary_id, "banned")

    browser.goto("/observe/routing-health")
    browser.run_code(
        "routing_health_incident",
        action_for_origin(origin, ui_body("""
await page.getByTestId('routing-health-page').waitFor({state: 'visible', timeout: 15000});
await expectVisible(page.getByTestId(%s), 'primary_runtime_row_missing');
await expectVisible(page.getByTestId(%s), 'incident_event_row_missing');
return {surface: 'routing_health', primary_state_visible: true, event_visible: true, event_id: %d, terminal_target_id: %d};
""" % (json.dumps("runtime-row-%d" % primary_id), json.dumps("event-row-%d" % event_id), event_id, primary_id))),
        timeout=90,
    )
    health_snapshot = browser.capture_snapshot("routing-health-incident")
    browser.write_text("routing-health.snapshot.txt", (browser.case_dir / health_snapshot["path"]).read_text(encoding="utf-8"))
    browser.checkpoint("routing_health_verified")

    browser.run_code(
        "routing_event_detail",
        action_for_origin(origin, ui_body("""
const row = await expectVisible(page.getByTestId(%s), 'incident_event_row_missing');
await clickButton(row, /查看详情|View detail/i, 'event_detail_action_missing');
await expectVisible(page.getByText(/事件详情|Event details/i), 'event_detail_sheet_missing');
const body = await page.locator('body').innerText();
if (!body.includes(%s)) throw new Error('event_identity_not_visible');
return {surface: 'event_detail', event_id: %d, visible: true};
""" % (json.dumps("event-row-%d" % event_id), json.dumps(str(event_id)), event_id))),
    )
    event_snapshot = browser.capture_snapshot("routing-event-detail")
    browser.write_text("event-detail.snapshot.txt", (browser.case_dir / event_snapshot["path"]).read_text(encoding="utf-8"))
    browser.checkpoint("event_detail_verified")

    reset_action = browser.run_code(
        "routing_state_reset",
        action_for_origin(origin, ui_body("""
await page.keyboard.press('Escape');
const row = await expectVisible(page.getByTestId(%s), 'primary_runtime_row_missing');
const resetButton = await expectVisible(row.getByRole('button', {name: /重置冷却|Reset cooldown/i}), 'reset_button_missing');
await resetButton.click({force: true});
const dialog = await expectVisible(page.getByRole('alertdialog'), 'reset_dialog_missing');
const responsePromise = responseFor('POST', new RegExp('^/api/loadbalance/current-state/%d/reset$'));
await clickButton(dialog, /重置冷却|Reset cooldown/i, 'reset_confirm_missing');
const response = await responsePromise;
if (response.status() !== 200) throw new Error('reset_status');
const payload = await response.json();
return {surface: 'routing_health', status: response.status(), connection_id: payload.connection_id, cleared: payload.cleared === true, state: payload.state ? {state: payload.state.state, cycle_retry_attempts: payload.state.cycle_retry_attempts, cumulative_retry_attempts: payload.state.cumulative_retry_attempts, ban_mode: payload.state.ban_mode} : null};
""" % (json.dumps("runtime-row-%d" % primary_id), primary_id))),
    )
    reset_state = wait_current_state(client, primary_id, "available")
    if reset_state.get("cumulative_retry_attempts") != 0:
        raise CaseError("wfl_005_reset_state_mismatch", assertion_failure=True)
    browser.write_json(
        "reset-response.json",
        {
            "event": event,
            "before": {
                "state": banned_state.get("state"),
                "cumulative_retry_attempts": banned_state.get("cumulative_retry_attempts"),
                "ban_mode": banned_state.get("ban_mode"),
            },
            "after": {
                "state": reset_state.get("state"),
                "cumulative_retry_attempts": reset_state.get("cumulative_retry_attempts"),
                "ban_mode": reset_state.get("ban_mode"),
            },
            "ui_response": reset_action,
            "passed": True,
        },
    )
    browser.checkpoint("state_reset")

    recovery_caller = "matrix-wfl-005-recovery"
    _, recovery_installed = mock.json(
        "POST",
        "/__mock__/scripts",
        body={"request_id": recovery_caller, "behaviors": [{}], "repeat_last": True},
        expected=(200,),
    )
    if not isinstance(recovery_installed, dict) or recovery_installed.get("stored") is not True:
        raise CaseError("wfl_005_recovery_script_install_failed")
    recovery_http = client.request(
        "POST",
        "/v1/responses",
        body={"model": fixture["runtime_model"], "input": "deterministic local recovery probe", "stream": False},
        headers={"X-Request-ID": recovery_caller, "X-Prism-Mock-Request-ID": recovery_caller},
    )
    if recovery_http.status != 200:
        raise CaseError("wfl_005_recovery_request_failed", assertion_failure=True)
    recovery_projection = wait_request_projection(database, recovery_caller)
    recovery_ledger = safe_mock_ledger(mock, recovery_caller)
    recovered_state = wait_current_state(client, primary_id, "available")
    recovery_ok = (
        recovery_projection.get("rows") == 1
        and isinstance(recovery_projection.get("request_log_ids"), list)
        and len(recovery_projection["request_log_ids"]) == 1
        and recovery_projection.get("request_log_id") == recovery_projection["request_log_ids"][0]
        and recovery_projection.get("model_ids") == [fixture["runtime_model"]]
        and recovery_projection.get("row_kinds") == ["upstream"]
        and recovery_projection.get("status_codes") == [200]
        and recovery_projection.get("attempt_numbers") == [1]
        and recovery_projection.get("attempt_results") == ["completed"]
        and recovery_projection.get("terminal_target_ids") == [primary_id]
        and recovery_projection.get("terminal_target_labels") == [fixture["primary_terminal_label"]]
        and recovery_projection.get("endpoint_labels") == [fixture["endpoint_label"]]
        and recovery_projection.get("winner_flags") == [True]
        and recovery_projection.get("attribution_states") == ["none"]
        and recovery_projection.get("proxy_key_ids") == []
        and recovery_projection.get("proxy_key_names") == []
        and len(recovery_ledger) == 1
        and recovery_ledger[0].get("request_id") == recovery_caller
        and recovery_ledger[0].get("planned_status") == 200
        and recovered_state.get("last_success_at") is not None
    )
    if not recovery_ok:
        raise CaseError("wfl_005_primary_recovery_mismatch", assertion_failure=True)
    recovery_finalized = wait_finalized_request(
        database,
        str(recovery_projection.get("ingress_request_id")),
        expected_status=200,
        expected_attempt_count=1,
        expected_terminal_target_id=primary_id,
        expected_endpoint_label=str(fixture["endpoint_label"]),
        expected_failover=False,
    )
    recovery_chain_ui = inspect_request_chain_ui(
        browser,
        step="recovery_chain_inspect",
        projection=recovery_projection,
        expected_status_codes=(200,),
        expected_winner_flags=(True,),
        expected_final_target=str(fixture["primary_terminal_label"]),
        expected_endpoint=str(fixture["endpoint_label"]),
    )
    browser.write_json(
        "recovery-proof.json",
        {
            "caller_id": recovery_caller,
            "http_status": recovery_http.status,
            "request_projection": recovery_projection,
            "finalized_request": recovery_finalized,
            "ui_chain": recovery_chain_ui,
            "mock_ledger": recovery_ledger,
            "expected_attempt_chain": [
                {"attempt_number": 1, "status_code": 200, "attempt_result": "completed", "winner": True, "terminal_target_id": primary_id},
            ],
            "current_state": {
                "state": recovered_state.get("state"),
                "last_success_at_present": recovered_state.get("last_success_at") is not None,
                "terminal_target_id": primary_id,
            },
            "exact_match": True,
            "passed": True,
        },
    )
    browser.checkpoint("primary_recovered")
    for caller_id in (incident_caller, recovery_caller):
        mock.json("DELETE", "/__mock__/scripts/" + caller_id, expected=(200,))


def run_wfl_006(browser: BrowserCase, client: support.LocalHTTP, database: support.LocalPostgres) -> None:
    origin = browser.spec.frontend_origin
    fixture = create_runtime_fixture(
        browser,
        client,
        prefix="matrix-wfl-006",
        with_image_generation=True,
    )
    initial_settings = api_object(client, "/api/settings/audit")
    initial_mode = original_openai_audit_mode(initial_settings)
    payload_marker = private_value(browser, "payload_marker")
    credential_marker = private_value(browser, "credential_marker")
    payload_index = int(browser.state["private_value_indexes"]["payload_marker"])
    credential_index = int(browser.state["private_value_indexes"]["credential_marker"])

    browser.goto("/system/settings?scope=global&section=audit-privacy#audit-privacy")
    browser.start_trace()
    disabled_setting = set_openai_audit_mode(browser, "disabled")
    disabled_snapshot = browser.capture_snapshot("audit-settings-disabled", group="settings")
    disabled_caller = "matrix-wfl-006-disabled"
    disabled_http = client.request(
        "POST",
        "/v1/responses",
        body={"model": fixture["runtime_model"], "input": "disabled audit probe", "stream": False},
        headers={"X-Request-ID": disabled_caller, "X-Prism-Mock-Request-ID": disabled_caller},
    )
    if disabled_http.status != 200:
        raise CaseError("wfl_006_disabled_probe_failed", assertion_failure=True)
    disabled_facts = wait_audit_projection(database, disabled_caller, expected_audit_rows=0)
    if disabled_facts.get("enabled_flags") != [False]:
        raise CaseError("wfl_006_disabled_snapshot_mismatch", assertion_failure=True)
    disabled_request_id = safe_id(disabled_facts.get("request_log_id"))
    browser.goto("/observe/requests/%d/audit" % disabled_request_id)
    disabled_ui = browser.run_code(
        "audit_disabled_detail",
        action_for_origin(origin, ui_body("""
await page.getByTestId('dedicated-request-log-audit-page').waitFor({state: 'visible', timeout: 15000});
const text = await page.locator('body').innerText();
if (!/审计.*禁用|Audit.*disabled/i.test(text)) throw new Error('audit_disabled_copy_missing');
if (await page.getByTestId('dedicated-audit-detail').count()) throw new Error('disabled_audit_detail_present');
if (await page.locator('a[href*="/body/"]').count()) throw new Error('disabled_raw_download_present');
return {surface: 'request_audit', mode: 'disabled', disabled_state_visible: true, audit_record_visible: false, raw_download_available: false};
""")),
    )
    disabled_detail_snapshot = browser.capture_snapshot("audit-detail-disabled")
    browser.checkpoint("disabled_mode_verified")

    browser.goto("/system/settings?scope=global&section=audit-privacy#audit-privacy")
    metadata_setting = set_openai_audit_mode(browser, "metadata_only")
    metadata_snapshot = browser.capture_snapshot("audit-settings-metadata", group="settings")
    metadata_caller = "matrix-wfl-006-metadata"
    metadata_http = client.request(
        "POST",
        "/v1/responses",
        body={"model": fixture["runtime_model"], "input": payload_marker, "stream": False},
        headers={
            "X-Request-ID": metadata_caller,
            "X-Prism-Mock-Request-ID": metadata_caller,
            "X-Matrix-Context": "credential=" + credential_marker,
        },
    )
    if metadata_http.status != 200:
        raise CaseError("wfl_006_metadata_probe_failed", assertion_failure=True)
    metadata_facts = wait_audit_projection(database, metadata_caller, expected_audit_rows=1)
    if (
        metadata_facts.get("enabled_flags") != [True]
        or metadata_facts.get("capture_flags") != [False]
        or metadata_facts.get("ingress_payload_stored_flags") != [False]
        or metadata_facts.get("result_payload_stored_flags") != [False]
        or metadata_facts.get("scrub_sentinel_present") is not True
    ):
        raise CaseError("wfl_006_metadata_snapshot_mismatch", assertion_failure=True)
    metadata_request_id = safe_id(metadata_facts.get("request_log_id"))
    metadata_audit_ids = metadata_facts.get("audit_ids")
    if not isinstance(metadata_audit_ids, list) or len(metadata_audit_ids) != 1:
        raise CaseError("wfl_006_metadata_audit_identity_missing", assertion_failure=True)
    metadata_audit_id = safe_id(metadata_audit_ids[0])
    browser.goto("/observe/requests/%d/audit?audit_id=%d" % (metadata_request_id, metadata_audit_id))
    metadata_ui = browser.run_code(
        "audit_metadata_detail",
        action_for_origin(origin, ui_body("""
await page.getByTestId('dedicated-audit-detail').waitFor({state: 'visible', timeout: 15000});
const facts = await page.evaluate((bindings) => {
  const text = document.body.innerText;
  const values = bindings.map((name) => window.__prismWflPrivate?.[name]);
  return {
    mode_visible: /仅元数据|Metadata only/i.test(text),
    private_value_visible: values.some((value) => typeof value !== 'string' || value.length === 0 || text.includes(value)),
  };
}, ['PRISM_WFL_PRIVATE_PAYLOAD', 'PRISM_WFL_PRIVATE_CONTEXT']);
if (!facts.mode_visible) throw new Error('metadata_mode_copy_missing');
if (facts.private_value_visible) throw new Error('metadata_private_value_visible');
return {surface: 'request_audit', mode: 'metadata_only', detail_visible: true, private_value_visible: false};
""")),
        private_environment={
            "PRISM_WFL_PRIVATE_PAYLOAD": payload_index,
            "PRISM_WFL_PRIVATE_CONTEXT": credential_index,
        },
    )
    metadata_detail_snapshot = browser.capture_snapshot("audit-detail-metadata")
    browser.checkpoint("metadata_mode_verified")

    browser.goto("/system/settings?scope=global&section=audit-privacy#audit-privacy")
    body_setting = set_openai_audit_mode(browser, "body_capture")
    body_snapshot = browser.capture_snapshot("audit-settings-body", group="settings")
    body_caller = "matrix-wfl-006-body"
    body_http = client.request(
        "POST",
        "/v1/images/generations",
        body={
            "model": fixture["runtime_model"],
            "prompt": "local image audit redaction probe",
            "image": "data:image/png;base64," + payload_marker,
            "stream": False,
        },
        headers={
            "X-Request-ID": body_caller,
            "X-Prism-Mock-Request-ID": body_caller,
            "X-Matrix-Context": "credential=" + credential_marker,
        },
    )
    if body_http.status != 200:
        raise CaseError("wfl_006_body_probe_failed", assertion_failure=True)
    body_facts = wait_audit_projection(database, body_caller, expected_audit_rows=1)
    if (
        body_facts.get("enabled_flags") != [True]
        or body_facts.get("capture_flags") != [True]
        or body_facts.get("ingress_payload_stored_flags") != [True]
        or body_facts.get("result_payload_stored_flags") != [True]
        or body_facts.get("scrub_sentinel_present") is not True
        or any(int(value) <= 0 for value in body_facts.get("ingress_stored_bytes", []))
        or any(int(value) <= 0 for value in body_facts.get("result_stored_bytes", []))
    ):
        raise CaseError("wfl_006_body_snapshot_mismatch", assertion_failure=True)
    body_request_id = safe_id(body_facts.get("request_log_id"))
    body_audit_ids = body_facts.get("audit_ids")
    if not isinstance(body_audit_ids, list) or len(body_audit_ids) != 1:
        raise CaseError("wfl_006_body_audit_identity_missing", assertion_failure=True)
    body_audit_id = safe_id(body_audit_ids[0])
    browser.goto("/observe/requests/%d/audit?audit_id=%d" % (body_request_id, body_audit_id))
    body_ui = browser.run_code(
        "audit_body_detail",
        action_for_origin(origin, ui_body("""
await page.getByTestId('dedicated-audit-detail').waitFor({state: 'visible', timeout: 15000});
const facts = await page.evaluate((bindings) => {
  const text = document.body.innerText;
  const values = bindings.map((name) => window.__prismWflPrivate?.[name]);
  return {
    mode_visible: /完整捕获|Full capture/i.test(text),
    image_redaction_visible: text.includes('[redacted image bytes]'),
    private_value_visible: values.some((value) => typeof value !== 'string' || value.length === 0 || text.includes(value)),
  };
}, ['PRISM_WFL_PRIVATE_PAYLOAD', 'PRISM_WFL_PRIVATE_CONTEXT']);
if (!facts.mode_visible) throw new Error('body_mode_copy_missing');
if (!facts.image_redaction_visible) throw new Error('image_redaction_missing');
if (facts.private_value_visible) throw new Error('body_private_value_visible');
return {surface: 'request_audit', mode: 'body_capture', detail_visible: true, image_redaction_visible: true, private_value_visible: false};
""")),
        private_environment={
            "PRISM_WFL_PRIVATE_PAYLOAD": payload_index,
            "PRISM_WFL_PRIVATE_CONTEXT": credential_index,
        },
    )
    body_detail_snapshot = browser.capture_snapshot("audit-detail-body")
    browser.checkpoint("body_mode_verified")

    database_marker_scan = audit_marker_absence_projection(
        database,
        (payload_marker, credential_marker),
    )

    def run_download_mode(mode: str, audit_id: int, request_id: int) -> dict[str, Any]:
        detail_path = "/observe/requests/%d/audit?audit_id=%d" % (request_id, audit_id)
        browser.goto(detail_path)
        return browser.run_code(
            "audit_downloads_%s" % ("metadata" if mode == "metadata_only" else "body"),
            action_for_origin(origin, ui_body("""
const item = %s;
const results = [];
  const detail = await expectVisible(page.getByTestId('dedicated-audit-detail'), 'download_detail_missing');
  const detailText = await detail.innerText();
  const modeVisible = item.mode === 'metadata_only'
    ? /仅元数据|Metadata only/i.test(detailText)
    : /完整捕获|Full capture/i.test(detailText);
  if (!modeVisible) throw new Error('download_mode_context_missing');
  const sourceOrigin = urlOrigin(page.url());
  if (urlPath(page.url()) !== item.detail_path.split('?')[0]) throw new Error('download_source_page_mismatch');
  for (const direction of ['request', 'response']) {
    const path = `/api/audit/logs/${item.audit_id}/body/${direction}`;
    const downloadPromise = page.waitForEvent('download', {timeout: 20000});
    const responsePromise = page.waitForResponse((response) => {
      return response.request().method() === 'GET' && urlPath(response.url()) === path;
    }, {timeout: 20000});
    const triggerId = `wfl006-download-${item.mode}-${direction}`;
    await page.evaluate(({href, triggerId}) => {
      const anchor = document.createElement('a');
      anchor.href = href;
      anchor.download = '';
      anchor.dataset.testid = triggerId;
      anchor.textContent = `Download raw ${direction} body`;
      anchor.style.display = 'inline-block';
      document.body.appendChild(anchor);
    }, {href: path, triggerId});
    const trigger = await expectVisible(page.getByTestId(triggerId), 'download_visible_trigger_missing');
    const [download, response] = await Promise.all([downloadPromise, responsePromise, trigger.click()]);
    await trigger.evaluate((element) => element.remove());
    if (response.status() !== 200) throw new Error('download_response_status');
    if (urlOrigin(response.url()) !== sourceOrigin) throw new Error('download_cross_origin_response');
    const headers = await response.allHeaders();
    const expectedName = `audit-${item.audit_id}-${direction}.bin`;
    const expectedDisposition = `attachment; filename="${expectedName}"`;
    if (headers['content-type'] !== 'application/octet-stream') throw new Error('download_content_type');
    if (headers['content-disposition'] !== expectedDisposition) throw new Error('download_content_disposition');
    if (headers['x-content-type-options'] !== 'nosniff') throw new Error('download_nosniff_missing');
    if (headers['content-security-policy'] !== 'sandbox') throw new Error('download_sandbox_missing');
    if (headers['cache-control'] !== 'private, no-store') throw new Error('download_private_cache_missing');
    const vary = (headers.vary || '').toLowerCase().split(',').map((value) => value.trim());
    if (!['authorization', 'cookie', 'x-profile-id'].every((value) => vary.includes(value))) {
      throw new Error('download_permission_vary_missing');
    }
    const stream = await download.createReadStream();
    if (!stream) throw new Error('download_stream_missing');
    let streamBytes = 0;
    for await (const chunk of stream) {
      const length = Number(chunk?.byteLength ?? chunk?.length ?? -1);
      if (!Number.isInteger(length) || length < 0) throw new Error('download_stream_chunk_invalid');
      streamBytes += length;
    }
    const failure = await download.failure();
    if (failure !== null) throw new Error('download_browser_permission_failed');
    if (download.suggestedFilename() !== expectedName) throw new Error('download_filename_mismatch');
    const downloadedPath = await download.path();
    if (typeof downloadedPath !== 'string' || downloadedPath.length === 0) throw new Error('download_path_missing');
    const loaderId = `wfl006-downloaded-file-${item.mode}-${direction}`;
    await page.evaluate((id) => {
      const existing = document.getElementById(id);
      if (existing) existing.remove();
      const input = document.createElement('input');
      input.type = 'file';
      input.id = id;
      input.style.display = 'none';
      document.documentElement.appendChild(input);
    }, loaderId);
    await page.locator(`#${loaderId}`).setInputFiles(downloadedPath);
    const fileFacts = await page.evaluate(async ({id, bindings}) => {
      const input = document.getElementById(id);
      const file = input?.files?.[0];
      if (!file) return null;
      const buffer = await file.arrayBuffer();
      const bytes = new Uint8Array(buffer);
      const digest = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)))
        .map((value) => value.toString(16).padStart(2, '0')).join('');
      const text = new TextDecoder().decode(bytes);
      const values = bindings.map((name) => window.__prismWflPrivate?.[name]);
      input.value = '';
      input.remove();
      return {
        bytes: bytes.byteLength,
        sha256: digest,
        private_value_match: values.some((value) => typeof value !== 'string' || value.length === 0 || text.includes(value)),
        product_image_redaction_present: text.includes('[redacted image bytes]'),
      };
    }, {id: loaderId, bindings: ['PRISM_WFL_PRIVATE_PAYLOAD', 'PRISM_WFL_PRIVATE_CONTEXT']});
    if (!fileFacts || fileFacts.bytes !== streamBytes) throw new Error('download_file_stream_mismatch');
    if (Number(headers['content-length']) !== fileFacts.bytes) throw new Error('download_content_length_mismatch');
    results.push({
      mode: item.mode,
      direction,
      source_page: item.detail_path,
      ui_context: {detail_visible: true, mode_visible: true, same_origin: true},
      browser_permission: {visible_trigger: true, download_event: true, stream_available: true, failure: null},
      response: {
        status: response.status(),
        content_type: headers['content-type'],
        content_disposition: headers['content-disposition'],
        content_length: Number(headers['content-length']),
        private_no_store: true,
        profile_auth_vary: true,
        nosniff: true,
        sandbox: true,
        bytes_observed: headers['x-prism-body-bytes-observed'] ? Number(headers['x-prism-body-bytes-observed']) : null,
        bytes_stored: headers['x-prism-body-bytes-stored'] ? Number(headers['x-prism-body-bytes-stored']) : null,
        truncated: headers['x-prism-body-truncated'] === 'true',
        capture_end_state: headers['x-prism-body-capture-end-state'] || null,
      },
      bytes: fileFacts.bytes,
      sha256: fileFacts.sha256,
      private_value_match: fileFacts.private_value_match,
      product_image_redaction_present: fileFacts.product_image_redaction_present,
      suggested_name: download.suggestedFilename(),
    });
  }
if (results.some((item) => item.private_value_match)) throw new Error('download_private_value_visible');
return {surface: 'audit_download', downloads: results};
""" % json.dumps({"mode": mode, "audit_id": audit_id, "detail_path": detail_path}))),
            private_environment={
                "PRISM_WFL_PRIVATE_PAYLOAD": payload_index,
                "PRISM_WFL_PRIVATE_CONTEXT": credential_index,
            },
            timeout=120,
        )

    metadata_download_result = run_download_mode(
        "metadata_only", metadata_audit_id, metadata_request_id
    )
    body_download_result = run_download_mode("body_capture", body_audit_id, body_request_id)
    metadata_download_rows = metadata_download_result.get("downloads")
    body_download_rows = body_download_result.get("downloads")
    download_rows = (
        [*metadata_download_rows, *body_download_rows]
        if isinstance(metadata_download_rows, list) and isinstance(body_download_rows, list)
        else None
    )
    if not isinstance(download_rows, list) or len(download_rows) != 4:
        raise CaseError("wfl_006_download_grid_incomplete", assertion_failure=True)
    metadata_downloads = [item for item in download_rows if isinstance(item, dict) and item.get("mode") == "metadata_only"]
    body_downloads = [item for item in download_rows if isinstance(item, dict) and item.get("mode") == "body_capture"]
    body_request_downloads = [item for item in body_downloads if item.get("direction") == "request"]
    if (
        len(metadata_downloads) != 2
        or any(item.get("bytes") != 0 for item in metadata_downloads)
        or len(body_downloads) != 2
        or any(not isinstance(item.get("bytes"), int) or item["bytes"] <= 0 for item in body_downloads)
        or any(item.get("private_value_match") is not False for item in download_rows if isinstance(item, dict))
        or len(body_request_downloads) != 1
        or body_request_downloads[0].get("product_image_redaction_present") is not True
        or any(item.get("ui_context") != {"detail_visible": True, "mode_visible": True, "same_origin": True} for item in download_rows)
        or any(
            item.get("browser_permission")
            != {"visible_trigger": True, "download_event": True, "stream_available": True, "failure": None}
            for item in download_rows
        )
        or any(
            not isinstance(item.get("response"), dict)
            or item["response"].get("status") != 200
            or item["response"].get("content_type") != "application/octet-stream"
            or item["response"].get("content_length") != item.get("bytes")
            or item["response"].get("private_no_store") is not True
            or item["response"].get("profile_auth_vary") is not True
            or item["response"].get("nosniff") is not True
            or item["response"].get("sandbox") is not True
            for item in download_rows
        )
    ):
        raise CaseError("wfl_006_download_semantics_mismatch", assertion_failure=True)
    browser.write_json(
        "raw-downloads.redacted.json",
        {
            "downloads": download_rows,
            "database_marker_absence": database_marker_scan,
            "passed": True,
        },
    )
    browser.checkpoint("raw_download_verified")

    browser.stop_trace()
    private_values = workflow.load_private_values(pathlib.Path(browser.state["paths"]["private_values"]))
    scan_paths = [
        browser.case_dir / item["path"]
        for item in [disabled_snapshot, disabled_detail_snapshot, metadata_snapshot, metadata_detail_snapshot, body_snapshot, body_detail_snapshot]
    ] + [browser.case_dir / "raw-downloads.redacted.json", browser.case_dir / "trace.zip"]
    for path in scan_paths:
        if path.suffix == ".zip":
            workflow.validate_trace_archive(path, private_values, require_redaction_manifest=True)
        else:
            workflow.assert_no_remaining_secret(path.read_text(encoding="utf-8", errors="strict"), path.name, private_values)
    browser.write_text(
        "secret-scan.txt",
        "case_id=WFL-006\nstatus=passed\nprivate_values_checked=3\npublic_artifacts_checked=%d\nunified_trace_sanitizer=passed\n" % len(scan_paths),
    )
    browser.checkpoint("secret_scan_passed")

    browser.goto("/system/settings?scope=global&section=audit-privacy#audit-privacy")
    restored_setting = set_openai_audit_mode(browser, initial_mode, step_suffix="_restore")
    browser.capture_snapshot("audit-settings-restored", group="settings")
    restored_settings = api_object(client, "/api/settings/audit")
    if original_openai_audit_mode(restored_settings) != initial_mode:
        raise CaseError("wfl_006_settings_restore_failed", assertion_failure=True)
    browser.checkpoint("settings_restored")

    browser.write_json(
        "settings-snapshots.json",
        {
            "initial_mode": initial_mode,
            "transitions": [disabled_setting, metadata_setting, body_setting, restored_setting],
            "snapshots": browser.settings,
            "restored": True,
            "passed": True,
        },
    )
    browser.write_json(
        "audit-mode-details.json",
        {
            "modes": [
                {"mode": "disabled", "settings": disabled_setting, "runtime_status": disabled_http.status, "facts": disabled_facts, "ui": disabled_ui},
                {"mode": "metadata_only", "settings": metadata_setting, "runtime_status": metadata_http.status, "facts": metadata_facts, "ui": metadata_ui},
                {"mode": "body_capture", "settings": body_setting, "runtime_status": body_http.status, "facts": body_facts, "ui": body_ui},
            ],
            "database_marker_absence": database_marker_scan,
            "passed": True,
        },
    )


def format_wfl_007_money(micros: int, symbol: str, *, maximum_fraction_digits: int) -> str:
    if isinstance(micros, bool) or not isinstance(micros, int) or micros < 0:
        raise CaseError("wfl_007_money_value_invalid", assertion_failure=True)
    if not isinstance(symbol, str) or not symbol or len(symbol) > 5:
        raise CaseError("wfl_007_money_symbol_invalid", assertion_failure=True)
    if maximum_fraction_digits not in {4, 6}:
        raise CaseError("wfl_007_money_precision_invalid", assertion_failure=True)
    if maximum_fraction_digits == 4:
        micros = ((micros + 50) // 100) * 100
    whole = micros // 1_000_000
    fractional = "%06d" % (micros % 1_000_000)
    fractional = fractional[:maximum_fraction_digits].rstrip("0")
    if maximum_fraction_digits == 6:
        fractional = fractional.ljust(2, "0")
    else:
        fractional = fractional.ljust(4, "0")
    return "%s%d.%s" % (symbol, whole, fractional)


def wfl_007_database_numeric_projection(
    usage: Mapping[str, Any],
    costing: Mapping[str, Any],
    fixture: Mapping[str, Any],
    pricing_id: int,
) -> dict[str, Any]:
    expected_tokens = {
        "input_tokens": 19,
        "output_tokens": 23,
        "total_tokens": 42,
        "cache_read_input_tokens": 5,
        "cache_creation_input_tokens": None,
        "reasoning_tokens": 7,
    }
    expected_costs = {
        "input_cost_micros": 19,
        "output_cost_micros": 46,
        "cache_read_input_cost_micros": 3,
        "cache_creation_input_cost_micros": 0,
        "reasoning_cost_micros": 21,
        "total_cost_original_micros": 89,
        "total_cost_user_currency_micros": 89,
    }
    expected_snapshots = {
        "pricing_snapshot_unit": "PER_1M",
        "pricing_snapshot_input": "1",
        "pricing_snapshot_output": "2",
        "pricing_snapshot_cache_read_input": "0.5",
        "pricing_snapshot_cache_creation_input": "0.75",
        "pricing_snapshot_reasoning": "3",
    }
    currency_code = costing.get("report_currency_code")
    currency_symbol = costing.get("report_currency_symbol")
    identity_checks = {
        "rows": 1,
        "status_code": 200,
        "success": True,
        "model_id": fixture.get("runtime_model"),
        "endpoint_id": fixture.get("endpoint_id"),
        "endpoint_label_snapshot": fixture.get("endpoint_label"),
        "pricing_template_id_used": pricing_id,
        "pricing_template_name_snapshot": "matrix-wfl-007-pricing",
        "pricing_statuses": ["priced"],
        "pricing_trust": ["trusted"],
        "currency_code_original": currency_code,
        "report_currency_code": currency_code,
        "report_currency_symbol": currency_symbol,
        "reporting_currency_epoch": costing.get("reporting_currency_epoch"),
        "fx_rate_used": "1",
        "fx_rate_source": "DEFAULT_1_TO_1",
    }
    expected = {**expected_tokens, **expected_costs, **expected_snapshots, **identity_checks}
    if any(usage.get(key) != value for key, value in expected.items()):
        raise CaseError("wfl_007_database_numeric_projection_mismatch", assertion_failure=True)
    usage_event_id_value = usage.get("usage_event_id")
    revision_id_value = usage.get("pricing_template_revision_id_used")
    if any(
        isinstance(value, bool) or not isinstance(value, int) or value <= 0
        for value in (usage_event_id_value, revision_id_value)
    ):
        raise CaseError("wfl_007_usage_identity_invalid", assertion_failure=True)
    usage_event_id = int(usage_event_id_value)
    revision_id = int(revision_id_value)
    ingress_request_id = usage.get("ingress_request_id")
    if not isinstance(ingress_request_id, str) or not SAFE_IDENTIFIER_RE.fullmatch(ingress_request_id):
        raise CaseError("wfl_007_usage_ingress_identity_invalid", assertion_failure=True)
    projection = {
        "usage_event_id": usage_event_id,
        "ingress_request_id": ingress_request_id,
        "model_id": usage.get("model_id"),
        "endpoint_id": usage.get("endpoint_id"),
        "endpoint_label": usage.get("endpoint_label_snapshot"),
        "pricing_template_id": pricing_id,
        "pricing_template_revision_id": revision_id,
        "tokens": {key: usage.get(key) for key in expected_tokens},
        "costs_micros": {key: usage.get(key) for key in expected_costs},
        "pricing_snapshots": {key: usage.get(key) for key in expected_snapshots},
        "currency": {
            "source_code": usage.get("currency_code_original"),
            "report_code": usage.get("report_currency_code"),
            "report_symbol": usage.get("report_currency_symbol"),
            "reporting_epoch": usage.get("reporting_currency_epoch"),
            "fx_rate": usage.get("fx_rate_used"),
            "fx_source": usage.get("fx_rate_source"),
        },
        "status_code": usage.get("status_code"),
        "pricing_status": "priced",
        "pricing_trust": "trusted",
    }
    workflow.assert_safe_json(projection, "wfl_007_database_numeric_projection")
    return projection


def wfl_007_request_detail_ui(
    browser: BrowserCase,
    *,
    request_log_id: int,
    database_projection: Mapping[str, Any],
    step: str,
) -> dict[str, Any]:
    tokens = database_projection.get("tokens")
    costs = database_projection.get("costs_micros")
    snapshots = database_projection.get("pricing_snapshots")
    currency = database_projection.get("currency")
    if not all(isinstance(value, Mapping) for value in (tokens, costs, snapshots, currency)):
        raise CaseError("wfl_007_detail_projection_shape_invalid", assertion_failure=True)
    symbol = currency.get("report_symbol")
    total_micros = costs.get("total_cost_user_currency_micros")
    if not isinstance(symbol, str) or isinstance(total_micros, bool) or not isinstance(total_micros, int):
        raise CaseError("wfl_007_detail_projection_shape_invalid", assertion_failure=True)
    expected = {
        "request_log_id": request_log_id,
        "input": str(tokens.get("input_tokens")),
        "output": str(tokens.get("output_tokens")),
        "total": str(tokens.get("total_tokens")),
        "cache_read": str(tokens.get("cache_read_input_tokens")),
        "cache_creation": "—" if tokens.get("cache_creation_input_tokens") is None else str(tokens.get("cache_creation_input_tokens")),
        "reasoning": str(tokens.get("reasoning_tokens")),
        "total_cost": format_wfl_007_money(total_micros, symbol, maximum_fraction_digits=6),
        "report_currency": currency.get("report_code"),
        "source_currency": currency.get("source_code"),
        "fx_rate": currency.get("fx_rate"),
        "fx_source": currency.get("fx_source"),
        "pricing_unit": snapshots.get("pricing_snapshot_unit"),
        "snapshot_input": snapshots.get("pricing_snapshot_input"),
        "snapshot_output": snapshots.get("pricing_snapshot_output"),
        "snapshot_cache_read": snapshots.get("pricing_snapshot_cache_read_input"),
        "snapshot_cache_creation": snapshots.get("pricing_snapshot_cache_creation_input"),
        "snapshot_reasoning": snapshots.get("pricing_snapshot_reasoning"),
    }
    workflow.assert_safe_json(expected, "wfl_007_detail_expected")
    browser.goto("/observe/requests?request_id=%d&view=attempts" % request_log_id)
    result = browser.run_code(
        step,
        action_for_origin(browser.spec.frontend_origin, ui_body(r"""
const expected = %s;
const clean = (value) => value.replaceAll('\u00a0', ' ').trim();
const sheet = await expectVisible(page.getByTestId('request-log-detail-sheet'), 'request_detail_sheet_missing');
const grid = await expectVisible(sheet.getByTestId('request-log-overview-grid'), 'request_detail_grid_missing');
const readRow = async (pattern, code) => {
  const label = await expectVisible(grid.getByText(pattern, {exact: true}), code + '_label_missing');
  const value = label.locator('..').locator(':scope > div').first();
  await value.waitFor({state: 'visible', timeout: 15000}).catch(() => { throw new Error(code + '_value_missing'); });
  return clean(await value.innerText());
};
const values = {
  input: await readRow(/^输入$|^Input$/i, 'input_tokens'),
  output: await readRow(/^输出$|^Output$/i, 'output_tokens'),
  total: await readRow(/^总计$|^Total$/i, 'total_tokens'),
  cache_read: await readRow(/^缓存读取$|^Cache read$/i, 'cache_read_tokens'),
  cache_creation: await readRow(/^缓存创建$|^Cache creation$/i, 'cache_creation_tokens'),
  reasoning: await readRow(/^推理$|^Reasoning$/i, 'reasoning_tokens'),
  total_cost: await readRow(/^总费用$|^Total cost$/i, 'total_cost'),
  report_currency: await readRow(/^报告币种$|^Report currency$/i, 'report_currency'),
  source_currency: await readRow(/^原始币种$|^Source currency$/i, 'source_currency'),
  fx_rate: await readRow(/^使用的汇率$|^FX rate used$/i, 'fx_rate'),
  fx_source: await readRow(/^汇率来源$|^FX rate source$/i, 'fx_source'),
  pricing_unit: await readRow(/^定价单位$|^Pricing unit$/i, 'pricing_unit'),
  snapshot_input: await readRow(/^定价快照输入$|^Pricing snapshot input$/i, 'snapshot_input'),
  snapshot_output: await readRow(/^定价快照输出$|^Pricing snapshot output$/i, 'snapshot_output'),
  snapshot_cache_read: await readRow(/^定价快照缓存读取$|^Pricing snapshot cache read$/i, 'snapshot_cache_read'),
  snapshot_cache_creation: await readRow(/^定价快照缓存创建$|^Pricing snapshot cache creation$/i, 'snapshot_cache_creation'),
  snapshot_reasoning: await readRow(/^定价快照推理$|^Pricing snapshot reasoning$/i, 'snapshot_reasoning'),
};
for (const [key, expectedValue] of Object.entries(expected)) {
  if (key === 'request_log_id') continue;
  if (values[key] !== String(expectedValue)) throw new Error(`request_detail_${key}_mismatch`);
}
const summary = clean(await sheet.getByTestId('request-log-summary-strip').innerText());
if (!summary.includes(expected.total) || !summary.includes(expected.total_cost)) throw new Error('request_detail_summary_numeric_mismatch');
if (!/已定价|Priced/i.test(await sheet.innerText())) throw new Error('request_detail_pricing_status_missing');
const requestIdMatch = String(page.url()).match(/[?&]request_id=([^&#]*)/);
if (!requestIdMatch || decodeURIComponent(requestIdMatch[1]) !== String(expected.request_log_id)) throw new Error('request_detail_identity_mismatch');
return {
  surface: 'request_detail',
  request_log_id: expected.request_log_id,
  exact_values: values,
  summary_total_tokens_visible: true,
  summary_total_cost_visible: true,
  priced_status_visible: true,
};
""" % json.dumps(expected))),
    )
    exact_values = result.get("exact_values")
    if (
        result.get("request_log_id") != request_log_id
        or not isinstance(exact_values, dict)
        or any(exact_values.get(key) != str(value) for key, value in expected.items() if key != "request_log_id")
        or result.get("summary_total_tokens_visible") is not True
        or result.get("summary_total_cost_visible") is not True
        or result.get("priced_status_visible") is not True
    ):
        raise CaseError("wfl_007_request_detail_ui_mismatch", assertion_failure=True)
    return result


def wfl_007_activity_ui(
    browser: BrowserCase,
    *,
    database_projection: Mapping[str, Any],
    model_label: str,
    step: str,
) -> dict[str, Any]:
    currency = database_projection.get("currency")
    costs = database_projection.get("costs_micros")
    tokens = database_projection.get("tokens")
    if not all(isinstance(value, Mapping) for value in (currency, costs, tokens)):
        raise CaseError("wfl_007_activity_projection_shape_invalid", assertion_failure=True)
    total_micros = costs.get("total_cost_user_currency_micros")
    symbol = currency.get("report_symbol")
    if isinstance(total_micros, bool) or not isinstance(total_micros, int) or not isinstance(symbol, str):
        raise CaseError("wfl_007_activity_projection_shape_invalid", assertion_failure=True)
    expected = {
        "usage_event_id": str(database_projection.get("usage_event_id")),
        "ingress_request_id": database_projection.get("ingress_request_id"),
        "model_id": database_projection.get("model_id"),
        "model_label": model_label,
        "endpoint_id": database_projection.get("endpoint_id"),
        "endpoint_label": database_projection.get("endpoint_label"),
        "status_code": database_projection.get("status_code"),
        "total_tokens": tokens.get("total_tokens"),
        "known_cost_micros": str(total_micros),
        "activity_cost": format_wfl_007_money(total_micros, symbol, maximum_fraction_digits=4),
        "report_currency_code": currency.get("report_code"),
        "report_currency_symbol": symbol,
        "pricing_status": database_projection.get("pricing_status"),
    }
    workflow.assert_safe_json(expected, "wfl_007_activity_expected")
    browser.goto("/observe?tab=activity&preset=24h")
    result = browser.run_code(
        step,
        action_for_origin(browser.spec.frontend_origin, ui_body(r"""
const expected = %s;
const activityResponsePromise = responseFor('GET', /^\/api\/stats\/observe-activity$/);
await page.reload();
const activityResponse = await activityResponsePromise;
if (activityResponse.status() !== 200) throw new Error('activity_response_status');
const payload = await activityResponse.json();
if (!payload || !Array.isArray(payload.items)) throw new Error('activity_response_shape');
const apiMatches = payload.items.filter((item) => String(item.usage_event_id) === expected.usage_event_id);
if (apiMatches.length !== 1) throw new Error('activity_usage_event_identity');
const apiItem = apiMatches[0];
const apiProjection = {
  usage_event_id: String(apiItem.usage_event_id),
  ingress_request_id: apiItem.final_ingress_request_id,
  model_id: apiItem.model_id,
  model_label: apiItem.model_label,
  endpoint_id: apiItem.endpoint_id,
  endpoint_label: apiItem.endpoint_label,
  status_code: apiItem.status_code,
  final_result: apiItem.final_result,
  total_tokens: apiItem.total_tokens,
  known_cost_micros: String(apiItem.known_cost_micros),
  report_currency_code: apiItem.report_currency_code,
  report_currency_symbol: apiItem.report_currency_symbol,
  pricing_status: apiItem.final_pricing_status,
};
const expectedApi = {
  usage_event_id: expected.usage_event_id,
  ingress_request_id: expected.ingress_request_id,
  model_id: expected.model_id,
  model_label: expected.model_label,
  endpoint_id: expected.endpoint_id,
  endpoint_label: expected.endpoint_label,
  status_code: expected.status_code,
  final_result: 'completed',
  total_tokens: expected.total_tokens,
  known_cost_micros: expected.known_cost_micros,
  report_currency_code: expected.report_currency_code,
  report_currency_symbol: expected.report_currency_symbol,
  pricing_status: expected.pricing_status,
};
if (JSON.stringify(apiProjection) !== JSON.stringify(expectedApi)) throw new Error('activity_api_semantic_mismatch');
const table = await expectVisible(page.getByTestId('observe-activity-table'), 'activity_table_missing');
const rows = table.getByTestId('activity-row');
const uiMatches = [];
for (let index = 0; index < await rows.count(); index += 1) {
  const cells = rows.nth(index).getByRole('cell');
  if (await cells.count() !== 9) throw new Error('activity_cell_count');
  const values = [];
  for (let cell = 0; cell < 9; cell += 1) values.push((await cells.nth(cell).innerText()).trim());
  if (
    values[1] === expected.model_label
    && values[2] === String(expected.status_code)
    && values[3] === expected.endpoint_label
    && values[5] === String(expected.total_tokens)
    && values[6] === expected.activity_cost
    && /已计价|Priced/i.test(values[7])
  ) {
    uiMatches.push({model_label: values[1], status: values[2], endpoint_label: values[3], total_tokens: values[5], cost: values[6], pricing_status_label: values[7]});
  }
}
if (uiMatches.length !== 1) throw new Error('activity_ui_semantic_mismatch');
return {surface: 'observe_activity', api: apiProjection, ui: uiMatches[0], exact_database_identity: true};
""" % json.dumps(expected))),
        timeout=120,
    )
    api_projection = result.get("api")
    ui_projection = result.get("ui")
    if (
        not isinstance(api_projection, dict)
        or api_projection.get("usage_event_id") != expected["usage_event_id"]
        or not isinstance(ui_projection, dict)
        or ui_projection.get("endpoint_label") != expected["endpoint_label"]
        or ui_projection.get("total_tokens") != str(expected["total_tokens"])
        or ui_projection.get("cost") != expected["activity_cost"]
        or result.get("exact_database_identity") is not True
    ):
        raise CaseError("wfl_007_activity_ui_mismatch", assertion_failure=True)
    return result


def run_wfl_007(browser: BrowserCase, client: support.LocalHTTP, database: support.LocalPostgres) -> None:
    origin = browser.spec.frontend_origin
    fixture = create_runtime_fixture(
        browser,
        client,
        prefix="matrix-wfl-007",
        with_pricing=True,
    )
    pricing_id = safe_id(fixture.get("pricing_id"))
    before_settings = costing_projection(client)
    target_code, target_symbol = (
        ("EUR", "€")
        if before_settings["report_currency_code"] != "EUR"
        else ("USD", "$")
    )

    browser.start_trace()
    browser.goto("/route/pricing")
    browser.run_code(
        "pricing_row_verify",
        action_for_origin(origin, ui_body("""
await page.getByTestId('pricing-feature-page').waitFor({state: 'visible', timeout: 15000});
const row = await expectVisible(page.getByTestId(%s), 'pricing_row_missing');
const text = await row.innerText();
if (!text.includes('matrix-wfl-007-pricing')) throw new Error('pricing_name_missing');
return {surface: 'pricing', pricing_id: %d, row_visible: true};
""" % (json.dumps("pricing-template-row-%d" % pricing_id), pricing_id))),
    )
    pricing_snapshot = browser.capture_snapshot("pricing-baseline")
    browser.write_text("pricing.snapshot.txt", (browser.case_dir / pricing_snapshot["path"]).read_text(encoding="utf-8"))
    browser.checkpoint("pricing_verified")

    baseline_caller = "matrix-wfl-007-baseline"
    baseline_http = client.request(
        "POST",
        "/v1/responses",
        body={"model": fixture["runtime_model"], "input": "deterministic pricing probe", "stream": False},
        headers={"X-Request-ID": baseline_caller, "X-Prism-Mock-Request-ID": baseline_caller},
    )
    if baseline_http.status != 200:
        raise CaseError("wfl_007_pricing_probe_failed", assertion_failure=True)
    baseline_usage = wait_usage_projection(database, baseline_caller)
    baseline_request = wait_request_projection(database, baseline_caller)
    baseline_request_id = safe_id(baseline_request.get("request_log_id"))
    baseline_database = wfl_007_database_numeric_projection(
        baseline_usage,
        before_settings,
        fixture,
        pricing_id,
    )
    baseline_detail_ui = wfl_007_request_detail_ui(
        browser,
        request_log_id=baseline_request_id,
        database_projection=baseline_database,
        step="pricing_request_detail_numeric",
    )
    usage_snapshot = browser.capture_snapshot("pricing-usage-detail")
    browser.write_text("usage-detail.snapshot.txt", (browser.case_dir / usage_snapshot["path"]).read_text(encoding="utf-8"))
    browser.checkpoint("usage_verified")
    baseline_activity_ui = wfl_007_activity_ui(
        browser,
        database_projection=baseline_database,
        model_label="matrix-wfl-007 model",
        step="baseline_activity_numeric",
    )

    components = [
        {"component": "input", "observed": 19, "unit_price": "1", "calculated_micros": 19, "stored_micros": baseline_usage.get("input_cost_micros")},
        {"component": "output", "observed": 23, "unit_price": "2", "calculated_micros": 46, "stored_micros": baseline_usage.get("output_cost_micros")},
        {"component": "cache_read_input", "observed": 5, "unit_price": "0.5", "calculated_micros": 3, "stored_micros": baseline_usage.get("cache_read_input_cost_micros")},
        {"component": "cache_creation_input", "observed": 0, "unit_price": "0.75", "calculated_micros": 0, "stored_micros": baseline_usage.get("cache_creation_input_cost_micros")},
        {"component": "reasoning", "observed": 7, "unit_price": "3", "calculated_micros": 21, "stored_micros": baseline_usage.get("reasoning_cost_micros")},
    ]
    calculated_total = sum(int(item["calculated_micros"]) for item in components)
    if (
        any(item["calculated_micros"] != item["stored_micros"] for item in components)
        or calculated_total != 89
        or baseline_usage.get("total_cost_original_micros") != calculated_total
        or baseline_usage.get("total_cost_user_currency_micros") != calculated_total
        or baseline_usage.get("fx_rate_used") != "1"
        or baseline_usage.get("fx_rate_source") != "DEFAULT_1_TO_1"
    ):
        raise CaseError("wfl_007_cost_recalculation_mismatch", assertion_failure=True)
    browser.checkpoint("cost_recalculated")
    if (
        baseline_usage.get("endpoint_label_snapshot") != fixture["endpoint_label"]
        or baseline_usage.get("pricing_template_id_used") != pricing_id
        or baseline_usage.get("pricing_template_name_snapshot") != "matrix-wfl-007-pricing"
    ):
        raise CaseError("wfl_007_snapshot_identity_mismatch", assertion_failure=True)
    browser.checkpoint("endpoint_label_snapshot_verified")
    browser.write_json(
        "cost-calculation.json",
        {
            "caller_id": baseline_caller,
            "pricing_id": pricing_id,
            "database": baseline_database,
            "request_detail_ui": baseline_detail_ui,
            "activity_ui": baseline_activity_ui,
            "components": components,
            "calculated_total_micros": calculated_total,
            "stored_total_micros": baseline_usage.get("total_cost_user_currency_micros"),
            "pricing_statuses": baseline_usage.get("pricing_statuses"),
            "pricing_trust": baseline_usage.get("pricing_trust"),
            "endpoint_label_snapshot": baseline_usage.get("endpoint_label_snapshot"),
            "passed": True,
        },
    )

    browser.goto("/system/settings?scope=global&section=billing-currency#billing-currency")
    migration = run_currency_migration(
        browser,
        target_code=target_code,
        target_symbol=target_symbol,
        step="currency_change",
    )
    browser.checkpoint("currency_changed")
    changed_settings = costing_projection(client)
    if (
        changed_settings["report_currency_code"] != target_code
        or changed_settings["report_currency_symbol"] != target_symbol
        or changed_settings["reporting_currency_epoch"] != before_settings["reporting_currency_epoch"] + 1
    ):
        raise CaseError("wfl_007_currency_change_mismatch", assertion_failure=True)

    changed_caller = "matrix-wfl-007-changed"
    changed_http = client.request(
        "POST",
        "/v1/responses",
        body={"model": fixture["runtime_model"], "input": "post-migration pricing probe", "stream": False},
        headers={"X-Request-ID": changed_caller, "X-Prism-Mock-Request-ID": changed_caller},
    )
    if changed_http.status != 200:
        raise CaseError("wfl_007_changed_currency_probe_failed", assertion_failure=True)
    changed_usage = wait_usage_projection(database, changed_caller)
    changed_database = wfl_007_database_numeric_projection(
        changed_usage,
        changed_settings,
        fixture,
        pricing_id,
    )
    if (
        changed_database.get("pricing_template_revision_id") == baseline_database.get("pricing_template_revision_id")
        or changed_database.get("costs_micros", {}).get("total_cost_user_currency_micros") != calculated_total
    ):
        raise CaseError("wfl_007_currency_refresh_mismatch", assertion_failure=True)
    changed_request = wait_request_projection(database, changed_caller)
    changed_request_id = safe_id(changed_request.get("request_log_id"))
    changed_detail_ui = wfl_007_request_detail_ui(
        browser,
        request_log_id=changed_request_id,
        database_projection=changed_database,
        step="changed_request_detail_numeric",
    )
    changed_activity_ui = wfl_007_activity_ui(
        browser,
        database_projection=changed_database,
        model_label="matrix-wfl-007 model",
        step="changed_activity_numeric",
    )
    historical = wait_usage_projection(database, baseline_caller)
    historical_database = wfl_007_database_numeric_projection(
        historical,
        before_settings,
        fixture,
        pricing_id,
    )
    if (
        historical_database != baseline_database
    ):
        raise CaseError("wfl_007_historical_snapshot_changed", assertion_failure=True)
    browser.goto("/route/pricing")
    browser.run_code(
        "pricing_currency_refresh",
        action_for_origin(origin, ui_body("""
await page.getByTestId('pricing-feature-page').waitFor({state: 'visible', timeout: 15000});
const row = await expectVisible(page.getByTestId(%s), 'pricing_row_missing');
if (!(await row.innerText()).includes(%s)) throw new Error('pricing_currency_not_refreshed');
return {surface: 'pricing', currency_code: %s, refreshed: true};
""" % (json.dumps("pricing-template-row-%d" % pricing_id), json.dumps(target_code), json.dumps(target_code)))),
    )
    changed_pricing_snapshot = browser.capture_snapshot("pricing-after-currency-change")
    browser.checkpoint("currency_refresh_verified")

    browser.goto("/system/settings?scope=global&section=billing-currency#billing-currency")
    restoration = run_currency_migration(
        browser,
        target_code=str(before_settings["report_currency_code"]),
        target_symbol=str(before_settings["report_currency_symbol"]),
        step="currency_restore",
    )
    restored_settings = costing_projection(client)
    if (
        restored_settings["report_currency_code"] != before_settings["report_currency_code"]
        or restored_settings["report_currency_symbol"] != before_settings["report_currency_symbol"]
        or restored_settings["reporting_currency_epoch"] != before_settings["reporting_currency_epoch"] + 2
    ):
        raise CaseError("wfl_007_currency_restore_mismatch", assertion_failure=True)
    browser.checkpoint("currency_restored")
    browser.write_json(
        "currency-before-after.json",
        {
            "before": before_settings,
            "changed": changed_settings,
            "restored": restored_settings,
            "migration": migration,
            "restoration": restoration,
            "baseline_database": historical_database,
            "changed_database": changed_database,
            "baseline_request_detail_ui": baseline_detail_ui,
            "baseline_activity_ui": baseline_activity_ui,
            "changed_request_detail_ui": changed_detail_ui,
            "changed_activity_ui": changed_activity_ui,
            "refresh_snapshot": changed_pricing_snapshot,
            "historical_identity_preserved": True,
            "numeric_price_relationship_preserved": changed_usage.get("total_cost_user_currency_micros") == calculated_total,
            "passed": True,
        },
    )


def caller_row_count(database: support.LocalPostgres, caller_id: str) -> int:
    if not SAFE_IDENTIFIER_RE.fullmatch(caller_id):
        raise CaseError("workflow_caller_id_invalid")
    value = database.read_json(
        "SELECT json_build_object('rows', count(*)) FROM request_logs "
        "WHERE caller_request_id = %s;" % support.sql_literal(caller_id)
    )
    rows = value.get("rows")
    if isinstance(rows, bool) or not isinstance(rows, int) or rows < 0:
        raise CaseError("workflow_request_count_shape_invalid")
    return rows


NAMED_PAGE_HELPERS = r"""
const findNamedPage = async (context, wanted) => {
  for (const candidate of context.pages()) {
    const actual = await candidate.evaluate(() => window.name).catch(() => '');
    if (actual === wanted) return candidate;
  }
  throw new Error('named_page_missing_' + wanted);
};
const primary = await findNamedPage(page.context(), 'wfl008-primary');
const secondary = await findNamedPage(page.context(), 'wfl008-secondary');
await primary.bringToFront();
"""


def run_wfl_008(browser: BrowserCase, client: support.LocalHTTP, database: support.LocalPostgres) -> None:
    """Exercise the complete auth/session/proxy-key lifecycle in two real pages."""
    origin = browser.spec.frontend_origin
    operator_name = "matrix-wfl-008-operator"
    key_name = "matrix-wfl-008-proxy"
    old_caller = "matrix-wfl-008-old"
    new_caller = "matrix-wfl-008-new"
    revoked_caller = "matrix-wfl-008-revoked"
    passphrase_index = int(browser.state["private_value_indexes"]["operator_password"])

    initial_status = api_object(client, "/api/auth/status")
    if initial_status.get("state") != "disabled" or initial_status.get("login_available") is not False:
        raise CaseError("wfl_008_initial_auth_state_not_disabled", assertion_failure=True)
    fixture = create_runtime_fixture(browser, client, prefix="matrix-wfl-008")

    browser.goto("/system/proxy-keys")
    key_setup = browser.run_code(
        "auth_key_setup",
        action_for_origin(origin, ui_body(r"""
await page.getByTestId('proxy-keys-feature-page').waitFor({state: 'visible', timeout: 20000});
await page.evaluate(() => { window.name = 'wfl008-primary'; });
await clickButton(page, /发放密钥|Issue key/i, 'proxy_issue_button_missing');
const sheet = await expectVisible(page.getByTestId('proxy-key-issue-sheet'), 'proxy_issue_sheet_missing');
await fillLabel(sheet, /^名称$|^Name$/i, %s, 'proxy_name_missing');
const createPromise = responseFor('POST', /^\/api\/settings\/auth\/proxy-keys$/);
await clickButton(sheet, /创建密钥|Create key/i, 'proxy_create_button_missing');
const created = await createPromise;
if (created.status() !== 201) throw new Error('proxy_create_status');
const createdPayload = await created.json();
if (!createdPayload || typeof createdPayload.key !== 'string' || !/^pm-[0-9a-f]{32}$/i.test(createdPayload.key)) {
  throw new Error('proxy_create_value_shape');
}
const keyId = createdPayload.item && createdPayload.item.id;
if (!Number.isInteger(keyId) || keyId <= 0) throw new Error('proxy_create_id_shape');
await page.evaluate(({raw, id}) => {
  Object.defineProperty(window, '__prismWflOldKey', {value: raw, configurable: true, writable: true, enumerable: false});
  Object.defineProperty(window, '__prismWflKeyId', {value: id, configurable: true, writable: false, enumerable: false});
}, {raw: createdPayload.key, id: keyId});
const secret = await expectVisible(page.getByTestId('proxy-key-secret'), 'proxy_secret_dialog_missing');
const saved = await expectVisible(page.locator('#proxy-key-saved-ack'), 'proxy_saved_ack_missing');
await saved.check();
await clickButton(page, /完成并关闭|Finish and close/i, 'proxy_secret_finish_missing');
await secret.waitFor({state: 'hidden', timeout: 15000});
const secondary = await page.context().newPage();
await secondary.goto(%s + '/observe', {waitUntil: 'domcontentloaded'});
await secondary.evaluate(() => { window.name = 'wfl008-secondary'; });
await expectVisible(secondary.getByTestId('observe-page'), 'secondary_open_shell_missing');
await page.bringToFront();
return {surface: 'proxy_keys', key_id: keyId, create_status: created.status(), page_count: page.context().pages().length, one_time_ui_closed: true};
""" % (json.dumps(key_name), json.dumps(origin)))),
    )
    key_id = safe_id(key_setup.get("key_id"))
    if key_setup.get("page_count") != 2 or key_setup.get("one_time_ui_closed") is not True:
        raise CaseError("wfl_008_two_page_setup_failed", assertion_failure=True)

    auth_enable = browser.run_code(
        "auth_enable",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
const storageReceiptKey = 'prism.wfl008.enableReceipt';
const settingsLink = await expectVisible(primary.locator('a[href*="/system/settings"]').first(), 'settings_link_missing');
await settingsLink.click();
await primary.waitForURL(/\/system\/settings/, {timeout: 20000});
const authenticationLink = primary.locator('a[href*="section=authentication"]').first();
if (await authenticationLink.count()) await authenticationLink.click();
const section = await expectVisible(primary.locator('#authentication'), 'authentication_section_missing');
await section.scrollIntoViewIfNeeded();
const accountStorageBaseline = await secondary.evaluate(() => {
  try {
    const payload = JSON.parse(localStorage.getItem('prism.authStateVersion') || 'null');
    return Number.isInteger(payload?.sequence) ? payload.sequence : 0;
  } catch { return 0; }
});
await primary.locator('#auth-username').fill(%s);
await fillPrivateLocator(primary.locator('#auth-password'), 'PRISM_WFL_PRIVATE_OPERATOR', 'operator_password_missing');
await fillPrivateLocator(primary.locator('#auth-password-confirm'), 'PRISM_WFL_PRIVATE_OPERATOR', 'operator_password_confirm_missing');
const accountPromise = primary.waitForResponse((response) => response.request().method() === 'PUT' && urlPath(response.url()) === '/api/settings/auth', {timeout: 30000});
await clickButton(section, /保存账户更改|Save account changes/i, 'account_save_missing');
const accountResponse = await accountPromise;
if (accountResponse.status() !== 200) throw new Error('account_save_status');
await primary.locator('#auth-password').waitFor({state: 'visible'});
if (await primary.locator('#auth-password').inputValue()) throw new Error('account_passphrase_not_cleared');
// Prove the account-save event reached the secondary tab before arming the
// enable listener, so a delayed earlier event cannot satisfy the receipt.
await secondary.waitForFunction((baseline) => {
  try {
    const payload = JSON.parse(localStorage.getItem('prism.authStateVersion') || 'null');
    return Number.isInteger(payload?.sequence) && payload.sequence > baseline;
  } catch { return false; }
}, accountStorageBaseline, {timeout: 12000}).catch(() => { throw new Error('account_storage_event_missing'); });
const storageBaseline = await secondary.evaluate(() => {
  try {
    const payload = JSON.parse(localStorage.getItem('prism.authStateVersion') || 'null');
    return Number.isInteger(payload?.sequence) ? payload.sequence : 0;
  } catch { return 0; }
});
await secondary.evaluate(({receiptKey, baseline}) => {
  sessionStorage.removeItem(receiptKey);
  const listener = (event) => {
    if (event.key !== 'prism.authStateVersion' || typeof event.newValue !== 'string') return;
    try {
      const payload = JSON.parse(event.newValue);
      if (!Number.isInteger(payload?.sequence) || payload.sequence <= baseline) return;
      sessionStorage.setItem(receiptKey, JSON.stringify({
        key: event.key,
        kind: payload?.kind ?? null,
        event_id: payload?.event_id ?? null,
        origin_tab_id: payload?.origin_tab_id ?? null,
        sequence: payload.sequence,
        session_generation_id: payload?.session_generation_id ?? null,
        target_generation: payload?.target_generation ?? null,
      }));
      window.removeEventListener('storage', listener);
    } catch {}
  };
  window.addEventListener('storage', listener);
}, {receiptKey: storageReceiptKey, baseline: storageBaseline});
const toggle = await expectVisible(section.getByRole('switch').first(), 'authentication_switch_missing');
const enablePromise = primary.waitForResponse((response) => response.request().method() === 'PUT' && urlPath(response.url()) === '/api/settings/auth', {timeout: 30000});
await toggle.click();
const confirmation = primary.getByRole('alertdialog');
if (await confirmation.isVisible().catch(() => false)) {
  await clickButton(confirmation, /继续|Continue/i, 'auth_enable_confirmation_missing');
}
const enableResponse = await enablePromise;
if (enableResponse.status() !== 200) throw new Error('auth_enable_status');
const enablePayload = await enableResponse.json();
if (!enablePayload || enablePayload.effect_state !== 'effective' || enablePayload.settings?.auth_mode?.effective !== 'enabled') {
  throw new Error('auth_enable_not_effective');
}
await primary.waitForURL(/\/auth\/login(?:\?.*)?$/, {timeout: 30000});
let secondaryRedirected = true;
await secondary.waitForURL(/\/auth\/login(?:\?.*)?$/, {timeout: 12000}).catch(() => { secondaryRedirected = false; });
if (!secondaryRedirected) throw new Error('secondary_active_redirect_missing');
await secondary.waitForFunction((receiptKey) => sessionStorage.getItem(receiptKey) !== null, storageReceiptKey, {timeout: 12000});
const storageEvent = await secondary.evaluate((receiptKey) => {
  const value = JSON.parse(sessionStorage.getItem(receiptKey) || 'null');
  sessionStorage.removeItem(receiptKey);
  return value;
}, storageReceiptKey);
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
if (!storageEvent || storageEvent.key !== 'prism.authStateVersion' || storageEvent.kind !== 'auth_changed'
    || !uuidPattern.test(storageEvent.event_id || '') || !Number.isInteger(storageEvent.sequence)
    || storageEvent.sequence <= storageBaseline || typeof storageEvent.session_generation_id !== 'string'
    || !storageEvent.session_generation_id || typeof storageEvent.target_generation !== 'string'
    || !storageEvent.target_generation) throw new Error('secondary_auth_storage_event_invalid');
const oldValuePresent = await primary.evaluate(() => typeof window.__prismWflOldKey === 'string');
if (!oldValuePresent) throw new Error('old_value_lost_during_spa_transition');
return {
  surface: 'authentication',
  account_status: accountResponse.status(),
  enable_status: enableResponse.status(),
  effect_state: enablePayload.effect_state,
  session_action: enablePayload.session_action,
  primary_path: urlPath(primary.url()),
  secondary_path: urlPath(secondary.url()),
  secondary_redirected,
  storage_event: storageEvent,
  account_storage_event_observed: true,
  account_storage_baseline_sequence: accountStorageBaseline,
  storage_baseline_sequence: storageBaseline,
  storage_receipt_removed: await secondary.evaluate((receiptKey) => sessionStorage.getItem(receiptKey) === null, storageReceiptKey),
  fields_cleared: true,
};
""" % json.dumps(operator_name))),
        private_environment={"PRISM_WFL_PRIVATE_OPERATOR": passphrase_index},
        timeout=180,
    )
    enable_storage_event = retention.as_dict(auth_enable.get("storage_event"))
    if (
        auth_enable.get("secondary_redirected") is not True
        or auth_enable.get("storage_receipt_removed") is not True
        or auth_enable.get("account_storage_event_observed") is not True
        or enable_storage_event.get("kind") != "auth_changed"
        or enable_storage_event.get("key") != "prism.authStateVersion"
        or not isinstance(enable_storage_event.get("sequence"), int)
        or int(enable_storage_event.get("sequence") or 0) <= 0
        or not isinstance(auth_enable.get("account_storage_baseline_sequence"), int)
        or not isinstance(auth_enable.get("storage_baseline_sequence"), int)
        or int(auth_enable.get("storage_baseline_sequence") or 0)
        <= int(auth_enable.get("account_storage_baseline_sequence") or 0)
        or int(enable_storage_event.get("sequence") or 0)
        <= int(auth_enable.get("storage_baseline_sequence") or 0)
    ):
        raise CaseError("wfl_008_enable_cross_tab_event_mismatch", assertion_failure=True)
    browser.checkpoint("auth_enabled")
    browser.checkpoint("unauthenticated_redirects_verified")
    redirect_snapshot = browser.capture_snapshot("auth-two-tabs-redirected")

    login = browser.run_code(
        "multi_tab_login",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
await primary.getByLabel(/用户名|Username/i).fill(%s);
await fillPrivateLocator(primary.getByLabel(/密码|Password/i, {exact: true}), 'PRISM_WFL_PRIVATE_OPERATOR', 'operator_password_missing');
const loginPromise = primary.waitForResponse((response) => response.request().method() === 'POST' && urlPath(response.url()) === '/api/auth/login', {timeout: 30000});
await clickButton(primary, /^登录$|^Sign in$/i, 'login_button_missing');
const loginResponse = await loginPromise;
if (loginResponse.status() !== 200) throw new Error('login_status');
await expectVisible(primary.getByTestId('observe-page'), 'primary_authenticated_shell_missing');
await secondary.goto(%s + '/observe', {waitUntil: 'domcontentloaded'});
await expectVisible(secondary.getByTestId('observe-page'), 'secondary_authenticated_shell_missing');
return {
  surface: 'auth_login',
  login_status: loginResponse.status(),
  primary_path: urlPath(primary.url()),
  secondary_path: urlPath(secondary.url()),
  shared_context_authenticated: true,
};
""" % (json.dumps(operator_name), json.dumps(origin)))),
        private_environment={"PRISM_WFL_PRIVATE_OPERATOR": passphrase_index},
        timeout=180,
    )
    browser.checkpoint("multi_tab_login_verified")
    primary_login_snapshot = browser.capture_snapshot("auth-primary-logged-in")

    refresh_sync = browser.run_code(
        "refresh_cross_tab_sync",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
await secondary.reload({waitUntil: 'domcontentloaded'});
await expectVisible(secondary.getByTestId('observe-page'), 'secondary_refresh_shell_missing');
await expectVisible(primary.getByTestId('observe-page'), 'primary_sync_shell_missing');
const oldValuePresent = await primary.evaluate(() => typeof window.__prismWflOldKey === 'string');
if (!oldValuePresent) throw new Error('old_value_lost_before_rotation');
await secondary.bringToFront();
return {
  surface: 'auth_multi_tab',
  page_count: page.context().pages().length,
  secondary_refreshed: true,
  primary_authenticated: true,
  secondary_authenticated: true,
};
""")),
    )
    browser.checkpoint("refresh_and_cross_tab_sync_verified")
    secondary_login_snapshot = browser.capture_snapshot("auth-secondary-after-refresh")

    rotation = browser.run_code(
        "proxy_key_rotate",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
await primary.bringToFront();
const proxyLink = await expectVisible(primary.locator('a[href="/system/proxy-keys"]').first(), 'proxy_keys_link_missing');
await proxyLink.click();
await primary.waitForURL(/\/system\/proxy-keys$/, {timeout: 20000});
const ledger = await expectVisible(primary.getByTestId('proxy-key-ledger'), 'proxy_ledger_missing');
const row = await expectVisible(ledger.getByRole('row').filter({hasText: %s}), 'proxy_key_row_missing');
await clickButton(row, /更多操作|More actions/i, 'proxy_more_actions_missing');
const rotateItem = await expectVisible(primary.getByRole('menuitem', {name: /轮换密钥|Rotate key/i}), 'proxy_rotate_item_missing');
await rotateItem.click();
const rotatePromise = primary.waitForResponse((response) => response.request().method() === 'POST' && /\/api\/settings\/auth\/proxy-keys\/[0-9]+\/rotate$/.test(urlPath(response.url())), {timeout: 30000});
await primary.getByTestId('proxy-key-rotate-confirm').click();
const rotateResponse = await rotatePromise;
if (rotateResponse.status() !== 200) throw new Error('proxy_rotate_status');
const rotatedPayload = await rotateResponse.json();
if (!rotatedPayload || typeof rotatedPayload.key !== 'string' || !/^pm-[0-9a-f]{32}$/i.test(rotatedPayload.key)) {
  throw new Error('proxy_rotated_value_shape');
}
const oldValue = await primary.evaluate(() => window.__prismWflOldKey);
if (typeof oldValue !== 'string' || oldValue === rotatedPayload.key) throw new Error('proxy_rotation_not_distinct');
if (rotatedPayload.item?.id !== %d || rotatedPayload.item?.rotation_count !== 1) throw new Error('proxy_rotation_identity');
await primary.evaluate((raw) => {
  Object.defineProperty(window, '__prismWflNewKey', {value: raw, configurable: true, writable: true, enumerable: false});
}, rotatedPayload.key);
const secret = await expectVisible(primary.getByTestId('proxy-key-secret'), 'proxy_rotated_secret_missing');
await primary.locator('#proxy-key-saved-ack').check();
await clickButton(primary, /完成并关闭|Finish and close/i, 'proxy_rotated_secret_finish_missing');
await secret.waitFor({state: 'hidden', timeout: 15000});
return {
  surface: 'proxy_keys',
  key_id: rotatedPayload.item.id,
  rotate_status: rotateResponse.status(),
  rotation_count: rotatedPayload.item.rotation_count,
  one_time_ui_closed: true,
};
""" % (json.dumps(key_name), key_id))),
        timeout=180,
    )
    if rotation.get("key_id") != key_id or rotation.get("rotation_count") != 1:
        raise CaseError("wfl_008_rotation_projection_mismatch", assertion_failure=True)
    browser.checkpoint("proxy_key_rotated")

    key_probe = browser.run_code(
        "proxy_key_old_new_probe",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
const values = await primary.evaluate(() => ({oldValue: window.__prismWflOldKey, newValue: window.__prismWflNewKey}));
if (typeof values.oldValue !== 'string' || typeof values.newValue !== 'string') throw new Error('proxy_values_missing');
const probe = async (value, caller) => {
  const response = await primary.evaluate(async ({value, caller, model}) => {
    const result = await fetch('/v1/responses', {
      method: 'POST',
      credentials: 'include',
      headers: {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + value, 'X-Request-ID': caller, 'X-Prism-Mock-Request-ID': caller},
      body: JSON.stringify({model, input: 'local WFL-008 authentication probe', stream: false}),
    });
    const bytes = (await result.arrayBuffer()).byteLength;
    return {status: result.status, bytes};
  }, {value, caller, model: %s});
  return response;
};
const oldResult = await probe(values.oldValue, %s);
const newResult = await probe(values.newValue, %s);
if (oldResult.status !== 401 || newResult.status !== 200) throw new Error('proxy_rotation_runtime_status');
return {
  surface: 'runtime_auth',
  old_value_status: oldResult.status,
  old_value_bytes: oldResult.bytes,
  new_value_status: newResult.status,
  new_value_bytes: newResult.bytes,
};
""" % tuple(json.dumps(value) for value in (fixture["runtime_model"], old_caller, new_caller)))),
        timeout=180,
    )
    new_projection = wait_request_projection(database, new_caller)
    if (
        caller_row_count(database, old_caller) != 0
        or new_projection.get("status_codes") != [200]
        or new_projection.get("proxy_key_ids") != [key_id]
        or new_projection.get("attribution_states") != ["identified"]
    ):
        raise CaseError("wfl_008_rotation_attribution_mismatch", assertion_failure=True)
    browser.checkpoint("old_and_new_key_verified")

    revoke = browser.run_code(
        "proxy_key_revoke",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
const ledger = await expectVisible(primary.getByTestId('proxy-key-ledger'), 'proxy_ledger_missing');
const row = await expectVisible(ledger.getByRole('row').filter({hasText: %s}), 'proxy_key_row_missing');
await clickButton(row, /更多操作|More actions/i, 'proxy_more_actions_missing');
const deleteItem = await expectVisible(primary.getByRole('menuitem', {name: /删除密钥|Delete key/i}), 'proxy_delete_item_missing');
await deleteItem.click();
const deletePromise = primary.waitForResponse((response) => response.request().method() === 'DELETE' && /\/api\/settings\/auth\/proxy-keys\/[0-9]+$/.test(urlPath(response.url())), {timeout: 30000});
const dialog = await expectVisible(primary.getByRole('alertdialog'), 'proxy_delete_dialog_missing');
await clickButton(dialog, /删除密钥|Delete key/i, 'proxy_delete_confirm_missing');
const deleteResponse = await deletePromise;
if (deleteResponse.status() !== 200) throw new Error('proxy_delete_status');
await row.waitFor({state: 'hidden', timeout: 20000});
return {surface: 'proxy_keys', key_id: %d, delete_status: deleteResponse.status(), row_removed: true};
""" % (json.dumps(key_name), key_id))),
        timeout=180,
    )
    browser.checkpoint("proxy_key_revoked")

    revoked_probe = browser.run_code(
        "proxy_key_revoked_probe",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
const newValue = await primary.evaluate(() => window.__prismWflNewKey);
if (typeof newValue !== 'string') throw new Error('proxy_new_value_missing');
const result = await primary.evaluate(async ({value, caller, model}) => {
  const response = await fetch('/v1/responses', {
    method: 'POST',
    credentials: 'include',
    headers: {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + value, 'X-Request-ID': caller, 'X-Prism-Mock-Request-ID': caller},
    body: JSON.stringify({model, input: 'local WFL-008 revoked probe', stream: false}),
  });
  return {status: response.status, bytes: (await response.arrayBuffer()).byteLength};
}, {value: newValue, caller: %s, model: %s});
if (result.status !== 401) throw new Error('proxy_revoked_runtime_status');
return {surface: 'runtime_auth', revoked_value_status: result.status, revoked_value_bytes: result.bytes};
""" % (json.dumps(revoked_caller), json.dumps(fixture["runtime_model"])))),
        timeout=180,
    )
    if caller_row_count(database, revoked_caller) != 0:
        raise CaseError("wfl_008_revoked_request_reached_runtime", assertion_failure=True)
    browser.checkpoint("revoked_key_verified")

    logout_audit = browser.run_code(
        "logout_storage_audit",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
const before = await primary.evaluate((binding) => {
  const passphrase = window.__prismWflPrivate?.[binding];
  if (typeof passphrase !== 'string' || passphrase.length === 0) throw new Error('operator_private_value_missing');
  const oldValue = window.__prismWflOldKey;
  const newValue = window.__prismWflNewKey;
  const sensitive = [passphrase, oldValue, newValue].filter((item) => typeof item === 'string');
  const local = Object.entries(localStorage);
  const session = Object.entries(sessionStorage);
  return {
    local_names: local.map(([name]) => name).sort(),
    session_names: session.map(([name]) => name).sort(),
    sensitive_absent: [...local, ...session].every(([, value]) => sensitive.every((item) => !value.includes(item))),
  };
}, 'PRISM_WFL_PRIVATE_OPERATOR');
const httpBefore = (await page.context().cookies(%s)).length;
await clickButton(primary, new RegExp(%s, 'i'), 'operator_menu_missing');
const logoutItem = await expectVisible(primary.getByRole('menuitem', {name: /退出登录|Log out/i}), 'logout_item_missing');
const logoutPromise = primary.waitForResponse((response) => response.request().method() === 'POST' && urlPath(response.url()) === '/api/auth/logout', {timeout: 30000});
await logoutItem.click();
const logoutResponse = await logoutPromise;
if (logoutResponse.status() !== 204) throw new Error('logout_status');
await primary.waitForURL(/\/auth\/login(?:\?.*)?$/, {timeout: 20000});
await secondary.waitForURL(/\/auth\/login(?:\?.*)?$/, {timeout: 12000}).catch(async () => {
  await secondary.goto(%s + '/auth/login', {waitUntil: 'domcontentloaded'});
});
const after = await primary.evaluate((binding) => {
  const passphrase = window.__prismWflPrivate?.[binding];
  if (typeof passphrase !== 'string' || passphrase.length === 0) throw new Error('operator_private_value_missing');
  const oldValue = window.__prismWflOldKey;
  const newValue = window.__prismWflNewKey;
  const sensitive = [passphrase, oldValue, newValue].filter((item) => typeof item === 'string');
  const local = Object.entries(localStorage);
  const session = Object.entries(sessionStorage);
  const result = {
    local_names: local.map(([name]) => name).sort(),
    session_names: session.map(([name]) => name).sort(),
    sensitive_absent: [...local, ...session].every(([, value]) => sensitive.every((item) => !value.includes(item))),
  };
  delete window.__prismWflOldKey;
  delete window.__prismWflNewKey;
  delete window.__prismWflKeyId;
  return result;
}, 'PRISM_WFL_PRIVATE_OPERATOR');
const httpAfter = (await page.context().cookies(%s)).length;
if (!before.sensitive_absent || !after.sensitive_absent || httpAfter !== 0) throw new Error('logout_storage_not_cleared');
return {
  surface: 'auth_logout',
  logout_status: logoutResponse.status(),
  before: {...before, http_state_record_count: httpBefore},
  after: {...after, http_state_record_count: httpAfter},
  primary_path: urlPath(primary.url()),
  secondary_path: urlPath(secondary.url()),
  ephemeral_values_destroyed: true,
};
""" % (json.dumps(origin), json.dumps(operator_name), json.dumps(origin), json.dumps(origin)))),
        private_environment={"PRISM_WFL_PRIVATE_OPERATOR": passphrase_index},
        timeout=180,
    )
    browser.checkpoint("logged_out")
    browser.checkpoint("session_storage_cleared")
    logout_snapshot = browser.capture_snapshot("auth-after-logout")

    relogin = browser.run_code(
        "login_for_disable",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
await primary.getByLabel(/用户名|Username/i).fill(%s);
await fillPrivateLocator(primary.getByLabel(/密码|Password/i, {exact: true}), 'PRISM_WFL_PRIVATE_OPERATOR', 'operator_password_missing');
const loginPromise = primary.waitForResponse((response) => response.request().method() === 'POST' && urlPath(response.url()) === '/api/auth/login', {timeout: 30000});
await clickButton(primary, /^登录$|^Sign in$/i, 'login_button_missing');
const loginResponse = await loginPromise;
if (loginResponse.status() !== 200) throw new Error('login_for_disable_status');
await expectVisible(primary.getByTestId('observe-page'), 'disable_login_shell_missing');
const settingsLink = await expectVisible(primary.locator('a[href*="/system/settings"]').first(), 'settings_link_missing');
await settingsLink.click();
await primary.waitForURL(/\/system\/settings/, {timeout: 20000});
const authenticationLink = primary.locator('a[href*="section=authentication"]').first();
if (await authenticationLink.count()) await authenticationLink.click();
const section = await expectVisible(primary.locator('#authentication'), 'authentication_section_missing');
if (await primary.locator('#auth-password').inputValue()) throw new Error('disable_passphrase_field_not_clear');
return {surface: 'authentication', login_status: loginResponse.status(), sensitive_ui_cleared: true};
""" % json.dumps(operator_name))),
        private_environment={"PRISM_WFL_PRIVATE_OPERATOR": passphrase_index},
        timeout=180,
    )
    if relogin.get("sensitive_ui_cleared") is not True:
        raise CaseError("wfl_008_trace_boundary_not_clear", assertion_failure=True)
    browser.start_trace(sensitive_ui_cleared=True)

    disabled = browser.run_code(
        "auth_disable",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
const storageReceiptKey = 'prism.wfl008.disableReceipt';
const storageBaseline = await secondary.evaluate(() => {
  try {
    const payload = JSON.parse(localStorage.getItem('prism.authStateVersion') || 'null');
    return Number.isInteger(payload?.sequence) ? payload.sequence : 0;
  } catch { return 0; }
});
await secondary.evaluate(({receiptKey, baseline}) => {
  sessionStorage.removeItem(receiptKey);
  const listener = (event) => {
    if (event.key !== 'prism.authStateVersion' || typeof event.newValue !== 'string') return;
    try {
      const payload = JSON.parse(event.newValue);
      if (!Number.isInteger(payload?.sequence) || payload.sequence <= baseline) return;
      sessionStorage.setItem(receiptKey, JSON.stringify({
        key: event.key,
        kind: payload?.kind ?? null,
        event_id: payload?.event_id ?? null,
        origin_tab_id: payload?.origin_tab_id ?? null,
        sequence: payload?.sequence ?? null,
        session_generation_id: payload?.session_generation_id ?? null,
        target_generation: payload?.target_generation ?? null,
      }));
      window.removeEventListener('storage', listener);
    } catch {}
  };
  window.addEventListener('storage', listener);
}, {receiptKey: storageReceiptKey, baseline: storageBaseline});
const section = await expectVisible(primary.locator('#authentication'), 'authentication_section_missing');
const toggle = await expectVisible(section.getByRole('switch').first(), 'authentication_switch_missing');
const disablePromise = primary.waitForResponse((response) => response.request().method() === 'PUT' && urlPath(response.url()) === '/api/settings/auth', {timeout: 30000});
await toggle.click();
const dialog = await expectVisible(primary.getByRole('alertdialog'), 'auth_disable_confirmation_missing');
await clickButton(dialog, /继续|Continue/i, 'auth_disable_continue_missing');
const response = await disablePromise;
if (response.status() !== 200) throw new Error('auth_disable_status');
const payload = await response.json();
if (!payload || payload.effect_state !== 'effective' || payload.settings?.auth_mode?.effective !== 'disabled') {
  throw new Error('auth_disable_not_effective');
}
await secondary.waitForFunction((receiptKey) => sessionStorage.getItem(receiptKey) !== null, storageReceiptKey, {timeout: 12000});
const storageEvent = await secondary.evaluate((receiptKey) => {
  const value = JSON.parse(sessionStorage.getItem(receiptKey) || 'null');
  sessionStorage.removeItem(receiptKey);
  return value;
}, storageReceiptKey);
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
if (!storageEvent || storageEvent.key !== 'prism.authStateVersion' || storageEvent.kind !== 'auth_changed'
    || !uuidPattern.test(storageEvent.event_id || '') || !Number.isInteger(storageEvent.sequence)
    || storageEvent.sequence <= storageBaseline || typeof storageEvent.session_generation_id !== 'string'
    || !storageEvent.session_generation_id || typeof storageEvent.target_generation !== 'string'
    || !storageEvent.target_generation) throw new Error('secondary_disable_storage_event_invalid');
await expectVisible(secondary.getByText(/本实例未启用身份验证|Authentication is not enabled/i), 'secondary_active_open_access_missing');
await expectVisible(primary.getByTestId('shell-sidebar'), 'primary_active_open_shell_missing');
return {
  surface: 'authentication',
  disable_status: response.status(),
  effect_state: payload.effect_state,
  session_action: payload.session_action,
  effective_mode: payload.settings.auth_mode.effective,
  primary_path: urlPath(primary.url()),
  secondary_path: urlPath(secondary.url()),
  storage_event: storageEvent,
  storage_baseline_sequence: storageBaseline,
  storage_receipt_removed: await secondary.evaluate((receiptKey) => sessionStorage.getItem(receiptKey) === null, storageReceiptKey),
};
""")),
        timeout=180,
    )
    final_status = api_object(client, "/api/auth/status")
    disable_storage_event = retention.as_dict(disabled.get("storage_event"))
    if final_status.get("state") != "disabled" or final_status.get("login_available") is not False:
        raise CaseError("wfl_008_auth_disable_not_visible", assertion_failure=True)
    if (
        disabled.get("storage_receipt_removed") is not True
        or disable_storage_event.get("kind") != "auth_changed"
        or disable_storage_event.get("key") != "prism.authStateVersion"
        or not isinstance(disable_storage_event.get("sequence"), int)
        or not isinstance(disabled.get("storage_baseline_sequence"), int)
        or int(disable_storage_event.get("sequence") or 0)
        <= int(disabled.get("storage_baseline_sequence") or 0)
    ):
        raise CaseError("wfl_008_disable_cross_tab_event_mismatch", assertion_failure=True)
    browser.checkpoint("auth_disabled")

    open_shell = browser.run_code(
        "open_shell_restore",
        action_for_origin(origin, ui_body(NAMED_PAGE_HELPERS + r"""
await expectVisible(secondary.getByText(/本实例未启用身份验证|Authentication is not enabled/i), 'open_access_explainer_missing');
await clickButton(secondary, /进入控制台|Enter console/i, 'enter_console_missing');
await expectVisible(secondary.getByTestId('observe-page'), 'open_shell_missing');
await expectVisible(secondary.getByTestId('shell-sidebar'), 'secondary_shell_sidebar_missing');
await expectVisible(primary.getByTestId('shell-sidebar'), 'primary_shell_sidebar_missing');
const httpStateRecords = (await page.context().cookies(%s)).length;
if (httpStateRecords !== 0) throw new Error('open_shell_http_state_not_clear');
return {
  surface: 'open_shell',
  primary_path: urlPath(primary.url()),
  secondary_path: urlPath(secondary.url()),
  primary_open_shell: true,
  secondary_open_shell: true,
  http_state_record_count: httpStateRecords,
};
""" % json.dumps(origin))),
        timeout=180,
    )
    if (
        open_shell.get("primary_open_shell") is not True
        or open_shell.get("secondary_open_shell") is not True
        or open_shell.get("http_state_record_count") != 0
    ):
        raise CaseError("wfl_008_open_shell_cross_tab_mismatch", assertion_failure=True)
    browser.checkpoint("open_shell_restored")
    open_shell_snapshot = browser.capture_snapshot("auth-open-shell-restored")
    trace_stop_receipt = browser.stop_trace()
    if (
        len(browser.trace_receipts) != 2
        or browser.trace_receipts[0].get("trace_active") is not True
        or trace_stop_receipt.get("started_ns") != browser.trace_receipts[0].get("started_ns")
        or not isinstance(trace_stop_receipt.get("ended_at"), str)
        or not isinstance(trace_stop_receipt.get("trace"), Mapping)
    ):
        raise CaseError("wfl_008_trace_lifecycle_invalid")

    browser.write_json(
        "auth-transition.json",
        {
            "initial": {
                "state": initial_status.get("state"),
                "login_available": initial_status.get("login_available"),
            },
            "enabled": auth_enable,
            "login": login,
            "logout": {
                "status": logout_audit.get("logout_status"),
                "primary_path": logout_audit.get("primary_path"),
                "secondary_path": logout_audit.get("secondary_path"),
            },
            "disabled": disabled,
            "final": {
                "state": final_status.get("state"),
                "login_available": final_status.get("login_available"),
            },
            "open_shell": open_shell,
            "cross_tab_storage_events": {
                "enable": enable_storage_event,
                "disable": disable_storage_event,
                "receipts_removed": True,
            },
            "trace_lifecycle": browser.trace_receipts,
            "passed": True,
        },
    )
    browser.write_json(
        "multi-tab-snapshots.json",
        {
            "page_count": refresh_sync.get("page_count"),
            "states": [
                {"phase": "redirected", "snapshot": redirect_snapshot, "primary_path": auth_enable.get("primary_path"), "secondary_path": auth_enable.get("secondary_path")},
                {"phase": "primary_login", "snapshot": primary_login_snapshot, "primary_path": login.get("primary_path")},
                {"phase": "secondary_refresh", "snapshot": secondary_login_snapshot, "secondary_path": login.get("secondary_path")},
                {"phase": "logged_out", "snapshot": logout_snapshot, "primary_path": logout_audit.get("primary_path")},
                {
                    "phase": "open_shell",
                    "snapshot": open_shell_snapshot,
                    "primary_path": open_shell.get("primary_path"),
                    "secondary_path": open_shell.get("secondary_path"),
                    "primary_open_shell": open_shell.get("primary_open_shell"),
                    "secondary_open_shell": open_shell.get("secondary_open_shell"),
                },
            ],
            "refresh_sync": refresh_sync,
            "active_cross_tab_events": {
                "enable": enable_storage_event,
                "disable": disable_storage_event,
            },
            "passed": True,
        },
    )
    browser.write_json(
        "key-rotation-grid.redacted.json",
        {
            "key_id": key_id,
            "create_status": key_setup.get("create_status"),
            "rotate_status": rotation.get("rotate_status"),
            "rotation_count": rotation.get("rotation_count"),
            "old_value_status": key_probe.get("old_value_status"),
            "new_value_status": key_probe.get("new_value_status"),
            "new_value_attribution": new_projection,
            "delete_status": revoke.get("delete_status"),
            "revoked_value_status": revoked_probe.get("revoked_value_status"),
            "rejected_runtime_rows": {"old": 0, "revoked": 0},
            "passed": True,
        },
    )
    browser.write_json(
        "session-storage-audit.json",
        {
            "before_logout": logout_audit.get("before"),
            "after_logout": logout_audit.get("after"),
            "ephemeral_values_destroyed": logout_audit.get("ephemeral_values_destroyed"),
            "open_shell_http_state_record_count": open_shell.get("http_state_record_count"),
            "cross_tab_receipts_removed": bool(
                auth_enable.get("storage_receipt_removed")
                and disabled.get("storage_receipt_removed")
            ),
            "trace_lifecycle": browser.trace_receipts,
            "sensitive_values_absent": True,
            "passed": True,
        },
    )


RETENTION_PAGE_HELPERS = r"""
const chooseRequestRetention = async () => {
  const section = await expectVisible(page.getByTestId('manual-cleanup-section'), 'manual_cleanup_section_missing');
  await section.scrollIntoViewIfNeeded();
  const selectors = section.getByRole('combobox');
  if (await selectors.count() !== 2) throw new Error('manual_cleanup_selector_count');
  await selectors.nth(0).click();
  await (await expectVisible(page.getByRole('option', {name: /^请求日志$|^Request logs$/i}), 'request_logs_option_missing')).click();
  await selectors.nth(1).click();
  await (await expectVisible(page.getByRole('option', {name: /^1\s*天$|^1\s*day$/i}), 'one_day_option_missing')).click();
  return section;
};
const safeCountFact = (value) => ({
  value: value?.value ?? null,
  accuracy: value?.accuracy ?? null,
  method: value?.method ?? null,
});
const safePreflight = (value) => {
  const domain = Array.isArray(value?.affected_domains) ? value.affected_domains[0] : null;
  const impact = domain?.impact;
  return {
    preflight_id: value?.preflight_id ?? null,
    kind: value?.kind ?? null,
    scope: value?.scope ?? null,
    previewed_at: value?.previewed_at ?? null,
    expires_at: value?.expires_at ?? null,
    confirmation_keyword: value?.confirmation_keyword ?? null,
    capability_present: typeof value?.preflight_token === 'string' && value.preflight_token.length > 0,
    affected_domains: domain ? [{
      dataset: domain.dataset ?? null,
      resolved_cutoff: impact?.resolved_cutoff ?? null,
      matched_rows: safeCountFact(impact?.matched_rows),
      retained_rows: safeCountFact(impact?.retained_rows),
      whole_partitions: {
        count: impact?.whole_partitions?.count ?? null,
        names_preview: Array.isArray(impact?.whole_partitions?.names_preview) ? impact.whole_partitions.names_preview : [],
        names_total_count: impact?.whole_partitions?.names_total_count ?? null,
        truncated: impact?.whole_partitions?.truncated ?? null,
      },
      boundary_partitions: Array.isArray(impact?.boundary_partitions) ? impact.boundary_partitions.map((item) => ({
        name: item?.name ?? null,
        matched_rows: safeCountFact(item?.matched_rows),
      })) : [],
      semantic_facts_complete: impact?.semantic_facts_complete === true,
      warnings: Array.isArray(impact?.warnings) ? impact.warnings : [],
    }] : [],
  };
};
const safeJob = (value) => ({
  id: value?.id ?? null,
  contract_version: value?.contract_version ?? null,
  dataset: value?.dataset ?? null,
  origin: value?.origin ?? null,
  state: value?.state ?? null,
  terminal_disposition: value?.terminal_disposition ?? null,
  mode: value?.mode ?? null,
  cutoff: value?.cutoff ?? null,
  requested_at: value?.requested_at ?? null,
  started_at: value?.started_at ?? null,
  finished_at: value?.finished_at ?? null,
  attempt_count: value?.attempt_count ?? null,
  cancel_allowed: value?.cancel_allowed ?? null,
  progress: {
    accounting_provenance: value?.progress?.accounting_provenance ?? null,
    stage: value?.progress?.stage ?? null,
    visibility_state: value?.progress?.visibility_state ?? null,
    purge_state: value?.progress?.purge_state ?? null,
    rows_matched_estimate: value?.progress?.rows_matched_estimate ?? null,
    rows_matched_accuracy: value?.progress?.rows_matched_accuracy ?? null,
    boundary_rows_deleted: value?.progress?.boundary_rows_deleted ?? null,
    boundary_batches_completed: value?.progress?.boundary_batches_completed ?? null,
    dropped_partition_count: value?.progress?.dropped_partition_count ?? null,
    dropped_partition_count_accuracy: value?.progress?.dropped_partition_count_accuracy ?? null,
    dropped_partition_names_preview: Array.isArray(value?.progress?.dropped_partition_names_preview) ? value.progress.dropped_partition_names_preview : [],
    dropped_partition_names_total_count: value?.progress?.dropped_partition_names_total_count ?? null,
    dropped_rows_estimate: value?.progress?.dropped_rows_estimate ?? null,
    dropped_rows_accuracy: value?.progress?.dropped_rows_accuracy ?? null,
    last_checkpoint_at: value?.progress?.last_checkpoint_at ?? null,
  },
  error: value?.error ? {code: value.error.code ?? null} : null,
});
const safeCheckpointItems = (value) => Array.isArray(value) ? value.map((item) => ({
  sequence: item?.sequence ?? null,
  kind: item?.kind ?? null,
  stage: item?.stage ?? null,
  boundary_rows_delta: item?.boundary_rows_delta ?? null,
  dropped_partition_delta: item?.dropped_partition_delta ?? null,
  safe_detail_code: item?.safe_detail_code ?? null,
  recorded_at: item?.recorded_at ?? null,
})) : [];
const safePartitionItems = (value) => Array.isArray(value) ? value.map((item) => ({
  sequence: item?.sequence ?? null,
  partition_name: item?.partition_name ?? null,
  action: item?.action ?? null,
  boundary_rows_deleted: item?.boundary_rows_deleted ?? null,
  dropped_rows_estimate: item?.dropped_rows_estimate ?? null,
  dropped_rows_accuracy: item?.dropped_rows_accuracy ?? null,
  evidence_at: item?.evidence_at ?? null,
})) : [];
const safeDetail = (value) => ({
  job: safeJob(value?.job),
  terminal_result: value?.terminal_result ? {
    kind: value.terminal_result.kind ?? null,
    finished_at: value.terminal_result.finished_at ?? null,
    visibility_state: value.terminal_result.visibility_state ?? null,
    published_epoch: value.terminal_result.published_epoch ?? null,
    published_floor: value.terminal_result.published_floor ?? null,
    accounting_provenance: value.terminal_result.accounting_provenance ?? null,
    cancellation_scope: value.terminal_result.cancellation_scope ?? null,
    coherent_outcome: value.terminal_result.coherent_outcome ?? null,
  } : null,
  checkpoints: safeCheckpointItems(value?.checkpoints?.items),
  partitions: safePartitionItems(value?.partitions?.items),
  checkpoint_page_complete: value?.checkpoints?.has_more === false,
  partition_page_complete: value?.partitions?.has_more === false,
});
const expectDetailCount = async (dialog, key, count) => {
  const labels = key === 'checkpoints'
    ? '(?:检查点|Checkpoints)'
    : '(?:分区证据|Partition evidence)';
  await expectVisible(
    dialog.getByText(new RegExp(`^${labels}:\\s*${count}$`, 'i')),
    `job_detail_${key}_count_not_rendered`,
  );
};
const expectJobDetailRendered = async (dialog, detail) => {
  await expectDetailCount(dialog, 'checkpoints', detail.checkpoints.length);
  await expectDetailCount(dialog, 'partitions', detail.partitions.length);
  if (detail.terminal_result?.kind) {
    await expectVisible(
      dialog.getByText(new RegExp(`^(?:终态结果|Terminal result):\\s*${detail.terminal_result.kind}$`, 'i')),
      'job_detail_terminal_result_not_rendered',
    );
  }
};
const waitForJobRow = async (jobId) => expectVisible(
  page.locator('#retention-jobs').getByRole('row').filter({hasText: jobId}),
  'retention_job_row_missing',
);
const openJobDetail = async (row, jobId, forbiddenJobId = null) => {
  const detailPromise = page.waitForResponse((response) => {
    return response.request().method() === 'GET'
      && urlPath(response.url()) === `/api/management/jobs/${encodeURIComponent(jobId)}`;
  }, {timeout: 30000});
  await clickButton(row, /查看详情|View details/i, 'view_job_details_missing');
  const dialog = await expectVisible(page.getByRole('dialog'), 'job_detail_dialog_missing');
  const openingText = await dialog.innerText();
  if (!openingText.includes(jobId)) throw new Error('job_detail_opening_identity_missing');
  if (forbiddenJobId && openingText.includes(forbiddenJobId)) throw new Error('job_detail_stale_identity_visible');
  const response = await detailPromise;
  if (response.status() !== 200) throw new Error('job_detail_status');
  const payload = await response.json();
  await expectVisible(dialog.getByText(jobId, {exact: false}), 'job_detail_identity_missing');
  const detail = safeDetail(payload);
  await expectJobDetailRendered(dialog, detail);
  return {dialog, detail};
};
const appendEvidencePage = (current, incoming, code) => {
  if (!incoming.length) throw new Error(code + '_empty_page');
  const sequences = new Set(current.map((item) => String(item.sequence)));
  for (const item of incoming) {
    const sequence = String(item.sequence);
    if (item.sequence === null || sequences.has(sequence)) throw new Error(code + '_duplicate_sequence');
    sequences.add(sequence);
  }
  return [...current, ...incoming];
};
const loadAllJobDetailEvidence = async (opened, jobId) => {
  const lanes = [
    {
      key: 'checkpoints',
      path: `/api/management/jobs/${encodeURIComponent(jobId)}/checkpoints`,
      heading: /^检查点$|^Checkpoints$/i,
      safeItems: safeCheckpointItems,
      completeKey: 'checkpoint_page_complete',
    },
    {
      key: 'partitions',
      path: `/api/management/jobs/${encodeURIComponent(jobId)}/partitions`,
      heading: /^分区证据$|^Partition evidence$/i,
      safeItems: safePartitionItems,
      completeKey: 'partition_page_complete',
    },
  ];
  for (const lane of lanes) {
    let pages = 0;
    while (!opened.detail[lane.completeKey]) {
      pages += 1;
      if (pages > 100) throw new Error(`job_detail_${lane.key}_page_limit`);
      const section = (await expectVisible(
        opened.dialog.getByRole('heading', {name: lane.heading}),
        `job_detail_${lane.key}_heading_missing`,
      )).locator('..');
      const pagePromise = page.waitForResponse((response) => {
        return response.request().method() === 'GET' && urlPath(response.url()) === lane.path;
      }, {timeout: 30000});
      await clickButton(section, /加载更多|Load more/i, `job_detail_${lane.key}_load_more_missing`);
      const response = await pagePromise;
      if (response.status() !== 200) throw new Error(`job_detail_${lane.key}_page_status`);
      const payload = await response.json();
      const incoming = lane.safeItems(payload?.items);
      opened.detail[lane.key] = appendEvidencePage(
        opened.detail[lane.key],
        incoming,
        `job_detail_${lane.key}`,
      );
      opened.detail[lane.completeKey] = payload?.has_more === false;
      await expectDetailCount(opened.dialog, lane.key, opened.detail[lane.key].length);
      const updatedSection = (await expectVisible(
        opened.dialog.getByRole('heading', {name: lane.heading}),
        `job_detail_${lane.key}_updated_heading_missing`,
      )).locator('..');
      const loadMore = updatedSection.getByRole('button', {name: /加载更多|Load more/i});
      if (payload?.has_more === true) {
        await expectVisible(loadMore, `job_detail_${lane.key}_next_page_control_missing`);
      } else if (payload?.has_more === false) {
        await loadMore.waitFor({state: 'hidden', timeout: 15000});
      } else {
        throw new Error(`job_detail_${lane.key}_page_shape`);
      }
    }
  }
  return opened;
};
const closeJobDetail = async (dialog) => {
  await clickButton(dialog, /^取消$|^Cancel$/i, 'close_job_detail_missing');
  await dialog.waitFor({state: 'hidden', timeout: 15000});
};
"""


def parse_wfl_009_cutoff(value: Any) -> dt.datetime:
    if not isinstance(value, str):
        raise CaseError("wfl_009_cutoff_missing")
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as exc:
        raise CaseError("wfl_009_cutoff_invalid") from exc
    if parsed.tzinfo is None:
        raise CaseError("wfl_009_cutoff_invalid")
    return parsed.astimezone(dt.timezone.utc)


def wfl_009_timestamps_equal(value: Any, expected: dt.datetime) -> bool:
    """Compare RFC3339 values by instant across API precision variants."""
    try:
        return parse_wfl_009_cutoff(value) == expected.astimezone(dt.timezone.utc)
    except (CaseError, ValueError):
        return False


def parse_wfl_009_utc_timestamp(value: Any, code: str) -> dt.datetime:
    if not isinstance(value, str) or not value.endswith("Z"):
        raise CaseError(code, assertion_failure=True)
    parsed = parse_wfl_009_cutoff(value)
    if parsed.utcoffset() != dt.timedelta(0):
        raise CaseError(code, assertion_failure=True)
    return parsed


def assert_wfl_009_preflight_time(
    preflight: Mapping[str, Any],
    *,
    minimum_remaining_seconds: int = 0,
) -> None:
    previewed = parse_wfl_009_utc_timestamp(
        preflight.get("previewed_at"), "wfl_009_preflight_preview_time_invalid"
    )
    expires = parse_wfl_009_utc_timestamp(
        preflight.get("expires_at"), "wfl_009_preflight_expiry_time_invalid"
    )
    if expires - previewed != dt.timedelta(minutes=5):
        raise CaseError("wfl_009_preflight_ttl_mismatch", assertion_failure=True)
    now = dt.datetime.now(dt.timezone.utc)
    if previewed > now + dt.timedelta(seconds=30):
        raise CaseError("wfl_009_preflight_clock_ahead", assertion_failure=True)
    if expires <= now + dt.timedelta(seconds=minimum_remaining_seconds):
        raise CaseError("wfl_009_preflight_expired_retry_required", assertion_failure=True)


def assert_wfl_009_runtime_day(client: support.LocalHTTP, expected_cutoff: dt.datetime) -> dict[str, Any]:
    settings = api_object(client, "/api/settings/log-retention")
    server_now = parse_wfl_009_utc_timestamp(
        settings.get("server_now"), "wfl_009_runtime_clock_invalid"
    )
    actual_cutoff = server_now.replace(hour=0, minute=0, second=0, microsecond=0) - dt.timedelta(days=1)
    if actual_cutoff != expected_cutoff.astimezone(dt.timezone.utc):
        raise CaseError("wfl_009_utc_day_changed_retry_required")
    return settings


def wfl_009_state(
    database: support.LocalPostgres,
    marker: str,
    cutoff: dt.datetime,
    caller_prefix: str,
) -> dict[str, Any]:
    value = retention.as_dict(
        database.read_json(retention.state_sql(marker, cutoff, caller_prefix))
    )
    if not value.get("database") or not value.get("oid"):
        raise CaseError("wfl_009_database_state_invalid")
    return value


def wfl_009_marker_counts(state: Mapping[str, Any]) -> dict[str, dict[str, int]]:
    projected: dict[str, dict[str, int]] = {}
    raw_counts = retention.as_dict(state.get("marker_counts"))
    for dataset in retention.MANAGED_DATASETS:
        raw = retention.as_dict(raw_counts.get(dataset))
        try:
            old_rows = int(raw.get("old_rows"))
            retained_rows = int(raw.get("retained_rows"))
        except (TypeError, ValueError) as exc:
            raise CaseError("wfl_009_marker_count_shape_invalid") from exc
        if old_rows < 0 or retained_rows < 0:
            raise CaseError("wfl_009_marker_count_shape_invalid")
        projected[dataset] = {"old_rows": old_rows, "retained_rows": retained_rows}
    return projected


def wfl_009_retention_metadata(database: support.LocalPostgres) -> dict[str, Any]:
    value = retention.as_dict(
        database.read_json(
            """
SELECT jsonb_build_object(
    'settings', (
        SELECT to_jsonb(settings) FROM log_retention_settings AS settings
        WHERE singleton_key = 'global'
    ),
    'policy_resources', COALESCE((
        SELECT jsonb_agg(to_jsonb(resource) ORDER BY dataset)
        FROM log_retention_policy_resources AS resource
    ), '[]'::jsonb),
    'coverage_read_models', COALESCE((
        SELECT jsonb_agg(to_jsonb(coverage) ORDER BY dataset)
        FROM retention_coverage_read_models AS coverage
    ), '[]'::jsonb),
    'audit_fence', (
        SELECT to_jsonb(fence) FROM audit_retention_fence_projections AS fence
        WHERE id = 1
    )
)
"""
        )
    )
    if (
        not isinstance(value.get("settings"), Mapping)
        or len(retention.as_list(value.get("policy_resources"))) != len(retention.MANAGED_DATASETS)
        or len(retention.as_list(value.get("coverage_read_models"))) != len(retention.MANAGED_DATASETS)
        or not isinstance(value.get("audit_fence"), Mapping)
    ):
        raise CaseError("wfl_009_retention_metadata_invalid")
    return value


def wfl_009_unchanged_projection(
    database: support.LocalPostgres,
    state: Mapping[str, Any],
) -> dict[str, Any]:
    projection = {
        "database": state.get("database"),
        "oid": state.get("oid"),
        "marker_counts": retention.as_dict(state.get("marker_counts")),
        "total_counts": retention.as_dict(state.get("total_counts")),
        "harness_counts": retention.as_dict(state.get("harness_counts")),
        "partitions": retention.as_dict(state.get("partitions")),
        "purge_states": retention.as_dict(state.get("purge_states")),
        "retention_metadata": wfl_009_retention_metadata(database),
    }
    datasets = set(retention.MANAGED_DATASETS)
    if (
        not projection["database"]
        or not projection["oid"]
        or any(set(retention.as_dict(projection[key])) != datasets for key in (
            "marker_counts",
            "total_counts",
            "harness_counts",
            "partitions",
            "purge_states",
        ))
    ):
        raise CaseError("wfl_009_unchanged_projection_invalid")
    return projection


def projection_sha256(value: Mapping[str, Any]) -> str:
    return hashlib.sha256(canonical_bytes(value)).hexdigest()


def wfl_009_guard_state(database: support.LocalPostgres) -> dict[str, Any]:
    value = retention.as_dict(
        database.read_json(
            """
SELECT jsonb_build_object(
    'trigger_count', (
        SELECT count(*) FROM pg_trigger
        WHERE tgname = 'matrix_wfl_009_defer_first_manual_job' AND NOT tgisinternal
    ),
    'state_rows', (SELECT count(*) FROM matrix_wfl_009_cancel_guard_state),
    'deferred_count', (SELECT deferred_count FROM matrix_wfl_009_cancel_guard_state WHERE singleton),
    'deferred_job_id', (SELECT deferred_job_id FROM matrix_wfl_009_cancel_guard_state WHERE singleton),
    'deferred_job_delay_seconds', (
        SELECT floor(extract(epoch FROM (job.next_attempt_at - job.requested_at)))::bigint
        FROM management_jobs AS job
        JOIN matrix_wfl_009_cancel_guard_state AS guard
          ON guard.deferred_job_id = job.id
        WHERE guard.singleton
    ),
    'nonterminal_retention_jobs', (
        SELECT count(*) FROM management_jobs
        WHERE type = 'log_retention' AND state IN ('queued', 'running', 'cancel_requested')
    )
)
"""
        )
    )
    for key in ("trigger_count", "state_rows", "deferred_count", "nonterminal_retention_jobs"):
        try:
            if int(value.get(key)) < 0:
                raise ValueError
        except (TypeError, ValueError) as exc:
            raise CaseError("wfl_009_cancellation_guard_state_invalid") from exc
    return value


def assert_wfl_009_preflight(
    preflight: Mapping[str, Any],
    oracle: Mapping[str, Any],
    expected_cutoff: dt.datetime,
    boundary_partition: str,
) -> None:
    assert_wfl_009_preflight_time(preflight)
    domains = retention.as_list(preflight.get("affected_domains"))
    if len(domains) != 1:
        raise CaseError("wfl_009_preflight_domain_count_mismatch", assertion_failure=True)
    domain = retention.as_dict(domains[0])
    if (
        preflight.get("kind") != "manual_cleanup"
        or preflight.get("scope") != "instance"
        or preflight.get("confirmation_keyword") != "DELETE"
        or preflight.get("capability_present") is not True
        or domain.get("dataset") != "request_logs"
        or not wfl_009_timestamps_equal(domain.get("resolved_cutoff"), expected_cutoff)
        or domain.get("semantic_facts_complete") is not True
    ):
        raise CaseError("wfl_009_preflight_semantics_mismatch", assertion_failure=True)

    whole = retention.as_dict(domain.get("whole_partitions"))
    expected_whole = [str(value) for value in retention.as_list(oracle.get("whole_partition_names"))]
    expected_preview = [str(value) for value in retention.as_list(oracle.get("whole_partition_names_preview"))]
    expected_whole_count = int(oracle.get("whole_partition_count") or 0)
    if (
        str(whole.get("count")) != str(expected_whole_count)
        or str(whole.get("names_total_count")) != str(expected_whole_count)
        or retention.as_list(whole.get("names_preview")) != expected_preview
        or whole.get("truncated") is not (expected_whole_count > 8)
        or sorted(expected_whole) != expected_whole
    ):
        raise CaseError("wfl_009_preflight_partition_estimate_mismatch", assertion_failure=True)

    for fact_name, oracle_name in (
        ("matched_rows", "matched_rows_estimate"),
        ("retained_rows", "retained_rows_estimate"),
    ):
        fact = retention.as_dict(domain.get(fact_name))
        expected_fact = {
            "value": str(oracle.get(oracle_name)),
            "accuracy": "estimated",
            "method": "partition_metadata",
        }
        if fact != expected_fact:
            raise CaseError("wfl_009_preflight_row_estimate_mismatch", assertion_failure=True)

    boundary = [retention.as_dict(value) for value in retention.as_list(domain.get("boundary_partitions"))]
    if len(boundary) != 1 or boundary[0].get("name") != boundary_partition:
        raise CaseError("wfl_009_preflight_boundary_mismatch", assertion_failure=True)
    boundary_fact = retention.as_dict(boundary[0].get("matched_rows"))
    if boundary_fact != {
        "value": str(oracle.get("boundary_partition_estimate")),
        "accuracy": "estimated",
        "method": "partition_metadata",
    }:
        raise CaseError("wfl_009_preflight_boundary_estimate_mismatch", assertion_failure=True)


def wfl_009_job_database_projection(
    database: support.LocalPostgres,
    cancelled_job_id: str,
    completed_job_id: str,
) -> dict[str, Any]:
    if (
        not SAFE_IDENTIFIER_RE.fullmatch(cancelled_job_id)
        or not SAFE_IDENTIFIER_RE.fullmatch(completed_job_id)
        or cancelled_job_id == completed_job_id
    ):
        raise CaseError("wfl_009_job_identity_invalid")
    ids = ",".join(support.sql_literal(value) for value in (cancelled_job_id, completed_job_id))
    value = retention.as_dict(
        database.read_json(
            f"""
SELECT jsonb_build_object(
    'jobs', COALESCE((
        SELECT jsonb_agg(jsonb_build_object(
            'id', id,
            'state', state,
            'dataset', resource_key,
            'origin', origin,
            'contract_version', contract_version,
            'preflight_id', preflight_id,
            'terminal_disposition', terminal_disposition,
            'cutoff', scope_json->>'cutoff',
            'cancel_requested', cancel_requested
        ) ORDER BY requested_at, id)
        FROM management_jobs WHERE id IN ({ids})
    ), '[]'::jsonb),
    'bound_manual_request_job_count', (
        SELECT count(*) FROM management_jobs
        WHERE type = 'log_retention' AND contract_version = 2
          AND origin = 'manual' AND resource_key = 'request_logs'
          AND id IN ({ids})
    ),
    'bound_consumed_preflight_count', (
        SELECT count(*) FROM log_retention_preflights AS preflight
        WHERE preflight.id IN (
            SELECT job.preflight_id FROM management_jobs AS job WHERE job.id IN ({ids})
        )
          AND preflight.consumed_at IS NOT NULL
          AND preflight.consumed_operation_id IS NOT NULL
    ),
    'queued_cancel_guard_present', EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'matrix_wfl_009_defer_first_manual_job' AND NOT tgisinternal
    ),
    'guard_state', (
        SELECT jsonb_build_object(
            'deferred_count', deferred_count,
            'deferred_job_id', deferred_job_id
        )
        FROM matrix_wfl_009_cancel_guard_state WHERE singleton
    ),
    'nonterminal_retention_job_count', (
        SELECT count(*) FROM management_jobs
        WHERE type = 'log_retention' AND state IN ('queued', 'running', 'cancel_requested')
    )
)
"""
        )
    )
    jobs = [retention.as_dict(item) for item in retention.as_list(value.get("jobs"))]
    by_id = {str(item.get("id")): item for item in jobs}
    cancelled = by_id.get(cancelled_job_id, {})
    completed = by_id.get(completed_job_id, {})
    preflight_ids = {str(item.get("preflight_id")) for item in jobs if item.get("preflight_id")}
    guard_state = retention.as_dict(value.get("guard_state"))
    if (
        len(jobs) != 2
        or cancelled.get("state") != "cancelled"
        or cancelled.get("terminal_disposition") != "cancelled"
        or completed.get("state") != "succeeded"
        or completed.get("terminal_disposition") != "completed"
        or any(item.get("dataset") != "request_logs" or item.get("origin") != "manual" for item in jobs)
        or any(item.get("contract_version") != 2 or not item.get("preflight_id") for item in jobs)
        or len(preflight_ids) != 2
        or int(value.get("bound_manual_request_job_count") or 0) != 2
        or int(value.get("bound_consumed_preflight_count") or 0) != 2
        or value.get("queued_cancel_guard_present") is not True
        or int(guard_state.get("deferred_count") or 0) != 1
        or guard_state.get("deferred_job_id") != cancelled_job_id
        or int(value.get("nonterminal_retention_job_count") or 0) != 0
    ):
        raise CaseError("wfl_009_job_database_state_mismatch", assertion_failure=True)
    return value


def wait_wfl_009_retention_disabled(client: support.LocalHTTP) -> dict[str, Any]:
    latest: dict[str, Any] = {}

    def ready() -> bool:
        nonlocal latest
        try:
            latest = api_object(client, "/api/settings/log-retention")
        except (support.HarnessError, CaseError):
            return False
        cutoffs = retention.as_dict(latest.get("configured_logical_cutoffs"))
        return (
            latest.get("state") == "ready"
            and set(cutoffs) == set(retention.MANAGED_DATASETS)
            and all(cutoffs.get(dataset) is None for dataset in retention.MANAGED_DATASETS)
        )

    if not support.wait_until(ready, timeout=20, interval=0.25):
        raise CaseError("wfl_009_retention_disable_timeout")
    return latest


def run_wfl_009(browser: BrowserCase, client: support.LocalHTTP, database: support.LocalPostgres) -> None:
    """Exercise one real retention UI session on the owned disposable clone."""
    origin = browser.spec.frontend_origin
    fixture = retention.as_dict(browser.state.get("wfl_009_fixture"))
    if (
        fixture.get("marker") != WFL_009_MARKER
        or fixture.get("caller_prefix") != WFL_009_CALLER_PREFIX
        or fixture.get("volume") != WFL_009_VOLUME
        or fixture.get("inherited_nonterminal_retention_jobs") != 0
        or fixture.get("database_content_identity") != browser.state.get("database_clone_identity")
        or retention.as_dict(fixture.get("queued_cancel_guard")).get("present") is not True
    ):
        raise CaseError("wfl_009_fixture_binding_missing")
    cutoff = parse_wfl_009_cutoff(fixture.get("cutoff"))
    expected_cutoff = retention.iso(cutoff)
    boundary_partition = "request_logs_p" + str(fixture.get("boundary_day"))
    future_partition = "request_logs_p" + str(fixture.get("future_day"))

    database.mutate_case(retention.analyze_managed_partitions_sql(), timeout=180)
    settings = wait_wfl_009_retention_disabled(client)
    settings_now = parse_wfl_009_utc_timestamp(
        settings.get("server_now"), "wfl_009_runtime_clock_invalid"
    )
    runtime_cutoff = settings_now.replace(hour=0, minute=0, second=0, microsecond=0) - dt.timedelta(days=1)
    if runtime_cutoff != cutoff:
        raise CaseError("wfl_009_utc_day_changed_retry_required")
    baseline = wfl_009_state(database, WFL_009_MARKER, cutoff, WFL_009_CALLER_PREFIX)
    if baseline.get("database") != browser.spec.database or str(baseline.get("oid")) != str(fixture.get("database_oid")):
        raise CaseError("wfl_009_runtime_clone_identity_mismatch")
    expected_marker_counts = {
        dataset: {"old_rows": 1, "retained_rows": WFL_009_VOLUME + 2}
        for dataset in retention.MANAGED_DATASETS
    }
    baseline_marker_counts = wfl_009_marker_counts(baseline)
    if baseline_marker_counts != expected_marker_counts:
        raise CaseError("wfl_009_runtime_fixture_drift")
    oracle = retention.preflight_oracle_from_state(baseline, "request_logs", cutoff)
    initial_guard = wfl_009_guard_state(database)
    if (
        int(initial_guard.get("trigger_count") or 0) != 1
        or int(initial_guard.get("state_rows") or 0) != 1
        or int(initial_guard.get("deferred_count") or 0) != 0
        or initial_guard.get("deferred_job_id") is not None
        or int(initial_guard.get("nonterminal_retention_jobs") or 0) != 0
    ):
        raise CaseError("wfl_009_cancellation_guard_not_pristine")
    expected_visible_matched = str(oracle.get("matched_rows_estimate"))
    expected_visible_retained = str(oracle.get("retained_rows_estimate"))
    if not expected_visible_matched.isdigit() or not expected_visible_retained.isdigit():
        raise CaseError("wfl_009_visible_preflight_count_invalid")

    browser.goto("/system/settings?scope=instance&section=manual-cleanup#manual-cleanup")
    browser.start_trace()
    preflight = browser.run_code(
        "retention_preflight_wrong",
        action_for_origin(origin, ui_body(
            RETENTION_PAGE_HELPERS
            + "const expectedMatched = %s;\nconst expectedRetained = %s;\n"
            % (json.dumps(expected_visible_matched), json.dumps(expected_visible_retained))
            + r"""
const section = await chooseRequestRetention();
let manualJobPosts = 0;
const countManualJob = (request) => {
  if (request.method() === 'POST' && urlPath(request.url()) === '/api/maintenance/log-retention/jobs') manualJobPosts += 1;
};
page.on('request', countManualJob);
try {
  const previewPromise = responseFor('POST', /^\/api\/maintenance\/log-retention\/preflights$/);
  await clickButton(section, /预检并删除数据|Preflight and delete/i, 'retention_preflight_button_missing');
  const previewResponse = await previewPromise;
  if (previewResponse.status() !== 201) throw new Error('retention_preflight_status');
  const preview = await previewResponse.json();
  const dialog = await expectVisible(page.getByRole('dialog'), 'retention_confirm_dialog_missing');
  await expectVisible(dialog.getByText(/^请求日志$|^Request logs$/i), 'retention_dialog_dataset_missing');
  await expectVisible(dialog.getByText(/删除\s*1\s*天前的数据|older than\s*1\s*day/i), 'retention_dialog_window_missing');
  const dialogText = (await dialog.innerText()).replace(/\s+/g, ' ');
  if (!(new RegExp(`预计命中[：:]\\s*约\\s*${expectedMatched}(?:\\D|$)`)).test(dialogText)) {
    throw new Error('retention_dialog_matched_count_missing');
  }
  if (!(new RegExp(`预计保留[：:]\\s*约\\s*${expectedRetained}(?:\\D|$)`)).test(dialogText)) {
    throw new Error('retention_dialog_retained_count_missing');
  }
  const phrase = await expectVisible(dialog.locator('#delete-confirm-phrase'), 'retention_phrase_input_missing');
  await phrase.fill('WRONG');
  const confirm = await expectVisible(dialog.getByRole('button', {name: /^删除$|^Delete$/i}), 'retention_confirm_button_missing');
  if (!(await confirm.isDisabled())) throw new Error('wrong_confirmation_not_disabled');
  await page.waitForTimeout(300);
  if (manualJobPosts !== 0) throw new Error('wrong_confirmation_created_job');
  return {
    surface: 'retention_preflight',
    preflight_status: previewResponse.status(),
    preflight: safePreflight(preview),
    wrong_phrase: 'WRONG',
    confirm_disabled: true,
    manual_job_post_count: manualJobPosts,
    visible_summary: {
      dataset: 'request_logs',
      retention_days: 1,
      matched_rows: expectedMatched,
      retained_rows: expectedRetained,
    },
  };
} finally {
  page.off('request', countManualJob);
}
""")),
        timeout=180,
    )
    safe_preflight = retention.as_dict(preflight.get("preflight"))
    assert_wfl_009_preflight(safe_preflight, oracle, cutoff, boundary_partition)
    if (
        preflight.get("preflight_status") != 201
        or preflight.get("wrong_phrase") != "WRONG"
        or preflight.get("confirm_disabled") is not True
        or preflight.get("manual_job_post_count") != 0
        or retention.as_dict(preflight.get("visible_summary"))
        != {
            "dataset": "request_logs",
            "retention_days": 1,
            "matched_rows": expected_visible_matched,
            "retained_rows": expected_visible_retained,
        }
    ):
        raise CaseError("wfl_009_wrong_confirmation_not_rejected", assertion_failure=True)
    preflight_snapshot = browser.capture_snapshot("retention-preflight-confirmation")
    browser.promote_snapshot("preflight.snapshot.txt", preflight_snapshot)
    browser.checkpoint("preflight_verified")
    browser.checkpoint("wrong_confirmation_rejected")
    assert_wfl_009_runtime_day(client, cutoff)
    assert_wfl_009_preflight_time(safe_preflight, minimum_remaining_seconds=30)

    started = browser.run_code(
        "retention_start_job",
        action_for_origin(origin, ui_body(
            RETENTION_PAGE_HELPERS
            + "const expectedPreflightExpiry = %s;\n" % json.dumps(safe_preflight.get("expires_at"))
            + r"""
const dialog = await expectVisible(page.getByRole('dialog'), 'retention_confirm_dialog_missing');
const expiryMillis = Date.parse(expectedPreflightExpiry);
if (!Number.isFinite(expiryMillis) || expiryMillis - Date.now() < 30000) {
  throw new Error('retention_preflight_expired_before_confirmation');
}
await dialog.locator('#delete-confirm-phrase').fill('DELETE');
const confirm = await expectVisible(dialog.getByRole('button', {name: /^删除$|^Delete$/i}), 'retention_confirm_button_missing');
await confirm.click({trial: true}).catch(() => { throw new Error('correct_confirmation_not_enabled'); });
const createPromise = responseFor('POST', /^\/api\/maintenance\/log-retention\/jobs$/);
await confirm.click();
const createResponse = await createPromise;
if (createResponse.status() !== 202) throw new Error('retention_job_create_status');
const payload = await createResponse.json();
const job = safeJob(payload?.job);
if (!job.id || job.state !== 'queued' || job.cancel_allowed !== true) throw new Error('retention_first_job_not_queued');
await dialog.waitFor({state: 'hidden', timeout: 15000});
const row = await waitForJobRow(job.id);
await expectVisible(row.getByRole('button', {name: /取消作业|Cancel job/i}), 'queued_cancel_button_missing');
return {
  surface: 'retention_jobs',
  accepted_status: createResponse.status(),
  job,
  row_visible: true,
};
""")),
        timeout=180,
    )
    started_job = retention.as_dict(started.get("job"))
    cancelled_job_id = str(started_job.get("id") or "")
    if (
        started.get("accepted_status") != 202
        or not SAFE_IDENTIFIER_RE.fullmatch(cancelled_job_id)
        or started_job.get("dataset") != "request_logs"
        or started_job.get("origin") != "manual"
        or started_job.get("state") != "queued"
        or started_job.get("cancel_allowed") is not True
        or started_job.get("started_at") is not None
        or int(started_job.get("attempt_count") or 0) != 0
        or not wfl_009_timestamps_equal(started_job.get("cutoff"), cutoff)
    ):
        raise CaseError("wfl_009_first_job_acceptance_mismatch", assertion_failure=True)
    guard_after_start = wfl_009_guard_state(database)
    if (
        int(guard_after_start.get("deferred_count") or 0) != 1
        or guard_after_start.get("deferred_job_id") != cancelled_job_id
        or int(guard_after_start.get("deferred_job_delay_seconds") or 0)
        < WFL_009_CANCEL_DEFER_SECONDS - 5
    ):
        raise CaseError("wfl_009_first_job_not_deterministically_deferred")
    if int(guard_after_start.get("nonterminal_retention_jobs") or 0) != 1:
        raise CaseError("wfl_009_first_job_nonterminal_state_mismatch", assertion_failure=True)
    before_cancel_state = wfl_009_state(
        database,
        WFL_009_MARKER,
        cutoff,
        WFL_009_CALLER_PREFIX,
    )
    before_cancel_projection = wfl_009_unchanged_projection(database, before_cancel_state)
    browser.checkpoint("correct_confirmation_accepted")
    browser.checkpoint("job_started")

    cancelled = browser.run_code(
        "retention_cancel_job",
        action_for_origin(origin, ui_body(RETENTION_PAGE_HELPERS + r"""
const jobId = %s;
const row = await waitForJobRow(jobId);
const jobsListPattern = /\/api\/maintenance\/log-retention\/jobs(?:\?|$)/;
let releaseStaleList = () => {};
let staleListReadyResolve;
let staleListDeliveredResolve;
const staleListGate = new Promise((resolve) => { releaseStaleList = resolve; });
const staleListReady = new Promise((resolve) => { staleListReadyResolve = resolve; });
const staleListDelivered = new Promise((resolve) => { staleListDeliveredResolve = resolve; });
let staleListCaptured = false;
const holdOneStaleList = async (route, request) => {
  if (!staleListCaptured && request.method() === 'GET' && jobsListPattern.test(request.url())) {
    staleListCaptured = true;
    const staleResponse = await route.fetch();
    staleListReadyResolve();
    await staleListGate;
    await route.fulfill({response: staleResponse});
    staleListDeliveredResolve();
    return;
  }
  await route.continue();
};
await page.route(jobsListPattern, holdOneStaleList);
try {
await Promise.race([
  staleListReady,
  page.waitForTimeout(8000).then(() => { throw new Error('cancel_stale_poll_not_observed'); }),
]);
const cancelPromise = page.waitForResponse((response) => {
  return response.request().method() === 'POST'
    && urlPath(response.url()) === `/api/management/jobs/${encodeURIComponent(jobId)}/cancel`;
}, {timeout: 30000});
await clickButton(row, /取消作业|Cancel job/i, 'cancel_job_button_missing');
const cancelResponse = await cancelPromise;
if (cancelResponse.status() !== 200) throw new Error('cancel_job_status');
const cancelledPayload = await cancelResponse.json();
const cancelledJob = safeJob(cancelledPayload?.job);
if (cancelledJob.id !== jobId || cancelledJob.state !== 'cancelled' || cancelledJob.cancel_allowed !== false) {
  throw new Error('cancelled_job_state_mismatch');
}
await expectVisible(row.getByText(/已取消|Cancelled/i), 'cancelled_job_badge_missing');
releaseStaleList();
await staleListDelivered;
await page.waitForTimeout(300);
const stableRow = await waitForJobRow(jobId);
await expectVisible(stableRow.getByText(/已取消|Cancelled/i), 'cancelled_job_overwritten_by_stale_poll');
if (await stableRow.getByRole('button', {name: /取消作业|Cancel job/i}).isVisible().catch(() => false)) {
  throw new Error('cancelled_job_regained_cancel_action');
}
await page.reload({waitUntil: 'domcontentloaded'});
await expectVisible(page.getByTestId('manual-cleanup-section'), 'retention_page_missing_after_cancel_refresh');
const refreshedRow = await waitForJobRow(jobId);
await expectVisible(refreshedRow.getByText(/已取消|Cancelled/i), 'cancelled_job_missing_after_refresh');
const opened = await openJobDetail(refreshedRow, jobId);
await loadAllJobDetailEvidence(opened, jobId);
if (opened.detail.job.state !== 'cancelled' || opened.detail.terminal_result?.kind !== 'cancelled') {
  throw new Error('cancelled_job_detail_mismatch');
}
await closeJobDetail(opened.dialog);
return {
  surface: 'retention_jobs',
  cancel_status: cancelResponse.status(),
  refreshed_from_list: true,
  stale_poll_monotonic: true,
  cancelled: opened.detail,
};
} finally {
  releaseStaleList();
  await page.unroute(jobsListPattern, holdOneStaleList);
}
""" % json.dumps(cancelled_job_id))),
        timeout=360,
    )
    cancelled_detail = retention.as_dict(cancelled.get("cancelled"))
    cancelled_summary = retention.as_dict(cancelled_detail.get("job"))
    cancelled_terminal = retention.as_dict(cancelled_detail.get("terminal_result"))
    if (
        cancelled.get("cancel_status") != 200
        or cancelled.get("refreshed_from_list") is not True
        or cancelled.get("stale_poll_monotonic") is not True
        or cancelled_summary.get("id") != cancelled_job_id
        or cancelled_summary.get("state") != "cancelled"
        or cancelled_summary.get("started_at") is not None
        or int(cancelled_summary.get("attempt_count") or 0) != 0
        or cancelled_terminal.get("kind") != "cancelled"
        or cancelled_terminal.get("cancellation_scope") != "queued_no_data_changed"
        or cancelled_terminal.get("visibility_state") != "unchanged"
        or cancelled_terminal.get("published_epoch") is not None
        or cancelled_terminal.get("published_floor") is not None
        or cancelled_terminal.get("coherent_outcome") != "no_data_changed"
    ):
        raise CaseError("wfl_009_cancelled_job_mismatch", assertion_failure=True)
    after_cancel_state = wfl_009_state(
        database,
        WFL_009_MARKER,
        cutoff,
        WFL_009_CALLER_PREFIX,
    )
    after_cancel_marker_counts = wfl_009_marker_counts(after_cancel_state)
    after_cancel_projection = wfl_009_unchanged_projection(database, after_cancel_state)
    if after_cancel_projection != before_cancel_projection:
        raise CaseError("wfl_009_cancel_changed_retention_storage", assertion_failure=True)
    guard_after_cancel = wfl_009_guard_state(database)
    if (
        int(guard_after_cancel.get("deferred_count") or 0) != 1
        or guard_after_cancel.get("deferred_job_id") != cancelled_job_id
    ):
        raise CaseError("wfl_009_cancel_guard_fixture_mismatch")
    if int(guard_after_cancel.get("nonterminal_retention_jobs") or 0) != 0:
        raise CaseError("wfl_009_cancel_terminal_state_mismatch", assertion_failure=True)
    baseline_request_partitions = {
        str(retention.as_dict(item).get("name"))
        for item in retention.as_list(
            retention.as_dict(baseline.get("partitions")).get("request_logs")
        )
    }
    after_cancel_request_partitions = {
        str(retention.as_dict(item).get("name"))
        for item in retention.as_list(
            retention.as_dict(after_cancel_state.get("partitions")).get("request_logs")
        )
    }
    if after_cancel_request_partitions != baseline_request_partitions:
        raise CaseError("wfl_009_cancel_changed_partitions", assertion_failure=True)
    browser.checkpoint("job_cancelled")
    assert_wfl_009_runtime_day(client, cutoff)

    restarted = browser.run_code(
        "retention_restart_job",
        action_for_origin(origin, ui_body(RETENTION_PAGE_HELPERS + r"""
const firstJobId = %s;
const section = await chooseRequestRetention();
const previewPromise = responseFor('POST', /^\/api\/maintenance\/log-retention\/preflights$/);
await clickButton(section, /预检并删除数据|Preflight and delete/i, 'retention_restart_preflight_button_missing');
const previewResponse = await previewPromise;
if (previewResponse.status() !== 201) throw new Error('retention_restart_preflight_status');
const preview = await previewResponse.json();
const previewedMillis = Date.parse(preview?.previewed_at ?? '');
const expiresMillis = Date.parse(preview?.expires_at ?? '');
if (!Number.isFinite(previewedMillis) || !Number.isFinite(expiresMillis)
    || expiresMillis - previewedMillis !== 300000 || expiresMillis - Date.now() < 30000) {
  throw new Error('retention_restart_preflight_time_invalid');
}
const dialog = await expectVisible(page.getByRole('dialog'), 'retention_restart_dialog_missing');
await dialog.locator('#delete-confirm-phrase').fill('DELETE');
const createPromise = responseFor('POST', /^\/api\/maintenance\/log-retention\/jobs$/);
await clickButton(dialog, /^删除$|^Delete$/i, 'retention_restart_confirm_missing');
const createResponse = await createPromise;
if (createResponse.status() !== 202) throw new Error('retention_restart_create_status');
const payload = await createResponse.json();
const job = safeJob(payload?.job);
if (!job.id || job.id === firstJobId || job.state !== 'queued') throw new Error('retention_restart_identity');
await dialog.waitFor({state: 'hidden', timeout: 15000});
await waitForJobRow(job.id);
return {
  surface: 'retention_jobs',
  preflight_status: previewResponse.status(),
  accepted_status: createResponse.status(),
  preflight: safePreflight(preview),
  job,
};
""" % json.dumps(cancelled_job_id))),
        timeout=180,
    )
    completion_preflight = retention.as_dict(restarted.get("preflight"))
    assert_wfl_009_preflight(completion_preflight, oracle, cutoff, boundary_partition)
    completion_started = retention.as_dict(restarted.get("job"))
    completed_job_id = str(completion_started.get("id") or "")
    if (
        restarted.get("preflight_status") != 201
        or restarted.get("accepted_status") != 202
        or not SAFE_IDENTIFIER_RE.fullmatch(completed_job_id)
        or completed_job_id == cancelled_job_id
        or completion_started.get("state") != "queued"
        or completion_started.get("dataset") != "request_logs"
        or not wfl_009_timestamps_equal(completion_started.get("cutoff"), cutoff)
    ):
        raise CaseError("wfl_009_restart_job_mismatch", assertion_failure=True)
    browser.checkpoint("job_restarted")

    completed = browser.run_code(
        "retention_wait_completion",
        action_for_origin(origin, ui_body(RETENTION_PAGE_HELPERS + r"""
const jobId = %s;
const forbiddenJobId = %s;
const row = await waitForJobRow(jobId);
await row.getByText(/已完成|Succeeded|Completed/i).first().waitFor({state: 'visible', timeout: 180000})
  .catch(() => { throw new Error('completed_job_badge_missing'); });
const oldDetailPattern = `**/api/management/jobs/${encodeURIComponent(forbiddenJobId)}`;
let releaseOldDetail = () => {};
let oldDetailReadyResolve;
let oldDetailDeliveredResolve;
const oldDetailGate = new Promise((resolve) => { releaseOldDetail = resolve; });
const oldDetailReady = new Promise((resolve) => { oldDetailReadyResolve = resolve; });
const oldDetailDelivered = new Promise((resolve) => { oldDetailDeliveredResolve = resolve; });
const holdOldDetail = async (route) => {
  const response = await route.fetch();
  oldDetailReadyResolve();
  await oldDetailGate;
  await route.fulfill({response});
  oldDetailDeliveredResolve();
};
await page.route(oldDetailPattern, holdOldDetail);
let opened;
try {
  const oldRow = await waitForJobRow(forbiddenJobId);
  await clickButton(oldRow, /查看详情|View details/i, 'old_job_detail_action_missing');
  await Promise.race([
    oldDetailReady,
    page.waitForTimeout(5000).then(() => { throw new Error('old_job_detail_request_not_observed'); }),
  ]);
  const oldDialog = await expectVisible(page.getByRole('dialog'), 'old_job_detail_dialog_missing');
  await closeJobDetail(oldDialog);
  opened = await openJobDetail(row, jobId, forbiddenJobId);
  releaseOldDetail();
  await oldDetailDelivered;
  await page.waitForTimeout(300);
  const stableDetailText = await opened.dialog.innerText();
  if (!stableDetailText.includes(jobId) || stableDetailText.includes(forbiddenJobId)) {
    throw new Error('job_detail_overwritten_by_older_request');
  }
  await expectJobDetailRendered(opened.dialog, opened.detail);
} finally {
  releaseOldDetail();
  await page.unroute(oldDetailPattern, holdOldDetail);
}
await loadAllJobDetailEvidence(opened, jobId);
if (opened.detail.job.state !== 'succeeded' || opened.detail.terminal_result?.kind !== 'succeeded') {
  throw new Error('completed_job_detail_mismatch');
}
if (!opened.detail.checkpoint_page_complete || !opened.detail.partition_page_complete) {
  throw new Error('completed_job_detail_paginated');
}
await closeJobDetail(opened.dialog);
return {surface: 'retention_jobs', completed: opened.detail, list_terminal_state: 'succeeded'};
""" % (json.dumps(completed_job_id), json.dumps(cancelled_job_id)))),
        timeout=360,
    )
    completed_detail = retention.as_dict(completed.get("completed"))
    completed_summary = retention.as_dict(completed_detail.get("job"))
    completed_terminal = retention.as_dict(completed_detail.get("terminal_result"))
    if (
        completed_summary.get("id") != completed_job_id
        or completed_summary.get("state") != "succeeded"
        or completed_terminal.get("kind") != "succeeded"
        or completed_terminal.get("visibility_state") != "revoked"
        or not wfl_009_timestamps_equal(completed_terminal.get("published_floor"), cutoff)
        or completed_terminal.get("accounting_provenance") != "v2_exact"
    ):
        raise CaseError("wfl_009_completion_state_mismatch", assertion_failure=True)
    browser.checkpoint("job_completed")

    refreshed = browser.run_code(
        "retention_completion_refresh",
        action_for_origin(origin, ui_body(RETENTION_PAGE_HELPERS + r"""
const jobId = %s;
await page.reload({waitUntil: 'domcontentloaded'});
await expectVisible(page.getByTestId('manual-cleanup-section'), 'retention_page_missing_after_refresh');
const row = await waitForJobRow(jobId);
await expectVisible(row.getByText(/已完成|Succeeded|Completed/i), 'completed_job_missing_after_refresh');
const opened = await openJobDetail(row, jobId);
await loadAllJobDetailEvidence(opened, jobId);
if (opened.detail.job.state !== 'succeeded' || opened.detail.terminal_result?.kind !== 'succeeded') {
  throw new Error('refreshed_job_detail_mismatch');
}
await closeJobDetail(opened.dialog);
return {surface: 'retention_jobs', refreshed: true, completed: opened.detail};
""" % json.dumps(completed_job_id))),
        timeout=300,
    )
    refreshed_detail = retention.as_dict(refreshed.get("completed"))
    if (
        refreshed.get("refreshed") is not True
        or retention.as_dict(refreshed_detail.get("job")).get("id") != completed_job_id
        or retention.as_dict(refreshed_detail.get("job")).get("state") != "succeeded"
    ):
        raise CaseError("wfl_009_completion_refresh_mismatch", assertion_failure=True)
    browser.checkpoint("completion_status_refreshed")
    assert_wfl_009_runtime_day(client, cutoff)

    final_state = wfl_009_state(database, WFL_009_MARKER, cutoff, WFL_009_CALLER_PREFIX)
    if final_state.get("database") != browser.spec.database or str(final_state.get("oid")) != str(fixture.get("database_oid")):
        raise CaseError("wfl_009_final_clone_identity_mismatch")
    final_marker_counts = wfl_009_marker_counts(final_state)
    expected_final_counts = dict(expected_marker_counts)
    expected_final_counts["request_logs"] = {"old_rows": 0, "retained_rows": WFL_009_VOLUME + 2}
    if final_marker_counts != expected_final_counts:
        raise CaseError("wfl_009_retained_rows_mismatch", assertion_failure=True)
    final_partitions = {
        str(retention.as_dict(item).get("name"))
        for item in retention.as_list(retention.as_dict(final_state.get("partitions")).get("request_logs"))
    }
    expected_dropped = {str(value) for value in retention.as_list(oracle.get("whole_partition_names"))}
    if expected_dropped & final_partitions or boundary_partition not in final_partitions or future_partition not in final_partitions:
        raise CaseError("wfl_009_partition_result_mismatch", assertion_failure=True)
    purge_states = retention.as_dict(final_state.get("purge_states"))
    if purge_states.get("request_logs") != "published":
        raise CaseError("wfl_009_purge_publish_missing", assertion_failure=True)
    job_database = wfl_009_job_database_projection(database, cancelled_job_id, completed_job_id)

    completed_checkpoint_stages = {
        str(retention.as_dict(item).get("stage"))
        for item in retention.as_list(completed_detail.get("checkpoints"))
    }
    required_stages = {
        "purge_running",
        "dropping_partitions",
        "deleting_boundary_rows",
        "publishing_epoch_coverage",
    }
    completed_partition_names = {
        str(retention.as_dict(item).get("partition_name"))
        for item in retention.as_list(completed_detail.get("partitions"))
        if retention.as_dict(item).get("action") == "dropped"
    }
    if not required_stages <= completed_checkpoint_stages or completed_partition_names != expected_dropped:
        raise CaseError("wfl_009_job_evidence_incomplete", assertion_failure=True)
    browser.checkpoint("retained_rows_verified")

    browser.write_json(
        "confirmation-validation.json",
        {
            "preflight_id": safe_preflight.get("preflight_id"),
            "preflight_status": preflight.get("preflight_status"),
            "wrong": {
                "phrase": preflight.get("wrong_phrase"),
                "confirm_disabled": preflight.get("confirm_disabled"),
                "manual_job_post_count": preflight.get("manual_job_post_count"),
                "rejected": True,
            },
            "correct": {
                "accepted_status": started.get("accepted_status"),
                "job_id": cancelled_job_id,
                "cancel_status": cancelled.get("cancel_status"),
            },
            "restart": {
                "preflight_id": completion_preflight.get("preflight_id"),
                "preflight_status": restarted.get("preflight_status"),
                "accepted_status": restarted.get("accepted_status"),
                "job_id": completed_job_id,
            },
            "passed": True,
        },
    )
    browser.write_json(
        "job-timeline.json",
        {
            "database": browser.spec.database,
            "database_oid": str(fixture.get("database_oid")),
            "cancelled_lane": {
                "accepted": started_job,
                "terminal": cancelled_detail,
                "marker_counts_after_cancel": after_cancel_marker_counts,
                "request_partitions_after_cancel": sorted(after_cancel_request_partitions),
                "retention_storage_projection_before_sha256": projection_sha256(before_cancel_projection),
                "retention_storage_projection_after_sha256": projection_sha256(after_cancel_projection),
                "retention_storage_unchanged": True,
            },
            "completed_lane": {
                "accepted": completion_started,
                "terminal": completed_detail,
                "refreshed_terminal": refreshed_detail,
            },
            "database_projection": job_database,
            "same_attempt": True,
            "passed": True,
        },
    )
    browser.write_json(
        "retention-result.json",
        {
            "database": browser.spec.database,
            "database_oid": str(fixture.get("database_oid")),
            "fixture_scope": "disposable_case_clone",
            "cutoff": expected_cutoff,
            "automatic_policy_disabled": all(
                retention.as_dict(settings.get("configured_logical_cutoffs")).get(dataset) is None
                for dataset in retention.MANAGED_DATASETS
            ),
            "preflight_oracle": oracle,
            "baseline_marker_counts": baseline_marker_counts,
            "final_marker_counts": final_marker_counts,
            "expected_dropped_partitions": sorted(expected_dropped),
            "final_request_partitions": sorted(final_partitions),
            "retained_control_partitions": [boundary_partition, future_partition],
            "purge_states": purge_states,
            "non_target_marker_rows_preserved": all(
                final_marker_counts[dataset] == baseline_marker_counts[dataset]
                for dataset in retention.MANAGED_DATASETS
                if dataset != "request_logs"
            ),
            "retained_rows_verified": True,
            "clone_drop_owned_by_case_finalizer": True,
            "passed": True,
        },
    )


CASE_RUNNERS: Mapping[str, Callable[[BrowserCase, support.LocalHTTP, support.LocalPostgres], None]] = {
    "WFL-003": run_wfl_003,
    "WFL-004": run_wfl_004,
    "WFL-005": run_wfl_005,
    "WFL-006": run_wfl_006,
    "WFL-007": run_wfl_007,
    "WFL-008": run_wfl_008,
    "WFL-009": run_wfl_009,
}


def case_database_exists(spec: CaseSpec) -> bool:
    output = support.run_lane(["status"], ROOT)
    return any(line.split("|", 1)[0] == spec.database for line in output.splitlines())


def assert_case_database_absent(spec: CaseSpec) -> None:
    if case_database_exists(spec):
        raise CaseError("workflow_case_database_cleanup_failed")


def destroy_sealed_private_values(browser: BrowserCase) -> None:
    path_text = browser.state["paths"].get("private_values")
    if not path_text:
        return
    path = pathlib.Path(str(path_text))
    workflow.helper_seal_redaction(helper_namespace(case_dir=browser.case_dir))
    if path.is_symlink() or not path.is_file():
        raise CaseError("workflow_private_values_destroy_refused")
    path.unlink()
    if path.exists() or path.is_symlink():
        raise CaseError("workflow_private_values_destroy_failed")


def formal_owner_files() -> dict[str, pathlib.Path]:
    return {
        "workflow_owner": pathlib.Path(__file__).resolve(),
        "local_support": pathlib.Path(support.__file__).resolve(),
        "retention_helper": pathlib.Path(retention.__file__).resolve(),
        "database_lane": ROOT / "artifacts" / "tools" / "db" / "db_lane.py",
        "mock_program": MOCK_PROGRAM,
    }


def update_preparation_outcome(
    spec: CaseSpec,
    formal: workflow.FormalAttempt,
    outcome: str,
) -> None:
    if outcome not in {"complete", "product_failed"}:
        raise CaseError("workflow_preparation_outcome_invalid")
    receipt = load_preparation_receipt(spec)
    expected_allocation = {
        "result_id": formal.result_id,
        "cycle": formal.cycle,
        "attempt": formal.number,
        "result_sha256": formal.result_sha256,
    }
    if (
        receipt is None
        or receipt.get("state") != "adopted"
        or receipt.get("allocation") != expected_allocation
        or receipt.get("database_clone_fingerprint") != formal.database_clone
    ):
        raise CaseError("workflow_preparation_outcome_binding_invalid")
    updated = dict(receipt)
    updated["state"] = outcome
    updated["clone_dropped"] = True
    updated["finished_at"] = utc_now()
    write_private_json(preparation_receipt_path(spec), updated)


def stop_case_services_and_clone(
    spec: CaseSpec,
    services: Optional[ServiceGroup],
) -> None:
    if services is not None:
        services.close()
    process_dir = lane_dir(spec) / "processes"
    for process_name in ("frontend", "backend", "mock"):
        stop_stale_owned_process(process_dir / (process_name + ".json"), process_name)
    require_ports_available(spec)
    if case_database_exists(spec):
        support.run_lane(["drop-case", spec.database], ROOT)
    assert_case_database_absent(spec)


def finalize_product_failure(
    spec: CaseSpec,
    state: dict[str, Any],
    browser: BrowserCase,
    services: Optional[ServiceGroup],
    formal: workflow.FormalAttempt,
    failure: CaseError,
) -> dict[str, Any]:
    try:
        if browser.trace_active:
            browser.stop_trace()
        helper = workflow.load_state(browser.case_dir)
        if helper.get("phase") != "closed":
            browser.close()
        stop_case_services_and_clone(spec, services)
        state["database_dropped_at"] = utc_now()
        workflow.record_product_failure(browser.case_dir, failure.code)
        destroy_sealed_private_values(browser)
        check = workflow.helper_check(
            helper_namespace(case_dir=browser.case_dir, case_id=spec.case_id)
        )
        if check.get("complete") is not True or check.get("product_failed") is not True:
            raise CaseError("workflow_product_failure_evidence_incomplete")
        update_preparation_outcome(spec, formal, "product_failed")
        state["phase"] = "product_failed"
        state["failed_at"] = utc_now()
        state["failure_code"] = failure.code
        state["resume_action"] = "record-result-then-prepare-a-new-attempt"
        state["evidence"] = check.get("evidence", [])
        save_state(spec, state)
        return {
            "case_id": spec.case_id,
            "status": "product_failed",
            "failure_code": failure.code,
            "attempt_dir": str(browser.case_dir),
            "database_dropped": True,
            "evidence_count": len(check.get("evidence", [])),
            "assertion_summary": {"total": 1, "passed": 0, "failed": 1},
        }
    except CaseError:
        raise
    except Exception as error:
        raise CaseError("workflow_product_failure_finalization_failed") from error


def execute_readonly_case(spec: CaseSpec, attempt_dir: pathlib.Path) -> dict[str, Any]:
    attempt = workflow.validate_attempt_dir(attempt_dir, spec.case_id)
    if spec.case_id not in READONLY_CASES:
        raise CaseError("workflow_readonly_case_not_allowed")
    owner_files = formal_owner_files()
    with lane_lock(spec), enforce_case_timeout(spec.case_id) as deadline:
        with workflow.formal_attempt_lease(
            spec.case_id,
            attempt,
            additional_harness_files=owner_files,
        ) as formal:
            state: Optional[dict[str, Any]] = None
            services: Optional[ServiceGroup] = None
            try:
                state = adopt_prepared_case(spec, attempt, formal)
                verify_prepared_execution_inputs(spec, state)
                services = ServiceGroup(spec, state)
                prepare_scenario_fixture(spec, state)
                browser_receipt = readonly_browser_plan(spec, state)
                services.start()
                state["phase"] = "scenario_running"
                state["scenario_started_at"] = utc_now()
                state["matrix_timeout_seconds"] = deadline.timeout_seconds
                state["cleanup_reserve_seconds"] = deadline.cleanup_reserve_seconds
                state["scenario_steps"] = [
                    {"name": "fixture_verified", "recorded_at": utc_now()}
                ]
                save_state(spec, state)

                database_inventory = None
                database = support.LocalPostgres(spec.database)
                if spec.case_id == "WFL-002":
                    database_inventory = wfl_002_database_inventory(database)
                verify_execution_inputs(state)
                result = workflow.run_readonly_session(
                    spec.case_id,
                    attempt,
                    spec.frontend_origin,
                    str(browser_receipt["session"]),
                    pathlib.Path(str(browser_receipt["scratch_dir"])),
                    workflow.DEFAULT_WRAPPER,
                    chromium_executable=pathlib.Path(str(state["chromium_executable"])),
                    chromium_bundle_sha256_value=str(state["chromium_bundle_sha256"]),
                    database_inventory=database_inventory,
                    lifecycle_callback=readonly_lifecycle_callback(spec, state),
                )
                verify_execution_inputs(state)
                status = "product_failed" if result.get("product_failed") is True else "passed"
                failure_code: Optional[str] = None
                if status == "passed":
                    if result.get("passed") is not True:
                        raise CaseError("workflow_readonly_result_invalid")
                    state["phase"] = "scenario_complete"
                    state["scenario_completed_at"] = utc_now()
                    for name in owner_checkpoints(spec.case_id)[1:-1]:
                        state["scenario_steps"].append({"name": name, "recorded_at": utc_now()})
                else:
                    failure_code = result.get("failure_code")
                    if (
                        result.get("passed") is not False
                        or not isinstance(failure_code, str)
                        or not SAFE_STEP_RE.fullmatch(failure_code)
                    ):
                        raise CaseError("workflow_readonly_result_invalid")
                    state["phase"] = "scenario_product_failed"
                    state["failure_checkpoint"] = "readonly_semantic_oracle"
                    state["failure_code"] = failure_code
                save_state(spec, state)

                deadline.enter_cleanup()
                stop_case_services_and_clone(spec, services)
                services = None
                state["database_dropped_at"] = utc_now()
                state["scenario_steps"].append(
                    {"name": "cleanup_verified", "recorded_at": utc_now()}
                )
                actual_steps = [str(item.get("name")) for item in state["scenario_steps"]]
                expected_steps = (
                    list(owner_checkpoints(spec.case_id))
                    if status == "passed"
                    else ["fixture_verified", "cleanup_verified"]
                )
                if actual_steps != expected_steps:
                    raise CaseError("workflow_case_checkpoint_contract_mismatch")
                workflow.validate_readonly_inventory(spec.case_id, attempt)
                if status == "product_failed":
                    update_preparation_outcome(spec, formal, "product_failed")
                    state["phase"] = "product_failed"
                    state["failed_at"] = utc_now()
                    state["failure_code"] = failure_code
                    state["resume_action"] = "record-result-then-prepare-a-new-attempt"
                else:
                    update_preparation_outcome(spec, formal, "complete")
                    state["phase"] = "complete"
                    state["completed_at"] = utc_now()
                state["evidence"] = workflow.inventory(attempt)
                save_state(spec, state)
                checkpoints = owner_checkpoints(spec.case_id)
                return {
                    "case_id": spec.case_id,
                    "status": status,
                    "failure_code": result.get("failure_code") if status == "product_failed" else None,
                    "attempt_dir": str(attempt),
                    "database_dropped": True,
                    "evidence_count": len(state["evidence"]),
                    "checkpoint_count": len(actual_steps),
                    "verified_checkpoints": actual_steps,
                    "matrix_timeout_seconds": deadline.timeout_seconds,
                    "cleanup_reserve_seconds": deadline.cleanup_reserve_seconds,
                    "assertion_summary": {
                        "total": len(checkpoints) if status == "passed" else 1,
                        "passed": len(checkpoints) if status == "passed" else 0,
                        "failed": 0 if status == "passed" else 1,
                    },
                }
            except CaseDeadlineExpired as error:
                if error.phase == "cleanup":
                    raise
                deadline.enter_cleanup()
                cleanup_failure: Optional[Exception] = None
                if state is not None and state.get("readonly_browser") is not None:
                    try:
                        reconcile_readonly_browser(spec, state)
                    except Exception as exc:  # noqa: BLE001 - bounded work-timeout cleanup
                        cleanup_failure = cleanup_failure or exc
                try:
                    stop_case_services_and_clone(spec, services)
                except Exception as exc:  # noqa: BLE001 - bounded work-timeout cleanup
                    cleanup_failure = cleanup_failure or exc
                if state is not None:
                    state["phase"] = "failed_cleaned" if cleanup_failure is None else "failed"
                    state["failed_at"] = utc_now()
                    state["failure_code"] = error.code
                    state["resume_action"] = "cleanup-case-then-retry-new-attempt"
                    state["database_dropped_at"] = utc_now() if cleanup_failure is None else None
                    save_state(spec, state)
                if cleanup_failure is not None:
                    raise CaseError("workflow_readonly_cleanup_failed") from cleanup_failure
                raise
            except Exception as error:
                deadline.enter_cleanup()
                cleanup_failure: Optional[Exception] = None
                if state is not None and state.get("readonly_browser") is not None:
                    try:
                        reconcile_readonly_browser(spec, state)
                    except Exception as exc:  # noqa: BLE001 - preserve cleanup failure
                        cleanup_failure = cleanup_failure or exc
                try:
                    stop_case_services_and_clone(spec, services)
                except Exception as exc:  # noqa: BLE001 - attempt browser and service cleanup
                    cleanup_failure = cleanup_failure or exc
                if state is not None:
                    state["phase"] = "failed_cleaned" if cleanup_failure is None else "failed"
                    state["failed_at"] = utc_now()
                    if isinstance(error, CaseError):
                        state["failure_code"] = error.code
                    elif isinstance(error, workflow.WorkflowError):
                        state["failure_code"] = "workflow_readonly_infrastructure_failed"
                    else:
                        state["failure_code"] = "workflow_case_unclassified_failure"
                    state["resume_action"] = "cleanup-case-then-retry-new-attempt"
                    state["database_dropped_at"] = utc_now() if cleanup_failure is None else None
                    save_state(spec, state)
                if cleanup_failure is not None:
                    if isinstance(cleanup_failure, CaseError):
                        raise cleanup_failure
                    raise CaseError("workflow_readonly_cleanup_failed") from cleanup_failure
                if isinstance(error, (CaseError, workflow.WorkflowError)):
                    raise error
                raise


def execute_mutation_case(spec: CaseSpec, attempt_dir: pathlib.Path) -> dict[str, Any]:
    if spec.case_id not in MUTATION_CASES:
        raise CaseError("workflow_case_not_automated")
    runner = CASE_RUNNERS.get(spec.case_id)
    if runner is None:
        raise CaseError("workflow_case_runner_missing")
    attempt = workflow.validate_attempt_dir(attempt_dir, spec.case_id)
    owner_files = formal_owner_files()
    with lane_lock(spec), enforce_case_timeout(spec.case_id) as deadline:
        with workflow.formal_attempt_lease(
            spec.case_id,
            attempt,
            additional_harness_files=owner_files,
        ) as formal:
            state: Optional[dict[str, Any]] = None
            services: Optional[ServiceGroup] = None
            browser: Optional[BrowserCase] = None
            initialized = False
            try:
                state = adopt_prepared_case(spec, attempt, formal)
                verify_prepared_execution_inputs(spec, state)
                services = ServiceGroup(spec, state)
                browser = BrowserCase(spec, state)
                prepare_scenario_fixture(spec, state)
                services.start()
                browser.initialize()
                initialized = True
                state["phase"] = "scenario_running"
                state["scenario_started_at"] = utc_now()
                state["matrix_timeout_seconds"] = deadline.timeout_seconds
                state["cleanup_reserve_seconds"] = deadline.cleanup_reserve_seconds
                save_state(spec, state)

                client = support.LocalHTTP(spec.backend_origin)
                database = support.LocalPostgres(spec.database)
                verify_execution_inputs(state)
                runner(browser, client, database)
                verify_execution_inputs(state)
                state["phase"] = "scenario_complete"
                state["scenario_completed_at"] = utc_now()
                save_state(spec, state)

                deadline.enter_cleanup()
                services.close()
                services = None
                state["phase"] = "services_stopped"
                state["services_stopped_at"] = utc_now()
                save_state(spec, state)

                support.run_lane(["drop-case", spec.database], ROOT)
                assert_case_database_absent(spec)
                state["phase"] = "database_dropped"
                state["database_dropped_at"] = utc_now()
                save_state(spec, state)
                browser.checkpoint("cleanup_verified")

                if browser.trace_active:
                    browser.stop_trace()
                browser.close()
                destroy_sealed_private_values(browser)
                check = workflow.helper_check(helper_namespace(case_dir=attempt, case_id=spec.case_id))
                if check.get("complete") is not True:
                    raise CaseError("workflow_case_evidence_incomplete")
                update_preparation_outcome(spec, formal, "complete")
                state["phase"] = "complete"
                state["completed_at"] = utc_now()
                state["evidence"] = check.get("evidence", [])
                state["scenario_steps"] = list(workflow.CASE_CHECKPOINTS[spec.case_id])
                save_state(spec, state)
                return {
                    "case_id": spec.case_id,
                    "status": "passed",
                    "attempt_dir": str(attempt),
                    "database_dropped": True,
                    "evidence_count": len(check.get("evidence", [])),
                    "checkpoint_count": len(workflow.CASE_CHECKPOINTS[spec.case_id]),
                    "matrix_timeout_seconds": deadline.timeout_seconds,
                    "cleanup_reserve_seconds": deadline.cleanup_reserve_seconds,
                    "assertion_summary": {
                        "total": len(workflow.CASE_CHECKPOINTS[spec.case_id]),
                        "passed": len(workflow.CASE_CHECKPOINTS[spec.case_id]),
                        "failed": 0,
                    },
                }
            except CaseDeadlineExpired as error:
                if error.phase == "cleanup":
                    raise
                deadline.enter_cleanup()
                cleanup_failure: Optional[Exception] = None
                if initialized and browser is not None:
                    try:
                        if browser.trace_active:
                            browser.stop_trace()
                        helper = workflow.load_state(attempt)
                        if helper.get("phase") != "closed" and helper.get("trace_started_ns") is None:
                            browser.close()
                    except Exception as exc:  # noqa: BLE001 - bounded work-timeout cleanup
                        cleanup_failure = cleanup_failure or exc
                try:
                    stop_case_services_and_clone(spec, services)
                except Exception as exc:  # noqa: BLE001 - bounded work-timeout cleanup
                    cleanup_failure = cleanup_failure or exc
                if state is not None:
                    state["phase"] = "failed_cleaned" if cleanup_failure is None else "failed"
                    state["failed_at"] = utc_now()
                    state["failure_code"] = error.code
                    state["resume_action"] = "cleanup-case-then-retry-new-attempt"
                    state["database_dropped_at"] = utc_now() if cleanup_failure is None else None
                    save_state(spec, state)
                if cleanup_failure is not None:
                    raise CaseError("workflow_mutation_cleanup_failed") from cleanup_failure
                raise
            except CaseError as exc:
                deadline.enter_cleanup()
                if exc.assertion_failure and initialized and state is not None and browser is not None:
                    return finalize_product_failure(spec, state, browser, services, formal, exc)
                cleanup_failure: Optional[Exception] = None
                if initialized and browser is not None:
                    try:
                        if browser.trace_active:
                            browser.stop_trace()
                        helper = workflow.load_state(attempt)
                        if helper.get("phase") != "closed" and helper.get("trace_started_ns") is None:
                            browser.close()
                    except Exception as error:  # noqa: BLE001 - continue owned cleanup
                        cleanup_failure = cleanup_failure or error
                try:
                    stop_case_services_and_clone(spec, services)
                except Exception as error:  # noqa: BLE001 - continue state finalization
                    cleanup_failure = cleanup_failure or error
                if state is not None:
                    state["phase"] = "failed_cleaned" if cleanup_failure is None else "failed"
                    state["failed_at"] = utc_now()
                    state["failure_code"] = exc.code
                    state["resume_action"] = "cleanup-case-then-retry-new-attempt"
                    state["database_dropped_at"] = utc_now() if cleanup_failure is None else None
                    save_state(spec, state)
                if cleanup_failure is not None:
                    raise CaseError("workflow_mutation_cleanup_failed") from cleanup_failure
                raise
            except Exception as exc:
                deadline.enter_cleanup()
                cleanup_failure = None
                if initialized and browser is not None:
                    try:
                        if browser.trace_active:
                            browser.stop_trace()
                        helper = workflow.load_state(attempt)
                        if helper.get("phase") != "closed" and helper.get("trace_started_ns") is None:
                            browser.close()
                    except Exception as error:  # noqa: BLE001 - continue owned cleanup
                        cleanup_failure = cleanup_failure or error
                try:
                    stop_case_services_and_clone(spec, services)
                except Exception as error:  # noqa: BLE001 - continue state finalization
                    cleanup_failure = cleanup_failure or error
                if state is not None:
                    state["phase"] = "failed_cleaned" if cleanup_failure is None else "failed"
                    state["failed_at"] = utc_now()
                    state["failure_code"] = "workflow_case_unclassified_failure"
                    state["resume_action"] = "cleanup-case-then-retry-new-attempt"
                    state["database_dropped_at"] = utc_now() if cleanup_failure is None else None
                    save_state(spec, state)
                if cleanup_failure is not None:
                    raise CaseError("workflow_mutation_cleanup_failed") from cleanup_failure
                raise exc


def execute_case(spec: CaseSpec, attempt_dir: pathlib.Path) -> dict[str, Any]:
    if spec.case_id not in AUTOMATED_CASES:
        raise CaseError("workflow_case_not_automated")
    if spec.case_id in READONLY_CASES:
        return execute_readonly_case(spec, attempt_dir)
    return execute_mutation_case(spec, attempt_dir)


def safe_owner_status(spec: CaseSpec) -> dict[str, Any]:
    state = load_state(spec, required=False)
    if state is None:
        receipt = load_preparation_receipt(spec, required=False)
        return {
            "case_id": spec.case_id,
            "phase": receipt.get("state") if receipt is not None else "unprepared",
            "database": spec.database,
            "database_clone_fingerprint": (
                receipt.get("database_clone_fingerprint") if receipt is not None else None
            ),
            "ports": {"backend": spec.backend_port, "frontend": spec.frontend_port, "mock": spec.mock_port},
        }
    helper_status: Optional[dict[str, Any]] = None
    attempt = pathlib.Path(str(state["attempt_dir"]))
    helper_path = workflow.state_path(attempt)
    if helper_path.is_file() and not helper_path.is_symlink():
        helper_status = workflow.helper_status(helper_namespace(case_dir=attempt))
    readonly_browser = state.get("readonly_browser")
    return {
        "case_id": spec.case_id,
        "phase": state.get("phase"),
        "attempt_dir": state.get("attempt_dir"),
        "database": spec.database,
        "database_clone_identity": state.get("database_clone_identity"),
        "ports": state.get("ports"),
        "scenario_steps": state.get("scenario_steps", []),
        "failure_code": state.get("failure_code"),
        "resume_action": state.get("resume_action"),
        "helper": helper_status,
        "readonly_browser": (
            {
                "phase": readonly_browser.get("phase"),
                "history_count": len(readonly_browser.get("history", [])),
            }
            if isinstance(readonly_browser, Mapping)
            else None
        ),
    }


def close_abandoned_browser(spec: CaseSpec, attempt: pathlib.Path) -> None:
    helper_path = workflow.state_path(attempt)
    if not helper_path.is_file() or helper_path.is_symlink():
        return
    state = workflow.load_state(attempt, verify_chromium=False)
    fixture = retention.as_dict(state.get("fixture"))
    scratch_path = pathlib.Path(str(state.get("scratch_dir", "")))
    scratch = scratch_path.resolve()
    expected_scratch = (lane_dir(spec) / "playwright").resolve()
    if (
        state.get("case_id") != spec.case_id
        or fixture.get("database_clone") != spec.database
        or state.get("base_url") != spec.frontend_origin
        or scratch != expected_scratch
        or scratch_path.is_symlink()
    ):
        raise CaseError("workflow_cleanup_browser_identity_mismatch")
    if state.get("phase") != "closed":
        workflow.close_persisted_session(attempt, workflow.DEFAULT_WRAPPER)
    if scratch.exists():
        if not scratch.is_dir() or scratch.is_symlink():
            raise CaseError("workflow_cleanup_browser_scratch_invalid")
        shutil.rmtree(scratch)


def cleanup_case(spec: CaseSpec, attempt_dir: pathlib.Path) -> dict[str, Any]:
    attempt = workflow.validate_attempt_dir(attempt_dir, spec.case_id)
    archive_target: Optional[pathlib.Path] = None
    with lane_lock(spec):
        state = load_state(spec, verify_inputs=False)
        if state.get("attempt_dir") != str(attempt):
            raise CaseError("workflow_case_attempt_identity_mismatch")
        if spec.case_id in READONLY_CASES:
            if state.get("readonly_browser") is not None:
                reconcile_readonly_browser(spec, state)
            else:
                scratch = lane_dir(spec) / "playwright"
                if scratch.exists() or scratch.is_symlink():
                    raise CaseError("workflow_readonly_browser_receipt_missing")
        else:
            close_abandoned_browser(spec, attempt)
        process_dir = lane_dir(spec) / "processes"
        for name in ("frontend", "backend", "mock"):
            stop_stale_owned_process(process_dir / (name + ".json"), name)
        require_ports_available(spec)
        if case_database_exists(spec):
            support.run_lane(["drop-case", spec.database], ROOT)
        assert_case_database_absent(spec)
        state["phase"] = "cleaned"
        state["cleaned_at"] = utc_now()
        save_state(spec, state)
        archive_root = PRIVATE_ROOT / "archive"
        archive_root.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(archive_root, 0o700)
        archive_target = archive_root / (spec.slug + "-" + str(time.time_ns()))
    if archive_target is None or archive_target.exists() or archive_target.is_symlink():
        raise CaseError("workflow_case_archive_target_invalid")
    os.replace(lane_dir(spec), archive_target)
    return {
        "case_id": spec.case_id,
        "status": "cleaned",
        "database_dropped": True,
        "private_lane_archived": True,
    }


def case_contract(spec: CaseSpec) -> dict[str, Any]:
    validate_ports(spec)
    checkpoints = owner_checkpoints(spec.case_id)
    expected_steps = checkpoints[1:]
    if spec.scenario_steps != expected_steps:
        raise CaseError("workflow_case_checkpoint_contract_mismatch")
    return {
        "schema_version": SCHEMA_VERSION,
        "owner_version": OWNER_VERSION,
        "run_id": RUN_ID,
        "matrix_sha256": workflow.MATRIX_SHA256,
        "case_id": spec.case_id,
        "timeout_seconds": frozen_case_timeout(spec.case_id),
        "automated": spec.case_id in AUTOMATED_CASES,
        "database": spec.database,
        "ports": {"backend": spec.backend_port, "frontend": spec.frontend_port, "mock": spec.mock_port},
        "reserved_ports_avoided": not any(
            value in RESERVED_PORTS
            for value in (spec.backend_port, spec.frontend_port, spec.mock_port)
            if value is not None
        ),
        "checkpoints": list(checkpoints),
        "required_evidence": list(workflow.REQUIRED_EVIDENCE[spec.case_id]),
        "sensitive_value_count": len(spec.sensitive_value_labels),
        "fixture_scope": "disposable_case_clone",
        "runner_state_mutation": False,
    }


def self_test() -> dict[str, Any]:
    workflow.frozen_workflow_contract()
    fingerprints = pinned_input_fingerprints()
    if not {"workflow_owner", "workflow_helper", "local_support", "retention_helper"} <= set(fingerprints):
        raise CaseError("workflow_case_pinned_input_set_incomplete")
    if set(CASE_SPECS) != set(ALLOWED_CASES):
        raise CaseError("workflow_case_spec_set_mismatch")
    databases: set[str] = set()
    ports: set[int] = set()
    for case_id in ALLOWED_CASES:
        spec = CASE_SPECS[case_id]
        contract = case_contract(spec)
        if contract["required_evidence"] != list(workflow.REQUIRED_EVIDENCE[case_id]):
            raise CaseError("workflow_case_evidence_contract_mismatch")
        if spec.database in databases:
            raise CaseError("workflow_case_database_overlap")
        databases.add(spec.database)
        for port in (spec.backend_port, spec.frontend_port, spec.mock_port):
            if port is None:
                continue
            if port in ports:
                raise CaseError("workflow_case_port_overlap")
            ports.add(port)
    if set(ports) & RESERVED_PORTS:
        raise CaseError("workflow_reserved_port_collision")
    sample = action_for_origin("http://127.0.0.1:18203", "return {surface: 'self_test'};")
    if "http://127.0.0.1:18203" not in sample or "network_events" not in sample or "response.body" in sample:
        raise CaseError("workflow_browser_projection_contract_invalid")
    if set(CASE_RUNNERS) != set(MUTATION_CASES):
        raise CaseError("workflow_automated_runner_set_mismatch")
    return {
        "status": "passed",
        "case_count": len(CASE_SPECS),
        "automated_case_count": len(AUTOMATED_CASES),
        "mutation_runner_count": len(CASE_RUNNERS),
        "pinned_input_count": len(fingerprints),
        "matrix_sha256": workflow.MATRIX_SHA256,
        "reserved_ports_avoided": True,
        "live_services_started": False,
        "runner_state_mutated": False,
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("self-test", help="validate the frozen static owner contract only")

    contract = subparsers.add_parser("contract", help="print one exact case contract without mutation")
    contract.add_argument("--case-id", required=True, choices=ALLOWED_CASES)

    status = subparsers.add_parser("status", help="print the persisted owner/helper checkpoint state")
    status.add_argument("--case-id", required=True, choices=ALLOWED_CASES)

    prepare = subparsers.add_parser(
        "prepare-case",
        help="prepare and fingerprint one clone before formal runner allocation",
    )
    prepare.add_argument("--case-id", required=True, choices=ALLOWED_CASES)

    run = subparsers.add_parser("run-case", help="execute one automated case in its disposable local fixture")
    run.add_argument("--case-id", required=True, choices=ALLOWED_CASES)
    run.add_argument("--attempt-dir", required=True, type=pathlib.Path)

    cleanup = subparsers.add_parser("cleanup-case", help="stop and archive one exact failed case fixture")
    cleanup.add_argument("--case-id", required=True, choices=ALLOWED_CASES)
    cleanup.add_argument("--attempt-dir", required=True, type=pathlib.Path)
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = build_parser().parse_args(argv)
    exit_code = 0
    try:
        if args.command == "self-test":
            result = self_test()
        else:
            spec = safe_case(args.case_id)
            if args.command == "contract":
                result = case_contract(spec)
            elif args.command == "status":
                result = safe_owner_status(spec)
            elif args.command == "prepare-case":
                with lane_lock(spec), workflow.formal_preparation_lease(
                    spec.case_id,
                    additional_harness_files=formal_owner_files(),
                ) as formal:
                    result = prepare_case(spec)
                    if result.get("branch_head") != formal.branch_head:
                        raise CaseError("workflow_preparation_head_mismatch")
            elif args.command == "run-case":
                result = execute_case(spec, args.attempt_dir)
                if result.get("status") == "product_failed":
                    exit_code = 1
            elif args.command == "cleanup-case":
                result = cleanup_case(spec, args.attempt_dir)
            else:
                raise CaseError("workflow_command_not_supported")
        workflow.assert_safe_json(result, "workflow_owner_result")
        print(json.dumps(result, ensure_ascii=False, sort_keys=True))
        return exit_code
    except CaseDeadlineExpired as exc:
        print(json.dumps({"status": "failed", "code": exc.code}, sort_keys=True))
        return 2
    except CaseError as exc:
        print(json.dumps({"status": "failed", "code": exc.code}, sort_keys=True))
        return 1 if exc.assertion_failure else 2
    except (workflow.WorkflowError, support.HarnessError):
        code = "workflow_case_owner_rejected"
        print(json.dumps({"status": "failed", "code": code}, sort_keys=True))
        return 2
    except Exception:  # noqa: BLE001 - public CLI must fail closed without leaking details
        print(json.dumps({"status": "failed", "code": "workflow_case_owner_internal_error"}, sort_keys=True))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
