import { useProxyKeyLedger } from "./useProxyKeyLedger";
import { useProxyKeyMutations } from "./useProxyKeyMutations";
import { useProxyKeySecretSession } from "./useProxyKeySecretSession";

/**
 * Compose the proxy-key page's ledger/query, mutation, and one-time-secret
 * lifecycles. No raw secret or independent workflow state is owned here.
 */
export function useProxyKeysFeatureData() {
  const ledger = useProxyKeyLedger();
  const secret = useProxyKeySecretSession();
  const { showCreatedSecret, showRotatedSecret, ...secretPage } = secret;
  const mutations = useProxyKeyMutations({
    authSettings: ledger.authSettings,
    capacity: ledger.capacity,
    proxyKeyLimit: ledger.proxyKeyLimit,
    remainingKeys: ledger.remainingKeys,
    showCreatedSecret,
    showRotatedSecret,
  });

  return { ...ledger, ...mutations, ...secretPage };
}
