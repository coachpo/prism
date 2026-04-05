import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ModelDetailPage } from "../../ModelDetailPage";
import { ProxyModelDetailPage } from "../../ProxyModelDetailPage";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { useModelDetailData } from "../useModelDetailData";
import { useModelDetailPageShell } from "../useModelDetailPageShell";

const originalLocalStorage = window.localStorage;
const mockOverviewCards = vi.fn();

function createLocalStorageMock(): Storage {
  let storage: Record<string, string> = {};

  return {
    clear: () => {
      storage = {};
    },
    getItem: (key) => storage[key] ?? null,
    key: (index) => Object.keys(storage)[index] ?? null,
    get length() {
      return Object.keys(storage).length;
    },
    removeItem: (key) => {
      delete storage[key];
    },
    setItem: (key, value) => {
      storage[key] = value;
    },
  };
}

vi.mock("../useModelDetailData", () => ({
  useModelDetailData: vi.fn(),
}));

vi.mock("../useModelDetailPageShell", () => ({
  useModelDetailPageShell: vi.fn(),
}));

vi.mock("../OverviewCards", () => ({
  OverviewCards: (props: unknown) => {
    mockOverviewCards(props);
    return <div>overview-cards</div>;
  },
}));

vi.mock("../ModelDetailHeader", () => ({
  ModelDetailHeader: ({ model }: { model: { model_id: string } }) => <div>{`model-detail-header:${model.model_id}`}</div>,
}));

vi.mock("../ConnectionsList", () => ({
  ConnectionsList: () => <div>connections-list</div>,
}));

vi.mock("../ConnectionDialog", () => ({
  ConnectionDialog: () => null,
}));

vi.mock("../ModelSettingsDialog", () => ({
  ModelSettingsDialog: () => null,
}));

vi.mock("../LoadbalanceEventsTab", () => ({
  LoadbalanceEventsTab: () => <div>events-tab</div>,
}));

vi.mock("../ProxyTargetsCard", () => ({
  ProxyTargetsCard: ({ proxyTargets }: { proxyTargets: Array<{ target_model_id: string }> }) => (
    <div>{`proxy-targets-card:${proxyTargets.map((target) => target.target_model_id).join(",")}`}</div>
  ),
}));

const useModelDetailDataMock = vi.mocked(useModelDetailData);
const useModelDetailPageShellMock = vi.mocked(useModelDetailPageShell);
type ModelDetailDataResult = ReturnType<typeof useModelDetailData>;

function buildHookValue(modelType: "native" | "proxy"): ModelDetailDataResult {
  const vendor = {
    id: 7,
    key: "anthropic",
    name: "Anthropic",
    description: null,
    icon_key: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
  };

  const data: ModelDetailDataResult = {
    model: {
      id: 9,
      vendor_id: 7,
      vendor,
      api_family: "anthropic",
      model_id: "claude-proxy",
      display_name: "Claude Proxy",
      model_type: modelType,
      proxy_targets: modelType === "proxy"
        ? [{ target_model_id: "claude-sonnet-4-5-20250929", position: 0 }]
        : [],
      loadbalance_strategy_id: null,
      loadbalance_strategy: null,
      is_enabled: true,
      connections: [],
      created_at: "2026-03-20T10:00:00Z",
      updated_at: "2026-03-20T10:00:00Z",
    },
    loading: false,
    loadbalanceStrategies: [],
    vendors: [vendor],
    isEditModelDialogOpen: false,
    setIsEditModelDialogOpen: vi.fn(),
    editLoadbalanceStrategyId: "",
    setEditLoadbalanceStrategyId: vi.fn(),
    proxyTargetOptions: [],
    handleSaveProxyTargets: vi.fn(),
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
    connectionCardRefs: new Map(),
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
    proxyTargetSummary: {
      targetCount: modelType === "proxy" ? 1 : 0,
      firstTargetId: modelType === "proxy" ? "claude-sonnet-4-5-20250929" : null,
      firstTargetLabel: modelType === "proxy" ? "Claude Sonnet 4.5 (20250929)" : null,
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
    pricingTemplates: [],
    reorderInFlight: false,
    handleReorderConnections: vi.fn(),
    handleResetCooldown: vi.fn(),
  };

  return data;
}

function renderPage() {
  return render(
    <LocaleProvider>
      <MemoryRouter initialEntries={["/models/9"]}>
        <Routes>
          <Route path="/models/:id" element={<ModelDetailPage />} />
          <Route path="/models/:id/proxy" element={<div>proxy-detail-route</div>} />
        </Routes>
      </MemoryRouter>
    </LocaleProvider>,
  );
}

function renderProxyPage() {
  return render(
    <LocaleProvider>
      <MemoryRouter initialEntries={["/models/9/proxy"]}>
        <Routes>
          <Route path="/models/:id/proxy" element={<ProxyModelDetailPage />} />
        </Routes>
      </MemoryRouter>
    </LocaleProvider>,
  );
}

describe("ModelDetailPage proxy target wiring", () => {
  beforeEach(() => {
    const localStorageMock = createLocalStorageMock();

    mockOverviewCards.mockReset();
    vi.stubGlobal("localStorage", localStorageMock);
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: localStorageMock,
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: originalLocalStorage,
    });
  });

  it("redirects proxy models from the generic detail route to the dedicated proxy route", () => {
    useModelDetailPageShellMock.mockReturnValue({
      activeTab: "connections",
      navigateBackToModels: vi.fn(),
      navigateToRequestLogs: vi.fn(),
      setActiveTab: vi.fn(),
    });
    useModelDetailDataMock.mockReturnValue(buildHookValue("proxy"));

    renderPage();

    expect(useModelDetailDataMock).toHaveBeenCalledWith("9");
    expect(screen.getByText("proxy-detail-route")).toBeInTheDocument();
  });

  it("renders header overview and proxy targets without native-only tabs or connection UI", () => {
    useModelDetailPageShellMock.mockReturnValue({
      activeTab: "connections",
      navigateBackToModels: vi.fn(),
      navigateToRequestLogs: vi.fn(),
      setActiveTab: vi.fn(),
    });
    useModelDetailDataMock.mockReturnValue(buildHookValue("proxy"));

    renderProxyPage();

    expect(useModelDetailDataMock).toHaveBeenCalledWith("9");
    const overviewProps = mockOverviewCards.mock.calls.at(-1)?.[0] as Record<string, unknown>;
    expect(overviewProps).toBeDefined();
    expect(overviewProps).not.toHaveProperty("metrics24hLoading");
    expect(overviewProps).not.toHaveProperty("modelKpis");
    expect(screen.getByText("model-detail-header:claude-proxy")).toBeInTheDocument();
    expect(screen.getByText("overview-cards")).toBeInTheDocument();
    expect(screen.getByText("proxy-targets-card:claude-sonnet-4-5-20250929")).toBeInTheDocument();
    expect(screen.queryByText("connections-list")).not.toBeInTheDocument();
    expect(screen.queryByText("events-tab")).not.toBeInTheDocument();
  });
});
