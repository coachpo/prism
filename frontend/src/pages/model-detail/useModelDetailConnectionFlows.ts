import { useCallback, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { Connection, ModelConfig } from "@/lib/types";
import { getOwnedConnectionTarget, moveConnectionInList } from "./useModelDetailDataSupport";
import { useConnectionHealthChecks } from "./useConnectionHealthChecks";

const TERMINAL_TARGET_OWNER_MISMATCH = "Terminal Target owner does not match the current model";
const SAVE_TERMINAL_TARGET_BEFORE_HEALTH_CHECK = "Save the terminal target before running a health check.";

interface UseModelDetailConnectionFlowsInput {
  model: ModelConfig | null;
  modelConfigId?: number;
  connections: Connection[];
  setConnections: Dispatch<SetStateAction<Connection[]>>;
  editingConnection: Connection | null;
  refreshCurrentState: () => void | Promise<void>;
  setDialogTestingConnection: (testing: boolean) => void;
  setDialogTestResult: (result: { status: string; detail: string } | null) => void;
}

export function useModelDetailConnectionFlows({
  model,
  modelConfigId,
  connections,
  setConnections,
  editingConnection,
  refreshCurrentState,
  setDialogTestingConnection,
  setDialogTestResult,
}: UseModelDetailConnectionFlowsInput) {
  const [reorderInFlight, setReorderInFlight] = useState(false);
  const { healthCheckingIds, runHealthChecks } = useConnectionHealthChecks({
    modelConfigId,
    setConnections,
    onSuccessfulChecks: refreshCurrentState,
  });

  const handleReorderConnections = useCallback(
    async (connectionId: number, toIndex: number) => {
      if (reorderInFlight || !Number.isFinite(modelConfigId)) return;
      const target = getOwnedConnectionTarget(model, modelConfigId, connectionId);
      const fromIndex = connections.findIndex((connection) => connection.id === connectionId);
      if (!target || fromIndex === -1 || toIndex < 0 || toIndex >= connections.length || fromIndex === toIndex) return;

      const previousConnections = connections;
      setReorderInFlight(true);
      setConnections(moveConnectionInList(connections, fromIndex, toIndex));
      try {
        await api.models.targets.movePosition(modelConfigId as number, target.id, toIndex);
      } catch (error) {
        setConnections(previousConnections);
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.reorderPriorityReverted);
      } finally {
        setReorderInFlight(false);
      }
    },
    [connections, model, modelConfigId, reorderInFlight, setConnections],
  );

  const handleHealthCheck = useCallback(
    async (connectionId: number) => {
      if (!getOwnedConnectionTarget(model, modelConfigId, connectionId)) {
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
        return;
      }
      const { successfulChecks, failedCount } = await runHealthChecks([connectionId]);
      const result = successfulChecks.get(connectionId);
      if (result) {
        toast.success(getStaticMessages().modelDetailData.healthCheckResult(result.health_status, String(result.response_time_ms)));
      }
      if (failedCount > 0) {
        toast.error(getStaticMessages().modelDetailData.healthCheckFailed);
      }
    },
    [model, modelConfigId, runHealthChecks],
  );

  const handleDialogTestConnection = useCallback(async () => {
    if (!editingConnection) {
      setDialogTestResult({ status: "error", detail: SAVE_TERMINAL_TARGET_BEFORE_HEALTH_CHECK });
      return;
    }
    setDialogTestingConnection(true);
    setDialogTestResult(null);
    try {
      if (!getOwnedConnectionTarget(model, modelConfigId, editingConnection.id)) {
        setDialogTestResult({ status: "error", detail: `${TERMINAL_TARGET_OWNER_MISMATCH}.` });
        return;
      }
      const result = await api.models.connections.healthCheck(modelConfigId as number, editingConnection.id);
      setDialogTestResult({ status: result.health_status, detail: result.detail });
      void refreshCurrentState();
    } catch {
      setDialogTestResult({ status: "error", detail: getStaticMessages().modelDetailData.connectionTestFailed });
    } finally {
      setDialogTestingConnection(false);
    }
  }, [editingConnection, model, modelConfigId, refreshCurrentState, setDialogTestResult, setDialogTestingConnection]);

  return {
    healthCheckingIds,
    reorderInFlight,
    handleReorderConnections,
    handleHealthCheck,
    handleDialogTestConnection,
  };
}
