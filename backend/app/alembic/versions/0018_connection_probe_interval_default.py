from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0018_connection_probe_interval_default"
down_revision = "0017_merge_timeout_policy_and_request_log_ua_rules"
branch_labels = None
depends_on = None


def upgrade() -> None:
    bind = op.get_bind()
    inspector = sa.inspect(bind)
    columns = {
        column["name"]: column for column in inspector.get_columns("connections")
    }

    if "monitoring_probe_interval_seconds" not in columns:
        op.add_column(
            "connections",
            sa.Column(
                "monitoring_probe_interval_seconds",
                sa.Integer(),
                nullable=False,
                server_default=sa.text("300"),
            ),
        )
        return

    op.execute(
        sa.text(
            "UPDATE connections "
            "SET monitoring_probe_interval_seconds = 300 "
            "WHERE monitoring_probe_interval_seconds IS NULL"
        )
    )
    op.alter_column(
        "connections",
        "monitoring_probe_interval_seconds",
        existing_type=sa.Integer(),
        nullable=False,
        server_default=sa.text("300"),
    )


def downgrade() -> None:
    raise NotImplementedError("Alembic revision is forward-only")
