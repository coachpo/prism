import { describe, expect, it, vi } from "vitest"
import { ApiError } from "@/lib/api"
import { fetchSetupReadiness, parseModelReadiness, setupCollapseDecision, type SetupReadinessSources } from "./setupCoordinator"

const axis = (state: "ready" | "not_ready" | "unknown" | "not_required") => ({ state, reason_codes: [] })

function sources(overrides: Partial<SetupReadinessSources> = {}): SetupReadinessSources {
  return {
    endpoints: async () => [{ id: 1 }],
    routing: async () => [{ id: 1, is_default: true }],
    models: async () => ({
      items: [],
      route_readiness: {
        route_witness_generation: "7",
        configuration: axis("ready"),
        application: axis("ready"),
        configuration_ready_model_count: 1,
        route_ready_model_count: 1,
        route_witness_count: 1,
        representative_witness: null,
      },
    }),
    pricing: async () => ({
      evaluated_route_witness_generation: "7",
      pricing_template_generation: 1,
      pricing_reference_generation: 1,
      configuration: axis("ready"),
      application: axis("ready"),
      route_witness_count: 1,
      applied_witness_count: 1,
      cost_ready_witness_count: 1,
      cost_ready: true,
      representative_matching: null,
    }),
    proxyKeys: async () => ({
      evaluated_route_witness_generation: "7",
      proxy_key_owner_revision: "1:",
      configuration: axis("ready"),
      application: axis("ready"),
      route_witness_count: 1,
      matching_witness_count: 1,
      optional_attribution_witness_count: null,
      representative_matching: null,
      representative_optional_attribution: null,
    }),
    ...overrides,
  }
}

describe("setup coordinator", () => {
  it("produces a fresh 4/4 snapshot without treating the action row as a hard item", async () => {
    const result = await fetchSetupReadiness("enabled", { sources: sources() })
    expect(result.phase).toBe("fresh")
    expect(result.route_configured_count).toBe(4)
    expect(result.facts.map((fact) => fact.id)).toEqual([
      "endpoints",
      "pricing",
      "routing",
      "models",
      "terminal_targets",
      "proxy_keys",
      "runtime_self_test",
    ])
    expect(result.facts.at(-1)?.result).toBeNull()
  })

  it("keeps the count unknown when the model snapshot is malformed and does not fan out", async () => {
    const pricing = vi.fn(async () => ({}))
    const proxyKeys = vi.fn(async () => ({}))
    const result = await fetchSetupReadiness("enabled", {
      sources: sources({ models: async () => ({ items: [] }), pricing, proxyKeys }),
    })
    expect(result.phase).toBe("unknown")
    expect(result.route_configured_count).toBeNull()
    expect(pricing).not.toHaveBeenCalled()
    expect(proxyKeys).not.toHaveBeenCalled()
    expect(result.facts.find((fact) => fact.id === "pricing")?.fetch_quality).toBe("unknown")
  })

  it("keeps independent facts visible while one owner read is degraded", async () => {
    const result = await fetchSetupReadiness("enabled", {
      sources: sources({ endpoints: async () => { throw new Error("endpoint unavailable") } }),
    })
    expect(result.phase).toBe("degraded")
    expect(result.route_configured_count).toBeNull()
    expect(result.facts.find((fact) => fact.id === "endpoints")).toMatchObject({ fetch_quality: "error", result: null })
    expect(result.facts.find((fact) => fact.id === "models")).toMatchObject({ fetch_quality: "fresh", result: "complete" })
  })

  it("retries only generation mismatches, at most twice after the initial attempt", async () => {
    let pricingCalls = 0
    const result = await fetchSetupReadiness("enabled", {
      sources: sources({
        pricing: async () => {
          pricingCalls += 1
          throw new ApiError("route_witness_generation_changed", 409, null, null, "route_witness_generation_changed")
        },
      }),
    })
    expect(pricingCalls).toBe(3)
    expect(result.phase).toBe("unknown")
    expect(result.route_configured_count).toBe(4)
    expect(result.error).toContain("发生变化")
  })

  it("maps auth-disabled access to skipped instead of complete even when no generation exists", async () => {
    const result = await fetchSetupReadiness("disabled", {
      sources: sources({
        models: async () => ({
          items: [],
          route_readiness: {
            route_witness_generation: null,
            configuration: axis("unknown"),
            application: axis("unknown"),
            configuration_ready_model_count: null,
            route_ready_model_count: null,
            route_witness_count: null,
            representative_witness: null,
          },
        }),
      }),
    })
    const proxy = result.facts.find((fact) => fact.id === "proxy_keys")
    expect(proxy).toMatchObject({ result: "skipped", fetch_quality: "fresh" })
  })

  it("collapses once per fresh-4/4 transition without stealing focus", () => {
    expect(setupCollapseDecision({ wasReady: false, isReady: true, focusedInside: false, manuallyReopened: false })).toBe("collapse")
    expect(setupCollapseDecision({ wasReady: false, isReady: true, focusedInside: true, manuallyReopened: false })).toBe("wait-until-blur")
    expect(setupCollapseDecision({ wasReady: true, isReady: true, focusedInside: false, manuallyReopened: false })).toBe("keep-open")
    expect(setupCollapseDecision({ wasReady: false, isReady: true, focusedInside: false, manuallyReopened: true })).toBe("keep-open")
    expect(setupCollapseDecision({ wasReady: true, isReady: false, focusedInside: false, manuallyReopened: false })).toBe("expand")
  })

  it("rejects a model profile that claims a non-proxy readiness state", () => {
    expect(parseModelReadiness({
      items: [],
      route_readiness: {
        route_witness_generation: "1",
        configuration: axis("not_required"),
        application: axis("ready"),
        configuration_ready_model_count: 0,
        route_ready_model_count: 0,
        route_witness_count: 0,
        representative_witness: null,
      },
    })).toBeNull()
  })
})
