from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0025_usage_request_event_billing_semantics"
down_revision = "0024_drop_missing_special_token_policy_columns"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "usage_request_events",
        sa.Column("billable_flag", sa.Boolean(), nullable=True),
    )
    op.add_column(
        "usage_request_events",
        sa.Column("priced_flag", sa.Boolean(), nullable=True),
    )
    op.add_column(
        "usage_request_events",
        sa.Column("unpriced_reason", sa.String(length=50), nullable=True),
    )


def downgrade() -> None:
    raise NotImplementedError(
        "Usage request event billing semantics migration is forward-only"
    )
