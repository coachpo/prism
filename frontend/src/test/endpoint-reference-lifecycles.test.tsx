import { act, renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { useEndpointReferences } from "@/features/endpoints/useEndpointReferences";
import type {
  EndpointReferenceDetail,
  EndpointReferenceItem,
  EndpointReferenceSummary,
} from "@/lib/types";
import { rewriteTestServer } from "./msw/server";

function summary(direct: number): EndpointReferenceSummary {
  return {
    direct_reference_count: direct,
    referencing_model_count: direct > 0 ? 1 : 0,
    enabled_reference_count: direct,
    orphan_reference_count: 0,
  };
}

function item(connectionId: number): EndpointReferenceItem {
  return {
    kind: "orphan_connection",
    connection_id: connectionId,
    terminal_target_id: connectionId,
    terminal_target_name: null,
    api_family: "openai",
    connection_is_active: false,
    access_target: null,
    owner_model: null,
    openai_text_capability: null,
    openai_image_capability: null,
    has_routing_schedule: false,
    pricing_template: null,
    enabled: false,
    inactive_reasons: ["orphaned"],
  };
}

function detail(
  endpointId: number,
  referenceSummary: EndpointReferenceSummary,
  items: EndpointReferenceItem[],
  nextCursor: string | null,
  snapshotHash = "hash-1",
): EndpointReferenceDetail {
  return {
    endpoint_id: endpointId,
    summary: referenceSummary,
    reference_page: {
      items,
      total_count: referenceSummary.direct_reference_count,
      next_cursor: nextCursor,
      reference_snapshot_hash: snapshotHash,
    },
  };
}

describe("Endpoint reference lifecycle ownership", () => {
  it("fences a late batch response behind a detail summary replacement", async () => {
    let batchRequested = false;
    let resolveBatch!: (value: {
      items: Array<{ endpoint_id: number; summary: EndpointReferenceSummary }>;
    }) => void;
    const batchResponse = new Promise<{
      items: Array<{ endpoint_id: number; summary: EndpointReferenceSummary }>;
    }>((resolve) => {
      resolveBatch = resolve;
    });

    rewriteTestServer.use(
      http.post("/api/endpoints/references/batch", async () => {
        batchRequested = true;
        return HttpResponse.json(await batchResponse);
      }),
      http.get("/api/endpoints/1/references", () =>
        HttpResponse.json(detail(1, summary(4), [item(41)], null)),
      ),
    );

    const { result } = renderHook(() => useEndpointReferences([1]));
    await waitFor(() => expect(batchRequested).toBe(true));

    await act(async () => {
      await result.current.loadDetail(1);
    });
    expect(result.current.details[1]?.status).toBe("ready");
    const detailSummary = result.current.summaries[1];
    expect(detailSummary?.status).toBe("ready");
    if (detailSummary?.status === "ready") {
      expect(detailSummary.value.direct_reference_count).toBe(4);
    }

    await act(async () => {
      resolveBatch({ items: [{ endpoint_id: 1, summary: summary(1) }] });
    });
    await waitFor(() => {
      const current = result.current.summaries[1];
      expect(current?.status).toBe("ready");
      if (current?.status === "ready") {
        expect(current.value.direct_reference_count).toBe(4);
      }
    });
    expect(result.current.details[1]?.status).toBe("ready");
  });

  it("chunks visible Endpoint IDs and replaces every returned summary", async () => {
    const batchSizes: number[] = [];
    rewriteTestServer.use(
      http.post("/api/endpoints/references/batch", async ({ request }) => {
        const body = (await request.json()) as { endpoint_ids: number[] };
        batchSizes.push(body.endpoint_ids.length);
        return HttpResponse.json({
          items: body.endpoint_ids.map((endpointId) => ({
            endpoint_id: endpointId,
            summary: summary(0),
          })),
        });
      }),
    );

    const ids = Array.from({ length: 101 }, (_, index) => index + 1);
    const { result } = renderHook(() => useEndpointReferences(ids));

    await waitFor(() => expect(batchSizes).toEqual([100, 1]));
    await waitFor(() => {
      expect(result.current.hasUnknownOrStale).toBe(false);
    });
    expect(Object.keys(result.current.summaries)).toHaveLength(101);
  });

  it("restarts from page one when a detail cursor snapshot changes", async () => {
    const cursors: Array<string | null> = [];
    let detailCall = 0;
    rewriteTestServer.use(
      http.post("/api/endpoints/references/batch", () =>
        HttpResponse.json({
          items: [{ endpoint_id: 1, summary: summary(2) }],
        }),
      ),
      http.get("/api/endpoints/1/references", ({ request }) => {
        const cursor = new URL(request.url).searchParams.get("cursor");
        cursors.push(cursor);
        detailCall += 1;
        if (detailCall === 1) {
          return HttpResponse.json(
            detail(1, summary(2), [item(1)], "cursor-1"),
          );
        }
        if (detailCall === 2) {
          return HttpResponse.json(
            detail(1, summary(2), [item(2)], "cursor-2", "hash-2"),
          );
        }
        return HttpResponse.json(
          detail(1, summary(2), [item(3)], null, "hash-2"),
        );
      }),
    );

    const { result } = renderHook(() => useEndpointReferences([1]));
    await act(async () => {
      await result.current.loadDetail(1);
    });
    await act(async () => {
      await result.current.loadMore(1);
    });

    await waitFor(() => {
      const current = result.current.details[1];
      expect(current?.status).toBe("ready");
      if (current?.status === "ready") {
        expect(current.value.loaded_items.map((entry) => entry.connection_id)).toEqual([3]);
      }
    });
    expect(cursors).toEqual([null, "cursor-1", null]);
  });

  it("keeps the failed append cursor retryable while retaining loaded rows", async () => {
    const cursors: Array<string | null> = [];
    let appendAttempts = 0;
    rewriteTestServer.use(
      http.post("/api/endpoints/references/batch", () =>
        HttpResponse.json({
          items: [{ endpoint_id: 1, summary: summary(2) }],
        }),
      ),
      http.get("/api/endpoints/1/references", ({ request }) => {
        const cursor = new URL(request.url).searchParams.get("cursor");
        cursors.push(cursor);
        if (cursor === null) {
          return HttpResponse.json(
            detail(1, summary(2), [item(1)], "cursor-1"),
          );
        }
        appendAttempts += 1;
        if (appendAttempts === 1) {
          return HttpResponse.json({ detail: "temporary" }, { status: 503 });
        }
        return HttpResponse.json(detail(1, summary(2), [item(2)], null));
      }),
    );

    const { result } = renderHook(() => useEndpointReferences([1]));
    await act(async () => {
      await result.current.loadDetail(1);
    });
    await act(async () => {
      await result.current.loadMore(1);
    });
    await waitFor(() => expect(result.current.details[1]?.status).toBe("stale"));
    const stale = result.current.details[1];
    if (stale?.status === "stale") {
      expect(stale.value.loaded_items.map((entry) => entry.connection_id)).toEqual([1]);
    }

    await act(async () => {
      await result.current.loadMore(1);
    });
    await waitFor(() => {
      const current = result.current.details[1];
      expect(current?.status).toBe("ready");
      if (current?.status === "ready") {
        expect(current.value.loaded_items.map((entry) => entry.connection_id)).toEqual([1, 2]);
      }
    });
    expect(cursors).toEqual([null, "cursor-1", "cursor-1"]);
  });
});
