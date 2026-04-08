import inspect
import json
from datetime import datetime, timezone
from types import SimpleNamespace
from typing import Any, cast
from uuid import uuid4
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from app.core.database import AsyncSessionLocal
from app.models.models import Profile, RequestLog


def _request_log(
    *,
    profile_id: int,
    response_time_ms: int,
    created_at: datetime,
    model_id: str = "gpt-test",
    status_code: int = 200,
    api_family: str = "openai",
    endpoint_id: int | None = None,
    resolved_target_model_id: str | None = None,
    ingress_request_id: str | None = None,
    attempt_number: int | None = None,
    provider_correlation_id: str | None = None,
    caller_user_agent: str | None = None,
    upstream_user_agent: str | None = None,
    success_flag: bool | None = None,
    billable_flag: bool | None = None,
    priced_flag: bool | None = None,
    total_cost_user_currency_micros: int | None = None,
) -> RequestLog:
    return RequestLog(
        profile_id=profile_id,
        model_id=model_id,
        api_family=api_family,
        resolved_target_model_id=resolved_target_model_id,
        endpoint_id=endpoint_id,
        connection_id=None,
        ingress_request_id=ingress_request_id,
        attempt_number=attempt_number,
        provider_correlation_id=provider_correlation_id,
        caller_user_agent=caller_user_agent,
        upstream_user_agent=upstream_user_agent,
        endpoint_base_url="https://api.openai.com",
        status_code=status_code,
        response_time_ms=response_time_ms,
        is_stream=False,
        request_path="/v1/chat/completions",
        success_flag=success_flag,
        billable_flag=billable_flag,
        priced_flag=priced_flag,
        total_cost_user_currency_micros=total_cost_user_currency_micros,
        created_at=created_at,
    )


@pytest.mark.asyncio
async def test_get_stats_summary_reads_p95_from_sql_aggregate_query() -> None:
    from app.services.stats.summary import get_stats_summary

    aggregate_result = MagicMock()
    aggregate_result.one.return_value = SimpleNamespace(
        total_requests=20,
        success_count=19,
        avg_response_time_ms=12.5,
        p95_response_time_ms=20,
        total_input_tokens=0,
        total_output_tokens=0,
        total_tokens=0,
    )
    empty_p95_result = MagicMock()
    empty_p95_result.all.return_value = []

    db = AsyncMock()
    db.execute = AsyncMock(side_effect=[aggregate_result, empty_p95_result])

    summary = await get_stats_summary(db, profile_id=7)

    assert summary["p95_response_time_ms"] == 20
    assert db.execute.await_count == 1


@pytest.mark.asyncio
async def test_get_stats_summary_uses_postgresql_percentile_cont_semantics() -> None:
    from app.services.stats.summary import get_stats_summary

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        primary_profile = Profile(
            name=f"summary-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        other_profile = Profile(
            name=f"summary-other-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add_all([primary_profile, other_profile])
        await db.flush()

        db.add_all(
            [
                _request_log(
                    profile_id=primary_profile.id,
                    response_time_ms=response_time_ms,
                    created_at=created_at,
                )
                for response_time_ms in range(1, 21)
            ]
            + [
                _request_log(
                    profile_id=other_profile.id,
                    response_time_ms=9_999,
                    created_at=created_at,
                )
            ]
        )
        await db.commit()

        summary = await get_stats_summary(db, profile_id=primary_profile.id)

    assert summary["total_requests"] == 20
    assert summary["p95_response_time_ms"] == 19


@pytest.mark.asyncio
async def test_get_request_logs_uses_stable_id_tiebreaker_for_timestamp_sort() -> None:
    from app.services.stats.request_logs import get_request_logs

    count_result = MagicMock()
    count_result.scalar.return_value = 0
    rows_result = MagicMock()
    rows_result.scalars.return_value.all.return_value = []
    statements = []

    async def capture_execute(statement):
        statements.append(statement)
        return count_result if len(statements) == 1 else rows_result

    db = AsyncMock()
    db.execute = AsyncMock(side_effect=capture_execute)

    await get_request_logs(db, profile_id=7, limit=50, offset=0)

    order_clauses = [str(clause) for clause in statements[1]._order_by_clauses]
    assert order_clauses == [
        str(RequestLog.created_at.desc()),
        str(RequestLog.id.desc()),
    ]


def test_operations_request_logs_contract_is_absent_from_stats_services() -> None:
    import app.services.stats.request_logs as request_logs
    import app.services.stats_service as stats_service

    removed_name = "get_" + "operations_request_logs"

    assert not hasattr(request_logs, removed_name)
    assert not hasattr(stats_service, removed_name)


def test_get_request_logs_signature_keeps_only_simplified_browse_filters() -> None:
    from app.services.stats.request_logs import get_request_logs

    parameter_names = set(inspect.signature(get_request_logs).parameters)

    assert {
        "db",
        "profile_id",
        "ingress_request_id",
        "model_id",
        "status_family",
        "from_time",
        "endpoint_id",
        "limit",
        "offset",
    }.issubset(parameter_names)
    assert "request_id" not in parameter_names
    assert "api_family" not in parameter_names
    assert "status_code" not in parameter_names
    assert "success" not in parameter_names
    assert "to_time" not in parameter_names
    assert "connection_id" not in parameter_names


@pytest.mark.asyncio
async def test_get_request_logs_filters_by_status_family() -> None:
    from app.services.stats.request_logs import get_request_logs

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        primary_profile = Profile(
            name=f"request-log-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        other_profile = Profile(
            name=f"request-log-other-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add_all([primary_profile, other_profile])
        await db.flush()

        db.add_all(
            [
                _request_log(
                    profile_id=primary_profile.id,
                    response_time_ms=100,
                    created_at=created_at,
                    status_code=404,
                ),
                _request_log(
                    profile_id=primary_profile.id,
                    response_time_ms=110,
                    created_at=created_at,
                    status_code=429,
                ),
                _request_log(
                    profile_id=primary_profile.id,
                    response_time_ms=120,
                    created_at=created_at,
                    status_code=500,
                ),
                _request_log(
                    profile_id=primary_profile.id,
                    response_time_ms=130,
                    created_at=created_at,
                    status_code=200,
                ),
                _request_log(
                    profile_id=other_profile.id,
                    response_time_ms=140,
                    created_at=created_at,
                    status_code=418,
                ),
            ]
        )
        await db.commit()

        items, total = await get_request_logs(
            db,
            profile_id=primary_profile.id,
            status_family="4xx",
            limit=50,
            offset=0,
        )

    assert total == 2
    assert [item.status_code for item in items] == [429, 404]


@pytest.mark.asyncio
async def test_get_request_logs_filters_by_ingress_request_id_and_preserves_attempt_order() -> (
    None
):
    from app.services.stats.request_logs import get_request_logs

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"ingress-log-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        other_profile = Profile(
            name=f"ingress-log-other-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add_all([profile, other_profile])
        await db.flush()

        db.add_all(
            [
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=100,
                    created_at=created_at,
                    ingress_request_id="ingress-123",
                    attempt_number=1,
                    provider_correlation_id="resp-1",
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=110,
                    created_at=created_at,
                    ingress_request_id="ingress-123",
                    attempt_number=2,
                    provider_correlation_id="resp-2",
                    status_code=503,
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=120,
                    created_at=created_at,
                    ingress_request_id="other-ingress",
                    attempt_number=1,
                ),
                _request_log(
                    profile_id=other_profile.id,
                    response_time_ms=130,
                    created_at=created_at,
                    ingress_request_id="ingress-123",
                    attempt_number=9,
                ),
            ]
        )
        await db.commit()

        items, total = await get_request_logs(
            db,
            profile_id=profile.id,
            ingress_request_id="ingress-123",
            limit=50,
            offset=0,
        )

    assert total == 2
    assert [item.attempt_number for item in items] == [2, 1]
    assert [item.provider_correlation_id for item in items] == ["resp-2", "resp-1"]


@pytest.mark.asyncio
async def test_get_request_logs_preserves_resolved_target_model_id() -> None:
    from app.services.stats.request_logs import get_request_logs

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"resolved-target-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.flush()

        db.add(
            _request_log(
                profile_id=profile.id,
                response_time_ms=100,
                created_at=created_at,
                resolved_target_model_id="target-model-a",
            )
        )
        await db.commit()

        items, total = await get_request_logs(
            db,
            profile_id=profile.id,
            limit=50,
            offset=0,
        )

    assert total == 1
    assert items[0].resolved_target_model_id == "target-model-a"


@pytest.mark.asyncio
async def test_get_request_log_detail_returns_none_outside_profile_scope() -> None:
    from app.services.stats.request_logs import get_request_log_detail

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        owner_profile = Profile(
            name=f"detail-owner-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        other_profile = Profile(
            name=f"detail-other-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add_all([owner_profile, other_profile])
        await db.flush()

        entry = _request_log(
            profile_id=owner_profile.id,
            response_time_ms=100,
            created_at=created_at,
            ingress_request_id="ingress-detail-1",
            attempt_number=2,
            provider_correlation_id="provider-detail-1",
        )
        db.add(entry)
        await db.commit()

        owner_detail = await get_request_log_detail(
            db,
            profile_id=owner_profile.id,
            request_id=entry.id,
        )
        other_detail = await get_request_log_detail(
            db,
            profile_id=other_profile.id,
            request_id=entry.id,
        )

    assert owner_detail is not None
    assert owner_detail.id == entry.id
    assert owner_detail.ingress_request_id == "ingress-detail-1"
    assert owner_detail.attempt_number == 2
    assert owner_detail.provider_correlation_id == "provider-detail-1"
    assert other_detail is None


@pytest.mark.asyncio
async def test_prepare_proxy_request_captures_caller_user_agent_case_insensitively() -> (
    None
):
    from fastapi import FastAPI
    from starlette.requests import Request

    from app.routers.proxy_domains.request_setup import prepare_proxy_request

    app = FastAPI()
    app.state.http_client = object()
    request = Request(
        {
            "type": "http",
            "http_version": "1.1",
            "method": "POST",
            "path": "/v1/chat/completions",
            "raw_path": b"/v1/chat/completions",
            "query_string": b"",
            "headers": [
                (b"host", b"testserver"),
                (b"content-type", b"application/json"),
                (b"User-Agent", b"Codex CLI/1.2"),
            ],
            "client": ("testclient", 50000),
            "server": ("testserver", 80),
            "scheme": "http",
            "app": app,
        }
    )
    raw_body = json.dumps(
        {
            "model": "vendorless-model",
            "messages": [{"role": "user", "content": "hi"}],
        }
    ).encode("utf-8")

    requested_result = MagicMock()
    requested_scalars = MagicMock()
    requested_scalars.one_or_none.return_value = SimpleNamespace(
        vendor=None,
        api_family="openai",
        model_id="vendorless-model",
        is_enabled=True,
    )
    requested_result.scalars.return_value = requested_scalars

    blocklist_result = MagicMock()
    blocklist_scalars = MagicMock()
    blocklist_scalars.all.return_value = []
    blocklist_result.scalars.return_value = blocklist_scalars

    db = AsyncMock()
    db.execute = AsyncMock(side_effect=[requested_result, blocklist_result])

    strategy = SimpleNamespace(
        strategy_type="legacy",
        legacy_strategy_type="single",
        auto_recovery={"mode": "disabled"},
    )
    resolved_model_config = SimpleNamespace(
        vendor=None,
        api_family="openai",
        model_id="vendorless-model",
        loadbalance_strategy=strategy,
    )
    attempt_plan = SimpleNamespace(
        connections=[SimpleNamespace(id=501, endpoint_id=501)],
        probe_eligible_connection_ids=[],
        policy=SimpleNamespace(
            deadline_budget_ms=1000,
            failover_recovery_enabled=False,
        ),
    )

    with (
        patch(
            "app.routers.proxy_domains.request_setup.get_model_config_with_connections",
            AsyncMock(return_value=resolved_model_config),
        ),
        patch(
            "app.routers.proxy_domains.request_setup.build_attempt_plan",
            AsyncMock(return_value=attempt_plan),
        ),
        patch(
            "app.routers.proxy_domains.request_setup.load_costing_settings",
            AsyncMock(return_value=SimpleNamespace()),
        ),
    ):
        setup = await prepare_proxy_request(
            request=request,
            db=db,
            raw_body=raw_body,
            request_path="/v1/chat/completions",
            profile_id=7,
        )

    assert setup.caller_user_agent == "Codex CLI/1.2"


@pytest.mark.asyncio
async def test_record_request_log_threads_caller_and_upstream_user_agents() -> None:
    from app.routers.proxy_domains.attempt_outcome_reporting import record_request_log

    log_request_fn = AsyncMock(return_value=123)
    deps = SimpleNamespace(log_request_fn=log_request_fn)
    state = SimpleNamespace(
        profile_id=7,
        request_path="/v1/chat/completions",
        setup=SimpleNamespace(
            model_id="gpt-5.4",
            api_family="openai",
            vendor_id=1,
            vendor_key="openai",
            vendor_name="OpenAI",
            resolved_target_model_id="gpt-4.1-mini",
            proxy_api_key_id=11,
            proxy_api_key_name="primary-key",
            ingress_request_id="ingress-ua-1",
            caller_user_agent="Codex CLI/1.2",
            build_cost_fields=lambda *_args, **_kwargs: {},
        ),
    )
    target = SimpleNamespace(
        attempt_number=2,
        connection=SimpleNamespace(
            id=8,
            endpoint_id=4,
            endpoint_rel=SimpleNamespace(base_url="https://api.openai.com"),
        ),
        headers={"User-Agent": "Claude Code/2.0"},
        description="Primary endpoint",
    )

    await record_request_log(
        deps=cast(Any, deps),
        state=cast(Any, state),
        target=cast(Any, target),
        status_code=200,
        response_headers=None,
        response_body=None,
        elapsed_ms=145,
        is_stream=False,
    )

    assert log_request_fn.await_args is not None
    persisted_kwargs = log_request_fn.await_args.kwargs
    assert persisted_kwargs["caller_user_agent"] == "Codex CLI/1.2"
    assert persisted_kwargs["upstream_user_agent"] == "Claude Code/2.0"


@pytest.mark.asyncio
async def test_log_request_persists_matching_caller_and_upstream_user_agent() -> None:
    from app.services.stats_service import log_request

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"ua-persist-same-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.commit()
        await db.refresh(profile)
        profile_id = profile.id

    request_log_id = await log_request(
        model_id="gpt-5.4",
        profile_id=profile_id,
        api_family="openai",
        endpoint_id=1,
        connection_id=1,
        endpoint_base_url="https://api.openai.com",
        status_code=200,
        response_time_ms=100,
        is_stream=False,
        request_path="/v1/chat/completions",
        caller_user_agent="Gemini CLI/1.0",
        upstream_user_agent="Gemini CLI/1.0",
    )

    async with AsyncSessionLocal() as db:
        entry = await db.get(RequestLog, request_log_id)

    assert entry is not None
    assert entry.caller_user_agent == "Gemini CLI/1.0"
    assert entry.upstream_user_agent == "Gemini CLI/1.0"


@pytest.mark.asyncio
async def test_log_request_persists_distinct_caller_and_upstream_user_agent() -> None:
    from app.services.stats_service import log_request

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"ua-persist-different-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.commit()
        await db.refresh(profile)
        profile_id = profile.id

    request_log_id = await log_request(
        model_id="gpt-5.4",
        profile_id=profile_id,
        api_family="openai",
        endpoint_id=1,
        connection_id=1,
        endpoint_base_url="https://api.openai.com",
        status_code=200,
        response_time_ms=100,
        is_stream=False,
        request_path="/v1/chat/completions",
        caller_user_agent="Codex CLI/1.2",
        upstream_user_agent="Claude Code/2.0",
    )

    async with AsyncSessionLocal() as db:
        entry = await db.get(RequestLog, request_log_id)

    assert entry is not None
    assert entry.caller_user_agent == "Codex CLI/1.2"
    assert entry.upstream_user_agent == "Claude Code/2.0"


@pytest.mark.asyncio
async def test_log_request_persists_caller_user_agent_without_upstream_for_pre_attempt_rows() -> (
    None
):
    from app.services.stats_service import log_request

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"ua-persist-pre-attempt-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.commit()
        await db.refresh(profile)
        profile_id = profile.id

    request_log_id = await log_request(
        model_id="gpt-5.4",
        profile_id=profile_id,
        api_family="openai",
        endpoint_id=None,
        connection_id=None,
        endpoint_base_url=None,
        status_code=503,
        response_time_ms=0,
        is_stream=False,
        request_path="/v1/chat/completions",
        caller_user_agent="Codex CLI/1.2",
        upstream_user_agent=None,
        error_detail="No active connections available",
    )

    async with AsyncSessionLocal() as db:
        entry = await db.get(RequestLog, request_log_id)

    assert entry is not None
    assert entry.caller_user_agent == "Codex CLI/1.2"
    assert entry.upstream_user_agent is None


@pytest.mark.asyncio
async def test_get_request_logs_classifies_user_agents_with_profile_precedence_and_raw_fallback() -> (
    None
):
    from app.models.models import UserAgentClientRule
    from app.services.stats.request_logs import get_request_logs

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"request-log-ua-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.flush()

        db.add_all(
            [
                UserAgentClientRule(
                    name="Codex Custom",
                    pattern="codex",
                    enabled=True,
                    is_system=False,
                    profile_id=profile.id,
                ),
                UserAgentClientRule(
                    name="Claude Code",
                    pattern="claude(?:\\s|-)?code",
                    enabled=True,
                    is_system=False,
                    profile_id=profile.id,
                ),
                UserAgentClientRule(
                    name="Gemini",
                    pattern="gemini",
                    enabled=True,
                    is_system=False,
                    profile_id=profile.id,
                ),
            ]
        )
        db.add_all(
            [
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=100,
                    created_at=created_at,
                    ingress_request_id="override",
                    caller_user_agent="Codex CLI/1.2",
                    upstream_user_agent="Claude Code/2.0",
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=110,
                    created_at=created_at,
                    ingress_request_id="same",
                    caller_user_agent="Gemini CLI/1.0",
                    upstream_user_agent="Gemini CLI/1.0",
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=120,
                    created_at=created_at,
                    ingress_request_id="fallback",
                    caller_user_agent="UnknownAgent/9.0",
                    upstream_user_agent="UnknownAgent/9.0",
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=130,
                    created_at=created_at,
                    ingress_request_id="nulls",
                    caller_user_agent=None,
                    upstream_user_agent=None,
                ),
            ]
        )
        await db.commit()

        items, total = await get_request_logs(
            db,
            profile_id=profile.id,
            limit=50,
            offset=0,
        )

    assert total == 4
    items_by_ingress = {item.ingress_request_id: item for item in items}
    assert (
        getattr(items_by_ingress["override"], "caller_client_display") == "Codex Custom"
    )
    assert (
        getattr(items_by_ingress["override"], "upstream_client_display")
        == "Claude Code"
    )
    assert getattr(items_by_ingress["override"], "user_agent_overridden") is True
    assert getattr(items_by_ingress["same"], "caller_client_display") == "Gemini"
    assert getattr(items_by_ingress["same"], "upstream_client_display") == "Gemini"
    assert getattr(items_by_ingress["same"], "user_agent_overridden") is False
    assert (
        getattr(items_by_ingress["fallback"], "caller_client_display")
        == "UnknownAgent/9.0"
    )
    assert (
        getattr(items_by_ingress["fallback"], "upstream_client_display")
        == "UnknownAgent/9.0"
    )
    assert getattr(items_by_ingress["fallback"], "user_agent_overridden") is False
    assert getattr(items_by_ingress["nulls"], "caller_client_display") is None
    assert getattr(items_by_ingress["nulls"], "upstream_client_display") is None
    assert getattr(items_by_ingress["nulls"], "user_agent_overridden") is False


def test_annotate_request_log_marks_one_sided_user_agent_differences_as_overrides() -> (
    None
):
    from app.services.stats.request_logs import _annotate_request_log_user_agent_fields

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    caller_only_entry = _request_log(
        profile_id=7,
        response_time_ms=100,
        created_at=created_at,
        caller_user_agent="Codex CLI/1.2",
        upstream_user_agent=None,
    )
    upstream_only_entry = _request_log(
        profile_id=7,
        response_time_ms=110,
        created_at=created_at,
        caller_user_agent=None,
        upstream_user_agent="Claude Code/2.0",
    )
    both_null_entry = _request_log(
        profile_id=7,
        response_time_ms=120,
        created_at=created_at,
        caller_user_agent=None,
        upstream_user_agent=None,
    )

    annotated_caller_only = _annotate_request_log_user_agent_fields(
        caller_only_entry, []
    )
    annotated_upstream_only = _annotate_request_log_user_agent_fields(
        upstream_only_entry,
        [],
    )
    annotated_both_null = _annotate_request_log_user_agent_fields(both_null_entry, [])

    assert getattr(annotated_caller_only, "user_agent_overridden") is True
    assert getattr(annotated_upstream_only, "user_agent_overridden") is True
    assert getattr(annotated_both_null, "user_agent_overridden") is False


@pytest.mark.asyncio
async def test_get_request_log_detail_reclassifies_historical_rows_using_current_rules() -> (
    None
):
    from app.models.models import UserAgentClientRule
    from app.services.stats.request_logs import get_request_log_detail

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"request-log-ua-detail-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.flush()

        custom_rule = UserAgentClientRule(
            name="Codex",
            pattern="codex",
            enabled=True,
            is_system=False,
            profile_id=profile.id,
        )
        upstream_rule = UserAgentClientRule(
            name="Claude Code",
            pattern="claude(?:\\s|-)?code",
            enabled=True,
            is_system=False,
            profile_id=profile.id,
        )
        entry = _request_log(
            profile_id=profile.id,
            response_time_ms=100,
            created_at=created_at,
            ingress_request_id="detail-ingress",
            caller_user_agent="Codex CLI/1.2",
            upstream_user_agent="Claude Code/2.0",
        )
        db.add_all([custom_rule, upstream_rule, entry])
        await db.commit()

        first_detail = await get_request_log_detail(
            db,
            profile_id=profile.id,
            request_id=entry.id,
        )

        assert first_detail is not None
        assert first_detail.caller_user_agent == "Codex CLI/1.2"
        assert first_detail.upstream_user_agent == "Claude Code/2.0"
        assert getattr(first_detail, "caller_client_display") == "Codex"
        assert getattr(first_detail, "upstream_client_display") == "Claude Code"
        assert getattr(first_detail, "user_agent_overridden") is True

        custom_rule.name = "Codex Renamed"
        await db.commit()

        second_detail = await get_request_log_detail(
            db,
            profile_id=profile.id,
            request_id=entry.id,
        )

    assert second_detail is not None
    assert getattr(second_detail, "caller_client_display") == "Codex Renamed"
    assert getattr(second_detail, "upstream_client_display") == "Claude Code"


@pytest.mark.asyncio
async def test_get_request_logs_filters_by_model_id_and_endpoint_id() -> None:
    from app.services.stats.request_logs import get_request_logs

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"request-log-model-endpoint-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.flush()

        db.add_all(
            [
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=100,
                    created_at=created_at,
                    model_id="gpt-5.4",
                    endpoint_id=1,
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=110,
                    created_at=created_at,
                    model_id="gpt-5.4",
                    endpoint_id=2,
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=120,
                    created_at=created_at,
                    model_id="claude-3.7-sonnet",
                    endpoint_id=1,
                ),
            ]
        )
        await db.commit()

        items, total = await get_request_logs(
            db,
            profile_id=profile.id,
            model_id="gpt-5.4",
            endpoint_id=1,
            limit=50,
            offset=0,
        )

    assert total == 1
    assert [item.model_id for item in items] == ["gpt-5.4"]
    assert [item.endpoint_id for item in items] == [1]


@pytest.mark.asyncio
async def test_get_stats_summary_groups_by_api_family() -> None:
    from app.services.stats.summary import get_stats_summary

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"summary-api-family-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.flush()

        db.add_all(
            [
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=100,
                    created_at=created_at,
                    api_family="openai",
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=110,
                    created_at=created_at,
                    api_family="openai",
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=120,
                    created_at=created_at,
                    api_family="anthropic",
                ),
            ]
        )
        await db.commit()

        summary = await get_stats_summary(
            db,
            profile_id=profile.id,
            group_by="api_family",
        )

    summary_groups = summary["groups"]
    assert isinstance(summary_groups, list)
    assert {group["key"]: group["total_requests"] for group in summary_groups} == {
        "openai": 2,
        "anthropic": 1,
    }


@pytest.mark.asyncio
async def test_get_throughput_stats_filters_by_api_family() -> None:
    from app.services.stats.throughput import get_throughput_stats

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"throughput-api-family-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.flush()

        db.add_all(
            [
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=100,
                    created_at=created_at,
                    api_family="openai",
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=110,
                    created_at=created_at,
                    api_family="gemini",
                ),
            ]
        )
        await db.commit()

        report = await get_throughput_stats(
            db,
            profile_id=profile.id,
            api_family="openai",
        )

    assert report["total_requests"] == 1


@pytest.mark.asyncio
async def test_get_spending_report_groups_by_api_family() -> None:
    from app.services.stats.spending import get_spending_report

    created_at = datetime(2026, 3, 20, 12, 0, 0, tzinfo=timezone.utc)

    async with AsyncSessionLocal() as db:
        profile = Profile(
            name=f"spending-api-family-profile-{uuid4()}",
            is_active=False,
            is_default=False,
        )
        db.add(profile)
        await db.flush()

        db.add_all(
            [
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=100,
                    created_at=created_at,
                    api_family="openai",
                    success_flag=True,
                    billable_flag=True,
                    priced_flag=True,
                    total_cost_user_currency_micros=120,
                ),
                _request_log(
                    profile_id=profile.id,
                    response_time_ms=110,
                    created_at=created_at,
                    api_family="anthropic",
                    success_flag=True,
                    billable_flag=True,
                    priced_flag=True,
                    total_cost_user_currency_micros=80,
                ),
            ]
        )
        await db.commit()

        report = await get_spending_report(
            db,
            profile_id=profile.id,
            group_by="api_family",
        )

    report_groups = report["groups"]
    assert isinstance(report_groups, list)
    assert {group["key"]: group["total_cost_micros"] for group in report_groups} == {
        "openai": 120,
        "anthropic": 80,
    }
