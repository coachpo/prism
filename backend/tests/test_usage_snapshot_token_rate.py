from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace
from typing import cast

from app.schemas.schemas import UsageSnapshotResponse
from app.services.stats.usage_snapshot import (
    _build_endpoint_statistics,
    _build_model_statistics,
    get_usage_snapshot,
    _load_snapshot_events,
)
from sqlalchemy.ext.asyncio import AsyncSession


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
    total_tokens: int | None,
    response_time_ms: int | None,
    completion_duration_ms: int | None,
    endpoint_id: int | None = 1,
    endpoint_name: str | None = "Primary Endpoint",
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
        model_id="gpt-5.4",
        output_tokens=2,
        proxy_api_key_id=None,
        proxy_api_key_name_snapshot=None,
        reasoning_tokens=0,
        request_path="/v1/chat/completions",
        response_time_ms=response_time_ms,
        completion_duration_ms=completion_duration_ms,
        resolved_target_model_id=None,
        status_code=200,
        success_flag=True,
        total_cost_user_currency_micros=0,
        total_tokens=total_tokens,
        model_display_name=None,
        endpoint_name=endpoint_name,
        endpoint_base_url=None,
        current_proxy_api_key_name=None,
        current_proxy_api_key_prefix=None,
    )


async def _build_stats(rows: list[SimpleNamespace]) -> list[dict[str, object]]:
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


def test_buffered_and_stream_completion_rates_return_avg_token_rate() -> None:
    rows = asyncio.run(
        _build_stats(
            [
                _row(
                    total_tokens=100,
                    response_time_ms=1000,
                    completion_duration_ms=5000,
                ),
                _row(
                    total_tokens=300,
                    response_time_ms=1000,
                    completion_duration_ms=1500,
                ),
            ]
        )
    )

    assert rows == [
        {
            "endpoint_id": 1,
            "endpoint_label": "Primary Endpoint",
            "request_count": 2,
            "success_rate": 100.0,
            "avg_token_rate": 110.0,
            "total_tokens": 400,
            "total_cost_micros": 0,
        }
    ]


def test_legacy_or_incomplete_rows_return_null_when_completion_duration_missing() -> (
    None
):
    rows = asyncio.run(
        _build_stats(
            [
                _row(
                    total_tokens=100,
                    response_time_ms=1000,
                    completion_duration_ms=1000,
                ),
                _row(
                    total_tokens=300,
                    response_time_ms=1000,
                    completion_duration_ms=None,
                ),
            ]
        )
    )

    assert rows[0]["avg_token_rate"] is None
    assert rows[0]["total_tokens"] == 400
    assert rows[0]["request_count"] == 2


def test_ineligible_rows_return_null_when_tokens_missing() -> None:
    rows = asyncio.run(
        _build_stats(
            [
                _row(
                    total_tokens=None,
                    response_time_ms=1000,
                    completion_duration_ms=1000,
                ),
                _row(
                    total_tokens=300,
                    response_time_ms=1000,
                    completion_duration_ms=1000,
                ),
            ]
        )
    )

    assert rows[0]["avg_token_rate"] is None
    assert rows[0]["total_tokens"] == 300
    assert rows[0]["request_count"] == 2


def test_model_statistics_return_avg_token_rate_for_completion_duration_rows() -> None:
    rows = asyncio.run(
        _build_model_stats(
            [
                _row(
                    total_tokens=100,
                    response_time_ms=1000,
                    completion_duration_ms=5000,
                ),
                _row(
                    total_tokens=300,
                    response_time_ms=1000,
                    completion_duration_ms=1500,
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
            "avg_token_rate": 110.0,
            "total_tokens": 400,
            "total_cost_micros": 0,
        }
    ]


def test_usage_snapshot_response_validates_with_model_avg_token_rate() -> None:
    snapshot = asyncio.run(
        _build_snapshot(
            [
                _row(
                    total_tokens=100,
                    response_time_ms=1000,
                    completion_duration_ms=5000,
                ),
                _row(
                    total_tokens=300,
                    response_time_ms=1000,
                    completion_duration_ms=1500,
                ),
            ]
        )
    )

    assert len(snapshot.model_statistics) == 1
    assert snapshot.model_statistics[0].avg_token_rate == 110.0
