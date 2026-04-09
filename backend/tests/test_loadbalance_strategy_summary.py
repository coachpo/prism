from app.models.models import LoadbalanceStrategy
from app.schemas.schemas import LoadbalanceStrategySummary


def test_loadbalance_strategy_summary_omits_timeout_policy_from_payload() -> None:
    strategy = LoadbalanceStrategy(
        id=1,
        profile_id=1,
        name="Default legacy routing",
        strategy_type="legacy",
        legacy_strategy_type="round-robin",
        auto_recovery={"mode": "disabled"},
        routing_policy=None,
    )

    summary = LoadbalanceStrategySummary.model_validate(strategy)

    assert "timeout_policy" not in summary.model_dump()
    assert summary.strategy_type == "legacy"
    assert summary.legacy_strategy_type == "round-robin"
