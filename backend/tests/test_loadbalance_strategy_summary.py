from app.models.models import LoadbalanceStrategy
from app.schemas.schemas import LoadbalanceStrategySummary


def test_loadbalance_strategy_summary_derives_timeout_policy_from_orm_defaults() -> (
    None
):
    strategy = LoadbalanceStrategy(
        id=1,
        profile_id=1,
        name="Default legacy routing",
        strategy_type="legacy",
        legacy_strategy_type="round-robin",
        auto_recovery={"mode": "disabled"},
        routing_policy=None,
        timeout_policy=None,
    )

    summary = LoadbalanceStrategySummary.model_validate(strategy)

    assert summary.timeout_policy.attempt_open_timeout_ms > 0
    assert summary.timeout_policy.buffered_total_timeout_ms > 0
    assert summary.timeout_policy.stream_precommit_timeout_ms > 0
