import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { UsageSnapshotResponse } from "@/lib/types";
import { StatisticsPage } from "@/pages/StatisticsPage";
import { installLocalStorageMock } from "./storage";

const api = vi.hoisted(() => ({
  settings: {
    timezone: {
      get: vi.fn(),
    },
  },
}));

vi.mock("@/lib/api", () => ({ api }));

const mockUsageState = {
  setChartGranularity: vi.fn(),
  setSelectedModelLines: vi.fn(),
  setSelectedTimeRange: vi.fn(),
  state: {
    chartGranularity: {
      costOverview: "hourly",
      requestTrends: "hourly",
      tokenTypeBreakdown: "hourly",
      tokenUsageTrends: "hourly",
    },
    selectedModelLines: [],
    selectedTimeRange: "1h",
  },
  toggleSelectedModelLine: vi.fn(),
};

let mockUsageData = {
  availableModelLineIds: [],
  costOverviewSeries: [],
  error: null as string | null,
  loading: false,
  refresh: vi.fn(),
  requestTrendSeries: [],
  selectedModelLineIds: [],
  snapshot: null as UsageSnapshotResponse | null,
  tokenTypeBreakdown: [],
  tokenUsageTrendSeries: [],
};

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({
    revision: 1,
    selectedProfile: { id: 1 },
  }),
}));

vi.mock("@/pages/statistics/useUsageStatisticsPageState", () => ({
  useUsageStatisticsPageState: () => mockUsageState,
}));

vi.mock("@/pages/statistics/useUsageStatisticsPageData", () => ({
  useUsageStatisticsPageData: () => mockUsageData,
}));

function createEmptySnapshot(): UsageSnapshotResponse {
  return {
    cost_overview: {
      daily: [],
      hourly: [],
      priced_request_count: 0,
      total_cost_micros: 0,
      unpriced_request_count: 3,
    },
    currency: { code: "USD", symbol: "$" },
    endpoint_statistics: [],
    generated_at: "2026-03-27T12:00:00Z",
    model_statistics: [],
    overview: {
      average_rpm: 0,
      average_tpm: 0,
      cached_tokens: 0,
      failed_requests: 0,
      input_tokens: 0,
      output_tokens: 0,
      reasoning_tokens: 0,
      success_rate: 0,
      success_requests: 0,
      total_cost_micros: 0,
      total_requests: 0,
      total_tokens: 0,
    },
    proxy_api_key_statistics: [],
    request_trends: { daily: [], hourly: [] },
    service_health: {
      availability_percentage: null,
      cells: [],
      failed_count: 0,
      interval_minutes: 15,
      request_count: 0,
      success_count: 0,
    },
    time_range: {
      end_at: "2026-03-27T12:00:00Z",
      preset: "1h",
      start_at: "2026-03-27T11:00:00Z",
    },
    token_type_breakdown: { daily: [], hourly: [] },
    token_usage_trends: { daily: [], hourly: [] },
  };
}

describe("StatisticsPage empty states", () => {
  beforeEach(() => {
    installLocalStorageMock();
    localStorage.clear();
    api.settings.timezone.get.mockResolvedValue({ timezone_preference: "UTC" });
    mockUsageData = {
      availableModelLineIds: [],
      costOverviewSeries: [],
      error: null,
      loading: false,
      refresh: vi.fn(),
      requestTrendSeries: [],
      selectedModelLineIds: [],
      snapshot: createEmptySnapshot(),
      tokenTypeBreakdown: [],
      tokenUsageTrendSeries: [],
    };
  });

  it("shows pricing guidance and the remaining empty-state tables without a scope note card", () => {
    render(
      <MemoryRouter>
        <LocaleProvider>
          <StatisticsPage />
        </LocaleProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText("Pricing data is missing for this time range")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Attach pricing templates to connections to unlock cost coverage on the statistics page.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Pricing Templates" })).toHaveAttribute(
      "href",
      "/pricing-templates",
    );
    expect(screen.getByText("Time Range")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Last 1 Hour" })).toBeInTheDocument();
    expect(screen.getByText("No proxy API key usage in this time range")).toBeInTheDocument();
    expect(screen.queryByTestId("statistics-no-request-events")).not.toBeInTheDocument();
    expect(screen.queryByText("No request events in this time range")).not.toBeInTheDocument();
  });
});
