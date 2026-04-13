from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from typing import Any, cast

from fastapi import HTTPException
import app.routers.config_domains.import_executor as import_executor_module
from app.routers.config_domains.import_executor import build_import_preview
from app.routers.config_domains.import_export import (
    _build_preview_error_response,
    export_vendor_catalog,
    preview_vendor_catalog_import,
)
from app.routers.config_domains.import_validator import validate_import_payload
from app.schemas.schemas import ConfigImportRequest, ConfigVendorCatalogImportRequest
from pydantic import ValidationError


class _FakeExecuteResult:
    def __init__(self, *, items=None) -> None:
        self._items = list(items or [])

    def scalars(self):
        return self

    def all(self):
        return list(self._items)


class _FakeAsyncSession:
    def __init__(self, results: list[_FakeExecuteResult]) -> None:
        self._results = iter(results)

    async def execute(self, _query):
        return next(self._results)


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


def test_profile_config_import_schema_rejects_v2_bundle_version() -> None:
    payload = _build_valid_profile_import_payload()
    payload["version"] = 2

    try:
        ConfigImportRequest.model_validate(payload)
    except ValidationError as exc:
        error = exc.errors()[0]
        assert error["loc"] == ("version",)
        assert "Input should be 3" in error["msg"]
        return

    raise AssertionError("Expected v2 profile config bundle to be rejected")


def test_validate_import_payload_rejects_constructed_v2_bundle_explicitly() -> None:
    payload = ConfigImportRequest.model_validate(_build_valid_profile_import_payload())
    downgraded_payload = payload.model_copy(update={"version": 2})

    try:
        validate_import_payload(downgraded_payload)
    except HTTPException as exc:
        assert exc.status_code == 400
        assert exc.detail == "Unsupported profile config bundle version '2'; expected 3"
        return

    raise AssertionError("Expected explicit version gate to reject v2 profile bundle")


def test_profile_preview_contract_uses_v3(monkeypatch) -> None:
    async def run() -> None:
        payload = ConfigImportRequest.model_validate(
            _build_valid_profile_import_payload()
        )
        monkeypatch.setattr(
            import_executor_module, "get_bundle_secret_key_id", lambda: "test-key"
        )

        preview = await build_import_preview(cast(Any, object()), data=payload)
        error_preview = _build_preview_error_response(
            data=payload,
            detail="bad bundle",
        )

        assert preview.model_dump()["version"] == 3
        assert preview.model_dump()["bundle_kind"] == "profile_config"
        assert error_preview.model_dump()["version"] == 3
        assert error_preview.model_dump()["blocking_errors"] == ["bad bundle"]

    asyncio.run(run())


def test_vendor_catalog_contract_stays_v2() -> None:
    async def run() -> None:
        export_db = cast(Any, _FakeAsyncSession([_FakeExecuteResult(items=[])]))
        export_payload = await export_vendor_catalog(db=export_db)
        assert export_payload.model_dump()["version"] == 2
        assert export_payload.model_dump()["bundle_kind"] == "vendor_catalog"

        preview_payload = ConfigVendorCatalogImportRequest.model_validate(
            {
                "version": 2,
                "bundle_kind": "vendor_catalog",
                "exported_at": datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc),
                "vendors": [],
            }
        )
        preview_db = cast(Any, _FakeAsyncSession([_FakeExecuteResult(items=[])]))
        preview_response = await preview_vendor_catalog_import(
            data=preview_payload,
            db=preview_db,
        )
        assert preview_response.model_dump()["version"] == 2
        assert preview_response.model_dump()["bundle_kind"] == "vendor_catalog"

        try:
            ConfigVendorCatalogImportRequest.model_validate(
                {
                    "version": 3,
                    "bundle_kind": "vendor_catalog",
                    "vendors": [],
                }
            )
        except ValidationError as exc:
            error = exc.errors()[0]
            assert error["loc"] == ("version",)
            assert "Input should be 2" in error["msg"]
            return

        raise AssertionError("Expected vendor catalog version 3 to be rejected")

    asyncio.run(run())
