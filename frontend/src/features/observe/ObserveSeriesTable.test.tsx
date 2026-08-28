import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import { ObserveSeriesTable } from "./ObserveSeriesTable";

const response: UsageSeriesResponse = {
  generated_at: "2026-08-09T00:00:00Z",
  coverage: {
    requested_preset: "24h",
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-09T00:00:00Z",
    retention_from_time: null,
    source: "raw",
    complete: true,
    gaps: [],
  },
  metric: "attempts",
  group_by: "none",
  selection_basis: "request_count",
  interval: "1h",
  series_limit: 6,
  truncated: false,
  caliber: { scope: "route_attempt" },
  dataset_coverage: {},
  samples: { observation_count: 3 },
  series: [
    {
      key: "total",
      entity_id: null,
      label: "合计",
      configured: null,
      request_count: 3,
      points: [
        {
          bucket_start: "2026-08-08T00:00:00Z",
          request_count: 3,
          http_success_count: 2,
          http_failed_count: 1,
          failed_count: 1,
          client_disconnected_count: 0,
          ttft_sample_count: 0,
          p50_ttft_ms: null,
          p95_ttft_ms: null,
          total_tokens: null,
          known_cost_micros: null,
          output_rate_sample_count: 0,
          avg_output_rate_tps: null,
          cache_basis_request_count: 0,
          cache_basis_input_tokens: null,
          cache_basis_cache_read_tokens: null,
          cache_basis_cache_creation_tokens: null,
          pricing_reconciliation: {
            pricing_eligible_request_count: 0,
            pricing_ineligible_request_count: 3,
            priced_request_count: 0,
            unpriced_request_count: 0,
            pricing_unknown_request_count: 0,
            unpriced_reason_counts: {
              PRICING_DISABLED: 0,
              MISSING_TOKEN_USAGE: 0,
              STREAM_USAGE_UNAVAILABLE: 0,
              MISSING_PRICE_DATA: 0,
            },
            pricing_coverage_state: "no_eligible",
          },
        },
      ],
    },
  ],
};

describe("ObserveSeriesTable scope labels", () => {
  it("labels route-attempt observations as attempts", () => {
    render(
      <LocaleProvider>
        <ObserveSeriesTable
          formatBucket={(value) => value}
          fragment={{
            phase: "ready",
            data: response,
            stale: false,
            error: null,
            retryAfterMs: null,
          }}
          metric="attempts"
        />
      </LocaleProvider>,
    );

    expect(
      screen.getByRole("columnheader", { name: "窗口合计 · 尝试数" }),
    ).toBeInTheDocument();
  });
});
