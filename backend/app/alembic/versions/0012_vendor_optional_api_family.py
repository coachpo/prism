from __future__ import annotations

from alembic import op  # pyright: ignore[reportAttributeAccessIssue]
import sqlalchemy as sa

revision = "0012_vendor_optional_api_family"
down_revision = "0011_expand_openai_probe_presets"
branch_labels = None
depends_on = None


def _get_column(table_name: str, column_name: str) -> dict[str, object] | None:
    inspector = sa.inspect(op.get_bind())
    for column in inspector.get_columns(table_name):
        if column["name"] == column_name:
            return column
    return None


def _drop_vendor_fk(table_name: str) -> None:
    inspector = sa.inspect(op.get_bind())
    for foreign_key in inspector.get_foreign_keys(table_name):
        if foreign_key.get("referred_table") != "vendors":
            continue
        if foreign_key.get("constrained_columns") != ["vendor_id"]:
            continue
        constraint_name = foreign_key.get("name")
        if constraint_name:
            op.drop_constraint(constraint_name, table_name, type_="foreignkey")


def _has_fk(table_name: str, constraint_name: str) -> bool:
    inspector = sa.inspect(op.get_bind())
    return any(
        foreign_key.get("name") == constraint_name
        for foreign_key in inspector.get_foreign_keys(table_name)
    )


def upgrade() -> None:
    model_vendor_column = _get_column("model_configs", "vendor_id")
    if model_vendor_column is not None:
        _drop_vendor_fk("model_configs")
        if not model_vendor_column.get("nullable", False):
            op.alter_column(
                "model_configs",
                "vendor_id",
                existing_type=sa.Integer(),
                nullable=True,
            )
        if not _has_fk("model_configs", "fk_model_configs_vendor_id_set_null"):
            op.create_foreign_key(
                "fk_model_configs_vendor_id_set_null",
                "model_configs",
                "vendors",
                ["vendor_id"],
                ["id"],
                ondelete="SET NULL",
            )

    audit_vendor_column = _get_column("audit_logs", "vendor_id")
    if audit_vendor_column is not None:
        _drop_vendor_fk("audit_logs")
        if not audit_vendor_column.get("nullable", False):
            op.alter_column(
                "audit_logs",
                "vendor_id",
                existing_type=sa.Integer(),
                nullable=True,
            )
        if not _has_fk("audit_logs", "fk_audit_logs_vendor_id_set_null"):
            op.create_foreign_key(
                "fk_audit_logs_vendor_id_set_null",
                "audit_logs",
                "vendors",
                ["vendor_id"],
                ["id"],
                ondelete="SET NULL",
            )

    loadbalance_vendor_column = _get_column("loadbalance_events", "vendor_id")
    if loadbalance_vendor_column is not None:
        _drop_vendor_fk("loadbalance_events")
        if not _has_fk(
            "loadbalance_events", "fk_loadbalance_events_vendor_id_set_null"
        ):
            op.create_foreign_key(
                "fk_loadbalance_events_vendor_id_set_null",
                "loadbalance_events",
                "vendors",
                ["vendor_id"],
                ["id"],
                ondelete="SET NULL",
            )


def downgrade() -> None:
    raise NotImplementedError("Vendor-optional api-family migration is forward-only")
