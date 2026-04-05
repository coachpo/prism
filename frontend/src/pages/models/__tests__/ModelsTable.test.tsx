import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ModelConfigListItem, Vendor } from "@/lib/types";
import { ModelsTable } from "../ModelsTable";

const originalLocalStorage = window.localStorage;

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

function buildVendor(overrides: Partial<Vendor> = {}): Vendor {
  return {
    id: 7,
    key: "openai",
    name: "OpenAI",
    description: null,
    icon_key: "openai",
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
    ...overrides,
  };
}

function buildModel(overrides: Partial<ModelConfigListItem> = {}): ModelConfigListItem {
  return {
    id: 11,
    vendor_id: 7,
    vendor: buildVendor(),
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o Mini",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: 100,
    loadbalance_strategy: {
      id: 100,
      name: "single-primary",
      strategy_type: "legacy",
      legacy_strategy_type: "single",
      auto_recovery: {
        mode: "disabled",
      },
    },
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 99,
    health_total_requests: 100,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
    ...overrides,
  };
}

function buildAdaptiveModel(overrides: Partial<ModelConfigListItem> = {}): ModelConfigListItem {
  return buildModel({
    id: 21,
    model_id: "adaptive-model",
    display_name: "Adaptive Model",
    loadbalance_strategy_id: 101,
    loadbalance_strategy: {
      id: 101,
      name: "adaptive-availability",
      strategy_type: "adaptive",
      routing_policy: {
        kind: "adaptive",
        routing_objective: "minimize_latency",
        deadline_budget_ms: 1500,
        hedge: {
          enabled: false,
          delay_ms: 75,
          max_additional_attempts: 1,
        },
        circuit_breaker: {
          failure_status_codes: [429, 503, 504],
          base_open_seconds: 60,
          failure_threshold: 2,
          backoff_multiplier: 2,
          max_open_seconds: 900,
          jitter_ratio: 0.2,
          ban_mode: "off",
          max_open_strikes_before_ban: 0,
          ban_duration_seconds: 0,
        },
        admission: {
          respect_qps_limit: true,
          respect_in_flight_limits: true,
        },
      },
    },
    ...overrides,
  });
}

function renderTable({
  filtered = [buildModel()],
  handleOpenDialog = vi.fn(),
  search = "",
  setDeleteTarget = vi.fn(),
}: {
  filtered?: ModelConfigListItem[];
  handleOpenDialog?: (model?: ModelConfigListItem) => void;
  search?: string;
  setDeleteTarget?: (model: ModelConfigListItem) => void;
} = {}) {
  return render(
    <LocaleProvider>
      <MemoryRouter initialEntries={["/models"]}>
        <Routes>
          <Route
            path="/models"
            element={
              <ModelsTable
                filtered={filtered}
                handleOpenDialog={handleOpenDialog}
                metricsLoading={false}
                modelMetrics24h={{}}
                modelSpend30dMicros={{}}
                search={search}
                setDeleteTarget={setDeleteTarget}
              />
            }
          />
          <Route path="/models/:id" element={<div>Native model detail route</div>} />
          <Route path="/models/:id/proxy" element={<div>Proxy model detail route</div>} />
        </Routes>
      </MemoryRouter>
    </LocaleProvider>
  );
}

describe("ModelsTable", () => {
  beforeEach(() => {
    const localStorageMock = createLocalStorageMock();

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

  it("copies the model ID from the explicit copy button without navigating", async () => {
    const writeTextMock = vi.fn<Clipboard["writeText"]>().mockResolvedValue(undefined);
    const originalClipboard = navigator.clipboard;
    const originalExecCommand = document.execCommand;

    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: writeTextMock },
    });

    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: vi.fn(() => true),
    });

    try {
      renderTable();

      fireEvent.click(screen.getByRole("button", { name: "Copy model ID gpt-4o-mini" }));

      await waitFor(() => {
        expect(writeTextMock).toHaveBeenCalledWith("gpt-4o-mini");
      });

      expect(screen.queryByText("Native model detail route")).not.toBeInTheDocument();
      expect(screen.queryByText("Proxy model detail route")).not.toBeInTheDocument();
    } finally {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: originalClipboard,
      });

      Object.defineProperty(document, "execCommand", {
        configurable: true,
        value: originalExecCommand,
      });
    }
  });

  it("navigates when the primary model title link is clicked", () => {
    renderTable();

    fireEvent.click(screen.getByRole("link", { name: "GPT-4o Mini" }));

    expect(screen.getByText("Native model detail route")).toBeInTheDocument();
  });

  it("navigates when the detail button is clicked", () => {
    renderTable();

    fireEvent.click(screen.getByRole("button", { name: "View model details for GPT-4o Mini" }));

    expect(screen.getByText("Native model detail route")).toBeInTheDocument();
  });

  it("routes proxy model title links to the dedicated proxy detail route", () => {
    renderTable({
      filtered: [
        buildModel({
          id: 12,
          model_id: "claude-proxy",
          display_name: "Claude Proxy",
          model_type: "proxy",
          api_family: "anthropic",
          vendor_id: 20,
          vendor: buildVendor({ id: 20, key: "anthropic", name: "Anthropic" }),
          proxy_targets: [{ target_model_id: "claude-sonnet-4-5-20250929", position: 0 }],
          loadbalance_strategy_id: null,
          loadbalance_strategy: null,
        }),
      ],
    });

    fireEvent.click(screen.getByRole("link", { name: "Claude Proxy" }));

    expect(screen.getByText("Proxy model detail route")).toBeInTheDocument();
  });

  it("routes proxy model detail buttons to the dedicated proxy detail route", () => {
    renderTable({
      filtered: [
        buildModel({
          id: 12,
          model_id: "claude-proxy",
          display_name: "Claude Proxy",
          model_type: "proxy",
          api_family: "anthropic",
          vendor_id: 20,
          vendor: buildVendor({ id: 20, key: "anthropic", name: "Anthropic" }),
          proxy_targets: [{ target_model_id: "claude-sonnet-4-5-20250929", position: 0 }],
          loadbalance_strategy_id: null,
          loadbalance_strategy: null,
        }),
      ],
    });

    fireEvent.click(screen.getByRole("button", { name: "View model details for Claude Proxy" }));

    expect(screen.getByText("Proxy model detail route")).toBeInTheDocument();
  });

  it("opens delete for the row without navigating to the detail route", () => {
    const model = buildModel();
    const setDeleteTarget = vi.fn();

    renderTable({ filtered: [model], setDeleteTarget });

    fireEvent.click(
      screen.getByRole("button", {
        name: 'Are you sure you want to delete "GPT-4o Mini"? This will also delete all associated endpoints.',
      }),
    );

    expect(screen.queryByText("Native model detail route")).not.toBeInTheDocument();
    expect(screen.queryByText("Proxy model detail route")).not.toBeInTheDocument();
    expect(screen.getByText("GPT-4o Mini")).toBeInTheDocument();
    expect(setDeleteTarget).toHaveBeenCalledWith(model);
  });

  it("does not copy the model ID when the detail button is clicked", async () => {
    const writeTextMock = vi.fn<Clipboard["writeText"]>().mockResolvedValue(undefined);
    const originalClipboard = navigator.clipboard;
    const originalExecCommand = document.execCommand;

    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: writeTextMock },
    });

    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: vi.fn(() => true),
    });

    try {
      renderTable();

      fireEvent.click(screen.getByRole("button", { name: "View model details for GPT-4o Mini" }));

      expect(screen.getByText("Native model detail route")).toBeInTheDocument();
      expect(writeTextMock).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: originalClipboard,
      });

      Object.defineProperty(document, "execCommand", {
        configurable: true,
        value: originalExecCommand,
      });
    }
  });

  it("groups models by vendor while keeping api family visible on each row", () => {
    renderTable({
      filtered: [
        buildModel({
          vendor_id: 30,
          vendor: buildVendor({ id: 30, key: "zai", name: "Z.ai", icon_key: "zhipu" }),
          api_family: "openai",
        }),
        buildModel({
          id: 12,
          vendor_id: 30,
          vendor: buildVendor({ id: 30, key: "zai", name: "Z.ai", icon_key: "zhipu" }),
          model_id: "glm-4.6",
          display_name: "GLM 4.6",
          api_family: "anthropic",
        }),
        buildModel({
          id: 13,
          model_id: "gpt-4.1-mini",
          display_name: "GPT-4.1 Mini",
          vendor_id: 7,
          vendor: buildVendor({ id: 7, key: "openai", name: "OpenAI", icon_key: "openai" }),
          api_family: "openai",
        }),
      ],
    });

    expect(screen.getByRole("button", { name: /z.ai/i })).toHaveTextContent("2 models");
    expect(screen.getByRole("button", { name: /^OpenAI 1 model$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /z.ai/i }).querySelector("svg")).not.toBeNull();
    expect(screen.getByRole("button", { name: /z.ai/i }).querySelector("img")).toBeNull();
    expect(screen.getByRole("button", { name: /^OpenAI 1 model$/i }).querySelector("svg")).not.toBeNull();
    expect(screen.getByRole("button", { name: /^OpenAI 1 model$/i }).querySelector("img")).toBeNull();
    expect(screen.getByText("GPT-4o Mini")).toBeInTheDocument();
    expect(screen.getByText("GLM 4.6")).toBeInTheDocument();
    expect(screen.getAllByText("OpenAI").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Anthropic").length).toBeGreaterThan(0);
  });

  it("shows kind-aware strategy summaries for legacy and adaptive assignments", () => {
    renderTable({
      filtered: [buildModel(), buildAdaptiveModel()],
    });

    expect(screen.getByText("single-primary · Single")).toBeInTheDocument();
    expect(screen.getByText("adaptive-availability · Adaptive strategy • Minimize latency")).toBeInTheDocument();
  });

  it("sorts vendor groups predictably with Unknown vendor last", () => {
    renderTable({
      filtered: [
        buildModel({
          id: 12,
          vendor_id: 30,
          vendor: buildVendor({ id: 30, key: "zai", name: "Z.ai", icon_key: "zhipu" }),
          display_name: "GLM 4.6",
        }),
        buildModel({
          id: 13,
          vendor_id: 0,
          vendor: undefined,
          display_name: "Mystery Model",
          model_id: "mystery-model",
        }),
        buildModel({
          id: 14,
          vendor_id: 7,
          vendor: buildVendor({ id: 7, key: "openai", name: "OpenAI", icon_key: "openai" }),
          display_name: "GPT-4.1 Mini",
          model_id: "gpt-4.1-mini",
        }),
      ],
    });

    const openAiGroup = screen.getByRole("button", { name: /^OpenAI 1 model$/i });
    const zaiGroup = screen.getByRole("button", { name: /^Z.ai 1 model$/i });
    const unknownGroup = screen.getByRole("button", { name: /^Unknown vendor 1 model$/i });

    expect(openAiGroup.compareDocumentPosition(zaiGroup) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(zaiGroup.compareDocumentPosition(unknownGroup) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(unknownGroup.querySelector("img")).toBeNull();
  });

  it("collapses and re-expands a vendor group without affecting other groups", () => {
    renderTable({
      filtered: [
        buildModel(),
        buildModel({
          id: 12,
          model_id: "gpt-4.1-mini",
          display_name: "GPT-4.1 Mini",
        }),
        buildModel({
          id: 13,
          model_id: "glm-4.6",
          display_name: "GLM 4.6",
          vendor_id: 30,
          vendor: buildVendor({ id: 30, key: "zai", name: "Z.ai", icon_key: "zhipu" }),
        }),
      ],
    });

    const openAiToggle = screen.getByRole("button", { name: /^OpenAI 2 models$/i });

    fireEvent.click(openAiToggle);

    expect(screen.queryByText("GPT-4o Mini")).not.toBeInTheDocument();
    expect(screen.queryByText("GPT-4.1 Mini")).not.toBeInTheDocument();
    expect(screen.getByText("GLM 4.6")).toBeInTheDocument();

    fireEvent.click(openAiToggle);

    expect(screen.getByText("GPT-4o Mini")).toBeInTheDocument();
    expect(screen.getByText("GPT-4.1 Mini")).toBeInTheDocument();
  });

  it("keeps matching vendor groups visible while search is active even after the group was collapsed", () => {
    const allModels = [
      buildModel(),
      buildModel({
        id: 12,
        model_id: "gpt-4.1-mini",
        display_name: "GPT-4.1 Mini",
      }),
      buildModel({
        id: 13,
        model_id: "glm-4.6",
        display_name: "GLM 4.6",
        vendor_id: 30,
        vendor: buildVendor({ id: 30, key: "zai", name: "Z.ai", icon_key: "zhipu" }),
      }),
    ];
    const { rerender } = renderTable({ filtered: allModels });

    fireEvent.click(screen.getByRole("button", { name: /^OpenAI 2 models$/i }));
    expect(screen.queryByText("GPT-4o Mini")).not.toBeInTheDocument();

    rerender(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/models"]}>
          <Routes>
            <Route
              path="/models"
              element={
                <ModelsTable
                  filtered={allModels.filter((model) => model.vendor?.key === "openai")}
                  handleOpenDialog={vi.fn()}
                  metricsLoading={false}
                  modelMetrics24h={{}}
                  modelSpend30dMicros={{}}
                  search="gpt"
                  setDeleteTarget={vi.fn()}
                />
              }
            />
            <Route path="/models/:id" element={<div>Native model detail route</div>} />
            <Route path="/models/:id/proxy" element={<div>Proxy model detail route</div>} />
          </Routes>
        </MemoryRouter>
      </LocaleProvider>
    );

    expect(screen.getByText("GPT-4o Mini")).toBeInTheDocument();
    expect(screen.getByText("GPT-4.1 Mini")).toBeInTheDocument();
  });

  it("renders the compact metrics inline with metadata separators", () => {
    renderTable();

    const metricsCluster = screen.getByText("1/1 active").parentElement;

    expect(metricsCluster).not.toBeNull();
    expect(metricsCluster).toContainElement(screen.getByText("single-primary · Single"));
    expect(screen.getAllByText("|")).toHaveLength(5);
  });

  it("renders proxy target summaries without singular target wording", () => {
    renderTable({
      filtered: [
        buildModel({
          id: 13,
          model_id: "claude-sonnet-4-6",
          display_name: "Claude Sonnet 4.6",
          vendor_id: 20,
          vendor: buildVendor({ id: 20, key: "anthropic", name: "Anthropic" }),
          api_family: "anthropic",
          model_type: "proxy",
          proxy_targets: [
            { target_model_id: "claude-sonnet-4-5-20250929", position: 0 },
            { target_model_id: "claude-sonnet-4-5-20250701", position: 1 },
          ],
          loadbalance_strategy_id: null,
          loadbalance_strategy: null,
        }),
      ],
    });

    expect(screen.getByText("2 targets · claude-sonnet-4-5-20250929 first")).toBeInTheDocument();
    expect(screen.queryByText(/Target claude-sonnet-4-5-20250929/i)).not.toBeInTheDocument();
  });

  it("renders legacy strategy summaries without adaptive wording", () => {
    renderTable({
      filtered: [
        buildModel({
          loadbalance_strategy: {
            ...buildModel().loadbalance_strategy!,
            id: 101,
            name: "fill-first-primary",
      strategy_type: "legacy",
      legacy_strategy_type: "fill-first",
            auto_recovery: {
              mode: "enabled",
              status_codes: [429, 503],
              cooldown: {
                base_seconds: 45,
                failure_threshold: 4,
                backoff_multiplier: 3.5,
                max_cooldown_seconds: 720,
                jitter_ratio: 0.35,
              },
              ban: {
                mode: "off",
              },
            },
          },
          loadbalance_strategy_id: 101,
        }),
      ],
    });

    expect(
      screen.getByText("fill-first-primary · Fill first"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Adaptive routing/i)).not.toBeInTheDocument();
  });

  it("renders a smaller copy button next to the model id", () => {
    renderTable();

    const copyButton = screen.getByRole("button", { name: "Copy model ID gpt-4o-mini" });

    expect(copyButton).toHaveAttribute("data-size", "icon-xs");
    expect(copyButton).toHaveClass("h-5", "w-5");
  });

  it("renders localized empty-state copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderTable({ filtered: [], search: "" });

    expect(screen.getByText("还没有配置模型")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "新建模型" })).toBeInTheDocument();
  });

  it("renders localized model row status and metric copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderTable();

    expect(screen.getAllByText("原生").length).toBeGreaterThan(0);
    expect(screen.getAllByText("已启用").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/活跃/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/成功率/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/支出/).length).toBeGreaterThan(0);
  });
});
