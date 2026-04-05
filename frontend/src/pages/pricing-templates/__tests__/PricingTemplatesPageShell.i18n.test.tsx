import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { PricingTemplatesPage } from "@/pages/PricingTemplatesPage";

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({
    revision: 1,
    selectedProfile: { id: 1, name: "Default" },
  }),
}));

vi.mock("../usePricingTemplatesPageData", () => ({
  usePricingTemplatesPageData: () => ({
    pricingTemplates: [],
    pricingTemplatesLoading: false,
    pricingTemplatePreparingEditId: null,
    handleOpenCreateDialog: vi.fn(),
    handleDeletePricingTemplateClick: vi.fn(),
    handlePrepareEditPricingTemplate: vi.fn(),
    editingPricingTemplate: null,
    pricingTemplateForm: {
      name: "",
      description: "",
      pricing_currency_code: "USD",
      input_price: "",
      output_price: "",
      cached_input_price: "",
      cache_creation_price: "",
      reasoning_price: "",
      missing_special_token_price_policy: "MAP_TO_OUTPUT",
    },
    pricingTemplateSaving: false,
    closePricingTemplateDialog: vi.fn(),
    setPricingTemplateDialogOpen: vi.fn(),
    handleSavePricingTemplate: vi.fn(),
    pricingTemplateDialogOpen: false,
    setPricingTemplateForm: vi.fn(),
    pricingTemplateUsageDialogOpen: false,
    closeUsageDialog: vi.fn(),
    setPricingTemplateUsageDialogOpen: vi.fn(),
    pricingTemplateUsageLoading: false,
    pricingTemplateUsageTemplate: null,
    handleViewPricingTemplateUsage: vi.fn(),
    viewingPricingTemplateUsage: null,
    pricingTemplateUsageRows: [],
    deletePricingTemplateConfirm: null,
    deletePricingTemplateConflict: null,
    pricingTemplateDeleting: false,
    setDeletePricingTemplateConfirm: vi.fn(),
    setDeletePricingTemplateConflict: vi.fn(),
    handleDeletePricingTemplate: vi.fn(),
  }),
}));

describe("PricingTemplatesPage shell i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized page shell copy", () => {
    render(
      <LocaleProvider>
        <PricingTemplatesPage />
      </LocaleProvider>,
    );

    expect(screen.getByRole("heading", { name: "价格模板" })).toBeInTheDocument();
    expect(screen.getAllByText("管理可在模型和端点间复用的价格模板").length).toBeGreaterThan(0);
    expect(screen.getByText("配置档案作用域设置")).toBeInTheDocument();
  });
});
