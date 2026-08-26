import { describe, expect, it } from "vitest";

import {
  buildRequestLogQueryParams,
  requestLogQuerySignature,
} from "./requestLogQuery";
import { parsePageSearch } from "./queryParams";

describe("request-log query projection", () => {
  it("preserves explicit custom time bounds in the wire query", () => {
    const state = parsePageSearch({
      from_time: "2026-08-01T00:00:00Z",
      to_time: "2026-08-02T00:00:00Z",
      view: "attempts",
    });

    expect(buildRequestLogQueryParams(state)).toMatchObject({
      time_range: "custom",
      from_time: "2026-08-01T00:00:00Z",
      to_time: "2026-08-02T00:00:00Z",
      view: "attempts",
    });
  });

  it("keeps chain cursor navigation in one query scope", () => {
    const firstState = parsePageSearch({
      chain_cursor: "cursor-1",
      view: "ingress_chains",
    });
    const secondState = parsePageSearch({
      chain_cursor: "cursor-2",
      view: "ingress_chains",
    });

    expect(
      requestLogQuerySignature(
        firstState,
        buildRequestLogQueryParams(firstState),
      ),
    ).toBe(
      requestLogQuerySignature(
        secondState,
        buildRequestLogQueryParams(secondState),
      ),
    );
  });
});
