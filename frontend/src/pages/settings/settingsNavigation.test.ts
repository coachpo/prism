import { describe, expect, it } from "vitest";

import {
  DEFAULT_SECTION_BY_SCOPE,
  GLOBAL_NAV_SECTIONS,
  GLOBAL_SECTION_IDS,
  INSTANCE_NAV_SECTIONS,
  INSTANCE_SECTION_IDS,
} from "./settingsNavigation";

describe("settings navigation contract", () => {
  it("keeps the scoped section allowlists and card navigation distinct", () => {
    expect([...GLOBAL_SECTION_IDS]).toEqual([
      "billing-currency",
      "timezone",
      "audit-privacy",
      "header-blocklist",
      "client-rules",
    ]);
    expect([...INSTANCE_SECTION_IDS]).toEqual([
      "authentication",
      "retention",
      "manual-cleanup",
      "retention-jobs",
    ]);
    expect(GLOBAL_NAV_SECTIONS.map(({ id }) => id)).toEqual([
      "billing-currency",
      "audit-privacy",
      "header-blocklist",
      "client-rules",
    ]);
    expect(INSTANCE_NAV_SECTIONS.map(({ id }) => id)).toEqual([
      "authentication",
      "retention",
      "manual-cleanup",
      "retention-jobs",
    ]);
  });

  it("keeps the default section for each public scope", () => {
    expect(DEFAULT_SECTION_BY_SCOPE).toEqual({
      global: "billing-currency",
      instance: "authentication",
    });
  });
});
