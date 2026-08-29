import { describe, expect, it } from "vitest";

import { buildRequestsSearch } from "@/features/observe/observeErrorRequestSearch";
import type { ObserveErrorSelection } from "@/features/observe/observeErrorSelection";

const selection: ObserveErrorSelection = {
  key: "route-attempt-problem",
  label: "route attempt problem",
  requestFilters: {
    row_kind: ["upstream"],
    attempt_result: ["stream_error", "transport_error", "__null__"],
    endpoint_id: ["11", "12"],
  },
  match: () => false,
};

describe("Observe Errors request-log deep links", () => {
  it.each(["ingress", "final_execution", "route_attempt"] as const)(
    "uses attempts view and preserves the signed repeated cohort for %s",
    (scope) => {
      expect(
        buildRequestsSearch(
          selection,
          {
            view: "attempts",
            query_context: "signed-context",
            final_from_time: "2026-08-01T00:00:00Z",
            final_to_time: "2026-08-02T00:00:00Z",
            base_request_filters: {},
          },
          "fallback-context",
          scope,
        ),
      ).toEqual({
        view: "attempts",
        query_context: "signed-context",
        row_kind: "upstream",
        attempt_result: "stream_error,transport_error,__null__",
        endpoint_id: "11,12",
      });
    },
  );
});
