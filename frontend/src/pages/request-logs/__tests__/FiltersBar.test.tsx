import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { FiltersBar } from "../FiltersBar";
import { FiltersBarPrimaryFilters } from "../FiltersBarPrimaryFilters";

function renderWithLocale(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

function createPrimaryActions() {
  return {
    setIngressRequestId: vi.fn(),
    setRequestId: vi.fn(),
    setEndpointId: vi.fn(),
    setModelId: vi.fn(),
    setApiFamily: vi.fn(),
    setSearch: vi.fn(),
    setStatusFamily: vi.fn(),
    setTimeRange: vi.fn(),
  };
}

function createPrimaryState(overrides: Record<string, unknown> = {}) {
  return {
    ingress_request_id: "",
    endpoint_id: "",
    model_id: "",
    api_family: "",
    search: "",
    status_family: "all" as const,
    time_range: "1h" as const,
    ...overrides,
  };
}

function createFilterOptions(overrides: Record<string, unknown> = {}) {
  return {
    apiFamilies: [],
    endpoints: [],
    models: [],
    ...overrides,
  };
}

function createFiltersBarActions() {
  return {
    state: {
      ...createPrimaryState(),
      outcome_filter: "all" as const,
      stream_filter: "all" as const,
      latency_bucket: "all" as const,
      token_min: "",
      token_max: "",
      triage: false,
      request_id: "",
      detail_tab: "overview" as const,
      limit: 100,
      offset: 0,
    },
    isExactMode: false,
    hasActiveFilters: false,
    clearFilters: vi.fn(),
    setIngressRequestId: vi.fn(),
    setRequestId: vi.fn(),
    setEndpointId: vi.fn(),
    setModelId: vi.fn(),
    setApiFamily: vi.fn(),
    setSearch: vi.fn(),
    setStatusFamily: vi.fn(),
    setTimeRange: vi.fn(),
    setOutcomeFilter: vi.fn(),
    setStreamFilter: vi.fn(),
    setLatencyBucket: vi.fn(),
    setTokenMin: vi.fn(),
    setTokenMax: vi.fn(),
    setTriage: vi.fn(),
    setLimit: vi.fn(),
    setOffset: vi.fn(),
    selectRequest: vi.fn(),
    clearRequest: vi.fn(),
    setDetailTab: vi.fn(),
    goToNextPage: vi.fn(),
    goToPreviousPage: vi.fn(),
  };
}

describe("request log filters", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("renders only request lookup and retained browse filters in the primary controls", () => {
    const actions = createPrimaryActions();
    const filterOptions = createFilterOptions();
    const state = createPrimaryState();

    renderWithLocale(
      <FiltersBarPrimaryFilters
        filterOptions={filterOptions}
        filterOptionsLoaded={true}
        state={state}
        actions={actions}
      />,
    );

    const requestIdInput = screen.getByPlaceholderText("Request ID");
    expect(requestIdInput).toHaveAttribute("name", "request_id_lookup");
    expect(requestIdInput).toHaveAttribute("autocomplete", "off");
    fireEvent.change(requestIdInput, { target: { value: "42" } });
    fireEvent.keyDown(requestIdInput, { key: "Enter", code: "Enter", charCode: 13 });

    const ingressRequestIdInput = screen.getByPlaceholderText("Ingress request ID");
    expect(ingressRequestIdInput).toHaveAttribute("name", "ingress_request_id");
    expect(ingressRequestIdInput).toHaveAttribute("autocomplete", "off");
    fireEvent.change(ingressRequestIdInput, {
      target: { value: "ingress_req_42" },
    });

    expect(screen.queryByText("Search")).not.toBeInTheDocument();
    expect(screen.queryByText("API Family")).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/model, vendor, path, or error/i)).not.toBeInTheDocument();
    expect(screen.getByText("Request ID")).toBeInTheDocument();
    expect(screen.getByText("Ingress request ID")).toBeInTheDocument();
    expect(screen.getByText("Model")).toBeInTheDocument();
    expect(screen.getByText("Endpoint")).toBeInTheDocument();
    expect(screen.getByText("Status")).toBeInTheDocument();
    expect(screen.getByText(/time range/i)).toBeInTheDocument();
    expect(screen.queryByText("Connection")).not.toBeInTheDocument();
    expect(actions.setRequestId).toHaveBeenCalledWith("42");
    expect(actions.setIngressRequestId).toHaveBeenCalledWith("ingress_req_42");
  });

  it("renders a single simplified filter surface without removed local refinements", () => {
    const actions = createFiltersBarActions();

    renderWithLocale(
      <FiltersBar
        actions={actions}
        filterOptions={createFilterOptions()}
        filterOptionsLoaded={true}
        onRefresh={vi.fn()}
        isRefreshing={false}
      />,
    );

    expect(screen.queryByText("Outcome")).not.toBeInTheDocument();
    expect(screen.queryByText("Stream")).not.toBeInTheDocument();
    expect(screen.queryByText("Latency")).not.toBeInTheDocument();
    expect(screen.queryByText("Token range")).not.toBeInTheDocument();
    expect(screen.queryByText("Triage")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /refresh request logs/i })).toBeInTheDocument();
    expect(screen.queryByText("Local refinement")).not.toBeInTheDocument();
    expect(screen.queryByText("View")).not.toBeInTheDocument();
    expect(screen.queryByText("Connection")).not.toBeInTheDocument();
  });

  it.fails("clears the request lookup draft when browse filters are cleared", () => {
    const actions = createFiltersBarActions();
    actions.hasActiveFilters = true;
    actions.state = {
      ...actions.state,
      ingress_request_id: "ingress_req_42",
    };

    const view = renderWithLocale(
      <FiltersBar
        actions={actions}
        filterOptions={createFilterOptions()}
        filterOptionsLoaded={true}
        onRefresh={vi.fn()}
        isRefreshing={false}
      />,
    );

    const requestIdInput = screen.getByPlaceholderText("Request ID");
    fireEvent.change(requestIdInput, { target: { value: "42" } });

    fireEvent.click(screen.getByRole("button", { name: /clear filters/i }));
    expect(actions.clearFilters).toHaveBeenCalledTimes(1);

    const clearedActions = {
      ...actions,
      hasActiveFilters: false,
      state: {
        ...actions.state,
        ingress_request_id: "",
      },
    };

    view.rerender(
      <LocaleProvider>
        <FiltersBar
          actions={clearedActions}
          filterOptions={createFilterOptions()}
          filterOptionsLoaded={true}
          onRefresh={vi.fn()}
          isRefreshing={false}
        />
      </LocaleProvider>,
    );

    expect(screen.getByPlaceholderText("Request ID")).toHaveValue("");
  });

  it("renders localized retained request-log filter copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <FiltersBarPrimaryFilters
        filterOptions={createFilterOptions()}
        filterOptionsLoaded={true}
        state={createPrimaryState()}
        actions={createPrimaryActions()}
      />,
    );

    expect(screen.queryByText("搜索")).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText("模型、供应商、路径或错误")).not.toBeInTheDocument();
    expect(screen.getByText("请求 ID")).toBeInTheDocument();
    expect(screen.getByText("入口请求 ID")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("入口请求 ID")).toBeInTheDocument();
  });

  it("renders the selected model display name while preserving the model_id value", () => {
    const filterOptions = createFilterOptions({
      apiFamilies: ["openai"],
      models: [
        {
          active_connection_count: 1,
          connection_count: 1,
          created_at: "2026-01-01T00:00:00Z",
          display_name: "GPT-4o Mini",
          health_success_rate: null,
          health_total_requests: 0,
          id: 1,
          is_enabled: true,
          loadbalance_strategy: null,
          loadbalance_strategy_id: null,
          model_id: "gpt-4o-mini",
          model_type: "native",
          vendor: {
            id: 1,
            key: "openai",
            name: "OpenAI",
            description: null,
            icon_key: null,
            audit_enabled: false,
            audit_capture_bodies: false,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
          proxy_targets: [],
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    });

    renderWithLocale(
      <FiltersBarPrimaryFilters
        filterOptions={filterOptions}
        filterOptionsLoaded={true}
        state={createPrimaryState({ model_id: "gpt-4o-mini" })}
        actions={createPrimaryActions()}
      />,
    );

    expect(screen.getByText("GPT-4o Mini")).toBeInTheDocument();
    expect(screen.queryByText("gpt-4o-mini")).not.toBeInTheDocument();
  });

  it("keeps long endpoint labels shrink-safe in the endpoint trigger", () => {
    const longEndpointLabel =
      "CodexPool primary endpoint with an intentionally long descriptive label for request-log coverage";

    const filterOptions = createFilterOptions({
      apiFamilies: ["openai"],
      endpoints: [
        {
          id: 7,
          name: longEndpointLabel,
          base_url: "https://example.com/v1",
          has_api_key: true,
          masked_api_key: "sk-••••",
          position: 0,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    });

    renderWithLocale(
      <FiltersBarPrimaryFilters
        filterOptions={filterOptions}
        filterOptionsLoaded={true}
        state={createPrimaryState({ endpoint_id: "7", api_family: "openai" })}
        actions={createPrimaryActions()}
      />,
    );

    const endpointTrigger = screen.getByText(longEndpointLabel).closest("button");
    expect(endpointTrigger).toHaveClass("w-full");
    expect(endpointTrigger).toHaveClass("min-w-0");
    expect(endpointTrigger).toHaveClass("max-w-full");
  });
});
