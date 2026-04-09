from __future__ import annotations

import asyncio

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


def test_proxy_request_applies_timeout_at_build_request_only() -> None:
    async def run() -> None:
        client = RecordingClient()
        timeout = httpx.Timeout(connect=10.0, read=20.0, write=30.0, pool=5.0)

        response = await proxy_request(
            client=client,
            method="POST",
            upstream_url="http://example.invalid/v1/chat/completions",
            headers={"x-test": "1"},
            raw_body=b"{}",
            timeout=timeout,
        )

        assert response.status_code == 200
        assert client.built_requests[0].extensions["timeout"] == {
            "connect": 10.0,
            "read": 20.0,
            "write": 30.0,
            "pool": 5.0,
        }
        assert client.send_calls == [
            {
                "request": client.built_requests[0],
                "stream": False,
                "follow_redirects": True,
            }
        ]

    asyncio.run(run())


def test_proxy_stream_applies_timeout_at_build_request_only() -> None:
    async def run() -> None:
        client = RecordingClient()
        timeout = httpx.Timeout(connect=11.0, read=21.0, write=31.0, pool=6.0)

        response = await proxy_stream(
            client=client,
            method="POST",
            upstream_url="http://example.invalid/v1/chat/completions",
            headers={"x-test": "1"},
            raw_body=b"{}",
            timeout=timeout,
        )

        assert response.status_code == 200
        assert client.built_requests[0].extensions["timeout"] == {
            "connect": 11.0,
            "read": 21.0,
            "write": 31.0,
            "pool": 6.0,
        }
        assert client.send_calls == [
            {
                "request": client.built_requests[0],
                "stream": True,
                "follow_redirects": True,
            }
        ]

    asyncio.run(run())
