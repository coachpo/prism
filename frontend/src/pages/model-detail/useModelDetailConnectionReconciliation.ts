import { useCallback } from "react";
import { toast } from "sonner";

import { connectionBelongsToModel } from "./modelAccessTargetProjection";
import {
  hydrateConnectionPricingTemplate,
  upsertConnectionInList,
  upsertEndpointInList,
} from "./connectionCollectionState";
import type {
  Connection,
  Endpoint,
  PricingTemplate,
} from "@/lib/types";

const TERMINAL_TARGET_OWNER_MISMATCH =
  "Terminal Target owner does not match the current model";

export type CommitModelDetailConnection = (
  connection: Connection,
) => Connection | null;

interface UseModelDetailConnectionReconciliationInput {
  modelConfigId: number;
  pricingTemplates: PricingTemplate[];
  setConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setAllConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setGlobalEndpoints: React.Dispatch<React.SetStateAction<Endpoint[]>>;
  refreshModels?: () => Promise<void>;
}

/**
 * Owns the server-DTO-to-local-collections boundary for Terminal Target
 * mutations. No submit parsing or lifecycle toast belongs here.
 */
export function useModelDetailConnectionReconciliation({
  modelConfigId,
  pricingTemplates,
  setConnections,
  setAllConnections,
  setGlobalEndpoints,
  refreshModels,
}: UseModelDetailConnectionReconciliationInput) {
  const commitConnection = useCallback<CommitModelDetailConnection>(
    (connection) => {
      if (!connectionBelongsToModel(connection, modelConfigId)) {
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
        return null;
      }

      const committedConnection = hydrateConnectionPricingTemplate(
        connection,
        pricingTemplates,
      );
      setAllConnections((current) =>
        upsertConnectionInList(current, committedConnection),
      );
      setConnections((current) =>
        upsertConnectionInList(current, committedConnection),
      );
      setGlobalEndpoints((current) =>
        upsertEndpointInList(current, committedConnection.endpoint),
      );
      void refreshModels?.().catch((error) => {
        console.error("Failed to refresh authoritative model list", error);
      });
      return committedConnection;
    },
    [
      modelConfigId,
      pricingTemplates,
      refreshModels,
      setAllConnections,
      setConnections,
      setGlobalEndpoints,
    ],
  );

  return { commitConnection };
}
