from __future__ import annotations

import asyncio

import pytest
from sqlalchemy import delete, select

from app.bootstrap.startup import DEFAULT_VENDORS
from app.core.crypto import hash_password
from app.models.models import RefreshToken
from app.services.auth_service import get_or_create_app_auth_settings
from tests.smoke_support import (
    ManagementSession,
    mounted_smoke_app,
    seed_management_session,
    seed_runtime_proxy_key,
)

pytestmark = pytest.mark.backend_smoke


def _assert_default_vendors_present(payload: list[dict[str, object]]) -> None:
    vendors_by_key = {vendor["key"]: vendor for vendor in payload}
    expected_vendors_by_key = {vendor["key"]: vendor for vendor in DEFAULT_VENDORS}

    assert set(expected_vendors_by_key) <= set(vendors_by_key)
    for key, expected_vendor in expected_vendors_by_key.items():
        vendor = vendors_by_key[key]
        assert vendor["name"] == expected_vendor["name"]
        assert vendor["description"] == expected_vendor["description"]
        assert vendor["icon_key"] == expected_vendor["icon_key"]


async def _configure_auth_disabled(harness) -> None:
    async with harness.db_session() as db:
        settings_row = await get_or_create_app_auth_settings(db)
        settings_row.auth_enabled = False
        settings_row.username = None
        settings_row.password_hash = None
        await db.execute(delete(RefreshToken))
        await db.commit()


async def _prepare_auth_enabled_state(
    harness,
    *,
    password: str,
    username: str,
) -> tuple[int, ManagementSession]:
    async with harness.db_session() as db:
        settings_row = await get_or_create_app_auth_settings(db)
        settings_row.auth_enabled = True
        settings_row.username = username
        settings_row.password_hash = hash_password(password)
        await db.flush()

        helper_session = await seed_management_session(
            db,
            commit=False,
            username=username,
        )
        await db.execute(delete(RefreshToken))
        await db.commit()
        return settings_row.id, helper_session


async def _list_refresh_tokens(harness) -> list[RefreshToken]:
    async with harness.db_session() as db:
        result = await db.execute(select(RefreshToken).order_by(RefreshToken.id.asc()))
        return list(result.scalars().all())


def test_auth_disabled_public_bootstrap_keeps_management_routes_open() -> None:
    async def run() -> None:
        async with mounted_smoke_app() as harness:
            await _configure_auth_disabled(harness)

            bootstrap_response = await harness.client.get("/api/auth/public-bootstrap")
            vendors_response = await harness.client.get("/api/vendors")

            assert bootstrap_response.status_code == 200
            assert bootstrap_response.json() == {
                "authenticated": False,
                "auth_enabled": False,
                "username": None,
            }

            assert vendors_response.status_code == 200
            _assert_default_vendors_present(vendors_response.json())

    asyncio.run(run())


def test_auth_enabled_unauth_session_lifecycle_shapes_management_cookies_and_state() -> (
    None
):
    async def run() -> None:
        password = "smoke-password-123"
        username = "smoke-admin"

        async with mounted_smoke_app() as harness:
            auth_subject_id, helper_session = await _prepare_auth_enabled_state(
                harness,
                password=password,
                username=username,
            )
            access_cookie_name = harness.settings.auth_cookie_name
            refresh_cookie_name = harness.settings.auth_refresh_cookie_name

            async with harness.db_session() as db:
                proxy_key = await seed_runtime_proxy_key(
                    db,
                    auth_subject_id=auth_subject_id,
                )

            unauthenticated_response = await harness.client.get("/api/vendors")
            proxy_key_response = await harness.client.get(
                "/api/vendors",
                headers=proxy_key.as_headers(),
            )
            helper_session_response = await harness.management_request(
                "GET",
                "/api/vendors",
                session=helper_session,
            )

            assert unauthenticated_response.status_code == 401
            assert unauthenticated_response.json() == {
                "detail": "Authentication required"
            }
            assert proxy_key_response.status_code == 401
            assert proxy_key_response.json() == {"detail": "Authentication required"}

            assert helper_session_response.status_code == 200
            _assert_default_vendors_present(helper_session_response.json())

            login_response = await harness.client.post(
                "/api/auth/login",
                json={
                    "username": username,
                    "password": password,
                    "session_duration": "7_days",
                },
            )

            assert login_response.status_code == 200
            assert login_response.json() == {
                "authenticated": True,
                "auth_enabled": True,
                "username": username,
            }
            assert harness.client.cookies.get(access_cookie_name) is not None
            first_refresh_cookie = harness.client.cookies.get(refresh_cookie_name)
            assert first_refresh_cookie is not None
            login_cookie_headers = login_response.headers.get_list("set-cookie")
            assert any(access_cookie_name in header for header in login_cookie_headers)
            assert any(refresh_cookie_name in header for header in login_cookie_headers)

            management_response = await harness.client.get("/api/vendors")
            session_response = await harness.client.get("/api/auth/session")
            login_refresh_rows = await _list_refresh_tokens(harness)

            assert management_response.status_code == 200
            _assert_default_vendors_present(management_response.json())
            assert session_response.status_code == 200
            assert session_response.json() == {
                "authenticated": True,
                "auth_enabled": True,
                "username": username,
            }
            assert len(login_refresh_rows) == 1
            original_refresh_row = login_refresh_rows[0]
            assert original_refresh_row.revoked_at is None
            assert original_refresh_row.last_used_at is None

            refresh_response = await harness.client.post("/api/auth/refresh")

            assert refresh_response.status_code == 200
            assert refresh_response.json() == {
                "authenticated": True,
                "auth_enabled": True,
                "username": username,
            }
            assert harness.client.cookies.get(access_cookie_name) is not None
            refreshed_refresh_cookie = harness.client.cookies.get(refresh_cookie_name)
            assert refreshed_refresh_cookie is not None
            assert refreshed_refresh_cookie != first_refresh_cookie
            refresh_cookie_headers = refresh_response.headers.get_list("set-cookie")
            assert any(
                access_cookie_name in header for header in refresh_cookie_headers
            )
            assert any(
                refresh_cookie_name in header for header in refresh_cookie_headers
            )

            rotated_refresh_rows = await _list_refresh_tokens(harness)
            assert len(rotated_refresh_rows) == 2
            assert rotated_refresh_rows[0].id == original_refresh_row.id
            assert rotated_refresh_rows[0].revoked_at is not None
            assert rotated_refresh_rows[0].last_used_at is not None
            assert rotated_refresh_rows[1].revoked_at is None
            assert rotated_refresh_rows[1].rotated_from_id == original_refresh_row.id

            logout_response = await harness.client.post("/api/auth/logout")

            assert logout_response.status_code == 200
            assert logout_response.json() == {
                "authenticated": False,
                "auth_enabled": True,
                "username": None,
            }
            assert harness.client.cookies.get(access_cookie_name) is None
            assert harness.client.cookies.get(refresh_cookie_name) is None

            revoked_refresh_rows = await _list_refresh_tokens(harness)
            assert len(revoked_refresh_rows) == 2
            assert all(row.revoked_at is not None for row in revoked_refresh_rows)

            post_logout_session_response = await harness.client.get("/api/auth/session")

            assert post_logout_session_response.status_code == 401
            assert post_logout_session_response.json() == {
                "detail": "Authentication required"
            }

    asyncio.run(run())
