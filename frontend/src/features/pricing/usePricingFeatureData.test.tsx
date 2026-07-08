import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { PricingTemplate, PricingTemplateImportRequest } from "@/lib/types"
import { usePricingFeatureData } from "./usePricingFeatureData"

const mocks = vi.hoisted(() => ({
  getSharedPricingTemplates: vi.fn(),
  importTemplates: vi.fn(),
  setSharedPricingTemplates: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  ApiError: class ApiError extends Error {
    status = 500
    detail: unknown
  },
  api: {
    pricingTemplates: {
      importTemplates: mocks.importTemplates,
    },
  },
}))

vi.mock("@/lib/referenceData", () => ({
  getSharedPricingTemplates: mocks.getSharedPricingTemplates,
  setSharedPricingTemplates: mocks.setSharedPricingTemplates,
}))

vi.mock("@/i18n/staticMessages", () => ({
  getStaticMessages: () => ({
    common: { requestFailed: "Request failed" },
    pricing: {
      importResultSummary: (created: number, updated: number, skipped: number) =>
        `${created}/${updated}/${skipped}`,
    },
    pricingTemplatesData: {
      changedWhileEditing: "Changed while editing",
      created: "Created",
      deleted: "Deleted",
      deleteFailed: "Delete failed",
      inUseCannotDelete: "In use",
      loadFailed: "Load failed",
      loadSingleFailed: "Load single failed",
      loadUsageFailed: "Load usage failed",
      saveFailed: "Save failed",
      updated: "Updated",
    },
  }),
}))

vi.mock("sonner", () => ({
  toast: {
    error: mocks.toastError,
    success: mocks.toastSuccess,
  },
}))

describe("usePricingFeatureData", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("force-refreshes pricing templates after import so cached data is replaced", async () => {
    const staleTemplates = [makeTemplate(1, "Before import")]
    const freshTemplates = [makeTemplate(2, "Imported")]
    mocks.getSharedPricingTemplates.mockImplementation(async (_revision: number, forceRefresh?: boolean) =>
      forceRefresh ? freshTemplates : staleTemplates,
    )
    mocks.importTemplates.mockResolvedValue({ created: 1, updated: 0, skipped: [], errors: [] })

    const { result } = renderHook(() => usePricingFeatureData(3))

    await waitFor(() => {
      expect(result.current.pricingTemplates.map((template) => template.name)).toEqual([
        "Before import",
      ])
    })

    await act(async () => {
      await result.current.handleImportPricingTemplates(importRequest)
    })

    await waitFor(() => {
      expect(result.current.pricingTemplates.map((template) => template.name)).toEqual([
        "Imported",
      ])
    })
    expect(mocks.getSharedPricingTemplates).toHaveBeenLastCalledWith(3, true)
  })
})

const importRequest: PricingTemplateImportRequest = {
  mode: "upsert_by_name",
  templates: [
    {
      name: "Imported",
      pricing_unit: "PER_1M",
      pricing_currency_code: "USD",
      input_price: "1",
      output_price: "2",
      cached_input_price: "0",
      cache_creation_price: "0",
      reasoning_price: "0",
    },
  ],
}

function makeTemplate(id: number, name: string): PricingTemplate {
  return {
    id,
    profile_id: 1,
    name,
    description: null,
    pricing_unit: "PER_1M",
    pricing_currency_code: "USD",
    input_price: "1",
    output_price: "2",
    cached_input_price: "0",
    cache_creation_price: "0",
    reasoning_price: "0",
    version: 1,
    created_at: `2026-07-08T00:00:0${id}Z`,
    updated_at: `2026-07-08T00:00:0${id}Z`,
  }
}
