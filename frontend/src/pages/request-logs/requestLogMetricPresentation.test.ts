import { describe, expect, it } from "vitest";

import {
  formatCost,
  formatTokenRate,
  formatTokens,
  formatTtft,
  tokenRateMissingReason,
} from "./requestLogMetricPresentation";

const reasonLabels: Record<string, string> = {
  unmeasurable_output_span_below_threshold: "跨度不足",
  unknown_missing_evidence: "历史未记录",
  not_applicable: "不适用",
  unknown: "历史未记录",
};

describe("request-log metric presentation", () => {
  it("keeps absent values honest across table and detail consumers", () => {
    expect(formatCost(null, null)).toBe("—");
    expect(formatTokens(null)).toBe("—");
    expect(formatTtft(null)).toBe("—");
    expect(formatTokenRate(null, "unknown")).toBe("—");
  });

  it("renders only the backend-authoritative measured rate", () => {
    // Measured evidence renders the persisted rate; the browser never
    // recomputes tok/s from tokens and durations.
    expect(formatTokenRate(1000, "measured")).toBe("1,000.0 tok/s");
    // A measured zero is a real zero, not a missing value.
    expect(formatTokenRate(0, "measured")).toBe("0.0 tok/s");
  });

  it("keeps unmeasured evidence missing regardless of any numeric value", () => {
    // Non-measured states must never render a rate even if a number is
    // present — this is what keeps GLM-style buffered bursts out of the UI.
    expect(formatTokenRate(53000, "unmeasurable")).toBe("—");
    expect(formatTokenRate(51000, "not_applicable")).toBe("—");
    expect(formatTokenRate(44500, "unknown")).toBe("—");
    expect(formatTokenRate(null, "measured")).toBe("—");
    expect(formatTokenRate(800, null)).toBe("—");
  });

  it("explains missing rates with the persisted reason", () => {
    expect(
      tokenRateMissingReason(
        {
          rateTps: null,
          state: "unmeasurable",
          reason: "unmeasurable_output_span_below_threshold",
        },
        reasonLabels,
        "fallback",
      ),
    ).toBe("跨度不足");
    expect(
      tokenRateMissingReason(
        { rateTps: null, state: "unknown", reason: "unknown_missing_evidence" },
        reasonLabels,
        "fallback",
      ),
    ).toBe("历史未记录");
    // A state without a reason falls back to the state-level explanation.
    expect(
      tokenRateMissingReason(
        { rateTps: null, state: "not_applicable", reason: null },
        reasonLabels,
        "fallback",
      ),
    ).toBe("不适用");
    expect(
      tokenRateMissingReason(
        { rateTps: null, state: "unknown", reason: null },
        reasonLabels,
        "fallback",
      ),
    ).toBe("历史未记录");
  });
});
