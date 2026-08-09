/**
 * Generated-proxy-key secret session state machine (Proxy Key SPEC §10).
 *
 * The session is created exclusively from a create/rotate mutation response
 * and lives in the proxy-keys feature boundary — never in React Query data,
 * mutation data, list membership or Dialog mount cleanup. Query refetches,
 * failures, reordering or missing items must not affect it; only an explicit
 * saved acknowledgement (or a deliberate abandon) destroys the raw key.
 */

import type { ProxyApiKey, ProxyKeyCapacity } from "@/lib/types";

export interface GeneratedProxyKeySession {
  source: "create" | "rotate";
  keyId: number;
  itemSnapshot: ProxyApiKey;
  rawKey: string;
  capacity: ProxyKeyCapacity;
  openedAt: number;
  savedAcknowledged: boolean;
}

export type GeneratedProxyKeyState =
  | { kind: "idle" }
  | { kind: "unacknowledged"; session: GeneratedProxyKeySession }
  | { kind: "closing_confirm"; session: GeneratedProxyKeySession; intent: "close" | "navigate" };

export type GeneratedProxyKeyEvent =
  | { type: "CREATE_SUCCEEDED"; session: GeneratedProxyKeySession }
  | { type: "ROTATE_SUCCEEDED"; session: GeneratedProxyKeySession }
  | { type: "SET_SAVED_ACK"; acknowledged: boolean }
  | { type: "REQUEST_CLOSE"; intent: "close" | "navigate" }
  | { type: "KEEP_EDITING" }
  | { type: "ABANDON_AND_LEAVE" }
  | { type: "FINISH" };

export const generatedProxyKeyInitialState: GeneratedProxyKeyState = { kind: "idle" };

export function generatedProxyKeyReducer(
  state: GeneratedProxyKeyState,
  event: GeneratedProxyKeyEvent,
): GeneratedProxyKeyState {
  switch (event.type) {
    case "CREATE_SUCCEEDED":
    case "ROTATE_SUCCEEDED":
      // Mutations stay disabled while a session is unacknowledged, so there
      // is no second-slot queue that could overwrite the first raw key.
      if (state.kind === "unacknowledged" || state.kind === "closing_confirm") {
        return state;
      }
      return { kind: "unacknowledged", session: event.session };

    case "SET_SAVED_ACK":
      if (state.kind !== "unacknowledged") {
        return state;
      }
      return {
        kind: "unacknowledged",
        session: { ...state.session, savedAcknowledged: event.acknowledged },
      };

    case "REQUEST_CLOSE":
      if (state.kind === "unacknowledged") {
        return { kind: "closing_confirm", session: state.session, intent: event.intent };
      }
      return state;

    case "KEEP_EDITING":
      if (state.kind === "closing_confirm") {
        return { kind: "unacknowledged", session: state.session };
      }
      return state;

    case "ABANDON_AND_LEAVE":
      // Releases every raw-key/curl reference; the key is unrecoverable.
      return { kind: "idle" };

    case "FINISH":
      // Only reachable with savedAcknowledged=true (enforced by the UI).
      if (state.kind === "unacknowledged" && state.session.savedAcknowledged) {
        return { kind: "idle" };
      }
      return state;

    default:
      return state;
  }
}
