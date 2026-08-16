// Honesty Contract, request-log list: a failed read owns the table area and a
// genuinely empty result keeps the empty state. The two must never be rendered
// as the same thing — the reported defect was a 422 that produced 「共 0 行」,
// 「0 条结果」 and 「当前范围内没有匹配的请求日志」 next to the failure callout.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { getStaticMessages } from "@/i18n/staticMessages";
import { ApiError } from "@/lib/api/core";
import type { QueryCoverage, RequestLogListItem, RequestLogListResponse } from "@/lib/types";
import type { ChainResponse, RequestLogRowV2 } from "@/lib/types/request-logs-v2";
import { RequestLogsPage } from "@/pages/RequestLogsPage";

const listRequests = vi.fn<(params?: unknown) => Promise<RequestLogListResponse>>();
const listChains = vi.fn<(params?: unknown) => Promise<unknown>>();
const requestDetail = vi.fn<(requestId: string) => Promise<never>>();

vi.mock("@/lib/api", () => ({
  api: {
    stats: {
      requests: (params?: unknown) => listRequests(params),
      chains: (params?: unknown) => listChains(params),
      proxyApiKeyFilterOptions: () => Promise.resolve({ items: [], selected: null }),
      requestDetail: (requestId: string) => requestDetail(requestId),
      exportCsv: () => Promise.reject(new Error("export is not exercised here")),
    },
    settings: {
      costing: { get: () => Promise.resolve({ timezone_preference: "UTC" }) },
    },
  },
}));

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const messages = getStaticMessages().requestLogs;
const honesty = getStaticMessages().honesty;

function coverage(): QueryCoverage {
  return {
    requested_from_time: "2026-08-12T00:00:00Z",
    requested_to_time: "2026-08-13T00:00:00Z",
    effective_from_time: "2026-08-12T00:00:00Z",
    effective_to_time: "2026-08-13T00:00:00Z",
    complete: true,
    gaps: [],
    state: "known",
    source_revision: "rev-1",
  };
}

function listResponse(items: RequestLogListItem[]): RequestLogListResponse {
  return {
    items,
    total: items.length,
    limit: 50,
    offset: 0,
    filter_options: { endpoints: [], models: [], clients: [], resolved_target_models: [] },
    coverage: coverage(),
  };
}

function row(requestLogId: string): RequestLogListItem {
  return {
    request_log_id: requestLogId,
    row_kind: "upstream",
    ingress_request_id: `ingress-${requestLogId}`,
    attempt_number: 1,
    attempt_trigger: "initial",
    attempt_result: "completed",
    is_winner: true,
    created_at: "2026-08-12T12:00:00Z",
    model_id: "gpt-4o",
    model_label: "gpt-4o",
    resolved_target_model_id: "gpt-4o",
    resolved_target_model_label: "gpt-4o",
    caller_client_display: null,
    upstream_client_display: null,
    user_agent_overridden: false,
    api_family: "openai",
    endpoint_id: 1,
    endpoint_label: "Primary",
    terminal_target_id: 1,
    terminal_target_label: "Primary target",
    terminal_target_configured: true,
    upstream_status_code: 200,
    gateway_status_code: null,
    legacy_status_code: null,
    attempt_duration_ms: 900,
    legacy_duration_ms: null,
    ttft_ms: 200,
    completion_duration_ms: 700,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    error_source: null,
    error_code: null,
    failure_stage: null,
    failure_detail_preview: null,
    failure_detail_source: "error_detail",
    failure_detail_preview_truncated: false,
    failure_detail_redacted: false,
    reasoning_effort: null,
    output_tokens: 12,
    total_tokens: 30,
    total_cost_user_currency_micros: 1000,
    pricing_status: "priced",
    pricing_evidence_trust: "trusted",
    unpriced_reason: null,
    report_currency_symbol: "$",
    proxy_api_key_id: null,
    proxy_api_key_name_snapshot: null,
    proxy_api_key_attribution_state: "unknown",
    proxy_api_key_auth_enforced_at_request: null,
  };
}

function chainRow(requestLogId: string, attemptNumber: number): RequestLogRowV2 {
  return {
    request_log_id: requestLogId,
    row_kind: "upstream",
    ingress_request_id: "ingress-row-pages",
    attempt_number: attemptNumber,
    attempt_trigger: attemptNumber === 1 ? "initial" : "failover",
    attempt_result: "completed",
    is_winner: attemptNumber === 2,
    attempt_duration_ms: 100,
    legacy_duration_ms: null,
    upstream_status_code: 200,
    gateway_status_code: null,
    legacy_status_code: null,
    error_source: null,
    error_code: null,
    failure_stage: null,
    failure_detail_preview: null,
    failure_detail_source: "error_detail",
    failure_detail_preview_truncated: false,
    failure_detail_redacted: false,
    failure_detail_persistence_truncated: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    model_id: "gpt-4o",
    resolved_target_model_id: "gpt-4o",
    endpoint_id: 1,
    terminal_target_id: 1,
    terminal_target_label: "Primary target",
    terminal_target_configured: true,
    terminal_target_owner_model_id: "gpt-4o",
    total_tokens: 30,
    total_cost_user_currency_micros: "1000",
    pricing_status: "priced",
    unpriced_reason: null,
    pricing_evidence_trust: "trusted",
    created_at: `2026-08-12T12:00:0${attemptNumber}Z`,
  };
}

function chainResponse(row: RequestLogRowV2, pageComplete: boolean, nextRowCursor: string | null): ChainResponse {
  return {
    view: "ingress_chains",
    query_context: null,
    source_ingress_total: 1,
    retained_ingress_total: 1,
    retained_upstream_attempt_total: 2,
    retained_request_log_row_total: 2,
    legacy_unknown_row_total: 0,
    page_ingress_count: 1,
    page_upstream_attempt_count: 2,
    page_request_log_row_count: 2,
    items: [{
      ingress_request_id: "ingress-row-pages",
      started_at: null,
      completed_at: null,
      elapsed_ms: null,
      elapsed_evidence_state: "unavailable",
      finalized_evidence_state: "unavailable",
      finalized_summary: null,
      expected_attempt_count: null,
      expected_request_log_row_count: null,
      retained_upstream_attempt_count: 2,
      retained_request_log_row_count: 2,
      legacy_unknown_row_count: 0,
      chain_complete: null,
      same_target_retry_occurred: false,
      hedge_occurred: false,
      failover_occurred: true,
      routing_evidence_complete: null,
      retained_rows_loaded_count: 1,
      retained_rows_page_complete: pageComplete,
      retained_row_count: 2,
      matched_row_count: 0,
      next_row_cursor: nextRowCursor,
      retained_rows: [row],
    }],
    has_more_chains: false,
    next_chain_cursor: null,
  };
}

function renderRequestLogsPage(search = "?view=attempts") {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const requestsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/observe/requests",
    component: RequestLogsPage,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([requestsRoute]),
    history: createMemoryHistory({ initialEntries: [`/observe/requests${search}`] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(
    <QueryClientProvider client={queryClient}>
      <LocaleProvider>
        <RouterProvider router={router} />
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

describe("request-log list read failure", () => {
  beforeEach(() => {
    globalThis.ResizeObserver ??= ResizeObserverStub as never;
    window.localStorage.clear();
    listRequests.mockReset();
    listChains.mockReset();
    requestDetail.mockReset();
    requestDetail.mockRejectedValue(new Error("detail is not exercised here"));
  });

  it("replaces the table with a failure surface instead of an empty result", async () => {
    listRequests.mockRejectedValue(new ApiError("请求失败", 422, null));

    renderRequestLogsPage();

    const failure = await screen.findByTestId("request-logs-load-error", undefined, { timeout: 3000 });
    expect(failure).toHaveTextContent(messages.loadFailed);
    expect(failure).toHaveTextContent(honesty.readFailedDescription);

    // The four situations must stay distinguishable: nothing here may claim a
    // real zero or a real empty result set.
    expect(screen.queryByTestId("request-logs-table")).not.toBeInTheDocument();
    expect(screen.queryByText(messages.noRequestLogsMatchSlice)).not.toBeInTheDocument();
    expect(screen.queryByText(messages.totalRowsSummary("0"))).not.toBeInTheDocument();
    expect(screen.queryByText(messages.zeroResults)).not.toBeInTheDocument();
  });

  it("retries the read from the failure surface", async () => {
    const user = userEvent.setup();
    listRequests.mockRejectedValueOnce(new ApiError("请求失败", 422, null));
    listRequests.mockResolvedValue(listResponse([row("101")]));

    renderRequestLogsPage();

    await screen.findByTestId("request-logs-load-error", undefined, { timeout: 3000 });
    await user.click(screen.getByRole("button", { name: getStaticMessages().common.retry }));

    expect(await screen.findByTestId("request-log-row-101")).toBeInTheDocument();
    expect(screen.queryByTestId("request-logs-load-error")).not.toBeInTheDocument();
  });

  it("states no chain counts when the landing view's read fails", async () => {
    listChains.mockRejectedValue(new ApiError("请求失败", 422, null));

    renderRequestLogsPage("?view=ingress_chains");

    await screen.findByTestId("request-logs-load-error", undefined, { timeout: 3000 });
    expect(screen.queryByTestId("ingress-chains-table")).not.toBeInTheDocument();
    expect(screen.queryByTestId("chain-page-counts")).not.toBeInTheDocument();
    expect(screen.queryByText(messages.chainCounts("0", "0", "0"))).not.toBeInTheDocument();
    expect(screen.queryByText(messages.chainEmpty)).not.toBeInTheDocument();
  });

  it("keeps the empty state for a successful read with no matching rows", async () => {
    listRequests.mockResolvedValue(listResponse([]));

    renderRequestLogsPage();

    expect(await screen.findByText(messages.noRequestLogsMatchSlice, undefined, { timeout: 3000 })).toBeInTheDocument();
    expect(screen.getByText(messages.totalRowsSummary("0"))).toBeInTheDocument();
    expect(screen.queryByTestId("request-logs-load-error")).not.toBeInTheDocument();
  });

  it("labels a failed refresh as stale instead of repainting the retained rows as fresh", async () => {
    const user = userEvent.setup();
    listRequests.mockResolvedValueOnce(listResponse([row("101")]));
    listRequests.mockRejectedValue(new ApiError("请求失败", 422, null));

    renderRequestLogsPage();

    await screen.findByTestId("request-log-row-101", undefined, { timeout: 3000 });
    await user.click(screen.getByRole("button", { name: messages.refreshRequestLogs }));

    await waitFor(() => expect(screen.getByTestId("request-logs-stale-badge")).toBeInTheDocument());
    expect(screen.getByTestId("request-logs-stale-badge")).toHaveAttribute("title", "请求失败");
    expect(screen.getByTestId("request-log-row-101")).toBeInTheDocument();
    expect(screen.queryByTestId("request-logs-load-error")).not.toBeInTheDocument();
  });

  it("loads the next signed row page and preserves BIGINT request ids", async () => {
    const user = userEvent.setup();
    const firstId = "9007199254740995";
    const secondId = "9007199254740997";
    listChains
      .mockResolvedValueOnce(chainResponse(chainRow(firstId, 1), false, "signed-row-cursor"))
      .mockResolvedValueOnce(chainResponse(chainRow(secondId, 2), true, null));

    renderRequestLogsPage("?view=ingress_chains");

    await screen.findByTestId("chain-summary-ingress-row-pages", undefined, { timeout: 3000 });
    await user.click(screen.getByRole("button", { name: messages.chainToggleAria("ingress-row-pages") }));
    expect(screen.getByTestId(`chain-row-${firstId}`)).toBeInTheDocument();

    await user.click(screen.getByTestId("chain-rows-more-ingress-row-pages"));

    await waitFor(() => expect(listChains).toHaveBeenCalledTimes(2));
    expect(listChains.mock.calls[1]?.[0]).toMatchObject({
      view: "ingress_chains",
      ingress_request_id: "ingress-row-pages",
      chain_limit: 1,
      row_cursor: "signed-row-cursor",
      chain_cursor: undefined,
    });
    expect(screen.getByTestId(`chain-row-${firstId}`)).toBeInTheDocument();
    expect(await screen.findByTestId(`chain-row-${secondId}`)).toBeInTheDocument();
    expect(screen.queryByTestId("chain-rows-more-ingress-row-pages")).not.toBeInTheDocument();

    await user.click(screen.getByTestId(`chain-row-${secondId}`));
    await waitFor(() => expect(requestDetail).toHaveBeenCalledWith(secondId));
    expect(listChains).toHaveBeenCalledTimes(2);
  });
});
