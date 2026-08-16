import { describe, expect, it } from "vitest";
import {
  getAuditPagePath,
  getSelectedAuditPath,
  parseRequestLogIdParam,
} from "./requestLogAuditRoute";

describe("dedicated request-log audit routes", () => {
  it("preserves a BIGINT request-log ID as a decimal string", () => {
    const requestLogId = "9007199254740997";

    expect(parseRequestLogIdParam(requestLogId)).toBe(requestLogId);
    expect(getAuditPagePath(requestLogId, null)).toBe(
      `/observe/requests/${requestLogId}/audit`,
    );
    expect(getAuditPagePath(requestLogId, "page-2")).toBe(
      `/observe/requests/${requestLogId}/audit?cursor=page-2`,
    );
    expect(getSelectedAuditPath(requestLogId, 201, "page-2")).toBe(
      `/observe/requests/${requestLogId}/audit?audit_id=201&cursor=page-2`,
    );
  });

  it("rejects missing, non-decimal, and zero request-log IDs", () => {
    expect(parseRequestLogIdParam(undefined)).toBeNull();
    expect(parseRequestLogIdParam("12.5")).toBeNull();
    expect(parseRequestLogIdParam("0")).toBeNull();
  });
});
