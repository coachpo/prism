import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { useModelDetailData } from "../useModelDetailData";
import { useModelDetailPageShell } from "../useModelDetailPageShell";

const { mockNavigate, mockOverviewCards, mockUseModelDetailData, mockSetIsEditModelDialogOpen } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockOverviewCards: vi.fn(),
  mockUseModelDetailData: vi.fn(),
  mockSetIsEditModelDialogOpen: vi.fn(),
}));

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useParams: () => ({ id: "5" }),
  };
});

vi.mock("../useModelDetailData", () => ({
  useModelDetailData: mockUseModelDetailData,
}));

vi.mock("../OverviewCards", () => ({
  OverviewCards: (props: unknown) => {
    mockOverviewCards(props);
    return <div>overview-cards</div>;
  },
}));

vi.mock("../ProxyTargetsCard", () => ({
  ProxyTargetsCard: ({ proxyTargets }: { proxyTargets: Array<{ target_model_id: string }> }) => (
    <div>{`proxy-targets:${proxyTargets.length}`}</div>
  ),
}));

vi.mock("../ConnectionDialog", () => ({
  ConnectionDialog: () => <div>connection-dialog</div>,
}));

vi.mock("../ModelSettingsDialog", () => ({
  ModelSettingsDialog: ({ isOpen }: { isOpen: boolean }) => (
    <div>{`model-settings-dialog:${isOpen ? "open" : "closed"}`}</div>
  ),
}));

vi.mock("../ConnectionsList", () => ({
  ConnectionsList: () => <div>mock-connections-list</div>,
}));

vi.mock("../LoadbalanceEventsTab", () => ({
  LoadbalanceEventsTab: () => <div>mock-loadbalance-events-tab</div>,
}));

vi.mock("../ModelDetailHeader", () => ({
  ModelDetailHeader: ({
    model,
    onEditModel,
  }: {
    model: { model_id: string };
    onEditModel: () => void;
  }) => (
    <div>
      <div>{`model-detail-header:${model.model_id}`}</div>
      <button type="button" onClick={onEditModel}>
        open-edit-model
      </button>
    </div>
  ),
}));

vi.mock("../ModelDetailTabs", () => ({
  ModelDetailTabs: ({
    activeTab,
    model,
    setActiveTab,
  }: {
    activeTab: "connections" | "events";
    model: { model_id: string };
    setActiveTab: (tab: "connections" | "events") => void;
  }) => (
    <div>
      <div>{`model-detail-tabs:${model.model_id}:${activeTab}`}</div>
      <button type="button" onClick={() => setActiveTab("events")}>
        switch-to-events
      </button>
    </div>
  ),
}));

type ModelDetailDataResult = ReturnType<typeof useModelDetailData>;

function buildModelDetailData(modelType: "native" | "proxy" = "native"): ModelDetailDataResult {
  const vendor = {
    id: 1,
    key: "openai",
    name: "OpenAI",
    description: null,
    icon_key: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "",
    updated_at: "",
  };

  const data: ModelDetailDataResult = {
    model: {
      id: 5,
      vendor_id: 1,
      vendor,
      api_family: "openai",
      model_id: "gpt-5.4",
      display_name: "GPT-5.4",
      model_type: modelType,
      is_enabled: true,
      proxy_targets: modelType === "proxy" ? [{ target_model_id: "gpt-4o", position: 0 }] : [],
      created_at: "2026-03-25T10:00:00Z",
      updated_at: "2026-03-25T10:00:00Z",
      connections: [],
      loadbalance_strategy_id: null,
      loadbalance_strategy: null,
    },
    loading: false,
    loadbalanceStrategies: [],
    vendors: [vendor],
    isEditModelDialogOpen: false,
    setIsEditModelDialogOpen: mockSetIsEditModelDialogOpen,
    editLoadbalanceStrategyId: "",
    setEditLoadbalanceStrategyId: vi.fn(),
    spending: null,
    spendingLoading: false,
    spendingCurrencySymbol: "$",
    spendingCurrencyCode: "USD",
    connections: [],
    isConnectionDialogOpen: false,
    setIsConnectionDialogOpen: vi.fn(),
    editingConnection: null,
    connectionSearch: "",
    setConnectionSearch: vi.fn(),
    healthCheckingIds: new Set<number>(),
    dialogTestingConnection: false,
    dialogTestResult: null,
    currentStateByConnectionId: new Map(),
    resettingConnectionIds: new Set<number>(),
    focusedConnectionId: null,
    connectionCardRefs: new Map<number, HTMLDivElement>(),
    globalEndpoints: [],
    createMode: "select",
    setCreateMode: vi.fn(),
    selectedEndpointId: "",
    setSelectedEndpointId: vi.fn(),
    newEndpointForm: { name: "", base_url: "", api_key: "" },
    setNewEndpointForm: vi.fn(),
    connectionForm: {
      name: "",
      is_active: true,
      custom_headers: null,
      pricing_template_id: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
    },
    setConnectionForm: vi.fn(),
    headerRows: [],
    setHeaderRows: vi.fn(),
    proxyTargetOptions: [{ modelId: "gpt-4o", label: "GPT-4o" }],
    proxyTargetSummary: {
      targetCount: modelType === "proxy" ? 1 : 0,
      firstTargetId: modelType === "proxy" ? "gpt-4o" : null,
      firstTargetLabel: modelType === "proxy" ? "GPT-4o" : null,
      routePolicyLabel: "Ordered priority routing",
    } as ModelDetailDataResult["proxyTargetSummary"],
    endpointSourceDefaultName: null,
    openConnectionDialog: vi.fn(),
    handleConnectionSubmit: vi.fn(),
    handleDeleteConnection: vi.fn(),
    handleHealthCheck: vi.fn(),
    handleDialogTestConnection: vi.fn(),
    handleToggleActive: vi.fn(),
    handleEditModelSubmit: vi.fn(),
    handleSaveProxyTargets: vi.fn(),
    pricingTemplates: [],
    reorderInFlight: false,
    handleReorderConnections: vi.fn(),
    handleResetCooldown: vi.fn(),
  };

  return data;
}

describe("useModelDetailPageShell", () => {
  it("starts on the connections tab and navigates to related routes", () => {
    const navigate = vi.fn();
    const { result } = renderHook(() => useModelDetailPageShell(navigate));

    expect(result.current.activeTab).toBe("connections");

    act(() => {
      result.current.setActiveTab("events");
      result.current.navigateBackToModels();
      result.current.navigateToRequestLogs("gpt-5.4");
    });

    expect(result.current.activeTab).toBe("events");
    expect(navigate).toHaveBeenNthCalledWith(1, "/models");
    expect(navigate).toHaveBeenNthCalledWith(2, "/request-logs?model_id=gpt-5.4");
  });
});

describe("ModelDetailPage shell", () => {
  beforeEach(() => {
    mockNavigate.mockReset();
    mockOverviewCards.mockReset();
    mockSetIsEditModelDialogOpen.mockReset();
    mockUseModelDetailData.mockReset();
    mockUseModelDetailData.mockReturnValue(buildModelDetailData("native"));
  });

  it("mounts the native detail shell with tabs and dialogs while leaving proxy-only UI out", async () => {
    const { ModelDetailPage } = await import("../../ModelDetailPage");

    render(<ModelDetailPage />);

    expect(mockUseModelDetailData).toHaveBeenCalledWith("5");
    const overviewProps = mockOverviewCards.mock.calls.at(-1)?.[0] as Record<string, unknown>;
    expect(overviewProps).toBeDefined();
    expect(overviewProps).not.toHaveProperty("metrics24hLoading");
    expect(overviewProps).not.toHaveProperty("modelKpis");
    expect(screen.getByText("model-detail-header:gpt-5.4")).toBeInTheDocument();
    expect(screen.getByText("model-detail-tabs:gpt-5.4:connections")).toBeInTheDocument();
    expect(screen.queryByText(/proxy-targets:/)).not.toBeInTheDocument();
    expect(screen.getByText("connection-dialog")).toBeInTheDocument();
    expect(screen.getByText("model-settings-dialog:closed")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "switch-to-events" }));

    expect(screen.getByText("model-detail-tabs:gpt-5.4:events")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "open-edit-model" }));

    expect(mockSetIsEditModelDialogOpen).toHaveBeenCalledWith(true);
  });
});
