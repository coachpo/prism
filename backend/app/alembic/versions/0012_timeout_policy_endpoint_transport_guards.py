from __future__ import annotations

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects import postgresql

revision = "0012_timeout_policy_endpoint_transport_guards"
down_revision = "0011_expand_openai_probe_presets"
branch_labels = None
depends_on = None


_STRATEGY_SHAPE_CHECK = (
    "((strategy_type = 'legacy' AND timeout_policy IS NOT NULL AND legacy_strategy_type IS NOT NULL "
    "AND auto_recovery IS NOT NULL AND routing_policy IS NULL) OR "
    "(strategy_type = 'adaptive' AND timeout_policy IS NOT NULL AND legacy_strategy_type IS NULL "
    "AND auto_recovery IS NULL AND routing_policy IS NOT NULL))"
)


def upgrade() -> None:
    op.add_column(
        "loadbalance_strategies",
        sa.Column(
            "timeout_policy",
            postgresql.JSONB(astext_type=sa.Text()),
            nullable=True,
        ),
    )
    op.add_column("endpoints", sa.Column("pool_timeout", sa.Float(), nullable=True))
    op.add_column("endpoints", sa.Column("connect_timeout", sa.Float(), nullable=True))
    op.add_column("endpoints", sa.Column("write_timeout", sa.Float(), nullable=True))
    op.add_column("endpoints", sa.Column("read_idle_timeout", sa.Float(), nullable=True))

    op.execute(
        """
        UPDATE loadbalance_strategies
        SET timeout_policy = jsonb_build_object(
            'attempt_open_timeout_ms', 2000,
            'buffered_total_timeout_ms', COALESCE((routing_policy ->> 'deadline_budget_ms')::integer, 30000),
            'stream_precommit_timeout_ms', 5000,
            'stream_hard_cap_timeout_ms', 120000
        )
        WHERE timeout_policy IS NULL
        """
    )
    op.execute(
        """
        UPDATE loadbalance_strategies
        SET routing_policy = CASE
            WHEN routing_policy IS NULL THEN NULL
            ELSE routing_policy - 'deadline_budget_ms'
        END
        """
    )
    op.execute(
        """
        UPDATE endpoints
        SET pool_timeout = COALESCE(pool_timeout, 5.0),
            connect_timeout = COALESCE(connect_timeout, 10.0),
            write_timeout = COALESCE(write_timeout, 30.0),
            read_idle_timeout = COALESCE(read_idle_timeout, 120.0)
        """
    )

    op.alter_column("loadbalance_strategies", "timeout_policy", nullable=False)
    op.alter_column("endpoints", "pool_timeout", nullable=False)
    op.alter_column("endpoints", "connect_timeout", nullable=False)
    op.alter_column("endpoints", "write_timeout", nullable=False)
    op.alter_column("endpoints", "read_idle_timeout", nullable=False)

    op.drop_constraint(
        "chk_loadbalance_strategies_shape",
        "loadbalance_strategies",
        type_="check",
    )
    op.create_check_constraint(
        "chk_loadbalance_strategies_shape",
        "loadbalance_strategies",
        _STRATEGY_SHAPE_CHECK,
    )


def downgrade() -> None:
    raise NotImplementedError(
        "Timeout policy + endpoint transport guard migration is forward-only"
    )
