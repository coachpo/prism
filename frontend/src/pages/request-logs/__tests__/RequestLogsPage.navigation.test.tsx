import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { RequestLogDetail, RequestLogListItem } from "@/lib/types";

let RequestLogsPage: typeof import("../../RequestLogsPage").RequestLogsPage;

const {
  mockUseRequestLogDetail,
  mockUseRequestLogPageState,
  mockUseRequestLogsPageData,
} = vi.hoisted(() => ({
  mockUseRequestLogDetail: vi.fn(),
  mockUseRequestLogPageState: vi.fn(),
  mockUseRequestLogsPageData: vi.fn(),
}));

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({
    revision: 3,
    selectedProfileId: 7,
  }),
}));

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (isoString: string) => `formatted:${isoString}`,
  }),
}));

vi.mock("@/i18n/useLocale", () => ({
  useLocale: () => ({
    messages: {
      requestLogs: {
        requestLogsTitle: "Request logs",
        requestLogsDescription: "Inspect request activity",
        requestNotFound: "Request not found",
        requestNotFoundDescription: (requestId: string | null) =>
          `Request ${requestId ?? ""} not found`,
        returnToRequestList: "Return to request list",
      },
    },
  }),
}));

vi.mock("@/components/PageHeader", () => ({
  PageHeader: ({ title, description }: { title: string; description: string }) => (
    <div>
      <h1>{title}</h1>
      <p>{description}</p>
    </div>
  ),
}));

vi.mock("../useRequestLogPageState", () => ({
  useRequestLogPageState: () => mockUseRequestLogPageState(),
}));

vi.mock("../useRequestLogsPageData", () => ({
  useRequestLogsPageData: (params: unknown) => mockUseRequestLogsPageData(params),
}));

vi.mock("../useRequestLogDetail", () => ({
  useRequestLogDetail: (params: { requestId: number | null; enabled: boolean }) =>
    mockUseRequestLogDetail(params),
}));

vi.mock("../clientFilters", () => ({
  applyClientFilters: () => {
    throw new Error("RequestLogsPage should not import clientFilters");
  },
}));

vi.mock("../RequestFocusBanner", () => ({
  RequestFocusBanner: ({ requestId }: { requestId: string | null }) => (
    <div>{`request-focus-banner:${requestId ?? ""}`}</div>
  ),
}));

vi.mock("../FiltersBar", () => ({
  FiltersBar: () => <div>filters-bar</div>,
}));

vi.mock("../RequestLogsTable", () => ({
  RequestLogsTable: ({
    activeRequestId,
    items,
    onSelectRequest,
  }: {
    activeRequestId: number | null;
    items: RequestLogListItem[];
    onSelectRequest: (id: number) => void;
  }) => (
    <div data-testid="request-logs-table">
      <div>{`active-request:${activeRequestId ?? "none"}`}</div>
      {items[0] ? (
        <button type="button" onClick={() => onSelectRequest(items[0].id)}>
          open-request
        </button>
      ) : null}
    </div>
  ),
}));

vi.mock("../RequestLogDetailSheet", () => ({
  RequestLogDetailSheet: ({
    onClose,
    open,
    request,
  }: {
    activeTab: "overview" | "audit";
    onClose: () => void;
    onTabChange: (tab: "overview" | "audit") => void;
    open: boolean;
    request: RequestLogDetail | null;
  }) => {
    if (!open || !request) {
      return null;
    }

    return (
      <div data-testid="request-log-detail-sheet">
        <div>{`Request #${request.summary.id}`}</div>
        <button type="button" onClick={onClose}>
          close-request
        </button>
      </div>
    );
  },
}));

function buildRequestLogListItem(
  overrides: Partial<RequestLogListItem> = {},
): RequestLogListItem {
  return {
    id: 42,
    created_at: "2026-03-16T00:00:00.000Z",
    model_id: "gpt-5.4",
    resolved_target_model_id: null,
    api_family: "openai",
    vendor_id: 1,
    vendor_key: "openai",
    vendor_name: "OpenAI",
    endpoint_id: 12,
    connection_id: 34,
    status_code: 200,
    response_time_ms: 912,
    is_stream: true,
    total_tokens: 150,
    total_cost_user_currency_micros: 3000,
    report_currency_symbol: "$",
    ...overrides,
  };
}

function buildRequestLogDetail(
  overrides: Partial<RequestLogDetail> = {},
): RequestLogDetail {
  return {
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
      ingress_request_id: null,
      attempt_number: null,
      provider_correlation_id: null,
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
    ...overrides,
  };
}

function createPageState(
  overrides: Partial<{
    detail_tab: "overview" | "audit";
    request_id: string;
  }> = {},
) {
  return {
    state: {
      ingress_request_id: "",
      model_id: "",
      api_family: "",
      endpoint_id: "",
      time_range: "24h",
      status_family: "all",
      search: "",
      outcome_filter: "all",
      stream_filter: "all",
      latency_bucket: "all",
      token_min: "",
      token_max: "",
      triage: false,
      request_id: "",
      detail_tab: "overview" as const,
      limit: 100,
      offset: 0,
      ...overrides,
    },
    isExactMode: overrides.request_id !== undefined && overrides.request_id !== "",
    clearRequest: vi.fn(),
    setDetailTab: vi.fn(),
    setLimit: vi.fn(),
    goToNextPage: vi.fn(),
    goToPreviousPage: vi.fn(),
  };
}

beforeAll(async () => {
  ({ RequestLogsPage } = await import("../../RequestLogsPage"));
});

describe("RequestLogsPage request detail loading", () => {
  beforeEach(() => {
    const stableDetail = buildRequestLogDetail();

    mockUseRequestLogDetail.mockReset();
    mockUseRequestLogPageState.mockReset();
    mockUseRequestLogsPageData.mockReset();

    mockUseRequestLogPageState.mockReturnValue(createPageState());
    mockUseRequestLogsPageData.mockReturnValue({
      items: [buildRequestLogListItem()],
      total: 1,
      loading: false,
      error: null,
      filterOptions: { apiFamilies: [], endpoints: [], models: [] },
      filterOptionsLoaded: true,
      refresh: vi.fn(),
    });
    mockUseRequestLogDetail.mockImplementation(
      ({ requestId }: { requestId: number | null; enabled: boolean }) => ({
        request: requestId === 42 ? stableDetail : null,
        loading: false,
        error: null,
        notFound: false,
        refresh: vi.fn(),
      }),
    );
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it("loads exact-request detail from the dedicated hook instead of the list payload", () => {
    mockUseRequestLogPageState.mockReturnValue(createPageState({ request_id: "42" }));
    mockUseRequestLogsPageData.mockReturnValue({
      items: [buildRequestLogListItem({ id: 7 })],
      total: 1,
      loading: false,
      error: null,
      filterOptions: { apiFamilies: [], endpoints: [], models: [] },
      filterOptionsLoaded: true,
      refresh: vi.fn(),
    });

    render(<RequestLogsPage />);

    expect(mockUseRequestLogsPageData).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: false,
      }),
    );
    expect(mockUseRequestLogDetail).toHaveBeenCalledWith({
      requestId: 42,
      enabled: true,
    });
    expect(screen.getByTestId("request-log-detail-sheet")).toBeInTheDocument();
    expect(screen.getByText("Request #42")).toBeInTheDocument();
  });

  it("renders the exact-mode not-found state from the detail hook 404", () => {
    mockUseRequestLogPageState.mockReturnValue(createPageState({ request_id: "42" }));
    mockUseRequestLogsPageData.mockReturnValue({
      items: [buildRequestLogListItem({ id: 7 })],
      total: 1,
      loading: false,
      error: null,
      filterOptions: { apiFamilies: [], endpoints: [], models: [] },
      filterOptionsLoaded: true,
      refresh: vi.fn(),
    });
    mockUseRequestLogDetail.mockImplementation(
      ({ requestId }: { requestId: number | null; enabled: boolean }) => ({
        request: null,
        loading: false,
        error: null,
        notFound: requestId === 42,
        refresh: vi.fn(),
      }),
    );

    render(<RequestLogsPage />);

    expect(screen.getByTestId("request-log-not-found")).toBeInTheDocument();
    expect(screen.queryByTestId("request-log-detail-sheet")).not.toBeInTheDocument();
  });

  it("clears exact mode when the focused request is closed", () => {
    const pageState = createPageState({ request_id: "42" });
    mockUseRequestLogPageState.mockReturnValue(pageState);

    render(<RequestLogsPage />);

    fireEvent.click(screen.getByRole("button", { name: "close-request" }));

    expect(pageState.clearRequest).toHaveBeenCalledTimes(1);
  });
});
