from __future__ import annotations

import asyncio
import copy
import json
from typing import Any
from uuid import uuid4

from fastapi import HTTPException
import pytest

import app.routers.config_domains.import_executor as import_executor_module
from tests.smoke_support import (
    mounted_smoke_app,
    seed_connection,
    seed_endpoint,
    seed_loadbalance_strategy,
    seed_management_session,
    seed_model,
    seed_profile,
    seed_runtime_proxy_key,
    seed_vendor,
)

pytestmark = pytest.mark.backend_smoke


def _stable_export_snapshot(payload: dict[str, Any]) -> dict[str, Any]:
    secret_payload = payload["secret_payload"]
    return {
        "bundle_kind": payload["bundle_kind"],
        "endpoints": payload["endpoints"],
        "header_blocklist_rules": payload["header_blocklist_rules"],
        "loadbalance_strategies": payload["loadbalance_strategies"],
        "models": payload["models"],
        "pricing_templates": payload["pricing_templates"],
        "profile_settings": payload["profile_settings"],
        "secret_payload": {
            "cipher": secret_payload["cipher"],
            "key_id": secret_payload["key_id"],
            "kind": secret_payload["kind"],
            "refs": [entry["ref"] for entry in secret_payload["entries"]],
        },
        "vendor_refs": payload["vendor_refs"],
        "version": payload["version"],
    }


def _header_value(headers: dict[str, str], name: str) -> str | None:
    for key, value in headers.items():
        if key.lower() == name.lower():
            return value
    return None


def test_profile_config_import_rollback_smoke(monkeypatch) -> None:
    async def run() -> None:
        suffix = uuid4().hex[:8]
        initial_endpoint_name = f"Config smoke endpoint {suffix}"
        initial_endpoint_base_url = f"https://config-{suffix}.invalid/original"
        replacement_endpoint_name = f"Config smoke replacement endpoint {suffix}"
        replacement_endpoint_base_url = f"https://config-{suffix}.invalid/replacement"
        vendor_key = f"config-smoke-vendor-{suffix}"
        vendor_name = f"Config Smoke Vendor {suffix}"
        strategy_name = f"Config Smoke Strategy {suffix}"
        model_id = f"config-smoke-model-{suffix}"
        endpoint_secret_ref = f"endpoint:{initial_endpoint_name}:api_key"
        injected_failure_detail = "Injected rollback smoke failure"

        async with mounted_smoke_app() as harness:
            async with harness.db_session() as db:
                management_session = await seed_management_session(
                    db,
                    commit=False,
                    username=f"config-smoke-admin-{suffix}",
                )
                profile = await seed_profile(
                    db,
                    is_active=False,
                    is_default=False,
                    name=f"Config Smoke Profile {suffix}",
                )
                vendor = await seed_vendor(db, key=vendor_key, name=vendor_name)
                strategy = await seed_loadbalance_strategy(
                    db,
                    name=strategy_name,
                    profile_id=profile.id,
                )
                model = await seed_model(
                    db,
                    api_family="openai",
                    display_name=f"Config Smoke Model {suffix}",
                    loadbalance_strategy_id=strategy.id,
                    model_id=model_id,
                    profile_id=profile.id,
                    vendor_id=vendor.id,
                )
                endpoint = await seed_endpoint(
                    db,
                    api_key=f"config-secret-{suffix}",
                    base_url=initial_endpoint_base_url,
                    name=initial_endpoint_name,
                    profile_id=profile.id,
                )
                await seed_connection(
                    db,
                    endpoint_id=endpoint.id,
                    model_config_id=model.id,
                    name=f"Config Smoke Connection {suffix}",
                    profile_id=profile.id,
                )
                await db.commit()
                profile_id = profile.id

            export_response = await harness.management_request(
                "GET",
                "/api/config/profile/export",
                profile_id=profile_id,
                session=management_session,
            )
            assert export_response.status_code == 200
            assert export_response.headers["content-disposition"].startswith(
                'attachment; filename="gateway-config-'
            )

            export_payload = export_response.json()
            assert export_payload["version"] == 3
            assert export_payload["bundle_kind"] == "profile_config"
            assert export_payload["vendor_refs"] == [
                {
                    "description_hint": None,
                    "icon_key_hint": None,
                    "key": vendor_key,
                    "name_hint": vendor_name,
                }
            ]
            assert export_payload["endpoints"] == [
                {
                    "api_key_secret_ref": endpoint_secret_ref,
                    "base_url": initial_endpoint_base_url,
                    "name": initial_endpoint_name,
                    "position": 1,
                }
            ]
            assert export_payload["pricing_templates"] == []
            assert export_payload["models"] == [
                {
                    "api_family": "openai",
                    "connections": [
                        {
                            "auth_type": None,
                            "custom_headers": None,
                            "endpoint_name": initial_endpoint_name,
                            "is_active": True,
                            "max_in_flight_non_stream": None,
                            "max_in_flight_stream": None,
                            "name": f"Config Smoke Connection {suffix}",
                            "openai_probe_endpoint_variant": None,
                            "pricing_template_name": None,
                            "priority": 0,
                            "qps_limit": None,
                        }
                    ],
                    "display_name": f"Config Smoke Model {suffix}",
                    "is_enabled": True,
                    "loadbalance_strategy_name": strategy_name,
                    "model_id": model_id,
                    "model_type": "native",
                    "proxy_targets": [],
                    "vendor_key": vendor_key,
                }
            ]
            strategy_payload = export_payload["loadbalance_strategies"][0]
            assert strategy_payload["name"] == strategy_name
            assert strategy_payload["strategy_type"] == "legacy"
            assert strategy_payload["legacy_strategy_type"] == "single"
            assert strategy_payload["routing_policy"] is None
            assert isinstance(strategy_payload["auto_recovery"], dict)
            assert export_payload["profile_settings"] == {
                "endpoint_fx_mappings": [],
                "report_currency_code": "USD",
                "report_currency_symbol": "$",
                "timezone_preference": None,
            }
            assert export_payload["header_blocklist_rules"] == []
            assert export_payload["secret_payload"]["kind"] == "encrypted"
            assert export_payload["secret_payload"]["cipher"] == "fernet-v1"
            assert isinstance(export_payload["secret_payload"]["key_id"], str)
            assert (
                export_payload["secret_payload"]["entries"][0]["ref"]
                == endpoint_secret_ref
            )

            before_snapshot = _stable_export_snapshot(export_payload)
            replacement_payload = copy.deepcopy(export_payload)
            replacement_payload["endpoints"][0]["name"] = replacement_endpoint_name
            replacement_payload["endpoints"][0]["base_url"] = (
                replacement_endpoint_base_url
            )
            replacement_payload["models"][0]["display_name"] = (
                f"Config Smoke Replacement Model {suffix}"
            )
            replacement_payload["models"][0]["connections"][0]["endpoint_name"] = (
                replacement_endpoint_name
            )

            preview_response = await harness.management_request(
                "POST",
                "/api/config/profile/import/preview",
                json=replacement_payload,
                session=management_session,
            )
            assert preview_response.status_code == 200
            preview_payload = preview_response.json()
            assert preview_payload == {
                "blocking_errors": [],
                "bundle_kind": "profile_config",
                "connections_imported": 1,
                "decryptable_secret_refs": [endpoint_secret_ref],
                "endpoints_imported": 1,
                "models_imported": 1,
                "pricing_templates_imported": 0,
                "ready": True,
                "secret_key_id": export_payload["secret_payload"]["key_id"],
                "strategies_imported": 1,
                "vendor_resolutions": [
                    {
                        "resolution": "reuse",
                        "vendor_key": vendor_key,
                        "warning": None,
                    }
                ],
                "version": 3,
                "warnings": [],
            }

            async def fail_sync_id_sequence_if_present(*_args: object) -> None:
                raise HTTPException(status_code=409, detail=injected_failure_detail)

            monkeypatch.setattr(
                import_executor_module,
                "_sync_id_sequence_if_present",
                fail_sync_id_sequence_if_present,
            )

            import_response = await harness.management_request(
                "POST",
                "/api/config/profile/import",
                json=replacement_payload,
                profile_id=profile_id,
                session=management_session,
            )
            assert import_response.status_code == 409
            assert import_response.json() == {"detail": injected_failure_detail}

            after_export_response = await harness.management_request(
                "GET",
                "/api/config/profile/export",
                profile_id=profile_id,
                session=management_session,
            )
            assert after_export_response.status_code == 200
            after_export_payload = after_export_response.json()
            assert _stable_export_snapshot(after_export_payload) == before_snapshot
            assert after_export_payload["endpoints"][0]["name"] == initial_endpoint_name
            assert (
                after_export_payload["endpoints"][0]["base_url"]
                == initial_endpoint_base_url
            )
            assert after_export_payload["models"][0]["display_name"] == (
                f"Config Smoke Model {suffix}"
            )
            assert (
                after_export_payload["endpoints"][0]["name"]
                != replacement_endpoint_name
            )
            assert (
                after_export_payload["endpoints"][0]["base_url"]
                != replacement_endpoint_base_url
            )

    asyncio.run(run())


def test_audit_log_list_and_detail_smoke() -> None:
    async def run() -> None:
        suffix = uuid4().hex[:8]
        endpoint_api_key = f"audit-secret-{suffix}"
        endpoint_base_url = f"https://audit-{suffix}.invalid/upstream"
        model_id = f"audit-smoke-model-{suffix}"
        prompt = f"audit-smoke-{suffix}-" + ("x" * 240)

        async with mounted_smoke_app() as harness:
            async with harness.db_session() as db:
                management_session = await seed_management_session(
                    db,
                    commit=False,
                    username=f"audit-smoke-admin-{suffix}",
                )
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    commit=False,
                    name=f"Audit Smoke Proxy Key {suffix}",
                )
                await db.commit()

            active_profile_response = await harness.management_request(
                "GET",
                "/api/profiles/active",
                session=management_session,
            )
            assert active_profile_response.status_code == 200
            active_profile_id = active_profile_response.json()["id"]

            async with harness.db_session() as db:
                vendor = await seed_vendor(
                    db,
                    audit_capture_bodies=True,
                    audit_enabled=True,
                    key=f"audit-smoke-vendor-{suffix}",
                    name=f"Audit Smoke Vendor {suffix}",
                )
                strategy = await seed_loadbalance_strategy(
                    db,
                    name=f"Audit Smoke Strategy {suffix}",
                    profile_id=active_profile_id,
                )
                model = await seed_model(
                    db,
                    api_family="openai",
                    display_name=f"Audit Smoke Model {suffix}",
                    loadbalance_strategy_id=strategy.id,
                    model_id=model_id,
                    profile_id=active_profile_id,
                    vendor_id=vendor.id,
                )
                endpoint = await seed_endpoint(
                    db,
                    api_key=endpoint_api_key,
                    base_url=endpoint_base_url,
                    name=f"Audit Smoke Endpoint {suffix}",
                    profile_id=active_profile_id,
                )
                connection = await seed_connection(
                    db,
                    endpoint_id=endpoint.id,
                    model_config_id=model.id,
                    name=f"Audit Smoke Connection {suffix}",
                    profile_id=active_profile_id,
                )
                await db.commit()
                vendor_id = vendor.id
                endpoint_id = endpoint.id
                connection_id = connection.id

            runtime_response = await harness.runtime_request(
                "POST",
                "/v1/chat/completions",
                json={
                    "messages": [{"content": prompt, "role": "user"}],
                    "model": model_id,
                },
                proxy_key=runtime_proxy_key,
            )
            assert runtime_response.status_code == 200
            assert runtime_response.json() == {"ok": True}

            await harness.wait_for_background_tasks()

            audit_list_response = await harness.management_request(
                "GET",
                "/api/audit/logs",
                params={"limit": 10, "model_id": model_id},
                profile_id=active_profile_id,
                session=management_session,
            )
            assert audit_list_response.status_code == 200
            audit_list_payload = audit_list_response.json()
            assert audit_list_payload["total"] == 1
            assert audit_list_payload["limit"] == 10
            assert audit_list_payload["offset"] == 0

            list_item = audit_list_payload["items"][0]
            assert list_item["profile_id"] == active_profile_id
            assert list_item["vendor_id"] == vendor_id
            assert list_item["model_id"] == model_id
            assert list_item["endpoint_id"] == endpoint_id
            assert list_item["connection_id"] == connection_id
            assert list_item["endpoint_base_url"] == endpoint_base_url
            assert list_item["request_method"] == "POST"
            assert list_item["request_url"].rstrip("?").endswith("/v1/chat/completions")
            assert list_item["response_status"] == 200
            assert list_item["is_stream"] is False
            assert list_item["request_body_preview"] is not None
            assert len(list_item["request_body_preview"]) == 200

            list_headers = json.loads(list_item["request_headers"])
            assert _header_value(list_headers, "authorization") == "Bearer [REDACTED]"
            assert endpoint_api_key not in list_item["request_headers"]

            audit_detail_response = await harness.management_request(
                "GET",
                f"/api/audit/logs/{list_item['id']}",
                profile_id=active_profile_id,
                session=management_session,
            )
            assert audit_detail_response.status_code == 200

            detail_payload = audit_detail_response.json()
            assert detail_payload["id"] == list_item["id"]
            assert detail_payload["profile_id"] == active_profile_id
            assert detail_payload["vendor_id"] == vendor_id
            assert detail_payload["model_id"] == model_id
            assert detail_payload["endpoint_id"] == endpoint_id
            assert detail_payload["connection_id"] == connection_id
            assert detail_payload["endpoint_base_url"] == endpoint_base_url
            assert detail_payload["request_body"] is not None
            assert detail_payload["request_body"].startswith(
                list_item["request_body_preview"]
            )
            assert len(detail_payload["request_body"]) > len(
                list_item["request_body_preview"]
            )
            assert prompt in detail_payload["request_body"]
            assert detail_payload["response_body"] is not None
            assert json.loads(detail_payload["response_body"]) == {"ok": True}

            detail_headers = json.loads(detail_payload["request_headers"])
            assert _header_value(detail_headers, "authorization") == "Bearer [REDACTED]"
            assert endpoint_api_key not in detail_payload["request_headers"]
            assert "[REDACTED]" in detail_payload["request_headers"]

    asyncio.run(run())
