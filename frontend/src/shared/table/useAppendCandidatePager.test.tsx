// The shared append-pager controller contract: replace/append lifecycle,
// generation ownership, late-response discard, single-flight appends, dedupe,
// retry, source-qualified revision isolation, and revision rollover. All
// timing is deterministic: deferred promises resolved explicitly, no sleeps.
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  useAppendCandidatePager,
  type CandidatePage,
} from "./useAppendCandidatePager";

interface FakeCandidate {
  id: string;
}

function page(
  offset: number,
  count: number,
  total: number,
  revision: string,
): CandidatePage<FakeCandidate> {
  const items: FakeCandidate[] = [];
  for (let index = 0; index < count; index += 1) {
    items.push({ id: `c${offset + index}` });
  }
  return { items, total, offset, revision };
}

interface Deferred {
  resolve: (value: CandidatePage<FakeCandidate>) => void;
  reject: (cause: unknown) => void;
  signal: AbortSignal;
  offset: number;
}

/** Controllable fetchPage: every call is captured as a deferred promise. */
function deferredFetch() {
  const calls: Deferred[] = [];
  const fetchPage = vi.fn(
    (offset: number, signal: AbortSignal) =>
      new Promise<CandidatePage<FakeCandidate>>((resolve, reject) => {
        calls.push({ resolve, reject, signal, offset });
      }),
  );
  return { calls, fetchPage };
}

type PagerProps = Parameters<typeof useAppendCandidatePager<FakeCandidate>>[0];

function propsFor(
  fetchPage: PagerProps["fetchPage"],
  sourceKey: PagerProps["sourceKey"],
  conditionKey = "cond-1",
  enabled = true,
): PagerProps {
  return {
    sourceKey,
    enabled,
    conditionKey,
    fetchPage,
    itemKey: (item: FakeCandidate) => item.id,
  };
}

function renderPager(
  fetchPage: PagerProps["fetchPage"],
  conditionKey = "cond-1",
) {
  return renderHook((options: PagerProps) => useAppendCandidatePager(options), {
    initialProps: propsFor(fetchPage, "models.dev", conditionKey),
  });
}

async function flushReads() {
  await act(async () => {
    await Promise.resolve();
  });
}

describe("useAppendCandidatePager", () => {
  it("commits the replace read and exposes revision/total/hasMore", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    expect(result.current.phase).toBe("loading");
    calls[0].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    expect(result.current.phase).toBe("ready");
    expect(result.current.items).toHaveLength(20);
    expect(result.current.total).toBe(47);
    expect(result.current.revision).toBe("models.dev:etag-1");
    expect(result.current.hasMore).toBe(true);
    expect(result.current.replacing).toBe(false);
  });

  it("keeps a failed replace a failure surface, never an empty list", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    calls[0].reject(new Error("catalog read failed"));
    await flushReads();
    expect(result.current.phase).toBe("error");
    expect(result.current.error).toBe("catalog read failed");
    expect(result.current.items).toHaveLength(0);
    expect(result.current.hasMore).toBe(false);
    expect(result.current.replacing).toBe(false);
  });

  it("retries the replace read through onRetry and clears the error", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    calls[0].reject(new Error("boom"));
    await flushReads();
    act(() => {
      result.current.onRetry();
    });
    expect(calls).toHaveLength(2);
    calls[1].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    expect(result.current.phase).toBe("ready");
    expect(result.current.error).toBeNull();
  });

  it("appends the next segment with dedupe and closes hasMore at total", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 25, "models.dev:etag-1"));
    await flushReads();
    act(() => {
      result.current.onLoadMore();
    });
    // The append reads the cursor the last accepted response reported.
    expect(calls[1].offset).toBe(20);
    calls[1].resolve(page(20, 5, 25, "models.dev:etag-1"));
    await flushReads();
    expect(result.current.items).toHaveLength(25);
    expect(result.current.hasMore).toBe(false);
    expect(result.current.appendError).toBeNull();
  });

  it("deduplicates overlapping segments without duplicating keys", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 40, "models.dev:etag-1"));
    await flushReads();
    act(() => {
      result.current.onLoadMore();
    });
    // Server re-echoes overlapping items; keys keep the list honest. The
    // segment starts two items before the cursor: c18/c19 overlap, c20..c29
    // are new.
    calls[1].resolve({
      items: [...page(18, 12, 40, "models.dev:etag-1").items],
      total: 40,
      offset: 20,
      revision: "models.dev:etag-1",
    });
    await flushReads();
    const ids = result.current.items.map((item) => item.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(result.current.items).toHaveLength(30);
  });

  it("keeps loaded candidates on append failure and retries the same offset once", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    act(() => {
      result.current.onLoadMore();
    });
    calls[1].reject(new Error("append failed"));
    await flushReads();
    expect(result.current.items).toHaveLength(20);
    expect(result.current.appendError).toBe("append failed");
    expect(result.current.hasMore).toBe(true);
    // Retry issues exactly one new read at the same offset.
    act(() => {
      result.current.onLoadMore();
    });
    expect(calls[2].offset).toBe(20);
    expect(calls).toHaveLength(3);
    calls[2].resolve(page(20, 20, 47, "models.dev:etag-1"));
    await flushReads();
    expect(result.current.items).toHaveLength(40);
    expect(result.current.appendError).toBeNull();
  });

  it("is single-flight: a double click issues exactly one append", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    act(() => {
      result.current.onLoadMore();
      result.current.onLoadMore();
    });
    expect(calls).toHaveLength(2); // replace + one append only
    calls[1].resolve(page(20, 20, 47, "models.dev:etag-1"));
    await flushReads();
    // After the append settles, a further click can issue the next segment.
    act(() => {
      result.current.onLoadMore();
    });
    expect(calls).toHaveLength(3);
    expect(calls[2].offset).toBe(40);
  });

  it("isolates A→B→A late replaces: only the current generation commits", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result, rerender } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    expect(result.current.items).toHaveLength(20);

    // Switch to B, then back to A. The A→B→A semantic cycle reuses A's key,
    // so generation ownership — not the key — must reject the late A reply.
    rerender(propsFor(fetchPage, "models.dev", "cond-B"));
    rerender(propsFor(fetchPage, "models.dev", "cond-1"));
    expect(calls).toHaveLength(3);
    const lateA = calls[1];
    const currentA = calls[2];
    // The old A response arrives after the new A read and must be discarded.
    lateA.resolve(page(0, 30, 99, "models.dev:etag-old"));
    await flushReads();
    expect(result.current.items).toHaveLength(0);
    expect(result.current.phase).toBe("loading");
    currentA.resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    expect(result.current.phase).toBe("ready");
    expect(result.current.items).toHaveLength(20);
    expect(result.current.revision).toBe("models.dev:etag-1");
  });

  it("commits source evidence under the same generation ownership as rows", async () => {
    type Evidence = { status: "fresh" | "stale" };
    type EvidencePage = CandidatePage<FakeCandidate, Evidence>;
    const calls: Array<{
      resolve: (value: EvidencePage) => void;
      signal: AbortSignal;
    }> = [];
    const fetchPage = vi.fn(
      (_offset: number, signal: AbortSignal) =>
        new Promise<EvidencePage>((resolve) => calls.push({ resolve, signal })),
    );
    const { result, rerender } = renderHook(
      ({ conditionKey }) =>
        useAppendCandidatePager<FakeCandidate, Evidence>({
          sourceKey: "pi.dev",
          enabled: true,
          conditionKey,
          fetchPage,
          itemKey: (item) => item.id,
        }),
      { initialProps: { conditionKey: "model-A" } },
    );
    rerender({ conditionKey: "model-B" });

    calls[0].resolve({
      ...page(0, 1, 1, "pi.dev:old"),
      evidence: { status: "fresh" },
    });
    await flushReads();
    expect(result.current.items).toHaveLength(0);
    expect(result.current.evidence).toBeNull();

    calls[1].resolve({
      ...page(0, 1, 1, "pi.dev:current"),
      evidence: { status: "stale" },
    });
    await flushReads();
    expect(result.current.items).toHaveLength(1);
    expect(result.current.evidence).toEqual({ status: "stale" });
  });

  it("discards a late append from an older generation", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result, rerender } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    act(() => {
      result.current.onLoadMore();
    });
    const oldAppend = calls[1];
    // The condition changes before the append answers.
    rerender(propsFor(fetchPage, "models.dev", "cond-B"));
    oldAppend.resolve(page(20, 20, 47, "models.dev:etag-1"));
    await flushReads();
    // The old append must not mix into the new condition's (empty) list.
    expect(result.current.items).toHaveLength(0);
    expect(result.current.phase).toBe("loading");
  });

  it("withholds revision-rollover appends and restarts from offset 0", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    act(() => {
      result.current.onLoadMore();
    });
    // The catalog revision changed between page 1 and the append.
    calls[1].resolve(page(20, 20, 60, "models.dev:etag-2"));
    await flushReads();
    expect(result.current.revisionRolledOver).toBe(true);
    expect(result.current.items).toHaveLength(0);
    expect(result.current.revision).toBe("models.dev:etag-2");
    // The pager restarts from offset 0 of the new revision.
    await waitFor(() => expect(calls[2].offset).toBe(0));
    act(() => result.current.onRolloverAcknowledged());
    expect(result.current.revisionRolledOver).toBe(true);
    expect(calls[2].signal.aborted).toBe(false);
    calls[2].resolve(page(0, 20, 60, "models.dev:etag-2"));
    await flushReads();
    expect(result.current.items).toHaveLength(20);
    expect(result.current.revisionRolledOver).toBe(true);
    act(() => result.current.onRolloverAcknowledged());
    expect(result.current.revisionRolledOver).toBe(false);
  });

  it("keeps models.dev and pi.dev revision key spaces isolated", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result, rerender } = renderPager(fetchPage);
    calls[0].resolve(page(0, 20, 47, "models.dev:etag-1"));
    await flushReads();
    expect(result.current.revision).toBe("models.dev:etag-1");

    // The same bare revision text under the other source is a different key:
    // committing a pi.dev page into a models.dev list is a rollover, never a
    // seamless continuation.
    rerender(propsFor(fetchPage, "pi.dev", "cond-1"));
    calls[1].resolve(page(0, 10, 30, "pi.dev:sha256-1"));
    await flushReads();
    expect(result.current.revision).toBe("pi.dev:sha256-1");
    expect(result.current.items).toHaveLength(10);

    act(() => {
      result.current.onLoadMore();
    });
    calls[2].resolve(page(10, 10, 30, "models.dev:etag-1"));
    await flushReads();
    // A foreign-source revision cannot be appended onto the pi.dev list.
    expect(result.current.revisionRolledOver).toBe(true);
    expect(result.current.revision).toBe("models.dev:etag-1");
  });

  it("idles to an empty ready state when disabled", async () => {
    const { calls, fetchPage } = deferredFetch();
    const { result, rerender } = renderPager(fetchPage);
    rerender(propsFor(fetchPage, "models.dev", "cond-1", false));
    // The disabled claim resolves the read without touching the network.
    await flushReads();
    expect(result.current.phase).toBe("empty");
    expect(result.current.items).toHaveLength(0);
    expect(result.current.error).toBeNull();
    expect(calls[0].signal.aborted).toBe(true);
  });
});
