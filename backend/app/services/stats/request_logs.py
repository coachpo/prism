from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
import re
from typing import Any, Literal

from sqlalchemy import and_, func, literal, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.models import Endpoint, RequestLog, UserAgentClientRule

_CompiledUserAgentClientRule = tuple[str, re.Pattern[str]]


@dataclass
class RequestLogListResult:
    items: list[Any]
    total: int
    endpoints: list[Any] = field(default_factory=list)


def _resolve_endpoint_label(
    *,
    endpoint_name: str | None,
    endpoint_base_url: str | None,
    request_log_endpoint_base_url: str | None = None,
    endpoint_id: int | None,
) -> str:
    return (
        endpoint_name
        or endpoint_base_url
        or request_log_endpoint_base_url
        or (f"Endpoint {endpoint_id}" if endpoint_id is not None else None)
        or "Unknown Endpoint"
    )


def _serialize_endpoint_option(endpoint: Endpoint) -> dict[str, object]:
    return {
        "endpoint_id": endpoint.id,
        "endpoint_label": _resolve_endpoint_label(
            endpoint_name=endpoint.name,
            endpoint_base_url=endpoint.base_url,
            endpoint_id=endpoint.id,
        ),
    }


async def _load_current_endpoints(
    db: AsyncSession,
    *,
    profile_id: int,
) -> list[Endpoint]:
    endpoints = (
        (
            await db.execute(
                select(Endpoint)
                .where(Endpoint.profile_id == profile_id)
                .order_by(Endpoint.position.asc(), Endpoint.id.asc())
            )
        )
        .scalars()
        .all()
    )
    return list(endpoints)


def _build_endpoint_options(
    current_endpoints: list[Endpoint],
    *,
    selected_endpoint_id: int | None,
) -> list[dict[str, object]]:
    endpoint_options = [
        _serialize_endpoint_option(endpoint) for endpoint in current_endpoints
    ]
    if selected_endpoint_id is None:
        return endpoint_options

    current_endpoint_ids = {endpoint.id for endpoint in current_endpoints}
    if selected_endpoint_id in current_endpoint_ids:
        return endpoint_options

    return [
        {
            "endpoint_id": selected_endpoint_id,
            "endpoint_label": f"Endpoint {selected_endpoint_id}",
        },
        *endpoint_options,
    ]


async def _load_compiled_user_agent_client_rules(
    db: AsyncSession,
    *,
    profile_id: int,
) -> list[_CompiledUserAgentClientRule]:
    rules = (
        (
            await db.execute(
                select(UserAgentClientRule)
                .where(
                    UserAgentClientRule.enabled == True,  # noqa: E712
                    (UserAgentClientRule.profile_id == profile_id)
                    | (UserAgentClientRule.is_system == True),  # noqa: E712
                )
                .order_by(
                    UserAgentClientRule.is_system.asc(),
                    UserAgentClientRule.id.asc(),
                )
            )
        )
        .scalars()
        .all()
    )
    compiled_rules: list[_CompiledUserAgentClientRule] = []
    for rule in rules:
        try:
            compiled_pattern = re.compile(rule.pattern, re.IGNORECASE)
        except re.error:
            continue
        compiled_rules.append((rule.name, compiled_pattern))
    return compiled_rules


def _classify_user_agent_display(
    user_agent: str | None,
    rules: list[_CompiledUserAgentClientRule],
) -> str | None:
    if user_agent is None:
        return None
    for rule_name, pattern in rules:
        if pattern.search(user_agent) is not None:
            return rule_name
    return user_agent


def _annotate_request_log_user_agent_fields(
    entry: RequestLog,
    rules: list[_CompiledUserAgentClientRule],
) -> RequestLog:
    setattr(
        entry,
        "caller_client_display",
        _classify_user_agent_display(entry.caller_user_agent, rules),
    )
    setattr(
        entry,
        "upstream_client_display",
        _classify_user_agent_display(entry.upstream_user_agent, rules),
    )
    setattr(
        entry,
        "user_agent_overridden",
        entry.caller_user_agent != entry.upstream_user_agent,
    )
    return entry


def _annotate_request_log_list_endpoint_label(
    entry: RequestLog,
    *,
    current_endpoint: Endpoint | None,
) -> RequestLog:
    setattr(
        entry,
        "endpoint_label",
        _resolve_endpoint_label(
            endpoint_name=current_endpoint.name
            if current_endpoint is not None
            else None,
            endpoint_base_url=(
                current_endpoint.base_url if current_endpoint is not None else None
            ),
            request_log_endpoint_base_url=entry.endpoint_base_url,
            endpoint_id=entry.endpoint_id,
        ),
    )
    return entry


def _build_request_log_browse_where(
    *,
    profile_id: int,
    ingress_request_id: str | None = None,
    model_id: str | None = None,
    status_family: Literal["4xx", "5xx"] | None = None,
    from_time: datetime | None = None,
    endpoint_id: int | None = None,
):
    filters = [RequestLog.profile_id == profile_id]
    if ingress_request_id:
        filters.append(RequestLog.ingress_request_id == ingress_request_id)
    if model_id:
        filters.append(RequestLog.model_id == model_id)
    if status_family == "4xx":
        filters.append(RequestLog.status_code.between(400, 499))
    elif status_family == "5xx":
        filters.append(RequestLog.status_code.between(500, 599))
    if from_time:
        filters.append(RequestLog.created_at >= from_time)
    if endpoint_id is not None:
        filters.append(RequestLog.endpoint_id == endpoint_id)

    return and_(*filters) if filters else literal(True)


def _build_request_log_detail_where(*, profile_id: int, request_id: int):
    return and_(
        RequestLog.profile_id == profile_id,
        RequestLog.id == request_id,
    )


async def _get_request_log_total(db: AsyncSession, where) -> int:
    count_q = select(func.count()).select_from(RequestLog).where(where)
    return (await db.execute(count_q)).scalar() or 0


def _request_log_order_by():
    return RequestLog.created_at.desc(), RequestLog.id.desc()


async def get_request_logs(
    db: AsyncSession,
    *,
    profile_id: int,
    ingress_request_id: str | None = None,
    model_id: str | None = None,
    status_family: Literal["4xx", "5xx"] | None = None,
    from_time: datetime | None = None,
    endpoint_id: int | None = None,
    limit: int = 50,
    offset: int = 0,
) -> RequestLogListResult:
    where = _build_request_log_browse_where(
        profile_id=profile_id,
        ingress_request_id=ingress_request_id,
        model_id=model_id,
        status_family=status_family,
        from_time=from_time,
        endpoint_id=endpoint_id,
    )
    total = await _get_request_log_total(db, where)
    current_endpoints = await _load_current_endpoints(db, profile_id=profile_id)
    current_endpoints_by_id = {endpoint.id: endpoint for endpoint in current_endpoints}

    q = (
        select(RequestLog)
        .where(where)
        .order_by(*_request_log_order_by())
        .limit(limit)
        .offset(offset)
    )
    rows = (await db.execute(q)).scalars().all()
    rules = await _load_compiled_user_agent_client_rules(db, profile_id=profile_id)
    return RequestLogListResult(
        items=[
            _annotate_request_log_list_endpoint_label(
                _annotate_request_log_user_agent_fields(row, rules),
                current_endpoint=(
                    current_endpoints_by_id.get(row.endpoint_id)
                    if row.endpoint_id is not None
                    else None
                ),
            )
            for row in list(rows)
        ],
        total=total,
        endpoints=_build_endpoint_options(
            current_endpoints,
            selected_endpoint_id=endpoint_id,
        ),
    )


async def get_request_log_detail(
    db: AsyncSession,
    *,
    profile_id: int,
    request_id: int,
) -> RequestLog | None:
    where = _build_request_log_detail_where(
        profile_id=profile_id,
        request_id=request_id,
    )
    q = select(RequestLog).where(where).limit(1)
    entry = (await db.execute(q)).scalar_one_or_none()
    if entry is None:
        return None
    rules = await _load_compiled_user_agent_client_rules(db, profile_id=profile_id)
    return _annotate_request_log_user_agent_fields(entry, rules)
