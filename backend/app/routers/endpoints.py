from typing import Annotated
from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.dependencies import get_db
from app.models.models import Endpoint, ModelConfig
from app.schemas.schemas import (
    EndpointCreate,
    EndpointUpdate,
    EndpointResponse,
    EndpointHealthResponse,
)

router = APIRouter(tags=["endpoints"])


@router.get(
    "/api/models/{model_config_id}/endpoints", response_model=list[EndpointResponse]
)
async def list_endpoints(
    model_config_id: int, db: Annotated[AsyncSession, Depends(get_db)]
):
    # Verify model exists
    model = await db.get(ModelConfig, model_config_id)
    if not model:
        raise HTTPException(status_code=404, detail="Model configuration not found")

    result = await db.execute(
        select(Endpoint)
        .where(Endpoint.model_config_id == model_config_id)
        .order_by(Endpoint.priority)
    )
    return result.scalars().all()


@router.post(
    "/api/models/{model_config_id}/endpoints",
    response_model=EndpointResponse,
    status_code=201,
)
async def create_endpoint(
    model_config_id: int,
    body: EndpointCreate,
    db: Annotated[AsyncSession, Depends(get_db)],
):
    model = await db.get(ModelConfig, model_config_id)
    if not model:
        raise HTTPException(status_code=404, detail="Model configuration not found")

    endpoint = Endpoint(
        model_config_id=model_config_id,
        base_url=body.base_url,
        api_key=body.api_key,
        is_active=body.is_active,
        priority=body.priority,
        description=body.description,
    )
    db.add(endpoint)
    await db.flush()
    await db.refresh(endpoint)
    return endpoint


@router.put("/api/endpoints/{endpoint_id}", response_model=EndpointResponse)
async def update_endpoint(
    endpoint_id: int,
    body: EndpointUpdate,
    db: Annotated[AsyncSession, Depends(get_db)],
):
    endpoint = await db.get(Endpoint, endpoint_id)
    if not endpoint:
        raise HTTPException(status_code=404, detail="Endpoint not found")

    update_data = body.model_dump(exclude_unset=True)
    for key, value in update_data.items():
        setattr(endpoint, key, value)
    endpoint.updated_at = datetime.utcnow()
    await db.flush()
    await db.refresh(endpoint)
    return endpoint


@router.delete("/api/endpoints/{endpoint_id}", status_code=204)
async def delete_endpoint(
    endpoint_id: int, db: Annotated[AsyncSession, Depends(get_db)]
):
    endpoint = await db.get(Endpoint, endpoint_id)
    if not endpoint:
        raise HTTPException(status_code=404, detail="Endpoint not found")
    await db.delete(endpoint)


@router.post(
    "/api/endpoints/{endpoint_id}/reset-health", response_model=EndpointHealthResponse
)
async def reset_endpoint_health(
    endpoint_id: int, db: Annotated[AsyncSession, Depends(get_db)]
):
    endpoint = await db.get(Endpoint, endpoint_id)
    if not endpoint:
        raise HTTPException(status_code=404, detail="Endpoint not found")

    endpoint.health_status = "unknown"
    endpoint.success_count = 0
    endpoint.failure_count = 0
    endpoint.updated_at = datetime.utcnow()
    await db.flush()

    return EndpointHealthResponse(
        id=endpoint.id,
        health_status=endpoint.health_status,
        success_count=endpoint.success_count,
        failure_count=endpoint.failure_count,
    )
