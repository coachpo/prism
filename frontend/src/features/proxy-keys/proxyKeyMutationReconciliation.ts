import type { QueryClient } from "@tanstack/react-query";

import type { ProxyApiKey, ProxyKeyCapacity } from "@/lib/types";
import { rewriteQueryKeys } from "@/shared/api/queryKeys";

type ProxyApiKeyListData = {
  items: ProxyApiKey[];
  capacity: ProxyKeyCapacity;
};

function updateProxyKeyLedger(
  queryClient: QueryClient,
  items: (current: ProxyApiKey[]) => ProxyApiKey[],
  capacity: ProxyKeyCapacity,
  invalidate: boolean,
) {
  const queryKey = rewriteQueryKeys.global.proxyApiKeys();
  queryClient.setQueryData<ProxyApiKeyListData>(queryKey, (current) => ({
    items: items(current?.items ?? []),
    capacity,
  }));
  if (invalidate) {
    void queryClient.invalidateQueries({ queryKey });
  }
}

export function reconcileProxyKeyLedgerAfterCreateOrRotate(
  queryClient: QueryClient,
  item: ProxyApiKey,
  capacity: ProxyKeyCapacity,
) {
  updateProxyKeyLedger(
    queryClient,
    (current) => [
      item,
      ...current.filter((existing) => existing.id !== item.id),
    ],
    capacity,
    true,
  );
}

export function reconcileProxyKeyLedgerAfterUpdate(
  queryClient: QueryClient,
  item: ProxyApiKey,
  capacity: ProxyKeyCapacity,
) {
  updateProxyKeyLedger(
    queryClient,
    (current) =>
      current.map((existing) => (existing.id === item.id ? item : existing)),
    capacity,
    false,
  );
}

export function reconcileProxyKeyLedgerAfterDelete(
  queryClient: QueryClient,
  keyId: number,
  capacity: ProxyKeyCapacity,
) {
  updateProxyKeyLedger(
    queryClient,
    (current) => current.filter((existing) => existing.id !== keyId),
    capacity,
    true,
  );
}
