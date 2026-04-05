import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { LoadbalanceStrategy, ModelConfigCreate, ModelConfigListItem, Vendor } from "@/lib/types";
import { ModelDialog } from "../ModelDialog";

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
  const { icon_key = null, ...rest } = overrides;

  return {
    id: 7,
    key: "openai",
    name: "OpenAI",
    description: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
    ...rest,
    icon_key,
  };
}

function buildNativeModel(overrides: Partial<ModelConfigListItem> = {}): ModelConfigListItem {
  return {
    id: 11,
    vendor_id: 7,
    vendor: buildVendor(),
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o Mini",
    model_type: "native",
    loadbalance_strategy_id: 100,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 99,
    health_total_requests: 100,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
    proxy_targets: [],
    ...overrides,
  } as unknown as ModelConfigListItem;
}

const loadbalanceStrategies: LoadbalanceStrategy[] = [
  {
    id: 100,
    profile_id: 1,
    name: "single-primary",
    strategy_type: "legacy",
    legacy_strategy_type: "single",
    auto_recovery: {
      mode: "disabled",
    },
    attached_model_count: 1,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
  },
  {
    id: 101,
    profile_id: 1,
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
    attached_model_count: 2,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
  },
];

function Harness() {
  const vendors = [buildVendor()];
  const nativeModelsForApiFamily = [
    buildNativeModel(),
    buildNativeModel({ id: 12, model_id: "gpt-4.1-mini", display_name: "GPT-4.1 Mini" }),
    buildNativeModel({ id: 13, model_id: "gpt-4.1", display_name: "GPT-4.1" }),
  ];
  const [formData, setFormData] = useState<ModelConfigCreate>(
    {
      vendor_id: 7,
      api_family: "openai",
      model_id: "friendly-proxy",
      display_name: "Friendly Proxy",
      model_type: "proxy",
      loadbalance_strategy_id: null,
      is_enabled: true,
      proxy_targets: [{ target_model_id: "gpt-4o-mini", position: 0 }],
    } as unknown as ModelConfigCreate,
  );

  return (
    <LocaleProvider>
      <ModelDialog
        editingModel={null}
        formData={formData}
        isDialogOpen
        loadbalanceStrategies={loadbalanceStrategies}
        nativeModelsForApiFamily={nativeModelsForApiFamily}
        vendors={vendors}
        setFormData={setFormData}
        setIsDialogOpen={vi.fn()}
        setLoadbalanceStrategyId={vi.fn()}
        setModelType={(value) => {
          setFormData((current) => ({ ...current, model_type: value } as unknown as ModelConfigCreate));
        }}
        onSubmit={vi.fn()}
      />
      <pre data-testid="proxy-targets-state">{JSON.stringify((formData as { proxy_targets?: unknown }).proxy_targets ?? [])}</pre>
    </LocaleProvider>
  );
}

function EmptyProxyTargetsHarness() {
  const vendors = [buildVendor()];
  const [formData, setFormData] = useState<ModelConfigCreate>(
    {
      vendor_id: 7,
      api_family: "openai",
      model_id: "friendly-proxy",
      display_name: "Friendly Proxy",
      model_type: "proxy",
      loadbalance_strategy_id: null,
      is_enabled: true,
      proxy_targets: [],
    } as unknown as ModelConfigCreate,
  );

  return (
    <LocaleProvider>
      <ModelDialog
        editingModel={null}
        formData={formData}
        isDialogOpen
        loadbalanceStrategies={loadbalanceStrategies}
        nativeModelsForApiFamily={[]}
        vendors={vendors}
        setFormData={setFormData}
        setIsDialogOpen={vi.fn()}
        setLoadbalanceStrategyId={vi.fn()}
        setModelType={(value) => {
          setFormData((current) => ({ ...current, model_type: value } as unknown as ModelConfigCreate));
        }}
        onSubmit={vi.fn()}
      />
    </LocaleProvider>
  );
}

function NewNativeModelHarness({
  loadbalanceStrategyList = loadbalanceStrategies,
}: {
  loadbalanceStrategyList?: LoadbalanceStrategy[];
}) {
  const vendors = [buildVendor()];
  const [formData, setFormData] = useState<ModelConfigCreate>({
    vendor_id: 7,
    api_family: "openai",
    model_id: "",
    display_name: "",
    model_type: "native",
    loadbalance_strategy_id: loadbalanceStrategyList[0]?.id ?? null,
    is_enabled: true,
    proxy_targets: [],
  });

  return (
    <LocaleProvider>
      <ModelDialog
        editingModel={null}
        formData={formData}
        isDialogOpen
        loadbalanceStrategies={loadbalanceStrategyList}
        nativeModelsForApiFamily={[]}
        vendors={vendors}
        setFormData={setFormData}
        setIsDialogOpen={vi.fn()}
        setLoadbalanceStrategyId={(value) => {
          setFormData((current) => ({ ...current, loadbalance_strategy_id: value }));
        }}
        setModelType={(value) => {
          setFormData((current) => ({ ...current, model_type: value }));
        }}
        onSubmit={vi.fn()}
      />
    </LocaleProvider>
  );
}

describe("ModelDialog proxy target editing", () => {
  beforeEach(() => {
    const localStorageMock = createLocalStorageMock();

    vi.stubGlobal("localStorage", localStorageMock);
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    vi.stubGlobal("ResizeObserver", ResizeObserverMock);
    HTMLElement.prototype.scrollIntoView = vi.fn();
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

  it("renders proxy dialog selects as full-width controls inside a scroll-safe dialog", () => {
    render(<Harness />);

    expect(screen.getByRole("dialog")).toHaveClass("max-h-[90vh]", "max-w-2xl", "overflow-y-auto");
    screen.getAllByRole("combobox").forEach((combobox) => {
      expect(combobox).toHaveClass("w-full", "min-w-0", "max-w-full");
    });
    expect(screen.getByText("Vendor").parentElement).toHaveClass("min-w-0");
    expect(screen.getByText("API Family").parentElement).toHaveClass("min-w-0");
  });

  it("keeps native loadbalance strategy selection full-width with local wrapping affordances", () => {
    render(
      <LocaleProvider>
        <ModelDialog
          editingModel={null}
          formData={{
            vendor_id: 7,
            api_family: "openai",
            model_id: "gpt-4o-mini",
            display_name: "GPT-4o Mini",
            model_type: "native",
            proxy_targets: [],
            loadbalance_strategy_id: 100,
            is_enabled: true,
          }}
          isDialogOpen
          loadbalanceStrategies={loadbalanceStrategies}
          nativeModelsForApiFamily={[]}
          vendors={[buildVendor()]}
          setFormData={vi.fn()}
          setIsDialogOpen={vi.fn()}
          setLoadbalanceStrategyId={vi.fn()}
          setModelType={vi.fn()}
          onSubmit={vi.fn()}
        />
      </LocaleProvider>,
    );

    const strategyTrigger = document.getElementById("model-loadbalance-strategy");
    const strategyValueContent = strategyTrigger?.querySelector('[data-slot="select-value"] > span');

    expect(strategyTrigger).toHaveClass("w-full");
    expect(strategyTrigger).toHaveClass("min-w-0");
    expect(strategyTrigger).toHaveClass("items-start");
    expect(strategyTrigger).toHaveClass("text-left");
    expect(strategyTrigger).toHaveClass("whitespace-normal");
    expect(strategyValueContent).toHaveClass("min-w-0");
    expect(strategyValueContent).toHaveClass("whitespace-normal");
    expect(strategyValueContent).toHaveClass("break-words");
    screen.getAllByRole("combobox").forEach((combobox) => {
      expect(combobox).toHaveClass("w-full", "min-w-0", "max-w-full");
    });
  });

  it("keeps the native loadbalance strategy select controlled when a strategy is chosen", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    const { rerender } = render(
      <LocaleProvider>
        <ModelDialog
          editingModel={null}
          formData={{
            vendor_id: 7,
            api_family: "openai",
            model_id: "gpt-4o-mini",
            display_name: "GPT-4o Mini",
            model_type: "native",
            proxy_targets: [],
            loadbalance_strategy_id: null,
            is_enabled: true,
          }}
          isDialogOpen
          loadbalanceStrategies={loadbalanceStrategies}
          nativeModelsForApiFamily={[]}
          vendors={[buildVendor()]}
          setFormData={vi.fn()}
          setIsDialogOpen={vi.fn()}
          setLoadbalanceStrategyId={vi.fn()}
          setModelType={vi.fn()}
          onSubmit={vi.fn()}
        />
      </LocaleProvider>,
    );

    rerender(
      <LocaleProvider>
        <ModelDialog
          editingModel={null}
          formData={{
            vendor_id: 7,
            api_family: "openai",
            model_id: "gpt-4o-mini",
            display_name: "GPT-4o Mini",
            model_type: "native",
            proxy_targets: [],
            loadbalance_strategy_id: 100,
            is_enabled: true,
          }}
          isDialogOpen
          loadbalanceStrategies={loadbalanceStrategies}
          nativeModelsForApiFamily={[]}
          vendors={[buildVendor()]}
          setFormData={vi.fn()}
          setIsDialogOpen={vi.fn()}
          setLoadbalanceStrategyId={vi.fn()}
          setModelType={vi.fn()}
          onSubmit={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(consoleError).not.toHaveBeenCalledWith(
      expect.stringContaining("changing an uncontrolled input to be controlled"),
    );
    expect(consoleError).not.toHaveBeenCalledWith(
      expect.stringContaining("uncontrolled to controlled"),
    );
  });

  it("shows legacy and adaptive strategy options with kind-aware summaries", () => {
    render(
      <LocaleProvider>
        <ModelDialog
          editingModel={null}
          formData={{
            vendor_id: 7,
            api_family: "openai",
            model_id: "gpt-4o-mini",
            display_name: "GPT-4o Mini",
            model_type: "native",
            proxy_targets: [],
            loadbalance_strategy_id: 100,
            is_enabled: true,
          }}
          isDialogOpen
          loadbalanceStrategies={loadbalanceStrategies}
          nativeModelsForApiFamily={[]}
          vendors={[buildVendor()]}
          setFormData={vi.fn()}
          setIsDialogOpen={vi.fn()}
          setLoadbalanceStrategyId={vi.fn()}
          setModelType={vi.fn()}
          onSubmit={vi.fn()}
        />
      </LocaleProvider>,
    );

    fireEvent.click(document.getElementById("model-loadbalance-strategy") as HTMLElement);

    expect(screen.getAllByText("single-primary (Single)").length).toBeGreaterThan(0);
    expect(screen.getAllByText("adaptive-availability (Adaptive strategy • Minimize latency)").length).toBeGreaterThan(0);

    const adaptiveOption = screen
      .getAllByText("adaptive-availability (Adaptive strategy • Minimize latency)")
      .find((node) => node.closest('[data-slot="select-content"]'));

    expect(adaptiveOption).toHaveClass("whitespace-normal");
    expect(adaptiveOption).toHaveClass("break-words");
  });

  it("removes proxy target assignment controls and hidden proxy target inputs from the dialog", () => {
    render(<Harness />);

    expect(screen.queryByRole("button", { name: "Add Target" })).not.toBeInTheDocument();
    expect(screen.queryByText("GPT-4o Mini (gpt-4o-mini)")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Move target gpt-4o-mini up" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove target gpt-4o-mini" })).not.toBeInTheDocument();
    expect(document.querySelector('input[name^="proxy_targets."]')).not.toBeInTheDocument();
    expect(screen.getByTestId("proxy-targets-state")).toHaveTextContent(
      JSON.stringify([{ target_model_id: "gpt-4o-mini", position: 0 }]),
    );
  });

  it("explains that proxy targets can be configured later on the proxy detail page", () => {
    render(<EmptyProxyTargetsHarness />);

    expect(screen.getByText("No proxy targets selected yet.")).toBeInTheDocument();
    expect(
      screen.getByText("You can create this proxy now and configure targets later on /models/:id/proxy."),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "No native models available for the openai API family yet. Configure targets later on /models/:id/proxy.",
      ),
    ).toBeInTheDocument();
  });

  it("keeps legacy strategy wording attached for native models", () => {
    render(
      <LocaleProvider>
        <ModelDialog
          editingModel={null}
          formData={{
            vendor_id: 7,
            api_family: "openai",
            model_id: "gpt-4o-mini",
            display_name: "GPT-4o Mini",
            model_type: "native",
            proxy_targets: [],
            loadbalance_strategy_id: 101,
            is_enabled: true,
          }}
          isDialogOpen
          loadbalanceStrategies={[
            ...loadbalanceStrategies,
            {
              ...loadbalanceStrategies[0],
              id: 101,
              name: "round-robin-availability",
      strategy_type: "legacy",
      legacy_strategy_type: "round-robin",
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
          ]}
          nativeModelsForApiFamily={[]}
          vendors={[buildVendor()]}
          setFormData={vi.fn()}
          setIsDialogOpen={vi.fn()}
          setLoadbalanceStrategyId={vi.fn()}
          setModelType={vi.fn()}
          onSubmit={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(document.getElementById("model-loadbalance-strategy")).toHaveTextContent(
      "round-robin-availability (Round robin)",
    );
  });

  it("syncs display name from model id until the user replaces the auto-generated value", () => {
    render(<NewNativeModelHarness />);

    const modelIdInput = screen.getByLabelText("Model ID");
    const displayNameInput = screen.getByLabelText("Display Name");

    fireEvent.change(modelIdInput, { target: { value: "gpt-4o" } });
    expect(displayNameInput).toHaveValue("gpt-4o");

    fireEvent.change(modelIdInput, { target: { value: "gpt-4.1" } });
    expect(displayNameInput).toHaveValue("gpt-4.1");

    fireEvent.change(displayNameInput, { target: { value: "Friendly Gateway" } });
    fireEvent.change(modelIdInput, { target: { value: "gpt-5.1" } });
    expect(displayNameInput).toHaveValue("Friendly Gateway");

    fireEvent.change(displayNameInput, { target: { value: "" } });
    fireEvent.change(modelIdInput, { target: { value: "gpt-5.4-mini" } });
    expect(displayNameInput).toHaveValue("gpt-5.4-mini");
  });

  it("replaces the native strategy select with a create-one notice when no strategies exist", () => {
    render(<NewNativeModelHarness loadbalanceStrategyList={[]} />);

    expect(
      screen.getByText(
        "No loadbalance strategies are available for this profile. Create one on the Loadbalance Strategies page first.",
      ),
    ).toBeInTheDocument();
    expect(document.getElementById("model-loadbalance-strategy")).not.toBeInTheDocument();
  });

  it.fails("tracks the 390px long-text clipping regression for model identifiers", () => {
    const originalInnerWidth = window.innerWidth;

    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 390,
      writable: true,
    });
    window.dispatchEvent(new Event("resize"));

    try {
      render(
        <LocaleProvider>
          <ModelDialog
            editingModel={null}
            formData={{
              vendor_id: 7,
              api_family: "openai",
              model_id: "gpt-5.4-super-long-unbroken-routing-identifier-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
              display_name:
                "超长模型显示名称-用于观察对话框布局与字段间距-LongLocalizedFriendlyName-1234567890",
              model_type: "native",
              proxy_targets: [],
              loadbalance_strategy_id: 100,
              is_enabled: true,
            }}
            isDialogOpen
            loadbalanceStrategies={loadbalanceStrategies}
            nativeModelsForApiFamily={[]}
            vendors={[buildVendor()]}
            setFormData={vi.fn()}
            setIsDialogOpen={vi.fn()}
            setLoadbalanceStrategyId={vi.fn()}
            setModelType={vi.fn()}
            onSubmit={vi.fn()}
          />
        </LocaleProvider>,
      );

      const dialog = screen.getByRole("dialog");
      const modelIdInput = document.getElementById("model-id");
      const displayNameInput = document.getElementById("model-display-name");

      expect(modelIdInput).toHaveValue(
        "gpt-5.4-super-long-unbroken-routing-identifier-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      );
      expect(displayNameInput).toHaveValue(
        "超长模型显示名称-用于观察对话框布局与字段间距-LongLocalizedFriendlyName-1234567890",
      );
      expect(dialog).toHaveClass("overflow-x-auto");
    } finally {
      Object.defineProperty(window, "innerWidth", {
        configurable: true,
        value: originalInnerWidth,
        writable: true,
      });
      window.dispatchEvent(new Event("resize"));
    }
  });
});
