import { useCallback, useReducer } from "react";

import type {
  ProxyApiKeyCreateResponse,
  ProxyApiKeyRotateResponse,
} from "@/lib/types";
import {
  generatedProxyKeyInitialState,
  generatedProxyKeyReducer,
} from "./generatedSecretSession";

export function useProxyKeySecretSession() {
  const [secretSession, dispatchSecretSession] = useReducer(
    generatedProxyKeyReducer,
    generatedProxyKeyInitialState,
  );
  const showCreatedSecret = useCallback(
    (created: ProxyApiKeyCreateResponse) => {
      dispatchSecretSession({
        type: "CREATE_SUCCEEDED",
        session: {
          source: "create",
          keyId: created.item.id,
          itemSnapshot: created.item,
          rawKey: created.key,
          capacity: created.capacity,
          openedAt: Date.now(),
          savedAcknowledged: false,
        },
      });
    },
    [],
  );
  const showRotatedSecret = useCallback(
    (rotated: ProxyApiKeyRotateResponse) => {
      dispatchSecretSession({
        type: "ROTATE_SUCCEEDED",
        session: {
          source: "rotate",
          keyId: rotated.item.id,
          itemSnapshot: rotated.item,
          rawKey: rotated.key,
          capacity: rotated.capacity,
          openedAt: Date.now(),
          savedAcknowledged: false,
        },
      });
    },
    [],
  );

  return {
    dispatchSecretSession,
    secretSession,
    showCreatedSecret,
    showRotatedSecret,
  };
}
