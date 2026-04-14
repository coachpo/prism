from __future__ import annotations

import asyncio
from datetime import datetime, timedelta, timezone
from typing import cast

from sqlalchemy import Table, create_engine
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import Session
from sqlalchemy.pool import StaticPool

from app.core.database import Base
from app.models.models import Endpoint, ModelConfig, UsageRequestEvent
from app.schemas.schemas import UsageModelStatistic
from app.services.stats.endpoint_model_statistics import get_endpoint_model_statistics


class _SyncAsyncSession:
    def __init__(self, session: Session) -> None:
        self._session = session

    def get_bind(self):
        return self._session.get_bind()

    async def scalar(self, statement):
        return self._session.scalar(statement)

    async def execute(self, statement):
        return self._session.execute(statement)


def _build_test_session() -> tuple[_SyncAsyncSession, Session]:
    engine = create_engine(
        "sqlite://",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )
    Base.metadata.create_all(
        bind=engine,
        tables=cast(
            list[Table],
            [
                Endpoint.__table__,
                ModelConfig.__table__,
                UsageRequestEvent.__table__,
            ],
        ),
    )
    session = Session(engine)
    return _SyncAsyncSession(session), session


def _usage_event(
    *,
    ingress_request_id: str,
    endpoint_id: int,
    model_id: str,
    created_at: datetime,
    total_tokens: int | None,
    output_tokens: int | None,
    completion_duration_ms: int | None,
    ttft_ms: int | None,
) -> UsageRequestEvent:
    return UsageRequestEvent(
        profile_id=1,
        ingress_request_id=ingress_request_id,
        model_id=model_id,
        resolved_target_model_id=None,
        api_family="openai",
        endpoint_id=endpoint_id,
        connection_id=None,
        proxy_api_key_id=None,
        proxy_api_key_name_snapshot=None,
        status_code=200,
        response_time_ms=1000,
        ttft_ms=ttft_ms,
        completion_duration_ms=completion_duration_ms,
        success_flag=True,
        input_tokens=None,
        output_tokens=output_tokens,
        total_tokens=total_tokens,
        total_cost_user_currency_micros=0,
        attempt_count=1,
        request_path="/v1/chat/completions",
        created_at=created_at,
    )


def test_mixed_null_ttft_rows_return_null_output_rate_and_compute_percentiles() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        endpoint_id = 10
        model_id = "gpt-5.4"
        created_at = datetime(2026, 4, 11, 12, 0, tzinfo=timezone.utc)

        try:
            session.add(
                Endpoint(
                    id=endpoint_id,
                    profile_id=1,
                    name="Primary endpoint",
                    base_url="https://example.com",
                    api_key="test-key",
                    position=1,
                )
            )
            session.add(
                ModelConfig(
                    id=21,
                    profile_id=1,
                    vendor_id=None,
                    api_family="openai",
                    model_id=model_id,
                    display_name="GPT 5.4",
                    model_type="proxy",
                    loadbalance_strategy_id=None,
                    is_enabled=True,
                )
            )
            session.add_all(
                [
                    _usage_event(
                        ingress_request_id="req-1",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at,
                        total_tokens=100,
                        output_tokens=90,
                        completion_duration_ms=1000,
                        ttft_ms=100,
                    ),
                    _usage_event(
                        ingress_request_id="req-2",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at + timedelta(minutes=1),
                        total_tokens=300,
                        output_tokens=220,
                        completion_duration_ms=1500,
                        ttft_ms=400,
                    ),
                    _usage_event(
                        ingress_request_id="req-3",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at + timedelta(minutes=2),
                        total_tokens=500,
                        output_tokens=500,
                        completion_duration_ms=2500,
                        ttft_ms=None,
                    ),
                ]
            )
            session.commit()

            rows = await get_endpoint_model_statistics(
                cast(AsyncSession, async_db),
                profile_id=1,
                endpoint_id=endpoint_id,
                from_time=created_at - timedelta(minutes=1),
                to_time=created_at + timedelta(minutes=3),
            )

            assert rows == [
                {
                    "model_id": model_id,
                    "model_label": "GPT 5.4",
                    "request_count": 3,
                    "success_rate": 100.0,
                    "total_tokens": 900,
                    "total_cost_micros": 0,
                    "p50_ttft_ms": 250,
                    "p95_ttft_ms": 385,
                    "avg_output_rate_tps": None,
                }
            ]

            statistic = UsageModelStatistic.model_validate(rows[0])
            assert statistic.p50_ttft_ms == 250
            assert statistic.p95_ttft_ms == 385
            assert statistic.avg_output_rate_tps is None
        finally:
            session.close()

    asyncio.run(run())


def test_all_ineligible_ttft_rows_return_null_percentiles_and_output_rate() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        endpoint_id = 11
        model_id = "gpt-5.4-mini"
        created_at = datetime(2026, 4, 11, 12, 0, tzinfo=timezone.utc)

        try:
            session.add(
                Endpoint(
                    id=endpoint_id,
                    profile_id=1,
                    name="Historical endpoint",
                    base_url="https://example.com",
                    api_key="test-key",
                    position=1,
                )
            )
            session.add(
                ModelConfig(
                    id=22,
                    profile_id=1,
                    vendor_id=None,
                    api_family="openai",
                    model_id=model_id,
                    display_name="GPT 5.4 Mini",
                    model_type="proxy",
                    loadbalance_strategy_id=None,
                    is_enabled=True,
                )
            )
            session.add_all(
                [
                    _usage_event(
                        ingress_request_id="req-4",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at,
                        total_tokens=100,
                        output_tokens=100,
                        completion_duration_ms=1000,
                        ttft_ms=None,
                    ),
                    _usage_event(
                        ingress_request_id="req-5",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at + timedelta(minutes=1),
                        total_tokens=300,
                        output_tokens=None,
                        completion_duration_ms=1500,
                        ttft_ms=None,
                    ),
                ]
            )
            session.commit()

            rows = await get_endpoint_model_statistics(
                cast(AsyncSession, async_db),
                profile_id=1,
                endpoint_id=endpoint_id,
                from_time=created_at - timedelta(minutes=1),
                to_time=created_at + timedelta(minutes=2),
            )

            assert rows == [
                {
                    "model_id": model_id,
                    "model_label": "GPT 5.4 Mini",
                    "request_count": 2,
                    "success_rate": 100.0,
                    "total_tokens": 400,
                    "total_cost_micros": 0,
                    "p50_ttft_ms": None,
                    "p95_ttft_ms": None,
                    "avg_output_rate_tps": None,
                }
            ]

            statistic = UsageModelStatistic.model_validate(rows[0])
            assert statistic.p50_ttft_ms is None
            assert statistic.p95_ttft_ms is None
            assert statistic.avg_output_rate_tps is None
        finally:
            session.close()

    asyncio.run(run())
