from __future__ import annotations

import importlib
from typing import Any, cast

import sqlalchemy as sa

op = cast(Any, importlib.import_module("alembic.op"))

revision = "0026_request_log_audit_enabled_at_request"
down_revision = "0025_usage_request_event_billing_semantics"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "request_logs",
        sa.Column("audit_enabled_at_request", sa.Boolean(), nullable=True),
    )


def downgrade() -> None:
    raise NotImplementedError(
        "Request log audit-enabled snapshot migration is forward-only"
    )
