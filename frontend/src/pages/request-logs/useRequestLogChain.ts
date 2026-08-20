import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { ChainResponse } from "@/lib/types/request-logs";

interface UseRequestLogChainParams {
  ingressRequestId: string | null;
  enabled: boolean;
}

// Retained ingress chain for the detail sheet (Requests SPEC §12.2): the
// server groups all retained rows for one ingress; the sheet shows the
// attempt order, triggers, and chain completeness instead of reconstructing
// a client-side chain from the current page.
export function useRequestLogChain({ ingressRequestId, enabled }: UseRequestLogChainParams) {
  const messages = getStaticMessages();
  const [chain, setChain] = useState<ChainResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const activeIngressRef = useRef<string | null>(null);

  const fetchChain = useCallback(
    async (targetIngress: string) => {
      activeIngressRef.current = targetIngress;
      setLoading(true);
      setError(null);
      setChain(null);
      try {
        const response = await api.stats.chains({
          ingress_request_id: targetIngress,
          view: "ingress_chains",
          chain_limit: 1,
          chain_row_limit: 200,
        });
        if (activeIngressRef.current !== targetIngress) return;
        setChain(response);
      } catch (err) {
        if (activeIngressRef.current !== targetIngress) return;
        setError(err instanceof Error ? err.message : messages.requestLogs.loadFailed);
      } finally {
        if (activeIngressRef.current === targetIngress) {
          setLoading(false);
        }
      }
    },
    [messages.requestLogs.loadFailed],
  );

  useEffect(() => {
    if (!enabled || !ingressRequestId) {
      activeIngressRef.current = null;
      setChain(null);
      setLoading(false);
      setError(null);
      return;
    }
    const timeoutId = setTimeout(() => {
      void fetchChain(ingressRequestId);
    }, 0);
    return () => {
      clearTimeout(timeoutId);
      activeIngressRef.current = null;
    };
  }, [enabled, fetchChain, ingressRequestId]);

  return { chain, loading, error };
}
