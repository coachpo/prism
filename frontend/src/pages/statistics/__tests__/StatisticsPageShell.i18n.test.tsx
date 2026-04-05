import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type {
  UsageCostOverviewPoint,
  UsageRequestTrendSeries,
  UsageSnapshotResponse,
  UsageTokenTrendSeries,
  UsageTokenTypeBreakdownPoint,
} from "@/lib/types";
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

vi.mock("recharts", async () => {
  const actual = await vi.importActual<typeof import("recharts")>("recharts");
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  };
});

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
    selectedModelLines: ["gpt-5.4"],
    selectedTimeRange: "1h",
  },
  toggleSelectedModelLine: vi.fn(),
};

let mockUsageData: {
  availableModelLineIds: string[];
  costOverviewSeries: UsageCostOverviewPoint[];
  error: string | null;
  loading: boolean;
  refresh: ReturnType<typeof vi.fn>;
  requestTrendSeries: UsageRequestTrendSeries[];
  selectedModelLineIds: string[];
  snapshot: UsageSnapshotResponse | null;
  tokenTypeBreakdown: UsageTokenTypeBreakdownPoint[];
  tokenUsageTrendSeries: UsageTokenTrendSeries[];
} = {
  availableModelLineIds: ["gpt-5.4", "claude-sonnet-4-6"],
  costOverviewSeries: [],
  error: null,
  loading: true,
  refresh: vi.fn(),
  requestTrendSeries: [],
  selectedModelLineIds: ["gpt-5.4"],
  snapshot: null,
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

class ResizeObserverMock {
  disconnect() {}
  observe() {}
  unobserve() {}
}

function createSnapshot(): UsageSnapshotResponse {
  return {
    cost_overview: {
      daily: [{ bucket_start: "2026-03-27T00:00:00Z", total_cost_micros: 4200 }],
      hourly: [{ bucket_start: "2026-03-27T11:00:00Z", total_cost_micros: 4200 }],
      priced_request_count: 1,
      total_cost_micros: 4200,
      unpriced_request_count: 0,
    },
    currency: { code: "USD", symbol: "$" },
    endpoint_statistics: [
      {
        endpoint_id: 10,
        endpoint_label: "Primary Endpoint",
        request_count: 4,
        success_rate: 100,
        total_cost_micros: 4200,
        total_tokens: 185,
      },
    ],
    generated_at: "2026-03-27T12:00:00Z",
    model_statistics: [
      {
        model_id: "gpt-5.4",
        model_label: "GPT-5.4",
        request_count: 4,
        success_rate: 100,
        total_cost_micros: 4200,
        total_tokens: 185,
      },
    ],
    overview: {
      average_rpm: 0.016,
      average_tpm: 2.6,
      cached_tokens: 25,
      failed_requests: 0,
      input_tokens: 140,
      output_tokens: 70,
      reasoning_tokens: 10,
      rolling_request_count: 4,
      rolling_rpm: 4,
      rolling_token_count: 245,
      rolling_tpm: 245,
      rolling_window_minutes: 60,
      success_rate: 100,
      success_requests: 4,
      total_cost_micros: 4200,
      total_requests: 4,
      total_tokens: 245,
    },
    proxy_api_key_statistics: [
      {
        proxy_api_key_id: 77,
        proxy_api_key_label: "Primary runtime key",
        request_count: 4,
        success_rate: 100,
        total_cost_micros: 4200,
        total_tokens: 245,
      },
    ],
    request_trends: {
      daily: [
        {
          key: "all",
          label: "All requests",
          points: [
            {
              bucket_start: "2026-03-27T00:00:00Z",
              failed_count: 0,
              request_count: 4,
              rpm: 0.0028,
              success_count: 4,
            },
          ],
          total_requests: 4,
        },
        {
          key: "gpt-5.4",
          label: "GPT-5.4",
          points: [
            {
              bucket_start: "2026-03-27T00:00:00Z",
              failed_count: 0,
              request_count: 4,
              rpm: 0.0028,
              success_count: 4,
            },
          ],
          total_requests: 4,
        },
      ],
      hourly: [
        {
          key: "all",
          label: "All requests",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              failed_count: 0,
              request_count: 4,
              rpm: 4,
              success_count: 4,
            },
          ],
          total_requests: 4,
        },
        {
          key: "gpt-5.4",
          label: "GPT-5.4",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              failed_count: 0,
              request_count: 4,
              rpm: 4,
              success_count: 4,
            },
          ],
          total_requests: 4,
        },
      ],
    },
    service_health: {
      availability_percentage: 100,
      cells: [
        {
          availability_percentage: 100,
          bucket_start: "2026-03-27T00:00:00Z",
          failed_count: 0,
          request_count: 2,
          status: "ok",
          success_count: 2,
        },
      ],
      failed_count: 0,
      interval_minutes: 15,
      request_count: 4,
      success_count: 4,
    },
    time_range: {
      end_at: "2026-03-27T12:00:00Z",
      preset: "1h",
      start_at: "2026-03-27T11:00:00Z",
    },
    token_type_breakdown: {
      daily: [
        {
          bucket_start: "2026-03-27T00:00:00Z",
          cached_tokens: 25,
          input_tokens: 140,
          output_tokens: 70,
          reasoning_tokens: 10,
        },
      ],
      hourly: [
        {
          bucket_start: "2026-03-27T11:00:00Z",
          cached_tokens: 25,
          input_tokens: 140,
          output_tokens: 70,
          reasoning_tokens: 10,
        },
      ],
    },
    token_usage_trends: {
      daily: [
        {
          key: "all",
          label: "All requests",
          points: [
            {
              bucket_start: "2026-03-27T00:00:00Z",
              cached_tokens: 25,
              input_tokens: 140,
              output_tokens: 70,
              reasoning_tokens: 10,
              total_tokens: 245,
              tpm: 0.17,
            },
          ],
          total_tokens: 245,
        },
        {
          key: "gpt-5.4",
          label: "GPT-5.4",
          points: [
            {
              bucket_start: "2026-03-27T00:00:00Z",
              cached_tokens: 25,
              input_tokens: 140,
              output_tokens: 70,
              reasoning_tokens: 10,
              total_tokens: 245,
              tpm: 0.17,
            },
          ],
          total_tokens: 245,
        },
      ],
      hourly: [
        {
          key: "all",
          label: "All requests",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              cached_tokens: 25,
              input_tokens: 140,
              output_tokens: 70,
              reasoning_tokens: 10,
              total_tokens: 245,
              tpm: 245,
            },
          ],
          total_tokens: 245,
        },
        {
          key: "gpt-5.4",
          label: "GPT-5.4",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              cached_tokens: 25,
              input_tokens: 140,
              output_tokens: 70,
              reasoning_tokens: 10,
              total_tokens: 245,
              tpm: 245,
            },
          ],
          total_tokens: 245,
        },
      ],
    },
  };
}

function renderPage() {
  return render(
    <MemoryRouter>
      <LocaleProvider>
        <StatisticsPage />
      </LocaleProvider>
    </MemoryRouter>,
  );
}

describe("StatisticsPage shell i18n", () => {
  beforeEach(() => {
    installLocalStorageMock();
    localStorage.clear();
    api.settings.timezone.get.mockResolvedValue({ timezone_preference: "UTC" });
    localStorage.setItem("prism.locale", "zh-CN");
    Object.defineProperty(globalThis, "ResizeObserver", {
      configurable: true,
      value: ResizeObserverMock,
      writable: true,
    });
    mockUsageData = {
      availableModelLineIds: ["gpt-5.4", "claude-sonnet-4-6"],
      costOverviewSeries: [],
      error: null,
      loading: true,
      refresh: vi.fn(),
      requestTrendSeries: [],
      selectedModelLineIds: ["gpt-5.4"],
      snapshot: null,
      tokenTypeBreakdown: [],
      tokenUsageTrendSeries: [],
    };
  });

  it("renders the localized statistics shell without the removed aggregate-rollup note", () => {
    const { rerender } = renderPage();

    expect(screen.getByRole("heading", { name: "用量统计" })).toBeInTheDocument();
    expect(
      screen.getByText(
        "基于最终请求的一体化用量快照，覆盖请求、令牌、成本、端点、模型和代理 API 密钥。",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "刷新用量统计" })).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "用量统计页面占位中" })).toBeInTheDocument();

    mockUsageData = {
      ...mockUsageData,
      costOverviewSeries: createSnapshot().cost_overview.hourly,
      loading: false,
      requestTrendSeries: createSnapshot().request_trends.hourly,
      selectedModelLineIds: ["gpt-5.4"],
      snapshot: createSnapshot(),
      tokenTypeBreakdown: createSnapshot().token_type_breakdown.hourly,
      tokenUsageTrendSeries: createSnapshot().token_usage_trends.hourly,
    };

    rerender(
      <MemoryRouter>
        <LocaleProvider>
          <StatisticsPage />
        </LocaleProvider>
      </MemoryRouter>,
    );

    expect(screen.getByTestId("usage-controls-toolbar")).toBeInTheDocument();
    expect(screen.getByText("时间范围")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "最近 1 小时" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "最近 6 小时" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "最近 24 小时" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "最近 7 天" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "最近 30 天" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "全部时间" })).toBeInTheDocument();
    expect(screen.getByTestId("usage-kpi-grid")).toBeInTheDocument();
    expect(screen.getByTestId("statistics-endpoint-table")).toBeInTheDocument();
    expect(screen.getByTestId("statistics-model-table")).toBeInTheDocument();
    expect(screen.getByTestId("statistics-proxy-key-table")).toBeInTheDocument();
    expect(screen.queryByTestId("statistics-no-request-events")).not.toBeInTheDocument();
    expect(screen.queryByText("请求事件")).not.toBeInTheDocument();
  });

  it("renders the shell error banner when usage data returns an error", () => {
    mockUsageData = {
      ...mockUsageData,
      error: "Unable to load usage snapshot",
      loading: false,
    };

    renderPage();

    expect(screen.getByText("Unable to load usage snapshot")).toBeInTheDocument();
  });
});
