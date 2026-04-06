from __future__ import annotations

from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from typing import Any, cast

from app.models.models import Connection
from app.services.loadbalancer.policy import resolve_effective_loadbalance_policy
from app.services.loadbalancer.types import (
    AttemptCandidateScoreInput,
    RuntimeCircuitState,
)
from tests.loadbalance_strategy_helpers import make_routing_policy_adaptive


def _make_policy(**overrides: object):
    return resolve_effective_loadbalance_policy(
        SimpleNamespace(
            routing_policy=make_routing_policy_adaptive(**cast(Any, overrides))
        )
    )


def _make_candidate_input(
    connection_id: int,
    *,
    priority: int,
    qps_limit: int | None = None,
    max_in_flight_non_stream: int | None = None,
    max_in_flight_stream: int | None = None,
    circuit_state: str = "closed",
    blocked_until_at: datetime | None = None,
    banned_until_at: datetime | None = None,
    probe_available_at: datetime | None = None,
    in_flight_non_stream: int = 0,
    in_flight_stream: int = 0,
    qps_window_count: int = 0,
    live_p95_latency_ms: float | None = 100.0,
    last_live_failure_kind: str | None = None,
    last_live_failure_at: datetime | None = None,
    last_live_success_at: datetime | None = None,
) -> AttemptCandidateScoreInput:
    connection = cast(
        Connection,
        cast(
            object,
            SimpleNamespace(
                id=connection_id,
                priority=priority,
                qps_limit=qps_limit,
                max_in_flight_non_stream=max_in_flight_non_stream,
                max_in_flight_stream=max_in_flight_stream,
                model_rollup_latency_ms=1,
                vendor_rollup_latency_ms=1,
            ),
        ),
    )
    return AttemptCandidateScoreInput(
        connection=connection,
        circuit_state=cast(RuntimeCircuitState, circuit_state),
        blocked_until_at=blocked_until_at,
        banned_until_at=banned_until_at,
        probe_available_at=probe_available_at,
        in_flight_non_stream=in_flight_non_stream,
        in_flight_stream=in_flight_stream,
        qps_window_count=qps_window_count,
        live_p95_latency_ms=live_p95_latency_ms,
        last_live_failure_kind=last_live_failure_kind,
        last_live_failure_at=last_live_failure_at,
        last_live_success_at=last_live_success_at,
    )


class TestLoadbalancerScoring:
    def test_rank_candidates_penalizes_saturation(self):
        from app.services.loadbalancer.scoring import rank_candidates

        now_at = datetime(2026, 3, 29, 12, 0, tzinfo=timezone.utc)
        policy = _make_policy()
        lightly_loaded = _make_candidate_input(
            11,
            priority=5,
            qps_limit=10,
            max_in_flight_non_stream=10,
            qps_window_count=1,
            in_flight_non_stream=1,
        )
        saturated = _make_candidate_input(
            12,
            priority=0,
            qps_limit=10,
            max_in_flight_non_stream=10,
            qps_window_count=10,
            in_flight_non_stream=10,
        )

        ranked = rank_candidates(
            policy=policy,
            candidate_inputs=[saturated, lightly_loaded],
            now_at=now_at,
        )

        assert [candidate.connection.id for candidate in ranked] == [11, 12]

    def test_rank_candidates_penalizes_recent_live_failures(self):
        from app.services.loadbalancer.scoring import rank_candidates

        now_at = datetime(2026, 3, 29, 12, 0, tzinfo=timezone.utc)
        policy = _make_policy()
        healthy = _make_candidate_input(
            21,
            priority=1,
        )
        recent_failure = _make_candidate_input(
            22,
            priority=0,
            last_live_failure_kind="timeout",
            last_live_failure_at=now_at - timedelta(seconds=5),
        )

        ranked = rank_candidates(
            policy=policy,
            candidate_inputs=[recent_failure, healthy],
            now_at=now_at,
        )

        assert [candidate.connection.id for candidate in ranked] == [21, 22]

    def test_rank_candidates_uses_live_latency(self):
        from app.services.loadbalancer.scoring import rank_candidates

        now_at = datetime(2026, 3, 29, 12, 0, tzinfo=timezone.utc)
        policy = _make_policy()
        faster_connection = _make_candidate_input(
            31,
            priority=1,
            live_p95_latency_ms=80.0,
        )
        slower_connection = _make_candidate_input(
            32,
            priority=0,
            live_p95_latency_ms=220.0,
        )

        ranked = rank_candidates(
            policy=policy,
            candidate_inputs=[slower_connection, faster_connection],
            now_at=now_at,
        )

        assert [candidate.connection.id for candidate in ranked] == [31, 32]

    def test_rank_candidates_penalizes_open_circuit_state(self):
        from app.services.loadbalancer.scoring import rank_candidates

        now_at = datetime(2026, 3, 29, 12, 0, tzinfo=timezone.utc)
        policy = _make_policy()
        closed = _make_candidate_input(
            41,
            priority=1,
            live_p95_latency_ms=100.0,
        )
        open_circuit = _make_candidate_input(
            42,
            priority=0,
            circuit_state="open",
            live_p95_latency_ms=50.0,
        )

        ranked = rank_candidates(
            policy=policy,
            candidate_inputs=[open_circuit, closed],
            now_at=now_at,
        )

        assert [candidate.connection.id for candidate in ranked] == [41, 42]

    def test_rank_candidates_recent_success_clears_recent_failure_penalty(
        self,
    ):
        from app.services.loadbalancer.scoring import rank_candidates

        now_at = datetime(2026, 3, 29, 12, 0, tzinfo=timezone.utc)
        policy = _make_policy()
        recovered = _make_candidate_input(
            43,
            priority=1,
            live_p95_latency_ms=140.0,
            last_live_failure_at=now_at - timedelta(seconds=10),
            last_live_success_at=now_at - timedelta(seconds=5),
        )
        still_failing = _make_candidate_input(
            44,
            priority=0,
            live_p95_latency_ms=80.0,
            last_live_failure_at=now_at - timedelta(seconds=5),
        )

        ranked = rank_candidates(
            policy=policy,
            candidate_inputs=[still_failing, recovered],
            now_at=now_at,
        )

        assert [candidate.connection.id for candidate in ranked] == [43, 44]

    def test_candidate_sort_key_uses_priority_then_id_as_stable_tie_breaker(self):
        from app.services.loadbalancer.scoring import (
            candidate_sort_key,
            rank_candidates,
        )

        now_at = datetime(2026, 3, 29, 12, 0, tzinfo=timezone.utc)
        policy = _make_policy()
        lower_priority = _make_candidate_input(
            51, priority=0, live_p95_latency_ms=100.0
        )
        higher_priority = _make_candidate_input(
            52, priority=1, live_p95_latency_ms=100.0
        )
        same_priority_lower_id = _make_candidate_input(
            53,
            priority=1,
            live_p95_latency_ms=100.0,
        )
        same_priority_higher_id = _make_candidate_input(
            54,
            priority=1,
            live_p95_latency_ms=100.0,
        )

        assert candidate_sort_key(
            policy, lower_priority, now_at=now_at
        ) < candidate_sort_key(
            policy,
            higher_priority,
            now_at=now_at,
        )
        assert candidate_sort_key(
            policy,
            same_priority_lower_id,
            now_at=now_at,
        ) < candidate_sort_key(
            policy,
            same_priority_higher_id,
            now_at=now_at,
        )

        ranked = rank_candidates(
            policy=policy,
            candidate_inputs=[same_priority_higher_id, lower_priority, higher_priority],
            now_at=now_at,
        )

        assert [candidate.connection.id for candidate in ranked] == [51, 52, 54]
