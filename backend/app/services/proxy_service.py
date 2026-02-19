import json
import logging
from typing import AsyncGenerator

import httpx

from app.models.models import Endpoint, ModelConfig

logger = logging.getLogger(__name__)

# Provider-specific configuration
PROVIDER_CONFIG = {
    "openai": {
        "chat_path": "/v1/chat/completions",
        "auth_header": "Authorization",
        "auth_prefix": "Bearer ",
        "extra_headers": {},
    },
    "anthropic": {
        "chat_path": "/v1/messages",
        "auth_header": "x-api-key",
        "auth_prefix": "",
        "extra_headers": {
            "anthropic-version": "2023-06-01",
        },
    },
    "gemini": {
        # Gemini's OpenAI-compatible endpoint
        "chat_path": "/v1/chat/completions",
        "auth_header": "Authorization",
        "auth_prefix": "Bearer ",
        "extra_headers": {},
    },
}

# HTTP status codes that trigger failover
FAILOVER_STATUS_CODES = {429, 500, 502, 503, 529}


def build_upstream_url(
    endpoint: Endpoint, provider_type: str, request_path: str
) -> str:
    """Build the upstream URL for a given endpoint and provider."""
    base = endpoint.base_url.rstrip("/")
    config = PROVIDER_CONFIG.get(provider_type, PROVIDER_CONFIG["openai"])

    # For Anthropic, use the messages endpoint
    if provider_type == "anthropic" and "/messages" in request_path:
        return f"{base}{config['chat_path']}"

    # For OpenAI and Gemini (OpenAI-compatible), use chat completions
    return f"{base}{config['chat_path']}"


def build_upstream_headers(endpoint: Endpoint, provider_type: str) -> dict[str, str]:
    """Build headers for the upstream request."""
    config = PROVIDER_CONFIG.get(provider_type, PROVIDER_CONFIG["openai"])
    headers = {
        "Content-Type": "application/json",
        config["auth_header"]: f"{config['auth_prefix']}{endpoint.api_key}",
    }
    headers.update(config["extra_headers"])
    return headers


async def proxy_request(
    client: httpx.AsyncClient,
    upstream_url: str,
    headers: dict[str, str],
    body: dict,
) -> httpx.Response:
    """Send a non-streaming request to the upstream provider."""
    response = await client.post(
        upstream_url,
        json=body,
        headers=headers,
    )
    return response


async def proxy_stream(
    client: httpx.AsyncClient,
    upstream_url: str,
    headers: dict[str, str],
    body: dict,
) -> AsyncGenerator[bytes, None]:
    """Stream a response from the upstream provider."""
    async with client.stream(
        "POST",
        upstream_url,
        json=body,
        headers=headers,
    ) as response:
        if response.status_code >= 400:
            error_body = await response.aread()
            raise httpx.HTTPStatusError(
                f"Upstream error: {response.status_code}",
                request=response.request,
                response=response,
            )
        async for chunk in response.aiter_bytes():
            if chunk:
                yield chunk


def should_failover(status_code: int) -> bool:
    """Check if a status code should trigger failover."""
    return status_code in FAILOVER_STATUS_CODES


def extract_model_from_body(body: dict) -> str | None:
    """Extract the model ID from the request body."""
    return body.get("model")
