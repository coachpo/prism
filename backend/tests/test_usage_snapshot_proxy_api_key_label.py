from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace
from typing import cast

from app.services.stats.usage_snapshot import (
    _build_proxy_api_key_statistics,
    _load_snapshot_events,
)
from sqlalchemy.ext.asyncio import AsyncSession


class _FakeResult:
    def __init__(self, rows: list[SimpleNamespace]) -> None:
        self._rows = rows

    def all(self) -> list[SimpleNamespace]:
        return list(self._rows)


class _FakeSession:
    def __init__(self, rows: list[SimpleNamespace]) -> None:
        self._rows = rows

    async def execute(self, _query) -> _FakeResult:
        return _FakeResult(self._rows)


def _row(
    *,
    proxy_api_key_name_snapshot: str | None,
    current_proxy_api_key_name: str | None,
    proxy_api_key_id: int | None = 1,
) -> SimpleNamespace:
    return SimpleNamespace(
        api_family="openai",
        attempt_count=1,
        cache_creation_input_tokens=0,
        cache_read_input_tokens=0,
        connection_id=None,
        created_at=datetime(2026, 4, 10, tzinfo=timezone.utc),
        endpoint_id=None,
        ingress_request_id="req-1",
        input_tokens=1,
        model_id="gpt-5.4",
        output_tokens=2,
        proxy_api_key_id=proxy_api_key_id,
        proxy_api_key_name_snapshot=proxy_api_key_name_snapshot,
        reasoning_tokens=0,
        request_path="/v1/chat/completions",
        resolved_target_model_id=None,
        status_code=200,
        success_flag=True,
        total_cost_user_currency_micros=0,
        total_tokens=3,
        model_display_name=None,
        endpoint_name=None,
        endpoint_base_url=None,
        current_proxy_api_key_name=current_proxy_api_key_name,
        current_proxy_api_key_prefix=None,
    )


def test_load_snapshot_events_uses_snapshot_name_first_then_live_name_then_fallback() -> (
    None
):
    async def run() -> None:
        db = _FakeSession(
            [
                _row(
                    proxy_api_key_name_snapshot="Snapshot key",
                    current_proxy_api_key_name="Live key",
                ),
                _row(
                    proxy_api_key_name_snapshot=None,
                    current_proxy_api_key_name="Live key",
                ),
                _row(
                    proxy_api_key_name_snapshot=None,
                    current_proxy_api_key_name=None,
                    proxy_api_key_id=None,
                ),
            ]
        )

        events = await _load_snapshot_events(
            cast(AsyncSession, db),
            profile_id=1,
            start_at=None,
            end_at=datetime(2026, 4, 11, tzinfo=timezone.utc),
        )

        assert [event.proxy_api_key_label for event in events] == [
            "Snapshot key",
            "Live key",
            None,
        ]
        assert [event.proxy_api_key_stats_label for event in events] == [
            "Snapshot key",
            "Live key",
            "No proxy API key",
        ]

    asyncio.run(run())


def test_build_proxy_api_key_statistics_groups_the_fallback_bucket() -> None:
    async def run() -> None:
        db = _FakeSession(
            [
                _row(
                    proxy_api_key_name_snapshot=None,
                    current_proxy_api_key_name=None,
                    proxy_api_key_id=None,
                ),
                _row(
                    proxy_api_key_name_snapshot=None,
                    current_proxy_api_key_name=None,
                    proxy_api_key_id=None,
                ),
            ]
        )

        events = await _load_snapshot_events(
            cast(AsyncSession, db),
            profile_id=1,
            start_at=None,
            end_at=datetime(2026, 4, 11, tzinfo=timezone.utc),
        )

        rows = _build_proxy_api_key_statistics(events)

        assert rows == [
            {
                "proxy_api_key_id": None,
                "proxy_api_key_label": "No proxy API key",
                "request_count": 2,
                "success_rate": 100.0,
                "total_tokens": 6,
                "total_cost_micros": 0,
            }
        ]

    asyncio.run(run())
