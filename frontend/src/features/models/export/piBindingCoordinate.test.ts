import { describe, expect, it } from "vitest";

import { piBindingCoordinateKey } from "./piBindingCoordinate";

describe("piBindingCoordinateKey", () => {
  it("uses deterministic textual tuple encoding without delimiter collisions", () => {
    const first = piBindingCoordinateKey({
      provider_id: "a",
      model_id: "b\u0000c",
    });
    const second = piBindingCoordinateKey({
      provider_id: "a\u0000b",
      model_id: "c",
    });

    expect(first).toBe('["a","b\\u0000c"]');
    expect(second).toBe('["a\\u0000b","c"]');
    expect(first).not.toBe(second);
  });
});
