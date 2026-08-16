import type { AuditLogDetail } from "@/lib/types";
import type { RequestLogDetailV2 } from "@/lib/types/request-logs-v2";

type AuditCaptureProvenance =
  | Pick<RequestLogDetailV2["routing"], "audit_enabled_at_request" | "audit_capture_bodies_at_request">
  | Pick<AuditLogDetail, "audit_enabled_at_request" | "audit_capture_bodies_at_request">;

export type RequestAuditCaptureMode = "disabled" | "metadata_only" | "full";
export type AuditDetailState = RequestAuditCaptureMode | "load_failed";

export function resolveRequestAuditCaptureMode(
  provenance: AuditCaptureProvenance,
): RequestAuditCaptureMode {
  if (!provenance.audit_enabled_at_request) {
    return "disabled";
  }

  return provenance.audit_capture_bodies_at_request ? "full" : "metadata_only";
}
