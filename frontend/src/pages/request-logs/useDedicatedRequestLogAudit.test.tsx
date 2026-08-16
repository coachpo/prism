import { renderHook, waitFor } from "@testing-library/react";
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

describe("dedicated request-log audit lookup", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("passes a BIGINT request-log ID unchanged through both API calls", async () => {
    const requestLogId = "9007199254740997";
    mocks.requestDetail.mockResolvedValue({
      summary: {
        api_family: "openai",
        created_at: "2026-08-13T12:00:00Z",
        request_log_id: requestLogId,
      },
      request: { operation_name: "openai.chat_completions" },
      routing: {
        audit_capture_bodies_at_request: true,
        audit_enabled_at_request: true,
      },
    });
    mocks.auditList.mockResolvedValue({
      has_more: false,
      items: [{ id: 201, request_log_id: requestLogId }],
      next_cursor: null,
    });
    mocks.auditDetail.mockResolvedValue({ id: 201, request_log_id: requestLogId });

    const { result } = renderHook(() => useDedicatedRequestLogAudit({
      requestId: requestLogId,
      selectedAuditId: null,
      selectedAuditParamLabel: null,
      selectedAuditParamPresent: false,
    }));

    await waitFor(() => expect(result.current.status).toBe("ready"));
    expect(mocks.requestDetail).toHaveBeenCalledWith(requestLogId);
    expect(mocks.auditList).toHaveBeenCalledWith(requestLogId, {
      from: "2026-08-13T00:00:00.000Z",
      to: "2026-08-14T00:00:00.000Z",
      limit: 20,
      cursor: undefined,
    });
    expect(mocks.auditDetail).toHaveBeenCalledWith(201);
    expect(result.current.request?.summary.request_log_id).toBe(requestLogId);
  });
});
