import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { RoutingDiagramCard } from "./RoutingDiagramCard";
import type { RoutingDiagramData } from "./routingDiagram";

describe("RoutingDiagramCard", () => {
  it("opens the retained inspector from plain-list node clicks", async () => {
    const user = userEvent.setup();
    const onSelectModel = vi.fn();

    renderRoutingDiagramCard({ onSelectModel });

    expect(screen.queryByTestId("routing-diagram-inspector")).not.toBeInTheDocument();

    await user.click(screen.getByTestId("routing-diagram-list-node-model-101"));

    expect(onSelectModel).not.toHaveBeenCalled();
    const inspector = screen.getByTestId("routing-diagram-inspector");
    expect(inspector).toHaveTextContent("Model A");
    expect(inspector).toHaveTextContent("节点类型");
    expect(inspector).toHaveTextContent("24 小时成功率");
  });

  it("keeps explicit model action buttons wired to navigation", async () => {
    const user = userEvent.setup();
    const onSelectModel = vi.fn();

    renderRoutingDiagramCard({ onSelectModel });

    await user.click(
      within(screen.getByTestId("routing-diagram-list-node-model-101")).getByRole("button", {
        name: "查看模型 Model A 的详情",
      }),
    );

    expect(onSelectModel).toHaveBeenCalledWith(101);
    expect(screen.queryByTestId("routing-diagram-inspector")).not.toBeInTheDocument();
  });
});

function renderRoutingDiagramCard({
  onSelectModel = vi.fn(),
  onDrillDownRequests = vi.fn(),
}: {
  onSelectModel?: (modelConfigId: number) => void;
  onDrillDownRequests?: (params: { endpoint_id?: number; model_id?: string }) => void;
} = {}) {
  return render(
    <LocaleProvider>
      <RoutingDiagramCard
        data={createTopologyGraph()}
        loading={false}
        error={null}
        onSelectModel={onSelectModel}
        onDrillDownRequests={onDrillDownRequests}
      />
    </LocaleProvider>,
  );
}

function createTopologyGraph(): RoutingDiagramData {
  return {
    nodes: [
      {
        id: "model-101",
        kind: "model",
        label: "Model A",
        sublabel: "model-a",
        status: "enabled",
        model_config_id: 101,
        model_id: "model-a",
      },
      {
        id: "terminal-target-501",
        kind: "connection",
        product_kind: "terminal_target",
        label: "Primary Target",
        sublabel: "Endpoint A",
        status: "active",
        terminal_target_id: 501,
        connection_id: 501,
        active: true,
        recent_request_count: 42,
        recent_success_rate: 97.6,
        last_request_at: "2026-04-10T12:34:56Z",
      },
      {
        id: "endpoint-201",
        kind: "endpoint",
        label: "Endpoint A",
        sublabel: "Endpoint 201",
        status: "configured",
        endpoint_id: 201,
      },
    ],
    edges: [
      {
        id: "access-target-1001",
        kind: "model_to_connection",
        product_kind: "model_to_terminal_target",
        source_node_id: "model-101",
        target_node_id: "terminal-target-501",
        position: 1,
        enabled: true,
      },
      {
        id: "terminal-target-binding-501",
        kind: "connection_to_endpoint",
        product_kind: "terminal_target_to_endpoint",
        source_node_id: "terminal-target-501",
        target_node_id: "endpoint-201",
      },
    ],
    stats: {
      model_count: 1,
      active_model_count: 1,
      disabled_model_count: 0,
      terminal_target_count: 1,
      active_terminal_target_count: 1,
      inactive_terminal_target_count: 0,
      endpoint_count: 1,
      edge_count: 2,
    },
  };
}
