import { describe, expect, it, vi } from "vitest"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import {
  CUSTOM_REQUEST_PARAMETERS_MAX_COMPACT_BYTES,
  parseCustomRequestParametersDraft,
  customRequestParametersDraftFromValue,
} from "@/pages/model-detail/customRequestParameters"
import { buildConnectionDraftPayload } from "@/pages/model-detail/connectionDataSupport"
import { ConnectionCustomRequestParametersEditor } from "@/pages/model-detail/ConnectionCustomRequestParametersEditor"
import type { Connection } from "@/lib/types"

function createEditingConnection(params?: Partial<Connection>): Connection {
  return {
    id: 7,
    profile_id: 1,
    model_config_id: 42,
    api_family: "openai",
    endpoint_id: 11,
    endpoint: undefined,
    is_active: true,
    priority: 0,
    name: "editor connection",
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
    updated_at: "2026-08-08T00:00:00Z",
    ...params,
  }
}

describe("parseCustomRequestParametersDraft", () => {
  it("normalizes blank, null, and empty object to unconfigured", () => {
    for (const draft of ["", "   ", "null", "{}", "  { }  "]) {
      expect(parseCustomRequestParametersDraft(draft)).toEqual({ value: null, error: null })
    }
  })

  it("accepts typed nested values and canonicalizes the object", () => {
    const result = parseCustomRequestParametersDraft(
      '{\n  "provider": {\n    "only": ["deepinfra/turbo"],\n    "allow_fallbacks": false\n  },\n  "temperature": null,\n  "metadata": {"n": 1, "f": 1.5, "b": true, "s": "x", "arr": [1, "two", null]}\n}',
    )
    expect(result.error).toBeNull()
    expect(result.value?.provider).toEqual({ only: ["deepinfra/turbo"], allow_fallbacks: false })
    expect(result.value?.temperature).toBeNull()
    expect(result.value?.metadata).toEqual({ n: 1, f: 1.5, b: true, s: "x", arr: [1, "two", null] })
  })

  it("rejects non-object roots", () => {
    for (const draft of ["[1,2]", '"text"', "42", "true"]) {
      const result = parseCustomRequestParametersDraft(draft)
      expect(result.error?.reason).toBe("not_object")
      expect(result.error?.path).toBe("custom_request_parameters")
    }
  })

  it("rejects malformed JSON", () => {
    const result = parseCustomRequestParametersDraft('{"a": }')
    expect(result.error?.reason).toBe("not_object")
  })

  it("rejects duplicate keys", () => {
    const result = parseCustomRequestParametersDraft('{"a":1,"a":2}')
    expect(result.error?.reason).toBe("duplicate_key")
    expect(result.error?.path).toBe("custom_request_parameters.a")
  })

  it("rejects blank keys", () => {
    const result = parseCustomRequestParametersDraft('{"":1}')
    expect(result.error?.reason).toBe("blank_key")
  })

  it("rejects all nine protected top-level fields", () => {
    for (const key of ["model", "models", "stream", "messages", "input", "contents", "instructions", "system", "systemInstruction"]) {
      const result = parseCustomRequestParametersDraft(JSON.stringify({ [key]: "x" }))
      expect(result.error?.reason).toBe("protected_field")
      expect(result.error?.path).toBe(`custom_request_parameters.${key}`)
    }
  })

  it("allows protected keys nested deeper", () => {
    const result = parseCustomRequestParametersDraft('{"provider":{"model":"fine"}}')
    expect(result.error).toBeNull()
  })

  it("rejects numbers outside the safe integer range and non-finite exponents", () => {
    expect(parseCustomRequestParametersDraft('{"n":9007199254740992}').error?.reason).toBe("number_out_of_range")
    expect(parseCustomRequestParametersDraft('{"n":-9007199254740992}').error?.reason).toBe("number_out_of_range")
    expect(parseCustomRequestParametersDraft('{"n":1e999}').error?.reason).toBe("number_out_of_range")
    expect(parseCustomRequestParametersDraft('{"n":9007199254740991}').error).toBeNull()
    expect(parseCustomRequestParametersDraft('{"n":1.5}').error).toBeNull()
  })

  it("uses UTF-8 bytes for the compact-size limit", () => {
    const result = parseCustomRequestParametersDraft(`{"k":"${"é".repeat(32768)}"}`)
    expect(result.error?.reason).toBe("too_large")
  })

  it("decodes escaped keys before duplicate and protected-key checks", () => {
    const duplicate = parseCustomRequestParametersDraft('{"a":1,"\\u0061":2}')
    expect(duplicate.error?.reason).toBe("duplicate_key")
    expect(duplicate.error?.path).toBe("custom_request_parameters.a")

    const protectedKey = parseCustomRequestParametersDraft('{"\\u0073tream":true}')
    expect(protectedKey.error?.reason).toBe("protected_field")
    expect(protectedKey.error?.path).toBe("custom_request_parameters.stream")
  })

  it("rejects oversized compact encoding", () => {
    const result = parseCustomRequestParametersDraft(
      `{"k":"${"x".repeat(CUSTOM_REQUEST_PARAMETERS_MAX_COMPACT_BYTES)}"}`,
    )
    expect(result.error?.reason).toBe("too_large")
    expect(result.error?.limit).toBe(CUSTOM_REQUEST_PARAMETERS_MAX_COMPACT_BYTES)
  })

  it("rejects excessive nesting depth", () => {
    const deep = `{"a":${'{"b":'.repeat(16)}1${"}".repeat(16)}}`
    const result = parseCustomRequestParametersDraft(deep)
    expect(result.error?.reason).toBe("too_deep")
  })

  it("allows a scalar at the maximum container depth", () => {
    const boundary = `{"a":${'{"b":'.repeat(15)}1${"}".repeat(15)}}`
    expect(parseCustomRequestParametersDraft(boundary).error).toBeNull()
  })

  it("rejects excessive member counts", () => {
    const members = Array.from({ length: 257 }, (_, index) => `"k${index}":1`).join(",")
    const result = parseCustomRequestParametersDraft(`{${members}}`)
    expect(result.error?.reason).toBe("too_many_members")
  })
})

describe("customRequestParametersDraftFromValue", () => {
  it("returns empty string for unconfigured values", () => {
    expect(customRequestParametersDraftFromValue(null)).toBe("")
    expect(customRequestParametersDraftFromValue(undefined)).toBe("")
  })

  it("formats configured values with two-space indentation", () => {
    const draft = customRequestParametersDraftFromValue({ provider: { only: ["deepinfra/turbo"] } })
    expect(draft).toBe('{\n  "provider": {\n    "only": [\n      "deepinfra/turbo"\n    ]\n  }\n}')
  })
})

describe("buildConnectionDraftPayload custom request parameters", () => {
  const baseInput = {
    apiFamily: "openai" as const,
    createMode: "select" as const,
    selectedEndpointId: "11",
    newEndpointForm: { name: "", base_url: "", api_key: "" },
    connectionForm: { api_family: "openai" as const, name: "conn", is_active: true, openai_text_capability: "dual_native" as const },
    headerRows: [],
    editingConnection: null,
    endpointSourceDefaultName: null,
    customRequestParametersValue: null,
    routingScheduleValue: null,
  }

  it("sends null for unconfigured on create", () => {
    const { payload } = buildConnectionDraftPayload({ ...baseInput, customRequestParametersValue: null })
    expect(payload?.custom_request_parameters).toBeNull()
  })

  it("sends the parsed object on create", () => {
    const { payload } = buildConnectionDraftPayload({
      ...baseInput,
      customRequestParametersValue: { provider: { only: ["deepinfra/turbo"] } },
      routingScheduleValue: null,
    })
    expect(payload?.custom_request_parameters).toEqual({ provider: { only: ["deepinfra/turbo"] } })
  })

  it("sends explicit null when an edited object is cleared", () => {
    const { payload } = buildConnectionDraftPayload({
      ...baseInput,
      editingConnection: createEditingConnection(),
      customRequestParametersValue: null,
      routingScheduleValue: null,
    })
    expect(payload?.custom_request_parameters).toBeNull()
  })

  it("sends the replaced object on edit", () => {
    const { payload } = buildConnectionDraftPayload({
      ...baseInput,
      editingConnection: createEditingConnection({ custom_request_parameters: { provider: { only: ["old"] } } }),
      customRequestParametersValue: { provider: { only: ["new-provider"] }, temperature: null },
      routingScheduleValue: null,
    })
    expect(payload?.custom_request_parameters).toEqual({ provider: { only: ["new-provider"] }, temperature: null })
  })
})

describe("ConnectionCustomRequestParametersEditor", () => {
  it("shows the summary count, formats valid JSON, and surfaces field-level errors", async () => {
    const user = userEvent.setup()
    const onDraftChange = vi.fn()

    render(
      <LocaleProvider>
        <ConnectionCustomRequestParametersEditor draft='{"provider":{"only":["deepinfra/turbo"]}}' onDraftChange={onDraftChange} error={null} />
      </LocaleProvider>,
    )

    expect(screen.getByText("已配置 1 个顶层参数")).toBeTruthy()
    const textarea = screen.getByRole("textbox", { name: "自定义请求参数（JSON）" })
    expect(textarea.getAttribute("aria-invalid")).toBe("false")

    await user.click(screen.getByRole("button", { name: "格式化" }))
    expect(onDraftChange).toHaveBeenCalledWith('{\n  "provider": {\n    "only": [\n      "deepinfra/turbo"\n    ]\n  }\n}')

    await user.click(screen.getByRole("button", { name: "清空" }))
    expect(onDraftChange).toHaveBeenLastCalledWith("")
  })

  it("renders the error message with an accessible association", () => {
    render(
      <LocaleProvider>
        <ConnectionCustomRequestParametersEditor
          draft='{"model":"x"}'
          onDraftChange={() => undefined}
          error={{ reason: "protected_field", path: "custom_request_parameters.model" }}
        />
      </LocaleProvider>,
    )

    const textarea = screen.getByRole("textbox", { name: "自定义请求参数（JSON）" })
    expect(textarea.getAttribute("aria-invalid")).toBe("true")
    expect(screen.getByText("“custom_request_parameters.model”是受保护字段，不可设置。")).toBeTruthy()
  })
})
