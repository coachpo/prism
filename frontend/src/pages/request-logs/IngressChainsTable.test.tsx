import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import type {
  ChainIngressItem,
  RequestLogChainRow,
} from "@/lib/types/request-logs";
import { IngressChainsTable } from "./IngressChainsTable";

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (value: string) => value,
    timezone: "UTC",
  }),
}));

function attempt(overrides: Partial<RequestLogChainRow>): RequestLogChainRow {
  return {
    request_log_id: "attempt",
    row_kind: "upstream",
    ingress_request_id: "ingress-abc",
    attempt_number: 1,
    attempt_trigger: "initial",
    attempt_result: "http_error",
    is_winner: false,
    attempt_duration_ms: 820,
    legacy_duration_ms: null,
    upstream_status_code: 503,
    gateway_status_code: null,
    legacy_status_code: null,
    error_source: "upstream",
    error_code: "UPSTREAM_503",
    failure_stage: "upstream_response",
    failure_detail_preview: null,
    failure_detail_source: "error_detail",
    failure_detail_preview_truncated: false,
    failure_detail_redacted: false,
    failure_detail_persistence_truncated: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    ingress_model_id: "entry-a",
    attempt_target_model_id: "target-b",
    attempt_target_model_label: "Model B",
    upstream_model_id: "provider/Model-B",
    endpoint_id: 7,
    endpoint_label: "Endpoint B",
    terminal_target_id: 16,
    selected_terminal_target_id: 16,
    terminal_target_label: "TT-B",
    terminal_target_configured: true,
    terminal_target_owner_model_id: "target-b",
    total_tokens: null,
    total_cost_user_currency_micros: null,
    pricing_status: "unknown",
    unpriced_reason: null,
    pricing_resolution_kind: null,
    pricing_evidence_trust: "legacy_untrusted",
    pricing_template_kind: null,
    pricing_selection_state: null,
    pricing_card_role: null,
    pricing_selector_threshold_tokens: null,
    pricing_selector_basis_tokens: null,
    created_at: "2026-08-09T10:00:01Z",
    ...overrides,
  };
}

const chain: ChainIngressItem = {
  ingress_request_id: "ingress-abc",
  started_at: "2026-08-09T10:00:00Z",
  completed_at: "2026-08-09T10:00:02Z",
  elapsed_ms: 2_000,
  elapsed_evidence_state: "authoritative",
  finalized_evidence_state: "authoritative",
  finalized_summary: {
    request_log_id: "113",
    final_status_code: 200,
    final_result: "completed",
    final_error_code: null,
    ingress_model: { id: "entry-a", label: "Model A" },
    final_target_model: { id: "target-c", label: "Model C" },
    final_upstream_model_id: "provider/Model-C",
    terminal_target: {
      id: 17,
      label: "TT-C",
      configured: true,
      owner_model_id: "target-c",
    },
    endpoint: { id: 8, label: "Endpoint C" },
    ttft_ms: 610,
    output_rate_tps: 80,
    output_rate_state: "measured",
    output_rate_reason: null,
    total_tokens: 120,
    total_cost_user_currency_micros: 4_000,
    report_currency_code: "USD",
    report_currency_symbol: "$",
    reporting_currency_epoch: 1,
    currency_attribution: "identified",
    cost_segment_key: "e.1",
    final_pricing_status: "priced",
    final_unpriced_reason: null,
    final_pricing_resolution_kind: null,
    missing_price_components: null,
    final_pricing_evidence_trust: "trusted",
    pricing_template_id_used: 1,
    pricing_template_name_snapshot: "Default",
    pricing_template_revision_id_used: 1,
    pricing_config_version_used: 1,
    pricing_version_effective_at: "2026-08-09T00:00:00Z",
    pricing_snapshot_unit: "per_million_tokens",
    pricing_snapshot_input: "1",
    pricing_snapshot_output: "1",
    pricing_snapshot_cache_read_input: null,
    pricing_snapshot_cache_creation_input: null,
    pricing_snapshot_reasoning: null,
    attempt_count: 2,
    final_attempt_number: 2,
    final_attempt_trigger: "failover",
    final_target_entry_trigger: "failover",
  },
  expected_attempt_count: 2,
  expected_request_log_row_count: 2,
  retained_upstream_attempt_count: 2,
  retained_request_log_row_count: 2,
  legacy_unknown_row_count: 0,
  chain_complete: true,
  same_target_retry_occurred: false,
  hedge_occurred: false,
  failover_occurred: true,
  routing_evidence_complete: true,
  retained_rows_loaded_count: 2,
  retained_rows_page_complete: true,
  retained_row_count: 2,
  matched_row_count: 2,
  next_row_cursor: null,
  retained_rows: [
    attempt({ request_log_id: "112" }),
    attempt({
      request_log_id: "113",
      attempt_number: 2,
      attempt_trigger: "failover",
      attempt_result: "completed",
      is_winner: true,
      attempt_duration_ms: 610,
      upstream_status_code: 200,
      error_source: null,
      error_code: null,
      failure_stage: null,
      attempt_target_model_id: "target-c",
      attempt_target_model_label: "Model C",
      upstream_model_id: "provider/Model-C",
      endpoint_id: 8,
      endpoint_label: "Endpoint C",
      terminal_target_id: 17,
      selected_terminal_target_id: 17,
      terminal_target_label: "TT-C",
      terminal_target_owner_model_id: "target-c",
      total_tokens: 120,
      total_cost_user_currency_micros: 4_000,
      pricing_status: "priced",
      pricing_evidence_trust: "trusted",
      created_at: "2026-08-09T10:00:02Z",
    }),
  ],
};

describe("IngressChainsTable route attribution", () => {
  it("separates final model from the actual outlet and explains both attempts", async () => {
    const user = userEvent.setup();
    render(
      <LocaleProvider>
        <IngressChainsTable
          chains={[chain]}
          total={1}
          hasPreviousChains={false}
          hasMoreChains={false}
          chainPageStart={0}
          chainPageCounts={{ ingress: 1, attempts: 2, rows: 2 }}
          replacing={false}
          chainRowReads={{}}
          onLoadPreviousChains={() => {}}
          onLoadNextChains={() => {}}
          onLoadMoreRows={() => {}}
          onSelectRow={() => {}}
          loading={false}
        />
      </LocaleProvider>,
    );

    const summary = screen.getByTestId("chain-summary-ingress-abc");
    expect(within(summary).getByText("Model A")).toBeInTheDocument();
    expect(within(summary).getByText("Model C")).toBeInTheDocument();
    expect(within(summary).getByText(/provider\/Model-C/)).toBeInTheDocument();
    expect(within(summary).getByText("TT-C")).toBeInTheDocument();
    expect(within(summary).getByText("Endpoint C")).toBeInTheDocument();
    expect(within(summary).getByText("80.0 tok/s")).toBeInTheDocument();
    expect(within(summary).getByText("$0.0040")).toBeInTheDocument();

    await user.click(
      within(summary).getByRole("button", {
        name: "展开或收起入口请求 ingress-abc 的尝试链",
      }),
    );

    const failed = screen.getByTestId("chain-row-112");
    expect(failed).toHaveTextContent("首次尝试");
    expect(failed).toHaveTextContent("Model B");
    expect(failed).toHaveTextContent("provider/Model-B");
    expect(failed).toHaveTextContent("TT-B");
    expect(failed).toHaveTextContent("Endpoint B");
    expect(failed).toHaveTextContent("503");
    expect(failed).toHaveTextContent("HTTP 失败");
    expect(failed).toHaveTextContent("820 ms");
    expect(
      within(failed).getByTitle(
        "失败尝试是否产生上游费用未知；路由尝试口径不声明成本。",
      ),
    ).toBeInTheDocument();

    const winner = screen.getByTestId("chain-row-113");
    expect(winner).toHaveTextContent("故障转移");
    expect(winner).toHaveTextContent("Model C");
    expect(winner).toHaveTextContent("provider/Model-C");
    expect(winner).toHaveTextContent("TT-C");
    expect(winner).toHaveTextContent("Endpoint C");
    expect(winner).toHaveTextContent("完成");
    expect(winner).toHaveTextContent("胜出");
    expect(winner).toHaveTextContent("610 ms");
    expect(winner).toHaveTextContent("120");
    expect(winner).toHaveTextContent("$0.0040");

    for (const leaked of ["initial", "failover", "http_error", "completed"]) {
      expect(
        screen.queryByText(leaked, { exact: true }),
      ).not.toBeInTheDocument();
    }
  });
});
