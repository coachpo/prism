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
    billable_flag: bool = True,
    priced_flag: bool = True,
    total_cost_user_currency_micros: int = 0,
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
        billable_flag=billable_flag,
        priced_flag=priced_flag,
        input_tokens=None,
        output_tokens=output_tokens,
        total_tokens=total_tokens,
        total_cost_user_currency_micros=total_cost_user_currency_micros,
        attempt_count=1,
        request_path="/v1/chat/completions",
        created_at=created_at,
    )


def test_streamed_rows_return_avg_output_rate_tps_for_eligible_decode_windows() -> None:
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
                        output_tokens=100,
                        completion_duration_ms=5100,
                        ttft_ms=100,
                    ),
                    _usage_event(
                        ingress_request_id="req-2",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at + timedelta(minutes=1),
                        total_tokens=300,
                        output_tokens=180,
                        completion_duration_ms=4600,
                        ttft_ms=100,
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
                    "model_label": "GPT 5.4",
                    "request_count": 2,
                    "priced_request_count": 2,
                    "success_rate": 100.0,
                    "total_tokens": 400,
                    "total_cost_micros": 0,
                    "p50_ttft_ms": 100,
                    "p95_ttft_ms": 100,
                    "avg_output_rate_tps": 30.0,
                    "unpriced_request_count": 0,
                }
            ]

            statistic = UsageModelStatistic.model_validate(rows[0])
            assert statistic.avg_output_rate_tps == 30.0
        finally:
            session.close()

    asyncio.run(run())


def test_zero_output_rows_still_return_zero_avg_output_rate_tps() -> None:
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
                        ingress_request_id="req-3",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at,
                        total_tokens=50,
                        output_tokens=0,
                        completion_duration_ms=1000,
                        ttft_ms=200,
                    ),
                ]
            )
            session.commit()

            rows = await get_endpoint_model_statistics(
                cast(AsyncSession, async_db),
                profile_id=1,
                endpoint_id=endpoint_id,
                from_time=created_at - timedelta(days=1),
                to_time=created_at + timedelta(minutes=1),
            )

            assert rows == [
                {
                    "model_id": model_id,
                    "model_label": "GPT 5.4 Mini",
                    "request_count": 1,
                    "priced_request_count": 1,
                    "success_rate": 100.0,
                    "total_tokens": 50,
                    "total_cost_micros": 0,
                    "p50_ttft_ms": 200,
                    "p95_ttft_ms": 200,
                    "avg_output_rate_tps": 0.0,
                    "unpriced_request_count": 0,
                }
            ]

            statistic = UsageModelStatistic.model_validate(rows[0])
            assert statistic.avg_output_rate_tps == 0.0
        finally:
            session.close()

    asyncio.run(run())


def test_total_cost_micros_excludes_unbillable_rows() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        endpoint_id = 12
        model_id = "gpt-5.4"
        created_at = datetime(2026, 4, 11, 12, 0, tzinfo=timezone.utc)

        try:
            session.add(
                Endpoint(
                    id=endpoint_id,
                    profile_id=1,
                    name="Spend endpoint",
                    base_url="https://example.com",
                    api_key="test-key",
                    position=1,
                )
            )
            session.add(
                ModelConfig(
                    id=23,
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
                        ingress_request_id="req-billable",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at,
                        total_tokens=100,
                        output_tokens=0,
                        completion_duration_ms=1000,
                        ttft_ms=100,
                        billable_flag=True,
                        priced_flag=True,
                        total_cost_user_currency_micros=100_000,
                    ),
                    _usage_event(
                        ingress_request_id="req-unbillable",
                        endpoint_id=endpoint_id,
                        model_id=model_id,
                        created_at=created_at + timedelta(minutes=1),
                        total_tokens=120,
                        output_tokens=0,
                        completion_duration_ms=1000,
                        ttft_ms=100,
                        billable_flag=False,
                        priced_flag=False,
                        total_cost_user_currency_micros=900_000,
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
                    "model_label": "GPT 5.4",
                    "request_count": 2,
                    "priced_request_count": 1,
                    "success_rate": 100.0,
                    "total_tokens": 220,
                    "total_cost_micros": 100_000,
                    "p50_ttft_ms": 100,
                    "p95_ttft_ms": 100,
                    "avg_output_rate_tps": 0.0,
                    "unpriced_request_count": 1,
                }
            ]

            statistic = UsageModelStatistic.model_validate(rows[0])
            assert statistic.total_cost_micros == 100_000
        finally:
            session.close()

    asyncio.run(run())
