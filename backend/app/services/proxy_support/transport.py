from collections.abc import Sequence

import httpx


def build_upstream_timeout(endpoint: object | None) -> httpx.Timeout:
    pool_timeout = float(getattr(endpoint, "pool_timeout", 5.0) or 5.0)
    connect_timeout = float(getattr(endpoint, "connect_timeout", 10.0) or 10.0)
    write_timeout = float(getattr(endpoint, "write_timeout", 30.0) or 30.0)
    read_idle_timeout = float(getattr(endpoint, "read_idle_timeout", 120.0) or 120.0)
    return httpx.Timeout(
        connect=connect_timeout,
        read=read_idle_timeout,
        write=write_timeout,
        pool=pool_timeout,
    )


async def proxy_request(
    client: httpx.AsyncClient,
    method: str,
    upstream_url: str,
    headers: dict[str, str],
    raw_body: bytes | None,
    timeout: httpx.Timeout | None = None,
) -> httpx.Response:
    if raw_body is None:
        send_req = client.build_request(method, upstream_url, headers=headers)
    else:
        send_req = client.build_request(
            method,
            upstream_url,
            headers=headers,
            content=raw_body,
        )
    return await client.send(send_req, follow_redirects=True, timeout=timeout)


async def proxy_stream(
    client: httpx.AsyncClient,
    method: str,
    upstream_url: str,
    headers: dict[str, str],
    raw_body: bytes | None,
    timeout: httpx.Timeout | None = None,
):
    if raw_body is None:
        send_req = client.build_request(method, upstream_url, headers=headers)
    else:
        send_req = client.build_request(
            method,
            upstream_url,
            headers=headers,
            content=raw_body,
        )
    return await client.send(send_req, follow_redirects=True, stream=True, timeout=timeout)


def should_failover(status_code: int, failover_status_codes: Sequence[int]) -> bool:
    return status_code in failover_status_codes


__all__ = ["build_upstream_timeout", "proxy_request", "proxy_stream", "should_failover"]
