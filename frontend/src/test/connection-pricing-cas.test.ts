import { describe, expect, it } from "vitest"
import { buildConnectionUpdatePayload } from "@/pages/model-detail/connectionDataSupport"
import type { Connection, ConnectionCreate } from "@/lib/types"

function createConnection(params?: Partial<Connection>): Connection {
  return {
    id: 7,
    profile_id: 1,
    model_config_id: 10,
    api_family: "openai",
    endpoint_id: 11,
    endpoint: undefined,
    is_active: true,
    priority: 0,
    name: "openrouter link",
    auth_type: null,
    upstream_model_id: null,
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
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-12T09:30:00.123456Z",
    ...params,
  }
}

function createDraftPayload(params?: Partial<ConnectionCreate>): ConnectionCreate {
  return {
    api_family: "openai",
    endpoint_id: 11,
    name: "openrouter link",
    is_active: true,
    custom_headers: null,
    custom_request_parameters: null,
    routing_schedule: null,
    openai_text_capability: "dual_native",
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    ...params,
  }
}

describe("buildConnectionUpdatePayload pricing CAS", () => {
  it("omits pricing_template_id when the draft keeps the current unpriced reference", () => {
    const payload = buildConnectionUpdatePayload(createDraftPayload({ name: "renamed" }), createConnection())

    expect("pricing_template_id" in payload).toBe(false)
    expect(payload.expected_connection_updated_at).toBeUndefined()
    expect(payload.expected_pricing_template_id).toBeUndefined()
    expect(payload.name).toBe("renamed")
  })

  it("omits pricing_template_id when the draft keeps the current template", () => {
    const payload = buildConnectionUpdatePayload(
      createDraftPayload({ pricing_template_id: 3, is_active: false }),
      createConnection({ pricing_template_id: 3 }),
    )

    expect("pricing_template_id" in payload).toBe(false)
    expect(payload.is_active).toBe(false)
  })

  it("sends both CAS fields when the draft assigns a template", () => {
    const payload = buildConnectionUpdatePayload(
      createDraftPayload({ pricing_template_id: 5 }),
      createConnection(),
    )

    expect(payload.pricing_template_id).toBe(5)
    expect(payload.expected_connection_updated_at).toBe("2026-08-12T09:30:00.123456Z")
    expect(payload.expected_pricing_template_id).toBeNull()
  })

  it("sends both CAS fields when the draft clears an assigned template", () => {
    const payload = buildConnectionUpdatePayload(
      createDraftPayload({ pricing_template_id: null }),
      createConnection({ pricing_template_id: 5, updated_at: "2026-08-12T10:00:00Z" }),
    )

    expect(payload.pricing_template_id).toBeNull()
    expect(payload.expected_connection_updated_at).toBe("2026-08-12T10:00:00Z")
    expect(payload.expected_pricing_template_id).toBe(5)
  })

  it("sends both CAS fields when the draft switches templates", () => {
    const payload = buildConnectionUpdatePayload(
      createDraftPayload({ pricing_template_id: 9 }),
      createConnection({ pricing_template_id: 5 }),
    )

    expect(payload.pricing_template_id).toBe(9)
    expect(payload.expected_pricing_template_id).toBe(5)
  })
})
