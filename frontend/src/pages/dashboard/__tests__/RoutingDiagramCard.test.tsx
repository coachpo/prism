import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { RoutingDiagramCard } from "../RoutingDiagramCard";
import { RoutingDiagramShell } from "../RoutingDiagramShell";
import { RoutingDiagramChart } from "../routing-diagram/RoutingDiagramChart";
import { RoutingDiagramLinkShape } from "../routing-diagram/RoutingDiagramLinkShape";
import { RoutingDiagramTooltip } from "../routing-diagram/RoutingDiagramTooltip";

vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  Sankey: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  Tooltip: ({ content }: { content: React.ReactNode }) => <div>{content}</div>,
}));

function renderWithLocale(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

describe("routing diagram shell", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubGlobal(
      "ResizeObserver",
      class {
        disconnect() {}
        observe() {}
        unobserve() {}
      },
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders the loading state copy", () => {
    renderWithLocale(
      <RoutingDiagramShell
        chartContent={null}
        error={null}
        headerContent={null}
        loading={true}
      />,
    );

    expect(screen.getByText("Routing Health Map")).toBeInTheDocument();
    expect(screen.getByText(/Loading live routing volume and 24-hour health data/i)).toBeInTheDocument();
  });

  it("renders the empty state content when there is no chart", () => {
    renderWithLocale(
      <RoutingDiagramShell
        chartContent={null}
        emptyState={{
          description: "No routing diagram data is available for this profile.",
          title: "No routing data",
        }}
        error={null}
        headerContent={<div>summary</div>}
        loading={false}
      />,
    );

    expect(screen.getByText("summary")).toBeInTheDocument();
    expect(screen.getByText("No routing data")).toBeInTheDocument();
  });

  it("renders localized routing empty state when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <RoutingDiagramCard
        data={{
          nodes: [],
          links: [],
          endpointCount: 0,
          modelCount: 0,
          activeConnectionTotal: 0,
          trafficRequestTotal24h: 0,
        }}
        loading={false}
        error={null}
        onSelectModel={() => undefined}
      />,
    );

    expect(screen.getByText("路由健康图")).toBeInTheDocument();
    expect(screen.getByText("暂无活动路由")).toBeInTheDocument();
  });

  it("renders localized routing chart shell and legend copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <RoutingDiagramChart
        chartData={{ links: [], nodes: [] }}
        chartHeight={240}
        isCompact={false}
        onActivateNode={() => undefined}
      />,
    );

    expect(
      screen.getByText("连线宽度表示活动连接数量，颜色表示过去 24 小时的路由成功率。"),
    ).toBeInTheDocument();
    expect(screen.getByText("点击模型节点可打开详情")).toBeInTheDocument();
    expect(screen.getByText("健康")).toBeInTheDocument();
    expect(screen.getByText("降级")).toBeInTheDocument();
    expect(screen.getByText("失败")).toBeInTheDocument();
    expect(screen.getByText("暂无最近请求")).toBeInTheDocument();
  });

  it("renders localized tooltip health copy when route traffic is absent", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <RoutingDiagramTooltip
        active={true}
        payload={[
          {
            payload: {
              payload: {
                id: "route-1",
                sourceNodeId: "endpoint-1",
                targetNodeId: "model-1",
                modelId: "gpt-4o",
                modelLabel: "GPT 4o",
                modelConfigId: 1,
                endpointId: 1,
                endpointLabel: "demo-endpoint",
                activeConnectionCount: 1,
                trafficRequestCount24h: 0,
                requestCount24h: 0,
                successCount24h: 0,
                errorCount24h: 0,
                successRate24h: null,
              },
            },
          },
        ]}
      />,
    );

    expect(screen.getByText("24 小时健康度")).toBeInTheDocument();
    expect(screen.getAllByText("暂无数据").length).toBeGreaterThan(0);
  });

  it("renders localized tooltip success-rate fallback when node traffic is absent", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <RoutingDiagramTooltip
        active={true}
        payload={[
          {
            payload: {
              payload: {
                id: "node-1",
                kind: "model",
                label: "GPT 4o",
                sublabel: null,
                activeConnectionCount: 1,
                trafficRequestCount24h: 0,
                requestCount24h: 0,
                successCount24h: 0,
                errorCount24h: 0,
                successRate24h: null,
              },
            },
          },
        ]}
      />,
    );

    expect(screen.getByText("24 小时成功率")).toBeInTheDocument();
    expect(screen.getAllByText("暂无数据").length).toBeGreaterThan(0);
  });

  it("renders a localized routing link aria-label when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <svg aria-label="routing link fixture">
        <RoutingDiagramLinkShape
          props={{
            linkWidth: 2,
            payload: {
              id: "route-1",
              source: 0,
              target: 1,
              value: 1,
              sourceNodeId: "endpoint-1",
              targetNodeId: "model-1",
              modelId: "gpt-4o",
              modelLabel: "GPT 4o",
              modelConfigId: 1,
              endpointId: 1,
              endpointLabel: "demo-endpoint",
              activeConnectionCount: 1,
              trafficRequestCount24h: 0,
              requestCount24h: 0,
              successCount24h: 0,
              errorCount24h: 0,
              successRate24h: null,
            },
            sourceControlX: 20,
            sourceX: 10,
            sourceY: 10,
            targetControlX: 80,
            targetX: 90,
            targetY: 10,
          }}
        />
      </svg>,
    );

    expect(screen.getByLabelText("从 demo-endpoint 到 GPT 4o 的路由")).toBeInTheDocument();
  });
});
