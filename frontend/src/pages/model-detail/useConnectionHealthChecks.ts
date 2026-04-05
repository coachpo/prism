import { useCallback, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { api } from "@/lib/api";
import type { Connection, HealthCheckResponse } from "@/lib/types";
import { applyConnectionHealthChecks } from "./useModelDetailDataSupport";

interface UseConnectionHealthChecksInput {
  setConnections: Dispatch<SetStateAction<Connection[]>>;
  onSuccessfulChecks?: (connectionIds: number[]) => void | Promise<void>;
}

export function useConnectionHealthChecks({
  setConnections,
  onSuccessfulChecks,
}: UseConnectionHealthChecksInput) {
  const [healthCheckingIds, setHealthCheckingIds] = useState<Set<number>>(new Set());

  const runHealthChecks = useCallback(
    async (connectionIds: number[]) => {
      if (connectionIds.length === 0) {
        return {
          successfulChecks: new Map<number, HealthCheckResponse>(),
          failedCount: 0,
        };
      }

      setHealthCheckingIds((prev) => {
        const next = new Set(prev);
        connectionIds.forEach((connectionId) => next.add(connectionId));
        return next;
      });

      const results = await Promise.allSettled(
        connectionIds.map(async (connectionId) => ({
          connectionId,
          response: await api.connections.healthCheck(connectionId),
        }))
      );

      const successfulChecks = new Map<number, HealthCheckResponse>();
      let failedCount = 0;

      for (const result of results) {
        if (result.status === "fulfilled") {
          successfulChecks.set(result.value.connectionId, result.value.response);
        } else {
          failedCount += 1;
        }
      }

      setConnections((prevConnections) =>
        applyConnectionHealthChecks(prevConnections, successfulChecks)
      );

      if (successfulChecks.size > 0) {
        void onSuccessfulChecks?.(Array.from(successfulChecks.keys()));
      }

      setHealthCheckingIds((prev) => {
        const next = new Set(prev);
        connectionIds.forEach((connectionId) => next.delete(connectionId));
        return next;
      });

      return { successfulChecks, failedCount };
    },
    [onSuccessfulChecks, setConnections]
  );

  return {
    healthCheckingIds,
    runHealthChecks,
  };
}
