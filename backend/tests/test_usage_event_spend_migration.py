from __future__ import annotations

import asyncio
from typing import cast

from sqlalchemy.ext.asyncio import AsyncSession

from app.schemas.schemas import SpendingReportResponse, UsageSnapshotResponse
from app.services.stats.model_metrics import get_model_metrics_batch
from app.services.stats.spending import get_spending_report
from app.services.stats.usage_snapshot import get_usage_snapshot
from tests.usage_event_spend_seed import (
    build_usage_stats_test_session,
    seed_canonical_spend_migration_dataset,
)


def test_spending_report_seed_requires_canonical_usage_event_request_counts() -> None:
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

            assert report.summary.total_cost_micros == seed.canonical_total_cost_micros
            assert (
                report.summary.successful_request_count
                == seed.canonical_successful_request_count
            )
            assert (
                report.summary.priced_request_count
                == seed.canonical_priced_request_count
            )
            assert (
                report.summary.unpriced_request_count
                == seed.canonical_unpriced_request_count
            )
        finally:
            session.close()

    asyncio.run(run())


def test_usage_snapshot_seed_requires_billing_semantics_for_spend_totals() -> None:
    async def run() -> None:
        async_db, session = build_usage_stats_test_session()
        seed = seed_canonical_spend_migration_dataset(session)

        try:
            snapshot = UsageSnapshotResponse.model_validate(
                await get_usage_snapshot(
                    cast(AsyncSession, async_db),
                    profile_id=seed.profile_id,
                    preset="all",
                )
            )

            assert (
                snapshot.overview.total_cost_micros == seed.canonical_total_cost_micros
            )
            assert (
                snapshot.cost_overview.priced_request_count
                == seed.canonical_priced_request_count
            )
            assert (
                snapshot.cost_overview.unpriced_request_count
                == seed.canonical_unpriced_request_count
            )
            assert {
                item.model_id: {
                    "request_count": item.request_count,
                    "success_count": item.success_count,
                    "failed_count": item.failed_count,
                    "priced_request_count": item.priced_request_count,
                    "unpriced_request_count": item.unpriced_request_count,
                    "input_tokens": item.input_tokens,
                    "output_tokens": item.output_tokens,
                    "cached_tokens": item.cached_tokens,
                    "reasoning_tokens": item.reasoning_tokens,
                    "total_tokens": item.total_tokens,
                    "total_cost_micros": item.total_cost_micros,
                }
                for item in snapshot.model_statistics
            } == {
                seed.primary_model.model_id: {
                    "request_count": 3,
                    "success_count": 3,
                    "failed_count": 0,
                    "priced_request_count": 2,
                    "unpriced_request_count": 1,
                    "input_tokens": 820,
                    "output_tokens": 240,
                    "cached_tokens": 0,
                    "reasoning_tokens": 0,
                    "total_tokens": 1_060,
                    "total_cost_micros": seed.primary_model.total_cost_micros,
                },
                seed.secondary_model.model_id: {
                    "request_count": 3,
                    "success_count": 2,
                    "failed_count": 1,
                    "priced_request_count": 1,
                    "unpriced_request_count": 1,
                    "input_tokens": 760,
                    "output_tokens": 240,
                    "cached_tokens": 0,
                    "reasoning_tokens": 0,
                    "total_tokens": 1_000,
                    "total_cost_micros": seed.secondary_model.total_cost_micros,
                },
            }
        finally:
            session.close()

    asyncio.run(run())


def test_usage_snapshot_model_aggregate_sums_match_global_totals_for_all_models() -> (
    None
):
    async def run() -> None:
        async_db, session = build_usage_stats_test_session()
        seed = seed_canonical_spend_migration_dataset(session)

        try:
            snapshot = UsageSnapshotResponse.model_validate(
                await get_usage_snapshot(
                    cast(AsyncSession, async_db),
                    profile_id=seed.profile_id,
                    preset="all",
                )
            )

            assert sum(item.request_count for item in snapshot.model_statistics) == (
                snapshot.overview.total_requests
            )
            assert (
                sum((item.success_count or 0) for item in snapshot.model_statistics)
                == snapshot.overview.success_requests
            )
            assert (
                sum((item.failed_count or 0) for item in snapshot.model_statistics)
                == snapshot.overview.failed_requests
            )
            assert (
                sum(
                    (item.priced_request_count or 0)
                    for item in snapshot.model_statistics
                )
                == snapshot.cost_overview.priced_request_count
            )
            assert (
                sum(
                    (item.unpriced_request_count or 0)
                    for item in snapshot.model_statistics
                )
                == snapshot.cost_overview.unpriced_request_count
            )
            assert (
                sum((item.input_tokens or 0) for item in snapshot.model_statistics)
                == snapshot.overview.input_tokens
            )
            assert (
                sum((item.output_tokens or 0) for item in snapshot.model_statistics)
                == snapshot.overview.output_tokens
            )
            assert (
                sum((item.cached_tokens or 0) for item in snapshot.model_statistics)
                == snapshot.overview.cached_tokens
            )
            assert (
                sum((item.reasoning_tokens or 0) for item in snapshot.model_statistics)
                == snapshot.overview.reasoning_tokens
            )
            assert sum(item.total_tokens for item in snapshot.model_statistics) == (
                snapshot.overview.total_tokens
            )
            assert (
                sum(item.total_cost_micros for item in snapshot.model_statistics)
                == snapshot.overview.total_cost_micros
            )
        finally:
            session.close()

    asyncio.run(run())


def test_spending_report_and_usage_snapshot_reconcile_for_seeded_scope() -> None:
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
            snapshot = UsageSnapshotResponse.model_validate(
                await get_usage_snapshot(
                    cast(AsyncSession, async_db),
                    profile_id=seed.profile_id,
                    preset="all",
                )
            )

            assert (
                report.summary.total_cost_micros
                == snapshot.overview.total_cost_micros
                == snapshot.cost_overview.total_cost_micros
            )
            assert (
                report.summary.successful_request_count
                == snapshot.overview.success_requests
            )
            assert (
                report.summary.priced_request_count
                == snapshot.cost_overview.priced_request_count
            )
            assert (
                report.summary.unpriced_request_count
                == snapshot.cost_overview.unpriced_request_count
            )
            assert {row.key: row.total_cost_micros for row in report.groups} == {
                item.model_id: item.total_cost_micros
                for item in snapshot.model_statistics
            }
        finally:
            session.close()

    asyncio.run(run())


def test_model_metrics_seed_uses_usage_request_event_spend_totals() -> None:
    async def run() -> None:
        async_db, session = build_usage_stats_test_session()
        seed = seed_canonical_spend_migration_dataset(session)

        try:
            metrics = await get_model_metrics_batch(
                cast(AsyncSession, async_db),
                profile_id=seed.profile_id,
                model_ids=[seed.primary_model.model_id, seed.secondary_model.model_id],
            )

            assert metrics[seed.primary_model.model_id]["spend_30d_micros"] == (
                seed.primary_model.total_cost_micros
            )
            assert metrics[seed.secondary_model.model_id]["spend_30d_micros"] == (
                seed.secondary_model.total_cost_micros
            )
        finally:
            session.close()

    asyncio.run(run())
