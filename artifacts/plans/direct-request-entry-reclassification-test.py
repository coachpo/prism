#!/usr/bin/env python3
"""Disposable PostgreSQL acceptance runner for the C7 reclassification plan."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable


REPO_ROOT = Path(__file__).resolve().parents[2]
PLAN_SQL = REPO_ROOT / "artifacts/plans/direct-request-entry-reclassification.sql"
RUNBOOK = REPO_ROOT / "artifacts/plans/direct-request-entry-reclassification.md"
STATUS = REPO_ROOT / "STATUS.md"
CANONICAL_PATHS = (
    "artifacts/plans/direct-request-entry-reclassification.md",
    "artifacts/plans/direct-request-entry-reclassification.sql",
    "artifacts/plans/direct-request-entry-reclassification-test.py",
)
EXPECTED_SQL_SHA256 = "d4afecb631dc70fa974d96cf0a877958ee3187c64675092260c6729b0fd396d0"
POSTGRES_IMAGE = "postgres:16-alpine"
DATABASE = "prism_c7"
DATABASE_USER = "postgres"
DATABASE_PASSWORD = "c7-disposable-only"

DIRECT_MODELS = (
    "codex/codex-auto-review",
    "codex/gpt-5.6-terra",
    "deepseek-v4-flash",
    "deepseek-v4-pro",
    "glm-5.3-flash",
    "codex/gpt-image-2",
    "codex/gpt-5.4-mini",
    "codex/gpt-5.5",
    "codex/gpt-5.6-luna",
    "gpt-5.6-luna",
    "muse-spark-1.2-contributor",
    "qwen3.8-flash",
)
INTERNAL_MODELS = (
    "DeepSeek-V4-Flash",
    "deepseek/deepseek-v4-flash-0731",
    "deepseek/deepseek-v4-pro",
    "z-ai/glm-5.3-flash",
)
EXPECTED_EDGES = (
    ("deepseek-v4-flash", "DeepSeek-V4-Flash", 1),
    ("deepseek-v4-flash", "deepseek/deepseek-v4-flash-0731", 2),
    ("deepseek-v4-pro", "deepseek/deepseek-v4-pro", 1),
    ("glm-5.3-flash", "z-ai/glm-5.3-flash", 1),
)
MODEL_IDS = {model_id: index for index, model_id in enumerate((*DIRECT_MODELS, *INTERNAL_MODELS), 1)}


SCHEMA_SQL = r"""
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;

CREATE TABLE profiles (
  id integer PRIMARY KEY,
  is_default boolean NOT NULL,
  deleted_at timestamptz
);

CREATE TABLE prism_schema_migrations (
  version varchar(255) PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE model_configs (
  id integer PRIMARY KEY,
  profile_id integer NOT NULL,
  api_family varchar(50) NOT NULL,
  model_id varchar(200) NOT NULL,
  openai_accepted_format text,
  openai_image_operations text,
  direct_request_enabled boolean NOT NULL DEFAULT true,
  is_enabled boolean NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (profile_id, model_id)
);

CREATE TABLE connections (
  id integer PRIMARY KEY,
  profile_id integer NOT NULL,
  is_active boolean NOT NULL,
  upstream_model_id varchar(200)
);

CREATE TABLE model_access_targets (
  id serial PRIMARY KEY,
  profile_id integer NOT NULL,
  source_model_config_id integer NOT NULL,
  target_type varchar(20) NOT NULL,
  target_model_config_id integer,
  target_connection_id integer,
  position integer NOT NULL,
  is_enabled boolean NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT uq_c7_source_position UNIQUE (source_model_config_id, position) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX uq_c7_source_target_model
  ON model_access_targets (source_model_config_id, target_model_config_id)
  WHERE target_model_config_id IS NOT NULL;

CREATE TABLE runtime_cache_generations (
  domain text NOT NULL,
  scope_type text NOT NULL,
  scope_id text NOT NULL,
  version bigint NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by text,
  reason text,
  PRIMARY KEY (domain, scope_type, scope_id)
);

CREATE TABLE route_witness_generations (
  profile_id integer PRIMARY KEY,
  generation bigint NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE request_logs (id integer PRIMARY KEY, payload text NOT NULL);
CREATE TABLE usage_request_events (id integer PRIMARY KEY, payload text NOT NULL);
CREATE TABLE model_catalog_bindings (id integer PRIMARY KEY, payload text NOT NULL);
CREATE TABLE model_pi_catalog_bindings (id integer PRIMARY KEY, payload text NOT NULL);
CREATE TABLE pricing_templates (id integer PRIMARY KEY, payload text NOT NULL);
"""


SNAPSHOT_SQL = r"""
SELECT jsonb_build_object(
  'profiles', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM profiles AS t),
  'migrations', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY version), '[]'::jsonb) FROM prism_schema_migrations AS t),
  'models', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM model_configs AS t),
  'connections', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM connections AS t),
  'targets', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM model_access_targets AS t),
  'runtime_generations', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY domain, scope_type, scope_id), '[]'::jsonb) FROM runtime_cache_generations AS t),
  'witness_generations', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY profile_id), '[]'::jsonb) FROM route_witness_generations AS t),
  'request_logs', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM request_logs AS t),
  'usage_events', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM usage_request_events AS t),
  'modelsdev_bindings', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM model_catalog_bindings AS t),
  'pi_bindings', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM model_pi_catalog_bindings AS t),
  'pricing', (SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id), '[]'::jsonb) FROM pricing_templates AS t),
  'target_sequence', (SELECT jsonb_build_object('last_value', last_value, 'is_called', is_called) FROM model_access_targets_id_seq)
)::text;
"""


@dataclass(frozen=True)
class ProcessResult:
    returncode: int
    stdout: str
    stderr: str


def run_process(argv: list[str], *, input_text: str | None = None, timeout: int = 120) -> ProcessResult:
    completed = subprocess.run(
        argv,
        input=input_text,
        text=True,
        capture_output=True,
        timeout=timeout,
        check=False,
    )
    return ProcessResult(completed.returncode, completed.stdout, completed.stderr)


def require_success(result: ProcessResult, label: str) -> ProcessResult:
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise AssertionError(f"{label} failed: {detail[-2000:]}")
    return result


def file_sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def require_canonical_source_contract() -> dict[str, str]:
    root_result = require_success(
        run_process(["git", "-C", str(REPO_ROOT), "rev-parse", "--show-toplevel"]),
        "resolve repository root",
    )
    if Path(root_result.stdout.strip()).resolve() != REPO_ROOT:
        raise AssertionError("runner is not bound to the repository worktree containing its canonical assets")

    for relative in CANONICAL_PATHS:
        result = run_process(
            ["git", "-C", str(REPO_ROOT), "ls-files", "--error-unmatch", "--", relative]
        )
        if result.returncode != 0:
            raise AssertionError(f"canonical C7 asset is not Git-visible: {relative}")

    status_text = STATUS.read_text(encoding="utf-8")
    status_lines = [line for line in status_text.splitlines() if "12-entry/four-mapping" in line]
    if len(status_lines) != 1:
        raise AssertionError("STATUS.md must contain exactly one 12-entry/four-mapping ownership statement")
    status_line = status_lines[0]
    if "Git-ignored" in status_line or "not shipped in release source" in status_line:
        raise AssertionError("STATUS.md still claims the canonical C7 operator bundle is ignored or unshipped")

    sql_text = PLAN_SQL.read_text(encoding="utf-8")
    runbook_text = RUNBOOK.read_text(encoding="utf-8")
    sql_hash = file_sha256(PLAN_SQL)
    if sql_hash != EXPECTED_SQL_SHA256:
        raise AssertionError(f"reviewed SQL hash changed: {sql_hash}")
    if f"SQL SHA-256: {sql_hash}" not in runbook_text:
        raise AssertionError("runbook does not bind the reviewed SQL SHA-256")
    if CANONICAL_PATHS[1] not in runbook_text or CANONICAL_PATHS[2] not in runbook_text:
        raise AssertionError("runbook does not name both canonical SQL and acceptance runner paths")

    for model_id in (*DIRECT_MODELS, *INTERNAL_MODELS):
        if model_id not in sql_text or model_id not in runbook_text:
            raise AssertionError(f"canonical SQL/runbook identity set omits {model_id}")
    for parent, child, append_order in EXPECTED_EDGES:
        sql_edge = f"('{parent}', '{child}', {append_order})"
        if sql_edge not in sql_text:
            raise AssertionError(f"canonical SQL omits parent-to-child edge {parent} -> {child}")
        if not any(parent in line and child in line and "Model Target" in line for line in runbook_text.splitlines()):
            raise AssertionError(f"runbook omits parent-to-child edge {parent} -> {child}")

    for token in (
        "APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1",
        "BACKUP_VERIFIED",
        "PRISM_STOPPED",
        "BEGIN ISOLATION LEVEL SERIALIZABLE READ WRITE",
        "BEGIN ISOLATION LEVEL SERIALIZABLE READ ONLY",
    ):
        if token not in sql_text:
            raise AssertionError(f"canonical SQL omits safety token {token}")

    return {
        "runbook": file_sha256(RUNBOOK),
        "sql": sql_hash,
        "runner": file_sha256(Path(__file__).resolve()),
    }


def sql_literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


class DisposablePostgres:
    def __init__(self) -> None:
        self.name = f"prism-c7-{os.getpid()}-{uuid.uuid4().hex[:10]}"
        self.started = False

    def __enter__(self) -> "DisposablePostgres":
        result = run_process(
            [
                "docker", "run", "--detach", "--rm", "--name", self.name,
                "--env", f"POSTGRES_PASSWORD={DATABASE_PASSWORD}",
                "--env", f"POSTGRES_DB={DATABASE}",
                POSTGRES_IMAGE,
            ],
            timeout=120,
        )
        require_success(result, "start disposable PostgreSQL")
        self.started = True
        deadline = time.monotonic() + 60
        last = result
        while time.monotonic() < deadline:
            logs = run_process(["docker", "logs", self.name], timeout=10)
            last = run_process(
                ["docker", "exec", self.name, "pg_isready", "--username", DATABASE_USER, "--dbname", DATABASE],
                timeout=10,
            )
            initialized = "PostgreSQL init process complete" in (logs.stdout + logs.stderr)
            if initialized and last.returncode == 0:
                return self
            time.sleep(0.25)
        raise AssertionError(f"disposable PostgreSQL did not become ready: {(last.stderr or last.stdout).strip()}")

    def __exit__(self, _type: object, _value: object, _traceback: object) -> None:
        if self.started:
            run_process(["docker", "rm", "--force", self.name], timeout=30)

    def psql(
        self,
        *,
        command: str | None = None,
        input_text: str | None = None,
        variables: dict[str, str] | None = None,
        tuples_only: bool = False,
        timeout: int = 120,
    ) -> ProcessResult:
        argv = [
            "docker", "exec", "--interactive", self.name,
            "psql", "--no-psqlrc", "--set", "ON_ERROR_STOP=1",
            "--username", DATABASE_USER, "--dbname", DATABASE,
        ]
        if tuples_only:
            argv.extend(["--tuples-only", "--no-align", "--quiet"])
        for key, value in sorted((variables or {}).items()):
            argv.extend(["--set", f"{key}={value}"])
        if command is not None:
            argv.extend(["--command", command])
        return run_process(argv, input_text=input_text, timeout=timeout)

    def reset(self, *, existing_edges: bool) -> None:
        require_success(self.psql(command=SCHEMA_SQL), "reset disposable schema")
        require_success(self.psql(command=seed_sql(existing_edges=existing_edges)), "seed disposable fixture")

    def snapshot(self) -> dict[str, Any]:
        result = require_success(self.psql(command=SNAPSHOT_SQL, tuples_only=True), "snapshot disposable state")
        payload = result.stdout.strip()
        if not payload:
            raise AssertionError("state snapshot was empty")
        return json.loads(payload)

    def run_plan(self, *, apply: bool, overrides: dict[str, str] | None = None) -> ProcessResult:
        variables: dict[str, str] = {}
        if apply:
            variables = {
                "apply_token": "APPLY_DIRECT_ENTRY_RECLASSIFICATION_V1",
                "backup_token": "BACKUP_VERIFIED",
                "quiesce_token": "PRISM_STOPPED",
                "expected_database": DATABASE,
            }
        variables.update(overrides or {})
        return self.psql(input_text=PLAN_SQL.read_text(encoding="utf-8"), variables=variables, timeout=180)


def seed_sql(*, existing_edges: bool) -> str:
    timestamp = "2026-09-02 00:00:00+00"
    rows: list[str] = []
    for model_id, model_config_id in MODEL_IDS.items():
        accepted = "NULL" if model_id == "codex/gpt-image-2" else "'dual_native'"
        image = "'generations_and_edits'" if model_id == "codex/gpt-image-2" else "NULL"
        enabled = "false" if model_id in {"codex/gpt-image-2", "gpt-5.6-luna"} else "true"
        rows.append(
            f"({model_config_id}, 1, 'openai', {sql_literal(model_id)}, {accepted}, {image}, true, {enabled}, {sql_literal(timestamp)})"
        )
    statements = [
        "INSERT INTO profiles (id, is_default, deleted_at) VALUES (1, true, NULL);",
        "INSERT INTO prism_schema_migrations (version) VALUES ('000032_model_direct_request_enabled');",
        "INSERT INTO model_configs (id, profile_id, api_family, model_id, openai_accepted_format, openai_image_operations, direct_request_enabled, is_enabled, updated_at) VALUES\n  "
        + ",\n  ".join(rows)
        + ";",
        "INSERT INTO connections (id, profile_id, is_active, upstream_model_id) VALUES\n"
        "  (101, 1, true, 'deepseek-v4-flash'),\n"
        "  (102, 1, true, 'DeepSeek-V4-Flash'),\n"
        "  (103, 1, true, 'deepseek/deepseek-v4-flash-0731'),\n"
        "  (104, 1, true, 'deepseek-v4-pro'),\n"
        "  (105, 1, true, 'glm-5.3-flash');",
        "INSERT INTO model_access_targets (id, profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES\n"
        f"  (201, 1, {MODEL_IDS['deepseek-v4-flash']}, 'connection', 101, 0, true, {sql_literal(timestamp)}, {sql_literal(timestamp)}),\n"
        f"  (202, 1, {MODEL_IDS['DeepSeek-V4-Flash']}, 'connection', 102, 0, true, {sql_literal(timestamp)}, {sql_literal(timestamp)}),\n"
        f"  (203, 1, {MODEL_IDS['deepseek/deepseek-v4-flash-0731']}, 'connection', 103, 0, true, {sql_literal(timestamp)}, {sql_literal(timestamp)}),\n"
        f"  (204, 1, {MODEL_IDS['deepseek-v4-pro']}, 'connection', 104, 0, true, {sql_literal(timestamp)}, {sql_literal(timestamp)}),\n"
        f"  (205, 1, {MODEL_IDS['glm-5.3-flash']}, 'connection', 105, 0, true, {sql_literal(timestamp)}, {sql_literal(timestamp)});",
        "INSERT INTO runtime_cache_generations (domain, scope_type, scope_id, version, updated_at, updated_by, reason) VALUES\n"
        f"  ('profile_runtime', 'global', '*', 7, {sql_literal(timestamp)}, NULL, 'fixture'),\n"
        f"  ('runtime_planning', 'global', '*', 11, {sql_literal(timestamp)}, NULL, 'fixture'),\n"
        f"  ('profile_runtime', 'profile', '1', 13, {sql_literal(timestamp)}, NULL, 'fixture'),\n"
        f"  ('runtime_planning', 'profile', '1', 17, {sql_literal(timestamp)}, NULL, 'fixture'),\n"
        f"  ('auth', 'global', '*', 19, {sql_literal(timestamp)}, NULL, 'fixture');",
        f"INSERT INTO route_witness_generations (profile_id, generation, updated_at) VALUES (1, 23, {sql_literal(timestamp)});",
        "INSERT INTO request_logs VALUES (1, 'request-log-sentinel');",
        "INSERT INTO usage_request_events VALUES (1, 'usage-event-sentinel');",
        "INSERT INTO model_catalog_bindings VALUES (1, 'modelsdev-sentinel');",
        "INSERT INTO model_pi_catalog_bindings VALUES (1, 'pi-sentinel');",
        "INSERT INTO pricing_templates VALUES (1, 'pricing-sentinel');",
    ]
    if existing_edges:
        edge_rows = []
        for offset, (parent, child, position) in enumerate(EXPECTED_EDGES, 301):
            edge_rows.append(
                f"({offset}, 1, {MODEL_IDS[parent]}, 'model', {MODEL_IDS[child]}, {position}, true, {sql_literal(timestamp)}, {sql_literal(timestamp)})"
            )
        statements.append(
            "INSERT INTO model_access_targets (id, profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES\n  "
            + ",\n  ".join(edge_rows)
            + ";"
        )
    return "\n".join(statements)


def rows_by_key(rows: list[dict[str, Any]], *keys: str) -> dict[tuple[Any, ...], dict[str, Any]]:
    return {tuple(row[key] for key in keys): row for row in rows}


def assert_direct_bits(snapshot: dict[str, Any]) -> None:
    actual = {row["model_id"]: row["direct_request_enabled"] for row in snapshot["models"]}
    expected = {model_id: True for model_id in DIRECT_MODELS} | {model_id: False for model_id in INTERNAL_MODELS}
    if actual != expected:
        raise AssertionError(f"direct-entry set differs: {actual}")


def assert_only_direct_bits_changed(before: dict[str, Any], after: dict[str, Any]) -> None:
    before_models = rows_by_key(before["models"], "id")
    after_models = rows_by_key(after["models"], "id")
    if before_models.keys() != after_models.keys():
        raise AssertionError("model identity set changed")
    for key in before_models:
        left = dict(before_models[key])
        right = dict(after_models[key])
        left.pop("direct_request_enabled")
        right.pop("direct_request_enabled")
        if left != right:
            raise AssertionError(f"model fields other than direct_request_enabled changed for {key}")
    for field in (
        "profiles", "migrations", "connections", "request_logs", "usage_events",
        "modelsdev_bindings", "pi_bindings", "pricing",
    ):
        if before[field] != after[field]:
            raise AssertionError(f"unrelated state changed: {field}")


def generation_map(snapshot: dict[str, Any]) -> dict[tuple[str, str, str], int]:
    return {(row["domain"], row["scope_type"], row["scope_id"]): row["version"] for row in snapshot["runtime_generations"]}


def assert_generation_delta(before: dict[str, Any], after: dict[str, Any], delta: int) -> None:
    expected_keys = {
        ("profile_runtime", "global", "*"),
        ("runtime_planning", "global", "*"),
        ("profile_runtime", "profile", "1"),
        ("runtime_planning", "profile", "1"),
    }
    left = generation_map(before)
    right = generation_map(after)
    if left.keys() != right.keys():
        raise AssertionError("runtime generation scope set changed")
    for key in left:
        want = left[key] + (delta if key in expected_keys else 0)
        if right[key] != want:
            raise AssertionError(f"runtime generation {key}={right[key]}, want {want}")
    before_witness = before["witness_generations"][0]["generation"]
    after_witness = after["witness_generations"][0]["generation"]
    if after_witness != before_witness + delta:
        raise AssertionError(f"route witness generation={after_witness}, want {before_witness + delta}")


def assert_existing_success(pg: DisposablePostgres) -> None:
    pg.reset(existing_edges=True)
    before = pg.snapshot()
    result = require_success(pg.run_plan(apply=True), "apply existing-edge plan")
    if "model_qualification" not in result.stdout or " 4" not in result.stdout:
        raise AssertionError("apply did not report four model qualification changes")
    after = pg.snapshot()
    assert_direct_bits(after)
    assert_only_direct_bits_changed(before, after)
    if before["targets"] != after["targets"] or before["target_sequence"] != after["target_sequence"]:
        raise AssertionError("existing-edge apply changed an access target or its sequence")
    assert_generation_delta(before, after, 1)
    second_before = pg.snapshot()
    second = require_success(pg.run_plan(apply=True), "second existing-edge apply")
    second_after = pg.snapshot()
    if second_before != second_after:
        raise AssertionError("second existing-edge apply was not a true no-op")
    if "model_qualification" in second.stdout or "model_target_append" in second.stdout:
        raise AssertionError("second existing-edge apply reported a change")


def assert_missing_success(pg: DisposablePostgres) -> None:
    pg.reset(existing_edges=False)
    before = pg.snapshot()
    require_success(pg.run_plan(apply=True), "apply missing-edge plan")
    after = pg.snapshot()
    assert_direct_bits(after)
    assert_only_direct_bits_changed(before, after)
    before_targets = rows_by_key(before["targets"], "id")
    after_targets = rows_by_key(after["targets"], "id")
    for key, row in before_targets.items():
        if after_targets.get(key) != row:
            raise AssertionError(f"pre-existing target {key} changed while appending")
    model_by_name = {row["model_id"]: row["id"] for row in after["models"]}
    expected = {(model_by_name[parent], model_by_name[child]): position for parent, child, position in EXPECTED_EDGES}
    observed: dict[tuple[int, int], int] = {}
    for row in after["targets"]:
        if row["target_model_config_id"] is None:
            continue
        if row["target_type"] != "model" or not row["is_enabled"]:
            raise AssertionError("new relationship was not an enabled Model Target")
        observed[(row["source_model_config_id"], row["target_model_config_id"])] = row["position"]
    if observed != expected:
        raise AssertionError(f"parent-to-child edges differ: {observed}")
    if len(after["targets"]) != len(before["targets"]) + 4:
        raise AssertionError("missing-edge apply did not append exactly four rows")
    assert_generation_delta(before, after, 1)
    second_before = pg.snapshot()
    require_success(pg.run_plan(apply=True), "second missing-edge apply")
    if second_before != pg.snapshot():
        raise AssertionError("second missing-edge apply was not a true no-op")


def assert_preview(pg: DisposablePostgres) -> None:
    pg.reset(existing_edges=True)
    before = pg.snapshot()
    result = require_success(pg.run_plan(apply=False), "read-only preview")
    after = pg.snapshot()
    if before != after:
        raise AssertionError("preview changed persistent disposable state")
    if "will_update" not in result.stdout or "deepseek-v4-flash" not in result.stdout:
        raise AssertionError("preview omitted qualification or mapping evidence")


def assert_failure_atomic(
    pg: DisposablePostgres,
    *,
    name: str,
    mutation: str | None,
    expected_error: str,
    existing_edges: bool = True,
    variable_overrides: dict[str, str] | None = None,
) -> None:
    pg.reset(existing_edges=existing_edges)
    if mutation:
        require_success(pg.psql(command=mutation), f"prepare {name}")
    before = pg.snapshot()
    result = pg.run_plan(apply=True, overrides=variable_overrides)
    if result.returncode == 0:
        raise AssertionError(f"{name} unexpectedly succeeded")
    combined = result.stdout + "\n" + result.stderr
    if expected_error not in combined:
        raise AssertionError(f"{name} failed with wrong reason: {combined[-2000:]}")
    after = pg.snapshot()
    if before != after:
        raise AssertionError(f"{name} changed persistent business or generation state")


def run_case(name: str, fn: Callable[[], None], passed: list[str]) -> None:
    fn()
    passed.append(name)
    print(f"PASS {name}", flush=True)


def write_evidence(
    status: str,
    passed: list[str],
    source_hashes: dict[str, str],
    error: str | None = None,
) -> None:
    directory = os.environ.get("CLOSED_LOOP_EVIDENCE_DIR")
    if not directory:
        return
    target = Path(directory)
    target.mkdir(parents=True, exist_ok=True)
    payload: dict[str, Any] = {
        "schemaVersion": 1,
        "caseId": os.environ.get("CLOSED_LOOP_CASE_ID", "c7-controlled-reclassification-plan"),
        "status": status,
        "postgresImage": POSTGRES_IMAGE,
        "passedScenarios": passed,
        "sourceSha256": source_hashes,
    }
    if error:
        payload["error"] = error
    (target / "c7-summary.json").write_text(
        json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")) + "\n",
        encoding="utf-8",
    )


def main() -> int:
    passed: list[str] = []
    source_hashes: dict[str, str] = {}
    try:
        source_hashes = require_canonical_source_contract()
        passed.append("canonical-git-visible-source-contract")
        print("PASS canonical-git-visible-source-contract", flush=True)
        with DisposablePostgres() as pg:
            run_case("preview-read-only", lambda: assert_preview(pg), passed)
            run_case("existing-edges-apply-and-noop", lambda: assert_existing_success(pg), passed)
            run_case("missing-edges-append-and-noop", lambda: assert_missing_success(pg), passed)
            failures = (
                ("backup-token", None, "apply refused: pass -v backup_token=BACKUP_VERIFIED", True, {"backup_token": "UNVERIFIED"}),
                ("quiesce-token", None, "apply refused: stop Prism", True, {"quiesce_token": "RUNNING"}),
                ("database-token", None, "apply refused: -v expected_database", True, {"expected_database": "wrong_database"}),
                ("migration-history", "DELETE FROM prism_schema_migrations;", "migration history does not contain", True, None),
                ("default-profile", "UPDATE profiles SET is_default = false WHERE id = 1;", "profile 1 is not the live Default profile", True, None),
                ("inventory-missing", f"DELETE FROM model_configs WHERE id = {MODEL_IDS['qwen3.8-flash']};", "must contain exactly 16", True, None),
                ("inventory-extra", "INSERT INTO model_configs VALUES (99, 1, 'openai', 'unexpected-model', 'dual_native', NULL, true, true, now());", "must contain exactly 16", True, None),
                ("family-mismatch", f"UPDATE model_configs SET api_family = 'anthropic' WHERE id = {MODEL_IDS['DeepSeek-V4-Flash']};", "profile/family/OpenAI dimensions", True, None),
                ("text-mode-mismatch", f"UPDATE model_configs SET openai_accepted_format = 'responses_only' WHERE id = {MODEL_IDS['DeepSeek-V4-Flash']};", "profile/family/OpenAI dimensions", True, None),
                ("image-mode-mismatch", f"UPDATE model_configs SET openai_image_operations = 'generations' WHERE id = {MODEL_IDS['DeepSeek-V4-Flash']};", "profile/family/OpenAI dimensions", True, None),
                ("duplicate-edge", f"DROP INDEX uq_c7_source_target_model; INSERT INTO model_access_targets (id, profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES (399, 1, {MODEL_IDS['deepseek-v4-flash']}, 'model', {MODEL_IDS['DeepSeek-V4-Flash']}, 3, true, now(), now());", "required parent-to-child Model Target is duplicated", True, None),
                ("disabled-existing-edge", f"UPDATE model_access_targets SET is_enabled = false WHERE source_model_config_id = {MODEL_IDS['deepseek-v4-flash']} AND target_model_config_id = {MODEL_IDS['DeepSeek-V4-Flash']};", "existing required edge is not an enabled Model Target", True, None),
                ("position-gap", f"UPDATE model_access_targets SET position = 4 WHERE source_model_config_id = {MODEL_IDS['deepseek-v4-flash']} AND target_model_config_id = {MODEL_IDS['deepseek/deepseek-v4-flash-0731']};", "dense unique positions", True, None),
                ("prospective-cycle", f"INSERT INTO model_access_targets (id, profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES (398, 1, {MODEL_IDS['DeepSeek-V4-Flash']}, 'model', {MODEL_IDS['deepseek-v4-flash']}, 1, true, now(), now());", "prospective Model Target graph contains a cycle", False, None),
                ("flash-model-disabled", f"UPDATE model_configs SET is_enabled = false WHERE id = {MODEL_IDS['DeepSeek-V4-Flash']};", "DeepSeek identity conflict", True, None),
                ("flash-upstream-mismatch", "UPDATE connections SET upstream_model_id = 'wrong-case' WHERE id = 102;", "DeepSeek identity conflict", True, None),
            )
            for name, mutation, message, existing, overrides in failures:
                run_case(
                    f"fail-closed-{name}",
                    lambda name=name, mutation=mutation, message=message, existing=existing, overrides=overrides: assert_failure_atomic(
                        pg,
                        name=name,
                        mutation=mutation,
                        expected_error=message,
                        existing_edges=existing,
                        variable_overrides=overrides,
                    ),
                    passed,
                )
    except Exception as exc:
        write_evidence("failed", passed, source_hashes, str(exc))
        print(f"FAIL C7: {exc}", file=sys.stderr)
        return 1
    write_evidence("passed", passed, source_hashes)
    print(f"C7 PASS: {len(passed)} disposable scenarios", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
