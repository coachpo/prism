from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[2]
TESTDATA_ROOT = REPO_ROOT / "backend" / "testdata"
JsonObject = dict[str, Any]


def load_json(relative_path: str) -> JsonObject:
    data = json.loads((TESTDATA_ROOT / relative_path).read_text())
    require(isinstance(data, dict), f"{relative_path} must decode to a JSON object")
    return data


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def require_keys(mapping: JsonObject, keys: list[str], label: str) -> None:
    for key in keys:
        require(key in mapping, f"{label} is missing '{key}'")


def find_by_key(items: list[JsonObject], key: str, value: object) -> JsonObject:
    for item in items:
        if item.get(key) == value:
            return item
    raise AssertionError(f"Could not find item with {key}={value!r}")


def verify_profile_bundle(profile_bundle: JsonObject) -> None:
    require(profile_bundle["version"] == 1, "profile bundle version must stay 1 for the Go-era reset")
    require(
        profile_bundle["bundle_kind"] == "profile_config",
        "profile bundle kind must stay 'profile_config'",
    )
    require_keys(
        profile_bundle,
        [
            "vendor_refs",
            "endpoints",
            "pricing_templates",
            "loadbalance_strategies",
            "models",
            "profile_settings",
            "header_blocklist_rules",
            "secret_payload",
        ],
        "profile bundle",
    )

    vendor_refs = profile_bundle["vendor_refs"]
    require(bool(isinstance(vendor_refs, list) and vendor_refs), "vendor_refs must be non-empty")
    vendor_ref_keys = {entry["key"] for entry in vendor_refs}
    require(vendor_ref_keys == {"openai", "anthropic"}, "vendor_refs must freeze both vendor keys")

    secret_payload = profile_bundle["secret_payload"]
    require_keys(secret_payload, ["kind", "cipher", "key_id", "entries"], "secret_payload")
    require(secret_payload["kind"] == "encrypted", "secret_payload.kind must stay 'encrypted'")
    require(secret_payload["cipher"] == "fernet-v1", "secret_payload.cipher must stay 'fernet-v1'")
    require(bool(isinstance(secret_payload["entries"], list) and secret_payload["entries"]), "secret_payload.entries must be non-empty")

    endpoints = profile_bundle["endpoints"]
    require(isinstance(endpoints, list) and len(endpoints) == 2, "profile bundle must freeze two endpoints")
    endpoint_secret_refs = {
        endpoint["api_key_secret_ref"]
        for endpoint in endpoints
        if endpoint["api_key_secret_ref"] is not None
    }
    payload_secret_refs = {entry["ref"] for entry in secret_payload["entries"]}
    require(
        endpoint_secret_refs == payload_secret_refs,
        "secret_payload.entries must match endpoint api_key_secret_ref values exactly",
    )

    strategies = profile_bundle["loadbalance_strategies"]
    require(
        isinstance(strategies, list) and len(strategies) == 2,
        "profile bundle must freeze one legacy and one adaptive strategy",
    )
    legacy_strategy = find_by_key(strategies, "strategy_type", "legacy")
    adaptive_strategy = find_by_key(strategies, "strategy_type", "adaptive")

    require(legacy_strategy["legacy_strategy_type"] == "round-robin", "legacy strategy must keep legacy_strategy_type")
    require(legacy_strategy["routing_policy"] is None, "legacy strategy must keep routing_policy null")
    require(isinstance(legacy_strategy["auto_recovery"], dict), "legacy strategy must keep auto_recovery")
    require_keys(legacy_strategy["auto_recovery"], ["mode", "status_codes", "cooldown", "ban"], "legacy auto_recovery")

    require(adaptive_strategy["legacy_strategy_type"] is None, "adaptive strategy must null legacy_strategy_type")
    require(adaptive_strategy["auto_recovery"] is None, "adaptive strategy must null auto_recovery")
    require(isinstance(adaptive_strategy["routing_policy"], dict), "adaptive strategy must keep routing_policy")
    require_keys(
        adaptive_strategy["routing_policy"],
        ["kind", "routing_objective", "hedge", "circuit_breaker", "admission"],
        "adaptive routing_policy",
    )

    models = profile_bundle["models"]
    require(isinstance(models, list) and len(models) == 3, "profile bundle must freeze three models")
    native_openai = find_by_key(models, "model_id", "gpt-4o-native")
    native_anthropic = find_by_key(models, "model_id", "claude-3-5-sonnet")
    proxy_model = find_by_key(models, "model_id", "gpt-4o")

    require(native_openai["loadbalance_strategy_name"] == "legacy-primary", "openai model must reference legacy strategy")
    openai_connection = native_openai["connections"][0]
    require_keys(
        openai_connection,
        ["custom_headers", "openai_probe_endpoint_variant", "qps_limit", "max_in_flight_non_stream", "max_in_flight_stream"],
        "openai connection",
    )
    require(isinstance(openai_connection["custom_headers"], dict), "openai connection custom_headers must be a mapping")
    require(openai_connection["openai_probe_endpoint_variant"] == "responses_minimal", "openai connection must freeze openai_probe_endpoint_variant")

    require(native_anthropic["loadbalance_strategy_name"] == "adaptive-primary", "anthropic model must reference adaptive strategy")
    anthropic_connection = native_anthropic["connections"][0]
    require(anthropic_connection.get("openai_probe_endpoint_variant") in (None, ""), "non-openai connection must not set openai_probe_endpoint_variant")

    require(proxy_model["model_type"] == "proxy", "proxy model must keep model_type=proxy")
    require(proxy_model["connections"] == [], "proxy model must not include connections")
    require(
        proxy_model["proxy_targets"] == [{"target_model_id": "gpt-4o-native", "position": 0}],
        "proxy model must keep ordered proxy_targets",
    )


def verify_vendor_bundle(vendor_bundle: JsonObject, vendor_ref_keys: set[str]) -> None:
    require(vendor_bundle["version"] == 1, "vendor catalog bundle version must stay 1 for the Go-era reset")
    require(
        vendor_bundle["bundle_kind"] == "vendor_catalog",
        "vendor catalog bundle kind must stay 'vendor_catalog'",
    )
    vendors = vendor_bundle["vendors"]
    require(isinstance(vendors, list) and len(vendors) >= 2, "vendor catalog must freeze at least two vendors")
    vendor_keys = {vendor["key"] for vendor in vendors}
    require(vendor_ref_keys <= vendor_keys, "vendor bundle must cover every profile vendor_ref key")

    for vendor in vendors:
        require_keys(
            vendor,
            ["key", "name", "description", "icon_key", "audit_enabled", "audit_capture_bodies"],
            f"vendor {vendor.get('key')}",
        )


def verify_request_log_list(request_log_list: JsonObject) -> None:
    require_keys(request_log_list, ["filter_options", "items", "total", "limit", "offset"], "request log list")
    filter_options = request_log_list["filter_options"]
    require_keys(filter_options, ["endpoints"], "request log list filter_options")
    endpoints = filter_options["endpoints"]
    require(isinstance(endpoints, list) and len(endpoints) == 2, "request log list must freeze endpoint filter_options")
    for endpoint in endpoints:
        require_keys(endpoint, ["endpoint_id", "endpoint_label"], "request log list endpoint option")

    items = request_log_list["items"]
    require(isinstance(items, list) and len(items) == 1, "request log list must freeze one browse item")
    require_keys(
        items[0],
        [
            "id",
            "created_at",
            "model_id",
            "resolved_target_model_id",
            "api_family",
            "vendor_id",
            "vendor_key",
            "vendor_name",
            "endpoint_id",
            "endpoint_label",
            "connection_id",
            "status_code",
            "response_time_ms",
            "ttft_ms",
            "completion_duration_ms",
            "is_stream",
            "output_tokens",
            "total_tokens",
            "total_cost_user_currency_micros",
            "report_currency_symbol",
            "caller_client_display",
            "upstream_client_display",
            "user_agent_overridden",
        ],
        "request log list item",
    )


def verify_request_log_detail(request_log_detail: JsonObject) -> None:
    require(
        set(request_log_detail.keys()) == {"summary", "request", "routing", "usage", "costing", "pricing"},
        "request log detail must keep the summary/request/routing/usage/costing/pricing split",
    )
    require_keys(
        request_log_detail["request"],
        ["request_path", "ingress_request_id", "attempt_number", "provider_correlation_id"],
        "request log detail request group",
    )
    require_keys(
        request_log_detail["routing"],
        ["profile_id", "endpoint_id", "connection_id", "endpoint_base_url", "audit_enabled_at_request"],
        "request log detail routing group",
    )
    require_keys(
        request_log_detail["usage"],
        ["input_tokens", "output_tokens", "total_tokens", "success_flag", "billable_flag", "priced_flag"],
        "request log detail usage group",
    )
    require_keys(
        request_log_detail["costing"],
        ["total_cost_original_micros", "total_cost_user_currency_micros", "report_currency_symbol"],
        "request log detail costing group",
    )
    require_keys(
        request_log_detail["pricing"],
        ["pricing_snapshot_unit", "pricing_snapshot_input", "pricing_snapshot_output", "pricing_config_version_used"],
        "request log detail pricing group",
    )


def verify_dashboard_update(realtime_payload: JsonObject) -> None:
    require(
        set(realtime_payload.keys())
        == {
            "type",
            "request_log",
            "stats_summary_24h",
            "api_family_summary_24h",
            "spending_summary_30d",
            "throughput_24h",
            "routing_route_24h",
        },
        "dashboard.update payload must keep the documented top-level keys",
    )
    require(realtime_payload["type"] == "dashboard.update", "realtime payload type must stay 'dashboard.update'")
    require_keys(
        realtime_payload["request_log"],
        ["profile_id", "ingress_request_id", "attempt_number", "provider_correlation_id", "request_path"],
        "dashboard.update request_log",
    )
    require_keys(
        realtime_payload["stats_summary_24h"],
        ["total_requests", "success_count", "error_count", "success_rate", "groups"],
        "dashboard.update stats_summary_24h",
    )
    require_keys(
        realtime_payload["api_family_summary_24h"],
        ["total_requests", "success_count", "error_count", "success_rate", "groups"],
        "dashboard.update api_family_summary_24h",
    )
    require_keys(
        realtime_payload["spending_summary_30d"],
        ["summary", "groups", "groups_total", "top_spending_models", "top_spending_endpoints", "unpriced_breakdown", "report_currency_code", "report_currency_symbol"],
        "dashboard.update spending_summary_30d",
    )
    require_keys(
        realtime_payload["throughput_24h"],
        ["average_rpm", "peak_rpm", "current_rpm", "total_requests", "time_window_seconds", "buckets"],
        "dashboard.update throughput_24h",
    )
    require_keys(
        realtime_payload["routing_route_24h"],
        ["model_id", "model_config_id", "model_label", "endpoint_id", "endpoint_label", "active_connection_count", "traffic_request_count_24h", "request_count_24h", "success_count_24h", "error_count_24h", "success_rate_24h"],
        "dashboard.update routing_route_24h",
    )


def verify_cross_fixture_consistency(
    profile_bundle: JsonObject,
    request_log_list: JsonObject,
    request_log_detail: JsonObject,
    realtime_payload: JsonObject,
) -> None:
    list_item = request_log_list["items"][0]
    detail_summary = request_log_detail["summary"]
    realtime_request_log = realtime_payload["request_log"]
    openai_model = find_by_key(profile_bundle["models"], "model_id", "gpt-4o")
    openai_target = openai_model["proxy_targets"][0]["target_model_id"]

    require(list_item["model_id"] == detail_summary["model_id"] == realtime_request_log["model_id"] == "gpt-4o", "request log fixtures must agree on model_id")
    require(
        list_item["resolved_target_model_id"] == detail_summary["resolved_target_model_id"] == realtime_request_log["resolved_target_model_id"] == openai_target,
        "request log fixtures must agree on resolved_target_model_id",
    )
    require(
        request_log_detail["request"]["provider_correlation_id"] == realtime_request_log["provider_correlation_id"],
        "detail and realtime fixtures must agree on provider_correlation_id",
    )
    require(
        request_log_list["filter_options"]["endpoints"][0]["endpoint_label"] == "Primary OpenAI",
        "request log list must keep the frozen Primary OpenAI endpoint label",
    )
    require(
        request_log_detail["routing"]["endpoint_base_url"] == realtime_request_log["endpoint_base_url"] == "https://api.openai.com",
        "detail and realtime fixtures must agree on endpoint_base_url",
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Verify frozen Go rewrite fixture artifacts")
    parser.add_argument("--bundles", action="store_true", help="verify the profile config bundle fixture")
    parser.add_argument("--vendor-bundle", action="store_true", help="verify the vendor catalog bundle fixture")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    run_all = not (args.bundles or args.vendor_bundle)

    profile_bundle: JsonObject | None = None
    if run_all or args.bundles or args.vendor_bundle:
        profile_bundle = load_json("bundles/profile-v1-example.json")

    if run_all or args.bundles:
        verify_profile_bundle(profile_bundle)

    if run_all or args.vendor_bundle:
        vendor_bundle = load_json("bundles/vendor-v1-example.json")
        verify_vendor_bundle(vendor_bundle, {entry["key"] for entry in profile_bundle["vendor_refs"]})

    if run_all:
        request_log_list = load_json("requests/request-log-list.json")
        request_log_detail = load_json("requests/request-log-detail.json")
        realtime_payload = load_json("realtime/dashboard-update.json")
        verify_request_log_list(request_log_list)
        verify_request_log_detail(request_log_detail)
        verify_dashboard_update(realtime_payload)
        verify_cross_fixture_consistency(profile_bundle, request_log_list, request_log_detail, realtime_payload)

    print("go rewrite fixture artifacts verified")


if __name__ == "__main__":
    main()
