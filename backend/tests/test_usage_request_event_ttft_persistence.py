from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any, cast

import httpx
from fastapi import HTTPException

import app.routers.proxy_domains.attempt_handlers as attempt_handlers_module
import app.routers.proxy_domains.attempt_execution as attempt_execution_module
import app.routers.proxy_domains.attempt_streaming as attempt_streaming_module
from app.routers.proxy_domains.attempt_execution import execute_proxy_attempts
from app.routers.proxy_domains.attempt_handlers import handle_buffered_attempt
from app.routers.proxy_domains.attempt_streaming import build_streaming_response


def _build_cost_fields(connection, status_code, tokens=None) -> dict[str, object]:
    return {
        "pricing_snapshot_reasoning": "0.000000",
    }


def _make_state(
    *,
    request_started_at_monotonic: float = 10.0,
    completion_duration_ms: int | None = 1000,
) -> SimpleNamespace:
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
        is_streaming=True,
        method="POST",
        model_config=SimpleNamespace(model_id="gpt-5.4", connections=[]),
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
        request_started_at_monotonic=request_started_at_monotonic,
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
            name="Demo connection",
        ),
        description="Demo connection",
        endpoint_body=b'{"stream": true}',
        headers={"User-Agent": "upstream-agent", "x-client-request-id": "client-1"},
        limiter_lease_token=None,
        limiter_lease_ttl_seconds=None,
        upstream_url="https://demo.invalid/v1/chat/completions",
    )


def _make_streaming_deps(
    *,
    request_log_kwargs: dict[str, object],
    usage_event_kwargs: dict[str, object],
) -> SimpleNamespace:
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

    async def record_connection_failure_fn(*args, **kwargs):
        return None

    async def release_connection_lease_fn(*args, **kwargs):
        return True

    async def heartbeat_connection_lease_fn(*args, **kwargs):
        return True

    return SimpleNamespace(
        heartbeat_connection_lease_fn=heartbeat_connection_lease_fn,
        log_request_fn=log_request_fn,
        log_usage_request_event_fn=log_usage_request_event_fn,
        record_audit_log_fn=record_audit_log_fn,
        record_connection_failure_fn=record_connection_failure_fn,
        record_connection_recovery_fn=record_connection_recovery_fn,
        release_connection_lease_fn=release_connection_lease_fn,
    )


async def _iterate_chunks(*chunks: bytes):
    for chunk in chunks:
        yield chunk


async def _raise_after_chunks(*chunks: bytes):
    for chunk in chunks:
        yield chunk
    raise RuntimeError("stream interrupted before first content output")


def test_ttft_classifier_ignores_protocol_frames_and_accepts_content_deltas() -> None:
    assert (
        attempt_streaming_module._is_ttft_eligible_sse_event(
            b'data: {"type":"message_start"}\n'
        )
        is False
    )
    assert (
        attempt_streaming_module._is_ttft_eligible_sse_event(
            b'data: {"usage":{"output_tokens":1}}\n'
        )
        is False
    )
    assert (
        attempt_streaming_module._is_ttft_eligible_sse_event(
            b'data: {"choices":[{"delta":{"content":"Hello"}}]}\n'
        )
        is True
    )
    assert (
        attempt_streaming_module._is_ttft_eligible_sse_event(
            b'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\\"city\\":\\"SF\\"}"}}]}}]}\n'
        )
        is True
    )
    assert (
        attempt_streaming_module._is_ttft_eligible_sse_event(
            b'data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"step 1"}}\n'
        )
        is True
    )
    assert (
        attempt_streaming_module._is_ttft_eligible_sse_event(
            b'data: {"type":"response.function_call_arguments.delta","delta":"{\\"a\\":1}"}\n'
        )
        is True
    )


def test_completed_stream_persists_ttft(monkeypatch) -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}
        deps = _make_streaming_deps(
            request_log_kwargs=request_log_kwargs,
            usage_event_kwargs=usage_event_kwargs,
        )
        state = _make_state(completion_duration_ms=1000)
        target = _make_target()
        upstream_resp = SimpleNamespace(
            headers={"content-type": "text/event-stream", "x-request-id": "provider-2"},
            status_code=200,
            aclose=lambda: asyncio.sleep(0),
        )

        monkeypatch.setattr(
            attempt_streaming_module,
            "_compute_ttft_ms",
            lambda *, request_started_at_monotonic: 250,
        )

        prepared_response = build_streaming_response(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            upstream_resp=cast(Any, upstream_resp),
            remaining_stream_iter=_iterate_chunks(
                b'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
                b"data: [DONE]\n\n",
            ),
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
            b'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
            b"data: [DONE]\n\n",
        ]
        assert request_log_kwargs["response_time_ms"] == 250
        assert request_log_kwargs["ttft_ms"] == 250
        assert request_log_kwargs["completion_duration_ms"] == 1000
        assert request_log_kwargs["pricing_snapshot_reasoning"] == "0.000000"
        assert usage_event_kwargs["response_time_ms"] == 250
        assert usage_event_kwargs["ttft_ms"] == 250
        assert usage_event_kwargs["completion_duration_ms"] == 1000
        assert usage_event_kwargs["pricing_snapshot_reasoning"] == "0.000000"

    asyncio.run(run())


def test_responses_tool_call_delta_persists_ttft(monkeypatch) -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}
        deps = _make_streaming_deps(
            request_log_kwargs=request_log_kwargs,
            usage_event_kwargs=usage_event_kwargs,
        )
        state = _make_state(completion_duration_ms=1000)
        state.request_path = "/v1/responses"
        target = _make_target()
        target.upstream_url = "https://demo.invalid/v1/responses"
        upstream_resp = SimpleNamespace(
            headers={
                "content-type": "text/event-stream",
                "x-request-id": "provider-responses",
            },
            status_code=200,
            aclose=lambda: asyncio.sleep(0),
        )

        monkeypatch.setattr(
            attempt_streaming_module,
            "_compute_ttft_ms",
            lambda *, request_started_at_monotonic: 275,
        )

        prepared_response = build_streaming_response(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            upstream_resp=cast(Any, upstream_resp),
            remaining_stream_iter=_iterate_chunks(
                b'data: {"type":"response.function_call_arguments.delta","delta":"{\\"a\\":1}"}\n\n',
                b"data: [DONE]\n\n",
            ),
            response_headers={
                "content-type": "text/event-stream",
                "x-request-id": "provider-responses",
            },
            elapsed_ms=250,
            first_chunk=b'data: {"type":"response.created"}\n\n',
        )

        response = cast(Any, await prepared_response.commit_response_fn(1))
        chunks = [chunk async for chunk in response.body_iterator]

        assert chunks == [
            b'data: {"type":"response.created"}\n\n',
            b'data: {"type":"response.function_call_arguments.delta","delta":"{\\"a\\":1}"}\n\n',
            b"data: [DONE]\n\n",
        ]
        assert request_log_kwargs["ttft_ms"] == 275
        assert request_log_kwargs["completion_duration_ms"] == 1000
        assert usage_event_kwargs["ttft_ms"] == 275
        assert usage_event_kwargs["completion_duration_ms"] == 1000

    asyncio.run(run())


def test_interrupted_stream_after_first_output_persists_ttft_without_completion_duration(
    monkeypatch,
) -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}

        def unexpected_completion_time() -> int:
            raise AssertionError(
                "completion duration should not be computed for interrupted streams"
            )

        deps = _make_streaming_deps(
            request_log_kwargs=request_log_kwargs,
            usage_event_kwargs=usage_event_kwargs,
        )
        state = _make_state(completion_duration_ms=None)
        state.completion_duration_ms = unexpected_completion_time
        target = _make_target()
        upstream_resp = SimpleNamespace(
            headers={"content-type": "text/event-stream", "x-request-id": "provider-3"},
            status_code=200,
            aclose=lambda: asyncio.sleep(0),
        )

        monkeypatch.setattr(
            attempt_streaming_module,
            "_compute_ttft_ms",
            lambda *, request_started_at_monotonic: 300,
        )

        prepared_response = build_streaming_response(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            upstream_resp=cast(Any, upstream_resp),
            remaining_stream_iter=_iterate_chunks(
                b'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\\"city\\":\\"SF\\"}"}}]}}]}\n\n',
                b'data: {"choices":[{"delta":{"content":"ignored after close"}}]}\n\n',
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
        assert await body_iterator.__anext__() == b'data: {"type":"message_start"}\n\n'
        assert await body_iterator.__anext__() == (
            b'data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\\"city\\":\\"SF\\"}"}}]}}]}\n\n'
        )

        await body_iterator.aclose()

        assert request_log_kwargs["ttft_ms"] == 300
        assert request_log_kwargs["completion_duration_ms"] is None
        assert usage_event_kwargs["ttft_ms"] == 300
        assert usage_event_kwargs["completion_duration_ms"] is None

    asyncio.run(run())


def test_buffered_request_keeps_ttft_null(monkeypatch) -> None:
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
            return 201

        async def log_usage_request_event_fn(**kwargs):
            usage_event_kwargs.update(kwargs)
            return 202

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
        assert request_log_kwargs["ttft_ms"] is None
        assert request_log_kwargs["completion_duration_ms"] == 1000
        assert usage_event_kwargs["ttft_ms"] is None
        assert usage_event_kwargs["completion_duration_ms"] == 1000

    asyncio.run(run())


def test_fail_before_first_output_keeps_ttft_null(monkeypatch) -> None:
    async def run() -> None:
        request_log_kwargs: dict[str, object] = {}
        usage_event_kwargs: dict[str, object] = {}

        def unexpected_ttft_capture() -> float:
            raise AssertionError("TTFT should not be captured for protocol-only output")

        def unexpected_completion_time() -> int:
            raise AssertionError(
                "completion duration should not be computed before first output"
            )

        deps = _make_streaming_deps(
            request_log_kwargs=request_log_kwargs,
            usage_event_kwargs=usage_event_kwargs,
        )
        state = _make_state(completion_duration_ms=None)
        state.completion_duration_ms = unexpected_completion_time
        target = _make_target()
        upstream_resp = SimpleNamespace(
            headers={"content-type": "text/event-stream", "x-request-id": "provider-4"},
            status_code=200,
            aclose=lambda: asyncio.sleep(0),
        )

        monkeypatch.setattr(
            attempt_streaming_module,
            "_compute_ttft_ms",
            lambda *, request_started_at_monotonic: unexpected_ttft_capture(),
        )

        prepared_response = build_streaming_response(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            upstream_resp=cast(Any, upstream_resp),
            remaining_stream_iter=_raise_after_chunks(),
            response_headers={
                "content-type": "text/event-stream",
                "x-request-id": "provider-4",
            },
            elapsed_ms=250,
            first_chunk=b'data: {"type":"message_start"}\n\n',
        )

        response = cast(Any, await prepared_response.commit_response_fn(1))
        chunks = [chunk async for chunk in response.body_iterator]

        assert chunks == [b'data: {"type":"message_start"}\n\n']
        assert request_log_kwargs["ttft_ms"] is None
        assert request_log_kwargs["completion_duration_ms"] is None
        assert usage_event_kwargs["ttft_ms"] is None
        assert usage_event_kwargs["completion_duration_ms"] is None

    asyncio.run(run())


def test_terminal_all_attempts_failed_usage_event_keeps_ttft_null(monkeypatch) -> None:
    async def run() -> None:
        usage_event_kwargs: dict[str, object] = {}
        connection = SimpleNamespace(
            id=7,
            endpoint_id=9,
            endpoint_rel=SimpleNamespace(base_url="https://demo.invalid"),
            name="Demo connection",
            priority=0,
        )
        setup = SimpleNamespace(
            api_family="openai",
            audit_capture_bodies=False,
            audit_enabled=False,
            build_cost_fields=_build_cost_fields,
            caller_user_agent="caller-agent",
            client=None,
            client_headers={},
            blocklist_rules=(),
            effective_request_path="/v1/chat/completions",
            failover_policy=SimpleNamespace(
                hedge_delay_ms=0,
                hedge_enabled=False,
                failover_cooldown_seconds=30,
                failover_recovery_enabled=False,
                failover_status_codes=[429, 500],
                legacy_strategy_type="round_robin",
                max_additional_attempts=0,
                strategy_type="legacy",
            ),
            ingress_request_id="req-terminal",
            initial_candidates=[
                SimpleNamespace(connection=connection, probe_eligible=False)
            ],
            is_streaming=True,
            method="POST",
            model_config=SimpleNamespace(model_id="gpt-5.4", connections=[connection]),
            model_id="gpt-5.4",
            proxy_api_key_id=12,
            proxy_api_key_name="Operator key",
            request_compressed=False,
            rewritten_body=b'{"stream": true}',
            resolved_target_model_id="gpt-5.4-mini",
            vendor_id=None,
            vendor_key=None,
            vendor_name=None,
        )

        async def fake_execute_planned_attempts(**kwargs):
            outcome = await kwargs["run_attempt_fn"](kwargs["initial_candidates"][0], 1)
            return SimpleNamespace(
                response=None,
                attempted_any_endpoint=outcome.attempted,
                limiter_denied_any_endpoint=outcome.limiter_denied,
                last_error=outcome.error_detail,
                attempt_count=1,
            )

        async def fake_handle_streaming_attempt(**kwargs):
            return SimpleNamespace(
                attempted=True,
                accepted=False,
                limiter_denied=False,
                prepared_response=None,
                error_detail="upstream stream completed before first chunk",
            )

        async def clear_connection_state_fn(*args, **kwargs):
            return True

        async def claim_probe_eligible_fn(*args, **kwargs):
            return None

        async def log_request_fn(**kwargs):
            return None

        async def log_usage_request_event_fn(**kwargs):
            usage_event_kwargs.update(kwargs)
            return 301

        async def record_connection_failure_fn(*args, **kwargs):
            return None

        async def record_connection_recovery_fn(*args, **kwargs):
            return None

        async def record_audit_log_fn(**kwargs):
            return None

        async def release_connection_lease_fn(*args, **kwargs):
            return True

        monkeypatch.setattr(
            attempt_execution_module,
            "execute_planned_attempts",
            fake_execute_planned_attempts,
        )
        monkeypatch.setattr(
            attempt_execution_module,
            "handle_streaming_attempt",
            fake_handle_streaming_attempt,
        )
        monkeypatch.setattr(
            attempt_execution_module.ProxyRequestState,
            "completion_duration_ms",
            lambda self: 1000,
        )

        deps = SimpleNamespace(
            acquire_connection_limit_fn=None,
            build_upstream_headers_fn=lambda *args, **kwargs: {},
            build_upstream_url_fn=lambda *args,
            **kwargs: "https://demo.invalid/v1/chat/completions",
            claim_probe_eligible_fn=claim_probe_eligible_fn,
            clear_connection_state_fn=clear_connection_state_fn,
            filter_response_headers_fn=lambda headers, was_requested_compressed: dict(
                headers
            ),
            heartbeat_connection_lease_fn=None,
            log_request_fn=log_request_fn,
            log_usage_request_event_fn=log_usage_request_event_fn,
            proxy_request_fn=None,
            proxy_stream_fn=None,
            record_audit_log_fn=record_audit_log_fn,
            record_connection_failure_fn=record_connection_failure_fn,
            record_connection_recovery_fn=record_connection_recovery_fn,
            release_connection_lease_fn=release_connection_lease_fn,
            should_failover_fn=lambda status_code, failover_status_codes: False,
        )

        try:
            await execute_proxy_attempts(
                db=cast(Any, None),
                endpoint_is_active_now_fn=lambda *args, **kwargs: asyncio.sleep(
                    0, result=True
                ),
                request_path="/v1/chat/completions",
                request_query=None,
                profile_id=1,
                setup=cast(Any, setup),
                deps=cast(Any, deps),
            )
        except HTTPException as exc:
            assert exc.status_code == 502
        else:
            raise AssertionError(
                "Expected execute_proxy_attempts to raise HTTPException"
            )

        assert usage_event_kwargs["ttft_ms"] is None
        assert usage_event_kwargs["completion_duration_ms"] == 1000
        assert usage_event_kwargs["pricing_snapshot_reasoning"] == "0.000000"

    asyncio.run(run())
