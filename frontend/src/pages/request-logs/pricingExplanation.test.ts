import { describe, expect, it } from "vitest";
import { classifyTokenComponents, describeUnpricedCause } from "./pricingExplanation";

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
