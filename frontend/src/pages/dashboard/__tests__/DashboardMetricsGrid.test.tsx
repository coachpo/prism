import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { DashboardMetricsGrid } from "../DashboardMetricsGrid";

function renderWithLocale(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

describe("DashboardMetricsGrid", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("shows Average RPM as the primary throughput card", () => {
    const { container } = renderWithLocale(
      <DashboardMetricsGrid
        highlighted={false}
        snapshot={{
          activeModels: 2,
          averageRpm: 0,
          averageRpmRequestTotal: 0,
          avgLatency: 320,
          errorRate: 5,
          p95Latency: 880,
          streamShare: 0,
          successRate: 95,
          totalCost: 123456,
          totalModels: 3,
          totalRequests: 0,
        }}
      />
    );

    const metricCards = container.querySelectorAll('[data-slot="metric-card"]');
    expect(metricCards).toHaveLength(4);

    expect(screen.getByText("Average RPM")).toBeInTheDocument();
    expect(screen.getByText("0.000")).toBeInTheDocument();
    expect(screen.getByText("0 total requests")).toBeInTheDocument();
    expect(screen.queryByText("System Health")).not.toBeInTheDocument();
    expect(screen.queryByText("Critical")).not.toBeInTheDocument();

    const averageRpmCard = screen.getByText("Average RPM").closest('[data-slot="metric-card"]');
    expect(averageRpmCard).not.toBeNull();
    expect(averageRpmCard?.querySelector('[data-slot="metric-label"]')).toHaveTextContent("Average RPM");
    expect(averageRpmCard?.querySelector('[data-slot="metric-value"]')).toHaveTextContent("0.000");
    expect(averageRpmCard?.querySelector('[data-slot="metric-detail"]')).toHaveTextContent("0 total requests");
  });

  it("uses the throughput total for the RPM card detail", () => {
    renderWithLocale(
      <DashboardMetricsGrid
        highlighted={false}
        snapshot={{
          activeModels: 2,
          averageRpm: 1.25,
          averageRpmRequestTotal: 12,
          avgLatency: 320,
          errorRate: 5,
          p95Latency: 880,
          streamShare: 0,
          successRate: 95,
          totalCost: 123456,
          totalModels: 3,
          totalRequests: 24,
        }}
      />
    );

    const averageRpmCard = screen.getByText("Average RPM").closest('[data-slot="metric-card"]');

    expect(screen.getByText("12 total requests")).toBeInTheDocument();
    expect(screen.getByText("24")).toBeInTheDocument();
    expect(averageRpmCard).not.toBeNull();
    expect(averageRpmCard?.querySelector('[data-slot="metric-detail"]')).toHaveTextContent("12 total requests");
  });

  it("renders localized KPI labels when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <DashboardMetricsGrid
        highlighted={false}
        snapshot={{
          activeModels: 2,
          averageRpm: 1.25,
          averageRpmRequestTotal: 12,
          avgLatency: 320,
          errorRate: 5,
          p95Latency: 880,
          streamShare: 0,
          successRate: 95,
          totalCost: 123456,
          totalModels: 3,
          totalRequests: 24,
        }}
      />,
    );

    expect(screen.getByText("活跃模型")).toBeInTheDocument();
    expect(screen.getByText("24 小时请求数")).toBeInTheDocument();
    expect(screen.getByText("30 天支出")).toBeInTheDocument();
    expect(screen.getByText("平均 RPM")).toBeInTheDocument();
  });
});
