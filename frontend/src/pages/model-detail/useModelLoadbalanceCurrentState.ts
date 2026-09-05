import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  GlobalCurrentStateCompleteness,
  GlobalCurrentStateItem,
  LoadbalanceCurrentStateItem,
} from "@/lib/types";

interface UseModelLoadbalanceCurrentStateInput {
  /** Public model id; the global current-state read model filters by it. */
  modelId: string | undefined;
  revision: number;
  enabled?: boolean;
  /** 轮询间隔；null 表示只在可见性/焦点变化时重读，不定时轮询。 */
  pollIntervalMs?: number | null;
}

/**
 * Why a row carries no fully observed snapshot. The read model deliberately
 * separates these, so the surface must too: "the process has never seen this
 * target" and "the process has seen it but cannot report every field" are
 * different facts, and neither is a read failure.
 */
export type CurrentStateRowGap = "partial" | "unobserved";

/**
 * A failed read never degrades to an empty cohort. `staleData` is true when a
 * refresh failed on top of a previously successful read — the rows stay on
 * screen and the surface marks them stale instead of blanking them.
 */
export interface CurrentStateFailure {
  message: string;
  staleData: boolean;
}

export interface CurrentStateCompleteness extends GlobalCurrentStateCompleteness {
  /** The cohort was cut off by the page limit; absence proves nothing below it. */
  hasMore: boolean;
}

// The model-detail target summary consumes the per-target projection of the
// shared global current-state read model (SPEC §6): rows for this model are
// bridged to the target-keyed shape without a second fetch owner.
function toCurrentStateProjection(items: GlobalCurrentStateItem[]) {
  const map = new Map<number, LoadbalanceCurrentStateItem>();
  // Rows the cohort DID return but that carry no complete snapshot. Dropping
  // them entirely would make them indistinguishable from rows the cohort never
  // contained at all, which is the difference between "not observed" and
  // "not configured for observation".
  const gaps = new Map<number, CurrentStateRowGap>();
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
    // available state — record the gap and let the surface name it.
    if (item.observation_state !== "observed") {
      gaps.set(targetId, "unobserved");
      continue;
    }
    if (
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
      gaps.set(targetId, "partial");
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
  return { map, gaps };
}

export function useModelLoadbalanceCurrentState({
  modelId,
  revision,
  enabled = true,
  pollIntervalMs = 30_000,
}: UseModelLoadbalanceCurrentStateInput) {
  const [currentStateByConnectionId, setCurrentStateByConnectionId] = useState<
    Map<number, LoadbalanceCurrentStateItem>
  >(new Map());
  const [currentStateGapByConnectionId, setCurrentStateGapByConnectionId] = useState<
    Map<number, CurrentStateRowGap>
  >(new Map());
  const [currentStateFailure, setCurrentStateFailure] = useState<CurrentStateFailure | null>(null);
  const [currentStateCompleteness, setCurrentStateCompleteness] =
    useState<CurrentStateCompleteness | null>(null);
  const [resettingConnectionIds, setResettingConnectionIds] = useState<Set<number>>(
    new Set()
  );
  // A successful read is the only thing that proves the cohort is empty rather
  // than unread. Until one lands, an empty map means "not read yet".
  const loadedOnceRef = useRef(false);
  const requestIdRef = useRef(0);
  // 观测时间取服务端生成快照的时刻，而不是任何配置的修改时间。
  const [generatedAt, setGeneratedAt] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  // 30 秒轮询一直失败时不能每轮弹一次 toast：只在「成功→失败」这条边上报一次。
  const failureReportedRef = useRef(false);
  const resetKey = `${enabled ? modelId ?? "none" : "disabled"}:${revision}`;
  const resetKeyRef = useRef(resetKey);

  const fetchCurrentState = useCallback(async () => {
    if (!enabled || !modelId || modelId.trim() === "") {
      requestIdRef.current += 1;
      loadedOnceRef.current = false;
      setCurrentStateByConnectionId(new Map());
      setCurrentStateGapByConnectionId(new Map());
      setCurrentStateCompleteness(null);
      setCurrentStateFailure(null);
      setGeneratedAt(null);
      return;
    }

    const resolvedModelId = modelId.trim();

    const requestId = ++requestIdRef.current;
    setLoading(true);

    try {
      const data = await api.loadbalance.listCurrentState({
        model_id: resolvedModelId,
      });

      if (requestId !== requestIdRef.current) {
        return;
      }

      const { map, gaps } = toCurrentStateProjection(data.items);
      loadedOnceRef.current = true;
      setCurrentStateByConnectionId(map);
      setCurrentStateGapByConnectionId(gaps);
      setCurrentStateCompleteness({ ...data.completeness, hasMore: data.has_more });
      setCurrentStateFailure(null);
      setGeneratedAt(data.generated_at);
      failureReportedRef.current = false;
    } catch (error) {
      if (requestId !== requestIdRef.current) {
        return;
      }

      const message =
        error instanceof Error ? error.message : getStaticMessages().modelDetailData.loadBanPolicyStateFailed;
      // A failed read must not read as an observed empty cohort. Rows already
      // on screen stay and are marked stale; with nothing on screen yet the
      // surface renders the failure instead of an empty state.
      setCurrentStateFailure({ message, staleData: loadedOnceRef.current });
      if (!failureReportedRef.current) {
        failureReportedRef.current = true;
        toast.error(message, { id: "model-detail-current-state-failure" });
      }
      console.error("Failed to load model loadbalance current state", error);
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false);
      }
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
    const intervalId =
      pollIntervalMs === null
        ? null
        : window.setInterval(() => {
            if (document.visibilityState === "visible") {
              void fetchCurrentState();
            }
          }, pollIntervalMs);
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
      if (intervalId !== null) window.clearInterval(intervalId);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      window.removeEventListener("focus", onFocus);
    };
  }, [enabled, fetchCurrentState, modelId, pollIntervalMs]);

  useEffect(() => {
    if (resetKeyRef.current !== resetKey) {
      resetKeyRef.current = resetKey;
      requestIdRef.current += 1;
      loadedOnceRef.current = false;
      setCurrentStateByConnectionId(new Map());
      setCurrentStateGapByConnectionId(new Map());
      setCurrentStateCompleteness(null);
      setCurrentStateFailure(null);
      setResettingConnectionIds(new Set());
    }

    void fetchCurrentState();
  }, [fetchCurrentState, resetKey]);

  return {
    currentStateByConnectionId,
    currentStateGapByConnectionId,
    currentStateFailure,
    currentStateCompleteness,
    currentStateGeneratedAt: generatedAt,
    currentStateLoading: loading,
    resettingConnectionIds,
    refreshCurrentState: fetchCurrentState,
    resetCooldown,
  };
}
