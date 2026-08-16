// Cross-tab auth broadcast (SPEC §11): the payload carries event_id
// (crypto.randomUUID(), never Date.now() as a unique id), origin_tab_id, a
// per-origin increasing sequence, the non-secret session_generation_id and
// the kind. It never carries username, token, secret or page data. Remote
// events only execute local coordinator side effects and are never
// re-broadcast.

import type { AuthClientEvent } from "./sessionCoordinator";


export const AUTH_STATE_BROADCAST_KEY = "prism.authStateVersion";

export type AuthBroadcastKind = "session_expired" | "auth_changed";

export interface AuthBroadcastPayload {
  event_id: string;
  origin_tab_id: string;
  sequence: number;
  session_generation_id: string;
  kind: AuthBroadcastKind;
  target_generation?: string;
}

let originTabId = "";
let sequenceCounter = 0;

export function getOriginTabId(): string {
  if (originTabId === "") {
    originTabId = randomUUID();
  }
  return originTabId;
}

export function nextSequence(): number {
  sequenceCounter += 1;
  return sequenceCounter;
}

export function randomUUID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

// broadcastAuthStateChange writes the payload locally first (dedupe before
// broadcast to avoid loops) and fires the storage event for other tabs.
export function broadcastAuthStateChange(
  sessionGenerationId: string,
  kind: AuthBroadcastKind,
  targetGeneration?: string,
): void {
  if (typeof window === "undefined") {
    return;
  }
  const payload: AuthBroadcastPayload = {
    event_id: randomUUID(),
    origin_tab_id: getOriginTabId(),
    sequence: nextSequence(),
    session_generation_id: sessionGenerationId,
    kind,
    target_generation: targetGeneration,
  };
  try {
    window.localStorage.setItem(AUTH_STATE_BROADCAST_KEY, JSON.stringify(payload));
  } catch {
    // A blocked or unavailable storage area must not turn a successful auth
    // mutation into a failed settings action. The local coordinator still
    // performs the bootstrap fence in the writer tab.
  }
}

export function parseBroadcastPayload(raw: string | null): AuthBroadcastPayload | null {
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as AuthBroadcastPayload;
    if (
      typeof parsed.event_id !== "string" ||
      typeof parsed.origin_tab_id !== "string" ||
      typeof parsed.sequence !== "number" ||
      typeof parsed.session_generation_id !== "string" ||
      (parsed.kind !== "session_expired" && parsed.kind !== "auth_changed")
    ) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

// Dedupe state per tab: bounded seen event ids per origin, per-origin highest
// sequence and the terminal event already consumed for the current session
// generation.
export class BroadcastDedupe {
  private seenEventIds = new Set<string>();
  private highestSequenceByOrigin = new Map<string, number>();
  private consumedTerminalByGeneration = new Map<string, string>();

  seen(eventId: string): boolean {
    if (this.seenEventIds.has(eventId)) {
      return true;
    }
    if (this.seenEventIds.size >= 128) {
      const first = this.seenEventIds.values().next().value;
      if (first !== undefined) {
        this.seenEventIds.delete(first);
      }
    }
    this.seenEventIds.add(eventId);
    return false;
  }

  outOfOrder(originTabId: string, sequence: number): boolean {
    const highest = this.highestSequenceByOrigin.get(originTabId) ?? 0;
    if (sequence <= highest) {
      return true;
    }
    this.highestSequenceByOrigin.set(originTabId, sequence);
    return false;
  }

  terminalConsumed(sessionGenerationId: string, eventId: string): boolean {
    if (this.consumedTerminalByGeneration.get(sessionGenerationId) === eventId) {
      return true;
    }
    this.consumedTerminalByGeneration.set(sessionGenerationId, eventId);
    return false;
  }
}

// buildSessionExpiredEvent converts a validated cross-tab expiry into the
// local coordinator event (return_to stays local; the payload never carries
// it).
export function sessionExpiredEventForCrossTab(observedEpoch: number): AuthClientEvent {
  const path =
    typeof window !== "undefined" ? window.location.pathname + window.location.search + window.location.hash : "/";
  return { type: "SESSION_EXPIRED", observed_epoch: observedEpoch, evidence: "cross_tab", request_path: path };
}
