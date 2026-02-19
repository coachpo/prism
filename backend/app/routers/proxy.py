import logging
from typing import Annotated

import httpx
from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.responses import StreamingResponse, JSONResponse
from sqlalchemy.ext.asyncio import AsyncSession

from app.dependencies import get_db
from app.services.loadbalancer import (
    get_model_config_with_endpoints,
    select_endpoint,
    get_failover_candidates,
    record_success,
    record_failure,
)
from app.services.proxy_service import (
    build_upstream_url,
    build_upstream_headers,
    proxy_request,
    proxy_stream,
    should_failover,
    extract_model_from_body,
)

logger = logging.getLogger(__name__)

router = APIRouter(tags=["proxy"])


async def _handle_proxy(
    request: Request,
    db: AsyncSession,
    body: dict,
    request_path: str,
):
    """Core proxy logic shared between OpenAI and Anthropic endpoints."""
    model_id = extract_model_from_body(body)
    if not model_id:
        raise HTTPException(
            status_code=400, detail="Missing 'model' field in request body"
        )

    model_config = await get_model_config_with_endpoints(db, model_id)
    if not model_config:
        raise HTTPException(
            status_code=404, detail=f"Model '{model_id}' not configured or disabled"
        )

    provider_type = model_config.provider.provider_type
    client: httpx.AsyncClient = request.app.state.http_client
    is_streaming = body.get("stream", False)

    # Select primary endpoint
    endpoint = select_endpoint(model_config)
    if not endpoint:
        raise HTTPException(
            status_code=503, detail=f"No active endpoints for model '{model_id}'"
        )

    # Try primary endpoint, then failover candidates
    endpoints_to_try = [endpoint] + get_failover_candidates(model_config, endpoint.id)

    last_error = None
    for ep in endpoints_to_try:
        upstream_url = build_upstream_url(ep, provider_type, request_path)
        headers = build_upstream_headers(ep, provider_type)

        try:
            if is_streaming:
                # For streaming, we return immediately — can't failover mid-stream
                async def stream_with_tracking():
                    try:
                        async for chunk in proxy_stream(
                            client, upstream_url, headers, body
                        ):
                            yield chunk
                        await record_success(db, ep)
                        await db.commit()
                    except Exception as e:
                        await record_failure(db, ep)
                        await db.commit()
                        logger.error(f"Stream error on endpoint {ep.id}: {e}")
                        raise

                return StreamingResponse(
                    stream_with_tracking(),
                    media_type="text/event-stream",
                    headers={
                        "Cache-Control": "no-cache",
                        "X-Accel-Buffering": "no",
                    },
                )
            else:
                response = await proxy_request(client, upstream_url, headers, body)

                if response.status_code >= 400 and should_failover(
                    response.status_code
                ):
                    await record_failure(db, ep)
                    last_error = f"Upstream returned {response.status_code}"
                    logger.warning(
                        f"Endpoint {ep.id} failed with {response.status_code}, trying next"
                    )
                    continue

                if response.status_code >= 400:
                    # Non-failover error — return it directly
                    await record_failure(db, ep)
                    return JSONResponse(
                        status_code=response.status_code,
                        content=response.json(),
                    )

                await record_success(db, ep)
                return JSONResponse(
                    status_code=response.status_code,
                    content=response.json(),
                )

        except httpx.ConnectError as e:
            await record_failure(db, ep)
            last_error = f"Connection error: {e}"
            logger.warning(f"Endpoint {ep.id} connection failed: {e}")
            continue
        except httpx.TimeoutException as e:
            await record_failure(db, ep)
            last_error = f"Timeout: {e}"
            logger.warning(f"Endpoint {ep.id} timed out: {e}")
            continue
        except httpx.HTTPStatusError as e:
            await record_failure(db, ep)
            last_error = f"HTTP error: {e}"
            logger.warning(f"Endpoint {ep.id} HTTP error: {e}")
            continue

    raise HTTPException(
        status_code=502,
        detail=f"All endpoints failed for model '{model_id}'. Last error: {last_error}",
    )


@router.post("/v1/chat/completions")
async def proxy_chat_completions(
    request: Request,
    db: Annotated[AsyncSession, Depends(get_db)],
):
    """Proxy for OpenAI-compatible chat completions (OpenAI, Gemini)."""
    body = await request.json()
    return await _handle_proxy(request, db, body, "/v1/chat/completions")


@router.post("/v1/messages")
async def proxy_messages(
    request: Request,
    db: Annotated[AsyncSession, Depends(get_db)],
):
    """Proxy for Anthropic Messages API."""
    body = await request.json()
    return await _handle_proxy(request, db, body, "/v1/messages")
