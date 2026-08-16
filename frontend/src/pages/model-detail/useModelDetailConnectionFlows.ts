import { useCallback, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { Connection, ModelConfig } from "@/lib/types";
import { getOwnedConnectionTarget, moveConnectionInList } from "./useModelDetailDataSupport";

interface UseModelDetailConnectionFlowsInput {
  model: ModelConfig | null;
  modelConfigId?: number;
  connections: Connection[];
  setConnections: Dispatch<SetStateAction<Connection[]>>;
}
export function useModelDetailConnectionFlows({
  model,
  modelConfigId,
  connections,
  setConnections,
}: UseModelDetailConnectionFlowsInput) {
  const [reorderInFlight, setReorderInFlight] = useState(false);

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

  return {
    reorderInFlight,
    handleReorderConnections,
  };
}
