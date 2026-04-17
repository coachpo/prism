from __future__ import annotations

import asyncio
import os
from collections.abc import AsyncIterator, Awaitable, Callable, Mapping
from contextlib import asynccontextmanager
from dataclasses import dataclass
from typing import Any
from uuid import uuid4

from fastapi import FastAPI, WebSocket
import httpx
from sqlalchemy.engine import make_url
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from starlette.types import Message

import app.main as main_module
from app.core import database as database_core
from app.core.config import Settings, ensure_postgresql_database_url, get_settings
from app.core.crypto import encrypt_secret
from app.dependencies import PROFILE_ID_HEADER
from app.models.models import (
    Connection,
    Endpoint,
    LoadbalanceStrategy,
    ModelConfig,
    ModelProxyTarget,
    Profile,
    Vendor,
)
from app.services.auth_service import (
    clear_proxy_api_key_usage_write_buffer,
    create_proxy_api_key,
    create_session_for_auth_subject,
    get_or_create_app_auth_settings,
)
from app.services.background_tasks import BackgroundTaskManager, background_task_manager
from app.services.loadbalancer.policy import (
    build_default_auto_recovery_document,
    build_default_routing_policy_document,
)
from app.services.realtime.connection_manager import connection_manager
from app.services.stats.logging import shutdown_dashboard_update_lifecycle

SMOKE_BASE_URL = "http://testserver"
SMOKE_DEFAULT_DATABASE_PREFIX = "prism_smoke"
_SMOKE_USER_AGENT = "backend-smoke-suite"


def _normalize_name_segment(value: str) -> str:
    characters = [
        character.lower() if character.isalnum() else "_" for character in value.strip()
    ]
    normalized = "".join(characters).strip("_")
    while "__" in normalized:
        normalized = normalized.replace("__", "_")
    return normalized


def _unique_name(prefix: str) -> str:
    return f"{prefix}-{uuid4().hex[:8]}"


def _merge_headers(
    headers: Mapping[str, str] | None,
    updates: Mapping[str, str],
) -> dict[str, str]:
    merged = dict(headers or {})
    for key, value in updates.items():
        merged.setdefault(key, value)
    return merged


async def _get_existing_profile(
    db: AsyncSession,
    *,
    is_active: bool | None = None,
    is_default: bool | None = None,
) -> Profile | None:
    statement = select(Profile).where(Profile.deleted_at.is_(None))
    if is_active is not None:
        statement = statement.where(Profile.is_active.is_(is_active))
    if is_default is not None:
        statement = statement.where(Profile.is_default.is_(is_default))
    result = await db.execute(statement.order_by(Profile.id.asc()).limit(1))
    return result.scalar_one_or_none()


async def _get_reusable_profile(
    db: AsyncSession,
    *,
    description: str | None,
    is_active: bool,
    is_default: bool,
    is_editable: bool,
    name: str | None,
) -> Profile | None:
    if (
        name is not None
        or description is not None
        or not is_editable
        or not is_active
        or not is_default
    ):
        return None
    return await _get_existing_profile(db, is_active=True, is_default=True)


class _AsyncSessionContext:
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def __aenter__(self) -> AsyncSession:
        return self._session

    async def __aexit__(self, exc_type, exc, tb) -> bool:
        return False


class DeterministicUpstreamClient:
    def __init__(
        self,
        *,
        default_status_code: int = 200,
        default_json: object | None = {"ok": True},
        send_handler: Callable[[httpx.Request, bool, bool], object] | None = None,
    ) -> None:
        self.built_requests: list[httpx.Request] = []
        self.closed = False
        self.default_json = default_json
        self.default_status_code = default_status_code
        self.send_calls: list[dict[str, object]] = []
        self._queued_results: list[object] = []
        self._send_handler = send_handler

    def queue_exception(self, exc: Exception) -> None:
        self._queued_results.append(exc)

    def queue_json(
        self,
        payload: object | None,
        *,
        status_code: int = 200,
        headers: Mapping[str, str] | None = None,
    ) -> None:
        self._queued_results.append(
            httpx.Response(status_code, headers=headers, json=payload)
        )

    def queue_response(self, response: httpx.Response) -> None:
        self._queued_results.append(response)

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
                "follow_redirects": follow_redirects,
                "request": request,
                "stream": stream,
            }
        )

        if self._queued_results:
            result = self._queued_results.pop(0)
        elif self._send_handler is not None:
            result = self._send_handler(request, stream, follow_redirects)
            if isinstance(result, Awaitable):
                result = await result
        else:
            return httpx.Response(
                self.default_status_code,
                json=self.default_json,
                request=request,
            )

        if isinstance(result, Exception):
            raise result

        if not isinstance(result, httpx.Response):
            raise TypeError(
                "send_handler must return httpx.Response or raise an exception"
            )

        try:
            _ = result.request
        except RuntimeError:
            try:
                result.request = request
            except AttributeError:
                setattr(result, "_request", request)
        return result

    async def aclose(self) -> None:
        self.closed = True


class WebSocketHarness:
    def __init__(
        self,
        *,
        fail_on_send: bool = False,
        headers: list[tuple[bytes, bytes]] | None = None,
        path: str = "/api/realtime/ws",
        query_string: bytes = b"",
    ) -> None:
        self.accepted = False
        self.fail_on_send = fail_on_send
        self.sent_messages: list[Any] = []
        self._first_receive = True
        self.websocket = WebSocket(
            {
                "type": "websocket",
                "path": path,
                "headers": headers or [],
                "query_string": query_string,
                "client": ("127.0.0.1", 1234),
                "server": ("testserver", 80),
                "scheme": "ws",
                "subprotocols": [],
            },
            self._receive,
            self._send,
        )

    async def _receive(self) -> Message:
        if self._first_receive:
            self._first_receive = False
            return {"type": "websocket.connect"}
        return {"type": "websocket.disconnect", "code": 1000}

    async def _send(self, message: Message) -> None:
        if message["type"] == "websocket.accept":
            self.accepted = True
            return

        if message["type"] != "websocket.send":
            return

        if self.fail_on_send:
            raise RuntimeError("send failed")

        text_payload = message.get("text")
        if isinstance(text_payload, str):
            self.sent_messages.append(httpx.Response(200, text=text_payload).json())
            return

        self.sent_messages.append(message)


@dataclass(frozen=True)
class ManagementSession:
    access_token: str
    auth_subject_id: int
    username: str | None

    def as_cookies(self, *, cookie_name: str) -> dict[str, str]:
        return {cookie_name: self.access_token}


@dataclass(frozen=True)
class RuntimeProxyKey:
    key_id: int
    name: str
    raw_key: str

    def as_headers(self, *, header_name: str = "authorization") -> dict[str, str]:
        if header_name == "authorization":
            return {"authorization": f"Bearer {self.raw_key}"}
        if header_name in {"x-api-key", "x-goog-api-key"}:
            return {header_name: self.raw_key}
        raise ValueError(
            "header_name must be one of 'authorization', 'x-api-key', or 'x-goog-api-key'"
        )


@dataclass
class NativeConnectionGraph:
    connection: Connection
    endpoint: Endpoint
    model: ModelConfig
    profile: Profile
    strategy: LoadbalanceStrategy
    vendor: Vendor


@dataclass
class ProxyRouteGraph:
    connection: Connection
    endpoint: Endpoint
    profile: Profile
    proxy_model: ModelConfig
    proxy_target: ModelProxyTarget
    strategy: LoadbalanceStrategy
    target_model: ModelConfig
    vendor: Vendor


@dataclass
class SmokeAppHarness:
    app: FastAPI
    client: httpx.AsyncClient
    settings: Settings
    upstream: DeterministicUpstreamClient

    @asynccontextmanager
    async def db_session(self) -> AsyncIterator[AsyncSession]:
        async with database_core.AsyncSessionLocal() as session:
            yield session

    async def wait_for_background_tasks(self) -> None:
        manager = getattr(self.app.state, "background_task_manager", None)
        if isinstance(manager, BackgroundTaskManager) and manager.started:
            await manager.wait_for_idle()

    async def management_request(
        self,
        method: str,
        path: str,
        *,
        headers: Mapping[str, str] | None = None,
        cookies: Mapping[str, str] | None = None,
        profile_id: int | None = None,
        session: ManagementSession | None = None,
        **kwargs: Any,
    ) -> httpx.Response:
        request_headers = dict(headers or {})
        if profile_id is not None:
            request_headers.setdefault(PROFILE_ID_HEADER, str(profile_id))
        request_cookies = dict(cookies or {})
        if session is not None:
            request_cookies.setdefault(
                self.settings.auth_cookie_name,
                session.access_token,
            )
        return await self.client.request(
            method,
            path,
            cookies=request_cookies,
            headers=request_headers,
            **kwargs,
        )

    async def runtime_request(
        self,
        method: str,
        path: str,
        *,
        header_name: str = "authorization",
        headers: Mapping[str, str] | None = None,
        proxy_key: RuntimeProxyKey | str | None = None,
        **kwargs: Any,
    ) -> httpx.Response:
        request_headers = dict(headers or {})
        if proxy_key is not None:
            raw_key = (
                proxy_key.raw_key
                if isinstance(proxy_key, RuntimeProxyKey)
                else proxy_key
            )
            request_headers = _merge_headers(
                request_headers,
                RuntimeProxyKey(key_id=0, name="runtime", raw_key=raw_key).as_headers(
                    header_name=header_name
                ),
            )
        return await self.client.request(
            method, path, headers=request_headers, **kwargs
        )


def smoke_database_name(
    label: str,
    *,
    prefix: str = SMOKE_DEFAULT_DATABASE_PREFIX,
) -> str:
    normalized_prefix = _normalize_name_segment(prefix) or SMOKE_DEFAULT_DATABASE_PREFIX
    normalized_label = _normalize_name_segment(label)
    if not normalized_label:
        return normalized_prefix[:63]
    database_name = f"{normalized_prefix}_{normalized_label}"[:63]
    return database_name.rstrip("_")


def smoke_database_url(
    label: str,
    *,
    base_url: str | None = None,
    prefix: str = SMOKE_DEFAULT_DATABASE_PREFIX,
) -> str:
    source_url = base_url or os.getenv("DATABASE_URL") or get_settings().database_url
    ensure_postgresql_database_url(source_url)
    return str(
        make_url(source_url).set(database=smoke_database_name(label, prefix=prefix))
    )


async def reset_backend_runtime_state() -> None:
    await shutdown_dashboard_update_lifecycle()
    if background_task_manager.started:
        await background_task_manager.shutdown()
    clear_proxy_api_key_usage_write_buffer()
    connection_manager.connections.clear()
    connection_manager.rooms.clear()
    engine = getattr(database_core, "_engine", None)
    if engine is not None:
        await engine.dispose()
    database_core._engine = None
    database_core._session_factory = None
    get_settings.cache_clear()


@asynccontextmanager
async def mounted_smoke_app(
    *,
    app_env: str = "test",
    base_url: str = SMOKE_BASE_URL,
    database_url: str | None = None,
    upstream: DeterministicUpstreamClient | None = None,
) -> AsyncIterator[SmokeAppHarness]:
    env_updates = {"APP_ENV": app_env}
    if database_url is not None:
        env_updates["DATABASE_URL"] = database_url

    previous_env = {key: os.environ.get(key) for key in env_updates}
    for key, value in env_updates.items():
        os.environ[key] = value

    await reset_backend_runtime_state()
    active_upstream = upstream or DeterministicUpstreamClient()
    original_build_http_client = main_module.bootstrap.build_http_client
    main_module.bootstrap.build_http_client = lambda: active_upstream

    try:
        settings = get_settings()
        app = main_module._create_app(settings)
        async with main_module.lifespan(app):
            transport = httpx.ASGITransport(app=app)
            async with httpx.AsyncClient(
                base_url=base_url,
                follow_redirects=True,
                transport=transport,
            ) as client:
                yield SmokeAppHarness(
                    app=app,
                    client=client,
                    settings=settings,
                    upstream=active_upstream,
                )
    finally:
        main_module.bootstrap.build_http_client = original_build_http_client
        for key, value in previous_env.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
        await reset_backend_runtime_state()


async def seed_profile(
    db: AsyncSession,
    *,
    description: str | None = None,
    is_active: bool = False,
    is_default: bool = False,
    is_editable: bool = True,
    name: str | None = None,
) -> Profile:
    reusable_profile = await _get_reusable_profile(
        db,
        description=description,
        is_active=is_active,
        is_default=is_default,
        is_editable=is_editable,
        name=name,
    )
    if reusable_profile is not None:
        return reusable_profile

    if is_default:
        existing_default = await _get_existing_profile(db, is_default=True)
        if existing_default is not None:
            raise ValueError(
                "A default profile already exists; reuse the seeded profile or set is_default=False"
            )

    if is_active:
        existing_active = await _get_existing_profile(db, is_active=True)
        if existing_active is not None:
            raise ValueError(
                "An active profile already exists; reuse the seeded profile or set is_active=False"
            )

    profile = Profile(
        description=description,
        is_active=is_active,
        is_default=is_default,
        is_editable=is_editable,
        name=name or _unique_name("Smoke Profile"),
    )
    db.add(profile)
    await db.flush()
    return profile


async def seed_vendor(
    db: AsyncSession,
    *,
    audit_capture_bodies: bool = False,
    audit_enabled: bool = False,
    description: str | None = None,
    key: str | None = None,
    name: str | None = None,
) -> Vendor:
    vendor_key = key or _unique_name("smoke-vendor")
    vendor = Vendor(
        audit_capture_bodies=audit_capture_bodies,
        audit_enabled=audit_enabled,
        description=description,
        key=vendor_key,
        name=name or vendor_key.title(),
    )
    db.add(vendor)
    await db.flush()
    return vendor


async def seed_loadbalance_strategy(
    db: AsyncSession,
    *,
    auto_recovery: dict[str, object] | None = None,
    legacy_strategy_type: str = "single",
    name: str | None = None,
    profile_id: int,
    routing_policy: dict[str, object] | None = None,
    strategy_type: str = "legacy",
) -> LoadbalanceStrategy:
    if strategy_type == "legacy":
        strategy = LoadbalanceStrategy(
            auto_recovery=auto_recovery or build_default_auto_recovery_document(),
            legacy_strategy_type=legacy_strategy_type,
            name=name or _unique_name("Smoke legacy strategy"),
            profile_id=profile_id,
            routing_policy=None,
            strategy_type="legacy",
        )
    elif strategy_type == "adaptive":
        strategy = LoadbalanceStrategy(
            auto_recovery=None,
            legacy_strategy_type=None,
            name=name or _unique_name("Smoke adaptive strategy"),
            profile_id=profile_id,
            routing_policy=routing_policy or build_default_routing_policy_document(),
            strategy_type="adaptive",
        )
    else:
        raise ValueError("strategy_type must be 'legacy' or 'adaptive'")
    db.add(strategy)
    await db.flush()
    return strategy


async def seed_model(
    db: AsyncSession,
    *,
    api_family: str = "openai",
    display_name: str | None = None,
    is_enabled: bool = True,
    loadbalance_strategy_id: int | None = None,
    model_id: str | None = None,
    model_type: str = "native",
    profile_id: int,
    vendor_id: int | None,
) -> ModelConfig:
    if model_type == "native" and loadbalance_strategy_id is None:
        raise ValueError("Native models require loadbalance_strategy_id")

    resolved_model_id = model_id or _unique_name("smoke-model")
    model = ModelConfig(
        api_family=api_family,
        display_name=display_name or resolved_model_id,
        is_enabled=is_enabled,
        loadbalance_strategy_id=loadbalance_strategy_id,
        model_id=resolved_model_id,
        model_type=model_type,
        profile_id=profile_id,
        vendor_id=vendor_id,
    )
    db.add(model)
    await db.flush()
    return model


async def seed_proxy_target(
    db: AsyncSession,
    *,
    position: int = 1,
    source_model_config_id: int,
    target_model_config_id: int,
) -> ModelProxyTarget:
    proxy_target = ModelProxyTarget(
        position=position,
        source_model_config_id=source_model_config_id,
        target_model_config_id=target_model_config_id,
    )
    db.add(proxy_target)
    await db.flush()
    return proxy_target


async def seed_endpoint(
    db: AsyncSession,
    *,
    api_key: str = "smoke-api-key",
    base_url: str = "https://smoke.invalid",
    name: str | None = None,
    position: int = 1,
    profile_id: int,
) -> Endpoint:
    endpoint = Endpoint(
        api_key=encrypt_secret(api_key),
        base_url=base_url,
        name=name or _unique_name("Smoke endpoint"),
        position=position,
        profile_id=profile_id,
    )
    db.add(endpoint)
    await db.flush()
    return endpoint


async def seed_connection(
    db: AsyncSession,
    *,
    auth_type: str | None = None,
    custom_headers: str | None = None,
    endpoint_id: int,
    health_status: str = "healthy",
    is_active: bool = True,
    model_config_id: int,
    name: str | None = None,
    pricing_template_id: int | None = None,
    priority: int = 0,
    profile_id: int,
) -> Connection:
    connection = Connection(
        auth_type=auth_type,
        custom_headers=custom_headers,
        endpoint_id=endpoint_id,
        health_status=health_status,
        is_active=is_active,
        model_config_id=model_config_id,
        name=name or _unique_name("Smoke connection"),
        pricing_template_id=pricing_template_id,
        priority=priority,
        profile_id=profile_id,
    )
    db.add(connection)
    await db.flush()
    return connection


async def seed_native_connection_graph(
    db: AsyncSession,
    *,
    api_family: str = "openai",
    commit: bool = True,
    endpoint_api_key: str = "smoke-api-key",
    endpoint_base_url: str = "https://smoke.invalid",
    endpoint_name: str | None = None,
    is_active_profile: bool = True,
    is_default_profile: bool = True,
    legacy_strategy_type: str = "single",
    model_display_name: str | None = None,
    model_id: str | None = None,
    profile_name: str | None = None,
    strategy_name: str | None = None,
    strategy_type: str = "legacy",
    vendor_key: str | None = None,
    vendor_name: str | None = None,
) -> NativeConnectionGraph:
    profile = await seed_profile(
        db,
        is_active=is_active_profile,
        is_default=is_default_profile,
        name=profile_name,
    )
    vendor = await seed_vendor(db, key=vendor_key, name=vendor_name)
    strategy = await seed_loadbalance_strategy(
        db,
        legacy_strategy_type=legacy_strategy_type,
        name=strategy_name,
        profile_id=profile.id,
        strategy_type=strategy_type,
    )
    model = await seed_model(
        db,
        api_family=api_family,
        display_name=model_display_name,
        loadbalance_strategy_id=strategy.id,
        model_id=model_id,
        model_type="native",
        profile_id=profile.id,
        vendor_id=vendor.id,
    )
    endpoint = await seed_endpoint(
        db,
        api_key=endpoint_api_key,
        base_url=endpoint_base_url,
        name=endpoint_name,
        profile_id=profile.id,
    )
    connection = await seed_connection(
        db,
        endpoint_id=endpoint.id,
        model_config_id=model.id,
        profile_id=profile.id,
    )
    if commit:
        await db.commit()
    return NativeConnectionGraph(
        connection=connection,
        endpoint=endpoint,
        model=model,
        profile=profile,
        strategy=strategy,
        vendor=vendor,
    )


async def seed_proxy_route_graph(
    db: AsyncSession,
    *,
    api_family: str = "openai",
    commit: bool = True,
    endpoint_api_key: str = "smoke-api-key",
    endpoint_base_url: str = "https://smoke.invalid",
    profile_name: str | None = None,
    proxy_model_display_name: str | None = None,
    proxy_model_id: str | None = None,
    target_model_display_name: str | None = None,
    target_model_id: str | None = None,
    vendor_key: str | None = None,
    vendor_name: str | None = None,
) -> ProxyRouteGraph:
    native = await seed_native_connection_graph(
        db,
        api_family=api_family,
        commit=False,
        endpoint_api_key=endpoint_api_key,
        endpoint_base_url=endpoint_base_url,
        model_display_name=target_model_display_name,
        model_id=target_model_id,
        profile_name=profile_name,
        vendor_key=vendor_key,
        vendor_name=vendor_name,
    )
    proxy_model = await seed_model(
        db,
        api_family=api_family,
        display_name=proxy_model_display_name,
        loadbalance_strategy_id=None,
        model_id=proxy_model_id,
        model_type="proxy",
        profile_id=native.profile.id,
        vendor_id=native.vendor.id,
    )
    proxy_target = await seed_proxy_target(
        db,
        source_model_config_id=proxy_model.id,
        target_model_config_id=native.model.id,
    )
    if commit:
        await db.commit()
    return ProxyRouteGraph(
        connection=native.connection,
        endpoint=native.endpoint,
        profile=native.profile,
        proxy_model=proxy_model,
        proxy_target=proxy_target,
        strategy=native.strategy,
        target_model=native.model,
        vendor=native.vendor,
    )


async def seed_management_session(
    db: AsyncSession,
    *,
    commit: bool = True,
    username: str = "smoke-admin",
) -> ManagementSession:
    auth_settings = await get_or_create_app_auth_settings(db)
    auth_settings.auth_enabled = True
    auth_settings.username = username
    await db.flush()
    auth_settings, access_token, _, _, _ = await create_session_for_auth_subject(
        db,
        auth_subject_id=auth_settings.id,
        ip_address="127.0.0.1",
        session_duration="7_days",
        user_agent=_SMOKE_USER_AGENT,
    )
    if commit:
        await db.commit()
    return ManagementSession(
        access_token=access_token,
        auth_subject_id=auth_settings.id,
        username=auth_settings.username,
    )


async def seed_runtime_proxy_key(
    db: AsyncSession,
    *,
    auth_subject_id: int | None = None,
    commit: bool = True,
    name: str = "Smoke Proxy Key",
    notes: str | None = None,
) -> RuntimeProxyKey:
    raw_key, proxy_key = await create_proxy_api_key(
        db,
        auth_subject_id=auth_subject_id,
        name=name,
        notes=notes,
    )
    if commit:
        await db.commit()
    return RuntimeProxyKey(key_id=proxy_key.id, name=proxy_key.name, raw_key=raw_key)


__all__ = [
    "DeterministicUpstreamClient",
    "ManagementSession",
    "NativeConnectionGraph",
    "ProxyRouteGraph",
    "RuntimeProxyKey",
    "SMOKE_BASE_URL",
    "SMOKE_DEFAULT_DATABASE_PREFIX",
    "SmokeAppHarness",
    "WebSocketHarness",
    "mounted_smoke_app",
    "reset_backend_runtime_state",
    "seed_connection",
    "seed_endpoint",
    "seed_loadbalance_strategy",
    "seed_management_session",
    "seed_model",
    "seed_native_connection_graph",
    "seed_profile",
    "seed_proxy_target",
    "seed_proxy_route_graph",
    "seed_runtime_proxy_key",
    "seed_vendor",
    "smoke_database_name",
    "smoke_database_url",
]
