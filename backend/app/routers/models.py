from typing import Annotated
from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from app.dependencies import get_db
from app.models.models import ModelConfig, Provider
from app.schemas.schemas import (
    ModelConfigCreate,
    ModelConfigUpdate,
    ModelConfigResponse,
    ModelConfigListResponse,
    ProviderResponse,
)

router = APIRouter(prefix="/api/models", tags=["models"])


@router.get("", response_model=list[ModelConfigListResponse])
async def list_models(db: Annotated[AsyncSession, Depends(get_db)]):
    result = await db.execute(
        select(ModelConfig)
        .options(
            selectinload(ModelConfig.provider), selectinload(ModelConfig.endpoints)
        )
        .order_by(ModelConfig.id)
    )
    configs = result.scalars().all()

    response = []
    for config in configs:
        response.append(
            ModelConfigListResponse(
                id=config.id,
                provider_id=config.provider_id,
                provider=ProviderResponse.model_validate(config.provider),
                model_id=config.model_id,
                display_name=config.display_name,
                lb_strategy=config.lb_strategy,
                is_enabled=config.is_enabled,
                endpoint_count=len(config.endpoints),
                active_endpoint_count=sum(1 for ep in config.endpoints if ep.is_active),
                created_at=config.created_at,
                updated_at=config.updated_at,
            )
        )
    return response


@router.get("/{model_config_id}", response_model=ModelConfigResponse)
async def get_model(model_config_id: int, db: Annotated[AsyncSession, Depends(get_db)]):
    result = await db.execute(
        select(ModelConfig)
        .options(
            selectinload(ModelConfig.provider), selectinload(ModelConfig.endpoints)
        )
        .where(ModelConfig.id == model_config_id)
    )
    config = result.scalar_one_or_none()
    if not config:
        raise HTTPException(status_code=404, detail="Model configuration not found")
    return config


@router.post("", response_model=ModelConfigResponse, status_code=201)
async def create_model(
    body: ModelConfigCreate, db: Annotated[AsyncSession, Depends(get_db)]
):
    # Check provider exists
    provider = await db.get(Provider, body.provider_id)
    if not provider:
        raise HTTPException(status_code=400, detail="Provider not found")

    # Check model_id uniqueness
    existing = await db.execute(
        select(ModelConfig).where(ModelConfig.model_id == body.model_id)
    )
    if existing.scalar_one_or_none():
        raise HTTPException(
            status_code=409, detail=f"Model ID '{body.model_id}' already exists"
        )

    config = ModelConfig(
        provider_id=body.provider_id,
        model_id=body.model_id,
        display_name=body.display_name,
        lb_strategy=body.lb_strategy,
        is_enabled=body.is_enabled,
    )
    db.add(config)
    await db.flush()

    # Reload with relationships
    result = await db.execute(
        select(ModelConfig)
        .options(
            selectinload(ModelConfig.provider), selectinload(ModelConfig.endpoints)
        )
        .where(ModelConfig.id == config.id)
    )
    return result.scalar_one()


@router.put("/{model_config_id}", response_model=ModelConfigResponse)
async def update_model(
    model_config_id: int,
    body: ModelConfigUpdate,
    db: Annotated[AsyncSession, Depends(get_db)],
):
    result = await db.execute(
        select(ModelConfig)
        .options(
            selectinload(ModelConfig.provider), selectinload(ModelConfig.endpoints)
        )
        .where(ModelConfig.id == model_config_id)
    )
    config = result.scalar_one_or_none()
    if not config:
        raise HTTPException(status_code=404, detail="Model configuration not found")

    update_data = body.model_dump(exclude_unset=True)

    if "provider_id" in update_data:
        provider = await db.get(Provider, update_data["provider_id"])
        if not provider:
            raise HTTPException(status_code=400, detail="Provider not found")

    if "model_id" in update_data and update_data["model_id"] != config.model_id:
        existing = await db.execute(
            select(ModelConfig).where(ModelConfig.model_id == update_data["model_id"])
        )
        if existing.scalar_one_or_none():
            raise HTTPException(
                status_code=409,
                detail=f"Model ID '{update_data['model_id']}' already exists",
            )

    for key, value in update_data.items():
        setattr(config, key, value)
    config.updated_at = datetime.utcnow()
    await db.flush()

    # Reload
    result = await db.execute(
        select(ModelConfig)
        .options(
            selectinload(ModelConfig.provider), selectinload(ModelConfig.endpoints)
        )
        .where(ModelConfig.id == config.id)
    )
    return result.scalar_one()


@router.delete("/{model_config_id}", status_code=204)
async def delete_model(
    model_config_id: int, db: Annotated[AsyncSession, Depends(get_db)]
):
    result = await db.execute(
        select(ModelConfig).where(ModelConfig.id == model_config_id)
    )
    config = result.scalar_one_or_none()
    if not config:
        raise HTTPException(status_code=404, detail="Model configuration not found")

    await db.delete(config)
