from __future__ import annotations

import math
from datetime import timedelta

from sqlalchemy import and_, case, func, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.time import utc_now
from app.models.models import RequestLog, UsageRequestEvent
from app.services.stats.time_presets import resolve_time_preset


def _session_dialect_name(db: AsyncSession) -> str | None:
    get_bind = getattr(db, "get_bind", None)
    if callable(get_bind):
        bind = get_bind()
        dialect_name = getattr(getattr(bind, "dialect", None), "name", None)
        if isinstance(dialect_name, str):
            return dialect_name

    bind = getattr(db, "bind", None)
    dialect_name = getattr(getattr(bind, "dialect", None), "name", None)
    if isinstance(dialect_name, str):
        return dialect_name

    sync_session = getattr(db, "_session", None)
    dialect_name = getattr(
        getattr(getattr(sync_session, "bind", None), "dialect", None),
        "name",
        None,
    )
    if isinstance(dialect_name, str):
        return dialect_name
    return None


def _percentile_cont_int(values: list[int], percentile: float) -> int:
    if not values:
        return 0

    ordered_values = sorted(values)
    rank = (len(ordered_values) - 1) * percentile
    lower_index = math.floor(rank)
    upper_index = math.ceil(rank)
    lower_value = ordered_values[lower_index]
    upper_value = ordered_values[upper_index]
    interpolated = lower_value + (upper_value - lower_value) * (rank - lower_index)
    return int(round(interpolated))


async def get_model_metrics_batch(
    db: AsyncSession,
    *,
    profile_id: int,
    model_ids: list[str],
    summary_window_hours: int = 24,
    spending_preset: str = "last_30_days",
) -> dict[str, dict[str, float | int]]:
    if not model_ids:
        return {}

    unique_model_ids = list(dict.fromkeys(model_ids))
    summary_from_time = utc_now() - timedelta(hours=summary_window_hours)
    spending_from_time, spending_to_time = resolve_time_preset(
        spending_preset, None, None
    )

    summary_filters = [
        RequestLog.profile_id == profile_id,
        RequestLog.model_id.in_(unique_model_ids),
        RequestLog.created_at >= summary_from_time,
    ]
    success_case = case(
        (RequestLog.status_code.between(200, 299), 1),
        else_=0,
    )
    dialect_name = _session_dialect_name(db)
    query_columns = [
        RequestLog.model_id.label("model_id"),
        func.count().label("total_requests"),
        func.coalesce(func.sum(success_case), 0).label("success_count"),
    ]
    if dialect_name == "postgresql":
        query_columns.append(
            func.percentile_cont(0.95)
            .within_group(RequestLog.response_time_ms.asc())
            .label("p95_response_time_ms")
        )

    summary_rows = (
        await db.execute(
            select(*query_columns)
            .where(and_(*summary_filters))
            .group_by(RequestLog.model_id)
        )
    ).all()
    latency_values_by_model: dict[str, list[int]] = {}
    if dialect_name != "postgresql":
        latency_rows = (
            await db.execute(
                select(RequestLog.model_id, RequestLog.response_time_ms)
                .where(
                    and_(
                        *summary_filters,
                        RequestLog.response_time_ms.is_not(None),
                    )
                )
                .order_by(RequestLog.model_id, RequestLog.response_time_ms)
            )
        ).all()
        for row in latency_rows:
            latency_values_by_model.setdefault(row.model_id, []).append(
                int(row.response_time_ms)
            )

    spending_filters = [
        UsageRequestEvent.profile_id == profile_id,
        UsageRequestEvent.model_id.in_(unique_model_ids),
        UsageRequestEvent.success_flag.is_(True),
    ]
    if spending_from_time is not None:
        spending_filters.append(UsageRequestEvent.created_at >= spending_from_time)
    if spending_to_time is not None:
        spending_filters.append(UsageRequestEvent.created_at <= spending_to_time)

    spend_case = case(
        (
            UsageRequestEvent.billable_flag.is_(True),
            func.coalesce(UsageRequestEvent.total_cost_user_currency_micros, 0),
        ),
        else_=0,
    )
    spending_rows = (
        await db.execute(
            select(
                UsageRequestEvent.model_id.label("model_id"),
                func.coalesce(func.sum(spend_case), 0).label("total_cost_micros"),
            )
            .where(and_(*spending_filters))
            .group_by(UsageRequestEvent.model_id)
        )
    ).all()

    results: dict[str, dict[str, float | int]] = {
        model_id: {
            "success_rate": 0.0,
            "request_count_24h": 0,
            "p95_latency_ms": 0,
            "spend_30d_micros": 0,
        }
        for model_id in unique_model_ids
    }

    for row in summary_rows:
        total_requests = int(row.total_requests or 0)
        success_count = int(row.success_count or 0)
        success_rate = (
            round((success_count / total_requests * 100), 2)
            if total_requests > 0
            else 0.0
        )
        p95_latency_ms = (
            int(round(float(row.p95_response_time_ms or 0)))
            if dialect_name == "postgresql"
            else _percentile_cont_int(
                latency_values_by_model.get(row.model_id, []), 0.95
            )
        )

        results[row.model_id] = {
            **results[row.model_id],
            "success_rate": success_rate,
            "request_count_24h": total_requests,
            "p95_latency_ms": p95_latency_ms,
        }

    for row in spending_rows:
        results[row.model_id] = {
            **results[row.model_id],
            "spend_30d_micros": int(row.total_cost_micros or 0),
        }

    return results
