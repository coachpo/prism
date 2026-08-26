import { describe, expect, it } from "vitest";
import { overloadRetryDelayMs, parseRetryAfter } from "./request";

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
describe("overloadRetryDelayMs", () => {
  it("waits as long as the server asked", () => {
    expect(overloadRetryDelayMs(1000, 0, 0)).toBe(1000);
  });

  it("clamps an absurd Retry-After into the bounded window", () => {
    expect(overloadRetryDelayMs(10, 0, 0)).toBe(250);
    expect(overloadRetryDelayMs(600_000, 0, 0)).toBe(2000);
  });

  it("does not replay a 503 the server never marked transient", () => {
    expect(overloadRetryDelayMs(null, 0, 0)).toBeNull();
  });

  it("spreads concurrent replays instead of firing them in one instant", () => {
    expect(overloadRetryDelayMs(1000, 0, 1)).toBe(1500);
    expect(overloadRetryDelayMs(1000, 0, 0.5)).toBe(1250);
  });

  it("stops replaying at the limit so an overloaded server is not hammered", () => {
    expect(overloadRetryDelayMs(1000, 1, 0)).toBe(1000);
    expect(overloadRetryDelayMs(1000, 2, 0)).toBeNull();
    expect(overloadRetryDelayMs(1000, 9, 0)).toBeNull();
  });
});
