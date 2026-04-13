from __future__ import annotations

from datetime import datetime, timezone
from types import SimpleNamespace
from typing import Any, cast

import pytest

from app.models.models import RequestLog, UsageRequestEvent
from app.schemas.domains.stats import RequestLogDetailResponse, RequestLogResponse
from app.schemas.domains.usage_statistics import UsageRequestEventResponse
from app.services.costing_service import CostingSettingsSnapshot, compute_cost_fields

REMOVED_SNAPSHOT_FIELD = "_".join(
    ("pricing", "snapshot", "missing", "special", "token", "price", "policy")
)


def _make_request_log() -> RequestLog:
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
        ttft_ms=123,
        completion_duration_ms=987,
        is_stream=False,
        request_path="/v1/chat/completions",
        input_tokens=11,
        output_tokens=22,
        total_tokens=33,
        success_flag=True,
        billable_flag=True,
        priced_flag=True,
        unpriced_reason=None,
        cache_read_input_tokens=4,
        cache_creation_input_tokens=5,
        reasoning_tokens=6,
        input_cost_micros=22,
        output_cost_micros=44,
        cache_read_input_cost_micros=6,
        cache_creation_input_cost_micros=8,
        reasoning_cost_micros=10,
        total_cost_original_micros=90,
        total_cost_user_currency_micros=90,
        currency_code_original="USD",
        report_currency_code="USD",
        report_currency_symbol="$",
        fx_rate_used="1.000000",
        fx_rate_source="DEFAULT_1_TO_1",
        pricing_snapshot_unit="1M tokens",
        pricing_snapshot_input="2.000000",
        pricing_snapshot_output="4.000000",
        pricing_snapshot_cache_read_input="0.250000",
        pricing_snapshot_cache_creation_input="0.400000",
        pricing_snapshot_reasoning="0.600000",
        pricing_config_version_used=9,
        error_detail=None,
        endpoint_description="Demo endpoint",
        created_at=datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc),
    )
    setattr(entry, "caller_client_display", "CLI")
    setattr(entry, "upstream_client_display", "CLI")
    setattr(entry, "user_agent_overridden", False)
    return entry


def _make_usage_request_event() -> UsageRequestEvent:
    return UsageRequestEvent(
        id=201,
        profile_id=7,
        ingress_request_id="req-row-1",
        model_id="gpt-5.4",
        resolved_target_model_id="gpt-5.4-mini",
        api_family="openai",
        endpoint_id=34,
        connection_id=56,
        proxy_api_key_id=78,
        proxy_api_key_name_snapshot="Operator key",
        status_code=200,
        response_time_ms=321,
        ttft_ms=123,
        completion_duration_ms=987,
        success_flag=True,
        input_tokens=11,
        output_tokens=22,
        total_tokens=33,
        cache_read_input_tokens=4,
        cache_creation_input_tokens=5,
        reasoning_tokens=6,
        input_cost_micros=22,
        output_cost_micros=44,
        cache_read_input_cost_micros=6,
        cache_creation_input_cost_micros=8,
        reasoning_cost_micros=10,
        total_cost_original_micros=90,
        total_cost_user_currency_micros=90,
        currency_code_original="USD",
        report_currency_code="USD",
        report_currency_symbol="$",
        fx_rate_used="1.000000",
        fx_rate_source="DEFAULT_1_TO_1",
        pricing_snapshot_unit="1M tokens",
        pricing_snapshot_input="2.000000",
        pricing_snapshot_output="4.000000",
        pricing_snapshot_cache_read_input="0.250000",
        pricing_snapshot_cache_creation_input="0.400000",
        pricing_snapshot_reasoning="0.600000",
        pricing_config_version_used=9,
        attempt_count=2,
        request_path="/v1/chat/completions",
        created_at=datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc),
    )


def _build_pricing_template(**overrides: object) -> SimpleNamespace:
    payload = {
        "pricing_currency_code": "USD",
        "pricing_unit": "1M tokens",
        "input_price": "2.00",
        "output_price": "4.00",
        "cached_input_price": "0.25",
        "cache_creation_price": "0.40",
        "reasoning_price": "0.60",
        "version": 9,
    }
    payload.update(overrides)
    return SimpleNamespace(**payload)


def test_request_log_contract_omits_deleted_policy_snapshot() -> None:
    request_log_payload = RequestLogResponse.model_validate(
        _make_request_log()
    ).model_dump()
    detail_payload = RequestLogDetailResponse.from_request_log(
        _make_request_log()
    ).model_dump()

    assert REMOVED_SNAPSHOT_FIELD not in request_log_payload
    assert request_log_payload["pricing_snapshot_reasoning"] == "0.600000"
    assert detail_payload["pricing"]["pricing_snapshot_reasoning"] == "0.600000"
    assert REMOVED_SNAPSHOT_FIELD not in detail_payload["pricing"]


def test_usage_event_contract_omits_deleted_policy_snapshot() -> None:
    payload = UsageRequestEventResponse.model_validate(
        _make_usage_request_event()
    ).model_dump()

    assert REMOVED_SNAPSHOT_FIELD not in payload
    assert payload["pricing_snapshot_reasoning"] == "0.600000"
    assert payload["pricing_config_version_used"] == 9


@pytest.mark.parametrize(
    ("price_field", "token_field"),
    [
        ("cached_input_price", "cache_read_input_tokens"),
        ("cache_creation_price", "cache_creation_input_tokens"),
        ("reasoning_price", "reasoning_tokens"),
    ],
)
def test_compute_cost_fields_keeps_missing_price_behavior_for_remaining_prices(
    price_field: str,
    token_field: str,
) -> None:
    token_values = {
        "input_tokens": 11,
        "output_tokens": 22,
        "cache_read_input_tokens": 0,
        "cache_creation_input_tokens": 0,
        "reasoning_tokens": 0,
        token_field: 5,
    }

    result = compute_cost_fields(
        connection=cast(Any, SimpleNamespace(endpoint_id=34)),
        pricing_template=cast(Any, _build_pricing_template(**{price_field: None})),
        endpoint=cast(Any, SimpleNamespace(id=34)),
        model_id="gpt-5.4",
        status_code=200,
        input_tokens=token_values["input_tokens"],
        output_tokens=token_values["output_tokens"],
        cache_read_input_tokens=token_values["cache_read_input_tokens"],
        cache_creation_input_tokens=token_values["cache_creation_input_tokens"],
        reasoning_tokens=token_values["reasoning_tokens"],
        settings=CostingSettingsSnapshot(
            report_currency_code="USD",
            report_currency_symbol="$",
            endpoint_fx_map={("gpt-5.4", 34): "1.000000"},
        ),
    )

    assert result["priced_flag"] is False
    assert result["unpriced_reason"] == "MISSING_PRICE_DATA"
    assert REMOVED_SNAPSHOT_FIELD not in result
