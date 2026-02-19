from datetime import datetime
from pydantic import BaseModel, ConfigDict


# --- Provider Schemas ---


class ProviderBase(BaseModel):
    name: str
    provider_type: str
    description: str | None = None


class ProviderCreate(ProviderBase):
    pass


class ProviderResponse(ProviderBase):
    model_config = ConfigDict(from_attributes=True)

    id: int
    created_at: datetime
    updated_at: datetime


# --- Endpoint Schemas ---


class EndpointBase(BaseModel):
    base_url: str
    api_key: str
    is_active: bool = True
    priority: int = 0
    description: str | None = None


class EndpointCreate(EndpointBase):
    pass


class EndpointUpdate(BaseModel):
    base_url: str | None = None
    api_key: str | None = None
    is_active: bool | None = None
    priority: int | None = None
    description: str | None = None


class EndpointResponse(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    model_config_id: int
    base_url: str
    api_key: str
    is_active: bool
    priority: int
    description: str | None
    created_at: datetime
    updated_at: datetime


# --- Model Config Schemas ---


class ModelConfigBase(BaseModel):
    provider_id: int
    model_id: str
    display_name: str | None = None
    lb_strategy: str = "single"
    is_enabled: bool = True


class ModelConfigCreate(ModelConfigBase):
    pass


class ModelConfigUpdate(BaseModel):
    provider_id: int | None = None
    display_name: str | None = None
    lb_strategy: str | None = None
    is_enabled: bool | None = None


class ModelConfigResponse(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    provider_id: int
    provider: ProviderResponse
    model_id: str
    display_name: str | None
    lb_strategy: str
    is_enabled: bool
    endpoints: list[EndpointResponse]
    created_at: datetime
    updated_at: datetime


class ModelConfigListResponse(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    provider_id: int
    provider: ProviderResponse
    model_id: str
    display_name: str | None
    lb_strategy: str
    is_enabled: bool
    endpoint_count: int
    active_endpoint_count: int
    created_at: datetime
    updated_at: datetime
