import { renderHook, waitFor, act } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAuditDetail } from "../useAuditDetail";
import type { AuditLogDetail, AuditLogListItem, AuditLogListResponse } from "@/lib/types";

const api = vi.hoisted(() => ({
  audit: {
    list: vi.fn(),
    get: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function createAudit(id: number, requestLogId: number): AuditLogDetail {
  return {
    id,
    request_log_id: requestLogId,
    profile_id: 1,
    vendor_id: 2,
    model_id: "gpt-5.4",
    endpoint_id: 10,
    connection_id: 20,
    endpoint_base_url: "https://api.example.com/v1",
    endpoint_description: "Primary",
    request_method: "POST",
    request_url: "https://api.example.com/v1/chat/completions",
    request_headers: "{}",
    request_body: "{\"prompt\":\"hello\"}",
    response_status: 200,
    response_headers: "{}",
    response_body: "{\"id\":\"resp_123\"}",
    is_stream: false,
    duration_ms: 120,
    created_at: "2026-03-15T00:00:00.000Z",
  };
}

function createAuditListItem(id: number, requestLogId: number): AuditLogListItem {
  return {
    id,
    request_log_id: requestLogId,
    profile_id: 1,
    vendor_id: 2,
    model_id: "gpt-5.4",
    endpoint_id: 10,
    connection_id: 20,
    endpoint_base_url: "https://api.example.com/v1",
    endpoint_description: "Primary",
    request_method: "POST",
    request_url: "https://api.example.com/v1/chat/completions",
    request_headers: "{}",
    request_body_preview: "{\"prompt\":\"hello\"}",
    response_status: 200,
    is_stream: false,
    duration_ms: 120,
    created_at: "2026-03-15T00:00:00.000Z",
  };
}

describe("useAuditDetail", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("ignores stale results when the selected request changes mid-load", async () => {
    const firstList = deferred<AuditLogListResponse>();

    api.audit.list.mockImplementation(({ request_log_id }: { request_log_id: number }) => {
      if (request_log_id === 1) {
        return firstList.promise;
      }

      return Promise.resolve({
        items: [createAuditListItem(22, 2)],
        total: 1,
        limit: 20,
        offset: 0,
      });
    });

    api.audit.get.mockImplementation((id: number) => Promise.resolve(createAudit(id, 2)));

    const { result, rerender } = renderHook(
      ({ requestLogId, enabled }) => useAuditDetail({ requestLogId, enabled }),
      {
        initialProps: {
          requestLogId: 1,
          enabled: true,
        },
      }
    );

    rerender({
      requestLogId: 2,
      enabled: true,
    });

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
      expect(result.current.audits).toHaveLength(1);
    });

    expect(result.current.audits[0]?.id).toBe(22);

    await act(async () => {
      firstList.resolve({
        items: [createAuditListItem(11, 1)],
        total: 1,
        limit: 20,
        offset: 0,
      });
      await Promise.resolve();
    });

    expect(api.audit.get).toHaveBeenCalledTimes(1);
    expect(api.audit.get).toHaveBeenCalledWith(22);
    expect(result.current.audits[0]?.id).toBe(22);
  });

  it("hides previous request data while a different request is loading", async () => {
    const secondList = deferred<AuditLogListResponse>();

    api.audit.list.mockImplementation(({ request_log_id }: { request_log_id: number }) => {
      if (request_log_id === 1) {
        return Promise.resolve({
          items: [createAuditListItem(11, 1)],
          total: 1,
          limit: 20,
          offset: 0,
        });
      }

      return secondList.promise;
    });
    api.audit.get.mockImplementation((id: number) =>
      Promise.resolve(createAudit(id, id === 11 ? 1 : 2))
    );

    const { result, rerender } = renderHook(
      ({ requestLogId, enabled }) => useAuditDetail({ requestLogId, enabled }),
      {
        initialProps: {
          requestLogId: 1,
          enabled: true,
        },
      }
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
      expect(result.current.audits[0]?.id).toBe(11);
    });

    rerender({
      requestLogId: 2,
      enabled: true,
    });

    expect(result.current.audits).toEqual([]);
    expect(result.current.error).toBeNull();
    expect(result.current.loading).toBe(true);

    await act(async () => {
      secondList.resolve({
        items: [createAuditListItem(22, 2)],
        total: 1,
        limit: 20,
        offset: 0,
      });
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
      expect(result.current.audits[0]?.id).toBe(22);
    });
  });

  it("clears state when audit loading is disabled", async () => {
    api.audit.list.mockResolvedValue({
      items: [createAuditListItem(31, 3)],
      total: 1,
      limit: 20,
      offset: 0,
    });
    api.audit.get.mockResolvedValue(createAudit(31, 3));

    const { result, rerender } = renderHook(
      ({ requestLogId, enabled }) => useAuditDetail({ requestLogId, enabled }),
      {
        initialProps: {
          requestLogId: 3,
          enabled: true,
        },
      }
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
      expect(result.current.audits).toHaveLength(1);
    });

    rerender({
      requestLogId: 3,
      enabled: false,
    });

    expect(result.current.audits).toEqual([]);
    expect(result.current.error).toBeNull();
    expect(result.current.loading).toBe(false);
  });

  it("returns a structured capture_unavailable error after retries exhaust without audit rows", async () => {
    vi.useFakeTimers();
    api.audit.list.mockResolvedValue({
      items: [],
      total: 0,
      limit: 20,
      offset: 0,
    });

    const { result } = renderHook(() =>
      useAuditDetail({ requestLogId: 42, enabled: true }),
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBe("capture_unavailable");
    expect(result.current.audits).toEqual([]);
    expect(api.audit.list).toHaveBeenCalledTimes(5);
  });

  it("returns a structured load_failed error after repeated audit fetch failures", async () => {
    vi.useFakeTimers();
    api.audit.list.mockRejectedValue(new Error("network down"));

    const { result } = renderHook(() =>
      useAuditDetail({ requestLogId: 43, enabled: true }),
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBe("load_failed");
    expect(result.current.audits).toEqual([]);
    expect(api.audit.list).toHaveBeenCalledTimes(5);
  });
});
