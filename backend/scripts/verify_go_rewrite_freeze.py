from __future__ import annotations

import json
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[2]
BACKEND_ROOT = REPO_ROOT / "backend"
TESTDATA_ROOT = BACKEND_ROOT / "testdata"
DOCS_ROOT = REPO_ROOT / "docs"
FRONTEND_LIB_ROOT = REPO_ROOT / "frontend" / "src" / "lib"
SCHEMAS_ROOT = BACKEND_ROOT / "app" / "schemas" / "domains"
JsonObject = dict[str, Any]


def load_text(path: Path) -> str:
    return path.read_text()


def load_json(path: Path) -> JsonObject:
    data = json.loads(path.read_text())
    require(isinstance(data, dict), f"{path} must decode to a JSON object")
    return data


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def assert_contains(text: str, marker: str, label: str) -> None:
    require(marker in text, f"{label} is missing marker {marker!r}")


def assert_path(openapi: JsonObject, path: str, method: str | None = None) -> None:
    paths = openapi["paths"]
    require(path in paths, f"OpenAPI snapshot is missing path {path}")
    if method is not None:
        require(method in paths[path], f"OpenAPI snapshot is missing method {method.upper()} for {path}")


def verify_openapi_snapshot(
    docs_openapi: JsonObject,
    frozen_openapi: JsonObject,
) -> None:
    require(
        docs_openapi == frozen_openapi,
        "backend/testdata/openapi/current.json must stay structurally identical to docs/openapi.json",
    )

    helper_routes = [
        "/api/profiles/bootstrap",
        "/api/models/by-endpoints",
        "/api/models/by-endpoint/{endpoint_id}",
        "/api/endpoints/connections",
        "/api/endpoints/{endpoint_id}/duplicate",
        "/api/endpoints/{endpoint_id}/position",
        "/api/models/connections/batch",
        "/api/models/{model_config_id}/connections/health-check-preview",
        "/api/connections/{connection_id}/health-check",
        "/api/connections/{connection_id}/owner",
        "/api/pricing-templates/{template_id}/connections",
        "/api/loadbalance/strategies/defaults",
        "/api/loadbalance/current-state/{connection_id}/reset",
        "/api/stats/models/metrics",
        "/health",
    ]
    for route in helper_routes:
        assert_path(frozen_openapi, route)

    assert_path(frozen_openapi, "/api/config/profile/import/preview", method="post")
    runtime_paths = [
        path
        for path in frozen_openapi["paths"]
        if path.startswith("/v1") or path.startswith("/v1beta")
    ]
    require(not runtime_paths, "OpenAPI snapshot must exclude runtime proxy routes")


def verify_bundle_markers(
    api_spec_text: str,
    frontend_config_validation_text: str,
    admin_schema_text: str,
    connection_model_text: str,
    import_validator_text: str,
    import_export_text: str,
    profile_bundle: JsonObject,
    vendor_bundle: JsonObject,
) -> None:
    docs_markers = [
        "vendor_refs",
        "secret_payload",
        "strategy_type",
        "legacy_strategy_type",
        "auto_recovery",
        "routing_policy",
        "custom_headers",
        "openai_probe_endpoint_variant",
    ]
    for marker in docs_markers:
        assert_contains(api_spec_text, marker, "docs/API_SPEC.md")

    for marker in [
        "vendor_refs",
        "secret_payload",
        "strategy_type",
        "legacy_strategy_type",
        "auto_recovery",
        "routing_policy",
        "custom_headers",
        "openai_probe_endpoint_variant",
    ]:
        assert_contains(frontend_config_validation_text, marker, "frontend config import validation mirror")

    for marker in ["vendor_refs", "secret_payload"]:
        assert_contains(import_validator_text, marker, "backend import validator")
    assert_contains(import_export_text, "/profile/import/preview", "backend import/export routes")
    for marker in ["strategy_type", "legacy_strategy_type", "auto_recovery", "routing_policy"]:
        assert_contains(admin_schema_text, marker, "backend admin schema")
        assert_contains(connection_model_text, marker, "backend connection model schema")

    require(profile_bundle["version"] == 1, "profile fixture must keep Go-era version 1")
    require(profile_bundle["bundle_kind"] == "profile_config", "profile fixture must keep bundle_kind profile_config")
    require(vendor_bundle["version"] == 1, "vendor fixture must keep Go-era version 1")
    require(vendor_bundle["bundle_kind"] == "vendor_catalog", "vendor fixture must keep bundle_kind vendor_catalog")

    legacy_strategy = next(
        strategy
        for strategy in profile_bundle["loadbalance_strategies"]
        if strategy["strategy_type"] == "legacy"
    )
    adaptive_strategy = next(
        strategy
        for strategy in profile_bundle["loadbalance_strategies"]
        if strategy["strategy_type"] == "adaptive"
    )
    require("legacy_strategy_type" in legacy_strategy, "legacy strategy must expose legacy_strategy_type")
    require("auto_recovery" in legacy_strategy, "legacy strategy must expose auto_recovery")
    require("routing_policy" in adaptive_strategy, "adaptive strategy must expose routing_policy")

    openai_model = next(model for model in profile_bundle["models"] if model["model_id"] == "gpt-4o-native")
    openai_connection = openai_model["connections"][0]
    require("custom_headers" in openai_connection, "profile fixture must expose custom_headers")
    require(
        "openai_probe_endpoint_variant" in openai_connection,
        "profile fixture must expose openai_probe_endpoint_variant",
    )


def verify_request_log_markers(
    api_spec_text: str,
    stats_schema_text: str,
    request_logs_text: str,
    logging_text: str,
    request_log_list: JsonObject,
    request_log_detail: JsonObject,
) -> None:
    assert_contains(api_spec_text, "filter_options.endpoints", "docs/API_SPEC.md request log list contract")
    assert_contains(api_spec_text, "provider_correlation_id", "docs/API_SPEC.md request log detail contract")
    assert_contains(api_spec_text, "ingress_request_id", "docs/API_SPEC.md request log detail contract")
    assert_contains(api_spec_text, "attempt_number", "docs/API_SPEC.md request log detail contract")

    for marker in ["provider_correlation_id", "ingress_request_id", "attempt_number"]:
        assert_contains(stats_schema_text, marker, "backend stats schema")
        assert_contains(logging_text, marker, "backend stats logging service")
    assert_contains(stats_schema_text, "class RequestLogListResponse", "backend stats schema")
    assert_contains(stats_schema_text, "class RequestLogDetailResponse", "backend stats schema")
    assert_contains(request_logs_text, "_build_endpoint_options", "backend request log browse service")

    require("filter_options" in request_log_list, "request log list fixture must expose filter_options")
    require("endpoints" in request_log_list["filter_options"], "request log list fixture must expose filter_options.endpoints")
    require(
        "provider_correlation_id" in request_log_detail["request"],
        "request log detail fixture must expose provider_correlation_id",
    )
    require(
        "ingress_request_id" in request_log_detail["request"],
        "request log detail fixture must expose ingress_request_id",
    )
    require(
        "attempt_number" in request_log_detail["request"],
        "request log detail fixture must expose attempt_number",
    )


def verify_realtime_markers(
    api_spec_text: str,
    architecture_text: str,
    stats_schema_text: str,
    logging_text: str,
    connection_manager_text: str,
    realtime_payload: JsonObject,
) -> None:
    assert_contains(api_spec_text, "dashboard.update", "docs/API_SPEC.md realtime contract")
    assert_contains(api_spec_text, '"request_log"', "docs/API_SPEC.md realtime contract")
    assert_contains(api_spec_text, '"stats_summary_24h"', "docs/API_SPEC.md realtime contract")
    assert_contains(api_spec_text, '"api_family_summary_24h"', "docs/API_SPEC.md realtime contract")
    assert_contains(api_spec_text, '"spending_summary_30d"', "docs/API_SPEC.md realtime contract")
    assert_contains(api_spec_text, '"throughput_24h"', "docs/API_SPEC.md realtime contract")
    assert_contains(api_spec_text, '"routing_route_24h"', "docs/API_SPEC.md realtime contract")

    assert_contains(architecture_text, 'Broadcast {type: "dashboard.update", ...payload}', "docs/ARCHITECTURE.md realtime flow")
    assert_contains(architecture_text, "room membership keyed by (profile_id, channel)", "docs/ARCHITECTURE.md realtime flow")
    assert_contains(logging_text, '"type": "dashboard.update"', "backend dashboard logging")
    assert_contains(connection_manager_text, "(profile_id, channel)", "backend realtime connection manager")
    assert_contains(stats_schema_text, "class DashboardRealtimeUpdateResponse", "backend stats schema")

    require(realtime_payload["type"] == "dashboard.update", "realtime fixture must keep type=dashboard.update")
    for key in [
        "request_log",
        "stats_summary_24h",
        "api_family_summary_24h",
        "spending_summary_30d",
        "throughput_24h",
        "routing_route_24h",
    ]:
        require(key in realtime_payload, f"realtime fixture must expose {key}")


def verify_route_scope_markers(
    api_spec_text: str,
    architecture_text: str,
    management_text: str,
    observability_text: str,
    frozen_openapi: JsonObject,
) -> None:
    assert_contains(
        api_spec_text,
        "global management routes include `/api/profiles/*`, `/api/vendors/*`, `/api/auth/*`, `/api/realtime/*`, `/api/settings/auth*`, `/api/config/vendors/*`, and `POST /api/config/profile/import/preview`",
        "docs/API_SPEC.md management scope rules",
    )
    assert_contains(
        api_spec_text,
        "This preview route is a global readiness check and does not require `X-Profile-Id`.",
        "docs/API_SPEC.md preview scope exception",
    )
    assert_contains(
        architecture_text,
        "Global management routes include `/api/profiles/*`, `/api/vendors/*`, `/api/config/vendors/*`, `POST /api/config/profile/import/preview`, `/api/auth/*`, `/api/realtime/*`, and the auth/email/proxy-key settings routes under `/api/settings/auth*`.",
        "docs/ARCHITECTURE.md management scope rules",
    )
    assert_contains(
        architecture_text,
        "Runtime proxy routes (`/v1/*`, `/v1beta/*`) always use active profile and ignore override headers.",
        "docs/ARCHITECTURE.md runtime scope rules",
    )

    helper_route_frontend_markers = [
        ("/api/profiles/bootstrap", management_text, "/api/profiles/bootstrap"),
        ("/api/models/by-endpoints", management_text, "/api/models/by-endpoints"),
        ("/api/models/by-endpoint/{endpoint_id}", management_text, "/api/models/by-endpoint/${endpointId}"),
        ("/api/endpoints/connections", management_text, "/api/endpoints/connections"),
        ("/api/endpoints/{endpoint_id}/duplicate", management_text, "/api/endpoints/${id}/duplicate"),
        ("/api/endpoints/{endpoint_id}/position", management_text, "/api/endpoints/${id}/position"),
        ("/api/models/connections/batch", management_text, "/api/models/connections/batch"),
        ("/api/models/{model_config_id}/connections/health-check-preview", management_text, "/api/models/${modelConfigId}/connections/health-check-preview"),
        ("/api/connections/{connection_id}/health-check", management_text, "/api/connections/${id}/health-check"),
        ("/api/connections/{connection_id}/owner", management_text, "/api/connections/${id}/owner"),
        ("/api/pricing-templates/{template_id}/connections", management_text, "/api/pricing-templates/${id}/connections"),
        ("/api/loadbalance/strategies/defaults", management_text, "/api/loadbalance/strategies/defaults"),
        ("/api/loadbalance/current-state/{connection_id}/reset", observability_text, "/api/loadbalance/current-state/${connectionId}/reset"),
        ("/api/stats/models/metrics", observability_text, "/api/stats/models/metrics"),
        ("/api/config/profile/import/preview", observability_text, "/api/config/profile/import/preview"),
    ]
    for openapi_path, source_text, source_marker in helper_route_frontend_markers:
        assert_path(frozen_openapi, openapi_path)
        assert_contains(source_text, source_marker, f"frontend route mirror for {openapi_path}")


def verify_data_model_markers(data_model_text: str) -> None:
    assert_contains(data_model_text, "user_settings", "docs/DATA_MODEL.md")
    assert_contains(data_model_text, "UNLOGGED", "docs/DATA_MODEL.md")
    assert_contains(data_model_text, "connection_limiter_state", "docs/DATA_MODEL.md")


def main() -> None:
    api_spec_text = load_text(DOCS_ROOT / "API_SPEC.md")
    data_model_text = load_text(DOCS_ROOT / "DATA_MODEL.md")
    architecture_text = load_text(DOCS_ROOT / "ARCHITECTURE.md")
    frontend_config_validation_text = load_text(FRONTEND_LIB_ROOT / "configImportValidation.ts")
    management_text = load_text(FRONTEND_LIB_ROOT / "api" / "management.ts")
    observability_text = load_text(FRONTEND_LIB_ROOT / "api" / "observability.ts")
    admin_schema_text = load_text(SCHEMAS_ROOT / "admin.py")
    connection_model_text = load_text(SCHEMAS_ROOT / "connection_model.py")
    stats_schema_text = load_text(SCHEMAS_ROOT / "stats.py")
    import_validator_text = load_text(BACKEND_ROOT / "app" / "routers" / "config_domains" / "import_validator.py")
    import_export_text = load_text(BACKEND_ROOT / "app" / "routers" / "config_domains" / "import_export.py")
    request_logs_text = load_text(BACKEND_ROOT / "app" / "services" / "stats" / "request_logs.py")
    logging_text = load_text(BACKEND_ROOT / "app" / "services" / "stats" / "logging.py")
    connection_manager_text = load_text(BACKEND_ROOT / "app" / "services" / "realtime" / "connection_manager.py")

    docs_openapi = load_json(DOCS_ROOT / "openapi.json")
    frozen_openapi = load_json(TESTDATA_ROOT / "openapi" / "current.json")
    profile_bundle = load_json(TESTDATA_ROOT / "bundles" / "profile-v1-example.json")
    vendor_bundle = load_json(TESTDATA_ROOT / "bundles" / "vendor-v1-example.json")
    request_log_list = load_json(TESTDATA_ROOT / "requests" / "request-log-list.json")
    request_log_detail = load_json(TESTDATA_ROOT / "requests" / "request-log-detail.json")
    realtime_payload = load_json(TESTDATA_ROOT / "realtime" / "dashboard-update.json")

    verify_openapi_snapshot(docs_openapi, frozen_openapi)
    verify_bundle_markers(
        api_spec_text,
        frontend_config_validation_text,
        admin_schema_text,
        connection_model_text,
        import_validator_text,
        import_export_text,
        profile_bundle,
        vendor_bundle,
    )
    verify_request_log_markers(
        api_spec_text,
        stats_schema_text,
        request_logs_text,
        logging_text,
        request_log_list,
        request_log_detail,
    )
    verify_realtime_markers(
        api_spec_text,
        architecture_text,
        stats_schema_text,
        logging_text,
        connection_manager_text,
        realtime_payload,
    )
    verify_route_scope_markers(
        api_spec_text,
        architecture_text,
        management_text,
        observability_text,
        frozen_openapi,
    )
    verify_data_model_markers(data_model_text)

    print("go rewrite freeze contract verified")


if __name__ == "__main__":
    main()
