import { describe, expect, it } from "vitest"
import { ApiError } from "@/lib/api"
import { extractServerValidation, fieldErrorsFromServerValidation } from "@/shared/forms/serverValidation"

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
