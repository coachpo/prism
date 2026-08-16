// Auth endpoint exemption matcher (SPEC §5.1): the listed paths are
// permanently exempt from the generic 401 refresh interception and are
// handled by their own auth bootstrap/mutation flows. The operation-status
// entry is an exact method+path match, never a string prefix.

const AUTH_EXEMPT_PATHS = new Set([
  "/api/auth/status",
  "/api/auth/public-bootstrap",
  "/api/auth/session",
  "/api/auth/login",
  "/api/auth/logout",
  "/api/auth/refresh",
]);

const AUTH_OPERATION_STATUS_PATH_PATTERN =
  /^\/api\/auth\/operations\/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\/status$/;

// isAuthExemptPath matches the permanent auth-exempt management paths.
export function isAuthExemptPath(path: string): boolean {
  return AUTH_EXEMPT_PATHS.has(path);
}

// isPublicAuthOperationStatusPath matches the canonical public operation
// status route exactly: GET, same-origin effective path, no query/fragment,
// no percent-encoding, trailing slash or extra segments. The client cannot
// observe fragments; it validates everything the server can also observe.
export function isPublicAuthOperationStatusPath(
  method: string,
  pathname: string,
  search: string,
  effectiveOrigin: string,
): boolean {
  if (method !== "GET") {
    return false;
  }
  if (search !== "") {
    return false;
  }
  if (!pathname.startsWith("/") || pathname.includes("//") || pathname.includes("%") || pathname.endsWith("/")) {
    return false;
  }
  if (typeof window !== "undefined" && window.location.origin !== effectiveOrigin) {
    return false;
  }
  return AUTH_OPERATION_STATUS_PATH_PATTERN.test(pathname);
}

// parsePublicAuthOperationID extracts the canonical lowercase UUID when the
// path matches the exact public operation-status shape.
export function parsePublicAuthOperationID(pathname: string): string | null {
  const match = AUTH_OPERATION_STATUS_PATH_PATTERN.exec(pathname);
  if (!match) {
    return null;
  }
  return match[1].toLowerCase();
}
