import { describe, expect, it } from "vitest"
import { moveConnectionInList } from "@/pages/model-detail/useModelDetailDataSupport"
import { modelDetailQueryKeys } from "@/features/models/detail/queryKeys"
import { modelDetailSearchSchema, normalizeModelDetailSearch } from "@/features/models/detail/modelDetailSchemas"
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
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
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
  })

  it("normalizes dead tab search away and keeps one-shot parameters only", () => {
    // Old ?tab=connections|events URLs are stripped to the canonical route.
    expect(modelDetailSearchSchema.parse({ tab: "events" })).toEqual({})
    expect(modelDetailSearchSchema.parse({ tab: "connections" })).toEqual({})
    expect(modelDetailSearchSchema.parse({ tab: "stale" })).toEqual({})
    expect(normalizeModelDetailSearch({ tab: "events" })).toEqual({})
    // Supported one-shot parameters survive; endpoint_id is only legal with
    // the create-terminal-target action.
    expect(modelDetailSearchSchema.parse({ action: "create-terminal-target", endpoint_id: "3", focus_connection_id: "15" })).toEqual({
      action: "create-terminal-target",
      endpoint_id: "3",
      focus_connection_id: "15",
    })
    expect(modelDetailSearchSchema.parse({ focus_connection_id: "15" })).toEqual({ focus_connection_id: "15" })
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
