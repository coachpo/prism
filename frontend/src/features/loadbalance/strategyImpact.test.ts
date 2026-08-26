// C4 regression: strategy impact appends dedupe by stable model identity, so
// overlapping cursor pages never render the same attached model twice.
import { describe, expect, it } from "vitest";

import { mergeStrategyImpactItems } from "./strategyImpact";
import type { StrategyImpactListResponse } from "@/lib/types";

type ImpactItem = StrategyImpactListResponse["items"][number];

function impactItem(modelConfigId: number, modelId: string): ImpactItem {
  return {
    model_config_id: modelConfigId,
    model_id: modelId,
    display_name: modelId,
    is_enabled: true,
  };
}

describe("mergeStrategyImpactItems", () => {
  it("appends only models not already on screen", () => {
    const existing = [impactItem(1, "m-one"), impactItem(2, "m-two")];
    const incoming = [impactItem(2, "m-two"), impactItem(3, "m-three")];
    const merged = mergeStrategyImpactItems(existing, incoming);
    expect(merged.map((item) => item.model_config_id)).toEqual([1, 2, 3]);
  });

  it("keeps the first page untouched when it is the initial read", () => {
    const incoming = [impactItem(1, "m-one")];
    expect(mergeStrategyImpactItems(undefined, incoming)).toEqual(incoming);
  });

  it("returns a copy, never the caller's array", () => {
    const incoming = [impactItem(1, "m-one")];
    const merged = mergeStrategyImpactItems(undefined, incoming);
    expect(merged).not.toBe(incoming);
  });
});
