from __future__ import annotations

import asyncio

import app.services.stats_service as stats_service_module
from app.bootstrap import startup as startup_module


class _AsyncSessionContext:
    def __init__(self, session) -> None:
        self._session = session

    async def __aenter__(self):
        return self._session

    async def __aexit__(self, exc_type, exc, tb) -> bool:
        return False


class _FakeSession:
    def __init__(self, *, pending_usage_event_count: int) -> None:
        self.pending_usage_event_count = pending_usage_event_count
        self.scalar_calls = 0

    async def scalar(self, statement):
        self.scalar_calls += 1
        return self.pending_usage_event_count


def test_reconcile_usage_request_event_billing_fields_skips_when_no_pending_rows(
    monkeypatch,
) -> None:
    async def run() -> None:
        session = _FakeSession(pending_usage_event_count=0)
        backfill_called = False

        async def _fake_backfill(db_session):
            nonlocal backfill_called
            backfill_called = True
            return {
                "matched_request_log_count": 0,
                "unmatched_usage_event_count": 0,
                "duplicate_candidate_count": 0,
            }

        monkeypatch.setattr(
            startup_module.database_core,
            "AsyncSessionLocal",
            lambda: _AsyncSessionContext(session),
        )
        monkeypatch.setattr(
            stats_service_module,
            "backfill_usage_request_event_billing_fields",
            _fake_backfill,
        )

        await startup_module.reconcile_usage_request_event_billing_fields()

        assert session.scalar_calls == 1
        assert backfill_called is False

    asyncio.run(run())


def test_reconcile_usage_request_event_billing_fields_runs_service_backfill(
    monkeypatch,
) -> None:
    async def run() -> None:
        session = _FakeSession(pending_usage_event_count=2)
        called_with: list[object] = []

        async def _fake_backfill(db_session):
            called_with.append(db_session)
            return {
                "matched_request_log_count": 1,
                "unmatched_usage_event_count": 1,
                "duplicate_candidate_count": 0,
            }

        monkeypatch.setattr(
            startup_module.database_core,
            "AsyncSessionLocal",
            lambda: _AsyncSessionContext(session),
        )
        monkeypatch.setattr(
            stats_service_module,
            "backfill_usage_request_event_billing_fields",
            _fake_backfill,
        )

        await startup_module.reconcile_usage_request_event_billing_fields()

        assert session.scalar_calls == 1
        assert called_with == [session]

    asyncio.run(run())


def test_run_startup_sequence_includes_usage_event_billing_reconciliation(
    monkeypatch,
) -> None:
    async def run() -> None:
        calls: list[str] = []

        async def _record(name: str) -> None:
            calls.append(name)

        async def _run_startup_migrations() -> None:
            await _record("run_startup_migrations")

        async def _reconcile_usage_request_event_billing_fields() -> None:
            await _record("reconcile_usage_request_event_billing_fields")

        async def _seed_vendors() -> None:
            await _record("seed_vendors")

        async def _seed_profile_invariants() -> None:
            await _record("seed_profile_invariants")

        async def _seed_user_settings() -> None:
            await _record("seed_user_settings")

        async def _seed_user_agent_client_rules() -> None:
            await _record("seed_user_agent_client_rules")

        async def _seed_app_auth_settings() -> None:
            await _record("seed_app_auth_settings")

        async def _encrypt_endpoint_secrets() -> None:
            await _record("encrypt_endpoint_secrets")

        async def _seed_header_blocklist_rules() -> None:
            await _record("seed_header_blocklist_rules")

        monkeypatch.delenv(startup_module.SKIP_STARTUP_SEQUENCE_ENV, raising=False)
        monkeypatch.setattr(
            startup_module,
            "run_startup_migrations",
            _run_startup_migrations,
        )
        monkeypatch.setattr(
            startup_module,
            "reconcile_usage_request_event_billing_fields",
            _reconcile_usage_request_event_billing_fields,
        )
        monkeypatch.setattr(startup_module, "seed_vendors", _seed_vendors)
        monkeypatch.setattr(
            startup_module,
            "seed_profile_invariants",
            _seed_profile_invariants,
        )
        monkeypatch.setattr(startup_module, "seed_user_settings", _seed_user_settings)
        monkeypatch.setattr(
            startup_module,
            "seed_user_agent_client_rules",
            _seed_user_agent_client_rules,
        )
        monkeypatch.setattr(
            startup_module,
            "seed_app_auth_settings",
            _seed_app_auth_settings,
        )
        monkeypatch.setattr(
            startup_module,
            "encrypt_endpoint_secrets",
            _encrypt_endpoint_secrets,
        )
        monkeypatch.setattr(
            startup_module,
            "seed_header_blocklist_rules",
            _seed_header_blocklist_rules,
        )

        await startup_module.run_startup_sequence()

        assert calls == [
            "run_startup_migrations",
            "reconcile_usage_request_event_billing_fields",
            "seed_vendors",
            "seed_profile_invariants",
            "seed_user_settings",
            "seed_user_agent_client_rules",
            "seed_app_auth_settings",
            "encrypt_endpoint_secrets",
            "seed_header_blocklist_rules",
        ]

    asyncio.run(run())
