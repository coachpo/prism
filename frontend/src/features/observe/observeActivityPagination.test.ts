import { describe, expect, it } from "vitest";
import { nextObserveActivityCursor } from "./observeActivityPagination";

describe("observe activity pagination", () => {
  it("continues with the usage event identity only while another page exists", () => {
    const items = [{ usage_event_id: "42" }];
    expect(nextObserveActivityCursor(items, true)).toBe("42");
    expect(nextObserveActivityCursor(items, false)).toBeNull();
  });
});
