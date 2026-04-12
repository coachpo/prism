from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0023_request_log_ttft_ms"
down_revision = "0022_request_log_completion_duration_ms"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "request_logs",
        sa.Column("ttft_ms", sa.Integer(), nullable=True),
    )
    op.add_column(
        "usage_request_events",
        sa.Column("ttft_ms", sa.Integer(), nullable=True),
    )


def downgrade() -> None:
    raise NotImplementedError("Request log TTFT migration is forward-only")
