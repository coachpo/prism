from __future__ import annotations

from alembic import op
import sqlalchemy as sa

revision = "0016_request_log_ua_rules"
down_revision = "0015_openai_probe_variant"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "request_logs",
        sa.Column("caller_user_agent", sa.Text(), nullable=True),
    )
    op.add_column(
        "request_logs",
        sa.Column("upstream_user_agent", sa.Text(), nullable=True),
    )
    op.create_table(
        "user_agent_client_rules",
        sa.Column("id", sa.Integer(), nullable=False),
        sa.Column("profile_id", sa.Integer(), nullable=True),
        sa.Column("name", sa.String(length=200), nullable=False),
        sa.Column("pattern", sa.Text(), nullable=False),
        sa.Column("enabled", sa.Boolean(), nullable=False),
        sa.Column("is_system", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "((is_system = true AND profile_id IS NULL) OR (is_system = false AND profile_id IS NOT NULL))",
            name="ck_uacr_profile_scope",
        ),
        sa.ForeignKeyConstraint(
            ["profile_id"],
            ["profiles.id"],
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_user_agent_client_rules_profile_id",
        "user_agent_client_rules",
        ["profile_id"],
        unique=False,
    )
    op.create_index(
        "idx_uacr_enabled",
        "user_agent_client_rules",
        ["enabled"],
        unique=False,
    )
    op.create_index(
        "uq_uacr_system_pattern",
        "user_agent_client_rules",
        ["pattern"],
        unique=True,
        postgresql_where=sa.text("is_system = true"),
    )


def downgrade() -> None:
    raise NotImplementedError(
        "Request log user agents and user-agent client rule migration is forward-only"
    )
