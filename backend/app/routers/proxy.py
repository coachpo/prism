import logging
from typing import Annotated, AsyncGenerator

import httpx
from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.responses import Response, StreamingResponse
from sqlalchemy.ext.asyncio import AsyncSession

from app.dependencies import get_db
from app.models.models import Endpoint
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
    should_failover,
    extract_model_from_body,
    extract_stream_flag,
    filter_response_headers,
)

logger = logging.getLogger(__name__)

router = APIRouter(tags=["proxy"])

MODEL_ID_HEADER = "x-model-id"


def _get_client_headers(request: Request) -> dict[str, str]:
    return dict(request.headers)


def _resolve_model_id(request: Request, raw_body: bytes | None) -> str | None:
    if raw_body:
        model_id = extract_model_from_body(raw_body)
        if model_id:
            return model_id
    return request.headers.get(MODEL_ID_HEADER)


async def _iter_stream(
    resp: httpx.Response,
    db: AsyncSession,
    ep: Endpoint,
) -> AsyncGenerator[bytes, None]:
    try:
        async for chunk in resp.aiter_bytes():
            if chunk:
                yield chunk
        await record_success(db, ep)
        await db.commit()
    except Exception as e:
        await record_failure(db, ep)
        await db.commit()
        logger.error(f"Stream error on endpoint {ep.id}: {e}")
    finally:
        await resp.aclose()


async def _handle_proxy(
    request: Request,
    db: AsyncSession,
    raw_body: bytes | None,
    request_path: str,
):
    """Core proxy logic — routes any API path to the configured upstream."""
    model_id = _resolve_model_id(request, raw_body)
    if not model_id:
        raise HTTPException(
            status_code=400,
            detail=(
                "Cannot determine model for routing. "
                "Include 'model' in the request body or set the X-Model-Id header."
            ),
        )

    model_config = await get_model_config_with_endpoints(db, model_id)
    if not model_config:
        logger.warning(
            "Proxy lookup failed: model_id=%r not found or disabled (path=%s)",
            model_id,
            request_path,
        )
        raise HTTPException(
            status_code=404, detail=f"Model '{model_id}' not configured or disabled"
        )

    provider_type = model_config.provider.provider_type
    client: httpx.AsyncClient = request.app.state.http_client
    is_streaming = extract_stream_flag(raw_body) if raw_body else False
    client_headers = _get_client_headers(request)
    method = request.method

    endpoint = select_endpoint(model_config)
    if not endpoint:
        raise HTTPException(
            status_code=503, detail=f"No active endpoints for model '{model_id}'"
        )

    endpoints_to_try = [endpoint] + get_failover_candidates(model_config, endpoint.id)

    last_error = None
    for ep in endpoints_to_try:
        upstream_url = build_upstream_url(ep, request_path)
        if request.url.query:
            upstream_url = f"{upstream_url}?{request.url.query}"
        headers = build_upstream_headers(ep, provider_type, client_headers)

        try:
            if is_streaming:
                kwargs: dict = {"headers": headers}
                if raw_body:
                    kwargs["content"] = raw_body

                send_req = client.build_request(method, upstream_url, **kwargs)
                upstream_resp = await client.send(send_req, stream=True)

                resp_headers_filtered = filter_response_headers(upstream_resp.headers)

                if upstream_resp.status_code >= 400:
                    body = await upstream_resp.aread()
                    await upstream_resp.aclose()
                    await record_failure(db, ep)
                    await db.commit()

                    if should_failover(upstream_resp.status_code):
                        last_error = f"Upstream returned {upstream_resp.status_code}"
                        logger.warning(
                            f"Endpoint {ep.id} failed with {upstream_resp.status_code}, trying next"
                        )
                        continue

                    return Response(
                        content=body,
                        status_code=upstream_resp.status_code,
                        headers=resp_headers_filtered,
                    )

                media_type = upstream_resp.headers.get(
                    "content-type", "text/event-stream"
                )
                return StreamingResponse(
                    _iter_stream(upstream_resp, db, ep),
                    status_code=upstream_resp.status_code,
                    media_type=media_type,
                    headers={
                        **resp_headers_filtered,
                        "Cache-Control": "no-cache",
                        "X-Accel-Buffering": "no",
                    },
                )
            else:
                response = await proxy_request(
                    client, method, upstream_url, headers, raw_body
                )
                resp_headers = filter_response_headers(response.headers)

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
                    await record_failure(db, ep)
                    return Response(
                        content=response.content,
                        status_code=response.status_code,
                        headers=resp_headers,
                    )

                await record_success(db, ep)
                return Response(
                    content=response.content,
                    status_code=response.status_code,
                    headers=resp_headers,
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


@router.api_route("/v1/{path:path}", methods=["GET", "POST", "PUT", "PATCH", "DELETE"])
async def proxy_catch_all(
    request: Request,
    path: str,
    db: Annotated[AsyncSession, Depends(get_db)],
):
    """Catch-all transparent proxy for all /v1/* API paths."""
    raw_body = await request.body() or None
    return await _handle_proxy(request, db, raw_body, f"/v1/{path}")
