/**
 * Effective runtime origin resolution (Proxy Key SPEC §8.1).
 *
 * The management API base may be a standalone absolute backend origin
 * (VITE_API_BASE) or same-origin (launcher/container deployments). Runtime
 * URLs are built from the same effective backend origin — never from the
 * visible dashboard origin when a standalone backend is configured, and never
 * by string-replacing "/api" out of an arbitrary URL.
 */

// Test-only override: vitest/vite-node give every module its own import.meta
// env copy, so environment stubbing cannot reach this module. Product code
// never calls this; tests use it to exercise the configured-base contract.
let apiBaseTestOverride: string | null | undefined;

export function __setApiBaseForTest(value: string | null | undefined): void {
  apiBaseTestOverride = value;
  cachedApiBase = undefined;
}

function configuredApiBaseValue(): string | undefined {
  if (apiBaseTestOverride !== undefined) {
    return apiBaseTestOverride ?? undefined;
  }
  const env = (import.meta as unknown as { env?: Record<string, unknown> }).env;
  const raw = env?.VITE_API_BASE;
  return typeof raw === "string" ? raw : undefined;
}

function parseConfiguredApiBase(): URL | null {
  const rawApiBase = configuredApiBaseValue();
  if (rawApiBase === undefined) {
    return null;
  }
  const trimmed = rawApiBase.trim();
  if (trimmed === "") {
    return null;
  }
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    // Build-time/startup validation is the frontend's contract boundary; a
    // malformed configured base must not silently fall back to same-origin.
    throw new Error(`Invalid VITE_API_BASE: ${trimmed}`);
  }
  if (
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.search !== "" ||
    parsed.hash !== "" ||
    (parsed.pathname !== "/" && parsed.pathname !== "")
  ) {
    throw new Error(`Invalid VITE_API_BASE: ${trimmed}`);
  }
  return parsed;
}

let cachedApiBase: URL | null | undefined;

/**
 * Returns the absolute backend origin (no trailing path) used for both
 * management and runtime URLs.
 */
export function getEffectiveBackendOrigin(): URL {
  if (cachedApiBase === undefined) {
    cachedApiBase = parseConfiguredApiBase();
  }
  if (cachedApiBase !== null) {
    return new URL(cachedApiBase.origin);
  }
  return new URL(sameOriginValue());
}

function sameOriginValue(): string {
  const location = globalThis.location;
  if (location && typeof location.origin === "string") {
    return location.origin;
  }
  return "http://localhost";
}

export function resetEffectiveBackendOriginCache(): void {
  cachedApiBase = undefined;
}

/**
 * Builds an absolute runtime URL against the effective backend origin.
 * Never uses string replacement; always URL resolution.
 */
export function buildRuntimeUrl(path: string): URL {
  const origin = getEffectiveBackendOrigin();
  const resolved = new URL(path, origin);
  // The resolved URL must stay under the same origin; a path like "/v1/..."
  // already is, but guard against accidental absolute-path escapes.
  if (resolved.origin !== origin.origin) {
    throw new Error(`Runtime path escapes backend origin: ${path}`);
  }
  return resolved;
}
