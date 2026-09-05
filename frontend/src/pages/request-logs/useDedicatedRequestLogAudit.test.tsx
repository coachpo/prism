import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useDedicatedRequestLogAudit } from "./useDedicatedRequestLogAudit";

const mocks = vi.hoisted(() => ({
  auditDetail: vi.fn(),
  auditList: vi.fn(),
  requestDetail: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    audit: {
      get: mocks.auditDetail,
      listForRequestLog: mocks.auditList,
    },
    stats: { requestDetail: mocks.requestDetail },
  },
}));

function readyRequest(requestLogId: string) {
  return {
    summary: {
      api_family: "openai" as const,
      created_at: "2026-08-13T12:00:00Z",
      request_log_id: requestLogId,
    },
    request: { operation_name: "openai.chat_completions" },
    routing: {
      audit_capture_bodies_at_request: true,
      audit_enabled_at_request: true,
    },
  };
}

describe("dedicated request-log audit lookup", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("passes a BIGINT request-log ID unchanged through both API calls", async () => {
    const requestLogId = "9007199254740997";
    mocks.requestDetail.mockResolvedValue(readyRequest(requestLogId));
    mocks.auditList.mockResolvedValue({
      has_more: false,
      items: [{ id: 201, request_log_id: requestLogId }],
      next_cursor: null,
    });
    mocks.auditDetail.mockResolvedValue({
      id: 201,
      request_log_id: requestLogId,
    });

    const { result } = renderHook(() =>
      useDedicatedRequestLogAudit({
        requestId: requestLogId,
        selectedAuditId: null,
        selectedAuditParamLabel: null,
        selectedAuditParamPresent: false,
      }),
    );

    await waitFor(() => expect(result.current.detail.phase).toBe("ready"));
    expect(mocks.requestDetail).toHaveBeenCalledWith(requestLogId);
    expect(mocks.auditList).toHaveBeenCalledWith(requestLogId, {
      from: "2026-08-13T00:00:00.000Z",
      to: "2026-08-14T00:00:00.000Z",
      limit: 20,
      cursor: undefined,
    });
    expect(mocks.auditDetail).toHaveBeenCalledWith(201);
    expect(result.current.request.request?.summary.request_log_id).toBe(
      requestLogId,
    );
  });

  it("does not re-issue requestDetail when the audit cursor changes (C3)", async () => {
    const requestLogId = "42";
    mocks.requestDetail.mockResolvedValue(readyRequest(requestLogId));
    mocks.auditList.mockResolvedValue({
      has_more: true,
      items: [{ id: 1 }, { id: 2 }],
      next_cursor: "cursor-page-2",
    });
    mocks.auditDetail.mockResolvedValue({ id: 1 });

    const { result, rerender } = renderHook(
      ({ cursor }: { cursor?: string }) =>
        useDedicatedRequestLogAudit({
          requestId: requestLogId,
          selectedAuditId: null,
          selectedAuditParamLabel: null,
          selectedAuditParamPresent: false,
          cursor,
        }),
      { initialProps: { cursor: undefined as string | undefined } },
    );

    await waitFor(() => expect(result.current.detail.phase).toBe("ready"));
    expect(mocks.requestDetail).toHaveBeenCalledTimes(1);
    expect(mocks.auditList).toHaveBeenCalledTimes(1);

    await act(async () => {
      rerender({ cursor: "cursor-page-2" });
    });

    await waitFor(() =>
      expect(result.current.list.items.map((item) => item.id)).toEqual([1, 2]),
    );
    // The list page was re-read for the new cursor; the owning request's
    // detail was not.
    expect(mocks.auditList).toHaveBeenCalledTimes(2);
    expect(mocks.auditList).toHaveBeenLastCalledWith(requestLogId, {
      from: "2026-08-13T00:00:00.000Z",
      to: "2026-08-14T00:00:00.000Z",
      limit: 20,
      cursor: "cursor-page-2",
    });
    expect(mocks.requestDetail).toHaveBeenCalledTimes(1);
  });

  it("retries the list lane alone after a list failure (C3)", async () => {
    const requestLogId = "42";
    mocks.requestDetail.mockResolvedValue(readyRequest(requestLogId));
    mocks.auditList.mockRejectedValueOnce(new Error("list down"));
    mocks.auditList.mockResolvedValue({
      has_more: false,
      items: [{ id: 7 }],
      next_cursor: null,
    });
    mocks.auditDetail.mockResolvedValue({ id: 7 });

    const { result } = renderHook(() =>
      useDedicatedRequestLogAudit({
        requestId: requestLogId,
        selectedAuditId: null,
        selectedAuditParamLabel: null,
        selectedAuditParamPresent: false,
      }),
    );

    await waitFor(() => expect(result.current.list.phase).toBe("error"));
    expect(result.current.list.error).toBe("list down");
    // The request lane stays ready while its sibling failed.
    expect(result.current.request.phase).toBe("ready");

    await act(async () => {
      result.current.retryList();
    });

    await waitFor(() => expect(result.current.list.phase).toBe("ready"));
    expect(mocks.requestDetail).toHaveBeenCalledTimes(1);
    expect(result.current.detail.phase).toBe("ready");
  });

  it("switching the selected record reloads only the detail lane (C3)", async () => {
    const requestLogId = "42";
    mocks.requestDetail.mockResolvedValue(readyRequest(requestLogId));
    mocks.auditList.mockResolvedValue({
      has_more: false,
      items: [{ id: 1 }, { id: 2 }],
      next_cursor: null,
    });
    mocks.auditDetail.mockImplementation(async (id: number) => ({ id }));

    const { result, rerender } = renderHook(
      ({ auditId }: { auditId: number | null }) =>
        useDedicatedRequestLogAudit({
          requestId: requestLogId,
          selectedAuditId: auditId,
          selectedAuditParamLabel: auditId != null ? String(auditId) : null,
          selectedAuditParamPresent: auditId != null,
        }),
      { initialProps: { auditId: 1 as number | null } },
    );

    await waitFor(() => expect(result.current.detail.phase).toBe("ready"));
    expect(mocks.auditList).toHaveBeenCalledTimes(1);
    expect(mocks.auditDetail).toHaveBeenLastCalledWith(1);

    await act(async () => {
      rerender({ auditId: 2 });
    });

    await waitFor(() => expect(result.current.detail.selectedAuditId).toBe(2));
    // The list and the owning request were untouched by the selection change.
    expect(mocks.auditList).toHaveBeenCalledTimes(1);
    expect(mocks.requestDetail).toHaveBeenCalledTimes(1);
    expect(mocks.auditDetail).toHaveBeenLastCalledWith(2);
  });

  it("reports a missing audit selection without dropping the loaded page", async () => {
    const requestLogId = "42";
    mocks.requestDetail.mockResolvedValue(readyRequest(requestLogId));
    mocks.auditList.mockResolvedValue({
      has_more: false,
      items: [{ id: 1 }],
      next_cursor: null,
    });

    const { result } = renderHook(() =>
      useDedicatedRequestLogAudit({
        requestId: requestLogId,
        selectedAuditId: 999,
        selectedAuditParamLabel: "#999",
        selectedAuditParamPresent: true,
      }),
    );

    await waitFor(() =>
      expect(result.current.detail.phase).toBe("missing_selection"),
    );
    expect(result.current.detail.missingAuditLabel).toBe("#999");
    // The page of records that did load stays on screen.
    expect(result.current.list.items).toHaveLength(1);
    expect(mocks.auditDetail).not.toHaveBeenCalled();
  });

  it("keeps the backend's coverage on an empty page instead of dropping it", async () => {
    const requestLogId = "42";
    const coverage = {
      requested_from_time: "2026-08-13T00:00:00Z",
      requested_to_time: "2026-08-14T00:00:00Z",
      effective_from_time: "2026-08-13T18:00:00Z",
      effective_to_time: "2026-08-14T00:00:00Z",
      complete: false,
      gaps: [
        {
          from_time: "2026-08-13T00:00:00Z",
          to_time: "2026-08-13T18:00:00Z",
          reason: "retention_deleted",
        },
      ],
      state: "known" as const,
      source_revision: "rev-1",
    };
    mocks.requestDetail.mockResolvedValue(readyRequest(requestLogId));
    mocks.auditList.mockResolvedValue({
      has_more: false,
      items: [],
      next_cursor: null,
      coverage,
    });

    const { result } = renderHook(() =>
      useDedicatedRequestLogAudit({
        requestId: requestLogId,
        selectedAuditId: null,
        selectedAuditParamLabel: null,
        selectedAuditParamPresent: false,
      }),
    );

    await waitFor(() => expect(result.current.list.phase).toBe("empty"));
    // An empty page under an incomplete coverage is "the evidence may have
    // been deleted", not "no audit record exists".
    expect(result.current.list.coverage).toEqual(coverage);
    expect(result.current.list.fetchedAt).not.toBeNull();
    expect(result.current.lastFetchedAt).not.toBeNull();
  });
});
