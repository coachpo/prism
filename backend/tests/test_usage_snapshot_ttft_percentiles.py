from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace
from typing import cast

from sqlalchemy.ext.asyncio import AsyncSession

from app.schemas.schemas import UsageSnapshotResponse
from app.services.stats.usage_snapshot import (
    _build_endpoint_statistics,
    _build_model_statistics,
    _load_snapshot_events,
    _percentile_cont_int,
    get_usage_snapshot,
)


class _FakeResult:
    def __init__(self, rows: list[SimpleNamespace]) -> None:
        self._rows = rows

    def all(self) -> list[SimpleNamespace]:
        return list(self._rows)

    def scalar_one_or_none(self) -> None:
        return None


class _FakeSession:
    def __init__(self, rows: list[SimpleNamespace]) -> None:
        self._rows = rows

    async def execute(self, _query) -> _FakeResult:
        return _FakeResult(self._rows)


def _row(
    *,
    output_tokens: int | None,
    total_tokens: int | None,
    completion_duration_ms: int | None,
    ttft_ms: int | None,
    endpoint_id: int | None = 1,
    endpoint_name: str | None = "Primary Endpoint",
    model_id: str = "gpt-5.4",
    model_display_name: str | None = None,
) -> SimpleNamespace:
    return SimpleNamespace(
        api_family="openai",
        attempt_count=1,
        cache_creation_input_tokens=0,
        cache_read_input_tokens=0,
        connection_id=None,
        created_at=datetime(2026, 4, 10, tzinfo=timezone.utc),
        endpoint_id=endpoint_id,
        ingress_request_id="req-1",
        input_tokens=1,
        model_id=model_id,
        output_tokens=output_tokens,
        proxy_api_key_id=None,
        proxy_api_key_name_snapshot=None,
        reasoning_tokens=0,
        request_path="/v1/chat/completions",
        response_time_ms=1000,
        ttft_ms=ttft_ms,
        completion_duration_ms=completion_duration_ms,
        resolved_target_model_id=None,
        status_code=200,
        success_flag=True,
        total_cost_user_currency_micros=0,
        total_tokens=total_tokens,
        model_display_name=model_display_name,
        endpoint_name=endpoint_name,
        endpoint_base_url=None,
        current_proxy_api_key_name=None,
        current_proxy_api_key_prefix=None,
    )


async def _build_endpoint_stats(rows: list[SimpleNamespace]) -> list[dict[str, object]]:
    events = await _load_snapshot_events(
        cast(AsyncSession, _FakeSession(rows)),
        profile_id=1,
        start_at=None,
        end_at=datetime(2026, 4, 11, tzinfo=timezone.utc),
    )
    return _build_endpoint_statistics(events)


async def _build_model_stats(rows: list[SimpleNamespace]) -> list[dict[str, object]]:
    events = await _load_snapshot_events(
        cast(AsyncSession, _FakeSession(rows)),
        profile_id=1,
        start_at=None,
        end_at=datetime(2026, 4, 11, tzinfo=timezone.utc),
    )
    return _build_model_statistics(events)


async def _build_snapshot(rows: list[SimpleNamespace]) -> UsageSnapshotResponse:
    snapshot = await get_usage_snapshot(
        cast(AsyncSession, _FakeSession(rows)),
        profile_id=1,
        preset="all",
    )
    return UsageSnapshotResponse.model_validate(snapshot)


def test_percentile_helper_matches_postgresql_percentile_cont_interpolation() -> None:
    assert _percentile_cont_int([100, 200, 400, 800], 0.5) == 300
    assert _percentile_cont_int([100, 200, 400, 800], 0.95) == 740
    assert _percentile_cont_int([], 0.5) is None


def test_endpoint_statistics_preserve_ttft_percentiles_when_output_rate_becomes_null() -> (
    None
):
    rows = asyncio.run(
        _build_endpoint_stats(
            [
                _row(
                    output_tokens=90,
                    total_tokens=100,
                    completion_duration_ms=1000,
                    ttft_ms=100,
                ),
                _row(
                    output_tokens=220,
                    total_tokens=300,
                    completion_duration_ms=1500,
                    ttft_ms=400,
                ),
                _row(
                    output_tokens=500,
                    total_tokens=500,
                    completion_duration_ms=2500,
                    ttft_ms=None,
                ),
            ]
        )
    )

    assert rows == [
        {
            "endpoint_id": 1,
            "endpoint_label": "Primary Endpoint",
            "request_count": 3,
            "success_rate": 100.0,
            "p50_ttft_ms": 250,
            "p95_ttft_ms": 385,
            "avg_output_rate_tps": None,
            "total_tokens": 900,
            "total_cost_micros": 0,
        }
    ]


def test_model_statistics_return_null_when_every_row_is_ineligible_for_output_rate() -> (
    None
):
    rows = asyncio.run(
        _build_model_stats(
            [
                _row(
                    output_tokens=100,
                    total_tokens=100,
                    completion_duration_ms=1000,
                    ttft_ms=None,
                ),
                _row(
                    output_tokens=None,
                    total_tokens=300,
                    completion_duration_ms=1500,
                    ttft_ms=None,
                ),
            ]
        )
    )

    assert rows == [
        {
            "model_id": "gpt-5.4",
            "model_label": "gpt-5.4",
            "request_count": 2,
            "success_rate": 100.0,
            "p50_ttft_ms": None,
            "p95_ttft_ms": None,
            "avg_output_rate_tps": None,
            "total_tokens": 400,
            "total_cost_micros": 0,
        }
    ]


def test_usage_snapshot_response_validates_ttft_percentiles_with_null_output_rate() -> (
    None
):
    snapshot = asyncio.run(
        _build_snapshot(
            [
                _row(
                    output_tokens=90,
                    total_tokens=100,
                    completion_duration_ms=1000,
                    ttft_ms=100,
                    model_display_name="GPT 5.4",
                ),
                _row(
                    output_tokens=220,
                    total_tokens=300,
                    completion_duration_ms=1500,
                    ttft_ms=400,
                    model_display_name="GPT 5.4",
                ),
                _row(
                    output_tokens=500,
                    total_tokens=500,
                    completion_duration_ms=2500,
                    ttft_ms=None,
                    model_display_name="GPT 5.4",
                ),
            ]
        )
    )

    assert len(snapshot.endpoint_statistics) == 1
    assert snapshot.endpoint_statistics[0].p50_ttft_ms == 250
    assert snapshot.endpoint_statistics[0].p95_ttft_ms == 385
    assert snapshot.endpoint_statistics[0].avg_output_rate_tps is None

    assert len(snapshot.model_statistics) == 1
    statistic = snapshot.model_statistics[0]
    assert statistic.p50_ttft_ms == 250
    assert statistic.p95_ttft_ms == 385
    assert statistic.avg_output_rate_tps is None
    assert set(statistic.model_dump()) == {
        "model_id",
        "model_label",
        "request_count",
        "success_rate",
        "p50_ttft_ms",
        "p95_ttft_ms",
        "total_tokens",
        "total_cost_micros",
        "avg_output_rate_tps",
    }
