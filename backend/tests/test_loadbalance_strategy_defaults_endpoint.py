from __future__ import annotations

import asyncio
from typing import Any, cast

import pytest
from fastapi import HTTPException
from sqlalchemy import Table, create_engine
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import Session
from sqlalchemy.pool import StaticPool
from sqlalchemy.dialects.sqlite.base import SQLiteTypeCompiler
from sqlalchemy.dialects.postgresql import JSONB

from app.core.database import Base
from app.models.models import LoadbalanceStrategy, ModelConfig, Profile
from app.schemas.schemas import (
    LoadbalanceStrategyDefaultsResponse,
    LoadbalanceStrategyResponse,
)
from app.services.loadbalancer.policy import (
    build_default_auto_recovery_document,
    build_default_routing_policy_document,
)
from app.services.loadbalancer.strategies import (
    create_loadbalance_strategy_defaults,
    list_loadbalance_strategies,
)


if not hasattr(SQLiteTypeCompiler, "visit_JSONB"):
    SQLiteTypeCompiler.visit_JSONB = SQLiteTypeCompiler.visit_JSON  # type: ignore[attr-defined]


class _SyncAsyncSession:
    def __init__(self, session: Session) -> None:
        self._session = session

    def add(self, obj) -> None:
        self._session.add(obj)

    async def execute(self, statement):
        return self._session.execute(statement)

    async def flush(self):
        self._session.flush()


def _build_test_session() -> tuple[_SyncAsyncSession, Session]:
    engine = create_engine(
        "sqlite://",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )
    Base.metadata.create_all(
        bind=engine,
        tables=cast(
            list[Table],
            [Profile.__table__, LoadbalanceStrategy.__table__, ModelConfig.__table__],
        ),
    )
    session = Session(engine)
    return _SyncAsyncSession(session), session


def _add_profile(session: Session, *, profile_id: int) -> None:
    session.add(
        Profile(
            id=profile_id,
            name=f"Profile {profile_id}",
            is_active=profile_id == 1,
            is_default=profile_id == 1,
        )
    )
    session.flush()


def _assert_defaults_response(
    payload: dict[str, Any],
) -> LoadbalanceStrategyDefaultsResponse:
    validated = LoadbalanceStrategyDefaultsResponse.model_validate(payload)
    assert validated.created_count >= 0
    assert set(validated.model_dump()) == {
        "items",
        "created_count",
        "created_names",
        "existing_names",
    }
    for item in validated.items:
        assert isinstance(item, LoadbalanceStrategyResponse)
        assert item.profile_id >= 1
        assert item.attached_model_count >= 0
        assert item.created_at is not None
        assert item.updated_at is not None
    return validated


def test_empty_profile_creates_canonical_defaults_and_keeps_order() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        try:
            _add_profile(session, profile_id=1)
            session.commit()

            payload = await create_loadbalance_strategy_defaults(
                cast(AsyncSession, async_db),
                profile_id=1,
            )
            validated = _assert_defaults_response(payload)
            items = validated.items

            assert payload["created_count"] == 2
            assert payload["created_names"] == [
                "Default legacy routing",
                "Default adaptive routing",
            ]
            assert payload["existing_names"] == []
            assert [item.name for item in items] == [
                "Default adaptive routing",
                "Default legacy routing",
            ]

            legacy = next(
                item for item in items if item.name == "Default legacy routing"
            )
            adaptive = next(
                item for item in items if item.name == "Default adaptive routing"
            )
            assert legacy.auto_recovery is not None
            assert (
                legacy.auto_recovery.model_dump()
                == build_default_auto_recovery_document()
            )
            assert legacy.routing_policy is None
            assert adaptive.auto_recovery is None
            assert adaptive.routing_policy is not None
            assert (
                adaptive.routing_policy.model_dump()
                == build_default_routing_policy_document()
            )
        finally:
            session.close()

    asyncio.run(run())


def test_defaults_action_is_scoped_to_selected_profile_only() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        try:
            _add_profile(session, profile_id=1)
            _add_profile(session, profile_id=2)
            session.commit()

            payload = await create_loadbalance_strategy_defaults(
                cast(AsyncSession, async_db),
                profile_id=1,
            )
            items = _assert_defaults_response(payload).items

            assert payload["created_count"] == 2
            assert [item.profile_id for item in items] == [1, 1]
            second_profile_items = await list_loadbalance_strategies(
                cast(AsyncSession, async_db),
                profile_id=2,
            )
            assert second_profile_items == []
        finally:
            session.close()

    asyncio.run(run())


def test_custom_strategies_remain_untouched() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        try:
            _add_profile(session, profile_id=1)
            session.add(
                LoadbalanceStrategy(
                    profile_id=1,
                    name="Custom routing",
                    strategy_type="legacy",
                    legacy_strategy_type="single",
                    auto_recovery={"mode": "disabled"},
                    routing_policy=None,
                )
            )
            session.commit()

            payload = await create_loadbalance_strategy_defaults(
                cast(AsyncSession, async_db),
                profile_id=1,
            )
            items = _assert_defaults_response(payload).items

            assert payload["created_count"] == 2
            assert [item.name for item in items] == [
                "Default adaptive routing",
                "Default legacy routing",
                "Custom routing",
            ]
        finally:
            session.close()

    asyncio.run(run())


def test_repeat_call_is_idempotent() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        try:
            _add_profile(session, profile_id=1)
            session.commit()

            first = await create_loadbalance_strategy_defaults(
                cast(AsyncSession, async_db),
                profile_id=1,
            )
            second = await create_loadbalance_strategy_defaults(
                cast(AsyncSession, async_db),
                profile_id=1,
            )

            _assert_defaults_response(first)
            _assert_defaults_response(second)
            assert first["created_count"] == 2
            assert second["created_count"] == 0
            assert second["created_names"] == []
            assert second["existing_names"] == [
                "Default legacy routing",
                "Default adaptive routing",
            ]
        finally:
            session.close()

    asyncio.run(run())


def test_partial_existing_profile_creates_only_missing_default() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        try:
            _add_profile(session, profile_id=1)
            session.add(
                LoadbalanceStrategy(
                    profile_id=1,
                    name="Default legacy routing",
                    strategy_type="legacy",
                    legacy_strategy_type="round-robin",
                    auto_recovery=build_default_auto_recovery_document(),
                    routing_policy=None,
                )
            )
            session.commit()

            payload = await create_loadbalance_strategy_defaults(
                cast(AsyncSession, async_db),
                profile_id=1,
            )

            _assert_defaults_response(payload)
            assert payload["created_count"] == 1
            assert payload["created_names"] == ["Default adaptive routing"]
            assert payload["existing_names"] == ["Default legacy routing"]
        finally:
            session.close()

    asyncio.run(run())


def test_canonical_name_conflict_returns_409_and_creates_nothing() -> None:
    async def run() -> None:
        async_db, session = _build_test_session()
        try:
            _add_profile(session, profile_id=1)
            session.add(
                LoadbalanceStrategy(
                    profile_id=1,
                    name="Default adaptive routing",
                    strategy_type="legacy",
                    legacy_strategy_type="single",
                    auto_recovery={"mode": "disabled"},
                    routing_policy=None,
                )
            )
            session.commit()

            with pytest.raises(HTTPException) as exc_info:
                await create_loadbalance_strategy_defaults(
                    cast(AsyncSession, async_db),
                    profile_id=1,
                )

            assert exc_info.value.status_code == 409
            assert exc_info.value.detail == {
                "message": "Canonical loadbalance strategy default name conflict",
                "conflicting_names": ["Default adaptive routing"],
            }
        finally:
            session.close()

    asyncio.run(run())
