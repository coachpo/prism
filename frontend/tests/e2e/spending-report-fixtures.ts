import { expect, type Request } from "@playwright/test";

type EmptyIngressSpendingReportOptions = {
  currencyCode?: string;
  currencySymbol?: string;
};

export function createEmptyIngressSpendingReport({
  currencyCode = "USD",
  currencySymbol = "$",
}: EmptyIngressSpendingReportOptions = {}) {
  return {
    summary: {
      known_cost_micros: null,
      cost_sample_count: 0,
      cost_missing_count: 0,
      successful_request_count: 0,
      priced_request_count: 0,
      unpriced_request_count: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_read_input_tokens: 0,
      total_cache_creation_input_tokens: 0,
      total_reasoning_tokens: 0,
      total_tokens: 0,
      avg_cost_per_successful_request_micros: 0,
    },
    groups: [
      {
        key: "all",
        known_cost_micros: null,
        cost_sample_count: 0,
        total_requests: 0,
        priced_requests: 0,
        unpriced_requests: 0,
        total_tokens: 0,
      },
    ],
    groups_total: 1,
    top_spending_models: [],
    top_spending_endpoints: [],
    unpriced_breakdown: {},
    report_currency_symbol: currencySymbol,
    report_currency_code: currencyCode,
    scope: "ingress",
    caliber: {
      scope: "ingress",
      grain: "ingress_request",
      identity_basis: "ingress_model_id",
      outcome_basis: "final_result",
      latency_basis: "ingress_end_to_end",
      cost_basis: "served_final_trusted_cost",
      datasets: ["usage_request_events"],
    },
    coverage: {},
  };
}

export function expectIngressSpendingRequest(
  request: Request,
  expectedModelID?: string | readonly string[],
) {
  const url = new URL(request.url());
  expect(request.method()).toBe("GET");
  expect([...url.searchParams.keys()].sort()).toEqual([
    "ingress_model_id",
    "preset",
    "scope",
  ]);
  expect(url.searchParams.get("preset")).toBe("all");
  expect(url.searchParams.get("scope")).toBe("ingress");

  const ingressModelID = url.searchParams.get("ingress_model_id");
  expect(ingressModelID).toEqual(expect.any(String));
  expect(ingressModelID).not.toBe("");
  if (typeof expectedModelID === "string") {
    expect(ingressModelID).toBe(expectedModelID);
  } else if (expectedModelID) {
    expect(expectedModelID).toContain(ingressModelID as string);
  }
}
