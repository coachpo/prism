import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { GlobalCurrentStateItem, LoadbalanceCurrentStateItem } from "@/lib/types";

interface UseModelLoadbalanceCurrentStateInput {
  /** Public model id; the global current-state read model filters by it. */
  modelId: string | undefined;
  revision: number;
  enabled?: boolean;
}

// The model-detail target summary consumes the per-target projection of the
// shared global current-state read model (SPEC §6): rows for this model are
// bridged to the target-keyed shape without a second fetch owner.
function toCurrentStateMap(items: GlobalCurrentStateItem[]) {
  const map = new Map<number, LoadbalanceCurrentStateItem>();
  for (const item of items) {
    // The target identity is required by the global read model. Keep a
    // defensive connection_id fallback for mocked/legacy rows so one
    // malformed item cannot blank the whole model detail page.
    const legacyItem = item as GlobalCurrentStateItem & { connection_id?: unknown };
    const targetId = item.terminal_target?.id
      ?? (typeof legacyItem.connection_id === "number" ? legacyItem.connection_id : null);
    if (targetId == null) {
      continue;
    }
    // The global read model deliberately uses nullable fields for
    // unobserved/partial rows. Do not manufacture zero counters or an
    // available state; omitting an unproven row lets the detail surface keep
    // its explicit unobserved/unknown presentation.
    if (
      item.observation_state !== "observed" ||
      item.qps_window_request_count === null ||
      item.in_flight_non_stream === null ||
      item.in_flight_stream === null ||
      item.cycle_retry_attempts === null ||
      item.cumulative_retry_attempts === null ||
      item.last_retry_delay_ms === null ||
      item.ban_mode === null ||
      item.state === null ||
      item.created_at === null ||
      item.updated_at === null
    ) {
      continue;
    }
    map.set(targetId, {
      connection_id: targetId,
      window_started_at: item.qps_window_started_at,
      window_request_count: item.qps_window_request_count,
      in_flight_non_stream: item.in_flight_non_stream,
      in_flight_stream: item.in_flight_stream,
      cycle_retry_attempts: item.cycle_retry_attempts,
      cumulative_retry_attempts: item.cumulative_retry_attempts,
      next_retry_at: item.next_retry_at,
      last_retry_delay_ms: item.last_retry_delay_ms,
      ban_mode: item.ban_mode,
      banned_until_at: item.banned_until_at,
      last_failure_kind: item.last_failure_kind,
      last_success_at: item.last_success_at,
      last_success_response_headers_latency_ms: item.last_success_response_headers_latency_ms,
      state: item.state,
      created_at: item.created_at,
      updated_at: item.updated_at,
    });
  }
  return map;
}

export function useModelLoadbalanceCurrentState({
  modelId,
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
  const resetKey = `${enabled ? modelId ?? "none" : "disabled"}:${revision}`;
  const resetKeyRef = useRef(resetKey);

  const fetchCurrentState = useCallback(async () => {
    if (!enabled || !modelId || modelId.trim() === "") {
      requestIdRef.current += 1;
      setCurrentStateByConnectionId(new Map());
      return;
    }

    const resolvedModelId = modelId.trim();

    const requestId = ++requestIdRef.current;

    try {
      const data = await api.loadbalance.listCurrentState({
        model_id: resolvedModelId,
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
        error instanceof Error ? error.message : getStaticMessages().modelDetailData.loadBanPolicyStateFailed
      );
      console.error("Failed to load model loadbalance current state", error);
    }
  }, [enabled, modelId]);

  const resetCooldown = useCallback(async (connectionId: number) => {
    setResettingConnectionIds((current) => {
      const next = new Set(current);
      next.add(connectionId);
      return next;
    });

    try {
      const response = await api.loadbalance.resetCurrentState(connectionId);
      requestIdRef.current += 1;
      // Narrow reset: a 2xx is a confirmed success (including cleared=false);
      // the full post-reset snapshot calibrates the row. The row is never
      // optimistically removed.
      setCurrentStateByConnectionId((current) => {
        if (!current.has(connectionId)) {
          return current;
        }
        const next = new Map(current);
        if (response.state) {
          next.set(connectionId, {
            ...current.get(connectionId)!,
            cycle_retry_attempts: response.state.cycle_retry_attempts,
            cumulative_retry_attempts: response.state.cumulative_retry_attempts,
            next_retry_at: response.state.next_retry_at,
            last_retry_delay_ms: response.state.last_retry_delay_ms,
            ban_mode: response.state.ban_mode,
            banned_until_at: response.state.banned_until_at,
            last_failure_kind: response.state.last_failure_kind,
            state: response.state.state,
            updated_at: response.state.updated_at,
          });
        }
        return next;
      });
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : getStaticMessages().modelDetailData.resetBanPolicyStateFailed
      );
    } finally {
      setResettingConnectionIds((current) => {
        const next = new Set(current);
        next.delete(connectionId);
        return next;
      });
    }
  }, []);

  // Poll while the page is visible (30s interval), pause while hidden and
  // refresh immediately when the tab regains focus.
  useEffect(() => {
    if (!enabled || !modelId || modelId.trim() === "") {
      return;
    }
    const intervalId = window.setInterval(() => {
      if (document.visibilityState === "visible") {
        void fetchCurrentState();
      }
    }, 30_000);
    const onVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        void fetchCurrentState();
      }
    };
    const onFocus = () => {
      void fetchCurrentState();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    window.addEventListener("focus", onFocus);
    return () => {
      window.clearInterval(intervalId);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      window.removeEventListener("focus", onFocus);
    };
  }, [enabled, fetchCurrentState, modelId]);

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
