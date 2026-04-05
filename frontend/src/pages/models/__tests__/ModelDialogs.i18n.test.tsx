import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { DeleteModelDialog } from "../DeleteModelDialog";
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

describe("model dialogs i18n", () => {
  beforeEach(() => {
    const localStorageMock = createLocalStorageMock();

    vi.stubGlobal("localStorage", localStorageMock);
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: localStorageMock,
    });
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: originalLocalStorage,
    });
  });

  it("renders localized model dialog copy with stable form semantics", () => {
    const dialogProps: ComponentProps<typeof ModelDialog> = {
      editingModel: null,
      formData: {
        vendor_id: 1,
        api_family: "openai",
        model_id: "",
        display_name: null,
        model_type: "native",
        proxy_targets: [],
        loadbalance_strategy_id: null,
        is_enabled: true,
      },
      isDialogOpen: true,
      loadbalanceStrategies: [],
      nativeModelsForApiFamily: [],
      vendors: [],
      setFormData: vi.fn(),
      setIsDialogOpen: vi.fn(),
      setLoadbalanceStrategyId: vi.fn(),
      setModelType: vi.fn(),
      onSubmit: vi.fn(),
    };

    render(
      <LocaleProvider>
        <ModelDialog {...dialogProps} />
      </LocaleProvider>,
    );

    const modelIdInput = screen.getByLabelText("模型 ID");
    const displayNameInput = screen.getByLabelText("显示名称");

    expect(screen.getByText("新建模型")).toBeInTheDocument();
    expect(screen.getByText("供应商")).toBeInTheDocument();
    expect(screen.getByText("API 家族")).toBeInTheDocument();
    expect(modelIdInput).toHaveAttribute("name", "model_id");
    expect(modelIdInput).toHaveAttribute("autocomplete", "off");
    expect(displayNameInput).toHaveAttribute("name", "display_name");
    expect(displayNameInput).toHaveAttribute("autocomplete", "off");
    expect(modelIdInput.closest("form")).toHaveAttribute("autocomplete", "off");
    expect(document.querySelector('input[type="hidden"][name="vendor_id"]')).toHaveValue("1");
    expect(document.querySelector('input[type="hidden"][name="api_family"]')).toHaveValue("openai");
    expect(document.querySelector('input[type="hidden"][name="model_type"]')).toHaveValue("native");
    expect(screen.getByRole("button", { name: "保存" })).toBeInTheDocument();
  });

  it("renders localized delete-model dialog copy", () => {
    render(
      <LocaleProvider>
        <DeleteModelDialog
          deleteTarget={{
            id: 1,
            vendor_id: 1,
            vendor: {
              id: 1,
              key: "openai",
              name: "OpenAI",
              description: null,
              icon_key: null,
              audit_enabled: false,
              audit_capture_bodies: false,
              created_at: "",
              updated_at: "",
            },
            api_family: "openai",
            model_id: "gpt-4o",
            display_name: "GPT 4O",
            model_type: "native",
            proxy_targets: [],
            loadbalance_strategy_id: null,
            loadbalance_strategy: null,
            is_enabled: true,
            connection_count: 0,
            active_connection_count: 0,
            health_success_rate: 0,
            health_total_requests: 0,
            created_at: "",
            updated_at: "",
          }}
          onDelete={vi.fn()}
          setDeleteTarget={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("删除模型")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument();
  });
});
