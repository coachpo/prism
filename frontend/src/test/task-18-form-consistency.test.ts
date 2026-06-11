import { describe, expect, it } from "vitest"
import { ApiError } from "@/lib/api"
import { extractServerValidation, fieldErrorsFromServerValidation } from "@/shared/forms/serverValidation"
import {
  DEFAULT_SIDECAR_FORM,
  toSidecarCreatePayload,
  toSidecarUpdatePayload,
  validateSidecarForm,
} from "@/features/sidecars/sidecarFormState"
import { getStaticMessages } from "@/i18n/staticMessages"

describe("Task 18 server validation helpers", () => {
  it("keeps routing plan path and code context visible", () => {
    const validation = extractServerValidation(new ApiError("conflict", 409, {
      detail: {
        routing_plan_issues: [
          { path: "access_targets.0.target_model_id", code: "missing_model", message: "Target model is gone" },
        ],
      },
    }), "fallback")

    expect(validation.summary).toBe("access_targets.0.target_model_id (missing_model): Target model is gone")
    expect(validation.issues).toEqual([
      {
        code: "missing_model",
        field: "access_targets.0.target_model_id",
        message: "Target model is gone",
      },
    ])
  })

  it("maps server field errors for inline form display", () => {
    const validation = extractServerValidation(new ApiError("bad request", 400, {
      detail: {
        errors: [
          { field: "base_url", message: "Base URL is not reachable" },
          { field: "name", message: "Name already exists" },
        ],
      },
    }), "fallback")

    expect(fieldErrorsFromServerValidation(validation, ["name", "base_url"] as const)).toEqual({
      base_url: "Base URL is not reachable",
      name: "Name already exists",
    })
  })
})

describe("Task 18 sidecar schema-backed payloads", () => {
  const copy = getStaticMessages().sidecarsPage

  it("requires exact create fields before building backend payloads", () => {
    const validation = validateSidecarForm({
      ...DEFAULT_SIDECAR_FORM,
      name: "",
      base_url: "not a url",
      management_password: "",
      sync_interval_seconds: "0",
    }, { editing: false })

    expect(validation.values).toBeNull()
    expect(validation.errors.name).toBe("Name is required")
    expect(validation.errors.base_url).toBeTruthy()
    expect(validation.errors.management_password).toBe("Management password is required")
    expect(validation.errors.sync_interval_seconds).toBe("Must be a positive whole number")
  })

  it("trims create payloads and omits blank update passwords", () => {
    expect(toSidecarCreatePayload({
      ...DEFAULT_SIDECAR_FORM,
      name: "  local sidecar ",
      base_url: " https://sidecar.test ",
      environment_label: "  lab ",
      management_password: " secret ",
    }, copy)).toMatchObject({
      name: "local sidecar",
      base_url: "https://sidecar.test",
      environment_label: "lab",
      management_password: "secret",
      sync_interval_seconds: 300,
      request_timeout_seconds: 30,
    })

    expect(toSidecarUpdatePayload({
      ...DEFAULT_SIDECAR_FORM,
      name: "local sidecar",
      base_url: "https://sidecar.test",
      management_password: "   ",
    }, copy)).toEqual({
      name: "local sidecar",
      base_url: "https://sidecar.test",
      enabled: true,
      environment_label: null,
      sync_interval_seconds: 300,
      request_timeout_seconds: 30,
      allow_private_network: false,
      allow_insecure_http: false,
      skip_tls_verify: false,
    })
  })
})
