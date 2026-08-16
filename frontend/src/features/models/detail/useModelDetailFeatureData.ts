import { useCallback, useEffect, useMemo, useState } from "react"
import type {
  Connection,
  Endpoint,
  LoadbalanceStrategy,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
  SpendingSummary,
} from "@/lib/types"
import { getAccessTargetModelsForApiFamily } from "@/pages/models/modelFormState"
import {
  type AccessTargetSummary,
  buildAccessTargetSummary,
  getSameFamilyConnections,
} from "@/pages/model-detail/useModelDetailDataSupport"
import { useConnectionFocus } from "@/pages/model-detail/useConnectionFocus"
import { useModelDetailBootstrap } from "@/pages/model-detail/useModelDetailBootstrap"
import { useModelDetailConnectionMutations } from "@/pages/model-detail/useModelDetailConnectionMutations"
import { useModelDetailDialogState } from "@/pages/model-detail/useModelDetailDialogState"
import { useModelDetailModelForm } from "@/pages/model-detail/useModelDetailModelForm"
import { useModelLoadbalanceCurrentState } from "@/pages/model-detail/useModelLoadbalanceCurrentState"

type URLSearchParamsInit = ConstructorParameters<typeof URLSearchParams>[0]
type SetURLSearchParams = (
  nextInit: URLSearchParamsInit | ((current: URLSearchParams) => URLSearchParamsInit),
  options?: { replace?: boolean },
) => void

function resolveSearchParamsInit(
  nextInit: URLSearchParamsInit | ((current: URLSearchParams) => URLSearchParamsInit) | undefined,
  current: URLSearchParams,
): URLSearchParams {
  if (typeof nextInit === "function") {
    return new URLSearchParams(nextInit(current))
  }
  return new URLSearchParams(nextInit)
}

interface UseModelDetailFeatureDataInput {
  modelId: string | undefined
  searchParams: URLSearchParams
  setSearchParams: SetURLSearchParams
  navigateTo: (to: string) => void
  // The mutations hook has always accepted refreshDiagnostics and calls it in
  // four places, but nothing ever passed one in, so every call was a no-op and
  // the diagnostics panel kept showing pre-mutation results until a reload.
  refreshDiagnostics?: () => void | Promise<void>
}

export function useModelDetailFeatureData({
  modelId,
  searchParams,
  setSearchParams,
  navigateTo,
  refreshDiagnostics,
}: UseModelDetailFeatureDataInput) {
  const revision = 0
  const modelConfigId = modelId ? Number.parseInt(modelId, 10) : undefined

  const [model, setModel] = useState<ModelConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [allModels, setAllModels] = useState<ModelConfigListItem[]>([])
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<LoadbalanceStrategy[]>([])
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>([])
  const [spending, setSpending] = useState<SpendingSummary | null>(null)
  const [spendingLoading, setSpendingLoading] = useState(false)
  const [spendingFailed, setSpendingFailed] = useState(false)
  const [spendingWindow, setSpendingWindow] = useState<"today" | "last_7_days" | "all">("all")
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
    customRequestParametersDraft,
    routingScheduleDraft,
    setRoutingScheduleDraft,
    routingScheduleError,
    setRoutingScheduleError,
    setCustomRequestParametersDraft,
    customRequestParametersError,
    setCustomRequestParametersError,
    endpointSourceDefaultName,
    openConnectionDialog,
  } = useModelDetailDialogState({
    apiFamily: model?.api_family ?? null,
    openAIMode: model?.openai_accepted_format ?? null,
    globalEndpoints,
  })

  const { refetchSpending } = useModelDetailBootstrap({
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
    setSpendingFailed,
    setSpendingCurrencySymbol,
    setSpendingCurrencyCode,
    spendingPreset: spendingWindow,
  })

  // The global current-state read model filters on the public model id string,
  // not the numeric config id in the route. `model_configs` is unique on
  // (profile_id, model_id), so this is the same cohort the route addresses.
  const {
    currentStateByConnectionId,
    currentStateGapByConnectionId,
    currentStateFailure,
    currentStateCompleteness,
    resettingConnectionIds,
    refreshCurrentState,
    resetCooldown,
  } = useModelLoadbalanceCurrentState({
    modelId: model?.model_id,
    revision,
    enabled: Boolean(model),
  })

  const {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
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
    customRequestParametersDraft,
    routingScheduleDraft,
    setRoutingScheduleDraft,
    routingScheduleError,
    setRoutingScheduleError,
    setCustomRequestParametersError,
    editingConnection,
    pricingTemplates,
    endpointSourceDefaultName,
    refreshCurrentState,
    refreshDiagnostics,
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
    model,
    revision,
    setIsEditModelDialogOpenState,
    setAllModels,
    setModel,
  })

  const effectiveTargetApiFamily = model?.api_family ?? formData.api_family
  const targetModelsForApiFamily = useMemo(
    () => getAccessTargetModelsForApiFamily(allModels, effectiveTargetApiFamily, model?.model_id),
    [allModels, effectiveTargetApiFamily, model?.model_id],
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

  // MC-A6 Endpoint handoff: action=create-terminal-target&endpoint_id preselects and
  // locks the endpoint for a new Terminal Target, then consumes the action once.
  const handoffAction = searchParams.get("action")
  const handoffEndpointId = searchParams.get("endpoint_id")
  useEffect(() => {
    if (handoffAction !== "create-terminal-target" || handoffEndpointId === null) {
      return
    }
    const parsed = Number.parseInt(handoffEndpointId, 10)
    if (!Number.isFinite(parsed) || parsed <= 0) {
      return
    }
    if (createMode !== "select") {
      setCreateMode("select")
    }
    setSelectedEndpointId(String(parsed))
    setIsConnectionDialogOpen(true)
    // Consume the one-shot action so refresh never reopens the dialog.
    const next = new URLSearchParams(searchParams)
    next.delete("action")
    next.delete("endpoint_id")
    setFocusSearchParams(next, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [handoffAction, handoffEndpointId])

  return {
    model,
    loading,
    loadbalanceStrategies,
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
    spendingFailed,
    spendingWindow,
    setSpendingWindow,
    refetchSpending,
    spendingCurrencySymbol,
    spendingCurrencyCode,
    connections,
    refreshCurrentState,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    connectionSearch,
    setConnectionSearch,
    currentStateByConnectionId,
    currentStateGapByConnectionId,
    currentStateFailure,
    currentStateCompleteness,
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
    customRequestParametersDraft,
    routingScheduleDraft,
    setRoutingScheduleDraft,
    routingScheduleError,
    setRoutingScheduleError,
    setCustomRequestParametersDraft,
    customRequestParametersError,
    setCustomRequestParametersError,
    accessTargetSummary,
    endpointSourceDefaultName,
    openConnectionDialog,
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleDeleteAccessTarget,
    handleEditModelSubmit,
    pricingTemplates,
    handleResetCooldown: resetCooldown,
  }
}
