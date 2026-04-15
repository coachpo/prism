from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace
from typing import cast

import pytest
from pydantic import ValidationError
from sqlalchemy import Table, create_engine
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import Session
from sqlalchemy.pool import StaticPool

from app.core.database import Base
from app.models.models import Endpoint, RequestLog, UserAgentClientRule
from app.routers.stats_domains.request_logs_route_handlers import list_request_logs
from app.schemas.domains.stats import (
    RequestLogListItemResponse,
    RequestLogListResponse,
)
from app.services.stats.request_logs import RequestLogListResult, get_request_logs

_FAKE_DB = cast(AsyncSession, object())


class _SyncAsyncSession:
    def __init__(self, session: Session) -> None:
        self._session = session

    async def execute(self, statement):
        return self._session.execute(statement)


def _build_request_logs_test_session() -> tuple[_SyncAsyncSession, Session]:
    engine = create_engine(
        "sqlite://",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )
    Base.metadata.create_all(
        bind=engine,
        tables=cast(
            list[Table],
            [Endpoint.__table__, RequestLog.__table__, UserAgentClientRule.__table__],
        ),
    )
    session = Session(engine)
    return _SyncAsyncSession(session), session


def _make_request_log_row(
    *,
    id: int,
    created_at: datetime,
    endpoint_id: int | None,
    endpoint_base_url: str | None,
    endpoint_description: str | None = None,
) -> RequestLog:
    return RequestLog(
        id=id,
        profile_id=1,
        model_id="gpt-5.4",
        api_family="openai",
        endpoint_id=endpoint_id,
        connection_id=None,
        endpoint_base_url=endpoint_base_url,
        status_code=200,
        response_time_ms=321,
        is_stream=False,
        request_path="/v1/chat/completions",
        endpoint_description=endpoint_description,
        created_at=created_at,
    )


def _make_list_item_payload(**overrides: object) -> dict[str, object]:
    payload: dict[str, object] = {
        "id": 101,
        "created_at": datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc),
        "model_id": "gpt-5.4",
        "resolved_target_model_id": "gpt-5.4-mini",
        "api_family": "openai",
        "vendor_id": 12,
        "vendor_key": "openai",
        "vendor_name": "OpenAI",
        "endpoint_id": 34,
        "endpoint_label": "Primary endpoint",
        "connection_id": 56,
        "status_code": 200,
        "response_time_ms": 321,
        "ttft_ms": 123,
        "completion_duration_ms": 987,
        "is_stream": True,
        "output_tokens": 22,
        "total_tokens": 33,
        "total_cost_user_currency_micros": 66,
        "report_currency_symbol": "$",
        "caller_client_display": "CLI",
        "upstream_client_display": "CLI",
        "user_agent_overridden": False,
    }
    payload.update(overrides)
    return payload


def _make_list_item_stub(**overrides: object) -> SimpleNamespace:
    return SimpleNamespace(**_make_list_item_payload(**overrides))


def _make_list_result_stub(
    *,
    items: list[SimpleNamespace],
    total: int,
    endpoints: list[dict[str, object]],
) -> RequestLogListResult:
    return RequestLogListResult(items=items, total=total, endpoints=endpoints)


def test_list_item_contract_requires_endpoint_label() -> None:
    payload = _make_list_item_payload()
    payload.pop("endpoint_label")

    with pytest.raises(ValidationError, match="endpoint_label"):
        RequestLogListItemResponse.model_validate(payload)


def test_list_response_contract_requires_filter_options_endpoints() -> None:
    payload = {
        "items": [_make_list_item_payload()],
        "total": 1,
        "limit": 50,
        "offset": 0,
        "filter_options": {},
    }

    with pytest.raises(ValidationError, match="endpoints"):
        RequestLogListResponse.model_validate(payload)


def test_list_item_contract_uses_backend_owned_endpoint_label_without_endpoint_description() -> (
    None
):
    payload = RequestLogListItemResponse.model_validate(
        _make_list_item_payload(
            endpoint_id=None,
            endpoint_label="https://archived.example.invalid",
            endpoint_description="Connection description should stay out of the list payload",
        )
    ).model_dump()

    assert payload["endpoint_label"] == "https://archived.example.invalid"
    assert "endpoint_description" not in payload


def test_route_envelope_serializes_stale_filtered_endpoint_options() -> None:
    stale_endpoint_id = 999
    result = _make_list_result_stub(
        items=[
            _make_list_item_stub(
                endpoint_id=stale_endpoint_id,
                endpoint_label=f"Endpoint {stale_endpoint_id}",
            )
        ],
        total=1,
        endpoints=[
            {
                "endpoint_id": stale_endpoint_id,
                "endpoint_label": f"Endpoint {stale_endpoint_id}",
            },
            {"endpoint_id": 34, "endpoint_label": "Primary endpoint"},
        ],
    )

    async def stub_get_request_logs(_db, **kwargs):
        assert kwargs["endpoint_id"] == stale_endpoint_id
        return result

    payload = asyncio.run(
        list_request_logs(
            db=_FAKE_DB,
            profile_id=7,
            endpoint_id=stale_endpoint_id,
            limit=50,
            offset=0,
            get_request_logs_fn=stub_get_request_logs,
        )
    ).model_dump()

    assert payload["items"][0]["endpoint_label"] == f"Endpoint {stale_endpoint_id}"
    assert payload["filter_options"]["endpoints"] == [
        {
            "endpoint_id": stale_endpoint_id,
            "endpoint_label": f"Endpoint {stale_endpoint_id}",
        },
        {"endpoint_id": 34, "endpoint_label": "Primary endpoint"},
    ]
    assert payload["limit"] == 50
    assert payload["offset"] == 0


def test_route_envelope_returns_endpoint_filters_for_empty_pages() -> None:
    result = _make_list_result_stub(
        items=[],
        total=0,
        endpoints=[
            {"endpoint_id": 34, "endpoint_label": "Primary endpoint"},
            {"endpoint_id": 35, "endpoint_label": "Secondary endpoint"},
        ],
    )

    async def stub_get_request_logs(_db, **_kwargs):
        return result

    payload = asyncio.run(
        list_request_logs(
            db=_FAKE_DB,
            profile_id=7,
            limit=25,
            offset=75,
            get_request_logs_fn=stub_get_request_logs,
        )
    ).model_dump()

    assert payload == {
        "items": [],
        "total": 0,
        "limit": 25,
        "offset": 75,
        "filter_options": {
            "endpoints": [
                {"endpoint_id": 34, "endpoint_label": "Primary endpoint"},
                {"endpoint_id": 35, "endpoint_label": "Secondary endpoint"},
            ]
        },
    }


def test_get_request_logs_populates_endpoint_labels_and_filter_options() -> None:
    async def run() -> None:
        async_db, session = _build_request_logs_test_session()
        created_at = datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc)

        try:
            session.add_all(
                [
                    Endpoint(
                        id=12,
                        profile_id=1,
                        name="Earlier endpoint",
                        base_url="https://earlier.example.invalid",
                        api_key="test-key",
                        position=1,
                    ),
                    Endpoint(
                        id=34,
                        profile_id=1,
                        name="Primary endpoint",
                        base_url="https://primary.example.invalid",
                        api_key="test-key",
                        position=2,
                    ),
                    Endpoint(
                        id=35,
                        profile_id=1,
                        name="",
                        base_url="https://current-base-url.example.invalid",
                        api_key="test-key",
                        position=2,
                    ),
                    Endpoint(
                        id=88,
                        profile_id=2,
                        name="Other profile endpoint",
                        base_url="https://other-profile.example.invalid",
                        api_key="test-key",
                        position=1,
                    ),
                ]
            )
            session.add_all(
                [
                    _make_request_log_row(
                        id=201,
                        created_at=created_at,
                        endpoint_id=34,
                        endpoint_base_url="https://historical-primary.example.invalid",
                        endpoint_description="Ignore current description",
                    ),
                    _make_request_log_row(
                        id=202,
                        created_at=created_at.replace(minute=1),
                        endpoint_id=35,
                        endpoint_base_url="https://historical-secondary.example.invalid",
                        endpoint_description="Ignore current description",
                    ),
                    _make_request_log_row(
                        id=203,
                        created_at=created_at.replace(minute=2),
                        endpoint_id=999,
                        endpoint_base_url="https://archived.example.invalid",
                        endpoint_description="Archived description should stay out",
                    ),
                    _make_request_log_row(
                        id=204,
                        created_at=created_at.replace(minute=3),
                        endpoint_id=1000,
                        endpoint_base_url=None,
                        endpoint_description="Synthetic fallback only",
                    ),
                    _make_request_log_row(
                        id=205,
                        created_at=created_at.replace(minute=4),
                        endpoint_id=None,
                        endpoint_base_url=None,
                        endpoint_description="Unknown fallback only",
                    ),
                    RequestLog(
                        id=299,
                        profile_id=2,
                        model_id="gpt-5.4",
                        api_family="openai",
                        endpoint_id=88,
                        connection_id=None,
                        endpoint_base_url="https://other-profile.example.invalid",
                        status_code=200,
                        response_time_ms=321,
                        is_stream=False,
                        request_path="/v1/chat/completions",
                        created_at=created_at,
                    ),
                ]
            )
            session.commit()

            result = await get_request_logs(
                cast(AsyncSession, async_db),
                profile_id=1,
                limit=10,
                offset=0,
            )

            assert result.total == 5
            assert result.endpoints == [
                {"endpoint_id": 12, "endpoint_label": "Earlier endpoint"},
                {"endpoint_id": 34, "endpoint_label": "Primary endpoint"},
                {
                    "endpoint_id": 35,
                    "endpoint_label": "https://current-base-url.example.invalid",
                },
            ]

            labels_by_request_id = {
                item.id: item.endpoint_label for item in result.items
            }
            assert labels_by_request_id == {
                205: "Unknown Endpoint",
                204: "Endpoint 1000",
                203: "https://archived.example.invalid",
                202: "https://current-base-url.example.invalid",
                201: "Primary endpoint",
            }
        finally:
            session.close()

    asyncio.run(run())


def test_get_request_logs_prepends_one_synthetic_stale_selected_endpoint_option() -> (
    None
):
    async def run() -> None:
        async_db, session = _build_request_logs_test_session()
        created_at = datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc)

        try:
            session.add_all(
                [
                    Endpoint(
                        id=34,
                        profile_id=1,
                        name="Primary endpoint",
                        base_url="https://primary.example.invalid",
                        api_key="test-key",
                        position=2,
                    ),
                    Endpoint(
                        id=35,
                        profile_id=1,
                        name="Secondary endpoint",
                        base_url="https://secondary.example.invalid",
                        api_key="test-key",
                        position=1,
                    ),
                ]
            )
            session.add_all(
                [
                    _make_request_log_row(
                        id=301,
                        created_at=created_at,
                        endpoint_id=999,
                        endpoint_base_url=None,
                    ),
                    _make_request_log_row(
                        id=302,
                        created_at=created_at.replace(minute=1),
                        endpoint_id=34,
                        endpoint_base_url="https://historical-primary.example.invalid",
                    ),
                ]
            )
            session.commit()

            stale_result = await get_request_logs(
                cast(AsyncSession, async_db),
                profile_id=1,
                endpoint_id=999,
                limit=50,
                offset=0,
            )
            current_result = await get_request_logs(
                cast(AsyncSession, async_db),
                profile_id=1,
                endpoint_id=34,
                limit=50,
                offset=0,
            )

            assert stale_result.total == 1
            assert [item.id for item in stale_result.items] == [301]
            assert stale_result.items[0].endpoint_label == "Endpoint 999"
            assert stale_result.endpoints == [
                {"endpoint_id": 999, "endpoint_label": "Endpoint 999"},
                {"endpoint_id": 35, "endpoint_label": "Secondary endpoint"},
                {"endpoint_id": 34, "endpoint_label": "Primary endpoint"},
            ]

            assert current_result.total == 1
            assert [item.id for item in current_result.items] == [302]
            assert current_result.items[0].endpoint_label == "Primary endpoint"
            assert current_result.endpoints == [
                {"endpoint_id": 35, "endpoint_label": "Secondary endpoint"},
                {"endpoint_id": 34, "endpoint_label": "Primary endpoint"},
            ]
        finally:
            session.close()

    asyncio.run(run())
