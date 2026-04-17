from __future__ import annotations

import asyncio
from uuid import uuid4

import pytest

from tests.smoke_support import (
    mounted_smoke_app,
    seed_management_session,
    seed_native_connection_graph,
    seed_runtime_proxy_key,
)


@pytest.mark.backend_smoke
def test_profile_delete_refuses_default_profile_smoke() -> None:
    async def run() -> None:
        async with mounted_smoke_app() as harness:
            async with harness.db_session() as db:
                management_session = await seed_management_session(
                    db,
                    commit=False,
                    username="delete-guardrails-admin",
                )
                await db.commit()

            profiles_response = await harness.management_request(
                "GET",
                "/api/profiles",
                session=management_session,
            )
            assert profiles_response.status_code == 200
            default_profile_payload = next(
                profile
                for profile in profiles_response.json()
                if profile["is_default"] is True
            )

            delete_response = await harness.management_request(
                "DELETE",
                f"/api/profiles/{default_profile_payload['id']}",
                session=management_session,
            )

            assert delete_response.status_code == 400
            assert delete_response.json() == {
                "detail": "Default profile cannot be deleted."
            }

            follow_up_response = await harness.management_request(
                "GET",
                "/api/profiles",
                session=management_session,
            )
            assert follow_up_response.status_code == 200
            assert default_profile_payload["id"] in {
                profile["id"] for profile in follow_up_response.json()
            }

    asyncio.run(run())


@pytest.mark.backend_smoke_destructive
def test_request_log_delete_all_cleanup_smoke() -> None:
    async def run() -> None:
        suffix = uuid4().hex[:8]
        model_id = f"delete-guardrails-model-{suffix}"

        async with mounted_smoke_app() as harness:
            async with harness.db_session() as db:
                management_session = await seed_management_session(
                    db,
                    commit=False,
                    username=f"delete-guardrails-admin-{suffix}",
                )
                graph = await seed_native_connection_graph(
                    db,
                    commit=False,
                    endpoint_api_key=f"delete-guardrails-key-{suffix}",
                    endpoint_base_url=f"https://delete-{suffix}.invalid/openai",
                    model_id=model_id,
                    vendor_key=f"delete-guardrails-vendor-{suffix}",
                    vendor_name=f"Delete Guardrails Vendor {suffix}",
                )
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    auth_subject_id=management_session.auth_subject_id,
                    commit=False,
                    name=f"Delete Guardrails Proxy Key {suffix}",
                )
                await db.commit()

            runtime_response = await harness.runtime_request(
                "POST",
                "/v1/chat/completions",
                json={
                    "messages": [
                        {
                            "content": "delete guardrails destructive smoke",
                            "role": "user",
                        }
                    ],
                    "model": graph.model.model_id,
                },
                proxy_key=runtime_proxy_key,
            )
            assert runtime_response.status_code == 200
            assert runtime_response.json() == {"ok": True}

            before_delete_response = await harness.management_request(
                "GET",
                "/api/stats/requests",
                profile_id=graph.profile.id,
                session=management_session,
                params={"limit": 10, "model_id": graph.model.model_id},
            )
            assert before_delete_response.status_code == 200
            assert before_delete_response.json()["total"] == 1

            delete_response = await harness.management_request(
                "DELETE",
                "/api/stats/requests",
                profile_id=graph.profile.id,
                session=management_session,
                params={"delete_all": True},
            )
            assert delete_response.status_code == 200
            assert delete_response.json() == {"accepted": True}

            after_delete_response = await harness.management_request(
                "GET",
                "/api/stats/requests",
                profile_id=graph.profile.id,
                session=management_session,
                params={"limit": 10, "model_id": graph.model.model_id},
            )
            assert after_delete_response.status_code == 200
            assert after_delete_response.json()["total"] == 0
            assert after_delete_response.json()["items"] == []

    asyncio.run(run())
