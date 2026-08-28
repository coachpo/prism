import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Connection } from "@/lib/types";
import { useModelDetailConnectionReconciliation } from "./useModelDetailConnectionReconciliation";

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

const connection: Connection = {
  id: 15,
  profile_id: 1,
  model_config_id: 7,
  api_family: "openai",
  endpoint_id: 3,
  is_active: true,
  priority: 0,
  name: "Primary",
  auth_type: null,
  custom_headers: null,
  custom_headers_redacted: null,
  custom_request_parameters: null,
  routing_schedule: null,
  routing_schedule_state: null,
  openai_text_capability: "dual_native",
  openai_image_capability: null,
  pricing_template_id: null,
  qps_limit: null,
  max_in_flight_non_stream: null,
  max_in_flight_stream: null,
  pricing_template: null,
  created_at: "2026-08-28T00:00:00Z",
  updated_at: "2026-08-28T00:00:00Z",
};

describe("useModelDetailConnectionReconciliation", () => {
  it("refreshes the authoritative model-list projection after a mutation", async () => {
    const refreshModels = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() =>
      useModelDetailConnectionReconciliation({
        modelConfigId: 7,
        pricingTemplates: [],
        setConnections: vi.fn(),
        setAllConnections: vi.fn(),
        setGlobalEndpoints: vi.fn(),
        refreshModels,
      }),
    );

    act(() => {
      expect(result.current.commitConnection(connection)?.id).toBe(15);
    });
    await vi.waitFor(() => expect(refreshModels).toHaveBeenCalledOnce());
  });
});
