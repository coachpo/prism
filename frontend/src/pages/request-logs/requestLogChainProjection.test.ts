import { describe, expect, it } from "vitest";

import type { RequestLogListItem } from "@/lib/types";
import type {
  ChainIngressItem,
  ChainResponse,
} from "@/lib/types/request-logs";
import {
  appendUniqueRequestItems,
  flattenChainItems,
  mergeChainRowPage,
} from "./requestLogChainProjection";

function item(id: string): RequestLogListItem {
  return { request_log_id: id } as RequestLogListItem;
}

function chain(id: string, rowIds: string[]): ChainIngressItem {
  return {
    ingress_request_id: id,
    retained_rows: rowIds.map((rowId) => ({ request_log_id: rowId })),
    retained_rows_loaded_count: rowIds.length,
  } as ChainIngressItem;
}

describe("request-log chain projection", () => {
  it("keeps chain row projection and append dedupe keyed by request-log id", () => {
    const response = {
      items: [chain("ingress-1", ["101", "102"])],
    } as ChainResponse;

    const projected = flattenChainItems(response);
    expect(projected.map((row) => row.request_log_id)).toEqual(["101", "102"]);
    expect(
      appendUniqueRequestItems([item("101")], [item("101"), item("103")]).map(
        (row) => row.request_log_id,
      ),
    ).toEqual(["101", "103"]);
  });

  it("merges nested pages without duplicating retained rows", () => {
    const merged = mergeChainRowPage(
      chain("ingress-1", ["101"]),
      chain("ingress-1", ["101", "102"]),
    );

    expect(merged.retained_rows.map((row) => row.request_log_id)).toEqual([
      "101",
      "102",
    ]);
    expect(merged.retained_rows_loaded_count).toBe(2);
  });
});
