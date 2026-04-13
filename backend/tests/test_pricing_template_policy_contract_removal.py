from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from typing import Any, cast

import app.routers.pricing_templates_domains.route_handlers as route_handlers_module
from app.models.models import PricingTemplate
from app.routers.pricing_templates_domains.helpers import PRICING_AFFECTING_FIELDS
from app.routers.pricing_templates_domains.route_handlers import (
    create_pricing_template,
    update_pricing_template,
)
from app.schemas.schemas import (
    PricingTemplateCreate,
    PricingTemplateResponse,
    PricingTemplateUpdate,
)
from pydantic import ValidationError

REMOVED_POLICY_FIELD = "_".join(("missing", "special", "token", "price", "policy"))
LEGACY_POLICY_VALUE = "legacy-policy-value"


def _assert_validation_error(model, payload: dict[str, Any]) -> None:
    try:
        model.model_validate(payload)
    except ValidationError:
        return
    raise AssertionError("Expected validation to fail")


def _build_create_payload() -> dict[str, Any]:
    return {
        "name": "Demo template",
        "description": "Shared pricing",
        "pricing_unit": "PER_1M",
        "pricing_currency_code": "usd",
        "input_price": "1.50",
        "output_price": "3.00",
        "cached_input_price": "0.25",
        "cache_creation_price": "0.40",
        "reasoning_price": "0.60",
    }


def _build_template(*, updated_at: datetime, version: int = 4) -> PricingTemplate:
    return PricingTemplate(
        id=17,
        profile_id=9,
        name="Demo template",
        description="Shared pricing",
        pricing_unit="PER_1M",
        pricing_currency_code="USD",
        input_price="1.500000",
        output_price="3.000000",
        cached_input_price="0.250000",
        cache_creation_price="0.400000",
        reasoning_price="0.600000",
        version=version,
        created_at=datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc),
        updated_at=updated_at,
    )


class _FakeAsyncSession:
    def __init__(self) -> None:
        self.added: list[PricingTemplate] = []
        self.flush_calls = 0
        self.refresh_calls = 0

    def add(self, item: PricingTemplate) -> None:
        self.added.append(item)

    async def flush(self) -> None:
        self.flush_calls += 1

    async def refresh(self, _item: PricingTemplate) -> None:
        self.refresh_calls += 1


def test_pricing_template_create_and_update_reject_removed_policy_field() -> None:
    create_payload = _build_create_payload()
    create_payload[REMOVED_POLICY_FIELD] = LEGACY_POLICY_VALUE
    _assert_validation_error(PricingTemplateCreate, create_payload)

    _assert_validation_error(
        PricingTemplateUpdate,
        {
            "expected_updated_at": datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc),
            REMOVED_POLICY_FIELD: LEGACY_POLICY_VALUE,
        },
    )


def test_pricing_template_response_omits_removed_policy_field() -> None:
    payload = PricingTemplateResponse.model_validate(
        _build_template(
            updated_at=datetime(2026, 4, 12, 12, 30, tzinfo=timezone.utc),
        )
    ).model_dump()

    assert REMOVED_POLICY_FIELD not in payload
    assert payload["reasoning_price"] == "0.600000"


def test_pricing_affecting_fields_keep_only_remaining_pricing_fields() -> None:
    assert PRICING_AFFECTING_FIELDS == {
        "pricing_unit",
        "pricing_currency_code",
        "input_price",
        "output_price",
        "cached_input_price",
        "cache_creation_price",
        "reasoning_price",
    }


def test_create_pricing_template_no_longer_writes_removed_policy_field(
    monkeypatch,
) -> None:
    async def run() -> None:
        async def _noop_ensure_unique_template_name(*args, **kwargs) -> None:
            return None

        db = cast(Any, _FakeAsyncSession())
        monkeypatch.setattr(
            route_handlers_module,
            "ensure_unique_template_name",
            _noop_ensure_unique_template_name,
        )

        template = await create_pricing_template(
            PricingTemplateCreate.model_validate(_build_create_payload()),
            db,
            profile_id=9,
        )

        assert template is db.added[0]
        assert not hasattr(template, REMOVED_POLICY_FIELD)
        assert template.reasoning_price == "0.60"
        assert template.version == 1
        assert db.flush_calls == 1
        assert db.refresh_calls == 1

    asyncio.run(run())


def test_update_pricing_template_versioning_uses_remaining_pricing_fields_only(
    monkeypatch,
) -> None:
    async def run() -> None:
        original_updated_at = datetime(2026, 4, 12, 12, 0, tzinfo=timezone.utc)
        bumped_updated_at = datetime(2026, 4, 12, 12, 5, tzinfo=timezone.utc)
        templates = [
            _build_template(updated_at=original_updated_at, version=4),
            _build_template(updated_at=original_updated_at, version=7),
        ]
        db = cast(Any, _FakeAsyncSession())

        async def _fake_load_template_or_404(*args, **kwargs) -> PricingTemplate:
            return templates.pop(0)

        async def _noop_ensure_unique_template_name(*args, **kwargs) -> None:
            return None

        monkeypatch.setattr(
            route_handlers_module,
            "load_template_or_404",
            _fake_load_template_or_404,
        )
        monkeypatch.setattr(
            route_handlers_module,
            "ensure_unique_template_name",
            _noop_ensure_unique_template_name,
        )
        monkeypatch.setattr(route_handlers_module, "utc_now", lambda: bumped_updated_at)

        description_only = await update_pricing_template(
            17,
            PricingTemplateUpdate.model_validate(
                {
                    "expected_updated_at": original_updated_at,
                    "description": "Renamed description",
                }
            ),
            db,
            profile_id=9,
        )
        pricing_change = await update_pricing_template(
            17,
            PricingTemplateUpdate.model_validate(
                {
                    "expected_updated_at": original_updated_at,
                    "reasoning_price": "0.75",
                }
            ),
            db,
            profile_id=9,
        )

        assert description_only.description == "Renamed description"
        assert description_only.version == 4
        assert description_only.updated_at == bumped_updated_at

        assert pricing_change.reasoning_price == "0.75"
        assert pricing_change.version == 8
        assert pricing_change.updated_at == bumped_updated_at
        assert db.flush_calls == 2
        assert db.refresh_calls == 2

    asyncio.run(run())
