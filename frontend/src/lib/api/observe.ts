import { buildQuery, request } from "./core";

export type ObserveCoverage = {
  requested_preset: string;
  from_time: string;
  to_time: string;
  retention_from_time: string | null;
  source: "raw" | "rollup" | "hybrid";
  complete: boolean;
  gaps: { from_time: string; to_time: string; reason: string }[];
  retention_epoch?: string;
  retention_generation?: string;
  purge_state?: string;
  source_revision?: string;
};

export type ObserveCostSegment = {
  segment_key: string;
  reporting_currency_epoch: number | null;
  currency_attribution: "identified" | "legacy_unknown";
  currency_code: string | null;
  display_symbol: string | null;
  observed_symbols: string[];
  observed_symbol_count: number;
  observed_symbols_truncated: boolean;
  request_count: number;
  pricing_eligible_request_count: number;
  pricing_ineligible_request_count: number;
  priced_request_count: number;
  unpriced_request_count: number;
  pricing_unknown_request_count: number;
  unpriced_reason_counts: {
    PRICING_DISABLED: number;
    MISSING_TOKEN_USAGE: number;
    STREAM_USAGE_UNAVAILABLE: number;
    MISSING_PRICE_DATA: number;
  };
  pricing_coverage_state:
    | "complete"
    | "partial"
    | "no_trusted_cost"
    | "no_eligible";
  known_cost_micros: string | null;
  pricing_card_role_breakdown: {
    card_role: "standard" | "tier_base" | "tier_above" | "peak" | "offpeak";
    request_count: number;
    priced_request_count: number;
    known_cost_micros: string | null;
  }[];
};

export type QueryContextResponse = {
  query_context: string;
  requested_bounds: { from_time: string; to_time: string } | null;
  usage_bounds: { from_time: string; to_time: string };
  usage_coverage: ObserveCoverage;
  event_bounds: { from_time: string; to_time: string };
  event_coverage: ObserveCoverage;
  request_bounds: { from_time: string; to_time: string };
  request_coverage: ObserveCoverage;
  generated_at: string;
  caliber?: {
    scope: "ingress" | "final_execution" | "route_attempt";
    [key: string]: unknown;
  };
};

export type UsageSummaryResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  cost_segments: ObserveCostSegment[];
  request_count: number;
  http_success_count: number;
  http_failed_count: number;
  http_success_rate: number | null;
  completed_count: number;
  stream_error_count: number;
  client_disconnected_count: number;
  failed_count: number;
  ttft_sample_count: number;
  p50_ttft_ms: number | null;
  p95_ttft_ms: number | null;
  output_rate_sample_count: number;
  avg_output_rate_tps: number | null;
  total_tokens: number | null;
  cache_basis_request_count: number;
  cache_basis_input_tokens: number | null;
  cache_basis_cache_read_tokens: number | null;
  cache_basis_cache_creation_tokens: number | null;
  pricing_selector_unresolved_count: number;
  pricing_reconciliation: {
    pricing_eligible_request_count: number;
    pricing_ineligible_request_count: number;
    priced_request_count: number;
    unpriced_request_count: number;
    pricing_unknown_request_count: number;
    unpriced_reason_counts: {
      PRICING_DISABLED: number;
      MISSING_TOKEN_USAGE: number;
      STREAM_USAGE_UNAVAILABLE: number;
      MISSING_PRICE_DATA: number;
    };
    pricing_coverage_state:
      | "complete"
      | "partial"
      | "no_trusted_cost"
      | "no_eligible";
  };
  window_average_rpm: number | null;
  window_average_tpm: number | null;
};

export type UsageSeriesResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  metric: string;
  group_by: string;
  selection_basis: string;
  interval: string;
  series_limit: number;
  truncated: boolean;
  series: {
    key: string;
    entity_id: string | null;
    label: string;
    configured: boolean | null;
    request_count: number;
    points: {
      bucket_start: string;
      request_count: number;
      http_success_count: number;
      http_failed_count: number;
      failed_count: number;
      client_disconnected_count: number;
      ttft_sample_count: number;
      p50_ttft_ms: number | null;
      p95_ttft_ms: number | null;
      total_tokens: number | null;
      known_cost_micros: string | null;
      output_rate_sample_count: number;
      avg_output_rate_tps: number | null;
      cache_basis_request_count: number;
      cache_basis_input_tokens: number | null;
      cache_basis_cache_read_tokens: number | null;
      cache_basis_cache_creation_tokens: number | null;
      pricing_reconciliation: UsageSummaryResponse["pricing_reconciliation"];
    }[];
  }[];
};

export type DashboardNowResponse = {
  generated_at: string;
  health: { stale: boolean; cache_lag_ms: number | null };
  rolling: {
    window_minutes: number;
    coverage: ObserveCoverage;
    request_count: number;
    token_sample_count: number;
    token_coverage_complete: boolean;
    token_count: number | null;
    rpm: number | null;
    tpm: number | null;
  };
  enabled_model_count: number;
};

export const observe = {
  observeActivity: (
    queryContext: string,
    params: { limit?: number; before?: string },
    signal?: AbortSignal,
  ) => {
    const query = buildQuery({ ...params, query_context: queryContext });
    return request<ObserveActivityResponse>(
      `/api/stats/observe-activity${query ? `?${query}` : ""}`,
      { signal },
    );
  },
  usageErrors: (
    queryContext: string,
    params: { group_by?: string; limit?: number },
    signal?: AbortSignal,
  ) => {
    const query = buildQuery({ ...params, query_context: queryContext });
    return request<UsageErrorsResponse>(
      `/api/stats/usage-errors${query ? `?${query}` : ""}`,
      { signal },
    );
  },
  queryContext: (
    params: {
      preset: string;
      from_time?: string;
      to_time?: string;
      scope?: "ingress" | "final_execution" | "route_attempt";
    },
    signal?: AbortSignal,
  ) => {
    const query = buildQuery(params);
    return request<QueryContextResponse>(
      `/api/stats/query-context${query ? `?${query}` : ""}`,
      { signal },
    );
  },
  usageSummary: (queryContext: string, signal?: AbortSignal) =>
    request<UsageSummaryResponse>(
      `/api/stats/usage-summary?query_context=${encodeURIComponent(queryContext)}`,
      { signal },
    ),
  usageSeries: (
    queryContext: string,
    params: { metric?: string; group_by?: string; interval?: string },
    signal?: AbortSignal,
  ) => {
    const query = buildQuery({ ...params, query_context: queryContext });
    return request<UsageSeriesResponse>(
      `/api/stats/usage-series${query ? `?${query}` : ""}`,
      { signal },
    );
  },
  dashboardNow: (signal?: AbortSignal) =>
    request<DashboardNowResponse>("/api/stats/dashboard/now", { signal }),
};

export type UsageErrorsResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  requests_context: {
    view: string;
    query_context: string;
    final_from_time: string;
    final_to_time: string;
    base_request_filters: Record<string, string[]>;
  };
  summary: {
    request_count: number;
    http_error_count: number;
    stream_error_count: number;
    failed_count: number;
    client_disconnected_count: number;
    diagnostic_stream_anomaly_count: number;
  };
  timeline: {
    bucket_start: string;
    http_error_count: number;
    stream_error_count: number;
    failed_count: number;
    client_disconnected_count: number;
  }[];
  http_statuses: {
    status_code: number;
    count: number;
    denominator: number;
    percentage: number | null;
    last_seen_at: string;
    request_filters: Record<string, string[]>;
  }[];
  stream_outcomes: {
    stream_outcome: string;
    count: number;
    denominator: number;
    percentage: number | null;
    last_seen_at: string;
    request_filters: Record<string, string[]>;
    error_kinds: {
      stream_error_kind: string | null;
      count: number;
      denominator: number;
      percentage: number | null;
      request_filters: Record<string, string[]>;
    }[];
    other_error_kinds: {
      count: number;
      denominator: number;
      percentage: number | null;
      request_filters: Record<string, string[]> | null;
    };
  }[];
  groups: {
    entity_type: string;
    entity_id: string | null;
    label: string;
    configured: boolean | null;
    problem_count: number;
    failed_count: number;
    client_disconnected_count: number;
    denominator: number;
    percentage: number | null;
    last_seen_at: string;
    request_filters: Record<string, string[]>;
  }[];
  other: {
    http_statuses: {
      count: number;
      denominator: number;
      percentage: number | null;
      request_filters: Record<string, string[]> | null;
    };
    stream_outcomes: {
      count: number;
      denominator: number;
      percentage: number | null;
      request_filters: Record<string, string[]> | null;
    };
    groups: {
      count: number;
      denominator: number;
      percentage: number | null;
      request_filters: Record<string, string[]> | null;
    };
  };
};

export type ObserveActivityItem = {
  usage_event_id: string;
  final_ingress_request_id: string;
  created_at: string;
  ingress_model_id: string;
  ingress_model_label: string;
  final_target_model_id: string | null;
  final_target_model_label: string | null;
  route_changed: boolean;
  attempt_count: number;
  routing_evidence_complete: boolean;
  endpoint_id: number | null;
  endpoint_label: string;
  terminal_target_id: number | null;
  status_code: number;
  final_result: "completed" | "failed" | "client_disconnected";
  outcome_detail:
    | "completed"
    | "http_error"
    | "stream_error"
    | "client_disconnected";
  is_stream: boolean | null;
  stream_outcome: string;
  stream_error_kind: string | null;
  ttft_ms: number | null;
  total_duration_ms: number | null;
  output_tokens: number | null;
  total_tokens: number | null;
  known_cost_micros: string | null;
  final_pricing_status: string;
  final_unpriced_reason: string | null;
  reporting_currency_epoch: number | null;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
};

export type ObserveActivityResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  items: ObserveActivityItem[];
  has_more: boolean;
  caliber?: Record<string, unknown>;
  dataset_coverage?: Record<string, unknown>;
  samples?: Record<string, number>;
};

// ---- Model routing diagnostics (model connection UX pair) ----
