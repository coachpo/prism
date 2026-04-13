from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any, cast

import httpx

from app.routers.proxy_domains import attempt_handlers as attempt_handlers_module
from app.routers.proxy_domains.attempt_handlers import handle_buffered_attempt
from app.routers.proxy_domains.attempt_streaming import build_streaming_response


def _build_cost_fields(connection, status_code, tokens=None) -> dict[str, object]:
    return {
        "pricing_snapshot_reasoning": "0.000000",
    }


def _make_state(*, completion_duration_ms: int | None = 1000) -> SimpleNamespace:
    setup = SimpleNamespace(
        audit_capture_bodies=False,
        audit_enabled=False,
        api_family="openai",
        build_cost_fields=_build_cost_fields,
        caller_user_agent="caller-agent",
        failover_policy=SimpleNamespace(
            failover_cooldown_seconds=30,
            failover_recovery_enabled=False,
            failover_status_codes=[429, 500],
        ),
        ingress_request_id="req-runtime",
        method="POST",
        model_id="gpt-5.4",
        proxy_api_key_id=12,
        proxy_api_key_name="Operator key",
        request_compressed=False,
        resolved_target_model_id="gpt-5.4-mini",
        vendor_id=None,
        vendor_key=None,
        vendor_name=None,
    )
    state = SimpleNamespace(
        profile_id=1,
        request_path="/v1/chat/completions",
        setup=cast(Any, setup),
    )
    if completion_duration_ms is not None:
        state.completion_duration_ms = lambda: completion_duration_ms
    return state


def _make_target(*, attempt_number: int = 1) -> SimpleNamespace:
    return SimpleNamespace(
        attempt_number=attempt_number,
        connection=SimpleNamespace(
            id=3,
            endpoint_id=2,
            endpoint_rel=SimpleNamespace(base_url="https://demo.invalid"),
        ),
        description="Demo connection",
        endpoint_body=b'{"stream": true}',
        headers={"User-Agent": "upstream-agent", "x-client-request-id": "client-1"},
        limiter_lease_token=None,
        limiter_lease_ttl_seconds=None,
        upstream_url="https://demo.invalid/v1/chat/completions",
    )


async def _iterate_chunks(*chunks: bytes):
    for chunk in chunks:
        yield chunk


def test_buffered_request_scoped_completion_duration(monkeypatch) -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}

        async def proxy_request_fn(*args, **kwargs):
            return httpx.Response(
                status_code=200,
                headers={
                    "content-type": "application/json",
                    "x-request-id": "provider-1",
                },
                content=b'{"id": "resp_123"}',
            )

        async def log_request_fn(**kwargs):
            request_log_kwargs.update(kwargs)
            return 101

        async def log_usage_request_event_fn(**kwargs):
            usage_event_kwargs.update(kwargs)
            return 102

        async def record_audit_log_fn(**kwargs):
            return None

        async def record_connection_recovery_fn(*args, **kwargs):
            return None

        async def release_connection_lease_fn(*args, **kwargs):
            return True

        deps = SimpleNamespace(
            filter_response_headers_fn=lambda headers, was_requested_compressed: dict(
                headers
            ),
            log_request_fn=log_request_fn,
            log_usage_request_event_fn=log_usage_request_event_fn,
            proxy_request_fn=proxy_request_fn,
            record_audit_log_fn=record_audit_log_fn,
            record_connection_recovery_fn=record_connection_recovery_fn,
            release_connection_lease_fn=release_connection_lease_fn,
            should_failover_fn=lambda status_code, failover_status_codes: False,
        )
        state = _make_state(completion_duration_ms=1000)
        target = _make_target()

        monkeypatch.setattr(attempt_handlers_module.time, "monotonic", lambda: 10.75)

        result = await handle_buffered_attempt(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            client=cast(Any, None),
            start_time=10.5,
        )

        assert result.prepared_response is not None
        response = cast(Any, await result.prepared_response.commit_response_fn(2))

        assert response.status_code == 200
        assert request_log_kwargs["response_time_ms"] == 250
        assert request_log_kwargs["completion_duration_ms"] == 1000
        assert request_log_kwargs["pricing_snapshot_reasoning"] == "0.000000"
        assert usage_event_kwargs["response_time_ms"] == 250
        assert usage_event_kwargs["completion_duration_ms"] == 1000
        assert usage_event_kwargs["pricing_snapshot_reasoning"] == "0.000000"

    asyncio.run(run())


def test_completed_stream_uses_full_completion_duration(monkeypatch) -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}

        async def log_request_fn(**kwargs):
            request_log_kwargs.update(kwargs)
            return 201

        async def log_usage_request_event_fn(**kwargs):
            usage_event_kwargs.update(kwargs)
            return 202

        async def record_audit_log_fn(**kwargs):
            return None

        async def record_connection_recovery_fn(*args, **kwargs):
            return None

        async def record_connection_failure_fn(*args, **kwargs):
            return None

        async def release_connection_lease_fn(*args, **kwargs):
            return True

        async def heartbeat_connection_lease_fn(*args, **kwargs):
            return True

        async def close_response() -> None:
            return None

        deps = SimpleNamespace(
            heartbeat_connection_lease_fn=heartbeat_connection_lease_fn,
            log_request_fn=log_request_fn,
            log_usage_request_event_fn=log_usage_request_event_fn,
            record_audit_log_fn=record_audit_log_fn,
            record_connection_failure_fn=record_connection_failure_fn,
            record_connection_recovery_fn=record_connection_recovery_fn,
            release_connection_lease_fn=release_connection_lease_fn,
        )
        state = _make_state(completion_duration_ms=1000)
        target = _make_target()
        upstream_resp = SimpleNamespace(
            headers={"content-type": "text/event-stream", "x-request-id": "provider-2"},
            status_code=200,
            aclose=close_response,
        )

        prepared_response = build_streaming_response(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            upstream_resp=cast(Any, upstream_resp),
            remaining_stream_iter=_iterate_chunks(b"data: [DONE]\n\n"),
            response_headers={
                "content-type": "text/event-stream",
                "x-request-id": "provider-2",
            },
            elapsed_ms=250,
            first_chunk=b'data: {"type":"message_start"}\n\n',
        )

        response = cast(Any, await prepared_response.commit_response_fn(2))
        chunks = [chunk async for chunk in response.body_iterator]

        assert chunks == [
            b'data: {"type":"message_start"}\n\n',
            b"data: [DONE]\n\n",
        ]
        assert request_log_kwargs["response_time_ms"] == 250
        assert request_log_kwargs["completion_duration_ms"] == 1000
        assert request_log_kwargs["pricing_snapshot_reasoning"] == "0.000000"
        assert usage_event_kwargs["response_time_ms"] == 250
        assert usage_event_kwargs["completion_duration_ms"] == 1000
        assert usage_event_kwargs["pricing_snapshot_reasoning"] == "0.000000"

    asyncio.run(run())


def test_incomplete_stream_completion_duration_null(monkeypatch) -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}

        async def log_request_fn(**kwargs):
            request_log_kwargs.update(kwargs)
            return 301

        async def log_usage_request_event_fn(**kwargs):
            usage_event_kwargs.update(kwargs)
            return 302

        async def record_audit_log_fn(**kwargs):
            return None

        async def record_connection_recovery_fn(*args, **kwargs):
            return None

        async def record_connection_failure_fn(*args, **kwargs):
            return None

        async def release_connection_lease_fn(*args, **kwargs):
            return True

        async def heartbeat_connection_lease_fn(*args, **kwargs):
            return True

        async def close_response() -> None:
            return None

        def unexpected_completion_time() -> int:
            raise AssertionError(
                "completion duration should not be computed for incomplete streams"
            )

        deps = SimpleNamespace(
            heartbeat_connection_lease_fn=heartbeat_connection_lease_fn,
            log_request_fn=log_request_fn,
            log_usage_request_event_fn=log_usage_request_event_fn,
            record_audit_log_fn=record_audit_log_fn,
            record_connection_failure_fn=record_connection_failure_fn,
            record_connection_recovery_fn=record_connection_recovery_fn,
            release_connection_lease_fn=release_connection_lease_fn,
        )
        state = _make_state(completion_duration_ms=None)
        state.completion_duration_ms = unexpected_completion_time
        target = _make_target()
        upstream_resp = SimpleNamespace(
            headers={"content-type": "text/event-stream", "x-request-id": "provider-3"},
            status_code=200,
            aclose=close_response,
        )

        prepared_response = build_streaming_response(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            upstream_resp=cast(Any, upstream_resp),
            remaining_stream_iter=_iterate_chunks(
                b'data: {"type":"message_delta"}\n\n'
            ),
            response_headers={
                "content-type": "text/event-stream",
                "x-request-id": "provider-3",
            },
            elapsed_ms=250,
            first_chunk=b'data: {"type":"message_start"}\n\n',
        )

        response = cast(Any, await prepared_response.commit_response_fn(1))
        body_iterator = cast(Any, response.body_iterator)
        first_chunk = await body_iterator.__anext__()

        assert first_chunk == b'data: {"type":"message_start"}\n\n'

        await body_iterator.aclose()

        assert request_log_kwargs["response_time_ms"] == 250
        assert request_log_kwargs["completion_duration_ms"] is None
        assert request_log_kwargs["pricing_snapshot_reasoning"] == "0.000000"
        assert usage_event_kwargs["response_time_ms"] == 250
        assert usage_event_kwargs["completion_duration_ms"] is None
        assert usage_event_kwargs["pricing_snapshot_reasoning"] == "0.000000"

    asyncio.run(run())
