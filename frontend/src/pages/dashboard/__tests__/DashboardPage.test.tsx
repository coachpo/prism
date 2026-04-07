import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { DashboardPage } from "@/pages/DashboardPage";

const { mockUseDashboardPageData } = vi.hoisted(() => ({
  mockUseDashboardPageData: vi.fn(() => ({
    apiFamilyRows: [],
    clearRecentRequestHighlight: vi.fn(),
    connectionState: "connected",
    isRefreshing: false,
    isSyncing: false,
    loading: false,
    metricSnapshot: {
      activeModels: 1,
      averageRpm: 1,
      averageRpmRequestTotal: 1,
      avgLatency: 100,
      errorRate: 0,
      p95Latency: 120,
      streamShare: 0,
      successRate: 100,
      totalCost: 0,
      totalModels: 1,
      totalRequests: 1,
    },
    metricsHighlighted: false,
    modelDisplayNames: new Map<string, string>(),
    recentNewIds: new Set<number>(),
    recentRequests: [],
    refreshDashboard: vi.fn(),
    routingDiagramData: null,
    routingDiagramError: null,
    routingDiagramLoading: false,
    strategyFamilySummary: {
      adaptiveCount: 0,
      legacyCount: 0,
      unassignedCount: 0,
    },
    topSpendingModels: [],
  })),
}));

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({
    revision: 1,
    selectedProfile: { id: 1 },
  }),
}));

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (value: string) => value,
  }),
}));

vi.mock("@/components/WebSocketStatusIndicator", () => ({
  WebSocketStatusIndicator: () => <div data-testid="dashboard-websocket-status" />,
}));

vi.mock("@/pages/dashboard/useDashboardPageData", () => ({
  useDashboardPageData: mockUseDashboardPageData,
}));

vi.mock("@/pages/dashboard/DashboardOverviewTab", () => ({
  DashboardOverviewTab: ({ onInspectSpending }: { onInspectSpending: () => void }) => (
    <div>
      <div data-testid="dashboard-overview-tab">overview-content</div>
      <button type="button" onClick={onInspectSpending}>
        inspect-spending
      </button>
    </div>
  ),
}));

vi.mock("@/pages/dashboard/DashboardAnalyticsContent", () => ({
  DashboardAnalyticsContent: () => <div data-testid="dashboard-analytics-tab">analytics-content</div>,
}));

function LocationProbe({ onChange }: { onChange: (value: string) => void }) {
  const location = useLocation();

  useEffect(() => {
    onChange(`${location.pathname}${location.search}`);
  }, [location, onChange]);

  return null;
}

function renderDashboard(initialEntry: string, onLocationChange: (value: string) => void) {
  return render(
    <LocaleProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <LocationProbe onChange={onLocationChange} />
        <Routes>
          <Route path="/dashboard" element={<DashboardPage />} />
        </Routes>
      </MemoryRouter>
    </LocaleProvider>,
  );
}

describe("DashboardPage", () => {
  beforeEach(() => {
    mockUseDashboardPageData.mockClear();
  });

  it("defaults to the overview tab on /dashboard", async () => {
    let currentLocation = "";

    renderDashboard("/dashboard", (value) => {
      currentLocation = value;
    });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=overview");
    });

    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("dashboard-overview-tab")).toBeInTheDocument();
    expect(screen.queryByTestId("dashboard-analytics-tab")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Refresh dashboard" })).toBeInTheDocument();
    expect(mockUseDashboardPageData).toHaveBeenCalled();
  });

  it("keeps analytics deep links selected after direct navigation", async () => {
    let currentLocation = "";

    renderDashboard("/dashboard?tab=analytics", (value) => {
      currentLocation = value;
    });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=analytics");
    });

    expect(screen.getByRole("tab", { name: "Analytics" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("dashboard-analytics-tab")).toBeInTheDocument();
    expect(screen.queryByTestId("dashboard-overview-tab")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Refresh dashboard" })).not.toBeInTheDocument();
    expect(mockUseDashboardPageData).not.toHaveBeenCalled();
  });

  it("updates the address bar when switching dashboard tabs", async () => {
    let currentLocation = "";

    renderDashboard("/dashboard", (value) => {
      currentLocation = value;
    });

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Analytics" }), { button: 0 });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=analytics");
    });

    expect(screen.getByTestId("dashboard-analytics-tab")).toBeInTheDocument();

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Overview" }), { button: 0 });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=overview");
    });

    expect(screen.getByTestId("dashboard-overview-tab")).toBeInTheDocument();
  });

  it("routes inspect spending to the supported dashboard analytics state", async () => {
    let currentLocation = "";

    renderDashboard("/dashboard", (value) => {
      currentLocation = value;
    });

    fireEvent.click(screen.getByRole("button", { name: "inspect-spending" }));

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=analytics");
    });

    expect(screen.getByTestId("dashboard-analytics-tab")).toBeInTheDocument();
  });
});
