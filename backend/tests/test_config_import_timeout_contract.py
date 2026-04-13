from __future__ import annotations

from typing import Any

from pydantic import ValidationError

from app.schemas.schemas import ConfigImportRequest, EndpointUpdate

REMOVED_POLICY_FIELD = "_".join(("missing", "special", "token", "price", "policy"))
LEGACY_POLICY_VALUE = "legacy-policy-value"


def _assert_validation_error(model, payload: dict[str, Any]) -> None:
    try:
        model.model_validate(payload)
    except ValidationError:
        return
    raise AssertionError("Expected validation to fail")


def _build_valid_profile_import_payload() -> dict[str, Any]:
    return {
        "version": 3,
        "bundle_kind": "profile_config",
        "vendor_refs": [],
        "endpoints": [
            {
                "name": "Demo endpoint",
                "base_url": "https://demo.invalid",
                "position": 0,
            }
        ],
        "pricing_templates": [
            {
                "name": "Demo pricing",
                "pricing_unit": "PER_1M",
                "pricing_currency_code": "USD",
                "input_price": "1.50",
                "output_price": "3.00",
            }
        ],
        "loadbalance_strategies": [],
        "models": [],
        "secret_payload": {
            "kind": "encrypted",
            "cipher": "fernet-v1",
            "key_id": "test-key",
            "entries": [],
        },
    }


def test_endpoint_update_rejects_removed_timeout_fields() -> None:
    _assert_validation_error(
        EndpointUpdate,
        {
            "write_timeout": 60.0,
        },
    )


def test_config_import_accepts_current_timeoutless_bundle() -> None:
    payload = ConfigImportRequest.model_validate(_build_valid_profile_import_payload())

    dumped_payload = payload.model_dump()
    assert dumped_payload["version"] == 3
    assert dumped_payload["endpoints"][0] == {
        "name": "Demo endpoint",
        "base_url": "https://demo.invalid",
        "api_key_secret_ref": None,
        "position": 0,
    }
    assert dumped_payload["pricing_templates"][0] == {
        "name": "Demo pricing",
        "description": None,
        "pricing_unit": "PER_1M",
        "pricing_currency_code": "USD",
        "input_price": "1.50",
        "output_price": "3.00",
        "cached_input_price": None,
        "cache_creation_price": None,
        "reasoning_price": None,
        "version": 1,
    }


def test_config_import_rejects_removed_endpoint_timeout_fields() -> None:
    payload = _build_valid_profile_import_payload()
    payload["endpoints"][0]["write_timeout"] = 60.0

    _assert_validation_error(ConfigImportRequest, payload)


def test_config_import_rejects_removed_strategy_timeout_policy() -> None:
    payload = _build_valid_profile_import_payload()
    payload["loadbalance_strategies"] = [
        {
            "name": "Default legacy routing",
            "strategy_type": "legacy",
            "legacy_strategy_type": "round-robin",
            "auto_recovery": {"mode": "disabled"},
            "timeout_policy": {"attempt_open_timeout_ms": 2_000},
        }
    ]

    _assert_validation_error(ConfigImportRequest, payload)


def test_config_import_rejects_removed_pricing_template_policy_field() -> None:
    payload = _build_valid_profile_import_payload()
    payload["pricing_templates"][0][REMOVED_POLICY_FIELD] = LEGACY_POLICY_VALUE

    _assert_validation_error(ConfigImportRequest, payload)
