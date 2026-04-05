import { describe, expect, it } from "vitest";
import {
  formatNumber,
  formatRelativeTimeFromNow,
  formatTimestampForLocale,
} from "@/i18n/format";

describe("i18n format helpers", () => {
  it("formats numbers with the active locale", () => {
    expect(
      formatNumber(1_250_000, "en", {
        maximumFractionDigits: 1,
        notation: "compact",
      }),
    ).not.toBe(
      formatNumber(1_250_000, "zh-CN", {
        maximumFractionDigits: 1,
        notation: "compact",
      }),
    );
  });

  it("formats relative time with the active locale", () => {
    const now = Date.parse("2026-03-25T12:00:00Z");

    expect(formatRelativeTimeFromNow("2026-03-25T11:00:00Z", "en", now)).not.toBe(
      formatRelativeTimeFromNow("2026-03-25T11:00:00Z", "zh-CN", now),
    );
  });

  it("formats timestamps with locale and timezone", () => {
    expect(formatTimestampForLocale("en", "UTC", "2026-03-25T12:00:00Z")).not.toBe(
      formatTimestampForLocale("zh-CN", "UTC", "2026-03-25T12:00:00Z"),
    );
  });
});
