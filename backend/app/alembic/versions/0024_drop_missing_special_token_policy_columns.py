from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0024_drop_missing_special_token_policy_columns"
down_revision = "0023_request_log_ttft_ms"
branch_labels = None
depends_on = None


def _joined(*parts: str) -> str:
    return "_".join(parts)


def upgrade() -> None:
    bind = op.get_bind()
    inspector = sa.inspect(bind)
    pricing_template_columns = {
        column["name"] for column in inspector.get_columns("pricing_templates")
    }
    request_log_columns = {
        column["name"] for column in inspector.get_columns("request_logs")
    }
    usage_request_event_columns = {
        column["name"] for column in inspector.get_columns("usage_request_events")
    }
    pricing_template_policy_column = _joined(
        "missing", "special", "token", "price", "policy"
    )
    snapshot_policy_column = _joined(
        "pricing", "snapshot", pricing_template_policy_column
    )

    if pricing_template_policy_column in pricing_template_columns:
        op.drop_column(
            "pricing_templates",
            pricing_template_policy_column,
        )

    if snapshot_policy_column in request_log_columns:
        op.drop_column(
            "request_logs",
            snapshot_policy_column,
        )

    if snapshot_policy_column in usage_request_event_columns:
        op.drop_column(
            "usage_request_events",
            snapshot_policy_column,
        )


def downgrade() -> None:
    raise NotImplementedError("Special-token policy column removal is forward-only")
