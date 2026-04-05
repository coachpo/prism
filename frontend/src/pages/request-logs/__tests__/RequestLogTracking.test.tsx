import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ModelConfigListItem, RequestLogDetail } from "@/lib/types";
import { RequestLogOverviewTab } from "../detail/RequestLogOverviewTab";

const trackedRequest: RequestLogDetail = {
  summary: {
    id: 42,
    created_at: "2026-03-16T00:00:00.000Z",
    model_id: "gpt-5.4",
    resolved_target_model_id: null,
    api_family: "openai",
    vendor_id: 1,
    vendor_key: "openai",
    vendor_name: "OpenAI",
    status_code: 200,
    response_time_ms: 912,
    is_stream: true,
  },
  request: {
    request_path: "/v1/chat/completions",
    ingress_request_id: "ingress_req_42",
    attempt_number: 2,
    provider_correlation_id: "provider_req_abc123",
    proxy_api_key_id: null,
    proxy_api_key_name_snapshot: null,
    error_detail: null,
  },
  routing: {
    profile_id: 7,
    model_id: "gpt-5.4",
    resolved_target_model_id: null,
    api_family: "openai",
    vendor_id: 1,
    vendor_key: "openai",
    vendor_name: "OpenAI",
    endpoint_id: 12,
    connection_id: 34,
    endpoint_base_url: "https://api.example.com/v1",
    endpoint_description: "Primary endpoint",
  },
  usage: {
    input_tokens: 100,
    output_tokens: 50,
    total_tokens: 150,
    success_flag: true,
    billable_flag: true,
    priced_flag: true,
    unpriced_reason: null,
    cache_read_input_tokens: 0,
    cache_creation_input_tokens: 0,
    reasoning_tokens: 0,
  },
  costing: {
    input_cost_micros: 1000,
    output_cost_micros: 2000,
    cache_read_input_cost_micros: 0,
    cache_creation_input_cost_micros: 0,
    reasoning_cost_micros: 0,
    total_cost_original_micros: 3000,
    total_cost_user_currency_micros: 3000,
    currency_code_original: "USD",
    report_currency_code: "USD",
    report_currency_symbol: "$",
    fx_rate_used: null,
    fx_rate_source: null,
  },
  pricing: {
    pricing_snapshot_unit: null,
    pricing_snapshot_input: null,
    pricing_snapshot_output: null,
    pricing_snapshot_cache_read_input: null,
    pricing_snapshot_cache_creation_input: null,
    pricing_snapshot_reasoning: null,
    pricing_snapshot_missing_special_token_price_policy: null,
    pricing_config_version_used: 1,
  },
};

function createResolveModelLabel(
  labels: Record<string, string>,
  modelTypes: Record<string, "native" | "proxy"> = {},
) {
  return Object.assign(
    (modelId: string) => labels[modelId] ?? modelId,
    {
      getModelMetadata: (modelId: string) =>
        modelTypes[modelId]
          ? ({
              model_id: modelId,
              model_type: modelTypes[modelId],
            } as ModelConfigListItem)
          : undefined,
    },
  );
}

describe("RequestLogOverviewTab tracking fields", () => {
  it("renders ingress tracking metadata and a proxy-origin sign from current model metadata", () => {
    render(
      <LocaleProvider>
        <RequestLogOverviewTab
          request={trackedRequest}
          formatTimestamp={(iso) => `formatted:${iso}`}
          resolveModelLabel={createResolveModelLabel(
            { "gpt-5.4": "Gateway proxy" },
            { "gpt-5.4": "proxy" },
          )}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText(/proxy origin/i)).toBeInTheDocument();
    expect(screen.getByText(/ingress request id/i)).toBeInTheDocument();
    expect(screen.getByText("ingress_req_42")).toBeInTheDocument();
    expect(screen.getByText(/attempt number/i)).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText(/provider correlation id/i)).toBeInTheDocument();
    expect(screen.getByText("provider_req_abc123")).toBeInTheDocument();
  });
});
