import type { AuditLogDetail } from "@/lib/types";
import { decodeAuditBodyBase64 } from "./auditLogView";
import { buildPayloadViewModel } from "./detail/payloadDocumentViewModel";

export type ComparisonDirection = "request" | "response";
export function comparisonBody(
  detail: AuditLogDetail,
  direction: ComparisonDirection,
) {
  const stored = detail[`${direction}_body_stored`];
  const encoded = detail[`${direction}_body_base64`];
  const decoded =
    stored && encoded === ""
      ? { text: "", binary: false }
      : decodeAuditBodyBase64(encoded);
  const view =
    decoded.text === null
      ? null
      : buildPayloadViewModel(decoded.text, "openai", direction, null);
  return {
    text: stored && !decoded.binary && view?.readableText ? decoded.text : null,
    binary: decoded.binary || view?.readableText === false,
    truncated: detail[`${direction}_body_truncated`],
    observed: detail[`${direction}_body_bytes_observed`],
    stored: detail[`${direction}_body_bytes_stored`],
    status: detail[`${direction}_body_capture_status`],
    provenance: detail[`${direction}_body_capture_provenance`],
    endState: detail[`${direction}_body_capture_end_state`],
  };
}

/** Bounded positional diff of actual retained text; never reconstruct omitted payloads. */
export function compareRetainedLines(left: string, right: string) {
  const a = left.split("\n");
  const b = right.split("\n");
  const lines: {
    number: number;
    same: boolean;
    left: string | null;
    right: string | null;
  }[] = [];
  for (let i = 0; i < Math.min(200, Math.max(a.length, b.length)); i++) {
    lines.push({
      number: i + 1,
      same: a[i] === b[i],
      left: a[i]?.slice(0, 2000) ?? null,
      right: b[i]?.slice(0, 2000) ?? null,
    });
  }
  return { equal: left === right, lines };
}
