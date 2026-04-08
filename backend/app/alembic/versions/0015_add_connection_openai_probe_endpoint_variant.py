from __future__ import annotations

from alembic import op
import sqlalchemy as sa

revision = "0015_add_connection_openai_probe_endpoint_variant"
down_revision = "0014_widen_ingress_request_id"
branch_labels = None
depends_on = None


_CONNECTION_VARIANT_CHECK_NAME = "ck_connections_openai_probe_endpoint_variant"
_CONNECTION_VARIANT_CHECK_SQL = (
    "openai_probe_endpoint_variant IS NULL OR openai_probe_endpoint_variant IN "
    "('responses_minimal', 'responses_reasoning_none', "
    "'chat_completions_minimal', 'chat_completions_reasoning_none')"
)


def _constraint_names(bind, table_name: str) -> set[str]:
    inspector = sa.inspect(bind)
    return {
        constraint["name"]
        for constraint in inspector.get_check_constraints(table_name)
        if constraint.get("name")
    }


def _column_names(bind, table_name: str) -> set[str]:
    inspector = sa.inspect(bind)
    return {column["name"] for column in inspector.get_columns(table_name)}


def upgrade() -> None:
    bind = op.get_bind()
    connection_columns = _column_names(bind, "connections")
    if "openai_probe_endpoint_variant" not in connection_columns:
        op.add_column(
            "connections",
            sa.Column(
                "openai_probe_endpoint_variant", sa.String(length=40), nullable=True
            ),
        )

    connection_constraints = _constraint_names(bind, "connections")
    if _CONNECTION_VARIANT_CHECK_NAME not in connection_constraints:
        op.create_check_constraint(
            _CONNECTION_VARIANT_CHECK_NAME,
            "connections",
            _CONNECTION_VARIANT_CHECK_SQL,
        )

    op.execute(
        sa.text(
            """
            UPDATE connections AS connection
            SET openai_probe_endpoint_variant = 'responses_minimal'
            FROM model_configs AS model
            WHERE connection.model_config_id = model.id
              AND model.api_family = 'openai'
              AND connection.openai_probe_endpoint_variant IS NULL
            """
        )
    )


def downgrade() -> None:
    raise NotImplementedError(
        "Connection OpenAI probe endpoint variant migration is forward-only"
    )
