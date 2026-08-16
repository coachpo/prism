import { describe, expect, it } from "vitest";
import { cacheReadShare, classifyTokenComponents, describeUnpricedCause } from "./pricingExplanation";

describe("describeUnpricedCause", () => {
  it("returns null for priced rows", () => {
    expect(describeUnpricedCause({ pricingStatus: "priced", unpricedReason: null, streamOutcome: "completed" })).toBeNull();
  });

  it("explains MISSING_TOKEN_USAGE on a streamed row with the injection note", () => {
    const cause = describeUnpricedCause({ pricingStatus: "unpriced", unpricedReason: "MISSING_TOKEN_USAGE", streamOutcome: "completed" });
    expect(cause).toContain("stream_options.include_usage");
  });

  it("explains MISSING_TOKEN_USAGE on a non-stream row without the stream note", () => {
    const cause = describeUnpricedCause({ pricingStatus: "unpriced", unpricedReason: "MISSING_TOKEN_USAGE", streamOutcome: "not_streaming" });
    expect(cause).not.toContain("stream_options.include_usage");
    expect(cause).toContain("usage");
  });

  it("explains truncated streams", () => {
    const cause = describeUnpricedCause({ pricingStatus: "unpriced", unpricedReason: "STREAM_USAGE_UNAVAILABLE", streamOutcome: "upstream_ended_without_terminal" });
    expect(cause).toContain("终止");
  });

  it("returns null for unknown reasons", () => {
    expect(describeUnpricedCause({ pricingStatus: "unpriced", unpricedReason: "SOME_FUTURE_REASON", streamOutcome: null })).toBeNull();
  });
});

describe("classifyTokenComponents", () => {
  it("reports unavailable when the total is null", () => {
    expect(classifyTokenComponents({ input: 1, output: 2, total: null, cacheRead: null, cacheCreation: null, reasoning: null })).toEqual({ kind: "unavailable" });
  });

  it("reports total_only when every component is null", () => {
    expect(classifyTokenComponents({ input: null, output: null, total: 1200, cacheRead: null, cacheCreation: null, reasoning: null })).toEqual({ kind: "total_only", uncategorized: 1200 });
  });

  it("reports balanced when components reconstruct the total", () => {
    expect(classifyTokenComponents({ input: 600, output: 150, total: 1200, cacheRead: 400, cacheCreation: 0, reasoning: 50 })).toEqual({ kind: "balanced" });
  });

  it("reports the residual when components fall short of the total", () => {
    expect(classifyTokenComponents({ input: 600, output: 150, total: 1300, cacheRead: 400, cacheCreation: null, reasoning: 50 })).toEqual({ kind: "residual", uncategorized: 100 });
  });
});

describe("cacheReadShare", () => {
  it("computes the normal share against the disjoint denominator", () => {
    expect(cacheReadShare({ input: 200, cacheRead: 18000, cacheCreation: 0, operationName: "anthropic.messages" })).toEqual({ kind: "value", share: 18000 / 18200 });
  });

  it("reports a real zero share when nothing was served from cache", () => {
    // A cold start with creation tokens must not read as a near-100% cache
    // hit: the numerator is cache_read alone, never read + creation.
    expect(cacheReadShare({ input: 200, cacheRead: 0, cacheCreation: 18000, operationName: "anthropic.messages" })).toEqual({ kind: "value", share: 0 });
  });

  it("separates a missing component from an incomparable operation", () => {
    expect(cacheReadShare({ input: null, cacheRead: 18000, cacheCreation: 0, operationName: "anthropic.messages" })).toEqual({ kind: "components_missing" });
    expect(cacheReadShare({ input: 200, cacheRead: null, cacheCreation: 0, operationName: "anthropic.messages" })).toEqual({ kind: "components_missing" });
  });

  it("still computes when cache_creation is null (structurally absent)", () => {
    expect(cacheReadShare({ input: 200, cacheRead: 400, cacheCreation: null, operationName: "openai.chat.completions" })).toEqual({ kind: "value", share: 400 / 600 });
  });

  it("reports count_tokens-class and image operations as incomparable", () => {
    expect(cacheReadShare({ input: 41, cacheRead: 41, cacheCreation: 3, operationName: "gemini.count_tokens" })).toEqual({ kind: "incomparable_operation" });
    expect(cacheReadShare({ input: 100, cacheRead: 100, cacheCreation: null, operationName: "anthropic.count_tokens" })).toEqual({ kind: "incomparable_operation" });
    expect(cacheReadShare({ input: 100, cacheRead: null, cacheCreation: null, operationName: "openai.images.generations" })).toEqual({ kind: "incomparable_operation" });
    expect(cacheReadShare({ input: 100, cacheRead: null, cacheCreation: null, operationName: "openai.images.edits" })).toEqual({ kind: "incomparable_operation" });
  });

  it("judges operation eligibility before the components", () => {
    // Every component is present, but the operation is known to double-count:
    // it stays incomparable rather than falling through to a value.
    expect(cacheReadShare({ input: 41, cacheRead: 41, cacheCreation: 3, operationName: "gemini.count_tokens" })).toEqual({ kind: "incomparable_operation" });
  });

  it("reports a null operation_name as indeterminate, not incomparable", () => {
    expect(cacheReadShare({ input: 200, cacheRead: 18000, cacheCreation: 0, operationName: null })).toEqual({ kind: "indeterminate_operation" });
  });

  it("reports a zero denominator separately from a missing component", () => {
    expect(cacheReadShare({ input: 0, cacheRead: 0, cacheCreation: 0, operationName: "anthropic.messages" })).toEqual({ kind: "no_prompt_tokens" });
  });
});
