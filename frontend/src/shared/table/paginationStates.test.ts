// Shared pagination honest-state contract: the four read kinds must render
// distinct, recoverable states (DESIGN.md Honesty Contract). These tests pin
// the transition rules every paged list composes.
import { describe, expect, it } from "vitest";

import {
  beginPagedRead,
  commitPagedRead,
  failPagedRead,
  initialPagedListState,
  keepsCommittedRows,
  shouldShowPendingRows,
} from "./paginationStates";

type Page = { items: string[] };

function readyState(
  items: string[],
): ReturnType<typeof initialPagedListState<Page>> {
  return commitPagedRead(
    initialPagedListState<Page>({ items: [] }),
    { items },
    "ready",
  );
}

describe("paged list state transitions", () => {
  it("initial read shows pending rows with no committed data", () => {
    const state = beginPagedRead(initialPagedListState<Page>(), "initial");
    expect(shouldShowPendingRows(state)).toBe(true);
    expect(keepsCommittedRows(state)).toBe(false);
  });

  it("replace read swaps old rows for skeletons while in flight", () => {
    const state = beginPagedRead(readyState(["a"]), "replace");
    expect(shouldShowPendingRows(state)).toBe(true);
    expect(keepsCommittedRows(state)).toBe(false);
    // The old page is kept only as failure-recovery context; staleness is not
    // claimed up front — the read has not failed yet.
    expect(state.data?.items).toEqual(["a"]);
    expect(state.stale).toBe(false);
  });

  it("refresh read keeps committed rows visible and untouched", () => {
    const state = beginPagedRead(readyState(["a"]), "refresh");
    expect(shouldShowPendingRows(state)).toBe(false);
    expect(keepsCommittedRows(state)).toBe(true);
    expect(state.stale).toBe(false);
  });

  it("append read keeps committed rows visible", () => {
    const state = beginPagedRead(readyState(["a"]), "append");
    expect(shouldShowPendingRows(state)).toBe(false);
    expect(keepsCommittedRows(state)).toBe(true);
  });

  it("failed refresh marks kept data stale instead of dropping it", () => {
    const failed = failPagedRead(
      beginPagedRead(readyState(["a"]), "refresh"),
      "网络错误",
    );
    expect(failed.reading).toBe(false);
    expect(failed.stale).toBe(true);
    expect(failed.error).toBe("网络错误");
    expect(failed.data?.items).toEqual(["a"]);
  });

  it("a later replace read withdraws stale-marked rows and the badge with them", () => {
    const stale = failPagedRead(
      beginPagedRead(readyState(["a"]), "refresh"),
      "网络错误",
    );
    const replace = beginPagedRead(stale, "replace");
    expect(replace.stale).toBe(false);
    expect(shouldShowPendingRows(replace)).toBe(true);
  });

  it("failed replace presents a target-page error, not the old rows", () => {
    const failed = failPagedRead(
      beginPagedRead(readyState(["a"]), "replace"),
      "超时",
    );
    expect(failed.stale).toBe(false);
    expect(failed.error).toBe("超时");
    expect(failed.data).not.toBeNull();
  });

  it("failed append keeps data fresh-looking but carries the local error", () => {
    const failed = failPagedRead(
      beginPagedRead(readyState(["a"]), "append"),
      "失败",
    );
    expect(failed.stale).toBe(true);
    expect(failed.error).toBe("失败");
  });

  it("commit atomically swaps data and clears stale/error", () => {
    const base = failPagedRead(
      beginPagedRead(readyState(["old"]), "refresh"),
      "失败",
    );
    const committed = commitPagedRead(base, { items: ["new"] }, "ready");
    expect(committed.data?.items).toEqual(["new"]);
    expect(committed.stale).toBe(false);
    expect(committed.error).toBeNull();
    expect(committed.reading).toBe(false);
    expect(committed.lastSuccessfulAt).not.toBeNull();
  });
});
