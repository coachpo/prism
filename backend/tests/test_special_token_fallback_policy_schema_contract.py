from __future__ import annotations

import ast
import importlib.util
from pathlib import Path
from typing import Any, cast

from sqlalchemy import create_engine
from sqlalchemy.pool import StaticPool

from app.models.models import PricingTemplate, RequestLog, UsageRequestEvent

REMOVED_POLICY_FIELD = "_".join(("missing", "special", "token", "price", "policy"))
REMOVED_SNAPSHOT_FIELD = "_".join(
    ("pricing", "snapshot", "missing", "special", "token", "price", "policy")
)

MIGRATION_PATH = (
    Path(__file__).resolve().parents[1]
    / "app"
    / "alembic"
    / "versions"
    / "0024_drop_missing_special_token_policy_columns.py"
)


def _read_migration_module() -> ast.Module:
    return ast.parse(MIGRATION_PATH.read_text(encoding="utf-8"))


def _load_migration_module():
    spec = importlib.util.spec_from_file_location(
        "task_1_migration_0024", MIGRATION_PATH
    )
    if spec is None or spec.loader is None:
        raise AssertionError(f"Could not load migration module from {MIGRATION_PATH}")

    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _get_string_assignment(module: ast.Module, name: str) -> str:
    for statement in module.body:
        if not isinstance(statement, ast.Assign):
            continue
        for target in statement.targets:
            if isinstance(target, ast.Name) and target.id == name:
                return ast.literal_eval(statement.value)
    raise AssertionError(f"Could not find {name!r} assignment in {MIGRATION_PATH.name}")


def _get_upgrade_function(module: ast.Module) -> ast.FunctionDef:
    for statement in module.body:
        if isinstance(statement, ast.FunctionDef) and statement.name == "upgrade":
            return statement

    raise AssertionError(f"Could not find upgrade() in {MIGRATION_PATH.name}")


def _get_inspected_tables(module: ast.Module) -> set[str]:
    inspected_tables: set[str] = set()
    for statement in ast.walk(_get_upgrade_function(module)):
        if not isinstance(statement, ast.Call):
            continue

        if not isinstance(statement.func, ast.Attribute):
            continue

        if statement.func.attr != "get_columns" or len(statement.args) != 1:
            continue

        inspected_tables.add(ast.literal_eval(statement.args[0]))

    return inspected_tables


class _RecordingOp:
    def __init__(self, connection) -> None:
        self._connection = connection
        self.drop_calls: list[tuple[str, str]] = []

    def get_bind(self):
        return self._connection

    def drop_column(self, table_name: str, column_name: str) -> None:
        self.drop_calls.append((table_name, column_name))


def test_forward_revision_drops_only_the_three_policy_columns() -> None:
    migration_module = _read_migration_module()
    migration_runtime = _load_migration_module()
    migration_runtime_any: Any = migration_runtime

    assert MIGRATION_PATH.exists()
    assert (
        _get_string_assignment(migration_module, "revision")
        == "0024_drop_missing_special_token_policy_columns"
    )
    assert (
        _get_string_assignment(migration_module, "down_revision")
        == "0023_request_log_ttft_ms"
    )
    assert {
        "pricing_templates",
        "request_logs",
        "usage_request_events",
    }.issubset(_get_inspected_tables(migration_module))

    engine = create_engine(
        "sqlite://",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )

    with engine.begin() as connection:
        connection.exec_driver_sql(
            f"CREATE TABLE pricing_templates (id INTEGER PRIMARY KEY, {REMOVED_POLICY_FIELD} TEXT)"
        )
        connection.exec_driver_sql(
            f"CREATE TABLE request_logs (id INTEGER PRIMARY KEY, {REMOVED_SNAPSHOT_FIELD} TEXT)"
        )
        connection.exec_driver_sql(
            "CREATE TABLE usage_request_events "
            f"(id INTEGER PRIMARY KEY, {REMOVED_SNAPSHOT_FIELD} TEXT)"
        )

        fake_op = _RecordingOp(connection)
        original_op = getattr(migration_runtime_any, "op")
        setattr(migration_runtime_any, "op", fake_op)
        try:
            cast(Any, migration_runtime_any).upgrade()
        finally:
            setattr(migration_runtime_any, "op", original_op)

    assert set(fake_op.drop_calls) == {
        ("pricing_templates", REMOVED_POLICY_FIELD),
        ("request_logs", REMOVED_SNAPSHOT_FIELD),
        ("usage_request_events", REMOVED_SNAPSHOT_FIELD),
    }


def test_orm_metadata_omits_removed_policy_columns() -> None:
    assert REMOVED_POLICY_FIELD not in PricingTemplate.__table__.columns
    assert REMOVED_SNAPSHOT_FIELD not in RequestLog.__table__.columns
    assert REMOVED_SNAPSHOT_FIELD not in UsageRequestEvent.__table__.columns


def test_forward_revision_skips_already_absent_columns() -> None:
    migration_module = _load_migration_module()
    migration_module_any: Any = migration_module
    engine = create_engine(
        "sqlite://",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )

    with engine.begin() as connection:
        connection.exec_driver_sql(
            "CREATE TABLE pricing_templates (id INTEGER PRIMARY KEY)"
        )
        connection.exec_driver_sql("CREATE TABLE request_logs (id INTEGER PRIMARY KEY)")
        connection.exec_driver_sql(
            "CREATE TABLE usage_request_events (id INTEGER PRIMARY KEY)"
        )

        fake_op = _RecordingOp(connection)
        original_op = getattr(migration_module_any, "op")
        setattr(migration_module_any, "op", fake_op)
        try:
            cast(Any, migration_module_any).upgrade()
        finally:
            setattr(migration_module_any, "op", original_op)

    assert fake_op.drop_calls == []
