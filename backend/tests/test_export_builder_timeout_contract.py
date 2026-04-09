from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace
from typing import cast

import app.routers.config_domains.export_builder as export_builder_module
from app.routers.config_domains.export_builder import build_export_payload
from sqlalchemy.ext.asyncio import AsyncSession


class _FakeExecuteResult:
    def __init__(self, *, items=None, scalar=None) -> None:
        self._items = list(items or [])
        self._scalar = scalar

    def scalars(self):
        return self

    def all(self):
        return list(self._items)

    def scalar_one_or_none(self):
        return self._scalar


class _FakeAsyncSession:
    def __init__(self, results: list[_FakeExecuteResult]) -> None:
        self._results = iter(results)

    async def execute(self, _query):
        return next(self._results)


def test_build_export_payload_omits_removed_timeout_fields(monkeypatch) -> None:
    async def run() -> None:
        exported_at = datetime(2026, 4, 9, tzinfo=timezone.utc)
        endpoint = SimpleNamespace(
            id=1,
            name="Demo endpoint",
            base_url="https://demo.invalid",
            api_key="encrypted-secret",
            position=0,
        )
        legacy_strategy = SimpleNamespace(name="Legacy strategy", family="legacy")
        adaptive_strategy = SimpleNamespace(name="Adaptive strategy", family="adaptive")
        db = cast(
            AsyncSession,
            _FakeAsyncSession(
                [
                    _FakeExecuteResult(items=[endpoint]),
                    _FakeExecuteResult(items=[]),
                    _FakeExecuteResult(items=[legacy_strategy, adaptive_strategy]),
                    _FakeExecuteResult(items=[]),
                    _FakeExecuteResult(scalar=None),
                    _FakeExecuteResult(items=[]),
                    _FakeExecuteResult(items=[]),
                ]
            ),
        )

        monkeypatch.setattr(
            export_builder_module, "decrypt_secret", lambda value: f"plain:{value}"
        )
        monkeypatch.setattr(
            export_builder_module,
            "encrypt_bundle_secret",
            lambda value: f"bundle:{value}",
        )
        monkeypatch.setattr(
            export_builder_module, "get_bundle_secret_cipher", lambda: "fernet-v1"
        )
        monkeypatch.setattr(
            export_builder_module, "get_bundle_secret_key_id", lambda: "test-key"
        )
        monkeypatch.setattr(export_builder_module, "utc_now", lambda: exported_at)
        monkeypatch.setattr(
            export_builder_module,
            "resolve_effective_loadbalance_policy",
            lambda strategy: SimpleNamespace(
                strategy_type=strategy.family,
                legacy_strategy_type=(
                    "round-robin" if strategy.family == "legacy" else None
                ),
            ),
        )
        monkeypatch.setattr(
            export_builder_module,
            "serialize_auto_recovery",
            lambda _policy: {"mode": "disabled"},
        )
        monkeypatch.setattr(
            export_builder_module,
            "serialize_routing_policy",
            lambda _policy: {
                "kind": "adaptive",
                "routing_objective": "minimize_latency",
                "hedge": {
                    "enabled": False,
                    "delay_ms": 1500,
                    "max_additional_attempts": 1,
                },
                "circuit_breaker": {
                    "failure_status_codes": [429, 500, 503],
                    "base_open_seconds": 60,
                    "failure_threshold": 2,
                    "backoff_multiplier": 2,
                    "max_open_seconds": 900,
                    "jitter_ratio": 0.2,
                    "ban_mode": "off",
                    "max_open_strikes_before_ban": 0,
                    "ban_duration_seconds": 0,
                },
                "admission": {
                    "respect_qps_limit": True,
                    "respect_in_flight_limits": True,
                },
            },
        )

        payload = await build_export_payload(db, profile_id=1)
        dumped_payload = payload.model_dump()

        assert dumped_payload["exported_at"] == exported_at
        assert dumped_payload["endpoints"] == [
            {
                "name": "Demo endpoint",
                "base_url": "https://demo.invalid",
                "api_key_secret_ref": "endpoint:Demo endpoint:api_key",
                "position": 0,
            }
        ]
        for endpoint_payload in dumped_payload["endpoints"]:
            assert "pool_timeout" not in endpoint_payload
            assert "connect_timeout" not in endpoint_payload
            assert "write_timeout" not in endpoint_payload
            assert "read_idle_timeout" not in endpoint_payload

        exported_strategies = {
            item["name"]: item for item in dumped_payload["loadbalance_strategies"]
        }
        legacy_export = exported_strategies["Legacy strategy"]
        assert legacy_export["name"] == "Legacy strategy"
        assert legacy_export["strategy_type"] == "legacy"
        assert legacy_export["legacy_strategy_type"] == "round-robin"
        assert legacy_export["routing_policy"] is None
        assert legacy_export["auto_recovery"]["mode"] == "disabled"
        assert exported_strategies["Adaptive strategy"] == {
            "name": "Adaptive strategy",
            "strategy_type": "adaptive",
            "legacy_strategy_type": None,
            "auto_recovery": None,
            "routing_policy": {
                "kind": "adaptive",
                "routing_objective": "minimize_latency",
                "hedge": {
                    "enabled": False,
                    "delay_ms": 1500,
                    "max_additional_attempts": 1,
                },
                "circuit_breaker": {
                    "failure_status_codes": [429, 500, 503],
                    "base_open_seconds": 60.0,
                    "failure_threshold": 2,
                    "backoff_multiplier": 2.0,
                    "max_open_seconds": 900,
                    "jitter_ratio": 0.2,
                    "ban_mode": "off",
                    "max_open_strikes_before_ban": 0,
                    "ban_duration_seconds": 0,
                },
                "admission": {
                    "respect_qps_limit": True,
                    "respect_in_flight_limits": True,
                },
            },
        }
        for strategy_payload in exported_strategies.values():
            assert "timeout_policy" not in strategy_payload

    asyncio.run(run())
