from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any, cast

import app.core.database as database_module
from app.routers.proxy_domains.attempt_outcome_reporting import record_final_usage_event
from app.services.stats.usage_events import log_final_usage_request_event


class _FakeAsyncSession:
    def __init__(self, *, refreshed_id: int = 1) -> None:
        self.entry = None
        self.refreshed_id = refreshed_id
        self.rollback_called = False

    async def __aenter__(self) -> _FakeAsyncSession:
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        return None

    def add(self, entry) -> None:
        self.entry = entry

    async def commit(self) -> None:
        return None

    async def refresh(self, entry) -> None:
        entry.id = self.refreshed_id

    async def rollback(self) -> None:
        self.rollback_called = True


def test_forwards_and_persists_elapsed_ms_record_final_usage_event_forwards() -> None:
    async def run() -> None:
        captured_kwargs: dict[str, object] = {}

        async def log_usage_request_event_fn(**kwargs):
            captured_kwargs.update(kwargs)
            return 99

        deps = SimpleNamespace(log_usage_request_event_fn=log_usage_request_event_fn)
        state = SimpleNamespace(
            profile_id=7,
            request_path="/v1/chat/completions",
            setup=SimpleNamespace(
                model_id="gpt-5.4",
                api_family="openai",
                resolved_target_model_id="gpt-5.4-mini",
                proxy_api_key_id=12,
                proxy_api_key_name="Operator key",
                ingress_request_id="req-forward",
                build_cost_fields=lambda connection, status_code, tokens=None: {},
            ),
        )
        target = SimpleNamespace(
            connection=SimpleNamespace(
                id=21,
                endpoint_id=34,
            )
        )

        result = await record_final_usage_event(
            deps=cast(Any, deps),
            state=cast(Any, state),
            target=cast(Any, target),
            status_code=200,
            attempt_count=2,
            elapsed_ms=250,
            tokens={
                "input_tokens": 10,
                "output_tokens": 20,
                "total_tokens": 30,
            },
        )

        assert result == 99
        assert captured_kwargs["response_time_ms"] == 250
        assert captured_kwargs["attempt_count"] == 2
        assert captured_kwargs["total_tokens"] == 30

    asyncio.run(run())


def test_forwards_and_persists_elapsed_ms_log_final_usage_request_event_persists(
    monkeypatch,
) -> None:
    async def run() -> None:
        fake_session = _FakeAsyncSession(refreshed_id=123)
        monkeypatch.setattr(database_module, "AsyncSessionLocal", lambda: fake_session)

        result = await log_final_usage_request_event(
            model_id="gpt-5.4",
            profile_id=1,
            api_family="openai",
            resolved_target_model_id="gpt-5.4-mini",
            endpoint_id=2,
            connection_id=3,
            proxy_api_key_id=4,
            proxy_api_key_name_snapshot="Snapshot key",
            ingress_request_id="req-persist-ms",
            status_code=200,
            response_time_ms=250,
            success_flag=True,
            input_tokens=11,
            output_tokens=22,
            total_tokens=33,
            attempt_count=1,
            request_path="/v1/chat/completions",
        )

        assert result == 123
        assert fake_session.entry is not None
        assert fake_session.entry.response_time_ms == 250

    asyncio.run(run())


def test_nullable_for_historical_rows_log_final_usage_request_event_preserves_none_response_time_ms(
    monkeypatch,
) -> None:
    async def run() -> None:
        fake_session = _FakeAsyncSession(refreshed_id=456)
        monkeypatch.setattr(database_module, "AsyncSessionLocal", lambda: fake_session)

        result = await log_final_usage_request_event(
            model_id="gpt-5.4",
            profile_id=1,
            api_family="openai",
            resolved_target_model_id=None,
            endpoint_id=None,
            connection_id=None,
            proxy_api_key_id=None,
            proxy_api_key_name_snapshot=None,
            ingress_request_id="req-persist-none",
            status_code=502,
            response_time_ms=None,
            success_flag=False,
            attempt_count=2,
            request_path="/v1/chat/completions",
        )

        assert result == 456
        assert fake_session.entry is not None
        assert fake_session.entry.response_time_ms is None

    asyncio.run(run())
