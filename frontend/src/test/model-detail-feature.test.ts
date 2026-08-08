import { describe, expect, it } from "vitest"
import { buildAccessTargetSummary } from "@/pages/model-detail/useModelDetailDataSupport"
import { modelDetailQueryKeys } from "@/features/models/detail/queryKeys"
import { modelDetailSearchSchema, normalizeModelDetailTab } from "@/features/models/detail/modelDetailSchemas"
import type { ModelAccessTarget, ModelConfig } from "@/lib/types"

function createTarget(overrides: Partial<ModelAccessTarget> & { id: number; position: number }): ModelAccessTarget {
  return {
    target_type: "connection",
    target_model_id: null,
    connection_id: null,
    terminal_target_id: null,
    is_enabled: true,
    target_model: null,
    connection: null,
    terminal_target: null,
    created_at: "2026-06-11T00:00:00Z",
    updated_at: "2026-06-11T00:00:00Z",
    ...overrides,
  }
}

function createModel(accessTargets: ModelAccessTarget[]): ModelConfig {
  return {
    id: 42,
    profile_id: 1,
    api_family: "openai",
    model_id: "router",
    display_name: "Router",
    openai_accepted_format: "dual_native",
    loadbalance_strategy_id: 21,
    loadbalance_strategy: null,
    access_targets: accessTargets,
    is_enabled: true,
    created_at: "2026-06-11T00:00:00Z",
    updated_at: "2026-06-11T00:00:00Z",
  }
}

describe("model detail feature contracts", () => {
  it("keeps the pinned profile and model id in detail query keys", () => {
    expect(modelDetailQueryKeys.detail(1, 42)).toEqual([
      "rewrite",
      "selected-profile",
      "1",
      "models",
      "detail",
      "42",
    ])
    expect(modelDetailQueryKeys.tab(null, "42", "events")).toEqual([
      "rewrite",
      "selected-profile",
      "none",
      "models",
      "detail",
      "42",
      "tab",
      "events",
    ])
  })

  it("normalizes bookmarkable tab search without accepting stale values", () => {
    expect(modelDetailSearchSchema.parse({ tab: "events" })).toEqual({ tab: "events" })
    expect(modelDetailSearchSchema.parse({ tab: "stale" })).toEqual({ tab: "connections" })
    expect(normalizeModelDetailTab(undefined)).toBe("connections")
  })

  it("reports only one enabled authored-order first target across both types", () => {
    const model = createModel([
      createTarget({
        id: 501,
        target_type: "connection",
        connection_id: 901,
        terminal_target_id: 901,
        position: 0,
        is_enabled: true,
        connection: {
          id: 901,
          profile_id: 1,
          api_family: "openai",
          endpoint_id: 11,
          is_active: true,
          priority: 0,
          name: "Terminal A",
          auth_type: null,
          custom_headers: null,
          openai_text_capability: "dual_native",
          pricing_template_id: null,
          qps_limit: null,
          max_in_flight_non_stream: null,
          max_in_flight_stream: null,
          pricing_template: null,
          created_at: "2026-06-11T00:00:00Z",
          updated_at: "2026-06-11T00:00:00Z",
        },
      }),
      createTarget({
        id: 502,
        target_type: "model",
        target_model_id: "child",
        position: 1,
        is_enabled: true,
        target_model: { id: 7, profile_id: 1, api_family: "openai", model_id: "child", display_name: "Child", openai_accepted_format: "dual_native", loadbalance_strategy_id: 21, is_enabled: true },
      }),
      createTarget({
        id: 503,
        target_type: "connection",
        connection_id: 902,
        terminal_target_id: 902,
        position: 2,
        is_enabled: false,
        connection: null,
      }),
    ])

    const summary = buildAccessTargetSummary(model)
    expect(summary.totalTargetCount).toBe(3)
    expect(summary.enabledTargetCount).toBe(2)
    expect(summary.enabledModelFallbackTargetCount).toBe(1)
    expect(summary.enabledTerminalTargetCount).toBe(1)
    // The enabled authored-order first row is the terminal at position 0, not
    // a per-type “first model target” claim.
    expect(summary.firstEnabledTargetLabel).toBe("Terminal A")
  })
})
