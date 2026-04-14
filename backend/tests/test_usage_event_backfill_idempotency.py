from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from datetime import datetime, timedelta, timezone
from typing import cast

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.models import UsageRequestEvent
from app.services.stats import usage_events as usage_events_module
from tests.usage_event_spend_seed import (
    BillingExpectation,
    _request_log,
    _usage_event,
    build_usage_stats_test_session,
    seed_canonical_spend_migration_dataset,
)

UTC = timezone.utc


def _get_backfill_fn() -> Callable[..., Awaitable[dict[str, int]]]:
    backfill_fn = getattr(
        usage_events_module,
        "backfill_usage_request_event_billing_fields",
        None,
    )
    assert callable(backfill_fn)
    return cast(Callable[..., Awaitable[dict[str, int]]], backfill_fn)


def test_usage_event_backfill_is_idempotent_and_uses_the_final_attempt() -> None:
    async def run() -> None:
        async_db, session = build_usage_stats_test_session()
        seed = seed_canonical_spend_migration_dataset(session)

        try:
            typed_backfill_fn = _get_backfill_fn()

            first_result = await typed_backfill_fn(
                cast(AsyncSession, async_db),
                profile_id=seed.profile_id,
            )
            first_rows = {
                row.ingress_request_id: BillingExpectation(
                    billable_flag=bool(getattr(row, "billable_flag")),
                    priced_flag=bool(getattr(row, "priced_flag")),
                    unpriced_reason=getattr(row, "unpriced_reason"),
                )
                for row in session.execute(
                    select(UsageRequestEvent).order_by(
                        UsageRequestEvent.ingress_request_id
                    )
                )
                .scalars()
                .all()
            }

            second_result = await typed_backfill_fn(
                cast(AsyncSession, async_db),
                profile_id=seed.profile_id,
            )
            second_rows = {
                row.ingress_request_id: BillingExpectation(
                    billable_flag=bool(getattr(row, "billable_flag")),
                    priced_flag=bool(getattr(row, "priced_flag")),
                    unpriced_reason=getattr(row, "unpriced_reason"),
                )
                for row in session.execute(
                    select(UsageRequestEvent).order_by(
                        UsageRequestEvent.ingress_request_id
                    )
                )
                .scalars()
                .all()
            }

            assert first_result == {
                "matched_request_log_count": seed.matched_request_log_count,
                "unmatched_usage_event_count": seed.unmatched_usage_event_count,
                "duplicate_candidate_count": seed.duplicate_candidate_count,
            }
            assert second_result == first_result
            assert first_rows == seed.billing_by_ingress
            assert second_rows == first_rows
        finally:
            session.close()

    asyncio.run(run())


def test_usage_event_backfill_uses_created_at_then_id_tie_breakers() -> None:
    async def run() -> None:
        async_db, session = build_usage_stats_test_session()
        base_at = datetime(2026, 4, 11, 9, 0, tzinfo=UTC)

        try:
            session.add(
                _usage_event(
                    ingress_request_id="req-tie-break",
                    model_id="gpt-5.4",
                    endpoint_id=10,
                    created_at=base_at + timedelta(minutes=2),
                    status_code=200,
                    success_flag=True,
                    attempt_count=2,
                    billable_flag=None,
                    priced_flag=None,
                    unpriced_reason=None,
                    total_tokens=360,
                    total_cost_user_currency_micros=220_000,
                )
            )
            session.add_all(
                [
                    _request_log(
                        ingress_request_id="req-tie-break",
                        attempt_number=2,
                        model_id="gpt-5.4",
                        endpoint_id=10,
                        created_at=base_at,
                        status_code=200,
                        success_flag=True,
                        billable_flag=False,
                        priced_flag=False,
                        unpriced_reason="OLDER_CREATED_AT",
                        total_tokens=300,
                        total_cost_user_currency_micros=0,
                    ),
                    _request_log(
                        ingress_request_id="req-tie-break",
                        attempt_number=2,
                        model_id="gpt-5.4",
                        endpoint_id=10,
                        created_at=base_at + timedelta(minutes=1),
                        status_code=200,
                        success_flag=True,
                        billable_flag=True,
                        priced_flag=False,
                        unpriced_reason="LOWER_ID_AT_SAME_CREATED_AT",
                        total_tokens=340,
                        total_cost_user_currency_micros=0,
                    ),
                    _request_log(
                        ingress_request_id="req-tie-break",
                        attempt_number=2,
                        model_id="gpt-5.4",
                        endpoint_id=10,
                        created_at=base_at + timedelta(minutes=1),
                        status_code=200,
                        success_flag=True,
                        billable_flag=True,
                        priced_flag=True,
                        unpriced_reason=None,
                        total_tokens=360,
                        total_cost_user_currency_micros=220_000,
                    ),
                ]
            )
            session.commit()

            result = await _get_backfill_fn()(
                cast(AsyncSession, async_db), profile_id=1
            )
            usage_event = session.execute(
                select(UsageRequestEvent).where(
                    UsageRequestEvent.ingress_request_id == "req-tie-break"
                )
            ).scalar_one()

            assert result == {
                "matched_request_log_count": 1,
                "unmatched_usage_event_count": 0,
                "duplicate_candidate_count": 1,
            }
            assert usage_event.billable_flag is True
            assert usage_event.priced_flag is True
            assert usage_event.unpriced_reason is None
        finally:
            session.close()

    asyncio.run(run())
