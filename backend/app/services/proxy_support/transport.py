from collections.abc import Sequence

import httpx


async def proxy_request(
    client: httpx.AsyncClient,
    method: str,
    upstream_url: str,
    headers: dict[str, str],
    raw_body: bytes | None,
) -> httpx.Response:
    if raw_body is None:
        send_req = client.build_request(
            method,
            upstream_url,
            headers=headers,
        )
    else:
        send_req = client.build_request(
            method,
            upstream_url,
            headers=headers,
            content=raw_body,
        )
    return await client.send(send_req, follow_redirects=True)


async def proxy_stream(
    client: httpx.AsyncClient,
    method: str,
    upstream_url: str,
    headers: dict[str, str],
    raw_body: bytes | None,
):
    if raw_body is None:
        send_req = client.build_request(
            method,
            upstream_url,
            headers=headers,
        )
    else:
        send_req = client.build_request(
            method,
            upstream_url,
            headers=headers,
            content=raw_body,
        )
    return await client.send(send_req, follow_redirects=True, stream=True)


def should_failover(status_code: int, failover_status_codes: Sequence[int]) -> bool:
    return status_code in failover_status_codes


__all__ = ["proxy_request", "proxy_stream", "should_failover"]
