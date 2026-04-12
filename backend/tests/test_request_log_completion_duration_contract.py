from __future__ import annotations

from datetime import datetime, timezone

from app.models.models import RequestLog, UsageRequestEvent
from app.schemas.domains.stats import (
    RequestLogDetailResponse,
    RequestLogListItemResponse,
    RequestLogResponse,
)


def _make_request_log(*, completion_duration_ms: int | None) -> RequestLog:
    entry = RequestLog(
        id=101,
        profile_id=7,
        model_id="gpt-5.4",
        api_family="openai",
        vendor_id=12,
        vendor_key="openai",
        vendor_name="OpenAI",
        resolved_target_model_id="gpt-5.4-mini",
        endpoint_id=34,
        connection_id=56,
        proxy_api_key_id=78,
        proxy_api_key_name_snapshot="Operator key",
        ingress_request_id="req-row-1",
        attempt_number=1,
        provider_correlation_id="provider-request-id",
        caller_user_agent="caller-agent",
        upstream_user_agent="upstream-agent",
        endpoint_base_url="https://demo.invalid",
        status_code=200,
        response_time_ms=321,
        completion_duration_ms=completion_duration_ms,
        is_stream=False,
        request_path="/v1/chat/completions",
        input_tokens=11,
        output_tokens=22,
        total_tokens=33,
        success_flag=True,
        billable_flag=True,
        priced_flag=True,
        unpriced_reason=None,
        cache_read_input_tokens=0,
        cache_creation_input_tokens=0,
        reasoning_tokens=0,
        input_cost_micros=22,
        output_cost_micros=44,
        cache_read_input_cost_micros=0,
        cache_creation_input_cost_micros=0,
        reasoning_cost_micros=0,
        total_cost_original_micros=66,
        total_cost_user_currency_micros=66,
        currency_code_original="USD",
        report_currency_code="USD",
        report_currency_symbol="$",
        fx_rate_used="1.000000",
        fx_rate_source="DEFAULT_1_TO_1",
        pricing_snapshot_unit="1M tokens",
        pricing_snapshot_input="2.000000",
        pricing_snapshot_output="4.000000",
        pricing_snapshot_cache_read_input="0.000000",
        pricing_snapshot_cache_creation_input="0.000000",
        pricing_snapshot_reasoning="0.000000",
        pricing_config_version_used=9,
        error_detail=None,
        endpoint_description="Demo endpoint",
        created_at=datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc),
    )
    setattr(entry, "caller_client_display", "CLI")
    setattr(entry, "upstream_client_display", "CLI")
    setattr(entry, "user_agent_overridden", False)
    return entry


def test_nullable_historical_duration_fields() -> None:
    request_log_duration = RequestLog.__table__.c["completion_duration_ms"]
    usage_event_duration = UsageRequestEvent.__table__.c["completion_duration_ms"]

    assert request_log_duration.nullable is True
    assert usage_event_duration.nullable is True

    request_log_payload = RequestLogResponse.model_validate(
        _make_request_log(completion_duration_ms=None)
    ).model_dump()
    detail_payload = RequestLogDetailResponse.from_request_log(
        _make_request_log(completion_duration_ms=None)
    ).model_dump()

    assert request_log_payload["completion_duration_ms"] is None
    assert detail_payload["summary"]["response_time_ms"] == 321


def test_list_contract_exposes_completion_duration() -> None:
    payload = RequestLogListItemResponse.model_validate(
        _make_request_log(completion_duration_ms=987)
    ).model_dump()

    assert payload["completion_duration_ms"] == 987
    assert payload["response_time_ms"] == 321
    assert payload["user_agent_overridden"] is False
