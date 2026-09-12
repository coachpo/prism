import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  PricingTemplate,
  PricingTemplateImportRequest,
} from "@/lib/types";
import { usePricingFeatureData } from "./usePricingFeatureData";

const mocks = vi.hoisted(() => ({
  getSharedPricingTemplates: vi.fn(),
  getTemplate: vi.fn(),
  impact: vi.fn(),
  importTemplates: vi.fn(),
  importCommit: vi.fn(),
  setSharedPricingTemplates: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  ApiError: class ApiError extends Error {
    status = 500;
    detail: unknown;
  },
  api: {
    pricingTemplates: {
      get: mocks.getTemplate,
      impact: mocks.impact,
      importTemplates: mocks.importTemplates,
      importCommit: mocks.importCommit,
    },
  },
}));

vi.mock("@/lib/referenceData", () => ({
  getSharedPricingTemplates: mocks.getSharedPricingTemplates,
  setSharedPricingTemplates: mocks.setSharedPricingTemplates,
}));

vi.mock("@/i18n/staticMessages", () => ({
  getStaticMessages: () => ({
    common: { requestFailed: "Request failed" },
    pricing: {
      importResultSummary: (
        created: number,
        updated: number,
        skipped: number,
      ) => `${created}/${updated}/${skipped}`,
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
}));

vi.mock("sonner", () => ({
  toast: {
    error: mocks.toastError,
    success: mocks.toastSuccess,
  },
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => { resolve = complete; });
  return { promise, resolve };
}

describe("usePricingFeatureData", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("does not replace a new draft with a late edit read", async () => {
    const template = makeTemplate(1, "Old template");
    const delayed = deferred<PricingTemplate>();
    mocks.getTemplate.mockReturnValueOnce(delayed.promise);
    const { result } = renderHook(() => usePricingFeatureData(3));
    let editing: Promise<void>;
    act(() => { editing = result.current.handleEditPricingTemplate(template); });
    act(() => { result.current.openCreatePricingTemplateDialog(); });
    await act(async () => { delayed.resolve(template); await editing; });
    expect(result.current.editingPricingTemplate).toBeNull();
    expect(result.current.pricingTemplateDialogOpen).toBe(true);
    expect(result.current.pricingTemplateImpactLoading).toBe(false);
    expect(mocks.impact).not.toHaveBeenCalled();
  });

  it("discards a cancelled impact retry before opening another template", async () => {
    mocks.getTemplate.mockImplementation(async (id: number) => makeTemplate(id, `Template ${id}`));
    mocks.impact.mockResolvedValue({ template_id: 1 });
    const { result } = renderHook(() => usePricingFeatureData(3));
    await act(async () => { await result.current.handleEditPricingTemplate(makeTemplate(1, "First")); });
    const delayed = deferred<{ template_id: number }>();
    mocks.impact.mockReturnValueOnce(delayed.promise).mockResolvedValueOnce({ template_id: 2 });
    let retrying: Promise<void>;
    act(() => { retrying = result.current.retryPricingTemplateImpact(); result.current.closePricingTemplateDialog(); });
    await act(async () => { await result.current.handleEditPricingTemplate(makeTemplate(2, "Second")); });
    await act(async () => { delayed.resolve({ template_id: 1 }); await retrying; });
    expect(result.current.editingPricingTemplate?.id).toBe(2);
    expect(result.current.pricingTemplateImpact).toEqual({ template_id: 2 });
    expect(result.current.pricingTemplateImpactError).toBeNull();
  });

  it("previews an import without writing, then commits with the server preview hash", async () => {
    const staleTemplates = [makeTemplate(1, "Before import")];
    const freshTemplates = [makeTemplate(2, "Imported")];
    mocks.getSharedPricingTemplates.mockImplementation(
      async (_revision: number, forceRefresh?: boolean) =>
        forceRefresh ? freshTemplates : staleTemplates,
    );
    mocks.importTemplates.mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: [],
      errors: [],
      preview_hash: "preview-hash",
      committable: true,
    });
    mocks.importCommit.mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: [],
      errors: [],
    });

    const { result } = renderHook(() => usePricingFeatureData(3));

    await waitFor(() => {
      expect(
        result.current.pricingTemplates.map((template) => template.name),
      ).toEqual(["Before import"]);
    });

    await act(async () => {
      await result.current.handleImportPricingTemplates(importRequest);
    });

    // Phase one writes nothing: the preview is surfaced for confirmation.
    expect(mocks.importCommit).not.toHaveBeenCalled();
    expect(result.current.importPreview?.response.preview_hash).toBe(
      "preview-hash",
    );
    expect(
      result.current.pricingTemplates.map((template) => template.name),
    ).toEqual(["Before import"]);

    await act(async () => {
      await result.current.commitImportPreview();
    });

    expect(mocks.importCommit).toHaveBeenCalledWith({
      schema_version: 3,
      mode: importRequest.mode,
      templates: importRequest.templates,
      preview_hash: "preview-hash",
    });
    await waitFor(() => {
      expect(
        result.current.pricingTemplates.map((template) => template.name),
      ).toEqual(["Imported"]);
    });
    expect(mocks.getSharedPricingTemplates).toHaveBeenLastCalledWith(3, true);
    expect(result.current.importPreview).toBeNull();
  });

  it("fails an uncommittable preview closed", async () => {
    mocks.getSharedPricingTemplates.mockResolvedValue([]);
    mocks.importTemplates.mockResolvedValue({
      created: 0,
      updated: 0,
      skipped: [],
      errors: [{ detail: "bad row" }],
      preview_hash: "preview-hash",
      committable: false,
    });

    const { result } = renderHook(() => usePricingFeatureData(3));

    await act(async () => {
      await result.current.handleImportPricingTemplates(importRequest);
    });
    await act(async () => {
      await result.current.commitImportPreview();
    });

    expect(mocks.importCommit).not.toHaveBeenCalled();
  });
});

const importRequest: PricingTemplateImportRequest = {
  schema_version: 3,
  mode: "upsert_by_name",
  templates: [
    {
      name: "Imported",
      template_kind: "standard",
      card: {
        input_price: "1",
        output_price: "2",
        cached_input_price: "0",
        cache_creation_price: "0",
        reasoning_price: "0",
      },
    },
  ],
};

function makeTemplate(id: number, name: string): PricingTemplate {
  return {
    id,
    profile_id: 1,
    name,
    description: null,
    pricing_unit: "PER_1M",
    pricing_currency_code: "USD",
    active_currency_symbol: "$",
    catalog_provider_id: null,
    catalog_model_id: null,
    revision_source: "manual",
    catalog_revision: null,
    template_kind: "standard",
    card: {
      input_price: "1",
      output_price: "2",
      cached_input_price: "0",
      cache_creation_price: "0",
      reasoning_price: "0",
    },
    version: 1,
    revision_id: 1,
    version_effective_at: null,
    reporting_currency_epoch: 1,
    revision_count: 1,
    created_at: `2026-07-08T00:00:0${id}Z`,
    updated_at: `2026-07-08T00:00:0${id}Z`,
  };
}
