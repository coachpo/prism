import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { DashboardHighlightsGrid } from "../DashboardHighlightsGrid";

function renderWithLocale(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

describe("DashboardHighlightsGrid", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("renders the performance snapshot tile labels, values, and highlight class inline", () => {
    const { container } = renderWithLocale(
      <DashboardHighlightsGrid
        highlighted={true}
        onInspectSpending={vi.fn()}
        onOpenStatistics={vi.fn()}
        onReviewRequests={vi.fn()}
        apiFamilyRows={[]}
        strategyFamilySummary={{ adaptiveCount: 1, legacyCount: 2, unassignedCount: 0 }}
        snapshot={{
          activeModels: 2,
          averageRpm: 0,
          averageRpmRequestTotal: 0,
          avgLatency: 321,
          errorRate: 5.4,
          p95Latency: 881,
          streamShare: 12.3,
          successRate: 95,
          totalCost: 123456,
          totalModels: 3,
          totalRequests: 2161,
        }}
      />
    );

    const compactTiles = container.querySelectorAll('[data-slot="compact-metric-tile"]');
    expect(compactTiles).toHaveLength(4);

    const avgLatencyLabel = screen.getByText("Avg Latency");
    const p95LatencyLabel = screen.getByText("P95 Latency");
    const errorRateLabel = screen.getByText("Error Rate");
    const streamShareLabel = screen.getByText("Streaming Share");

    expect(screen.getByText("321ms")).toBeInTheDocument();
    expect(screen.getByText("881ms")).toBeInTheDocument();
    expect(screen.getByText("5.4%")).toBeInTheDocument();
    expect(screen.getByText("12.3%")).toBeInTheDocument();

    const avgLatencyTile = avgLatencyLabel.closest('[data-slot="compact-metric-tile"]');
    const p95LatencyTile = p95LatencyLabel.closest('[data-slot="compact-metric-tile"]');
    const errorRateTile = errorRateLabel.closest('[data-slot="compact-metric-tile"]');
    const streamShareTile = streamShareLabel.closest('[data-slot="compact-metric-tile"]');

    expect(avgLatencyTile).toHaveClass("ws-value-updated");
    expect(p95LatencyTile).toHaveClass("ws-value-updated");
    expect(errorRateTile).toHaveClass("ws-value-updated");
    expect(streamShareTile).toHaveClass("ws-value-updated");
    expect(avgLatencyTile?.querySelector('[data-slot="metric-label"]')).toHaveTextContent("Avg Latency");
    expect(avgLatencyTile?.querySelector('[data-slot="metric-value"]')).toHaveTextContent("321ms");
    expect(p95LatencyTile?.querySelector('[data-slot="metric-label"]')).toHaveTextContent("P95 Latency");
    expect(p95LatencyTile?.querySelector('[data-slot="metric-value"]')).toHaveTextContent("881ms");
  });

  it("uses shared api-family labels and icons for the API family mix card", () => {
    renderWithLocale(
      <DashboardHighlightsGrid
        highlighted={false}
        onInspectSpending={vi.fn()}
        onOpenStatistics={vi.fn()}
        onReviewRequests={vi.fn()}
        apiFamilyRows={[
          {
            key: "openai",
            total_requests: 2161,
            success_count: 2160,
            error_count: 1,
            avg_response_time_ms: 320,
            total_tokens: 1000,
          },
          {
            key: "anthropic",
            total_requests: 126,
            success_count: 126,
            error_count: 0,
            avg_response_time_ms: 410,
            total_tokens: 800,
          },
          {
            key: "gemini",
            total_requests: 8,
            success_count: 8,
            error_count: 0,
            avg_response_time_ms: 290,
            total_tokens: 200,
          },
        ]}
        strategyFamilySummary={{ adaptiveCount: 1, legacyCount: 2, unassignedCount: 0 }}
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
          totalRequests: 2161,
        }}
      />
    );

    const openAiLabel = screen.getByText("OpenAI");
    const anthropicLabel = screen.getByText("Anthropic");
    const geminiLabel = screen.getByText("Gemini");

    expect(openAiLabel).toBeInTheDocument();
    expect(anthropicLabel).toBeInTheDocument();
    expect(geminiLabel).toBeInTheDocument();
    expect(screen.queryByText("Openai")).not.toBeInTheDocument();
    expect(openAiLabel.previousElementSibling?.tagName).toBe("svg");
    expect(anthropicLabel.previousElementSibling?.tagName).toBe("svg");
    expect(geminiLabel.previousElementSibling?.tagName).toBe("svg");
    expect(screen.getByText("Routing strategy mix")).toBeInTheDocument();
    expect(screen.getByText("Legacy strategy 2")).toBeInTheDocument();
    expect(screen.getByText("Adaptive strategy 1")).toBeInTheDocument();
  });

  it("renders localized dashboard highlight copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <DashboardHighlightsGrid
        highlighted={false}
        onInspectSpending={vi.fn()}
        onOpenStatistics={vi.fn()}
        onReviewRequests={vi.fn()}
        apiFamilyRows={[]}
        strategyFamilySummary={{ adaptiveCount: 1, legacyCount: 2, unassignedCount: 0 }}
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
          totalRequests: 2161,
        }}
      />,
    );

    expect(screen.getByText("性能概览")).toBeInTheDocument();
    expect(screen.getByText("打开统计")).toBeInTheDocument();
    expect(screen.getByText("暂无 API 家族活动")).toBeInTheDocument();
    expect(screen.getByText("路由策略分布")).toBeInTheDocument();
    expect(screen.getByText("传统策略 2")).toBeInTheDocument();
    expect(screen.getByText("自适应策略 1")).toBeInTheDocument();
  });
});
