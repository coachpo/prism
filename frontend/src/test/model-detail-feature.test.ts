import { describe, expect, it } from "vitest"
import { moveConnectionInList } from "@/pages/model-detail/useModelDetailDataSupport"
import { modelDetailQueryKeys } from "@/features/models/detail/queryKeys"
import { modelDetailSearchSchema, normalizeModelDetailTab } from "@/features/models/detail/modelDetailSchemas"
import type { Connection } from "@/lib/types"

function createConnection(id: number, priority: number, name: string): Connection {
  return {
    id,
    profile_id: 1,
    model_config_id: 42,
    api_family: "openai",
    endpoint_id: id + 100,
    endpoint: undefined,
    is_active: true,
    priority,
    name,
    auth_type: null,
    custom_headers: null,
    openai_text_capability: "responses_only",
    openai_probe_endpoint_variant: "responses_minimal",
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    health_status: "unknown",
    health_detail: null,
    last_health_check: null,
    created_at: "2026-06-11T00:00:00Z",
    updated_at: "2026-06-11T00:00:00Z",
  }
}

describe("model detail feature contracts", () => {
  it("keeps Default profile and model id in detail query keys", () => {
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

  it("optimistic reorder helper resequences priorities and preserves rollback input", () => {
    const previous = [
      createConnection(501, 0, "Primary"),
      createConnection(502, 1, "Secondary"),
      createConnection(503, 2, "Tertiary"),
    ]

    const next = moveConnectionInList(previous, 0, 2)

    expect(next.map((connection) => [connection.id, connection.priority])).toEqual([
      [502, 0],
      [503, 1],
      [501, 2],
    ])
    expect(previous.map((connection) => [connection.id, connection.priority])).toEqual([
      [501, 0],
      [502, 1],
      [503, 2],
    ])
  })
})
