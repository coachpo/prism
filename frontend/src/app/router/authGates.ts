import type { AuthPhase } from "@/context/auth/sessionCoordinator";

export interface RouteLocationState {
  pathname: string;
  search: string;
  hash: string;
}

export interface AuthGateState {
  phase: AuthPhase;
  authEnabled: boolean;
  authenticated: boolean;
  loading: boolean;
}

export function buildAuthReturnState(location: RouteLocationState) {
  return `${location.pathname}${location.search}${location.hash}`;
}

// Safe return URL (SPEC §6.4): only relative same-origin local paths that
// start with a single slash; reject scheme, protocol-relative, backslashes,
// control characters, double-encoding and overlong values.
export function isSafeReturnPath(value: string | null | undefined): boolean {
  if (typeof value !== "string") {
    return false;
  }
  if (value.length === 0 || value.length > 2048) {
    return false;
  }
  if (!value.startsWith("/") || value.startsWith("//") || value.includes("\\")) {
    return false;
  }
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code < 0x20 || code === 0x7f) {
      return false;
    }
  }
  if (/%2f|%5c/i.test(value)) {
    return false;
  }
  return true;
}

// buildSafeReturnState validates and canonicalizes the redirect for the
// login page; failures fall back to the canonical Overview path.
export function buildSafeReturnState(location: RouteLocationState): string {
  const raw = buildAuthReturnState(location);
  if (!isSafeReturnPath(raw)) {
    return "/observe";
  }
  return raw;
}

const PHASE_AWAITING_DATA = new Set([
  "BOOTSTRAPPING",
  "REFRESHING",
  "LOGGING_OUT",
  "AUTH_DISABLED_VERIFYING",
  "AUTH_TRANSITION_FAIL_CLOSED",
  "AUTH_UNAVAILABLE",
  "SESSION_EXPIRED",
]);

// resolveProtectedRedirect returns the login redirect for anonymous
// authenticated instances; every other phase is handled by the global
// access layer (blockers/fallbacks), never by page-level navigation.
export function resolveProtectedRedirect(state: AuthGateState, location: RouteLocationState) {
  const phase = state.phase;
  if (phase.kind === "ANONYMOUS") {
    return {
      to: "/auth/login" as const,
      search: { redirect: buildSafeReturnState(location) },
    };
  }
  return null;
}

// resolvePublicRedirect keeps the login page for anonymous sessions and
// for every awaiting/blocked phase (the global access layer renders the
// right surface); it never silently bounces a disabled instance.
export function resolvePublicRedirect(state: AuthGateState) {
  const phase = state.phase;
  if (phase.kind === "AUTHENTICATED" || phase.kind === "REFRESHING") {
    return "/observe" as const;
  }
  if (phase.kind === "AUTH_DISABLED") {
    return null;
  }
  if (phase.kind === "ANONYMOUS") {
    return null;
  }
  return null;
}

// needsGlobalAccessLayer reports whether the current phase must render the
// global blocker/fallback instead of page content.
export function needsGlobalAccessLayer(phase: AuthPhase): boolean {
  return PHASE_AWAITING_DATA.has(phase.kind);
}
