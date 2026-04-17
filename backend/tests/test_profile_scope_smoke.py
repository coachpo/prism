from __future__ import annotations

import asyncio
from dataclasses import dataclass
import json
from uuid import uuid4

import pytest
from sqlalchemy.ext.asyncio import AsyncSession

from app.dependencies import PROFILE_ID_HEADER
from tests.smoke_support import (
    mounted_smoke_app,
    seed_connection,
    seed_endpoint,
    seed_loadbalance_strategy,
    seed_management_session,
    seed_model,
    seed_profile,
    seed_proxy_target,
    seed_runtime_proxy_key,
    seed_vendor,
)

pytestmark = pytest.mark.backend_smoke


@dataclass(frozen=True)
class SeededScopeGraph:
    endpoint_base_url: str
    profile_id: int
    target_model_id: str


def _model_payload_for(
    models_payload: list[dict[str, object]],
    *,
    model_id: str,
) -> dict[str, object]:
    return next(model for model in models_payload if model["model_id"] == model_id)


def _proxy_target_model_id(
    models_payload: list[dict[str, object]],
    *,
    model_id: str,
) -> str:
    proxy_model_payload = _model_payload_for(models_payload, model_id=model_id)
    proxy_targets = proxy_model_payload["proxy_targets"]
    assert isinstance(proxy_targets, list)
    assert len(proxy_targets) == 1
    proxy_target = proxy_targets[0]
    assert isinstance(proxy_target, dict)
    target_model_id = proxy_target["target_model_id"]
    assert isinstance(target_model_id, str)
    return target_model_id


def _upstream_request_model(raw_body: bytes) -> str:
    payload = json.loads(raw_body.decode("utf-8"))
    model_id = payload["model"]
    assert isinstance(model_id, str)
    return model_id


async def _seed_scope_graph(
    db: AsyncSession,
    *,
    endpoint_base_url: str,
    profile_id: int,
    proxy_model_id: str,
    target_model_id: str,
    vendor_key: str,
) -> SeededScopeGraph:
    vendor = await seed_vendor(
        db,
        key=vendor_key,
        name=vendor_key.replace("-", " ").title(),
    )
    strategy = await seed_loadbalance_strategy(db, profile_id=profile_id)
    native_model = await seed_model(
        db,
        api_family="openai",
        loadbalance_strategy_id=strategy.id,
        model_id=target_model_id,
        model_type="native",
        profile_id=profile_id,
        vendor_id=vendor.id,
    )
    endpoint = await seed_endpoint(
        db,
        base_url=endpoint_base_url,
        profile_id=profile_id,
    )
    await seed_connection(
        db,
        endpoint_id=endpoint.id,
        model_config_id=native_model.id,
        profile_id=profile_id,
    )
    proxy_model = await seed_model(
        db,
        api_family=native_model.api_family,
        loadbalance_strategy_id=None,
        model_id=proxy_model_id,
        model_type="proxy",
        profile_id=profile_id,
        vendor_id=vendor.id,
    )
    await seed_proxy_target(
        db,
        source_model_config_id=proxy_model.id,
        target_model_config_id=native_model.id,
    )
    return SeededScopeGraph(
        endpoint_base_url=endpoint_base_url,
        profile_id=profile_id,
        target_model_id=target_model_id,
    )


def test_management_scope_uses_effective_profile_while_runtime_uses_active_profile() -> (
    None
):
    async def run() -> None:
        suffix = uuid4().hex[:8]
        shared_proxy_model_id = f"scope-smoke-proxy-{suffix}"
        active_target_model_id = f"scope-active-target-{suffix}"
        inactive_target_model_id = f"scope-inactive-target-{suffix}"
        active_endpoint_base_url = "https://scope-active.invalid/alpha"
        inactive_endpoint_base_url = "https://scope-inactive.invalid/beta"

        async with mounted_smoke_app() as harness:
            async with harness.db_session() as db:
                management_session = await seed_management_session(
                    db,
                    commit=False,
                    username="scope-smoke-admin",
                )
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    auth_subject_id=management_session.auth_subject_id,
                    commit=False,
                    name="scope-smoke-proxy-key",
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
                active_graph = await _seed_scope_graph(
                    db,
                    endpoint_base_url=active_endpoint_base_url,
                    profile_id=active_profile_id,
                    proxy_model_id=shared_proxy_model_id,
                    target_model_id=active_target_model_id,
                    vendor_key=f"scope-active-vendor-{suffix}",
                )
                inactive_profile = await seed_profile(
                    db,
                    is_active=False,
                    is_default=False,
                    name=f"Scope Override {suffix}",
                )
                inactive_graph = await _seed_scope_graph(
                    db,
                    endpoint_base_url=inactive_endpoint_base_url,
                    profile_id=inactive_profile.id,
                    proxy_model_id=shared_proxy_model_id,
                    target_model_id=inactive_target_model_id,
                    vendor_key=f"scope-inactive-vendor-{suffix}",
                )
                await db.commit()

            profiles_response = await harness.management_request(
                "GET",
                "/api/profiles",
                session=management_session,
            )
            assert profiles_response.status_code == 200
            profiles_payload = profiles_response.json()
            assert active_graph.profile_id in {
                profile["id"] for profile in profiles_payload
            }
            assert inactive_graph.profile_id in {
                profile["id"] for profile in profiles_payload
            }
            assert next(
                profile["is_active"]
                for profile in profiles_payload
                if profile["id"] == active_graph.profile_id
            )
            assert not next(
                profile["is_active"]
                for profile in profiles_payload
                if profile["id"] == inactive_graph.profile_id
            )

            missing_profile_header_response = await harness.management_request(
                "GET",
                "/api/models",
                session=management_session,
            )
            assert missing_profile_header_response.status_code == 400
            assert missing_profile_header_response.json() == {
                "detail": f"{PROFILE_ID_HEADER} header is required"
            }

            active_models_response = await harness.management_request(
                "GET",
                "/api/models",
                profile_id=active_graph.profile_id,
                session=management_session,
            )
            inactive_models_response = await harness.management_request(
                "GET",
                "/api/models",
                profile_id=inactive_graph.profile_id,
                session=management_session,
            )
            assert active_models_response.status_code == 200
            assert inactive_models_response.status_code == 200
            assert (
                _proxy_target_model_id(
                    active_models_response.json(),
                    model_id=shared_proxy_model_id,
                )
                == active_target_model_id
            )
            assert (
                _proxy_target_model_id(
                    inactive_models_response.json(),
                    model_id=shared_proxy_model_id,
                )
                == inactive_target_model_id
            )

            runtime_payload = {
                "messages": [{"content": "scope smoke", "role": "user"}],
                "model": shared_proxy_model_id,
            }
            harness.upstream.built_requests.clear()

            first_runtime_response = await harness.runtime_request(
                "POST",
                "/v1/chat/completions",
                headers={PROFILE_ID_HEADER: str(inactive_graph.profile_id)},
                json=runtime_payload,
                proxy_key=runtime_proxy_key,
            )
            assert first_runtime_response.status_code == 200
            assert len(harness.upstream.built_requests) == 1
            first_upstream_request = harness.upstream.built_requests[0]
            assert str(first_upstream_request.url).rstrip("?") == (
                f"{active_graph.endpoint_base_url}/v1/chat/completions"
            )
            assert (
                _upstream_request_model(first_upstream_request.content)
                == active_graph.target_model_id
            )

            activate_response = await harness.management_request(
                "POST",
                f"/api/profiles/{inactive_graph.profile_id}/activate",
                json={"expected_active_profile_id": active_graph.profile_id},
                session=management_session,
            )
            assert activate_response.status_code == 200
            assert activate_response.json()["id"] == inactive_graph.profile_id
            assert activate_response.json()["is_active"] is True

            harness.upstream.built_requests.clear()

            second_runtime_response = await harness.runtime_request(
                "POST",
                "/v1/chat/completions",
                headers={PROFILE_ID_HEADER: str(active_graph.profile_id)},
                json=runtime_payload,
                proxy_key=runtime_proxy_key,
            )
            assert second_runtime_response.status_code == 200
            assert len(harness.upstream.built_requests) == 1
            second_upstream_request = harness.upstream.built_requests[0]
            assert str(second_upstream_request.url).rstrip("?") == (
                f"{inactive_graph.endpoint_base_url}/v1/chat/completions"
            )
            assert (
                _upstream_request_model(second_upstream_request.content)
                == inactive_graph.target_model_id
            )

            await harness.wait_for_background_tasks()

    asyncio.run(run())
