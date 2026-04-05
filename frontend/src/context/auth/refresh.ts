import type { SessionResponse } from "@/lib/types";

export const PROACTIVE_REFRESH_MS = 12 * 60 * 1000;

export function shouldRunProactiveRefresh(
  authenticated: boolean,
  authEnabled: boolean,
): boolean {
  return authenticated && authEnabled;
}

export function shouldRefreshOnVisibilityChange(
  visibilityState: DocumentVisibilityState,
  authenticated: boolean,
  authEnabled: boolean,
): boolean {
  return visibilityState === "visible" && shouldRunProactiveRefresh(authenticated, authEnabled);
}

export async function runPassiveSessionRefresh({
  applySessionState,
  getAuthStateVersion,
  isMutationInFlight,
  refreshSession,
  requestVersion,
}: {
  applySessionState: (session: SessionResponse) => void;
  getAuthStateVersion: () => number;
  isMutationInFlight: () => boolean;
  refreshSession: () => Promise<SessionResponse>;
  requestVersion: number;
}): Promise<void> {
  if (isMutationInFlight()) {
    return;
  }

  try {
    const session = await refreshSession();
    if (isMutationInFlight() || requestVersion !== getAuthStateVersion()) {
      return;
    }

    applySessionState(session);
  } catch {
    return;
  }
}
