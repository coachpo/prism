from __future__ import annotations

revision = "0017_merge_timeout_policy_and_request_log_ua_rules"
down_revision = (
    "0012_timeout_policy_endpoint_transport_guards",
    "0016_request_log_ua_rules",
)
branch_labels = None
depends_on = None


def upgrade() -> None:
    return None


def downgrade() -> None:
    raise NotImplementedError("Alembic merge revision is forward-only")
