import { useCallback, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { Connection } from "@/lib/types";
import { moveConnectionInList } from "./useModelDetailDataSupport";
import { useConnectionHealthChecks } from "./useConnectionHealthChecks";

interface UseModelDetailConnectionFlowsInput {
  connections: Connection[];
  setConnections: Dispatch<SetStateAction<Connection[]>>;
  editingConnection: Connection | null;
  refreshCurrentState: () => void | Promise<void>;
  setDialogTestingConnection: (testing: boolean) => void;
  setDialogTestResult: (result: { status: string; detail: string } | null) => void;
}

export function useModelDetailConnectionFlows({
  connections,
  setConnections,
  editingConnection,
  refreshCurrentState,
  setDialogTestingConnection,
  setDialogTestResult,
}: UseModelDetailConnectionFlowsInput) {
  const [reorderInFlight, setReorderInFlight] = useState(false);
  const { healthCheckingIds, runHealthChecks } = useConnectionHealthChecks({
    setConnections,
    onSuccessfulChecks: refreshCurrentState,
  });

  const handleReorderConnections = useCallback(
    async (connectionId: number, toIndex: number) => {
      if (reorderInFlight) return;
      const fromIndex = connections.findIndex((connection) => connection.id === connectionId);
      if (fromIndex === -1 || toIndex < 0 || toIndex >= connections.length || fromIndex === toIndex) return;
      setReorderInFlight(true);
      setConnections(moveConnectionInList(connections, fromIndex, toIndex));
      setReorderInFlight(false);
    },
    [connections, reorderInFlight, setConnections],
  );

  const handleHealthCheck = useCallback(
    async (connectionId: number) => {
      const { successfulChecks, failedCount } = await runHealthChecks([connectionId]);
      const result = successfulChecks.get(connectionId);
      if (result) {
        toast.success(getStaticMessages().modelDetailData.healthCheckResult(result.health_status, String(result.response_time_ms)));
      }
      if (failedCount > 0) {
        toast.error(getStaticMessages().modelDetailData.healthCheckFailed);
      }
    },
    [runHealthChecks],
  );

  const handleDialogTestConnection = useCallback(async () => {
    if (!editingConnection) {
      setDialogTestResult({ status: "error", detail: "Save the connection before running a health check." });
      return;
    }
    setDialogTestingConnection(true);
    setDialogTestResult(null);
    try {
      const result = await api.connections.healthCheck(editingConnection.id);
      setDialogTestResult({ status: result.health_status, detail: result.detail });
      void refreshCurrentState();
    } catch {
      setDialogTestResult({ status: "error", detail: getStaticMessages().modelDetailData.connectionTestFailed });
    } finally {
      setDialogTestingConnection(false);
    }
  }, [editingConnection, refreshCurrentState, setDialogTestResult, setDialogTestingConnection]);

  return {
    healthCheckingIds,
    reorderInFlight,
    handleReorderConnections,
    handleHealthCheck,
    handleDialogTestConnection,
  };
}
