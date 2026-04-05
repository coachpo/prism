import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { LoadbalanceCurrentStateItem } from "@/lib/types";

interface UseModelLoadbalanceCurrentStateInput {
  modelConfigId: number | undefined;
  revision: number;
  enabled?: boolean;
}

function toCurrentStateMap(items: LoadbalanceCurrentStateItem[]) {
  return new Map(items.map((item) => [item.connection_id, item]));
}

export function useModelLoadbalanceCurrentState({
  modelConfigId,
  revision,
  enabled = true,
}: UseModelLoadbalanceCurrentStateInput) {
  const [currentStateByConnectionId, setCurrentStateByConnectionId] = useState<
    Map<number, LoadbalanceCurrentStateItem>
  >(new Map());
  const [resettingConnectionIds, setResettingConnectionIds] = useState<Set<number>>(
    new Set()
  );
  const requestIdRef = useRef(0);
  const resetKey = `${enabled ? modelConfigId ?? "none" : "disabled"}:${revision}`;
  const resetKeyRef = useRef(resetKey);

  const fetchCurrentState = useCallback(async () => {
    if (!enabled || typeof modelConfigId !== "number" || !Number.isFinite(modelConfigId)) {
      requestIdRef.current += 1;
      setCurrentStateByConnectionId(new Map());
      return;
    }

    const resolvedModelConfigId = modelConfigId;

    const requestId = ++requestIdRef.current;

    try {
      const data = await api.loadbalance.listCurrentState({
        model_config_id: resolvedModelConfigId,
      });

      if (requestId !== requestIdRef.current) {
        return;
      }

      setCurrentStateByConnectionId(toCurrentStateMap(data.items));
    } catch (error) {
      if (requestId !== requestIdRef.current) {
        return;
      }

      toast.error(
        error instanceof Error ? error.message : getStaticMessages().modelDetailData.loadRecoveryStateFailed
      );
      console.error("Failed to load model loadbalance current state", error);
    }
  }, [enabled, modelConfigId]);

  const resetCooldown = useCallback(async (connectionId: number) => {
    setResettingConnectionIds((current) => {
      const next = new Set(current);
      next.add(connectionId);
      return next;
    });

    try {
      await api.loadbalance.resetCurrentState(connectionId);
      requestIdRef.current += 1;
      setCurrentStateByConnectionId((current) => {
        if (!current.has(connectionId)) {
          return current;
        }

        const next = new Map(current);
        next.delete(connectionId);
        return next;
      });
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : getStaticMessages().modelDetailData.resetRecoveryStateFailed
      );
    } finally {
      setResettingConnectionIds((current) => {
        const next = new Set(current);
        next.delete(connectionId);
        return next;
      });
    }
  }, []);

  useEffect(() => {
    if (resetKeyRef.current !== resetKey) {
      resetKeyRef.current = resetKey;
      requestIdRef.current += 1;
      setCurrentStateByConnectionId(new Map());
      setResettingConnectionIds(new Set());
    }

    void fetchCurrentState();
  }, [fetchCurrentState, resetKey]);

  return {
    currentStateByConnectionId,
    resettingConnectionIds,
    refreshCurrentState: fetchCurrentState,
    resetCooldown,
  };
}
