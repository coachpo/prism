import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  PricingTemplateListPage,
  PricingTemplateListPageItem,
} from "@/lib/types";
import { usePricingListFacts } from "./usePricingListFacts";

const mocks = vi.hoisted(() => ({ listPage: vi.fn() }));

vi.mock("@/lib/api", () => ({
  api: { pricingTemplates: { listPage: mocks.listPage } },
}));

function makeItem(id: number): PricingTemplateListPageItem {
  return {
    id: String(id),
    profile_id: "1",
    name: `template-${id}`,
    description: null,
    current_revision: {
      revision_id: String(id),
      version: 1,
      pricing_unit: "PER_1M",
      currency_code: "USD",
      reporting_currency_epoch: 1,
      currency_attribution: "active_epoch",
      template_kind: "standard",
      card: {
        input_price: "1",
        output_price: "2",
        cached_input_price: "0",
        cache_creation_price: "0",
        reasoning_price: "0",
      },
      effective_at: null,
      created_at: "2026-01-01T00:00:00Z",
      created_by_kind: "operator",
      created_by_operation_id: null,
    },
    configuration_status: "complete",
    missing_specialty_components: [],
    model_reference_count: 1,
    endpoint_reference_count: 1,
    terminal_target_reference_count: 1,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    deleted_at: null,
  };
}

function makePage(
  items: PricingTemplateListPageItem[],
  nextCursor: string | null,
): PricingTemplateListPage {
  return {
    items,
    total_count: 3,
    consumed_count: items.length,
    list_snapshot_hash: "hash",
    next_cursor: nextCursor,
  };
}

describe("usePricingListFacts", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("stays inside the server page bound and follows the cursor to the end", async () => {
    mocks.listPage
      .mockResolvedValueOnce(makePage([makeItem(1), makeItem(2)], "cursor-2"))
      .mockResolvedValueOnce(makePage([makeItem(3)], null));

    const { result } = renderHook(() => usePricingListFacts(1));

    await waitFor(() => expect(result.current.loading).toBe(false));

    // Every template carries facts, not just the ones on the first page.
    expect([...result.current.byId.keys()]).toEqual([1, 2, 3]);
    expect(result.current.failed).toBe(false);
    expect(mocks.listPage).toHaveBeenNthCalledWith(1, {
      cursor: undefined,
      limit: 100,
    });
    expect(mocks.listPage).toHaveBeenNthCalledWith(2, {
      cursor: "cursor-2",
      limit: 100,
    });
  });

  it("reports a failed read instead of a partial one", async () => {
    mocks.listPage
      .mockResolvedValueOnce(makePage([makeItem(1)], "cursor-2"))
      .mockRejectedValueOnce(new Error("boom"));

    const { result } = renderHook(() => usePricingListFacts(1));

    await waitFor(() => expect(result.current.loading).toBe(false));

    // A page that never arrived must not leave the table showing the rows that
    // did as if the read had succeeded.
    expect(result.current.failed).toBe(true);
    expect(result.current.byId.size).toBe(0);
  });

  it("refuses to loop when the server repeats a cursor", async () => {
    mocks.listPage.mockResolvedValue(makePage([makeItem(1)], "cursor-loop"));

    const { result } = renderHook(() => usePricingListFacts(1));

    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.failed).toBe(true);
    expect(mocks.listPage).toHaveBeenCalledTimes(2);
  });
});
