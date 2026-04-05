import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Connection, Endpoint, ModelConfig, ModelConfigListItem, PricingTemplate } from "@/lib/types";
import { ConnectionDialog } from "../ConnectionDialog";
import { ConnectionCardHeader } from "../connections-list/ConnectionCardHeader";
import { createDefaultConnectionForm } from "../useModelDetailDataSupport";
import { useModelDetailConnectionFlows } from "../useModelDetailConnectionFlows";
import { useModelDetailConnectionMutations } from "../useModelDetailConnectionMutations";
import { useModelDetailDialogState } from "../useModelDetailDialogState";

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal("ResizeObserver", ResizeObserverMock);
window.HTMLElement.prototype.scrollIntoView = vi.fn();

const api = vi.hoisted(() => ({
  connections: {
    create: vi.fn(),
    healthCheck: vi.fn(),
    healthCheckPreview: vi.fn(),
    update: vi.fn(),
  },
}));

const toast = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
}));

const clearSharedReferenceData = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", () => ({ api }));
vi.mock("sonner", () => ({ toast }));
vi.mock("@/lib/referenceData", () => ({ clearSharedReferenceData }));

function buildEndpoint(overrides: Partial<Endpoint> = {}): Endpoint {
  return {
    id: 7,
    name: "Primary endpoint",
    base_url: "https://api.example.com/v1",
    has_api_key: true,
    masked_api_key: "sk-••••",
    position: 0,
    created_at: "2026-03-25T10:00:00Z",
    updated_at: "2026-03-25T10:00:00Z",
    ...overrides,
  };
}

function buildConnection(overrides: Partial<Connection> = {}): Connection {
  return {
    id: 11,
    model_config_id: 5,
    endpoint_id: 7,
    endpoint: buildEndpoint(),
    is_active: true,
    priority: 0,
    name: "Primary",
    auth_type: null,
    custom_headers: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    health_status: "healthy",
    health_detail: null,
    last_health_check: "2026-03-25T10:00:00Z",
    created_at: "2026-03-25T10:00:00Z",
    updated_at: "2026-03-25T10:00:00Z",
    ...overrides,
  };
}

function buildPricingTemplate(overrides: Partial<PricingTemplate> = {}): PricingTemplate {
  return {
    id: 3,
    profile_id: 1,
    name: "Premium Pricing",
    description: "Tracks spend",
    pricing_unit: "PER_1M",
    pricing_currency_code: "USD",
    input_price: "1.25",
    output_price: "5.00",
    cached_input_price: null,
    cache_creation_price: null,
    reasoning_price: null,
    missing_special_token_price_policy: "MAP_TO_OUTPUT",
    version: 2,
    created_at: "2026-03-25T10:00:00Z",
    updated_at: "2026-03-25T10:00:00Z",
    ...overrides,
  };
}

function ConnectionDialogHarness({
  editingConnection,
  pricingTemplates = [],
  pricingTemplateIdOnOpen,
}: {
  editingConnection?: Connection;
  pricingTemplates?: PricingTemplate[];
  pricingTemplateIdOnOpen?: number | null;
}) {
  const [connections, setConnections] = useState<Connection[]>(editingConnection ? [editingConnection] : []);
  const [globalEndpoints, setGlobalEndpoints] = useState<Endpoint[]>([buildEndpoint()]);
  const [, setAllModels] = useState<ModelConfigListItem[]>([]);
  const [model, setModel] = useState<ModelConfig | null>({
    id: 5,
    vendor_id: 1,
    vendor: {
      id: 1,
      key: "openai",
      name: "OpenAI",
      description: null,
      icon_key: null,
      audit_enabled: false,
      audit_capture_bodies: false,
      created_at: "2026-03-25T10:00:00Z",
      updated_at: "2026-03-25T10:00:00Z",
    },
    api_family: "openai",
    model_id: "gpt-5.4",
    display_name: "GPT-5.4",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connections: editingConnection ? [editingConnection] : [],
    created_at: "2026-03-25T10:00:00Z",
    updated_at: "2026-03-25T10:00:00Z",
  });

  const {
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection: activeEditingConnection,
    dialogTestingConnection,
    setDialogTestingConnection,
    dialogTestResult,
    setDialogTestResult,
    createMode,
    setCreateMode,
    selectedEndpointId,
    setSelectedEndpointId,
    newEndpointForm,
    setNewEndpointForm,
    connectionForm,
    setConnectionForm,
    headerRows,
    setHeaderRows,
    endpointSourceDefaultName,
    openConnectionDialog,
  } = useModelDetailDialogState({ globalEndpoints });

  const { handleConnectionSubmit } = useModelDetailConnectionMutations({
    id: "5",
    revision: 1,
    createMode,
    selectedEndpointId,
    newEndpointForm,
    connectionForm,
    headerRows,
    editingConnection: activeEditingConnection,
    pricingTemplates,
    endpointSourceDefaultName,
    refreshCurrentState: vi.fn(),
    setIsConnectionDialogOpen,
    setAllModels,
    setConnections,
    setGlobalEndpoints,
    setModel,
  });

  const { handleDialogTestConnection } = useModelDetailConnectionFlows({
    connections,
    setConnections,
    model,
    modelConfigId: 5,
    setModel,
    createMode,
    selectedEndpointId,
    newEndpointForm,
    connectionForm,
    headerRows,
    editingConnection: activeEditingConnection,
    endpointSourceDefaultName,
    refreshCurrentState: vi.fn(),
    setDialogTestingConnection,
    setDialogTestResult,
  });

  const committedConnection = connections[0] ?? null;
  const committedConnectionName = committedConnection?.name ?? committedConnection?.endpoint?.name ?? "";

  return (
    <>
      <button
        type="button"
        onClick={() => {
          openConnectionDialog(editingConnection);
          if (pricingTemplateIdOnOpen !== undefined) {
            setConnectionForm((current) => ({
              ...current,
              pricing_template_id: pricingTemplateIdOnOpen,
            }));
          }
          if (!editingConnection) {
            setCreateMode("new");
          }
        }}
      >
        Open Connection Dialog
      </button>
      {committedConnection ? (
        <ConnectionCardHeader
          connection={committedConnection}
          connectionName={committedConnectionName}
          isChecking={false}
        />
      ) : null}
      <ConnectionDialog
        isOpen={isConnectionDialogOpen}
        onOpenChange={setIsConnectionDialogOpen}
        editingConnection={activeEditingConnection}
        connectionForm={connectionForm}
        setConnectionForm={setConnectionForm}
        newEndpointForm={newEndpointForm}
        setNewEndpointForm={setNewEndpointForm}
        createMode={createMode}
        setCreateMode={setCreateMode}
        selectedEndpointId={selectedEndpointId}
        setSelectedEndpointId={setSelectedEndpointId}
        globalEndpoints={globalEndpoints}
        headerRows={headerRows}
        setHeaderRows={setHeaderRows}
        handleConnectionSubmit={handleConnectionSubmit}
        dialogTestingConnection={dialogTestingConnection}
        dialogTestResult={dialogTestResult}
        handleDialogTestConnection={handleDialogTestConnection}
        endpointSourceDefaultName={endpointSourceDefaultName}
        pricingTemplates={pricingTemplates}
      />
    </>
  );
}

function renderWithLocale(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

function getHeaderKeyInputs() {
  return screen.queryAllByLabelText("Header Key");
}

describe("ConnectionDialog limiter fields", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    api.connections.create.mockResolvedValue(buildConnection({ id: 22 }));
    api.connections.healthCheck.mockResolvedValue({
      connection_id: 11,
      health_status: "healthy",
      checked_at: "2026-03-25T10:00:00Z",
      detail: "ok",
      response_time_ms: 120,
    });
    api.connections.healthCheckPreview.mockResolvedValue({
      health_status: "healthy",
      checked_at: "2026-03-25T10:00:00Z",
      detail: "preview ok",
      response_time_ms: 120,
    });
    api.connections.update.mockResolvedValue(buildConnection());
  });

  it("defaults new limiter fields to unlimited null values", () => {
    expect(createDefaultConnectionForm()).toEqual({
      name: "",
      is_active: true,
      custom_headers: null,
      pricing_template_id: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
    });
  });

  it("renders localized add-dialog copy in English without monitoring-only fields", async () => {
    renderWithLocale(<ConnectionDialogHarness />);

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    expect(await screen.findByText("Add Connection")).toBeInTheDocument();
    expect(screen.getByText("Endpoint Source")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Select Existing" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Create New" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Probe interval (seconds)")).not.toBeInTheDocument();
    expect(screen.queryByText("OpenAI probe endpoint")).not.toBeInTheDocument();
    expect(screen.getByLabelText("QPS Limit")).toBeInTheDocument();
    expect(screen.getAllByText("Leave blank for unlimited.")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "Save Connection" })).toBeInTheDocument();
  });

  it("keeps dialog select triggers shrink-safe for long selected labels", async () => {
      renderWithLocale(
        <ConnectionDialogHarness
          pricingTemplates={[
            buildPricingTemplate({
              name: "Premium Pricing Template With A Very Long Label For Overflow Coverage",
            }),
          ]}
          pricingTemplateIdOnOpen={3}
        />,
      );

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    expect(await screen.findByText("Add Connection")).toBeInTheDocument();

    screen.getAllByRole("combobox").forEach((combobox) => {
      expect(combobox).toHaveClass("w-full", "min-w-0", "max-w-full");
    });
  });

  it("renders localized edit-dialog copy in Chinese while keeping Test Connection", async () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(<ConnectionDialogHarness editingConnection={buildConnection()} />);

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    expect(await screen.findByText("编辑连接")).toBeInTheDocument();
    expect(screen.getByText("端点来源")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "选择现有端点" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "新建端点" })).toBeInTheDocument();
    expect(screen.queryByLabelText("探测间隔（秒）")).not.toBeInTheDocument();
    expect(screen.getByLabelText("QPS 限流")).toBeInTheDocument();
    expect(screen.getByLabelText("最大并发（非流式）")).toBeInTheDocument();
    expect(screen.getByLabelText("最大并发（流式）")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "测试连接" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存连接" })).toBeInTheDocument();
  });

  it("keeps the approved connection dialog layout anchors while removing monitoring-only controls", async () => {
    renderWithLocale(<ConnectionDialogHarness />);

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    expect(await screen.findByText("Add Connection")).toBeInTheDocument();

    const dialogContent = document.querySelector('[data-slot="dialog-content"]');
    const scrollArea = document.querySelector('[data-slot="scroll-area"]');
    const scrollBody = screen.getByTestId("connection-dialog-scroll-body");
    const endpointSourceSection = screen.getByTestId("connection-dialog-endpoint-source-section");

    expect(dialogContent).toHaveClass("max-w-5xl", "overflow-hidden");
    expect(dialogContent).toHaveClass("h-[min(94vh,64rem)]");
    expect(scrollArea).toHaveClass("min-h-0", "flex-1");
    expect(scrollBody).toHaveClass("grid", "gap-6", "px-6", "py-5", "sm:px-7");
    expect(endpointSourceSection).toHaveClass("space-y-4", "bg-muted/20", "p-4", "sm:p-5");

    const leftColumn = screen.getByTestId("connection-dialog-left-column");
    expect(within(leftColumn).getByLabelText("Name (Optional)")).toBeInTheDocument();
    expect(within(leftColumn).getByText("Active")).toBeInTheDocument();
    expect(within(leftColumn).getByLabelText("Pricing Template")).toBeInTheDocument();
    expect(within(leftColumn).queryByLabelText("Probe interval (seconds)")).not.toBeInTheDocument();
    expect(within(leftColumn).queryByText("OpenAI probe endpoint")).not.toBeInTheDocument();
  });

  it("adds stable names and autocomplete metadata for connection form fields", async () => {
    const pricingTemplate = buildPricingTemplate({ id: 9 });
    renderWithLocale(
      <ConnectionDialogHarness pricingTemplates={[pricingTemplate]} pricingTemplateIdOnOpen={pricingTemplate.id} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    expect(await screen.findByText("Add Connection")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveAttribute("name", "endpoint_name");
    expect(screen.getByLabelText("Base URL")).toHaveAttribute("name", "endpoint_base_url");
    expect(screen.getByLabelText("Base URL")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("API Key")).toHaveAttribute("name", "endpoint_api_key");
    expect(screen.getByLabelText("API Key")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("Name (Optional)")).toHaveAttribute("name", "name");
    expect(screen.getByLabelText("QPS Limit")).toHaveAttribute("name", "qps_limit");
    expect(screen.getByLabelText("Max In-Flight (Non-Stream)")).toHaveAttribute("name", "max_in_flight_non_stream");
    expect(screen.getByLabelText("Max In-Flight (Stream)")).toHaveAttribute("name", "max_in_flight_stream");
    expect(document.querySelector('input[type="hidden"][name="create_mode"]')).toHaveValue("new");
    expect(document.querySelector('input[type="hidden"][name="pricing_template_id"]')).toHaveValue("9");
    expect(document.querySelector('input[type="hidden"][name="openai_probe_endpoint_variant"]')).toBeNull();
  });

  it("renders each newly added custom-header row with stable keys", async () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    try {
      renderWithLocale(<ConnectionDialogHarness />);

      fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

      expect(await screen.findByRole("button", { name: "Add Header" })).toBeInTheDocument();
      expect(getHeaderKeyInputs()).toHaveLength(0);

      fireEvent.click(screen.getByRole("button", { name: "Add Header" }));
      await waitFor(() => expect(getHeaderKeyInputs()).toHaveLength(1));

      fireEvent.click(screen.getByRole("button", { name: "Add Header" }));
      await waitFor(() => expect(getHeaderKeyInputs()).toHaveLength(2));

      expect(
        consoleErrorSpy.mock.calls.some((call) => call.join(" ").includes("Encountered two children with the same key")),
      ).toBe(false);
    } finally {
      consoleErrorSpy.mockRestore();
    }
  });

  it("includes limiter fields in the create submit payload", async () => {
    renderWithLocale(<ConnectionDialogHarness />);

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    fireEvent.change(screen.getByPlaceholderText("e.g. OpenAI Primary"), {
      target: { value: "Inline endpoint" },
    });
    fireEvent.change(screen.getByPlaceholderText("https://api.openai.com"), {
      target: { value: "https://api.example.com/v1" },
    });
    fireEvent.change(screen.getByPlaceholderText("sk-..."), {
      target: { value: "sk-test" },
    });
    fireEvent.change(screen.getByLabelText("QPS Limit"), { target: { value: "9" } });
    fireEvent.change(screen.getByLabelText("Max In-Flight (Non-Stream)"), { target: { value: "5" } });
    fireEvent.change(screen.getByLabelText("Max In-Flight (Stream)"), { target: { value: "2" } });

    fireEvent.click(screen.getByRole("button", { name: "Save Connection" }));

    await waitFor(() => {
      expect(api.connections.create).toHaveBeenCalledWith(
        5,
        expect.objectContaining({
          qps_limit: 9,
          max_in_flight_non_stream: 5,
          max_in_flight_stream: 2,
        }),
      );
    });
  });

  it("uses the unsaved add-dialog draft when running Test Connection", async () => {
    renderWithLocale(<ConnectionDialogHarness />);

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    fireEvent.change(screen.getByPlaceholderText("e.g. OpenAI Primary"), {
      target: { value: "Preview endpoint" },
    });
    fireEvent.change(screen.getByPlaceholderText("https://api.openai.com"), {
      target: { value: "https://preview.example.com/v1" },
    });
    fireEvent.change(screen.getByPlaceholderText("sk-..."), {
      target: { value: "sk-preview" },
    });
    fireEvent.change(screen.getByLabelText("Name (Optional)"), {
      target: { value: "Preview connection" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Test Connection" }));

    await waitFor(() => {
      expect(api.connections.healthCheckPreview).toHaveBeenCalledWith(
        5,
        expect.objectContaining({
          endpoint_create: expect.objectContaining({
            name: "Preview endpoint",
            base_url: "https://preview.example.com/v1",
            api_key: "sk-preview",
          }),
          name: "Preview connection",
        }),
      );
    });
    expect(api.connections.healthCheck).not.toHaveBeenCalled();
    expect(await screen.findByText("Connection Healthy")).toBeInTheDocument();
    expect(screen.getByText("preview ok")).toBeInTheDocument();
  });

  it("submits null limiter values after clearing existing inputs", async () => {
    renderWithLocale(
      <ConnectionDialogHarness
        editingConnection={buildConnection({
          qps_limit: 20,
          max_in_flight_non_stream: 7,
          max_in_flight_stream: 3,
        })}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    fireEvent.change(screen.getByLabelText("QPS Limit"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Max In-Flight (Non-Stream)"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Max In-Flight (Stream)"), { target: { value: "" } });

    fireEvent.click(screen.getByRole("button", { name: "Save Connection" }));

    await waitFor(() => {
      expect(api.connections.update).toHaveBeenCalledWith(
        11,
        expect.objectContaining({
          qps_limit: null,
          max_in_flight_non_stream: null,
          max_in_flight_stream: null,
        }),
      );
    });
  });

  it("hydrates the committed connection pricing template after edit when update omits the nested relation", async () => {
    const pricingTemplate = buildPricingTemplate({
      id: 9,
      name: "EUR Template",
      pricing_currency_code: "EUR",
      version: 4,
    });

    api.connections.update.mockResolvedValue(
      buildConnection({
        pricing_template_id: pricingTemplate.id,
        pricing_template: null,
      }),
    );

    renderWithLocale(
      <ConnectionDialogHarness
        editingConnection={buildConnection()}
        pricingTemplates={[pricingTemplate]}
        pricingTemplateIdOnOpen={pricingTemplate.id}
      />,
    );

    expect(screen.getByText("Pricing Off")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Open Connection Dialog" }));

    expect(await screen.findByText("Edit Connection")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Save Connection" }));

    await waitFor(() => {
      expect(api.connections.update).toHaveBeenCalledWith(
        11,
        expect.objectContaining({ pricing_template_id: pricingTemplate.id }),
      );
    });

    await waitFor(() => {
      expect(screen.getByText("Pricing On")).toBeInTheDocument();
      expect(screen.getByText("EUR Template v4")).toBeInTheDocument();
      expect(screen.getByText("EUR")).toBeInTheDocument();
    });
  });
});
