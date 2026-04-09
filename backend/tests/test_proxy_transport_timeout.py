from __future__ import annotations

import asyncio
from typing import Any, cast

import httpx

from app.services.proxy_support.transport import proxy_request, proxy_stream


class RecordingClient:
    def __init__(self) -> None:
        self.built_requests: list[httpx.Request] = []
        self.send_calls: list[dict[str, object]] = []

    def build_request(
        self,
        method: str,
        url: str,
        *,
        headers: dict[str, str],
        content: bytes | None = None,
        timeout: httpx.Timeout | None = None,
    ) -> httpx.Request:
        request = httpx.Request(method, url, headers=headers, content=content)
        if timeout is not None:
            request.extensions["timeout"] = {
                "connect": timeout.connect,
                "read": timeout.read,
                "write": timeout.write,
                "pool": timeout.pool,
            }
        self.built_requests.append(request)
        return request

    async def send(
        self,
        request: httpx.Request,
        *,
        stream: bool = False,
        follow_redirects: bool = False,
    ) -> httpx.Response:
        self.send_calls.append(
            {
                "request": request,
                "stream": stream,
                "follow_redirects": follow_redirects,
            }
        )
        return httpx.Response(200, request=request)


def test_proxy_request_uses_shared_client_timeout_configuration() -> None:
    async def run() -> None:
        client = RecordingClient()

        response = await proxy_request(
            client=cast(Any, client),
            method="POST",
            upstream_url="http://example.invalid/v1/chat/completions",
            headers={"x-test": "1"},
            raw_body=b"{}",
        )

        assert response.status_code == 200
        assert "timeout" not in client.built_requests[0].extensions
        assert client.send_calls == [
            {
                "request": client.built_requests[0],
                "stream": False,
                "follow_redirects": True,
            }
        ]

    asyncio.run(run())


def test_proxy_stream_uses_shared_client_timeout_configuration() -> None:
    async def run() -> None:
        client = RecordingClient()

        response = await proxy_stream(
            client=cast(Any, client),
            method="POST",
            upstream_url="http://example.invalid/v1/chat/completions",
            headers={"x-test": "1"},
            raw_body=b"{}",
        )

        assert response.status_code == 200
        assert "timeout" not in client.built_requests[0].extensions
        assert client.send_calls == [
            {
                "request": client.built_requests[0],
                "stream": True,
                "follow_redirects": True,
            }
        ]

    asyncio.run(run())
