import { describe, expect, it } from "vitest";

import { getStatusIntent, getStatusTone } from "./requestLogStatus";

describe("request-log status presentation", () => {
  it.each([
    [200, "healthy", "border-l-healthy bg-healthy/5"],
    [404, "degraded", "border-l-degraded bg-degraded/5"],
    [500, "failing", "border-l-failing bg-failing/5"],
  ])("keeps the existing intent and tone for %s", (statusCode, intent, card) => {
    expect(getStatusIntent(statusCode)).toBe(intent);
    expect(getStatusTone(statusCode)).toEqual({ card });
  });
});
