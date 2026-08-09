import { describe, expect, it } from "vitest";
import {
  generatedProxyKeyInitialState,
  generatedProxyKeyReducer,
  type GeneratedProxyKeySession,
} from "@/features/proxy-keys/generatedSecretSession";

function session(overrides: Partial<GeneratedProxyKeySession> = {}): GeneratedProxyKeySession {
  return {
    source: "create",
    keyId: 42,
    itemSnapshot: { id: 42, name: "prod", key_prefix: "pm-1a2b", key_preview: "pm-1a2b••••••••9f3e", is_active: true, expires_at: null, last_used_at: null, last_used_ip: null, notes: null, rotated_from_id: null, created_at: "2026-08-09T00:00:00Z", updated_at: "2026-08-09T00:00:00Z" },
    rawKey: "pm-secret-value",
    capacity: { limit: 100, used: 1, remaining: 99, counted_at: "2026-08-09T00:00:00Z" },
    openedAt: 1234,
    savedAcknowledged: false,
    ...overrides,
  };
}

describe("generated proxy key secret session reducer", () => {
  it("moves idle -> unacknowledged on CREATE_SUCCEEDED before any query effect", () => {
    const next = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session(),
    });
    expect(next.kind).toBe("unacknowledged");
    if (next.kind === "unacknowledged") {
      expect(next.session.rawKey).toBe("pm-secret-value");
      expect(next.session.savedAcknowledged).toBe(false);
    }
  });

  it("moves idle -> unacknowledged on ROTATE_SUCCEEDED", () => {
    const next = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "ROTATE_SUCCEEDED",
      session: session({ source: "rotate", keyId: 43 }),
    });
    expect(next.kind).toBe("unacknowledged");
    if (next.kind === "unacknowledged") {
      expect(next.session.source).toBe("rotate");
      expect(next.session.keyId).toBe(43);
    }
  });

  it("never overwrites an unacknowledged session with a second mutation", () => {
    const first = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session({ rawKey: "first-secret" }),
    });
    const second = generatedProxyKeyReducer(first, {
      type: "CREATE_SUCCEEDED",
      session: session({ keyId: 99, rawKey: "second-secret" }),
    });
    expect(second.kind).toBe("unacknowledged");
    if (second.kind === "unacknowledged") {
      expect(second.session.rawKey).toBe("first-secret");
    }
  });

  it("sets the saved acknowledgement without acknowledging via copy", () => {
    const created = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session(),
    });
    const acked = generatedProxyKeyReducer(created, { type: "SET_SAVED_ACK", acknowledged: true });
    if (acked.kind === "unacknowledged") {
      expect(acked.session.savedAcknowledged).toBe(true);
    }
  });

  it("FINISH only releases references when acknowledged", () => {
    const created = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session(),
    });
    const prematureFinish = generatedProxyKeyReducer(created, { type: "FINISH" });
    expect(prematureFinish.kind).toBe("unacknowledged");

    const acked = generatedProxyKeyReducer(created, { type: "SET_SAVED_ACK", acknowledged: true });
    const finished = generatedProxyKeyReducer(acked, { type: "FINISH" });
    expect(finished.kind).toBe("idle");
  });

  it("REQUEST_CLOSE moves to closing_confirm with the intent", () => {
    const created = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session(),
    });
    const closing = generatedProxyKeyReducer(created, { type: "REQUEST_CLOSE", intent: "navigate" });
    expect(closing.kind).toBe("closing_confirm");
    if (closing.kind === "closing_confirm") {
      expect(closing.intent).toBe("navigate");
      expect(closing.session.rawKey).toBe("pm-secret-value");
    }
  });

  it("KEEP_EDITING returns to unacknowledged", () => {
    const created = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session(),
    });
    const closing = generatedProxyKeyReducer(created, { type: "REQUEST_CLOSE", intent: "close" });
    const kept = generatedProxyKeyReducer(closing, { type: "KEEP_EDITING" });
    expect(kept.kind).toBe("unacknowledged");
  });

  it("ABANDON_AND_LEAVE releases the raw key and returns to idle", () => {
    const created = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session(),
    });
    const closing = generatedProxyKeyReducer(created, { type: "REQUEST_CLOSE", intent: "navigate" });
    const abandoned = generatedProxyKeyReducer(closing, { type: "ABANDON_AND_LEAVE" });
    expect(abandoned.kind).toBe("idle");
  });

  it("is immune to list/auth/model/stats refetch events (no such transitions exist)", () => {
    // The reducer only accepts mutation/session events; a refetch cannot be
    // dispatched into it. This documents the query independence contract:
    // loading/error/refetch/reorder/omission of the key in the ledger never
    // touches the session state.
    const created = generatedProxyKeyReducer(generatedProxyKeyInitialState, {
      type: "CREATE_SUCCEEDED",
      session: session(),
    });
    expect(created.kind).toBe("unacknowledged");
  });
});
