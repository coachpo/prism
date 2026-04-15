from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any, cast

from app.routers.proxy_domains.attempt_outcome_reporting import (
    _final_usage_event_cost_fields,
    _persist_stream_request_log,
    _persist_stream_usage_request_event,
    build_stream_finalization_snapshot,
    record_final_usage_event,
    record_request_log,
)
from app.models.models import UsageRequestEvent
from app.schemas.domains.usage_statistics import UsageRequestEventResponse
from app.services.costing_service import CostFieldPayload

_EXPECTED_BILLING_FIELDS = {
    "billable_flag": True,
    "priced_flag": False,
    "unpriced_reason": "MISSING_PRICE_DATA",
}


def _build_cost_fields(connection, status_code, tokens=None) -> CostFieldPayload:
    return cast(
        CostFieldPayload,
        {
            "success_flag": 200 <= status_code < 300,
            **_EXPECTED_BILLING_FIELDS,
        },
    )


def _make_state(*, audit_enabled: bool = False) -> SimpleNamespace:
    setup = SimpleNamespace(
        api_family="openai",
        audit_capture_bodies=False,
        audit_enabled=audit_enabled,
        build_cost_fields=_build_cost_fields,
        caller_user_agent="caller-agent",
        ingress_request_id="req-finalized",
        method="POST",
        model_id="gpt-5.4",
        proxy_api_key_id=12,
        proxy_api_key_name="Operator key",
        resolved_target_model_id="gpt-5.4-mini",
        vendor_id=None,
        vendor_key=None,
        vendor_name=None,
    )
    return SimpleNamespace(
        profile_id=1,
        request_path="/v1/chat/completions",
        setup=cast(Any, setup),
    )


def _make_target() -> SimpleNamespace:
    return SimpleNamespace(
        attempt_number=1,
        connection=SimpleNamespace(
            id=3,
            endpoint_id=2,
            endpoint_rel=SimpleNamespace(base_url="https://demo.invalid"),
        ),
        description="Demo connection",
        endpoint_body=b'{"stream": true}',
        headers={"User-Agent": "upstream-agent", "x-client-request-id": "client-1"},
        upstream_url="https://demo.invalid/v1/chat/completions",
    )


def _billing_fields(payload: dict[str, object]) -> dict[str, object]:
    return {field_name: payload[field_name] for field_name in _EXPECTED_BILLING_FIELDS}


def test_final_usage_event_cost_projection_preserves_billing_semantics() -> None:
    projected = _final_usage_event_cost_fields(
        cast(
            CostFieldPayload,
            {
                "billable_flag": True,
                "priced_flag": False,
                "unpriced_reason": "MISSING_PRICE_DATA",
            },
        )
    )

    assert projected["billable_flag"] is True
    assert projected["priced_flag"] is False
    assert projected["unpriced_reason"] == "MISSING_PRICE_DATA"


def test_usage_request_event_contract_exposes_billing_semantics() -> None:
    expected_fields = {"billable_flag", "priced_flag", "unpriced_reason"}

    assert expected_fields.issubset(set(UsageRequestEvent.__table__.c.keys()))
    assert expected_fields.issubset(set(UsageRequestEventResponse.model_fields))


def test_buffered_finalized_request_preserves_billing_field_parity() -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}

        async def log_request_fn(**kwargs):
            request_log_kwargs.update(kwargs)
            return 101

        async def log_usage_request_event_fn(**kwargs):
            usage_event_kwargs.update(kwargs)
            return 102

        deps = SimpleNamespace(
            log_request_fn=log_request_fn,
            log_usage_request_event_fn=log_usage_request_event_fn,
        )
        state = _make_state(audit_enabled=True)
        target = _make_target()

        await record_request_log(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            status_code=200,
            response_headers={"x-request-id": "provider-1"},
            response_body=b'{"id":"resp_123"}',
            elapsed_ms=250,
            is_stream=False,
        )
        await record_final_usage_event(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            status_code=200,
            attempt_count=1,
            elapsed_ms=250,
        )

        assert _billing_fields(request_log_kwargs) == _EXPECTED_BILLING_FIELDS
        assert _billing_fields(usage_event_kwargs) == _EXPECTED_BILLING_FIELDS
        assert _billing_fields(usage_event_kwargs) == _billing_fields(
            request_log_kwargs
        )

    asyncio.run(run())


def test_stream_finalized_request_preserves_billing_field_parity() -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}

        async def log_request_fn(**kwargs):
            request_log_kwargs.update(kwargs)
            return 201

        async def log_usage_request_event_fn(**kwargs):
            usage_event_kwargs.update(kwargs)
            return 202

        async def record_audit_log_fn(**kwargs):
            return None

        deps = SimpleNamespace(
            log_request_fn=log_request_fn,
            log_usage_request_event_fn=log_usage_request_event_fn,
            record_audit_log_fn=record_audit_log_fn,
        )
        state = _make_state(audit_enabled=True)
        target = _make_target()

        snapshot = build_stream_finalization_snapshot(
            attempt_count=1,
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            error_detail=None,
            response_headers={
                "content-type": "text/event-stream",
                "x-request-id": "provider-2",
            },
            status_code=200,
            elapsed_ms=250,
            ttft_ms=120,
            completion_duration_ms=1000,
            payload=b"data: [DONE]\n\n",
            provider_correlation_id="provider-2",
            token_usage=None,
        )

        await _persist_stream_request_log(snapshot)
        await _persist_stream_usage_request_event(snapshot)

        assert request_log_kwargs["audit_enabled_at_request"] is True
        assert _billing_fields(request_log_kwargs) == _EXPECTED_BILLING_FIELDS
        assert _billing_fields(usage_event_kwargs) == _EXPECTED_BILLING_FIELDS
        assert _billing_fields(usage_event_kwargs) == _billing_fields(
            request_log_kwargs
        )

    asyncio.run(run())
