from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from typing import cast

from sqlalchemy import Table, create_engine
from sqlalchemy.orm import Session
from sqlalchemy.pool import StaticPool

from app.core.database import Base
from app.models.models import (
    Endpoint,
    ModelConfig,
    ProxyApiKey,
    RequestLog,
    UsageRequestEvent,
    UserSetting,
)

UTC = timezone.utc


@dataclass(frozen=True)
class BillingExpectation:
    billable_flag: bool
    priced_flag: bool
    unpriced_reason: str | None


@dataclass(frozen=True)
class ModelSpendExpectation:
    model_id: str
    request_count: int
    priced_request_count: int
    unpriced_request_count: int
    total_cost_micros: int
    total_tokens: int


@dataclass(frozen=True)
class CanonicalSpendSeed:
    profile_id: int
    report_start_at: datetime
    report_end_at: datetime
    primary_model: ModelSpendExpectation
    secondary_model: ModelSpendExpectation
    retry_ingress_request_id: str
    unmatched_ingress_request_id: str
    canonical_total_cost_micros: int
    canonical_successful_request_count: int
    canonical_priced_request_count: int
    canonical_unpriced_request_count: int
    matched_request_log_count: int
    unmatched_usage_event_count: int
    duplicate_candidate_count: int
    billing_by_ingress: dict[str, BillingExpectation]


class SyncAsyncSession:
    def __init__(self, session: Session) -> None:
        self._session = session

    def add(self, instance: object) -> None:
        self._session.add(instance)

    def add_all(self, instances: list[object]) -> None:
        self._session.add_all(instances)

    async def commit(self) -> None:
        self._session.commit()

    async def execute(self, statement):
        return self._session.execute(statement)

    async def flush(self) -> None:
        self._session.flush()

    def get_bind(self):
        return self._session.get_bind()

    async def refresh(self, instance: object) -> None:
        self._session.refresh(instance)

    async def rollback(self) -> None:
        self._session.rollback()

    async def scalar(self, statement):
        return self._session.scalar(statement)


def build_usage_stats_test_session() -> tuple[SyncAsyncSession, Session]:
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
                ProxyApiKey.__table__,
                RequestLog.__table__,
                UsageRequestEvent.__table__,
                UserSetting.__table__,
            ],
        ),
    )
    session = Session(engine)
    return SyncAsyncSession(session), session


def _request_log(
    *,
    ingress_request_id: str,
    attempt_number: int,
    model_id: str,
    endpoint_id: int,
    created_at: datetime,
    status_code: int,
    success_flag: bool,
    billable_flag: bool,
    priced_flag: bool,
    unpriced_reason: str | None,
    total_tokens: int,
    total_cost_user_currency_micros: int,
) -> RequestLog:
    return RequestLog(
        profile_id=1,
        model_id=model_id,
        api_family="openai",
        resolved_target_model_id=None,
        endpoint_id=endpoint_id,
        connection_id=None,
        proxy_api_key_id=None,
        proxy_api_key_name_snapshot=None,
        ingress_request_id=ingress_request_id,
        attempt_number=attempt_number,
        provider_correlation_id=f"provider-{ingress_request_id}-{attempt_number}",
        endpoint_base_url=f"https://endpoint-{endpoint_id}.example.com",
        status_code=status_code,
        response_time_ms=1000 + attempt_number,
        ttft_ms=120 if success_flag else None,
        completion_duration_ms=1100 if success_flag else None,
        is_stream=False,
        input_tokens=max(total_tokens - 80, 0),
        output_tokens=min(total_tokens, 80),
        total_tokens=total_tokens,
        success_flag=success_flag,
        billable_flag=billable_flag,
        priced_flag=priced_flag,
        unpriced_reason=unpriced_reason,
        cache_read_input_tokens=0,
        cache_creation_input_tokens=0,
        reasoning_tokens=0,
        input_cost_micros=0,
        output_cost_micros=0,
        cache_read_input_cost_micros=0,
        cache_creation_input_cost_micros=0,
        reasoning_cost_micros=0,
        total_cost_original_micros=total_cost_user_currency_micros,
        total_cost_user_currency_micros=total_cost_user_currency_micros,
        currency_code_original="USD",
        report_currency_code="USD",
        report_currency_symbol="$",
        fx_rate_used="1.000000",
        fx_rate_source="DEFAULT_1_TO_1",
        pricing_snapshot_unit="1M tokens",
        pricing_snapshot_input="0.000000",
        pricing_snapshot_output="0.000000",
        pricing_snapshot_cache_read_input="0.000000",
        pricing_snapshot_cache_creation_input="0.000000",
        pricing_snapshot_reasoning="0.000000",
        pricing_config_version_used=1,
        request_path="/v1/chat/completions",
        error_detail=None if success_flag else "upstream failure",
        endpoint_description=f"Endpoint {endpoint_id}",
        created_at=created_at,
    )


def _usage_event(
    *,
    ingress_request_id: str,
    model_id: str,
    endpoint_id: int,
    created_at: datetime,
    status_code: int,
    success_flag: bool,
    attempt_count: int,
    billable_flag: bool | None,
    priced_flag: bool | None,
    unpriced_reason: str | None,
    total_tokens: int,
    total_cost_user_currency_micros: int,
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
        status_code=status_code,
        response_time_ms=1000,
        ttft_ms=120 if success_flag else None,
        completion_duration_ms=1100 if success_flag else None,
        success_flag=success_flag,
        billable_flag=billable_flag,
        priced_flag=priced_flag,
        unpriced_reason=unpriced_reason,
        input_tokens=max(total_tokens - 80, 0),
        output_tokens=min(total_tokens, 80),
        total_tokens=total_tokens,
        cache_read_input_tokens=0,
        cache_creation_input_tokens=0,
        reasoning_tokens=0,
        input_cost_micros=0,
        output_cost_micros=0,
        cache_read_input_cost_micros=0,
        cache_creation_input_cost_micros=0,
        reasoning_cost_micros=0,
        total_cost_original_micros=total_cost_user_currency_micros,
        total_cost_user_currency_micros=total_cost_user_currency_micros,
        currency_code_original="USD",
        report_currency_code="USD",
        report_currency_symbol="$",
        fx_rate_used="1.000000",
        fx_rate_source="DEFAULT_1_TO_1",
        pricing_snapshot_unit="1M tokens",
        pricing_snapshot_input="0.000000",
        pricing_snapshot_output="0.000000",
        pricing_snapshot_cache_read_input="0.000000",
        pricing_snapshot_cache_creation_input="0.000000",
        pricing_snapshot_reasoning="0.000000",
        pricing_config_version_used=1,
        attempt_count=attempt_count,
        request_path="/v1/chat/completions",
        created_at=created_at,
    )


def seed_canonical_spend_migration_dataset(session: Session) -> CanonicalSpendSeed:
    base_at = datetime(2026, 4, 10, 12, 0, tzinfo=UTC)
    primary_model_id = "gpt-5.4"
    secondary_model_id = "claude-3.7-sonnet"
    retry_ingress_request_id = "req-retry-final"
    unmatched_ingress_request_id = "req-historical-unmatched"

    session.add(
        UserSetting(
            profile_id=1,
            report_currency_code="USD",
            report_currency_symbol="$",
        )
    )
    session.add_all(
        [
            Endpoint(
                id=10,
                profile_id=1,
                name="Primary endpoint",
                base_url="https://primary.example.com",
                api_key="test-key",
                position=1,
            ),
            Endpoint(
                id=20,
                profile_id=1,
                name="Archive endpoint",
                base_url="https://archive.example.com",
                api_key="test-key",
                position=2,
            ),
            ModelConfig(
                id=11,
                profile_id=1,
                vendor_id=None,
                api_family="openai",
                model_id=primary_model_id,
                display_name="GPT 5.4",
                model_type="proxy",
                loadbalance_strategy_id=None,
                is_enabled=True,
            ),
            ModelConfig(
                id=12,
                profile_id=1,
                vendor_id=None,
                api_family="openai",
                model_id=secondary_model_id,
                display_name="Claude 3.7 Sonnet",
                model_type="proxy",
                loadbalance_strategy_id=None,
                is_enabled=True,
            ),
        ]
    )
    session.add_all(
        [
            _request_log(
                ingress_request_id="req-priced-success",
                attempt_number=1,
                model_id=primary_model_id,
                endpoint_id=10,
                created_at=base_at,
                status_code=200,
                success_flag=True,
                billable_flag=True,
                priced_flag=True,
                unpriced_reason=None,
                total_tokens=300,
                total_cost_user_currency_micros=120_000,
            ),
            _request_log(
                ingress_request_id="req-unpriced-success",
                attempt_number=1,
                model_id=primary_model_id,
                endpoint_id=10,
                created_at=base_at + timedelta(minutes=5),
                status_code=200,
                success_flag=True,
                billable_flag=True,
                priced_flag=False,
                unpriced_reason="MISSING_PRICE_DATA",
                total_tokens=240,
                total_cost_user_currency_micros=0,
            ),
            _request_log(
                ingress_request_id="req-failed",
                attempt_number=1,
                model_id=secondary_model_id,
                endpoint_id=20,
                created_at=base_at + timedelta(minutes=10),
                status_code=503,
                success_flag=False,
                billable_flag=False,
                priced_flag=False,
                unpriced_reason=None,
                total_tokens=160,
                total_cost_user_currency_micros=0,
            ),
            _request_log(
                ingress_request_id=retry_ingress_request_id,
                attempt_number=1,
                model_id=primary_model_id,
                endpoint_id=10,
                created_at=base_at + timedelta(minutes=15),
                status_code=429,
                success_flag=False,
                billable_flag=False,
                priced_flag=False,
                unpriced_reason=None,
                total_tokens=180,
                total_cost_user_currency_micros=0,
            ),
            _request_log(
                ingress_request_id=retry_ingress_request_id,
                attempt_number=2,
                model_id=primary_model_id,
                endpoint_id=10,
                created_at=base_at + timedelta(minutes=16),
                status_code=200,
                success_flag=True,
                billable_flag=True,
                priced_flag=True,
                unpriced_reason=None,
                total_tokens=520,
                total_cost_user_currency_micros=500_000,
            ),
            _request_log(
                ingress_request_id="req-secondary-priced",
                attempt_number=1,
                model_id=secondary_model_id,
                endpoint_id=20,
                created_at=base_at + timedelta(minutes=20),
                status_code=200,
                success_flag=True,
                billable_flag=True,
                priced_flag=True,
                unpriced_reason=None,
                total_tokens=480,
                total_cost_user_currency_micros=90_000,
            ),
        ]
    )
    session.add_all(
        [
            _usage_event(
                ingress_request_id="req-priced-success",
                model_id=primary_model_id,
                endpoint_id=10,
                created_at=base_at,
                status_code=200,
                success_flag=True,
                attempt_count=1,
                billable_flag=True,
                priced_flag=True,
                unpriced_reason=None,
                total_tokens=300,
                total_cost_user_currency_micros=120_000,
            ),
            _usage_event(
                ingress_request_id="req-unpriced-success",
                model_id=primary_model_id,
                endpoint_id=10,
                created_at=base_at + timedelta(minutes=5),
                status_code=200,
                success_flag=True,
                attempt_count=1,
                billable_flag=True,
                priced_flag=False,
                unpriced_reason="MISSING_PRICE_DATA",
                total_tokens=240,
                total_cost_user_currency_micros=0,
            ),
            _usage_event(
                ingress_request_id="req-failed",
                model_id=secondary_model_id,
                endpoint_id=20,
                created_at=base_at + timedelta(minutes=10),
                status_code=503,
                success_flag=False,
                attempt_count=1,
                billable_flag=False,
                priced_flag=False,
                unpriced_reason=None,
                total_tokens=160,
                total_cost_user_currency_micros=0,
            ),
            _usage_event(
                ingress_request_id=retry_ingress_request_id,
                model_id=primary_model_id,
                endpoint_id=10,
                created_at=base_at + timedelta(minutes=16),
                status_code=200,
                success_flag=True,
                attempt_count=2,
                billable_flag=True,
                priced_flag=True,
                unpriced_reason=None,
                total_tokens=520,
                total_cost_user_currency_micros=500_000,
            ),
            _usage_event(
                ingress_request_id="req-secondary-priced",
                model_id=secondary_model_id,
                endpoint_id=20,
                created_at=base_at + timedelta(minutes=20),
                status_code=200,
                success_flag=True,
                attempt_count=1,
                billable_flag=True,
                priced_flag=True,
                unpriced_reason=None,
                total_tokens=480,
                total_cost_user_currency_micros=90_000,
            ),
            _usage_event(
                ingress_request_id=unmatched_ingress_request_id,
                model_id=secondary_model_id,
                endpoint_id=20,
                created_at=base_at + timedelta(minutes=25),
                status_code=200,
                success_flag=True,
                attempt_count=1,
                billable_flag=False,
                priced_flag=False,
                unpriced_reason="MISSING_REQUEST_LOG_BACKFILL",
                total_tokens=360,
                total_cost_user_currency_micros=330_000,
            ),
        ]
    )
    session.commit()

    return CanonicalSpendSeed(
        profile_id=1,
        report_start_at=base_at - timedelta(minutes=1),
        report_end_at=base_at + timedelta(minutes=30),
        primary_model=ModelSpendExpectation(
            model_id=primary_model_id,
            request_count=3,
            priced_request_count=2,
            unpriced_request_count=1,
            total_cost_micros=620_000,
            total_tokens=1_060,
        ),
        secondary_model=ModelSpendExpectation(
            model_id=secondary_model_id,
            request_count=2,
            priced_request_count=1,
            unpriced_request_count=1,
            total_cost_micros=90_000,
            total_tokens=840,
        ),
        retry_ingress_request_id=retry_ingress_request_id,
        unmatched_ingress_request_id=unmatched_ingress_request_id,
        canonical_total_cost_micros=710_000,
        canonical_successful_request_count=5,
        canonical_priced_request_count=3,
        canonical_unpriced_request_count=2,
        matched_request_log_count=5,
        unmatched_usage_event_count=1,
        duplicate_candidate_count=1,
        billing_by_ingress={
            "req-priced-success": BillingExpectation(True, True, None),
            "req-unpriced-success": BillingExpectation(
                True,
                False,
                "MISSING_PRICE_DATA",
            ),
            "req-failed": BillingExpectation(False, False, None),
            retry_ingress_request_id: BillingExpectation(True, True, None),
            "req-secondary-priced": BillingExpectation(True, True, None),
            unmatched_ingress_request_id: BillingExpectation(
                False,
                False,
                "MISSING_REQUEST_LOG_BACKFILL",
            ),
        },
    )


__all__ = [
    "BillingExpectation",
    "CanonicalSpendSeed",
    "ModelSpendExpectation",
    "SyncAsyncSession",
    "build_usage_stats_test_session",
    "seed_canonical_spend_migration_dataset",
]
