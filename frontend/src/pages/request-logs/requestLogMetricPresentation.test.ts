import { describe, expect, it } from "vitest";

import {
  formatCost,
  formatTokenRate,
  formatTokens,
  formatTtft,
} from "./requestLogMetricPresentation";

describe("request-log metric presentation", () => {
  it("keeps absent values honest across table and detail consumers", () => {
    expect(formatCost(null, null)).toBe("—");
    expect(formatTokens(null)).toBe("—");
    expect(formatTtft(null)).toBe("—");
    expect(formatTokenRate(null, 100, 500)).toBe("—");
  });

  it("preserves TTFT and decode-rate units", () => {
    expect(formatTtft(120)).toBe("120ms");
    expect(formatTokenRate(800, 200, 1_000)).toBe("1,000.0 tok/s");
  });
});
