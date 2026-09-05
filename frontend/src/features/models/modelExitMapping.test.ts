import { describe, expect, it } from "vitest"
import type { ManagedModelConfigListItem } from "@/lib/api/models"
import type { Connection, ModelAccessTarget, ModelConfigListItem } from "@/lib/types"
import { projectExitMapping } from "./modelExitMapping"
import { hasModelTarget, isUpstreamDecoupled } from "./modelRoutingFlags"

const ENTRY_MODEL_ID = "Entry-A"

function terminalTarget(
  id: number,
  position: number,
  options: {
    isEnabled?: boolean
    endpointName?: string | null
    upstreamModelId?: string | null
  } = {},
): ModelAccessTarget {
  const connection = {
    id: id + 900,
    profile_id: 1,
    api_family: "openai",
    endpoint_id: id + 100,
    endpoint: options.endpointName === undefined
      ? { id: id + 100, name: `endpoint-${id}`, base_url: "https://x", has_api_key: false, api_key_fingerprint: null, api_key_updated_at: null, config_revision: 1, created_at: "t", updated_at: "t" }
      : options.endpointName === null
        ? undefined
        : { id: id + 100, name: options.endpointName, base_url: "https://x", has_api_key: false, api_key_fingerprint: null, api_key_updated_at: null, config_revision: 1, created_at: "t", updated_at: "t" },
    is_active: true,
    priority: position,
    name: `conn-${id}`,
    auth_type: null,
    upstream_model_id: options.upstreamModelId === undefined ? ENTRY_MODEL_ID : options.upstreamModelId,
    custom_headers: null,
    custom_headers_redacted: null,
    custom_request_parameters: null,
    routing_schedule: null,
    routing_schedule_state: null,
    openai_text_capability: null,
    openai_image_capability: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: "t",
    updated_at: "t",
  } as unknown as Connection
  return {
    id,
    target_type: "connection",
    target_model_id: null,
    connection_id: connection.id,
    terminal_target_id: connection.id,
    position,
    is_enabled: options.isEnabled ?? true,
    target_model: null,
    connection,
    terminal_target: connection,
    created_at: "t",
    updated_at: "t",
  }
}

function modelTarget(
  id: number,
  position: number,
  options: { isEnabled?: boolean; logicalModelId?: string | null; summaryModelId?: string | null } = {},
): ModelAccessTarget {
  return {
    id,
    target_type: "model",
    target_model_id: options.logicalModelId === undefined ? "child-model" : options.logicalModelId,
    connection_id: null,
    terminal_target_id: null,
    position,
    is_enabled: options.isEnabled ?? true,
    target_model: options.summaryModelId === undefined
      ? null
      : options.summaryModelId === null
        ? null
        : ({ id: id + 500, model_id: options.summaryModelId } as unknown as ModelAccessTarget["target_model"]),
    connection: null,
    terminal_target: null,
    created_at: "t",
    updated_at: "t",
  }
}

function entryModel(accessTargets: ModelAccessTarget[]): ManagedModelConfigListItem {
  return {
    id: 1,
    profile_id: 1,
    api_family: "openai",
    model_id: ENTRY_MODEL_ID,
    display_name: null,
    openai_accepted_format: null,
    openai_image_operations: null,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: accessTargets,
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
    created_at: "t",
    updated_at: "t",
  } as unknown as ManagedModelConfigListItem
}

describe("projectExitMapping", () => {
  it("orders mixed peers by (position, id) regardless of input order", () => {
    const projection = projectExitMapping(entryModel([
      terminalTarget(23, 1),
      modelTarget(7, 0),
      terminalTarget(31, 2),
    ]))
    // 三条目标：折叠最后一条一点高度也省不下来（尾行自己就占一行），
    // 所以余量恰为 1 时直接显示。
    expect(projection.visible.map((item) => [item.accessTargetId, item.position])).toEqual([
      [7, 0],
      [23, 1],
      [31, 2],
    ])
    expect(projection.remainingCount).toBe(0)
  })

  it("folds the remainder once it is worth folding", () => {
    const projection = projectExitMapping(entryModel([
      modelTarget(7, 0),
      terminalTarget(23, 1),
      terminalTarget(31, 2),
      terminalTarget(37, 3),
    ]))
    expect(projection.visible.map((item) => item.accessTargetId)).toEqual([7, 23])
    expect(projection.remainingCount).toBe(2)
  })

  it("breaks position ties by row id", () => {
    const projection = projectExitMapping(entryModel([
      terminalTarget(42, 1),
      terminalTarget(41, 1),
    ]))
    expect(projection.visible.map((item) => item.accessTargetId)).toEqual([41, 42])
  })

  it("deduplicates repeated row ids so one physical target cannot show twice", () => {
    const duplicated = terminalTarget(11, 0)
    const projection = projectExitMapping(entryModel([duplicated, { ...duplicated }]))
    expect(projection.visible).toHaveLength(1)
    expect(projection.remainingCount).toBe(0)
  })

  it("shows terminal endpoints plus the actual upstream identity, not the entry id", () => {
    const [first] = projectExitMapping(entryModel([
      terminalTarget(11, 0, { endpointName: "OpenAI Primary", upstreamModelId: "provider/Model-X" }),
    ])).visible
    expect(first?.identity).toEqual({
      kind: "terminal",
      endpointName: "OpenAI Primary",
      upstreamModelId: "provider/Model-X",
    })
  })

  it("shows the logical target for Model Target rows", () => {
    const [first] = projectExitMapping(entryModel([modelTarget(9, 0, { summaryModelId: "child-summary" })])).visible
    expect(first?.identity).toEqual({ kind: "model", logicalModelId: "child-summary" })
    const [fallback] = projectExitMapping(entryModel([modelTarget(9, 0, { summaryModelId: null, logicalModelId: "child-raw" })])).visible
    expect(fallback?.identity).toEqual({ kind: "model", logicalModelId: "child-raw" })
  })

  it("keeps disabled rows visible and flags non-participation instead of hiding them", () => {
    const [first] = projectExitMapping(entryModel([
      terminalTarget(11, 0, { isEnabled: false }),
    ])).visible
    expect(first?.isEnabled).toBe(false)
  })

  it("renders missing upstream and endpoint evidence as null, never as the entry id", () => {
    const [first] = projectExitMapping(entryModel([
      terminalTarget(11, 0, { upstreamModelId: null }),
    ])).visible
    expect(first?.identity.kind).toBe("terminal")
    if (first?.identity.kind !== "terminal") throw new Error("unreachable")
    expect(first.identity.upstreamModelId).toBeNull()
    const [unnamed] = projectExitMapping(entryModel([
      terminalTarget(12, 0, { endpointName: null }),
    ])).visible
    if (unnamed?.identity.kind !== "terminal") throw new Error("unreachable")
    expect(unnamed.identity.endpointName).toBeNull()
  })
})

describe("identity flags", () => {
  it("treats decoupling as exact, case-sensitive comparison against the entry id", () => {
    expect(isUpstreamDecoupled(entryModel([terminalTarget(11, 0, { upstreamModelId: "Entry-A" })]))).toBe(false)
    expect(isUpstreamDecoupled(entryModel([terminalTarget(11, 0, { upstreamModelId: "entry-a" })]))).toBe(true)
    expect(isUpstreamDecoupled(entryModel([terminalTarget(11, 0, { upstreamModelId: "ENTRY-A" })]))).toBe(true)
    expect(isUpstreamDecoupled(entryModel([terminalTarget(11, 0, { upstreamModelId: "Entry-B" })]))).toBe(true)
  })

  it("never claims decoupling from missing or blank identity evidence", () => {
    expect(isUpstreamDecoupled(entryModel([terminalTarget(11, 0, { upstreamModelId: null })]))).toBe(false)
    expect(isUpstreamDecoupled(entryModel([terminalTarget(11, 0, { upstreamModelId: "" })]))).toBe(false)
    expect(isUpstreamDecoupled(entryModel([terminalTarget(11, 0, { upstreamModelId: "  " })]))).toBe(false)
  })

  it("ignores Model Target rows when judging upstream identity", () => {
    expect(isUpstreamDecoupled(entryModel([modelTarget(9, 0)]))).toBe(false)
  })

  it("detects Model Target rows for the has_model_target flag", () => {
    expect(hasModelTarget(entryModel([modelTarget(9, 0)]))).toBe(true)
    expect(hasModelTarget(entryModel([terminalTarget(11, 0)]))).toBe(false)
    expect(hasModelTarget(entryModel([]))).toBe(false)
  })
})

// Keep the detail-side list-item superset honest: the projection is consumed
// with both shapes in production, so both must typecheck against the helper.
describe("projection input shapes", () => {
  it("accepts detail ModelConfig rows identically", () => {
    const detail = { model_id: ENTRY_MODEL_ID, access_targets: [terminalTarget(11, 0)] } as unknown as ModelConfigListItem
    expect(projectExitMapping(detail).visible).toHaveLength(1)
  })
})
