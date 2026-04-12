from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0022_request_log_completion_duration_ms"
down_revision = "0020_usage_request_event_response_time_ms"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "request_logs",
        sa.Column("completion_duration_ms", sa.Integer(), nullable=True),
    )
    op.add_column(
        "usage_request_events",
        sa.Column("completion_duration_ms", sa.Integer(), nullable=True),
    )


def downgrade() -> None:
    raise NotImplementedError(
        "Request log completion-duration recovery migration is forward-only"
    )
