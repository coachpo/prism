import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, overloadRetryDelayMs, parseRetryAfter, request } from "./request";

afterEach(() => vi.unstubAllGlobals());

describe("management error presentation", () => {
  it("preserves server evidence without exposing it as the displayed message", async () => {
    const detail = { code: "database_unavailable", detail: "sql: failed at /internal/database.go:42" };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(detail), { status: 503 })));
    await expect(request("/api/endpoints")).rejects.toMatchObject({
      status: 503, code: "database_unavailable", detail,
      message: "Prism 暂时无法完成操作。请稍后重试；若刚才保存过内容，请先刷新确认结果。",
    });
  });

  it.each(["network", "invalid-response", "body-read"])("offers recovery for %s failures", async (failure) => {
    vi.stubGlobal("fetch", failure === "network"
      ? vi.fn().mockRejectedValue(new TypeError("Failed to fetch /internal/route"))
      : vi.fn().mockResolvedValue(failure === "body-read"
        ? new Response(new ReadableStream({ start(controller) { controller.error(new TypeError("Failed to read /internal/route")); } }))
        : new Response("<html>internal stack</html>")));
    const error = await request("/api/endpoints").catch(error => error);
    expect(error).toBeInstanceOf(ApiError);
    if (!(error instanceof ApiError)) throw new Error("Expected a recoverable API error");
    expect(error.message).toContain("请");
    expect(error.message).not.toMatch(/internal|Failed to fetch|html/);
  });
});

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
