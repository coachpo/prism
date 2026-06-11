const AUDIT_WINDOW_MS = 12 * 60 * 60 * 1000;

interface RequestLogAuditWindow {
  from: string;
  to: string;
}

export function deriveRequestLogAuditWindow(requestCreatedAt: string | null): RequestLogAuditWindow | null {
  if (!requestCreatedAt) {
    return null;
  }

  const createdTime = Date.parse(requestCreatedAt);
  if (!Number.isFinite(createdTime)) {
    return null;
  }

  return {
    from: new Date(createdTime - AUDIT_WINDOW_MS).toISOString(),
    to: new Date(createdTime + AUDIT_WINDOW_MS).toISOString(),
  };
}
