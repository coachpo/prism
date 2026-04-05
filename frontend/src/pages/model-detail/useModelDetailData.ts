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
import { buildProxyTargetSummary } from "./useModelDetailDataSupport";
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
  const [spending, setSpending] = useState<SpendingSummary | null>(null);
  const [spendingLoading, setSpendingLoading] = useState(false);
  const [spendingCurrencySymbol, setSpendingCurrencySymbol] = useState("$");
  const [spendingCurrencyCode, setSpendingCurrencyCode] = useState("USD");
  const [editLoadbalanceStrategyId, setEditLoadbalanceStrategyId] = useState("");

  const [connections, setConnections] = useState<Connection[]>([]);
  const [connectionSearch, setConnectionSearch] = useState("");
  const [focusedConnectionId, setFocusedConnectionId] = useState<number | null>(null);
  const [connectionCardRefs] = useState<Map<number, HTMLDivElement>>(new Map());

  const [globalEndpoints, setGlobalEndpoints] = useState<Endpoint[]>([]);

  const {
    isEditModelDialogOpen,
    setIsEditModelDialogOpen,
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
    globalEndpoints,
  });

  useModelDetailBootstrap({
    id,
    revision,
    navigate,
    setModel,
    setConnections,
    setGlobalEndpoints,
    setLoadbalanceStrategies,
    setAllModels,
    setPricingTemplates,
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
    enabled: model?.model_type === "native",
  });

  const {
    healthCheckingIds,
    reorderInFlight,
    handleReorderConnections,
    handleHealthCheck,
    handleDialogTestConnection,
  } = useModelDetailConnectionFlows({
    connections,
    setConnections,
    model,
    modelConfigId,
    setModel,
    createMode,
    selectedEndpointId,
    newEndpointForm,
    connectionForm,
    headerRows,
    editingConnection,
    endpointSourceDefaultName,
    refreshCurrentState,
    setDialogTestingConnection,
    setDialogTestResult,
  });

  const {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
  } = useModelDetailConnectionMutations({
    id,
    revision,
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
    setGlobalEndpoints,
    setModel,
  });

  const { proxyTargetOptions, handleEditModelSubmit, handleSaveProxyTargets } = useModelDetailModelForm({
    editLoadbalanceStrategyId,
    model,
    allModels,
    isEditModelDialogOpen,
    revision,
    setEditLoadbalanceStrategyId,
    setIsEditModelDialogOpen,
    setAllModels,
    setModel,
  });

  const proxyTargetSummary = useMemo(
    () => buildProxyTargetSummary(model, allModels),
    [allModels, model],
  );

  useConnectionFocus({
    model,
    searchParams,
    setSearchParams,
    connectionCardRefs,
    setFocusedConnectionId,
  });

  const vendors = useMemo(() => {
    const seenVendorIds = new Set<number>();
    const nextVendors: Vendor[] = [];

    const pushVendor = (vendor: Vendor | undefined) => {
      if (!vendor || seenVendorIds.has(vendor.id)) {
        return;
      }

      seenVendorIds.add(vendor.id);
      nextVendors.push(vendor);
    };

    pushVendor(model?.vendor);
    allModels.forEach((item) => {
      pushVendor(item.vendor);
    });

    return nextVendors;
  }, [allModels, model?.vendor]);

  return {
    model,
    loading,
    loadbalanceStrategies,
    vendors,
    isEditModelDialogOpen,
    setIsEditModelDialogOpen,
    editLoadbalanceStrategyId,
    setEditLoadbalanceStrategyId,
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
    proxyTargetOptions,
    proxyTargetSummary,
    endpointSourceDefaultName,
    openConnectionDialog,
    handleConnectionSubmit,
    handleDeleteConnection,
    handleHealthCheck,
    handleDialogTestConnection,
    handleToggleActive,
    handleEditModelSubmit,
    handleSaveProxyTargets,
    pricingTemplates,
    reorderInFlight,
    handleReorderConnections,
    handleResetCooldown: resetCooldown,
  };
}
