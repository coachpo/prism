import json
import logging
from typing import AsyncGenerator

import httpx

from app.models.models import Endpoint

logger = logging.getLogger(__name__)

PROVIDER_AUTH = {
    "openai": {
        "auth_header": "Authorization",
        "auth_prefix": "Bearer ",
        "extra_headers": {},
    },
    "anthropic": {
        "auth_header": "x-api-key",
        "auth_prefix": "",
        "extra_headers": {
            "anthropic-version": "2023-06-01",
        },
    },
    "gemini": {
        "auth_header": "Authorization",
        "auth_prefix": "Bearer ",
        "extra_headers": {},
    },
}

FAILOVER_STATUS_CODES = {429, 500, 502, 503, 529}

# Hop-by-hop headers that MUST NOT be forwarded (RFC 2616 §13.5.1)
HOP_BY_HOP_HEADERS = frozenset(
    {
        "connection",
        "keep-alive",
        "proxy-authenticate",
        "proxy-authorization",
        "te",
        "trailers",
        "transfer-encoding",
        "upgrade",
        "host",
    }
)


def build_upstream_url(endpoint: Endpoint, request_path: str) -> str:
    """Forward the exact request path to the endpoint's base URL."""
    base = endpoint.base_url.rstrip("/")
    path = request_path if request_path.startswith("/") else f"/{request_path}"
    return f"{base}{path}"


def build_upstream_headers(
    endpoint: Endpoint,
    provider_type: str,
    client_headers: dict[str, str] | None = None,
) -> dict[str, str]:
    """Build headers for the upstream request.

    Starts with forwarded client headers (minus hop-by-hop),
    then layers on auth and provider-specific headers which take precedence.
    """
    headers: dict[str, str] = {}

    if client_headers:
        for key, value in client_headers.items():
            if (
                key.lower() not in HOP_BY_HOP_HEADERS
                and key.lower() != "content-length"
            ):
                headers[key] = value

    config = PROVIDER_AUTH.get(provider_type, PROVIDER_AUTH["openai"])
    headers[config["auth_header"]] = f"{config['auth_prefix']}{endpoint.api_key}"
    headers.update(config["extra_headers"])

    return headers


def filter_response_headers(response_headers: httpx.Headers) -> dict[str, str]:
    """Filter upstream response headers, removing hop-by-hop headers."""
    filtered: dict[str, str] = {}
    for key, value in response_headers.items():
        if key.lower() not in HOP_BY_HOP_HEADERS and key.lower() != "content-length":
            filtered[key] = value
    return filtered


async def proxy_request(
    client: httpx.AsyncClient,
    method: str,
    upstream_url: str,
    headers: dict[str, str],
    raw_body: bytes | None,
) -> httpx.Response:
    """Send a non-streaming request to the upstream provider."""
    kwargs: dict = {"headers": headers}
    if raw_body:
        kwargs["content"] = raw_body
    return await client.request(method, upstream_url, **kwargs)


async def proxy_stream(
    client: httpx.AsyncClient,
    method: str,
    upstream_url: str,
    headers: dict[str, str],
    raw_body: bytes | None,
) -> AsyncGenerator[tuple[bytes, httpx.Headers, int], None]:
    """Stream a response from the upstream provider.

    Yields (chunk, response_headers, status_code).
    On error, raises HTTPStatusError with the raw upstream response.
    """
    kwargs: dict = {"headers": headers}
    if raw_body:
        kwargs["content"] = raw_body
    async with client.stream(method, upstream_url, **kwargs) as response:
        if response.status_code >= 400:
            await response.aread()
            raise httpx.HTTPStatusError(
                f"Upstream error: {response.status_code}",
                request=response.request,
                response=response,
            )
        async for chunk in response.aiter_bytes():
            if chunk:
                yield chunk, response.headers, response.status_code


def should_failover(status_code: int) -> bool:
    return status_code in FAILOVER_STATUS_CODES


def extract_model_from_body(raw_body: bytes) -> str | None:
    """Extract the model ID from the raw request body bytes.

    Parses JSON minimally just to read the 'model' key.
    """
    try:
        parsed = json.loads(raw_body)
        return parsed.get("model")
    except (json.JSONDecodeError, UnicodeDecodeError):
        return None


def extract_stream_flag(raw_body: bytes) -> bool:
    try:
        parsed = json.loads(raw_body)
        return bool(parsed.get("stream", False))
    except (json.JSONDecodeError, UnicodeDecodeError):
        return False
