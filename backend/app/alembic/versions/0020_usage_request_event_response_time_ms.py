from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0020_usage_request_event_response_time_ms"
down_revision = "0019_static_timeout_rollback"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "usage_request_events",
        sa.Column("response_time_ms", sa.Integer(), nullable=True),
    )


def downgrade() -> None:
    raise NotImplementedError(
        "Usage request event response_time_ms migration is forward-only"
    )
