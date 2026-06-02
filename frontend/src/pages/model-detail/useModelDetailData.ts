import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useProfileContext } from "@/context/ProfileContext";
import type {
  ModelConfig,
  ModelConfigListItem,
  Connection,
  Endpoint,
  LoadbalanceStrategy,
  Vendor,
  SpendingSummary,
  PricingTemplate,
} from "@/lib/types";
import { getAccessTargetModelsForApiFamily } from "../models/modelFormState";
import { buildAccessTargetSummary, getSameFamilyConnections } from "./useModelDetailDataSupport";
import { useConnectionFocus } from "./useConnectionFocus";
import { useModelDetailBootstrap } from "./useModelDetailBootstrap";
import { useModelDetailConnectionFlows } from "./useModelDetailConnectionFlows";
import { useModelDetailConnectionMutations } from "./useModelDetailConnectionMutations";
import { useModelDetailDialogState } from "./useModelDetailDialogState";
import { useModelDetailModelForm } from "./useModelDetailModelForm";
import { useModelLoadbalanceCurrentState } from "./useModelLoadbalanceCurrentState";

export function useModelDetailData(id: string | undefined) {
  const navigate = useNavigate();
  const { revision } = useProfileContext();
  const [searchParams, setSearchParams] = useSearchParams();
  const modelConfigId = id ? Number.parseInt(id, 10) : undefined;

  const [model, setModel] = useState<ModelConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [allModels, setAllModels] = useState<ModelConfigListItem[]>([]);
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<LoadbalanceStrategy[]>([]);
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>([]);
  const [vendors, setVendors] = useState<Vendor[]>([]);
  const [spending, setSpending] = useState<SpendingSummary | null>(null);
  const [spendingLoading, setSpendingLoading] = useState(false);
  const [spendingCurrencySymbol, setSpendingCurrencySymbol] = useState("$");
  const [spendingCurrencyCode, setSpendingCurrencyCode] = useState("USD");

  const [connections, setConnections] = useState<Connection[]>([]);
  const [allConnections, setAllConnections] = useState<Connection[]>([]);
  const [connectionSearch, setConnectionSearch] = useState("");
  const [focusedConnectionId, setFocusedConnectionId] = useState<number | null>(null);
  const [connectionCardRefs] = useState<Map<number, HTMLDivElement>>(new Map());

  const [globalEndpoints, setGlobalEndpoints] = useState<Endpoint[]>([]);

  const {
    isEditModelDialogOpen,
    setIsEditModelDialogOpen: setIsEditModelDialogOpenState,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    dialogTestingConnection,
    setDialogTestingConnection,
    dialogTestResult,
    setDialogTestResult,
    createMode,
    setCreateMode,
    selectedEndpointId,
    setSelectedEndpointId,
    newEndpointForm,
    setNewEndpointForm,
    connectionForm,
    setConnectionForm,
    headerRows,
    setHeaderRows,
    endpointSourceDefaultName,
    openConnectionDialog,
  } = useModelDetailDialogState({
    apiFamily: model?.api_family ?? null,
    globalEndpoints,
    ownerCapabilityDefaults: model
      ? {
          context_window_tokens: model.context_window_tokens,
          default_output_token_reserve: model.default_output_token_reserve,
          max_context_utilization: model.max_context_utilization,
          preferred_context_utilization_threshold: model.preferred_context_utilization_threshold,
        }
      : undefined,
  });

  useModelDetailBootstrap({
    id,
    revision,
    navigate,
    setModel,
    setConnections,
    setAllConnections,
    setGlobalEndpoints,
    setLoadbalanceStrategies,
    setAllModels,
    setPricingTemplates,
    setVendors,
    setLoading,
    setSpending,
    setSpendingLoading,
    setSpendingCurrencySymbol,
    setSpendingCurrencyCode,
  });

  const {
    currentStateByConnectionId,
    resettingConnectionIds,
    refreshCurrentState,
    resetCooldown,
  } = useModelLoadbalanceCurrentState({
    modelConfigId,
    revision,
    enabled: Boolean(model),
  });

  const {
    healthCheckingIds,
    reorderInFlight,
    handleReorderConnections,
    handleHealthCheck,
    handleDialogTestConnection,
  } = useModelDetailConnectionFlows({
    model,
    modelConfigId,
    connections,
    setConnections,
    editingConnection,
    refreshCurrentState,
    setDialogTestingConnection,
    setDialogTestResult,
  });

  const {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleDeleteAccessTarget,
  } = useModelDetailConnectionMutations({
    id,
    revision,
    model,
    modelApiFamily: model?.api_family ?? null,
    createMode,
    selectedEndpointId,
    newEndpointForm,
    connectionForm,
    headerRows,
    editingConnection,
    pricingTemplates,
    endpointSourceDefaultName,
    refreshCurrentState,
    setIsConnectionDialogOpen,
    setAllModels,
    setConnections,
    setAllConnections,
    setModel,
    setGlobalEndpoints,
  });

  const {
    formData,
    targetEditorError,
    setTargetEditorError,
    setFormData,
    setIsEditModelDialogOpen,
    setLoadbalanceStrategyId,
    handleEditModelSubmit,
  } = useModelDetailModelForm({
    allModels,
    model,
    revision,
    setIsEditModelDialogOpenState,
    setAllModels,
    setModel,
  });

  const targetModelsForApiFamily = useMemo(
    () => getAccessTargetModelsForApiFamily(allModels, formData.api_family, model?.model_id),
    [allModels, formData.api_family, model?.model_id],
  );
  const targetConnectionsForApiFamily = useMemo(
    () => getSameFamilyConnections(allConnections, formData.api_family, modelConfigId),
    [allConnections, formData.api_family, modelConfigId],
  );
  const accessTargetSummary = useMemo(() => buildAccessTargetSummary(model), [model]);

  useConnectionFocus({
    model,
    searchParams,
    setSearchParams,
    connectionCardRefs,
    setFocusedConnectionId,
  });

  return {
    model,
    loading,
    loadbalanceStrategies,
    vendors,
    isEditModelDialogOpen,
    setIsEditModelDialogOpen,
    formData,
    setFormData,
    setLoadbalanceStrategyId,
    targetConnectionsForApiFamily,
    targetModelsForApiFamily,
    targetEditorError,
    setTargetEditorError,
    spending,
    spendingLoading,
    spendingCurrencySymbol,
    spendingCurrencyCode,
    connections,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    connectionSearch,
    setConnectionSearch,
    healthCheckingIds,
    dialogTestingConnection,
    dialogTestResult,
    clearDialogTestResult: () => setDialogTestResult(null),
    currentStateByConnectionId,
    resettingConnectionIds,
    focusedConnectionId,
    connectionCardRefs,
    globalEndpoints,
    createMode,
    setCreateMode,
    selectedEndpointId,
    setSelectedEndpointId,
    newEndpointForm,
    setNewEndpointForm,
    connectionForm,
    setConnectionForm,
    headerRows,
    setHeaderRows,
    accessTargetSummary,
    endpointSourceDefaultName,
    openConnectionDialog,
    handleConnectionSubmit,
    handleDeleteConnection,
    handleHealthCheck,
    handleDialogTestConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleDeleteAccessTarget,
    handleEditModelSubmit,
    pricingTemplates,
    reorderInFlight,
    handleReorderConnections,
    handleResetCooldown: resetCooldown,
  };
}
