import { describe, expect, it } from "vitest";

import type { UsageSummaryResponse } from "@/lib/api/observability";
import { unmeasuredOutputRateCount } from "./outputRateCensus";

describe("Window KPI output-rate census", () => {
  it("counts every non-measured request, including not-applicable operations", () => {
    const summary = {
      output_rate_state_counts: {
        measured: 2,
        unmeasurable: 3,
        not_applicable: 4,
        unknown: 1,
      },
    } as UsageSummaryResponse;

    expect(unmeasuredOutputRateCount(summary)).toBe(8);
  });
});
