from __future__ import annotations

import asyncio

import pytest

from app.bootstrap.startup import DEFAULT_VENDORS
from app.main import APP_VERSION
from app.routers.profiles_domains.helpers import MAX_NON_DELETED_PROFILES
from tests.smoke_support import mounted_smoke_app, seed_management_session

pytestmark = pytest.mark.backend_smoke


def test_mounted_backend_startup_bootstraps_health_profiles_and_vendors_over_http() -> (
    None
):
    async def run() -> None:
        async with mounted_smoke_app() as harness:
            health_response = await harness.client.get("/health")

            async with harness.db_session() as db:
                session = await seed_management_session(db)

            bootstrap_response = await harness.management_request(
                "GET",
                "/api/profiles/bootstrap",
                session=session,
            )
            vendors_response = await harness.management_request(
                "GET",
                "/api/vendors",
                session=session,
            )

            assert health_response.status_code == 200
            assert health_response.json() == {"status": "ok", "version": APP_VERSION}

            assert bootstrap_response.status_code == 200
            bootstrap_payload = bootstrap_response.json()
            active_profile = bootstrap_payload["active_profile"]
            assert active_profile is not None
            assert bootstrap_payload["profile_limits"] == {
                "max_profiles": MAX_NON_DELETED_PROFILES
            }
            assert any(
                profile["id"] == active_profile["id"]
                for profile in bootstrap_payload["profiles"]
            )

            assert vendors_response.status_code == 200
            vendors_payload = vendors_response.json()
            vendors_by_key = {vendor["key"]: vendor for vendor in vendors_payload}
            expected_vendors_by_key = {
                vendor["key"]: vendor for vendor in DEFAULT_VENDORS
            }
            assert set(expected_vendors_by_key) <= set(vendors_by_key)
            for key, expected_vendor in expected_vendors_by_key.items():
                vendor = vendors_by_key[key]
                assert vendor["name"] == expected_vendor["name"]
                assert vendor["description"] == expected_vendor["description"]
                assert vendor["icon_key"] == expected_vendor["icon_key"]
                assert vendor["is_readonly"] is True

    asyncio.run(run())
