/**
 * Audit-row presentation helpers for the Requests/Audit v2 projection.
 *
 * The audit API mirrors the request-log v2 scoping contract: status and
 * duration are exposed per scope (upstream / gateway / legacy) and the row
 * kind decides which one a reader sees. Keep it strict per
 * pages/request-logs/AGENTS.md — never COALESCE across scopes.
 */

export type AuditScopedRow = Pick<
  { row_kind: string; upstream_status_code: number | null; gateway_status_code: number | null; legacy_status_code: number | null },
  "row_kind" | "upstream_status_code" | "gateway_status_code" | "legacy_status_code"
>;

export type AuditScopedDurationRow = Pick<
  { row_kind: string; attempt_duration_ms: number | null; legacy_duration_ms: number | null },
  "row_kind" | "attempt_duration_ms" | "legacy_duration_ms"
>;

export function auditScopedStatusCode(row: AuditScopedRow): number | null {
  switch (row.row_kind) {
    case "upstream":
      return row.upstream_status_code;
    case "planning":
    case "admission":
      return row.gateway_status_code;
    default:
      return row.legacy_status_code;
  }
}

export function auditScopedDurationMs(row: AuditScopedDurationRow): number | null {
  return row.row_kind === "upstream" ? row.attempt_duration_ms : row.legacy_duration_ms;
}

export interface AuditBodyText {
  text: string | null;
  binary: boolean;
}

/**
 * The backend stores captured bodies as bytea and exposes them base64-encoded
 * (utf8 for every body the runtime captures). Decode to the text the payload
 * viewer renders; non-UTF-8 bytes resolve to a binary marker instead of
 * mojibake. A missing or invalid payload resolves to "not stored".
 */
export function decodeAuditBodyBase64(base64: string | null | undefined): AuditBodyText {
  if (!base64) {
    return { text: null, binary: false };
  }

  let raw: string;
  try {
    raw = atob(base64);
  } catch {
    return { text: null, binary: true };
  }

  const bytes = new Uint8Array(raw.length);
  for (let index = 0; index < raw.length; index += 1) {
    bytes[index] = raw.charCodeAt(index);
  }

  try {
    return { text: new TextDecoder("utf-8", { fatal: true }).decode(bytes), binary: false };
  } catch {
    return { text: null, binary: true };
  }
}