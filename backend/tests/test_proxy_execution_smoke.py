from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace
from uuid import uuid4

import httpx
import pytest
from sqlalchemy import select

from app.models.models import LoadbalanceEvent, Profile, RequestLog, UsageRequestEvent
from tests.smoke_support import (
    DeterministicUpstreamClient,
    mounted_smoke_app,
    seed_connection,
    seed_endpoint,
    seed_loadbalance_strategy,
    seed_model,
    seed_profile,
    seed_proxy_target,
    seed_runtime_proxy_key,
    seed_vendor,
)

pytestmark = pytest.mark.backend_smoke


class ChunkedAsyncByteStream(httpx.AsyncByteStream):
    def __init__(self, chunks: list[bytes]) -> None:
        self._chunks = list(chunks)

    async def __aiter__(self):
        for chunk in self._chunks:
            yield chunk

    async def aclose(self) -> None:
        return None


class BlockingAsyncByteStream(httpx.AsyncByteStream):
    def __init__(self, first_chunk: bytes, trailing_chunks: list[bytes]) -> None:
        self._first_chunk = first_chunk
        self._release = asyncio.Event()
        self._trailing_chunks = list(trailing_chunks)

    def release(self) -> None:
        self._release.set()

    async def __aiter__(self):
        yield self._first_chunk
        await self._release.wait()
        for chunk in self._trailing_chunks:
            yield chunk

    async def aclose(self) -> None:
        self._release.set()


async def _wait_for_upstream_request_count(
    upstream: DeterministicUpstreamClient,
    *,
    expected_count: int,
) -> None:
    for _ in range(100):
        if len(upstream.built_requests) >= expected_count:
            return
        await asyncio.sleep(0.01)
    raise AssertionError(
        f"Timed out waiting for {expected_count} upstream request(s); saw {len(upstream.built_requests)}"
    )


def _request_json(request: httpx.Request) -> dict[str, object]:
    return json.loads(request.content.decode("utf-8"))


async def _active_profile(db) -> Profile:
    result = await db.execute(
        select(Profile)
        .where(Profile.deleted_at.is_(None), Profile.is_active.is_(True))
        .limit(1)
    )
    profile = result.scalar_one_or_none()
    if profile is not None:
        return profile
    return await seed_profile(db, is_active=True, is_default=False)


async def _seed_active_proxy_route_graph(
    db,
    *,
    endpoint_api_key: str,
    endpoint_base_url: str,
    proxy_model_id: str,
    target_model_id: str,
    vendor_key: str,
    vendor_name: str,
):
    profile = await _active_profile(db)
    vendor = await seed_vendor(db, key=vendor_key, name=vendor_name)
    strategy = await seed_loadbalance_strategy(db, profile_id=profile.id)
    target_model = await seed_model(
        db,
        loadbalance_strategy_id=strategy.id,
        model_id=target_model_id,
        model_type="native",
        profile_id=profile.id,
        vendor_id=vendor.id,
    )
    endpoint = await seed_endpoint(
        db,
        api_key=endpoint_api_key,
        base_url=endpoint_base_url,
        profile_id=profile.id,
    )
    connection = await seed_connection(
        db,
        endpoint_id=endpoint.id,
        model_config_id=target_model.id,
        profile_id=profile.id,
    )
    proxy_model = await seed_model(
        db,
        loadbalance_strategy_id=None,
        model_id=proxy_model_id,
        model_type="proxy",
        profile_id=profile.id,
        vendor_id=vendor.id,
    )
    proxy_target = await seed_proxy_target(
        db,
        source_model_config_id=proxy_model.id,
        target_model_config_id=target_model.id,
    )
    return SimpleNamespace(
        connection=connection,
        endpoint=endpoint,
        profile=profile,
        proxy_model=proxy_model,
        proxy_target=proxy_target,
        strategy=strategy,
        target_model=target_model,
        vendor=vendor,
    )


async def _request_logs_for_model(harness, *, model_id: str) -> list[RequestLog]:
    async with harness.db_session() as db:
        result = await db.execute(
            select(RequestLog)
            .where(RequestLog.model_id == model_id)
            .order_by(RequestLog.id.asc())
        )
        return list(result.scalars().all())


async def _usage_events_for_model(harness, *, model_id: str) -> list[UsageRequestEvent]:
    async with harness.db_session() as db:
        result = await db.execute(
            select(UsageRequestEvent)
            .where(UsageRequestEvent.model_id == model_id)
            .order_by(UsageRequestEvent.id.asc())
        )
        return list(result.scalars().all())


async def _loadbalance_events_for_connection(
    harness,
    *,
    connection_id: int,
) -> list[LoadbalanceEvent]:
    async with harness.db_session() as db:
        result = await db.execute(
            select(LoadbalanceEvent)
            .where(LoadbalanceEvent.connection_id == connection_id)
            .order_by(LoadbalanceEvent.id.asc())
        )
        return list(result.scalars().all())


def test_proxy_execution_smoke_successful_openai_request() -> None:
    async def run() -> None:
        suffix = uuid4().hex[:8]
        endpoint_api_key = f"success-endpoint-key-{suffix}"
        proxy_model_id = f"proxy-execution-success-{suffix}"
        target_model_id = f"target-execution-success-{suffix}"
        upstream = DeterministicUpstreamClient()
        upstream.queue_json(
            {
                "id": f"chatcmpl-success-{suffix}",
                "choices": [
                    {
                        "finish_reason": "stop",
                        "index": 0,
                        "message": {"content": "ok", "role": "assistant"},
                    }
                ],
                "model": target_model_id,
                "object": "chat.completion",
            },
            headers={"x-request-id": f"provider-success-{suffix}"},
        )

        async with mounted_smoke_app(upstream=upstream) as harness:
            async with harness.db_session() as db:
                graph = await _seed_active_proxy_route_graph(
                    db,
                    endpoint_api_key=endpoint_api_key,
                    endpoint_base_url=f"https://success-{suffix}.invalid/openai",
                    proxy_model_id=proxy_model_id,
                    target_model_id=target_model_id,
                    vendor_key=f"success-vendor-{suffix}",
                    vendor_name=f"Success Vendor {suffix}",
                )
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    commit=False,
                    name=f"runtime-success-{suffix}",
                )
                await db.commit()

            request_payload = {
                "messages": [{"content": "proxy success smoke", "role": "user"}],
                "model": graph.proxy_model.model_id,
            }
            harness.upstream.built_requests.clear()

            response = await harness.runtime_request(
                "POST",
                "/v1/chat/completions",
                json=request_payload,
                proxy_key=runtime_proxy_key,
            )

            assert response.status_code == 200
            assert response.json()["id"] == f"chatcmpl-success-{suffix}"
            assert len(harness.upstream.built_requests) == 1

            upstream_request = harness.upstream.built_requests[0]
            assert str(upstream_request.url).rstrip("?") == (
                f"{graph.endpoint.base_url}/v1/chat/completions"
            )
            assert upstream_request.headers["authorization"] == (
                f"Bearer {endpoint_api_key}"
            )
            assert (
                runtime_proxy_key.raw_key
                not in upstream_request.headers["authorization"]
            )

            upstream_body = _request_json(upstream_request)
            assert upstream_body["model"] == graph.target_model.model_id
            assert upstream_body["messages"] == request_payload["messages"]

            request_logs = await _request_logs_for_model(
                harness,
                model_id=graph.proxy_model.model_id,
            )
            usage_events = await _usage_events_for_model(
                harness,
                model_id=graph.proxy_model.model_id,
            )

            assert len(request_logs) == 1
            assert request_logs[0].connection_id == graph.connection.id
            assert (
                request_logs[0].resolved_target_model_id == graph.target_model.model_id
            )
            assert request_logs[0].proxy_api_key_name_snapshot == runtime_proxy_key.name
            assert request_logs[0].status_code == 200

            assert len(usage_events) == 1
            assert usage_events[0].attempt_count == 1
            assert usage_events[0].connection_id == graph.connection.id
            assert (
                usage_events[0].resolved_target_model_id == graph.target_model.model_id
            )
            assert usage_events[0].proxy_api_key_name_snapshot == runtime_proxy_key.name

    asyncio.run(run())


def test_proxy_execution_smoke_failover_primary_failure_fallback_success() -> None:
    async def run() -> None:
        suffix = uuid4().hex[:8]
        primary_endpoint_api_key = f"primary-endpoint-key-{suffix}"
        fallback_endpoint_api_key = f"fallback-endpoint-key-{suffix}"
        proxy_model_id = f"proxy-execution-failover-{suffix}"
        target_model_id = f"target-execution-failover-{suffix}"
        upstream = DeterministicUpstreamClient()
        upstream.queue_json(
            {"error": {"message": "primary unavailable"}},
            status_code=503,
            headers={"x-request-id": f"provider-primary-{suffix}"},
        )
        upstream.queue_json(
            {
                "id": f"chatcmpl-failover-{suffix}",
                "choices": [
                    {
                        "finish_reason": "stop",
                        "index": 0,
                        "message": {"content": "fallback ok", "role": "assistant"},
                    }
                ],
                "model": target_model_id,
                "object": "chat.completion",
            },
            headers={"x-request-id": f"provider-fallback-{suffix}"},
        )

        async with mounted_smoke_app(upstream=upstream) as harness:
            async with harness.db_session() as db:
                graph = await _seed_active_proxy_route_graph(
                    db,
                    endpoint_api_key=primary_endpoint_api_key,
                    endpoint_base_url=f"https://primary-{suffix}.invalid/openai",
                    proxy_model_id=proxy_model_id,
                    target_model_id=target_model_id,
                    vendor_key=f"failover-vendor-{suffix}",
                    vendor_name=f"Failover Vendor {suffix}",
                )
                fallback_endpoint = await seed_endpoint(
                    db,
                    api_key=fallback_endpoint_api_key,
                    base_url=f"https://fallback-{suffix}.invalid/openai",
                    profile_id=graph.profile.id,
                    position=2,
                )
                fallback_connection = await seed_connection(
                    db,
                    endpoint_id=fallback_endpoint.id,
                    model_config_id=graph.target_model.id,
                    priority=1,
                    profile_id=graph.profile.id,
                )
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    commit=False,
                    name=f"runtime-failover-{suffix}",
                )
                await db.commit()

            request_payload = {
                "messages": [{"content": "proxy failover smoke", "role": "user"}],
                "model": graph.proxy_model.model_id,
            }
            harness.upstream.built_requests.clear()

            response = await harness.runtime_request(
                "POST",
                "/v1/chat/completions",
                json=request_payload,
                proxy_key=runtime_proxy_key,
            )

            assert response.status_code == 200
            assert response.json()["id"] == f"chatcmpl-failover-{suffix}"
            assert len(harness.upstream.built_requests) == 2

            primary_request, fallback_request = harness.upstream.built_requests
            assert str(primary_request.url).rstrip("?") == (
                f"{graph.endpoint.base_url}/v1/chat/completions"
            )
            assert str(fallback_request.url).rstrip("?") == (
                f"{fallback_endpoint.base_url}/v1/chat/completions"
            )
            assert primary_request.headers["authorization"] == (
                f"Bearer {primary_endpoint_api_key}"
            )
            assert fallback_request.headers["authorization"] == (
                f"Bearer {fallback_endpoint_api_key}"
            )
            assert (
                _request_json(primary_request)["model"] == graph.target_model.model_id
            )
            assert (
                _request_json(fallback_request)["model"] == graph.target_model.model_id
            )

            await harness.wait_for_background_tasks()

            request_logs = await _request_logs_for_model(
                harness,
                model_id=graph.proxy_model.model_id,
            )
            usage_events = await _usage_events_for_model(
                harness,
                model_id=graph.proxy_model.model_id,
            )
            loadbalance_events = await _loadbalance_events_for_connection(
                harness,
                connection_id=graph.connection.id,
            )

            assert len(request_logs) == 2
            assert [request_log.attempt_number for request_log in request_logs] == [
                1,
                2,
            ]
            assert [request_log.connection_id for request_log in request_logs] == [
                graph.connection.id,
                fallback_connection.id,
            ]
            assert [request_log.status_code for request_log in request_logs] == [
                503,
                200,
            ]
            assert (
                len({request_log.ingress_request_id for request_log in request_logs})
                == 1
            )
            assert all(
                request_log.resolved_target_model_id == graph.target_model.model_id
                for request_log in request_logs
            )

            assert len(usage_events) == 1
            assert usage_events[0].attempt_count == 2
            assert usage_events[0].connection_id == fallback_connection.id
            assert (
                usage_events[0].resolved_target_model_id == graph.target_model.model_id
            )
            assert usage_events[0].status_code == 200

            assert any(
                event.event_type in {"not_opened", "opened", "extended"}
                and event.failure_kind == "transient_http"
                and event.model_id == graph.proxy_model.model_id
                for event in loadbalance_events
            )

    asyncio.run(run())


def test_proxy_execution_smoke_streaming_finalizes_only_after_drain() -> None:
    async def run() -> None:
        suffix = uuid4().hex[:8]
        endpoint_api_key = f"stream-endpoint-key-{suffix}"
        proxy_model_id = f"proxy-execution-stream-{suffix}"
        target_model_id = f"target-execution-stream-{suffix}"
        first_chunk = b'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n'
        trailing_chunks = [
            b'data: {"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}\n\n',
            b"data: [DONE]\n\n",
        ]
        stream_chunks = [first_chunk, *trailing_chunks]
        blocking_stream = BlockingAsyncByteStream(first_chunk, trailing_chunks)
        upstream = DeterministicUpstreamClient()
        upstream.queue_response(
            httpx.Response(
                200,
                headers={
                    "content-type": "text/event-stream",
                    "x-request-id": f"provider-stream-{suffix}",
                },
                stream=blocking_stream,
            )
        )

        async with mounted_smoke_app(upstream=upstream) as harness:
            async with harness.db_session() as db:
                graph = await _seed_active_proxy_route_graph(
                    db,
                    endpoint_api_key=endpoint_api_key,
                    endpoint_base_url=f"https://stream-{suffix}.invalid/openai",
                    proxy_model_id=proxy_model_id,
                    target_model_id=target_model_id,
                    vendor_key=f"stream-vendor-{suffix}",
                    vendor_name=f"Stream Vendor {suffix}",
                )
                runtime_proxy_key = await seed_runtime_proxy_key(
                    db,
                    commit=False,
                    name=f"runtime-stream-{suffix}",
                )
                await db.commit()

            request_payload = {
                "messages": [{"content": "proxy streaming smoke", "role": "user"}],
                "model": graph.proxy_model.model_id,
                "stream": True,
            }
            harness.upstream.built_requests.clear()

            response_task = asyncio.create_task(
                harness.runtime_request(
                    "POST",
                    "/v1/chat/completions",
                    json=request_payload,
                    proxy_key=runtime_proxy_key,
                )
            )

            try:
                await _wait_for_upstream_request_count(
                    harness.upstream, expected_count=1
                )
                assert response_task.done() is False

                upstream_request = harness.upstream.built_requests[0]
                upstream_body = _request_json(upstream_request)
                assert str(upstream_request.url).rstrip("?") == (
                    f"{graph.endpoint.base_url}/v1/chat/completions"
                )
                assert upstream_request.headers["authorization"] == (
                    f"Bearer {endpoint_api_key}"
                )
                assert upstream_body["model"] == graph.target_model.model_id
                assert upstream_body["stream"] is True
                assert upstream_body["stream_options"] == {"include_usage": True}

                assert (
                    await _request_logs_for_model(
                        harness,
                        model_id=graph.proxy_model.model_id,
                    )
                    == []
                )
                assert (
                    await _usage_events_for_model(
                        harness,
                        model_id=graph.proxy_model.model_id,
                    )
                    == []
                )

                blocking_stream.release()
                response = await response_task
                assert response.status_code == 200
                assert response.headers["content-type"].startswith("text/event-stream")
                assert response.content == b"".join(stream_chunks)
            finally:
                blocking_stream.release()
                if not response_task.done():
                    await response_task

            await harness.wait_for_background_tasks()

            request_logs = await _request_logs_for_model(
                harness,
                model_id=graph.proxy_model.model_id,
            )
            usage_events = await _usage_events_for_model(
                harness,
                model_id=graph.proxy_model.model_id,
            )

            assert len(request_logs) == 1
            assert request_logs[0].is_stream is True
            assert request_logs[0].connection_id == graph.connection.id
            assert (
                request_logs[0].resolved_target_model_id == graph.target_model.model_id
            )
            assert request_logs[0].completion_duration_ms is not None
            assert request_logs[0].ttft_ms is not None
            assert request_logs[0].total_tokens == 5

            assert len(usage_events) == 1
            assert usage_events[0].attempt_count == 1
            assert usage_events[0].connection_id == graph.connection.id
            assert usage_events[0].completion_duration_ms is not None
            assert usage_events[0].ttft_ms is not None
            assert usage_events[0].total_tokens == 5

    asyncio.run(run())
