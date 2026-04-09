from __future__ import annotations

import asyncio
import time
from types import SimpleNamespace

import httpx

from app.routers.proxy_domains.attempt_handlers import handle_streaming_attempt
import app.routers.proxy_domains.attempt_streaming as attempt_streaming


class DelayedStream(httpx.AsyncByteStream):
    def __init__(self, schedule: list[tuple[float, bytes]]) -> None:
        self._schedule = list(schedule)

    async def __aiter__(self):
        for delay_seconds, chunk in self._schedule:
            if delay_seconds:
                await asyncio.sleep(delay_seconds)
            yield chunk

    async def aclose(self) -> None:
        return None


async def _noop(*args, **kwargs):
    return None


async def _noop_bool(*args, **kwargs):
    return True


def _make_state(*, open_ms: int, precommit_ms: int, hard_cap_ms: int | None):
    policy = SimpleNamespace(
        attempt_open_timeout_ms=open_ms,
        buffered_total_timeout_ms=30_000,
        stream_precommit_timeout_ms=precommit_ms,
        stream_hard_cap_timeout_ms=hard_cap_ms,
        failover_status_codes=(500, 502, 503, 504),
        failover_recovery_enabled=True,
        failover_cooldown_seconds=30.0,
    )
    setup = SimpleNamespace(
        method="POST",
        request_compressed=False,
        failover_policy=policy,
        request_deadline_at_monotonic=time.monotonic() + (precommit_ms / 1000.0),
        model_id="gpt-5.4",
        vendor_id=None,
        vendor_key=None,
        vendor_name=None,
        ingress_request_id="test-request",
        caller_user_agent=None,
        proxy_api_key_id=None,
        proxy_api_key_name=None,
        api_family="openai",
        resolved_target_model_id="gpt-5.4",
        raw_body=b'{"stream": true}',
        rewritten_body=b'{"stream": true}',
        is_streaming=True,
        audit_enabled=False,
        audit_capture_bodies=False,
        build_cost_fields=lambda connection, status_code, tokens=None: {},
    )
    return SimpleNamespace(
        profile_id=1, request_path="/v1/chat/completions", setup=setup
    )


def _make_target():
    endpoint = SimpleNamespace(
        base_url="http://demo.invalid",
        pool_timeout=5.0,
        connect_timeout=10.0,
        write_timeout=30.0,
        read_idle_timeout=120.0,
    )
    connection = SimpleNamespace(
        id=1,
        endpoint_id=1,
        endpoint_rel=endpoint,
        name="demo-connection",
    )
    return SimpleNamespace(
        attempt_number=1,
        connection=connection,
        description="demo-connection",
        endpoint_body=b'{"stream": true}',
        headers={},
        limiter_lease_token=None,
        limiter_lease_ttl_seconds=None,
        upstream_url="http://demo.invalid/v1/chat/completions",
    )


def _make_deps(stream_factory):
    async def proxy_stream_fn(
        client, method, upstream_url, headers, raw_body, timeout=None
    ):
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            request=httpx.Request(method, upstream_url),
            stream=stream_factory(),
        )

    return SimpleNamespace(
        filter_response_headers_fn=lambda headers, was_requested_compressed=False: dict(
            headers
        ),
        should_failover_fn=lambda status, codes: status in codes,
        log_request_fn=_noop,
        log_usage_request_event_fn=_noop,
        record_connection_failure_fn=_noop,
        record_connection_recovery_fn=_noop,
        record_audit_log_fn=_noop,
        release_connection_lease_fn=_noop_bool,
        heartbeat_connection_lease_fn=_noop_bool,
        proxy_stream_fn=proxy_stream_fn,
    )


async def _collect_stream(prepared_response) -> list[bytes]:
    response = await prepared_response.commit_response_fn(1)
    chunks: list[bytes] = []
    async for chunk in response.body_iterator:
        chunks.append(chunk)
    return chunks


def test_streaming_attempt_requires_first_chunk_before_acceptance(monkeypatch) -> None:
    async def run() -> None:
        monkeypatch.setattr(
            attempt_streaming,
            "build_stream_finalization_snapshot",
            lambda **kwargs: None,
        )
        monkeypatch.setattr(attempt_streaming, "await_stream_finalization", _noop)
        state = _make_state(open_ms=500, precommit_ms=1000, hard_cap_ms=1000)
        deps = _make_deps(lambda: DelayedStream([(0.05, b"data: first\n\n")]))
        target = _make_target()
        async with httpx.AsyncClient() as client:
            start_time = time.monotonic()
            result = await handle_streaming_attempt(
                deps=deps,
                state=state,
                target=target,
                client=client,
                start_time=start_time,
            )
            elapsed_ms = round((time.monotonic() - start_time) * 1000)
        assert result.accepted is True
        assert result.prepared_response is not None
        assert elapsed_ms >= 40

    asyncio.run(run())


def test_streaming_attempt_open_timeout_bounds_header_to_first_byte_wait(
    monkeypatch,
) -> None:
    async def run() -> None:
        monkeypatch.setattr(
            attempt_streaming,
            "build_stream_finalization_snapshot",
            lambda **kwargs: None,
        )
        monkeypatch.setattr(attempt_streaming, "await_stream_finalization", _noop)
        state = _make_state(open_ms=80, precommit_ms=1000, hard_cap_ms=1000)
        deps = _make_deps(lambda: DelayedStream([(0.25, b"data: first\n\n")]))
        target = _make_target()
        async with httpx.AsyncClient() as client:
            start_time = time.monotonic()
            result = await handle_streaming_attempt(
                deps=deps,
                state=state,
                target=target,
                client=client,
                start_time=start_time,
            )
            elapsed_ms = round((time.monotonic() - start_time) * 1000)
        assert result.accepted is False
        assert elapsed_ms < 200

    asyncio.run(run())


def test_streaming_precommit_budget_still_bounds_total_time_to_first_commit(
    monkeypatch,
) -> None:
    async def run() -> None:
        monkeypatch.setattr(
            attempt_streaming,
            "build_stream_finalization_snapshot",
            lambda **kwargs: None,
        )
        monkeypatch.setattr(attempt_streaming, "await_stream_finalization", _noop)
        state = _make_state(open_ms=1000, precommit_ms=80, hard_cap_ms=1000)
        deps = _make_deps(lambda: DelayedStream([(0.25, b"data: first\n\n")]))
        target = _make_target()
        async with httpx.AsyncClient() as client:
            start_time = time.monotonic()
            result = await handle_streaming_attempt(
                deps=deps,
                state=state,
                target=target,
                client=client,
                start_time=start_time,
            )
            elapsed_ms = round((time.monotonic() - start_time) * 1000)
        assert result.accepted is False
        assert elapsed_ms < 200

    asyncio.run(run())


def test_streaming_hard_cap_applies_after_commit_during_iteration_and_finalization(
    monkeypatch,
) -> None:
    async def run() -> None:
        monkeypatch.setattr(
            attempt_streaming,
            "build_stream_finalization_snapshot",
            lambda **kwargs: None,
        )
        monkeypatch.setattr(attempt_streaming, "await_stream_finalization", _noop)
        state = _make_state(open_ms=500, precommit_ms=1000, hard_cap_ms=120)
        deps = _make_deps(
            lambda: DelayedStream(
                [
                    (0.03, b"data: first\n\n"),
                    (0.25, b"data: second\n\n"),
                ]
            )
        )
        target = _make_target()
        async with httpx.AsyncClient() as client:
            result = await handle_streaming_attempt(
                deps=deps,
                state=state,
                target=target,
                client=client,
                start_time=time.monotonic(),
            )
        assert result.accepted is True
        assert result.prepared_response is not None
        chunks = await _collect_stream(result.prepared_response)
        assert chunks == [b"data: first\n\n"]

    asyncio.run(run())
