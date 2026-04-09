from __future__ import annotations

import asyncio
from dataclasses import dataclass
from typing import Any

from fastapi.routing import APIRoute

from app.dependencies import get_db, get_effective_profile_id, get_request_auth_subject
from app.main import APP_VERSION, app
from app.routers import (
    audit,
    auth,
    connections,
    config,
    endpoints,
    loadbalance,
    models,
    pricing_templates,
    profiles,
    settings,
    stats,
)
from app.schemas.schemas import (
    AuditLogDeleteResponse,
    AuditLogDetail,
    AuditLogListResponse,
    BatchDeleteResponse,
    ConfigVendorCatalogExportResponse,
    ConfigVendorCatalogImportPreviewResponse,
    ConfigVendorCatalogImportResponse,
    ConnectionHealthCheckPreviewResponse,
    ConnectionSuccessRateResponse,
    ConnectionOwnerResponse,
    ConnectionDropdownResponse,
    ConnectionResponse,
    CostingSettingsResponse,
    EndpointResponse,
    EndpointModelsBatchResponse,
    HealthCheckResponse,
    LoadbalanceCurrentStateListResponse,
    LoadbalanceCurrentStateResetResponse,
    LoadbalanceEventDeleteResponse,
    LoadbalanceEventDetail,
    LoadbalanceEventListResponse,
    ModelConfigListResponse,
    ModelConfigResponse,
    ModelConnectionsBatchResponse,
    PricingTemplateConnectionsResponse,
    PricingTemplateResponse,
    ProxyApiKeyCreateResponse,
    ProxyApiKeyResponse,
    ProxyApiKeyRotateResponse,
    ProfileBootstrapResponse,
    ProfileResponse,
    RequestLogDetailResponse,
    RequestLogListResponse,
    AuthSettingsResponse,
    ThroughputStatsResponse,
    TimezonePreferenceResponse,
    UsageModelStatistic,
    UsageSnapshotResponse,
    WebAuthnAuthenticationOptionsResponse,
    WebAuthnCredentialListResponse,
    WebAuthnRegistrationOptionsResponse,
)


@dataclass(frozen=True)
class RouteExpectation:
    path: str
    methods: frozenset[str]
    response_model: Any | None
    required_dependencies: tuple[object, ...] = ()
    forbidden_dependencies: tuple[object, ...] = ()


def _route_dependency_calls(route: APIRoute) -> set[object]:
    return {
        dependency.call
        for dependency in route.dependant.dependencies
        if dependency.call is not None
    }


def _get_route(router, expectation: RouteExpectation) -> APIRoute:
    for route in router.routes:
        if not isinstance(route, APIRoute):
            continue
        if route.path != expectation.path:
            continue
        if expectation.methods.issubset(route.methods):
            return route
    raise AssertionError(
        f"Could not find route {expectation.path} with methods {sorted(expectation.methods)}"
    )


def _assert_router_contract(router, expectations: list[RouteExpectation]) -> None:
    for expectation in expectations:
        route = _get_route(router, expectation)
        assert route.response_model == expectation.response_model
        dependency_calls = _route_dependency_calls(route)
        for dependency in expectation.required_dependencies:
            assert dependency in dependency_calls
        for dependency in expectation.forbidden_dependencies:
            assert dependency not in dependency_calls


def test_health_entrypoint_returns_expected_liveness_payload() -> None:
    health_route = next(
        route
        for route in app.routes
        if isinstance(route, APIRoute) and route.path == "/health"
    )

    assert health_route.tags == ["health"]
    assert {"GET"}.issubset(health_route.methods)
    assert asyncio.run(health_route.endpoint()) == {
        "status": "ok",
        "version": APP_VERSION,
    }


def test_config_vendor_catalog_entrypoints_stay_global_and_typed() -> None:
    _assert_router_contract(
        config.router,
        [
            RouteExpectation(
                path="/api/config/vendors/export",
                methods=frozenset({"GET"}),
                response_model=ConfigVendorCatalogExportResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/config/vendors/import/preview",
                methods=frozenset({"POST"}),
                response_model=ConfigVendorCatalogImportPreviewResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/config/vendors/import",
                methods=frozenset({"POST"}),
                response_model=ConfigVendorCatalogImportResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
        ],
    )


def test_endpoints_entrypoints_stay_profile_scoped_and_typed() -> None:
    _assert_router_contract(
        endpoints.router,
        [
            RouteExpectation(
                path="/api/endpoints",
                methods=frozenset({"GET"}),
                response_model=list[EndpointResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/endpoints",
                methods=frozenset({"POST"}),
                response_model=EndpointResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/endpoints/{endpoint_id}/position",
                methods=frozenset({"PATCH"}),
                response_model=list[EndpointResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/endpoints/connections",
                methods=frozenset({"GET"}),
                response_model=ConnectionDropdownResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/endpoints/{endpoint_id}",
                methods=frozenset({"PUT"}),
                response_model=EndpointResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/endpoints/{endpoint_id}/duplicate",
                methods=frozenset({"POST"}),
                response_model=EndpointResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/endpoints/{endpoint_id}",
                methods=frozenset({"DELETE"}),
                response_model=None,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
        ],
    )


def test_models_entrypoints_stay_profile_scoped_and_typed() -> None:
    _assert_router_contract(
        models.router,
        [
            RouteExpectation(
                path="/api/models",
                methods=frozenset({"GET"}),
                response_model=list[ModelConfigListResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/by-endpoints",
                methods=frozenset({"POST"}),
                response_model=EndpointModelsBatchResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/{model_config_id}",
                methods=frozenset({"GET"}),
                response_model=ModelConfigResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models",
                methods=frozenset({"POST"}),
                response_model=ModelConfigResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/{model_config_id}",
                methods=frozenset({"PUT"}),
                response_model=ModelConfigResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/{model_config_id}",
                methods=frozenset({"DELETE"}),
                response_model=None,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/by-endpoint/{endpoint_id}",
                methods=frozenset({"GET"}),
                response_model=list[ModelConfigListResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
        ],
    )


def test_profiles_entrypoints_stay_global_and_typed() -> None:
    _assert_router_contract(
        profiles.router,
        [
            RouteExpectation(
                path="/api/profiles",
                methods=frozenset({"GET"}),
                response_model=list[ProfileResponse],
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/profiles/active",
                methods=frozenset({"GET"}),
                response_model=ProfileResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/profiles/bootstrap",
                methods=frozenset({"GET"}),
                response_model=ProfileBootstrapResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/profiles",
                methods=frozenset({"POST"}),
                response_model=ProfileResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/profiles/{profile_id}",
                methods=frozenset({"PATCH"}),
                response_model=ProfileResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/profiles/{profile_id}/activate",
                methods=frozenset({"POST"}),
                response_model=ProfileResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/profiles/{profile_id}",
                methods=frozenset({"DELETE"}),
                response_model=None,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
        ],
    )


def test_pricing_template_entrypoints_stay_profile_scoped_and_typed() -> None:
    _assert_router_contract(
        pricing_templates.router,
        [
            RouteExpectation(
                path="/api/pricing-templates",
                methods=frozenset({"GET"}),
                response_model=list[PricingTemplateResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/pricing-templates",
                methods=frozenset({"POST"}),
                response_model=PricingTemplateResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/pricing-templates/{template_id}",
                methods=frozenset({"GET"}),
                response_model=PricingTemplateResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/pricing-templates/{template_id}",
                methods=frozenset({"PUT"}),
                response_model=PricingTemplateResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/pricing-templates/{template_id}",
                methods=frozenset({"DELETE"}),
                response_model=None,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/pricing-templates/{template_id}/connections",
                methods=frozenset({"GET"}),
                response_model=PricingTemplateConnectionsResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
        ],
    )


def test_stats_clean_entrypoints_stay_profile_scoped_and_typed() -> None:
    _assert_router_contract(
        stats.router,
        [
            RouteExpectation(
                path="/api/stats/requests",
                methods=frozenset({"GET"}),
                response_model=RequestLogListResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/stats/requests/{request_id}",
                methods=frozenset({"GET"}),
                response_model=RequestLogDetailResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/stats/requests",
                methods=frozenset({"DELETE"}),
                response_model=BatchDeleteResponse,
                required_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/stats/statistics",
                methods=frozenset({"DELETE"}),
                response_model=BatchDeleteResponse,
                required_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/stats/throughput",
                methods=frozenset({"GET"}),
                response_model=ThroughputStatsResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/stats/connection-success-rates",
                methods=frozenset({"GET"}),
                response_model=list[ConnectionSuccessRateResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/stats/usage-snapshot",
                methods=frozenset({"GET"}),
                response_model=UsageSnapshotResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/stats/endpoints/{endpoint_id}/models",
                methods=frozenset({"GET"}),
                response_model=list[UsageModelStatistic],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
        ],
    )


def test_audit_entrypoints_stay_profile_scoped_and_typed() -> None:
    _assert_router_contract(
        audit.router,
        [
            RouteExpectation(
                path="/api/audit/logs",
                methods=frozenset({"GET"}),
                response_model=AuditLogListResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/audit/logs/{log_id}",
                methods=frozenset({"GET"}),
                response_model=AuditLogDetail,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/audit/logs",
                methods=frozenset({"DELETE"}),
                response_model=AuditLogDeleteResponse,
                required_dependencies=(get_effective_profile_id,),
            ),
        ],
    )


def test_loadbalance_clean_entrypoints_stay_profile_scoped_and_typed() -> None:
    _assert_router_contract(
        loadbalance.router,
        [
            RouteExpectation(
                path="/api/loadbalance/current-state",
                methods=frozenset({"GET"}),
                response_model=LoadbalanceCurrentStateListResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/loadbalance/current-state/{connection_id}/reset",
                methods=frozenset({"POST"}),
                response_model=LoadbalanceCurrentStateResetResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/loadbalance/events",
                methods=frozenset({"GET"}),
                response_model=LoadbalanceEventListResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/loadbalance/events/{event_id}",
                methods=frozenset({"GET"}),
                response_model=LoadbalanceEventDetail,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/loadbalance/events",
                methods=frozenset({"DELETE"}),
                response_model=LoadbalanceEventDeleteResponse,
                required_dependencies=(get_effective_profile_id,),
            ),
        ],
    )


def test_auth_webauthn_entrypoints_stay_on_the_reviewed_contract() -> None:
    _assert_router_contract(
        auth.router,
        [
            RouteExpectation(
                path="/api/auth/webauthn/register/options",
                methods=frozenset({"POST"}),
                response_model=WebAuthnRegistrationOptionsResponse,
                required_dependencies=(get_db, get_request_auth_subject),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/auth/webauthn/register/verify",
                methods=frozenset({"POST"}),
                response_model=None,
                required_dependencies=(get_db, get_request_auth_subject),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/auth/webauthn/authenticate/options",
                methods=frozenset({"POST"}),
                response_model=WebAuthnAuthenticationOptionsResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(
                    get_effective_profile_id,
                    get_request_auth_subject,
                ),
            ),
            RouteExpectation(
                path="/api/auth/webauthn/authenticate/verify",
                methods=frozenset({"POST"}),
                response_model=None,
                required_dependencies=(get_db,),
                forbidden_dependencies=(
                    get_effective_profile_id,
                    get_request_auth_subject,
                ),
            ),
            RouteExpectation(
                path="/api/auth/webauthn/credentials",
                methods=frozenset({"GET"}),
                response_model=WebAuthnCredentialListResponse,
                required_dependencies=(get_db, get_request_auth_subject),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/auth/webauthn/credentials/{credential_id}",
                methods=frozenset({"DELETE"}),
                response_model=None,
                required_dependencies=(get_db, get_request_auth_subject),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
        ],
    )


def test_connections_clean_child_entrypoints_stay_profile_scoped_and_typed() -> None:
    _assert_router_contract(
        connections.router,
        [
            RouteExpectation(
                path="/api/models/connections/batch",
                methods=frozenset({"POST"}),
                response_model=ModelConnectionsBatchResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/{model_config_id}/connections",
                methods=frozenset({"GET"}),
                response_model=list[ConnectionResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/{model_config_id}/connections",
                methods=frozenset({"POST"}),
                response_model=ConnectionResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/{model_config_id}/connections/health-check-preview",
                methods=frozenset({"POST"}),
                response_model=ConnectionHealthCheckPreviewResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/connections/{connection_id}",
                methods=frozenset({"PUT"}),
                response_model=ConnectionResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/models/{model_config_id}/connections/{connection_id}/priority",
                methods=frozenset({"PATCH"}),
                response_model=list[ConnectionResponse],
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/connections/{connection_id}/pricing-template",
                methods=frozenset({"PUT"}),
                response_model=ConnectionResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/connections/{connection_id}",
                methods=frozenset({"DELETE"}),
                response_model=None,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/connections/{connection_id}/health-check",
                methods=frozenset({"POST"}),
                response_model=HealthCheckResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/connections/{connection_id}/owner",
                methods=frozenset({"GET"}),
                response_model=ConnectionOwnerResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
        ],
    )


def test_settings_clean_child_entrypoints_preserve_scope_and_types() -> None:
    _assert_router_contract(
        settings.router,
        [
            RouteExpectation(
                path="/api/settings/auth",
                methods=frozenset({"GET"}),
                response_model=AuthSettingsResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/settings/auth",
                methods=frozenset({"PUT"}),
                response_model=AuthSettingsResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/settings/costing",
                methods=frozenset({"GET"}),
                response_model=CostingSettingsResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/settings/timezone",
                methods=frozenset({"GET"}),
                response_model=TimezonePreferenceResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/settings/costing",
                methods=frozenset({"PUT"}),
                response_model=CostingSettingsResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/settings/timezone",
                methods=frozenset({"PUT"}),
                response_model=TimezonePreferenceResponse,
                required_dependencies=(get_db, get_effective_profile_id),
            ),
            RouteExpectation(
                path="/api/settings/auth/proxy-keys",
                methods=frozenset({"GET"}),
                response_model=list[ProxyApiKeyResponse],
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/settings/auth/proxy-keys",
                methods=frozenset({"POST"}),
                response_model=ProxyApiKeyCreateResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/settings/auth/proxy-keys/{key_id}/rotate",
                methods=frozenset({"POST"}),
                response_model=ProxyApiKeyRotateResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/settings/auth/proxy-keys/{key_id}",
                methods=frozenset({"PATCH"}),
                response_model=ProxyApiKeyResponse,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
            RouteExpectation(
                path="/api/settings/auth/proxy-keys/{key_id}",
                methods=frozenset({"DELETE"}),
                response_model=None,
                required_dependencies=(get_db,),
                forbidden_dependencies=(get_effective_profile_id,),
            ),
        ],
    )
