import { describe, expect, it } from "vitest";

import { readSpendingRetentionClip } from "./metricsCoverage";
import type { StatsCoverageByDataset } from "@/lib/types/model-stats";

/** 线上 /api/stats/models/metrics 的真实形状：coverage 按数据集嵌套。 */
const clippedSpending: StatsCoverageByDataset = {
  usage_request_events: {
    requested_preset: "30d",
    from_time: "2026-09-02T01:49:51Z",
    to_time: "2026-09-05T12:00:00Z",
    retention_from_time: "2026-09-02T01:39:40Z",
    source: "raw",
    complete: false,
    gaps: [
      {
        from_time: "2026-08-06T12:50:00Z",
        to_time: "2026-09-02T01:39:00Z",
        reason: "retention_deleted",
      },
      {
        from_time: "2026-09-02T01:39:00Z",
        to_time: "2026-09-02T01:49:51Z",
        reason: "actual_coverage_unavailable",
      },
    ],
  },
};

describe("readSpendingRetentionClip", () => {
  it("reads the nested dataset record rather than the parent", () => {
    expect(readSpendingRetentionClip({ spending: clippedSpending })).toEqual({
      retentionFrom: "2026-09-02T01:39:40Z",
    });
  });

  it("falls back to the latest retention gap end when retention_from_time is absent", () => {
    const spending: StatsCoverageByDataset = {
      usage_request_events: {
        ...clippedSpending.usage_request_events!,
        retention_from_time: null,
      },
    };
    expect(readSpendingRetentionClip({ spending })).toEqual({
      retentionFrom: "2026-09-02T01:39:00Z",
    });
  });

  it("ignores incompleteness that retention did not cause", () => {
    const spending: StatsCoverageByDataset = {
      usage_request_events: {
        ...clippedSpending.usage_request_events!,
        gaps: [
          {
            from_time: "2026-09-02T01:39:00Z",
            to_time: "2026-09-02T01:49:51Z",
            reason: "actual_coverage_unavailable",
          },
        ],
      },
    };
    expect(readSpendingRetentionClip({ spending })).toBeNull();
  });

  it("returns null for complete coverage and for a missing payload", () => {
    const spending: StatsCoverageByDataset = {
      usage_request_events: {
        ...clippedSpending.usage_request_events!,
        complete: true,
      },
    };
    expect(readSpendingRetentionClip({ spending })).toBeNull();
    expect(readSpendingRetentionClip({ spending: {} })).toBeNull();
    expect(readSpendingRetentionClip(null)).toBeNull();
    expect(readSpendingRetentionClip(undefined)).toBeNull();
  });
});
