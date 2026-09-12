import { describe, expect, it } from "vitest"
import { ApiError } from "@/lib/api"
import { extractServerValidation, fieldErrorsFromServerValidation } from "@/shared/forms/serverValidation"

describe("server validation presentation", () => {
  it("keeps field identity available for forms without exposing server diagnostics", () => {
    const validation = extractServerValidation(new ApiError("conflict", 409, {
      detail: {
        routing_plan_issues: [
          { path: "access_targets.0.target_model_id", code: "missing_model", message: "Target model is gone" },
        ],
      },
    }), "fallback")

    expect(validation.summary).toBe("填写内容未被接受。请检查表单中标记的项目后重新保存。")
    expect(validation.issues).toEqual([
      {
        code: "missing_model",
        field: "access_targets.0.target_model_id",
        message: "此项内容未被接受，请检查后重新填写。",
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
      base_url: "此项内容未被接受，请检查后重新填写。",
      name: "此项内容未被接受，请检查后重新填写。",
    })
  })

  it("does not misreport an unavailable save as invalid input", () => {
    const validation = extractServerValidation(new ApiError("sql failed", 503, { detail: "sql: /internal/database.go" }), "保存失败")
    expect(validation.issues).toEqual([])
    expect(validation.summary).toContain("Prism 暂时无法完成操作")
    expect(validation.summary).not.toMatch(/sql|internal|server:/)
  })
})
