from datetime import datetime
from decimal import Decimal
from typing import Literal, Optional, Union

from pydantic import BaseModel, ConfigDict, Field, field_validator

from .common import _CURRENCY_CODE_RE, _validate_decimal_non_negative


class EndpointBase(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    base_url: str
    api_key: str


class EndpointCreate(EndpointBase):
    pass


class EndpointUpdate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: Optional[str] = None
    base_url: Optional[str] = None
    api_key: Optional[str] = None


class EndpointResponse(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    profile_id: int
    name: str
    base_url: str
    has_api_key: bool = False
    masked_api_key: Optional[str] = None
    position: int
    created_at: datetime
    updated_at: datetime


class EndpointPositionMoveRequest(BaseModel):
    to_index: int = Field(ge=0)


class ConnectionPriorityMoveRequest(BaseModel):
    to_index: int = Field(ge=0)


class PricingTemplateCreate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    description: Optional[str] = None
    pricing_unit: Literal["PER_1M"] = "PER_1M"
    pricing_currency_code: str
    input_price: str
    output_price: str
    cached_input_price: Optional[str] = None
    cache_creation_price: Optional[str] = None
    reasoning_price: Optional[str] = None

    @field_validator("name")
    @classmethod
    def validate_name(cls, v: str) -> str:
        trimmed = v.strip()
        if not trimmed:
            raise ValueError("name must not be empty")
        if len(trimmed) > 200:
            raise ValueError("name must be at most 200 characters")
        return trimmed

    @field_validator("description")
    @classmethod
    def validate_description(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return None
        trimmed = v.strip()
        return trimmed or None

    @field_validator(
        "input_price",
        "output_price",
        "cached_input_price",
        "cache_creation_price",
        "reasoning_price",
        mode="before",
    )
    @classmethod
    def validate_prices(
        cls, v: Union[str, int, float, Decimal, None], info
    ) -> Optional[str]:
        if v is None:
            return None
        return _validate_decimal_non_negative(str(v), info.field_name)

    @field_validator("pricing_currency_code")
    @classmethod
    def validate_currency_code(cls, v: str) -> str:
        code = v.strip().upper()
        if not _CURRENCY_CODE_RE.match(code):
            raise ValueError(
                "pricing_currency_code must be a 3-letter uppercase ISO code"
            )
        return code


class PricingTemplateUpdate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_updated_at: datetime
    name: Optional[str] = None
    description: Optional[str] = None
    pricing_unit: Optional[Literal["PER_1M"]] = None
    pricing_currency_code: Optional[str] = None
    input_price: Optional[str] = None
    output_price: Optional[str] = None
    cached_input_price: Optional[str] = None
    cache_creation_price: Optional[str] = None
    reasoning_price: Optional[str] = None

    @field_validator("name")
    @classmethod
    def validate_name(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return None
        trimmed = v.strip()
        if not trimmed:
            raise ValueError("name must not be empty")
        if len(trimmed) > 200:
            raise ValueError("name must be at most 200 characters")
        return trimmed

    @field_validator("description")
    @classmethod
    def validate_description(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return None
        trimmed = v.strip()
        return trimmed or None

    @field_validator(
        "input_price",
        "output_price",
        "cached_input_price",
        "cache_creation_price",
        "reasoning_price",
        mode="before",
    )
    @classmethod
    def validate_prices(
        cls, v: Union[str, int, float, Decimal, None], info
    ) -> Optional[str]:
        if v is None:
            return None
        return _validate_decimal_non_negative(str(v), info.field_name)

    @field_validator("pricing_currency_code")
    @classmethod
    def validate_currency_code(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return None
        code = v.strip().upper()
        if not _CURRENCY_CODE_RE.match(code):
            raise ValueError(
                "pricing_currency_code must be a 3-letter uppercase ISO code"
            )
        return code


class PricingTemplateListItem(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    profile_id: int
    name: str
    description: Optional[str]
    pricing_unit: Literal["PER_1M"]
    pricing_currency_code: str
    version: int
    updated_at: datetime


class PricingTemplateResponse(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    profile_id: int
    name: str
    description: Optional[str]
    pricing_unit: Literal["PER_1M"]
    pricing_currency_code: str
    input_price: str
    output_price: str
    cached_input_price: Optional[str]
    cache_creation_price: Optional[str]
    reasoning_price: Optional[str]
    version: int
    created_at: datetime
    updated_at: datetime


class PricingTemplateConnectionUsageItem(BaseModel):
    connection_id: int
    connection_name: Optional[str]
    model_config_id: int
    model_id: str
    endpoint_id: int
    endpoint_name: str


class PricingTemplateConnectionsResponse(BaseModel):
    template_id: int
    items: list[PricingTemplateConnectionUsageItem]


class ConnectionPricingTemplateUpdate(BaseModel):
    pricing_template_id: Optional[int] = None


class ConnectionPricingTemplateSummary(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    name: str
    pricing_unit: Literal["PER_1M"]
    pricing_currency_code: str
    version: int


__all__ = [
    "ConnectionPricingTemplateSummary",
    "ConnectionPricingTemplateUpdate",
    "ConnectionPriorityMoveRequest",
    "EndpointBase",
    "EndpointCreate",
    "EndpointPositionMoveRequest",
    "EndpointResponse",
    "EndpointUpdate",
    "PricingTemplateConnectionUsageItem",
    "PricingTemplateConnectionsResponse",
    "PricingTemplateCreate",
    "PricingTemplateListItem",
    "PricingTemplateResponse",
    "PricingTemplateUpdate",
]
