from tests.loadbalance_strategy_helpers import make_loadbalance_strategy


def test_make_loadbalance_strategy_supplies_timeout_policy_for_legacy_strategy() -> None:
    strategy = make_loadbalance_strategy(profile_id=1, strategy_type="single")

    assert strategy.strategy_type == "legacy"
    assert strategy.timeout_policy is not None
    assert strategy.timeout_policy["attempt_open_timeout_ms"] == 2_000
    assert strategy.timeout_policy["buffered_total_timeout_ms"] == 30_000
