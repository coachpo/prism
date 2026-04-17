from __future__ import annotations

import asyncio
import json as jsonlib
from datetime import datetime

import httpx
import pytest
from sqlalchemy import select

from app.bootstrap import startup as startup_module
from app.models.models import Connection, Profile
from tests.smoke_support import (
    DeterministicUpstreamClient,
    mounted_smoke_app,
    seed_connection,
    seed_endpoint,
    seed_loadbalance_strategy,
    seed_management_session,
    seed_model,
    seed_runtime_proxy_key,
    seed_vendor,
)

pytestmark = pytest.mark.backend_smoke


class RuntimeHealthUpstreamClient(DeterministicUpstreamClient):
    def __init__(self) -> None:
        super().__init__(
            default_json={
                "id": "chatcmpl-smoke",
                "object": "chat.completion",
                "choices": [
                    {
                        "finish_reason": "stop",
                        "index": 0,
                        "message": {"content": "ok", "role": "assistant"},
                    }
                ],
                "usage": {
                    "completion_tokens": 1,
                    "prompt_tokens": 1,
                    "total_tokens": 2,
                },
            }
        )

    async def post(
        self,
        url: str,
        *,
        headers: dict[str, str],
        json: dict[str, object],
        timeout: float = 30.0,
    ) -> httpx.Response:
        request_headers = dict(headers)
        request_headers.setdefault("content-type", "application/json")
        request = httpx.Request(
            "POST",
            url,
            headers=request_headers,
            content=jsonlib.dumps(json).encode("utf-8"),
        )
        request.extensions["timeout"] = timeout
        self.built_requests.append(request)
        await asyncio.sleep(0.01)
        return await self.send(request, stream=False, follow_redirects=True)


def _header_value(headers: httpx.Headers, name: str) -> str | None:
    expected = name.lower()
    for key, value in headers.items():
        if key.lower() == expected:
            return value
    return None


def _parse_iso_datetime(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def test_health_check_route_and_runtime_policy_smoke_over_mounted_backend() -> None:
    async def run() -> None:
        endpoint_api_key = "health-policy-endpoint-key"
        caller_user_agent = "claude-cli/2.1.109 (external, cli)"
        allowed_custom_header_value = "still-here"
        upstream = RuntimeHealthUpstreamClient()

        async with mounted_smoke_app(upstream=upstream) as harness:
            async with harness.db_session() as db:
                management_session = await seed_management_session(db, commit=False)
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    auth_subject_id=management_session.auth_subject_id,
                    commit=False,
                )
                active_profile = (
                    await db.execute(
                        select(Profile)
                        .where(
                            Profile.deleted_at.is_(None),
                            Profile.is_active.is_(True),
                        )
                        .order_by(Profile.id.asc())
                        .limit(1)
                    )
                ).scalar_one()
                vendor = await seed_vendor(db)
                strategy = await seed_loadbalance_strategy(
                    db,
                    profile_id=active_profile.id,
                )
                model = await seed_model(
                    db,
                    loadbalance_strategy_id=strategy.id,
                    profile_id=active_profile.id,
                    vendor_id=vendor.id,
                )
                endpoint = await seed_endpoint(
                    db,
                    api_key=endpoint_api_key,
                    base_url="https://health-policy.invalid",
                    profile_id=active_profile.id,
                )
                connection = await seed_connection(
                    db,
                    custom_headers=jsonlib.dumps(
                        {
                            "X-Allow-Smoke": allowed_custom_header_value,
                            "X-Correlation-ID": "blocked-after-merge",
                        }
                    ),
                    endpoint_id=endpoint.id,
                    model_config_id=model.id,
                    profile_id=active_profile.id,
                )
                await db.commit()

            upstream.queue_json({"ok": True}, status_code=200)
            upstream.queue_json(
                {"error": {"message": "invalid api key"}},
                status_code=401,
            )

            health_response = await harness.management_request(
                "POST",
                f"/api/connections/{connection.id}/health-check",
                profile_id=active_profile.id,
                session=management_session,
            )

            assert health_response.status_code == 200
            health_payload = health_response.json()
            checked_at = _parse_iso_datetime(health_payload["checked_at"])

            assert health_payload["connection_id"] == connection.id
            assert health_payload["health_status"] == "unhealthy"
            assert (
                health_payload["detail"]
                == "Authentication failed (HTTP 401): invalid api key"
            )
            assert health_payload["response_time_ms"] > 0

            async with harness.db_session() as db:
                saved_connection = (
                    await db.execute(
                        select(Connection).where(Connection.id == connection.id)
                    )
                ).scalar_one()

            assert saved_connection.health_status == "unhealthy"
            assert (
                saved_connection.health_detail
                == "Authentication failed (HTTP 401): invalid api key"
            )
            assert saved_connection.last_health_check == checked_at

            upstream.built_requests.clear()

            runtime_response = await harness.runtime_request(
                "POST",
                "/v1/chat/completions",
                headers={
                    "user-agent": caller_user_agent,
                    "x-client-kept": "runtime-ok",
                    "x-request-id": "blocked-before-merge",
                },
                json={
                    "messages": [{"content": "smoke", "role": "user"}],
                    "model": model.model_id,
                },
                proxy_key=runtime_proxy_key,
            )

            assert runtime_response.status_code == 200
            assert runtime_response.json()["id"] == "chatcmpl-smoke"
            assert len(upstream.built_requests) == 1

            upstream_request = upstream.built_requests[0]
            assert str(upstream_request.url).rstrip("?") == (
                f"{endpoint.base_url}/v1/chat/completions"
            )
            assert _header_value(upstream_request.headers, "authorization") == (
                f"Bearer {endpoint_api_key}"
            )
            assert _header_value(upstream_request.headers, "user-agent") == (
                caller_user_agent
            )
            assert _header_value(upstream_request.headers, "x-client-kept") == (
                "runtime-ok"
            )
            assert _header_value(upstream_request.headers, "x-allow-smoke") == (
                allowed_custom_header_value
            )
            assert _header_value(upstream_request.headers, "x-request-id") is None
            assert _header_value(upstream_request.headers, "x-correlation-id") is None

            seeded_rules_response = await harness.management_request(
                "GET",
                "/api/config/user-agent-client-rules",
                profile_id=active_profile.id,
                session=management_session,
                params={"include_disabled": False},
            )

            assert seeded_rules_response.status_code == 200
            seeded_rules_payload = seeded_rules_response.json()
            expected_claude_rule = next(
                rule
                for rule in startup_module.SYSTEM_USER_AGENT_CLIENT_RULE_DEFAULTS
                if rule["name"] == "Claude Code"
            )
            assert any(
                rule["is_system"] is True
                and rule["name"] == expected_claude_rule["name"]
                and rule["pattern"] == expected_claude_rule["pattern"]
                for rule in seeded_rules_payload
            )

            request_logs_response = await harness.management_request(
                "GET",
                "/api/stats/requests",
                profile_id=active_profile.id,
                session=management_session,
                params={"limit": 5, "model_id": model.model_id},
            )

            assert request_logs_response.status_code == 200
            request_logs_payload = request_logs_response.json()
            assert request_logs_payload["total"] >= 1
            request_log_item = request_logs_payload["items"][0]
            assert request_log_item["user_agent_overridden"] is False

            request_log_detail_response = await harness.management_request(
                "GET",
                f"/api/stats/requests/{request_log_item['id']}",
                profile_id=active_profile.id,
                session=management_session,
            )

            assert request_log_detail_response.status_code == 200
            request_log_detail_payload = request_log_detail_response.json()
            assert (
                request_log_detail_payload["request"]["caller_user_agent"]
                == caller_user_agent
            )
            assert (
                request_log_detail_payload["request"]["upstream_user_agent"]
                == caller_user_agent
            )

    asyncio.run(run())
