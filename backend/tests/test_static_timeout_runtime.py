from __future__ import annotations

import asyncio
import time
from types import SimpleNamespace
from typing import Any, cast

import httpx
from pydantic import ValidationError

import app.routers.proxy_domains.attempt_handlers as attempt_handlers_module
from app.routers.proxy_domains.attempt_handlers import handle_streaming_attempt
from app.schemas.schemas import EndpointCreate, LoadbalanceStrategyCreate


class ImmediateStream(httpx.AsyncByteStream):
    def __init__(self, chunks: list[bytes]) -> None:
        self._chunks = list(chunks)

    async def __aiter__(self):
        for chunk in self._chunks:
            yield chunk

    async def aclose(self) -> None:
        return None


async def _noop(*args, **kwargs):
    return None


async def _noop_bool(*args, **kwargs):
    return True


def _assert_validation_error(model, payload: dict[str, object]) -> None:
    try:
        model.model_validate(payload)
    except ValidationError:
        return
    raise AssertionError("Expected validation to fail")


def test_endpoint_create_rejects_removed_timeout_fields() -> None:
    _assert_validation_error(
        EndpointCreate,
        {
            "name": "Demo endpoint",
            "base_url": "https://demo.invalid",
            "api_key": "secret",
            "write_timeout": 60.0,
        },
    )


def test_loadbalance_strategy_create_rejects_removed_timeout_policy() -> None:
    _assert_validation_error(
        LoadbalanceStrategyCreate,
        {
            "name": "Default legacy routing",
            "strategy_type": "legacy",
            "legacy_strategy_type": "round-robin",
            "auto_recovery": {"mode": "disabled"},
            "timeout_policy": {"attempt_open_timeout_ms": 2_000},
        },
    )


def test_streaming_attempt_no_longer_requires_strategy_timeout_fields(
    monkeypatch,
) -> None:
    async def run() -> None:
        sentinel_prepared_response = object()
        monkeypatch.setattr(
            attempt_handlers_module,
            "build_streaming_response",
            lambda **kwargs: sentinel_prepared_response,
        )

        async def proxy_stream_fn(client, method, upstream_url, headers, raw_body):
            return httpx.Response(
                200,
                headers={"content-type": "text/event-stream"},
                request=httpx.Request(method, upstream_url),
                stream=ImmediateStream([b"data: first\n\n"]),
            )

        def filter_response_headers_fn(headers, was_requested_compressed=False):
            return dict(headers)

        deps = SimpleNamespace(
            filter_response_headers_fn=filter_response_headers_fn,
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
        state = SimpleNamespace(
            profile_id=1,
            request_path="/v1/chat/completions",
            setup=SimpleNamespace(
                method="POST",
                request_compressed=False,
                failover_policy=SimpleNamespace(
                    failover_status_codes=(500, 502, 503, 504),
                    failover_recovery_enabled=True,
                    failover_cooldown_seconds=30.0,
                ),
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
            ),
        )
        target = SimpleNamespace(
            attempt_number=1,
            connection=SimpleNamespace(
                id=1,
                endpoint_id=1,
                endpoint_rel=SimpleNamespace(base_url="http://demo.invalid"),
                name="demo-connection",
            ),
            description="demo-connection",
            endpoint_body=b'{"stream": true}',
            headers={},
            limiter_lease_token=None,
            limiter_lease_ttl_seconds=None,
            upstream_url="http://demo.invalid/v1/chat/completions",
        )

        async with httpx.AsyncClient() as client:
            result = await handle_streaming_attempt(
                deps=cast(Any, deps),
                state=cast(Any, state),
                target=cast(Any, target),
                client=client,
                start_time=time.monotonic(),
            )

        assert result.accepted is True
        assert result.prepared_response is sentinel_prepared_response

    asyncio.run(run())
