import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type {
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
} from "@/lib/types";
import { usePricingTemplateDetailReads } from "./usePricingTemplateDetailReads";

const mocks = vi.hoisted(() => ({
  connections: vi.fn(),
  revisions: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    pricingTemplates: {
      connections: mocks.connections,
      revisions: mocks.revisions,
    },
  },
}));

vi.mock("@/i18n/staticMessages", () => ({
  getStaticMessages: () => ({
    pricingTemplatesData: {
      historyLoadFailed: "History failed",
      loadUsageFailed: "Usage failed",
    },
  }),
}));

function template(id: number) {
  return { id } as PricingTemplate;
}

function usageItem(id: number): PricingTemplateConnectionUsageItem {
  return {
    connection_id: id,
    connection_name: `Connection ${id}`,
    model_config_id: id,
    model_id: `model-${id}`,
    endpoint_id: id,
    endpoint_name: `Endpoint ${id}`,
  };
}

describe("usePricingTemplateDetailReads", () => {
  it("fences a late usage response behind the newest template read", async () => {
    let resolveFirst!: (value: {
      template_id: number;
      items: PricingTemplateConnectionUsageItem[];
    }) => void;
    mocks.connections.mockImplementation((templateId: number) => {
      if (templateId === 1) {
        return new Promise((resolve) => {
          resolveFirst = resolve;
        });
      }
      return Promise.resolve({ template_id: 2, items: [usageItem(2)] });
    });

    const { result } = renderHook(() => usePricingTemplateDetailReads());

    act(() => {
      void result.current.handleViewPricingTemplateUsage(template(1));
    });
    await waitFor(() => expect(mocks.connections).toHaveBeenCalledWith(1));

    await act(async () => {
      await result.current.handleViewPricingTemplateUsage(template(2));
    });
    expect(result.current.pricingTemplateUsageRows).toEqual([usageItem(2)]);

    await act(async () => {
      resolveFirst({ template_id: 1, items: [usageItem(1)] });
      await Promise.resolve();
    });
    expect(result.current.pricingTemplateUsageRows).toEqual([usageItem(2)]);
  });
});
