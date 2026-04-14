from __future__ import annotations

import asyncio
from typing import cast

from sqlalchemy.ext.asyncio import AsyncSession

from app.schemas.schemas import SpendingReportResponse
from app.services.stats.spending import get_spending_report
from tests.usage_event_spend_seed import (
    build_usage_stats_test_session,
    seed_canonical_spend_migration_dataset,
)


def test_spending_report_grouped_by_model_requires_usage_request_event_counts() -> None:
    async def run() -> None:
        async_db, session = build_usage_stats_test_session()
        seed = seed_canonical_spend_migration_dataset(session)

        try:
            report = SpendingReportResponse.model_validate(
                await get_spending_report(
                    cast(AsyncSession, async_db),
                    profile_id=seed.profile_id,
                    from_time=seed.report_start_at,
                    to_time=seed.report_end_at,
                    group_by="model",
                )
            )

            assert [row.model_dump() for row in report.groups] == [
                {
                    "key": seed.primary_model.model_id,
                    "total_cost_micros": seed.primary_model.total_cost_micros,
                    "total_requests": seed.primary_model.request_count,
                    "priced_requests": seed.primary_model.priced_request_count,
                    "unpriced_requests": seed.primary_model.unpriced_request_count,
                    "total_tokens": seed.primary_model.total_tokens,
                },
                {
                    "key": seed.secondary_model.model_id,
                    "total_cost_micros": seed.secondary_model.total_cost_micros,
                    "total_requests": seed.secondary_model.request_count,
                    "priced_requests": seed.secondary_model.priced_request_count,
                    "unpriced_requests": seed.secondary_model.unpriced_request_count,
                    "total_tokens": seed.secondary_model.total_tokens,
                },
            ]
            assert report.unpriced_breakdown == {
                "MISSING_PRICE_DATA": 1,
                "MISSING_REQUEST_LOG_BACKFILL": 1,
            }
        finally:
            session.close()

    asyncio.run(run())
