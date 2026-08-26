import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ProxyApiKeyCreateResponse } from "@/lib/types";
import { useProxyKeySecretSession } from "./useProxyKeySecretSession";

describe("useProxyKeySecretSession", () => {
  it("hands the create response into the guarded one-time session", () => {
    const created = {
      key: "pm-secret-value",
      item: { id: 7 } as ProxyApiKeyCreateResponse["item"],
      capacity: {
        limit: 10,
        used: 1,
        remaining: 9,
        counted_at: "2026-08-26T00:00:00Z",
      },
    } as ProxyApiKeyCreateResponse;
    const { result } = renderHook(() => useProxyKeySecretSession());

    act(() => {
      result.current.showCreatedSecret(created);
    });

    expect(result.current.secretSession.kind).toBe("unacknowledged");
    if (result.current.secretSession.kind === "unacknowledged") {
      expect(result.current.secretSession.session.keyId).toBe(7);
      expect(result.current.secretSession.session.rawKey).toBe(
        "pm-secret-value",
      );
      expect(result.current.secretSession.session.savedAcknowledged).toBe(false);
    }
  });
});
