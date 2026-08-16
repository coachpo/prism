/**
 * Header scrub matcher — mirrors backend/internal/domain/audit/scrub.go so the
 * browser applies the same name+value matcher for legacy/deep-defense. Any
 * change here must keep parity with the backend matcher.
 */

export const HEADER_SCRUB_SENTINEL = "[REDACTED]";

const SENSITIVE_HEADER_NAMES = new Set([
  "authorization",
  "proxy-authorization",
  "cookie",
  "set-cookie",
  "x-api-key",
  "x-goog-api-key",
  "x-amz-security-token",
  "x-auth-token",
  "www-authenticate",
  "proxy-authenticate",
  "api-key",
  "private-key",
  "x-azure-key",
  "x-rapidapi-key",
  "x-rbl-key",
  "x-ms-secret",
  "x-client-secret",
  "x-vercel-secret",
  "x-vercel-token",
  "x-vercel-api-key",
  "x-token",
  "access-token",
  "refresh-token",
  "id-token",
  "session-token",
  "client-secret",
  "client-id-secret",
  "slack-signing-secret",
  "stripe-signature",
  "x-paypal-client-secret",
]);

const SENSITIVE_NAME_PARTS = [
  "api-key",
  "apikey",
  "token",
  "secret",
  "credential",
  "password",
  "private-key",
  "signature",
];

const VALUE_PATTERNS = [
  /\bBearer\s+[A-Za-z0-9._~+/=-]{6,}/i,
  /\bBasic\s+[A-Za-z0-9+/=]{8,}/i,
  /\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/,
  /(?:password|passwd|pwd|secret|api[_-]?key|token|credential|private[_-]?key|access[_-]?key)\s*[=:]\s*["']?[^\s"',;]+/i,
  /\b(?:sk|pk|rk|AKIA)[A-Za-z0-9]{16,}\b/,
  /\b(?:sk|pk|rk)-[A-Za-z0-9_-]{12,}\b/,
];

export function headerNameSensitive(name: string): boolean {
  const normalized = name.trim().toLowerCase();
  if (normalized.length === 0) return false;
  if (SENSITIVE_HEADER_NAMES.has(normalized)) return true;
  return SENSITIVE_NAME_PARTS.some((part) => normalized.includes(part));
}

export function headerValueSensitive(value: string): boolean {
  return VALUE_PATTERNS.some((pattern) => pattern.test(value));
}

export function scrubHeaderValue(name: string, value: string): string {
  if (headerNameSensitive(name)) return HEADER_SCRUB_SENTINEL;
  if (headerValueSensitive(value)) return HEADER_SCRUB_SENTINEL;
  return value;
}

export function scrubHeaderMap(headers: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(headers).map(([name, value]) => [name, scrubHeaderValue(name, String(value))]),
  );
}
