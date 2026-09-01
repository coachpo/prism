import type {
  ApiFamily,
  Connection,
  Endpoint,
  ModelConfig,
  PricingTemplate,
} from "@/lib/types";
import {
  useModelDetailConnectionLifecycle,
} from "./useModelDetailConnectionLifecycle";
import { useModelDetailConnectionReconciliation } from "./useModelDetailConnectionReconciliation";
import { useModelDetailConnectionSubmit } from "./useModelDetailConnectionSubmit";
import type { ConnectionSubmitPreparationInput } from "./connectionSubmitPreparation";

export interface UseModelDetailConnectionMutationsInput
  extends Omit<
    ConnectionSubmitPreparationInput,
    "apiFamily" | "editingConnection"
  > {
  id: string | undefined;
  revision: number;
  model: ModelConfig | null;
  modelApiFamily: ApiFamily | null;
  editingConnection: Connection | null;
  pricingTemplates: PricingTemplate[];
  refreshCurrentState: () => void | Promise<void>;
  refreshDiagnostics?: () => void | Promise<void>;
  refreshModels?: () => Promise<void>;
  setRoutingScheduleError: (
    error: import("./routingScheduleDraft").RoutingScheduleDraftError | null,
  ) => void;
  setCustomRequestParametersError: (
    error:
      | import("./customRequestParameters").CustomRequestParametersParseError
      | null,
  ) => void;
  /** In-place 422 error for the upstream model id field. */
  setUpstreamModelIdError: (error: string | null) => void;
  setIsConnectionDialogOpen: (open: boolean) => void;
  setConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setAllConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setGlobalEndpoints: React.Dispatch<React.SetStateAction<Endpoint[]>>;
  applyTargets: (targets: import("@/lib/types").ModelAccessTarget[]) => void;
}

/**
 * Page-level connection mutation composition. Submit preparation, server DTO
 * reconciliation, and connection lifecycle actions remain separate owners.
 */
export function useModelDetailConnectionMutations({
  id,
  revision,
  model,
  modelApiFamily,
  createMode,
  selectedEndpointId,
  newEndpointForm,
  connectionForm,
  headerRows,
  customRequestParametersDraft,
  setCustomRequestParametersError,
  setUpstreamModelIdError,
  editingConnection,
  pricingTemplates,
  endpointSourceDefaultName,
  refreshCurrentState,
  refreshDiagnostics,
  refreshModels,
  routingScheduleDraft,
  setRoutingScheduleError,
  setIsConnectionDialogOpen,
  setConnections,
  setAllConnections,
  setGlobalEndpoints,
  applyTargets,
}: UseModelDetailConnectionMutationsInput) {
  const modelConfigId = id ? Number.parseInt(id, 10) : NaN;
  const { commitConnection } = useModelDetailConnectionReconciliation({
    modelConfigId,
    pricingTemplates,
    setConnections,
    setAllConnections,
    setGlobalEndpoints,
    refreshModels,
  });
  const submit = useModelDetailConnectionSubmit({
    id,
    revision,
    model,
    modelApiFamily,
    createMode,
    selectedEndpointId,
    newEndpointForm,
    connectionForm,
    headerRows,
    customRequestParametersDraft,
    setCustomRequestParametersError,
    setUpstreamModelIdError,
    editingConnection,
    endpointSourceDefaultName,
    routingScheduleDraft,
    setRoutingScheduleError,
    setIsConnectionDialogOpen,
    applyTargets,
    commitConnection,
  });
  const lifecycle = useModelDetailConnectionLifecycle({
    modelConfigId,
    model,
    revision,
    refreshCurrentState,
    refreshDiagnostics,
    setConnections,
    setAllConnections,
    applyTargets,
    commitConnection,
  });

  return { ...lifecycle, ...submit };
}
