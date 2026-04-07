from __future__ import annotations

revision = "0014_widen_ingress_request_id"
down_revision = "0012_vendor_optional_api_family"
branch_labels = None
depends_on = None


def upgrade() -> None:
    return None


def downgrade() -> None:
    raise NotImplementedError("Ingress request id compatibility bridge is forward-only")
