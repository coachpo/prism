import { useCallback, useMemo, useState } from "react"
import { createSearchParams, type SetURLSearchParams, type URLSearchParamsInit } from "react-router-dom"
import { useProfileContext } from "@/context/ProfileContext"
import type {
  Connection,
  Endpoint,
  LoadbalanceStrategy,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
  SpendingSummary,
} from "@/lib/types"
import {
  getAccessTargetModelsForApiFamily,
  getPromotionTargetModelsForApiFamily,
} from "@/pages/models/modelFormState"
import {
  type AccessTargetSummary,
  buildAccessTargetSummary,
  getSameFamilyConnections,
} from "@/pages/model-detail/useModelDetailDataSupport"
import { useConnectionFocus } from "@/pages/model-detail/useConnectionFocus"
import { useModelDetailBootstrap } from "@/pages/model-detail/useModelDetailBootstrap"
import { useModelDetailConnectionFlows } from "@/pages/model-detail/useModelDetailConnectionFlows"
import { useModelDetailConnectionMutations } from "@/pages/model-detail/useModelDetailConnectionMutations"
import { useModelDetailDialogState } from "@/pages/model-detail/useModelDetailDialogState"
import { useModelDetailModelForm } from "@/pages/model-detail/useModelDetailModelForm"
import { useModelLoadbalanceCurrentState } from "@/pages/model-detail/useModelLoadbalanceCurrentState"

function resolveSearchParamsInit(
  nextInit: URLSearchParamsInit | ((current: URLSearchParams) => URLSearchParamsInit) | undefined,
  current: URLSearchParams,
): URLSearchParams {
  if (typeof nextInit === "function") {
    return createSearchParams(nextInit(current))
  }
  return createSearchParams(nextInit)
}

interface UseModelDetailFeatureDataInput {
  modelId: string | undefined
  searchParams: URLSearchParams
  setSearchParams: SetURLSearchParams
  navigateTo: (to: string) => void
}

export function useModelDetailFeatureData({
  modelId,
  searchParams,
  setSearchParams,
  navigateTo,
}: UseModelDetailFeatureDataInput) {
  const { revision } = useProfileContext()
  const modelConfigId = modelId ? Number.parseInt(modelId, 10) : undefined

  const [model, setModel] = useState<ModelConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [allModels, setAllModels] = useState<ModelConfigListItem[]>([])
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<LoadbalanceStrategy[]>([])
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>([])
  const [spending, setSpending] = useState<SpendingSummary | null>(null)
  const [spendingLoading, setSpendingLoading] = useState(false)
  const [spendingCurrencySymbol, setSpendingCurrencySymbol] = useState("$")
  const [spendingCurrencyCode, setSpendingCurrencyCode] = useState("USD")

  const [connections, setConnections] = useState<Connection[]>([])
  const [allConnections, setAllConnections] = useState<Connection[]>([])
  const [connectionSearch, setConnectionSearch] = useState("")
  const [focusedConnectionId, setFocusedConnectionId] = useState<number | null>(null)
  const [connectionCardRefs] = useState<Map<number, HTMLDivElement>>(new Map())
  const [globalEndpoints, setGlobalEndpoints] = useState<Endpoint[]>([])

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
  })

  useModelDetailBootstrap({
    id: modelId,
    revision,
    navigate: navigateTo,
    setModel,
    setConnections,
    setAllConnections,
    setGlobalEndpoints,
    setLoadbalanceStrategies,
    setAllModels,
    setPricingTemplates,
    setLoading,
    setSpending,
    setSpendingLoading,
    setSpendingCurrencySymbol,
    setSpendingCurrencyCode,
  })

  const {
    currentStateByConnectionId,
    resettingConnectionIds,
    refreshCurrentState,
    resetCooldown,
  } = useModelLoadbalanceCurrentState({
    modelConfigId,
    revision,
    enabled: Boolean(model),
  })

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
  })

  const {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleUpdateModelTarget,
    handleDeleteAccessTarget,
  } = useModelDetailConnectionMutations({
    id: modelId,
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
  })

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
  })

  const effectiveTargetApiFamily = isEditModelDialogOpen
    ? formData.api_family
    : model?.api_family ?? formData.api_family
  const targetModelsForApiFamily = useMemo(
    () => getAccessTargetModelsForApiFamily(allModels, effectiveTargetApiFamily, model?.model_id),
    [allModels, effectiveTargetApiFamily, model?.model_id],
  )
  const promotionTargetModelsForApiFamily = useMemo(
    () => getPromotionTargetModelsForApiFamily(allModels, effectiveTargetApiFamily, formData.model_id),
    [allModels, effectiveTargetApiFamily, formData.model_id],
  )
  const targetConnectionsForApiFamily = useMemo(
    () => getSameFamilyConnections(allConnections, effectiveTargetApiFamily, modelConfigId),
    [allConnections, effectiveTargetApiFamily, modelConfigId],
  )
  const accessTargetSummary = useMemo<AccessTargetSummary>(() => buildAccessTargetSummary(model), [model])
  const setFocusSearchParams = useCallback<SetURLSearchParams>(
    (nextInit, options) => {
      setSearchParams(resolveSearchParamsInit(nextInit, new URLSearchParams(searchParams)), options)
    },
    [searchParams, setSearchParams],
  )

  useConnectionFocus({
    model,
    searchParams,
    setSearchParams: setFocusSearchParams,
    connectionCardRefs,
    setFocusedConnectionId,
  })

  return {
    model,
    loading,
    loadbalanceStrategies,
    isEditModelDialogOpen,
    setIsEditModelDialogOpen,
    formData,
    setFormData,
    setLoadbalanceStrategyId,
    promotionTargetModelsForApiFamily,
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
    handleUpdateModelTarget,
    handleDeleteAccessTarget,
    handleEditModelSubmit,
    pricingTemplates,
    reorderInFlight,
    handleReorderConnections,
    handleResetCooldown: resetCooldown,
  }
}
