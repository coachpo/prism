export function parseRequestLogIdParam(value: string | null | undefined): string | null {
  if (!value) return null;

  const requestLogId = value.trim().replace(/^#/, "");
  if (!/^\d+$/.test(requestLogId) || /^0+$/.test(requestLogId)) return null;
  return requestLogId;
}

export function getSelectedAuditPath(
  requestLogId: string,
  auditId: number,
  cursor: string | null,
): string {
  const params = new URLSearchParams({ audit_id: String(auditId) });
  if (cursor) params.set("cursor", cursor);
  return `/observe/requests/${requestLogId}/audit?${params.toString()}`;
}

export function getAuditPagePath(requestLogId: string, cursor: string | null): string {
  if (!cursor) return `/observe/requests/${requestLogId}/audit`;
  const params = new URLSearchParams({ cursor });
  return `/observe/requests/${requestLogId}/audit?${params.toString()}`;
}
