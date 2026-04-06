import { describe, expect, it } from "vitest";
import { DASHBOARD_TAB_OPTIONS, DEFAULTS, parsePageState, stateToParams } from "../queryParams";

describe("dashboard query params", () => {
  it("defaults to the overview tab when the URL is unset", () => {
    expect(parsePageState(new URLSearchParams())).toEqual({ tab: "overview" });
    expect(stateToParams({ tab: "overview" }).toString()).toBe("tab=overview");
  });

  it("keeps the approved dashboard tab options and default", () => {
    expect(DASHBOARD_TAB_OPTIONS).toEqual(["overview", "analytics"]);
    expect(DEFAULTS.tab).toBe("overview");
  });

  it("preserves the analytics tab during parse and serialization", () => {
    const parsed = parsePageState(new URLSearchParams("tab=analytics"));

    expect(parsed.tab).toBe("analytics");
    expect(stateToParams(parsed).toString()).toBe("tab=analytics");
  });

  it("normalizes invalid tabs back to the canonical overview state", () => {
    const parsed = parsePageState(new URLSearchParams("tab=detail"));

    expect(parsed.tab).toBe("overview");
    expect(stateToParams(parsed).toString()).toBe("tab=overview");
  });
});
