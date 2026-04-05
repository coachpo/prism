import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ModelsPage } from "@/pages/ModelsPage";

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({ revision: 1 }),
}));

vi.mock("../useModelsPageData", () => ({
  useModelsPageData: () => ({
    loading: false,
    models: [{ id: 1 }],
    search: "",
    setSearch: vi.fn(),
    filtered: [],
    handleOpenDialog: vi.fn(),
    metricsLoading: false,
    modelMetrics24h: {},
    modelSpend30dMicros: {},
    editingModel: null,
    formData: {
      vendor_id: 1,
      api_family: "openai",
      model_id: "",
      display_name: null,
      model_type: "native",
      loadbalance_strategy_id: null,
      is_enabled: true,
    },
    isDialogOpen: false,
    loadbalanceStrategies: [],
    nativeModelsForApiFamily: [],
    vendors: [],
    selectedVendor: undefined,
    setFormData: vi.fn(),
    setIsDialogOpen: vi.fn(),
    setLoadbalanceStrategyId: vi.fn(),
    setModelType: vi.fn(),
    handleSubmit: vi.fn(),
    deleteTarget: null,
    handleDelete: vi.fn(),
    setDeleteTarget: vi.fn(),
  }),
}));

describe("ModelsPage shell i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized page header, count copy, button, and search placeholder", () => {
    render(
      <LocaleProvider>
        <MemoryRouter>
          <ModelsPage />
        </MemoryRouter>
      </LocaleProvider>,
    );

    expect(screen.getByText("模型")).toBeInTheDocument();
    expect(screen.getByText("1 个模型配置")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "新建模型" }).length).toBeGreaterThan(0);
    expect(screen.getByPlaceholderText("搜索模型...")) .toBeInTheDocument();
    expect(screen.getByPlaceholderText("搜索模型...")).toHaveAttribute("name", "models_search");
    expect(screen.getByPlaceholderText("搜索模型...")).toHaveAttribute("autocomplete", "off");
  });
});
