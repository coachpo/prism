from __future__ import annotations

import asyncio
import json
import os
from collections.abc import Sequence
from types import SimpleNamespace
from typing import Any
from uuid import uuid4

from fastapi import FastAPI
import pytest
from sqlalchemy import select
from starlette.types import Message

from app.models.models import Profile
from app.services.realtime.connection_manager import connection_manager
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


class ASGIWebSocketSession:
    def __init__(
        self,
        app: FastAPI,
        *,
        headers: Sequence[tuple[bytes, bytes]] | None = None,
        path: str = "/api/realtime/ws",
    ) -> None:
        self._app = app
        self._disconnect_sent = False
        self._headers = [(b"host", b"testserver"), *(headers or [])]
        self._incoming: asyncio.Queue[Message] = asyncio.Queue()
        self._outgoing: asyncio.Queue[Message] = asyncio.Queue()
        self._path = path
        self._task: asyncio.Task[None] | None = None

    async def __aenter__(self) -> ASGIWebSocketSession:
        self._task = asyncio.create_task(
            self._app(self._scope(), self._receive, self._send)
        )
        await self._incoming.put({"type": "websocket.connect"})

        while True:
            message = await asyncio.wait_for(self._outgoing.get(), timeout=2.0)
            if message["type"] == "websocket.accept":
                return self
            if message["type"] == "websocket.close":
                raise AssertionError(
                    "WebSocket closed during connect: "
                    f"code={message.get('code')} reason={message.get('reason')}"
                )

    async def __aexit__(self, exc_type, exc, tb) -> bool:
        await self.close()
        return False

    async def close(self) -> None:
        if self._disconnect_sent:
            if self._task is not None:
                await asyncio.wait_for(self._task, timeout=2.0)
            return

        self._disconnect_sent = True
        await self._incoming.put({"type": "websocket.disconnect", "code": 1000})
        if self._task is not None:
            await asyncio.wait_for(self._task, timeout=2.0)

    async def receive_json(self, *, timeout: float = 2.0) -> dict[str, Any]:
        while True:
            message = await asyncio.wait_for(self._outgoing.get(), timeout=timeout)
            if message["type"] == "websocket.send":
                text = message.get("text")
                if text is None:
                    text = message.get("bytes", b"").decode("utf-8")
                return json.loads(text)
            if message["type"] == "websocket.close":
                raise AssertionError(
                    "WebSocket closed unexpectedly: "
                    f"code={message.get('code')} reason={message.get('reason')}"
                )

    async def send_json(self, payload: dict[str, Any]) -> None:
        await self._incoming.put(
            {
                "type": "websocket.receive",
                "text": json.dumps(payload),
            }
        )

    async def _receive(self) -> Message:
        return await self._incoming.get()

    async def _send(self, message: Message) -> None:
        await self._outgoing.put(message)

    def _scope(self) -> dict[str, Any]:
        return {
            "type": "websocket",
            "asgi": {"version": "3.0", "spec_version": "2.4"},
            "scheme": "ws",
            "path": self._path,
            "raw_path": self._path.encode("utf-8"),
            "query_string": b"",
            "headers": list(self._headers),
            "client": ("127.0.0.1", 1234),
            "server": ("testserver", 80),
            "subprotocols": [],
            "state": {},
        }


def _assert_single_worker_runtime_assumption() -> None:
    configured_workers = os.getenv("PRISM_BACKEND_WORKERS")
    assert configured_workers in {None, "", "1"}, (
        "This smoke assumes a single in-process worker because realtime rooms live "
        "in the process-local connection manager."
    )


def test_realtime_observability_smoke_runtime_request_propagates_to_websocket_and_stats() -> (
    None
):
    async def run() -> None:
        _assert_single_worker_runtime_assumption()

        suffix = uuid4().hex[:8]
        endpoint_api_key = f"realtime-observability-endpoint-key-{suffix}"
        model_display_name = f"Realtime Observability {suffix}"
        model_id = f"realtime-observability-model-{suffix}"
        runtime_user_agent = f"realtime-observability-smoke/{suffix}"
        total_tokens = 18
        input_tokens = 11
        output_tokens = 7
        upstream = DeterministicUpstreamClient()
        upstream.queue_json(
            {
                "id": f"chatcmpl-realtime-{suffix}",
                "object": "chat.completion",
                "model": model_id,
                "choices": [
                    {
                        "finish_reason": "stop",
                        "index": 0,
                        "message": {"content": "observed", "role": "assistant"},
                    }
                ],
                "usage": {
                    "completion_tokens": output_tokens,
                    "prompt_tokens": input_tokens,
                    "total_tokens": total_tokens,
                },
            },
            headers={"x-request-id": f"provider-realtime-{suffix}"},
        )

        async with mounted_smoke_app(upstream=upstream) as harness:
            assert connection_manager.get_stats()["total_connections"] == 0
            assert connection_manager.get_stats()["total_rooms"] == 0

            async with harness.db_session() as db:
                management_session = await seed_management_session(
                    db,
                    commit=False,
                    username=f"realtime-admin-{suffix}",
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
                vendor = await seed_vendor(
                    db,
                    key=f"realtime-observability-vendor-{suffix}",
                    name=f"Realtime Observability Vendor {suffix}",
                )
                strategy = await seed_loadbalance_strategy(
                    db,
                    profile_id=active_profile.id,
                )
                model = await seed_model(
                    db,
                    display_name=model_display_name,
                    loadbalance_strategy_id=strategy.id,
                    model_id=model_id,
                    profile_id=active_profile.id,
                    vendor_id=vendor.id,
                )
                endpoint = await seed_endpoint(
                    db,
                    api_key=endpoint_api_key,
                    base_url=f"https://realtime-observability-{suffix}.invalid/openai",
                    profile_id=active_profile.id,
                )
                connection = await seed_connection(
                    db,
                    endpoint_id=endpoint.id,
                    model_config_id=model.id,
                    profile_id=active_profile.id,
                )
                graph = SimpleNamespace(
                    connection=connection,
                    endpoint=endpoint,
                    model=model,
                    profile=active_profile,
                )
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    auth_subject_id=management_session.auth_subject_id,
                    commit=False,
                    name=f"Realtime Observability Key {suffix}",
                )
                await db.commit()

            cookie_header = (
                b"cookie",
                (
                    f"{harness.settings.auth_cookie_name}="
                    f"{management_session.access_token}"
                ).encode("utf-8"),
            )

            async with ASGIWebSocketSession(
                harness.app,
                headers=[cookie_header],
            ) as websocket:
                assert await websocket.receive_json() == {
                    "type": "authenticated",
                    "username": management_session.username,
                }
                assert await websocket.receive_json() == {"type": "heartbeat"}

                await websocket.send_json({"type": "ping"})
                assert await websocket.receive_json() == {"type": "pong"}

                await websocket.send_json(
                    {
                        "type": "subscribe",
                        "profile_id": graph.profile.id,
                        "channel": "dashboard",
                    }
                )
                assert await websocket.receive_json() == {
                    "type": "subscribed",
                    "profile_id": graph.profile.id,
                    "channel": "dashboard",
                }
                assert connection_manager.has_subscribers(
                    profile_id=graph.profile.id,
                    channel="dashboard",
                )

                runtime_response = await harness.runtime_request(
                    "POST",
                    "/v1/chat/completions",
                    headers={"user-agent": runtime_user_agent},
                    json={
                        "messages": [
                            {
                                "content": "propagate to realtime and stats",
                                "role": "user",
                            }
                        ],
                        "model": graph.model.model_id,
                    },
                    proxy_key=runtime_proxy_key,
                )

                assert runtime_response.status_code == 200
                assert runtime_response.json()["id"] == f"chatcmpl-realtime-{suffix}"

                await harness.wait_for_background_tasks()

                dashboard_update = await websocket.receive_json(timeout=5.0)
                request_log = dashboard_update["request_log"]
                route_snapshot = dashboard_update["routing_route_24h"]

                assert dashboard_update["type"] == "dashboard.update"
                assert request_log["profile_id"] == graph.profile.id
                assert request_log["model_id"] == graph.model.model_id
                assert request_log["request_path"] == "/v1/chat/completions"
                assert request_log["endpoint_id"] == graph.endpoint.id
                assert request_log["connection_id"] == graph.connection.id
                assert (
                    request_log["proxy_api_key_name_snapshot"] == runtime_proxy_key.name
                )
                assert request_log["status_code"] == 200
                assert request_log["input_tokens"] == input_tokens
                assert request_log["output_tokens"] == output_tokens
                assert request_log["total_tokens"] == total_tokens

                assert route_snapshot == {
                    "model_id": graph.model.model_id,
                    "model_config_id": graph.model.id,
                    "model_label": graph.model.display_name,
                    "endpoint_id": graph.endpoint.id,
                    "endpoint_label": graph.endpoint.name,
                    "active_connection_count": 1,
                    "traffic_request_count_24h": 1,
                    "request_count_24h": 1,
                    "success_count_24h": 1,
                    "error_count_24h": 0,
                    "success_rate_24h": 100.0,
                }
                assert dashboard_update["stats_summary_24h"]["total_requests"] >= 1
                assert dashboard_update["stats_summary_24h"]["success_count"] >= 1

                request_logs_response = await harness.management_request(
                    "GET",
                    "/api/stats/requests",
                    profile_id=graph.profile.id,
                    session=management_session,
                    params={"limit": 5, "model_id": graph.model.model_id},
                )
                assert request_logs_response.status_code == 200
                request_logs_payload = request_logs_response.json()
                assert request_logs_payload["total"] == 1
                request_log_item = request_logs_payload["items"][0]
                assert request_log_item["id"] == request_log["id"]
                assert request_log_item["endpoint_id"] == graph.endpoint.id
                assert request_log_item["connection_id"] == graph.connection.id
                assert request_log_item["status_code"] == 200
                assert request_log_item["total_tokens"] == total_tokens

                request_log_detail_response = await harness.management_request(
                    "GET",
                    f"/api/stats/requests/{request_log['id']}",
                    profile_id=graph.profile.id,
                    session=management_session,
                )
                assert request_log_detail_response.status_code == 200
                request_log_detail = request_log_detail_response.json()
                assert request_log_detail["summary"]["id"] == request_log["id"]
                assert request_log_detail["routing"]["profile_id"] == graph.profile.id
                assert request_log_detail["routing"]["endpoint_id"] == graph.endpoint.id
                assert (
                    request_log_detail["routing"]["connection_id"]
                    == graph.connection.id
                )
                assert (
                    request_log_detail["request"]["proxy_api_key_name_snapshot"]
                    == runtime_proxy_key.name
                )
                assert (
                    request_log_detail["request"]["caller_user_agent"]
                    == runtime_user_agent
                )
                assert request_log_detail["usage"]["total_tokens"] == total_tokens

                summary_response = await harness.management_request(
                    "GET",
                    "/api/stats/summary",
                    profile_id=graph.profile.id,
                    session=management_session,
                    params={
                        "connection_id": graph.connection.id,
                        "endpoint_id": graph.endpoint.id,
                        "model_id": graph.model.model_id,
                    },
                )
                assert summary_response.status_code == 200
                summary_payload = summary_response.json()
                assert summary_payload["total_requests"] == 1
                assert summary_payload["success_count"] == 1
                assert summary_payload["error_count"] == 0
                assert summary_payload["success_rate"] == 100.0
                assert summary_payload["total_input_tokens"] == input_tokens
                assert summary_payload["total_output_tokens"] == output_tokens
                assert summary_payload["total_tokens"] == total_tokens
                assert summary_payload["groups"] == []
                assert (
                    route_snapshot["request_count_24h"]
                    == summary_payload["total_requests"]
                )
                assert (
                    route_snapshot["success_count_24h"]
                    == summary_payload["success_count"]
                )
                assert (
                    route_snapshot["error_count_24h"] == summary_payload["error_count"]
                )

                endpoint_models_response = await harness.management_request(
                    "GET",
                    f"/api/stats/endpoints/{graph.endpoint.id}/models",
                    profile_id=graph.profile.id,
                    session=management_session,
                    params={"preset": "all"},
                )
                assert endpoint_models_response.status_code == 200
                endpoint_models_payload = endpoint_models_response.json()
                assert len(endpoint_models_payload) == 1
                endpoint_model_stat = endpoint_models_payload[0]
                assert endpoint_model_stat["model_id"] == graph.model.model_id
                assert endpoint_model_stat["model_label"] == graph.model.display_name
                assert endpoint_model_stat["request_count"] == 1
                assert endpoint_model_stat["success_rate"] == 100.0
                assert endpoint_model_stat["priced_request_count"] == 0
                assert endpoint_model_stat["unpriced_request_count"] == 1
                assert endpoint_model_stat["total_tokens"] == total_tokens

            assert connection_manager.get_stats()["total_connections"] == 0
            assert connection_manager.get_stats()["total_rooms"] == 0

    asyncio.run(run())
