from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0019_static_timeout_rollback"
down_revision = "0018_connection_probe_interval_default"
branch_labels = None
depends_on = None

_STRATEGY_SHAPE_CHECK = (
    "((strategy_type = 'legacy' AND legacy_strategy_type IS NOT NULL AND auto_recovery IS NOT NULL AND routing_policy IS NULL) "
    "OR (strategy_type = 'adaptive' AND legacy_strategy_type IS NULL AND auto_recovery IS NULL AND routing_policy IS NOT NULL))"
)


def upgrade() -> None:
    bind = op.get_bind()
    inspector = sa.inspect(bind)
    strategy_columns = {
        column["name"] for column in inspector.get_columns("loadbalance_strategies")
    }
    endpoint_columns = {column["name"] for column in inspector.get_columns("endpoints")}
    strategy_constraints = {
        constraint["name"]
        for constraint in inspector.get_check_constraints("loadbalance_strategies")
        if constraint.get("name")
    }

    if "chk_loadbalance_strategies_shape" in strategy_constraints:
        op.drop_constraint(
            "chk_loadbalance_strategies_shape",
            "loadbalance_strategies",
            type_="check",
        )

    if "timeout_policy" in strategy_columns:
        op.drop_column("loadbalance_strategies", "timeout_policy")

    for column_name in (
        "pool_timeout",
        "connect_timeout",
        "write_timeout",
        "read_idle_timeout",
    ):
        if column_name in endpoint_columns:
            op.drop_column("endpoints", column_name)

    op.create_check_constraint(
        "chk_loadbalance_strategies_shape",
        "loadbalance_strategies",
        _STRATEGY_SHAPE_CHECK,
    )


def downgrade() -> None:
    raise NotImplementedError("Alembic revision is forward-only")
