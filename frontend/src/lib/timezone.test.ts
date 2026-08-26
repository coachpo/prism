import { describe, expect, it } from "vitest";

import { formatTimezoneOffset, formatTimezonePreview } from "./timezone";

describe("timezone presentation", () => {
  it("keeps IANA daylight-saving offsets tied to the supplied instant", () => {
    expect(formatTimezoneOffset("Europe/Helsinki", new Date("2026-01-15T12:00:00Z"))).toBe("UTC+02:00");
    expect(formatTimezoneOffset("Europe/Helsinki", new Date("2026-07-15T12:00:00Z"))).toBe("UTC+03:00");
  });

  it("formats the preview through the current locale and timezone", () => {
    expect(formatTimezonePreview("UTC", new Date("2026-01-02T03:04:00Z"))).toBe("2026-01-02 03:04");
  });
});
