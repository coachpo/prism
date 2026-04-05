import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import type { PricingTemplate } from "@/lib/types";
import { usePricingTemplatesPageData } from "../usePricingTemplatesPageData";

const api = vi.hoisted(() => ({
  pricingTemplates: {
    get: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    connections: vi.fn(),
  },
}));

const referenceData = vi.hoisted(() => ({
  getSharedPricingTemplates: vi.fn(),
  setSharedPricingTemplates: vi.fn(),
}));

vi.mock("@/lib/api", () => ({ api, ApiError: class ApiError extends Error { status = 409; detail = null; } }));
vi.mock("@/lib/referenceData", () => ({
  getSharedPricingTemplates: referenceData.getSharedPricingTemplates,
  setSharedPricingTemplates: referenceData.setSharedPricingTemplates,
}));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

function buildPricingTemplate(overrides: Partial<PricingTemplate> = {}): PricingTemplate {
  return {
    id: 1,
    profile_id: 1,
    name: "Starter Template",
    version: 1,
    description: null,
    pricing_currency_code: "USD",
    pricing_unit: "PER_1M" as const,
    input_price: "0.10",
    output_price: "0.20",
    cached_input_price: null,
    cache_creation_price: null,
    reasoning_price: null,
    missing_special_token_price_policy: "MAP_TO_OUTPUT" as const,
    created_at: "2026-03-30T09:00:00Z",
    updated_at: "2026-03-30T10:00:00Z",
    ...overrides,
  };
}

describe("usePricingTemplatesPageData i18n", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.documentElement.lang = "zh-CN";
    referenceData.getSharedPricingTemplates.mockResolvedValue([]);
  });

  it("emits localized validation errors when the template name is missing", async () => {
    const { result } = renderHook(() => usePricingTemplatesPageData(1));

    await waitFor(() => {
      expect(result.current.pricingTemplatesLoading).toBe(false);
    });

    await act(async () => {
      await result.current.handleSavePricingTemplate();
    });

    expect(toast.error).toHaveBeenCalledWith("名称为必填项");
  });

  it("emits localized validation errors for invalid output price when name is present", async () => {
    const { result } = renderHook(() => usePricingTemplatesPageData(1));

    await waitFor(() => {
      expect(result.current.pricingTemplatesLoading).toBe(false);
    });

    await act(async () => {
      result.current.setPricingTemplateForm((prev) => ({
        ...prev,
        name: "价格模板",
        input_price: "12.3456",
        output_price: "not-a-number",
      }));
    });

    await act(async () => {
      await result.current.handleSavePricingTemplate();
    });

    expect(toast.error).toHaveBeenCalledWith("输出价格必须为非负数");
    expect(api.pricingTemplates.create).not.toHaveBeenCalled();
  });

  it("keeps the create draft stable while closing after a successful save and prepends the new template locally", async () => {
    const created = buildPricingTemplate({
      id: 3,
      name: "Canary Pricing",
      description: "Created in dialog",
      input_price: "0.15",
      output_price: "0.30",
      updated_at: "2026-03-30T11:00:00Z",
    });
    api.pricingTemplates.create.mockResolvedValue(created);

    const { result } = renderHook(() => usePricingTemplatesPageData(1));

    await waitFor(() => {
      expect(result.current.pricingTemplatesLoading).toBe(false);
    });

    act(() => {
      result.current.openCreatePricingTemplateDialog();
      result.current.setPricingTemplateForm((prev) => ({
        ...prev,
        name: "Canary Pricing",
        description: "Created in dialog",
        input_price: "0.15",
        output_price: "0.30",
      }));
    });

    await act(async () => {
      await result.current.handleSavePricingTemplate();
    });

    expect(api.pricingTemplates.create).toHaveBeenCalledWith({
      name: "Canary Pricing",
      description: "Created in dialog",
      pricing_currency_code: "USD",
      input_price: "0.15",
      output_price: "0.30",
      cached_input_price: null,
      cache_creation_price: null,
      reasoning_price: null,
      missing_special_token_price_policy: "MAP_TO_OUTPUT",
    });
    expect(result.current.pricingTemplateDialogOpen).toBe(false);
    expect(result.current.pricingTemplates.map((template) => template.id)).toEqual([3]);
    expect(result.current.pricingTemplateForm).toMatchObject({
      name: "Canary Pricing",
      description: "Created in dialog",
      input_price: "0.15",
      output_price: "0.30",
    });
  });

  it("keeps the edit snapshot stable while closing after a successful save and updates the local list", async () => {
    const summary = buildPricingTemplate({
      id: 7,
      name: "Starter Template",
      description: "Original description",
      updated_at: "2026-03-30T10:00:00Z",
    });
    const loaded = buildPricingTemplate({
      id: 7,
      name: "Starter Template",
      description: "Original description",
      updated_at: "2026-03-30T10:00:00Z",
    });
    const updated = buildPricingTemplate({
      id: 7,
      name: "Starter Template Updated",
      description: "Edited description",
      updated_at: "2026-03-30T12:00:00Z",
    });
    referenceData.getSharedPricingTemplates.mockResolvedValueOnce([summary]);
    api.pricingTemplates.get.mockResolvedValue(loaded);
    api.pricingTemplates.update.mockResolvedValue(updated);

    const { result } = renderHook(() => usePricingTemplatesPageData(1));

    await waitFor(() => {
      expect(result.current.pricingTemplatesLoading).toBe(false);
      expect(result.current.pricingTemplates[0]?.id).toBe(7);
    });

    await act(async () => {
      await result.current.handleEditPricingTemplate(summary);
    });

    act(() => {
      result.current.setPricingTemplateForm((prev) => ({
        ...prev,
        name: "Starter Template Updated",
        description: "Edited description",
      }));
    });

    await act(async () => {
      await result.current.handleSavePricingTemplate();
    });

    expect(api.pricingTemplates.update).toHaveBeenCalledWith(7, {
      expected_updated_at: loaded.updated_at,
      name: "Starter Template Updated",
      description: "Edited description",
      pricing_currency_code: "USD",
      input_price: "0.10",
      output_price: "0.20",
      cached_input_price: null,
      cache_creation_price: null,
      reasoning_price: null,
      missing_special_token_price_policy: "MAP_TO_OUTPUT",
    });
    expect(result.current.pricingTemplateDialogOpen).toBe(false);
    expect(result.current.pricingTemplates[0]).toMatchObject({
      id: 7,
      name: "Starter Template Updated",
      description: "Edited description",
    });
    expect(result.current.editingPricingTemplate?.id).toBe(7);
    expect(result.current.pricingTemplateForm).toMatchObject({
      name: "Starter Template Updated",
      description: "Edited description",
    });
  });
});
