from __future__ import annotations

from collections import defaultdict
from datetime import datetime, timezone
from typing import Literal, Sequence

from fastapi import HTTPException
from sqlalchemy import Float, and_, case, cast, func, select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.sql.elements import ColumnElement

from app.core.time import utc_now
from app.models.models import Endpoint, ModelConfig, UsageRequestEvent
from app.services.stats.time_presets import resolve_time_preset
from app.services.stats.usage_snapshot import _percentile_cont_int

EndpointModelStatisticsPreset = Literal["1h", "6h", "24h", "7d", "30d", "all"]


def _normalize_datetime(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value.astimezone(timezone.utc)


def _success_rate(*, success_count: int, total_count: int) -> float:
    if total_count <= 0:
        return 0.0
    return round((success_count / total_count) * 100.0, 2)


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


def _rounded_nullable_int(value: float | int | None) -> int | None:
    if value is None:
        return None
    return int(round(float(value)))


async def _load_ttft_values_by_model(
    db: AsyncSession,
    *,
    filters: Sequence[ColumnElement[bool]],
) -> dict[str, list[int]]:
    rows = (
        await db.execute(
            select(UsageRequestEvent.model_id, UsageRequestEvent.ttft_ms)
            .where(and_(*filters, UsageRequestEvent.ttft_ms.is_not(None)))
            .order_by(UsageRequestEvent.model_id, UsageRequestEvent.created_at)
        )
    ).all()

    values_by_model: dict[str, list[int]] = defaultdict(list)
    for row in rows:
        values_by_model[row.model_id].append(int(row.ttft_ms))
    return values_by_model


async def get_endpoint_model_statistics(
    db: AsyncSession,
    *,
    profile_id: int,
    endpoint_id: int,
    preset: EndpointModelStatisticsPreset = "1h",
    from_time: datetime | None = None,
    to_time: datetime | None = None,
) -> list[dict[str, object]]:
    generated_at = _normalize_datetime(to_time or utc_now())
    normalized_from_time = (
        _normalize_datetime(from_time) if from_time is not None else None
    )
    normalized_to_time = (
        _normalize_datetime(to_time) if to_time is not None else generated_at
    )
    effective_preset = None if from_time is not None or to_time is not None else preset
    start_at, end_at = resolve_time_preset(
        effective_preset,
        normalized_from_time,
        normalized_to_time,
    )
    normalized_start_at = (
        _normalize_datetime(start_at) if start_at is not None else None
    )
    normalized_end_at = _normalize_datetime(end_at or generated_at)

    live_endpoint_exists = await db.scalar(
        select(Endpoint.id).where(
            and_(Endpoint.profile_id == profile_id, Endpoint.id == endpoint_id)
        )
    )
    historical_usage_exists = await db.scalar(
        select(UsageRequestEvent.endpoint_id)
        .where(
            and_(
                UsageRequestEvent.profile_id == profile_id,
                UsageRequestEvent.endpoint_id == endpoint_id,
            )
        )
        .limit(1)
    )
    if live_endpoint_exists is None and historical_usage_exists is None:
        raise HTTPException(status_code=404, detail="Endpoint not found")

    filters = [
        UsageRequestEvent.profile_id == profile_id,
        UsageRequestEvent.endpoint_id == endpoint_id,
        UsageRequestEvent.created_at <= normalized_end_at,
    ]
    if normalized_start_at is not None:
        filters.append(UsageRequestEvent.created_at >= normalized_start_at)

    success_count = case((UsageRequestEvent.success_flag.is_(True), 1), else_=0)
    priced_request_count = case(
        (
            and_(
                UsageRequestEvent.success_flag.is_(True),
                UsageRequestEvent.priced_flag.is_(True),
            ),
            1,
        ),
        else_=0,
    )
    unpriced_request_count = case(
        (
            and_(
                UsageRequestEvent.success_flag.is_(True),
                UsageRequestEvent.priced_flag.is_not(True),
            ),
            1,
        ),
        else_=0,
    )
    spend_case = case(
        (
            UsageRequestEvent.billable_flag.is_(True),
            func.coalesce(UsageRequestEvent.total_cost_user_currency_micros, 0),
        ),
        else_=0,
    )
    avg_output_rate_tps_eligible = and_(
        UsageRequestEvent.output_tokens.is_not(None),
        UsageRequestEvent.ttft_ms.is_not(None),
        UsageRequestEvent.completion_duration_ms.is_not(None),
        UsageRequestEvent.completion_duration_ms > UsageRequestEvent.ttft_ms,
    )
    output_rate_tps_expr = (
        cast(UsageRequestEvent.output_tokens, Float) * 1000.0
    ) / cast(
        UsageRequestEvent.completion_duration_ms - UsageRequestEvent.ttft_ms,
        Float,
    )
    eligible_output_rate_tps_expr = case(
        (avg_output_rate_tps_eligible, output_rate_tps_expr),
        else_=None,
    )
    avg_output_rate_tps = func.avg(eligible_output_rate_tps_expr).label(
        "avg_output_rate_tps"
    )
    ttft_percentile_p50 = func.percentile_cont(0.5).within_group(
        UsageRequestEvent.ttft_ms.asc()
    )
    ttft_percentile_p95 = func.percentile_cont(0.95).within_group(
        UsageRequestEvent.ttft_ms.asc()
    )
    dialect_name = _session_dialect_name(db)
    query_columns = [
        UsageRequestEvent.model_id,
        ModelConfig.display_name.label("model_display_name"),
        func.count().label("request_count"),
        func.coalesce(func.sum(success_count), 0).label("success_count"),
        func.coalesce(func.sum(priced_request_count), 0).label("priced_request_count"),
        func.coalesce(func.sum(unpriced_request_count), 0).label(
            "unpriced_request_count"
        ),
        func.coalesce(
            func.sum(func.coalesce(UsageRequestEvent.total_tokens, 0)), 0
        ).label("total_tokens"),
        func.coalesce(
            func.sum(spend_case),
            0,
        ).label("total_cost_micros"),
        avg_output_rate_tps,
    ]
    if dialect_name == "postgresql":
        query_columns.extend(
            [
                ttft_percentile_p50.label("p50_ttft_ms"),
                ttft_percentile_p95.label("p95_ttft_ms"),
            ]
        )

    rows = (
        await db.execute(
            select(*query_columns)
            .select_from(UsageRequestEvent)
            .outerjoin(
                ModelConfig,
                and_(
                    ModelConfig.profile_id == UsageRequestEvent.profile_id,
                    ModelConfig.model_id == UsageRequestEvent.model_id,
                ),
            )
            .where(and_(*filters))
            .group_by(UsageRequestEvent.model_id, ModelConfig.display_name)
        )
    ).all()
    ttft_values_by_model = (
        {}
        if dialect_name == "postgresql"
        else await _load_ttft_values_by_model(db, filters=filters)
    )

    items = [
        {
            "model_id": row.model_id,
            "model_label": row.model_display_name or row.model_id,
            "request_count": int(row.request_count or 0),
            "priced_request_count": int(row.priced_request_count or 0),
            "unpriced_request_count": int(row.unpriced_request_count or 0),
            "success_rate": _success_rate(
                success_count=int(row.success_count or 0),
                total_count=int(row.request_count or 0),
            ),
            "total_tokens": int(row.total_tokens or 0),
            "total_cost_micros": int(row.total_cost_micros or 0),
            "p50_ttft_ms": (
                _rounded_nullable_int(row.p50_ttft_ms)
                if dialect_name == "postgresql"
                else _percentile_cont_int(
                    ttft_values_by_model.get(row.model_id, []),
                    0.5,
                )
            ),
            "p95_ttft_ms": (
                _rounded_nullable_int(row.p95_ttft_ms)
                if dialect_name == "postgresql"
                else _percentile_cont_int(
                    ttft_values_by_model.get(row.model_id, []),
                    0.95,
                )
            ),
            "avg_output_rate_tps": (
                round(float(row.avg_output_rate_tps), 2)
                if row.avg_output_rate_tps is not None
                else None
            ),
        }
        for row in rows
    ]
    items.sort(
        key=lambda row: (
            -int(row["request_count"]),
            str(row["model_label"]),
            str(row["model_id"]),
        )
    )
    return items


__all__ = ["EndpointModelStatisticsPreset", "get_endpoint_model_statistics"]
