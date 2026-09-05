import { describe, expect, it } from "vitest"
import { classifyOpenAICoverage, openaiAcceptedOperationSet, openaiTargetSupportedOperationSet } from "./classifyOpenAICoverage"

describe("classifyOpenAICoverage", () => {
  it("classifies the directional 3x3 matrix", () => {
    const cases: Array<{ format: "dual_native" | "chat_completions_only" | "responses_only"; capability: "dual_native" | "chat_completions_only" | "responses_only"; want: "full" | "partial" | "none" }> = [
      { format: "chat_completions_only", capability: "chat_completions_only", want: "full" },
      { format: "chat_completions_only", capability: "responses_only", want: "none" },
      { format: "chat_completions_only", capability: "dual_native", want: "full" },
      { format: "responses_only", capability: "chat_completions_only", want: "none" },
      { format: "responses_only", capability: "responses_only", want: "full" },
      { format: "responses_only", capability: "dual_native", want: "full" },
      { format: "dual_native", capability: "chat_completions_only", want: "partial" },
      { format: "dual_native", capability: "responses_only", want: "partial" },
      { format: "dual_native", capability: "dual_native", want: "full" },
    ]
    for (const test of cases) {
      const result = classifyOpenAICoverage(test.format, test.capability)
      expect(result.coverage, `${test.format} x ${test.capability}`).toBe(test.want)
    }
  })

  it("treats an absent mode as serving no text operation", () => {
    // An image-only owner declares no accepted format. Reading that as
    // dual_native produced a bogus "partial" badge on a target that is in fact
    // fully consistent with its owner.
    expect(openaiAcceptedOperationSet(null)).toEqual([])
    expect(openaiTargetSupportedOperationSet(null)).toEqual([])
    expect(classifyOpenAICoverage(null, null).coverage).toBe("full")
    expect(classifyOpenAICoverage(null, "responses_only").coverage).toBe("full")
    expect(classifyOpenAICoverage("responses_only", null).coverage).toBe("none")
  })

  it("groups the responses family into one operation set", () => {
    const responses = openaiTargetSupportedOperationSet("responses_only")
    expect(responses).toEqual(["openai.responses", "openai.responses.input_tokens", "openai.responses.compact"])
    const dual = openaiAcceptedOperationSet("dual_native")
    expect(dual).toHaveLength(4)
  })

  it("reports the missing accepted operations", () => {
    const result = classifyOpenAICoverage("dual_native", "chat_completions_only")
    expect(result.unsupportedAcceptedOperations).toEqual(["openai.responses", "openai.responses.input_tokens", "openai.responses.compact"])
  })
})
