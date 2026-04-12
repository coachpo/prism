from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
import time
from typing import Awaitable, Callable, Optional

import httpx

from app.models.models import Connection
from app.services.loadbalancer.executor import (
    AttemptExecutionResult,
    ExecutionCandidate,
    PreparedExecutionResponse,
)
from app.services.loadbalancer.limiter import LeaseKind, LimiterAcquireResult

from .request_setup import ProxyRequestSetup


@dataclass
class ProxyRuntimeDependencies:
    build_upstream_headers_fn: Callable[..., dict[str, str]]
    build_upstream_url_fn: Callable[..., str]
    acquire_connection_limit_fn: Callable[..., Awaitable[LimiterAcquireResult]]
    claim_probe_eligible_fn: Callable[..., Awaitable[None]]
    clear_connection_state_fn: Callable[..., Awaitable[bool]]
    filter_response_headers_fn: Callable[..., dict[str, str]]
    heartbeat_connection_lease_fn: Callable[..., Awaitable[bool]]
    log_request_fn: Callable[..., Awaitable[Optional[int]]]
    log_usage_request_event_fn: Callable[..., Awaitable[Optional[int]]]
    record_connection_failure_fn: Callable[..., Awaitable[None]]
    record_connection_recovery_fn: Callable[..., Awaitable[None]]
    proxy_request_fn: Callable[..., Awaitable[httpx.Response]]
    proxy_stream_fn: Callable[..., Awaitable[httpx.Response]]
    record_audit_log_fn: Callable[..., Awaitable[None]]
    release_connection_lease_fn: Callable[..., Awaitable[bool]]
    should_failover_fn: Callable[[int, Sequence[int]], bool]


@dataclass
class ProxyRequestState:
    profile_id: int
    request_path: str
    request_started_at_monotonic: float
    setup: ProxyRequestSetup

    def completion_duration_ms(self) -> int:
        return int((time.monotonic() - self.request_started_at_monotonic) * 1000)


@dataclass
class ProxyAttemptTarget:
    attempt_number: int
    connection: Connection
    description: str
    endpoint_body: Optional[bytes]
    headers: dict[str, str]
    limiter_lease_token: Optional[str]
    limiter_lease_ttl_seconds: Optional[int]
    upstream_url: str


__all__ = [
    "AttemptExecutionResult",
    "ExecutionCandidate",
    "PreparedExecutionResponse",
    "ProxyAttemptTarget",
    "ProxyRequestState",
    "ProxyRuntimeDependencies",
]
