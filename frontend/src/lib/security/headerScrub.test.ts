import { describe, expect, it } from "vitest";
import { headerNameSensitive, headerValueSensitive, scrubHeaderMap, scrubHeaderValue } from "./headerScrub";

describe("headerScrub", () => {
  it("matches the backend sensitive name set", () => {
    const sensitive = [
      "Authorization",
      "authorization",
      "Cookie",
      "Set-Cookie",
      "Proxy-Authorization",
      "X-Api-Key",
      "x-goog-api-key",
      "X-AMZ-Security-Token",
      "X-Token",
      "Access-Token",
      "Client-Secret",
      "X-Client-Secret",
      "X-RapidAPI-Key",
      "Private-Key",
      "X-PayPal-Client-Secret",
    ];
    const safe = ["Content-Type", "Accept", "X-Request-Id", "User-Agent", "Traceparent", "X-Profile-Id"];
    for (const name of sensitive) {
      expect(headerNameSensitive(name)).toBe(true);
    }
    for (const name of safe) {
      expect(headerNameSensitive(name)).toBe(false);
    }
  });

  it("scans credential-shaped values on non-sensitive names", () => {
    const sensitiveValues = [
      "Bearer sk-abc123XYZ890",
      "bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.signature",
      "Basic dXNlcjpwYXNzd29yZA==",
      "token=sekritvalue42; path=/",
      "password=hunter2",
      "AKIAIOSFODNN7EXAMPLE",
      "sk-proj-abcdefghijklmnopqrstuvwxyz123456",
    ];
    const safeValues = ["text/plain; charset=utf-8", "application/json", "example.com", "2026-08-09T10:00:00Z"];
    for (const value of sensitiveValues) {
      expect(headerValueSensitive(value)).toBe(true);
    }
    for (const value of safeValues) {
      expect(headerValueSensitive(value)).toBe(false);
    }
  });

  it("scrubs names unconditionally and values by pattern", () => {
    expect(scrubHeaderValue("Authorization", "anything")).toBe("[REDACTED]");
    expect(scrubHeaderValue("X-Trace-Id", "token=leaked")).toBe("[REDACTED]");
    expect(scrubHeaderValue("X-Trace-Id", "abc123")).toBe("abc123");
  });

  it("scrubHeaderMap preserves keys and masks sensitive entries", () => {
    const scrubbed = scrubHeaderMap({
      Authorization: "Bearer sk-secret",
      "X-Trace-Id": "abc123",
      "X-Project": "prism",
    });
    expect(scrubbed).toEqual({ Authorization: "[REDACTED]", "X-Trace-Id": "abc123", "X-Project": "prism" });
  });
});
