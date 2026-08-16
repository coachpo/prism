"""HTTP access to the running gateway: runtime proxy plane and management plane.

Uses only the standard library. Streaming responses are read incrementally so a
Server-Sent Events body can be asserted on event by event rather than as an
opaque blob.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field

DEFAULT_TIMEOUT = 30.0
UPSTREAM_TIMEOUT = 180.0


@dataclass
class Response:
    status: int
    headers: dict[str, str]
    body: bytes
    elapsed_s: float = 0.0
    # Populated for streamed reads.
    sse_events: list[str] = field(default_factory=list)
    transport_error: str | None = None

    @property
    def text(self) -> str:
        return self.body.decode("utf-8", errors="replace")

    def json(self) -> object | None:
        try:
            return json.loads(self.body.decode("utf-8"))
        except (ValueError, UnicodeDecodeError):
            return None

    def header(self, name: str) -> str | None:
        return self.headers.get(name.lower())


def _build(method: str, url: str, headers: dict[str, str] | None, body: object | None) -> urllib.request.Request:
    payload: bytes | None = None
    merged = dict(headers or {})
    if body is not None:
        if isinstance(body, (bytes, bytearray)):
            payload = bytes(body)
        else:
            payload = json.dumps(body).encode("utf-8")
            merged.setdefault("Content-Type", "application/json")
    return urllib.request.Request(url, data=payload, headers=merged, method=method)


def request(
    method: str,
    url: str,
    *,
    headers: dict[str, str] | None = None,
    body: object | None = None,
    timeout: float = DEFAULT_TIMEOUT,
) -> Response:
    """Perform one request. HTTP error statuses are returned, not raised."""
    import time

    started = time.monotonic()
    prepared = _build(method, url, headers, body)
    try:
        with urllib.request.urlopen(prepared, timeout=timeout) as raw:
            return Response(
                status=raw.status,
                headers={key.lower(): value for key, value in raw.headers.items()},
                body=raw.read(),
                elapsed_s=time.monotonic() - started,
            )
    except urllib.error.HTTPError as error:
        return Response(
            status=error.code,
            headers={key.lower(): value for key, value in error.headers.items()} if error.headers else {},
            body=error.read(),
            elapsed_s=time.monotonic() - started,
        )
    except (urllib.error.URLError, OSError, TimeoutError) as error:
        # A transport failure is its own state. It must stay distinguishable
        # from an HTTP error and from an empty body.
        return Response(
            status=0,
            headers={},
            body=b"",
            elapsed_s=time.monotonic() - started,
            transport_error=str(error),
        )


def stream(
    method: str,
    url: str,
    *,
    headers: dict[str, str] | None = None,
    body: object | None = None,
    timeout: float = UPSTREAM_TIMEOUT,
    max_bytes: int = 8 * 1024 * 1024,
) -> Response:
    """Read a streaming response, splitting the SSE frames as they arrive."""
    import time

    started = time.monotonic()
    prepared = _build(method, url, headers, body)
    collected = bytearray()
    try:
        with urllib.request.urlopen(prepared, timeout=timeout) as raw:
            status = raw.status
            response_headers = {key.lower(): value for key, value in raw.headers.items()}
            while len(collected) < max_bytes:
                chunk = raw.read(4096)
                if not chunk:
                    break
                collected.extend(chunk)
    except urllib.error.HTTPError as error:
        return Response(
            status=error.code,
            headers={key.lower(): value for key, value in error.headers.items()} if error.headers else {},
            body=error.read(),
            elapsed_s=time.monotonic() - started,
        )
    except (urllib.error.URLError, OSError, TimeoutError) as error:
        return Response(
            status=0,
            headers={},
            body=bytes(collected),
            elapsed_s=time.monotonic() - started,
            transport_error=str(error),
        )

    raw_text = bytes(collected).decode("utf-8", errors="replace")
    events = [frame for frame in raw_text.split("\n\n") if frame.strip()]
    return Response(
        status=status,
        headers=response_headers,
        body=bytes(collected),
        elapsed_s=time.monotonic() - started,
        sse_events=events,
    )


@dataclass
class Gateway:
    """Typed access to both planes of one running Prism instance."""

    runtime_base: str
    management_base: str
    proxy_key: str | None = field(default=None, repr=False)

    # --- runtime proxy plane -------------------------------------------------

    def _runtime_headers(self, *, authorize: bool, override_key: str | None = None) -> dict[str, str]:
        headers = {"Content-Type": "application/json"}
        key = override_key if override_key is not None else self.proxy_key
        if authorize and key:
            headers["Authorization"] = f"Bearer {key}"
        return headers

    def chat(
        self,
        model: str,
        prompt: str,
        *,
        max_tokens: int = 32,
        streaming: bool = False,
        authorize: bool = True,
        override_key: str | None = None,
        timeout: float = UPSTREAM_TIMEOUT,
    ) -> Response:
        payload = {
            "model": model,
            "messages": [{"role": "user", "content": prompt}],
            "max_tokens": max_tokens,
        }
        if streaming:
            payload["stream"] = True
        url = f"{self.runtime_base}/v1/chat/completions"
        headers = self._runtime_headers(authorize=authorize, override_key=override_key)
        if streaming:
            return stream("POST", url, headers=headers, body=payload, timeout=timeout)
        return request("POST", url, headers=headers, body=payload, timeout=timeout)

    def responses(self, model: str, prompt: str, *, timeout: float = UPSTREAM_TIMEOUT) -> Response:
        return request(
            "POST",
            f"{self.runtime_base}/v1/responses",
            headers=self._runtime_headers(authorize=True),
            body={"model": model, "input": prompt},
            timeout=timeout,
        )

    def runtime_raw(
        self,
        method: str,
        path: str,
        *,
        body: object | None = None,
        authorize: bool = True,
        timeout: float = DEFAULT_TIMEOUT,
    ) -> Response:
        return request(
            method,
            f"{self.runtime_base}{path}",
            headers=self._runtime_headers(authorize=authorize),
            body=body,
            timeout=timeout,
        )

    def health(self) -> Response:
        return request("GET", f"{self.runtime_base}/health", timeout=10.0)

    # --- management plane ----------------------------------------------------

    def management(
        self,
        method: str,
        path: str,
        *,
        query: dict[str, str] | None = None,
        body: object | None = None,
        timeout: float = DEFAULT_TIMEOUT,
    ) -> Response:
        url = f"{self.management_base}{path}"
        if query:
            url = f"{url}?{urllib.parse.urlencode(query)}"
        return request(method, url, body=body, timeout=timeout)
