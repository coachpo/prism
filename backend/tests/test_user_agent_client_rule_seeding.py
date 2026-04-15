from __future__ import annotations

import asyncio
import re
from typing import cast

from sqlalchemy import Table, create_engine, select
from sqlalchemy.orm import Session
from sqlalchemy.pool import StaticPool

from app.bootstrap import startup as startup_module
from app.core.database import Base
from app.models.models import UserAgentClientRule
from app.services.stats.request_logs import _classify_user_agent_display


class _AsyncSessionContext:
    def __init__(self, session) -> None:
        self._session = session

    async def __aenter__(self):
        return self._session

    async def __aexit__(self, exc_type, exc, tb) -> bool:
        return False


class _SyncAsyncSession:
    def __init__(self, session: Session) -> None:
        self._session = session

    async def execute(self, statement):
        return self._session.execute(statement)

    def add(self, instance) -> None:
        self._session.add(instance)

    async def commit(self) -> None:
        self._session.commit()


def _build_user_agent_rule_test_session() -> tuple[_SyncAsyncSession, Session]:
    engine = create_engine(
        "sqlite://",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )
    Base.metadata.create_all(
        bind=engine,
        tables=cast(list[Table], [UserAgentClientRule.__table__]),
    )
    session = Session(engine)
    return _SyncAsyncSession(session), session


def test_claude_code_rule_classifies_claude_cli_user_agent() -> None:
    pattern = next(
        rule["pattern"]
        for rule in startup_module.SYSTEM_USER_AGENT_CLIENT_RULE_DEFAULTS
        if rule["name"] == "Claude Code"
    )
    rules = [("Claude Code", re.compile(pattern, re.IGNORECASE))]

    assert (
        _classify_user_agent_display(
            "claude-cli/2.1.109 (external, cli)",
            rules,
        )
        == "Claude Code"
    )
    assert _classify_user_agent_display("Claude Code/1.0", rules) == "Claude Code"


def test_seed_user_agent_client_rules_updates_existing_claude_code_pattern() -> None:
    async def run() -> None:
        async_db, session = _build_user_agent_rule_test_session()
        original_async_session_local = startup_module.database_core.AsyncSessionLocal
        original_pattern = "claude(?:\\s|-)?code"
        updated_pattern = "claude(?:\\s|-)?(?:code|cli)"

        try:
            session.add(
                UserAgentClientRule(
                    name="Claude Code",
                    pattern=original_pattern,
                    enabled=False,
                    is_system=True,
                )
            )
            session.commit()

            startup_module.database_core.AsyncSessionLocal = (
                lambda: _AsyncSessionContext(async_db)
            )

            await startup_module.seed_user_agent_client_rules()

            system_rules = (
                session.execute(
                    select(UserAgentClientRule)
                    .where(UserAgentClientRule.is_system == True)  # noqa: E712
                    .order_by(UserAgentClientRule.id.asc())
                )
                .scalars()
                .all()
            )
            claude_rules = [rule for rule in system_rules if rule.name == "Claude Code"]

            assert len(system_rules) == len(
                startup_module.SYSTEM_USER_AGENT_CLIENT_RULE_DEFAULTS
            )
            assert len(claude_rules) == 1
            assert claude_rules[0].pattern == updated_pattern
            assert claude_rules[0].enabled is False
        finally:
            startup_module.database_core.AsyncSessionLocal = (
                original_async_session_local
            )
            session.close()

    asyncio.run(run())
