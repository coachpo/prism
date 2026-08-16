import { describe, expect, it } from "vitest";
import { parseRetryAfter } from "./core";

describe("parseRetryAfter", () => {
  it("parses delay-seconds", () => {
    expect(parseRetryAfter("30")).toBe(30000);
    expect(parseRetryAfter("0")).toBe(0);
    expect(parseRetryAfter(" 12 ")).toBe(12000);
  });

  it("parses HTTP-date and rejects garbage", () => {
    const now = new Date("2026-08-09T12:00:00Z");
    expect(parseRetryAfter("Sun, 09 Aug 2026 12:00:30 GMT", now)).toBe(30000);
    expect(parseRetryAfter("not-a-date", now)).toBeNull();
    expect(parseRetryAfter(null, now)).toBeNull();
    expect(parseRetryAfter("", now)).toBeNull();
  });
});
