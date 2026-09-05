import { useCallback, useEffect, useMemo, useState } from "react"
import { api } from "@/lib/api"
import { getSharedPricingTemplates, setSharedModels } from "@/lib/referenceData"
import type {
  Connection,
  Endpoint,
  LoadbalanceStrategy,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
} from "@/lib/types"
import {
  type AccessTargetSummary,
  buildAccessTargetSummary,
  getAccessTargetModelsForApiFamily,
  getOwnedModelConnections,
  getSameFamilyConnections,
} from "@/pages/model-detail/modelAccessTargetProjection"
import { useConnectionFocus } from "@/pages/model-detail/useConnectionFocus"
import { useModelDetailAccessTargetMutations } from "@/pages/model-detail/useModelDetailAccessTargetMutations"
import {
  NO_DEGRADED_PARTS,
  useModelDetailBootstrap,
  type ModelDetailDegradedParts,
} from "@/pages/model-detail/useModelDetailBootstrap"
import { useModelDetailConnectionMutations } from "@/pages/model-detail/useModelDetailConnectionMutations"
import { useModelDetailDialogState } from "@/pages/model-detail/useModelDetailDialogState"
import { useModelDetailModelForm } from "@/pages/model-detail/useModelDetailModelForm"
import { useModelDetailTargetReconciliation } from "@/pages/model-detail/useModelDetailTargetReconciliation"
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
  // The mutations hook has always accepted refreshDiagnostics and calls it in
  // four places, but nothing ever passed one in, so every call was a no-op and
  // the diagnostics panel kept showing pre-mutation results until a reload.
  refreshDiagnostics?: () => void | Promise<void>
}

export function useModelDetailFeatureData({
  modelId,
  searchParams,
  setSearchParams,
  refreshDiagnostics,
}: UseModelDetailFeatureDataInput) {
  const revision = 0
  const modelConfigId = modelId ? Number.parseInt(modelId, 10) : undefined

  const [model, setModel] = useState<ModelConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [allModels, setAllModels] = useState<ModelConfigListItem[]>([])
  const [loadbalanceStrategies, setLoadbalanceStrategies] = useState<LoadbalanceStrategy[]>([])
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>([])
  const [connections, setConnections] = useState<Connection[]>([])
  const [allConnections, setAllConnections] = useState<Connection[]>([])
  const [connectionSearch, setConnectionSearch] = useState("")
  const [focusedConnectionId, setFocusedConnectionId] = useState<number | null>(null)
  // 深链定位的落点是访问目标表里的那一行。
  const [connectionCardRefs] = useState<Map<number, HTMLElement>>(new Map())
  const [globalEndpoints, setGlobalEndpoints] = useState<Endpoint[]>([])

  const refreshModels = useCallback(async () => {
    const authoritativeModels = await api.models.list()
    setAllModels(authoritativeModels)
    setSharedModels(revision, authoritativeModels)
  }, [revision])

  // A catalog pricing commit updates the current model's embedded Terminal
  // Target, the model-scoped connection list, and the pricing collection in one
  // transaction. Re-read those three owners together so the row, future target
  // choices, and the edit dialog cannot disagree after the dialog closes.
  const refreshCatalogPricingReads = useCallback(async () => {
    if (!modelConfigId) return
    const [authoritativeModel, authoritativeConnections, authoritativePricing] =
      await Promise.all([
        api.models.get(modelConfigId),
        api.models.connections.list(modelConfigId),
        getSharedPricingTemplates(revision, true),
      ])
    setModel(authoritativeModel)
    setConnections(getOwnedModelConnections(authoritativeModel, modelConfigId))
    setAllConnections(
      authoritativeConnections.filter(
        (connection) =>
          connection.model_config_id == null ||
          connection.model_config_id === modelConfigId,
      ),
    )
    setPricingTemplates(authoritativePricing)
  }, [modelConfigId, revision])

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
    upstreamModelIdError,
    setUpstreamModelIdError,
    endpointSourceDefaultName,
    openConnectionDialog,
  } = useModelDetailDialogState({
    apiFamily: model?.api_family ?? null,
    openAIMode: model?.openai_accepted_format ?? null,
    globalEndpoints,
    ownerModelID: model?.model_id ?? null,
  })

  // 页面每 30 秒自己重读运行态：这件事必须对操作者可见且可关，
  // 否则新鲜度条上的自动刷新只是装饰。
  const [autoRefreshValue, setAutoRefreshValue] = useState<"off" | "30s" | "60s">(
    "30s",
  )
  const autoRefreshIntervalMs =
    autoRefreshValue === "off"
      ? null
      : autoRefreshValue === "30s"
        ? 30_000
        : 60_000

  const [loadError, setLoadError] = useState<string | null>(null)
  // 「不存在」与「没读到」是两件事，页面要分开渲染，所以分开记。
  const [notFound, setNotFound] = useState(false)
  const [degradedParts, setDegradedParts] =
    useState<ModelDetailDegradedParts>(NO_DEGRADED_PARTS)

  const { fetchModel } = useModelDetailBootstrap({
    id: modelId,
    revision,
    setModel,
    setConnections,
    setAllConnections,
    setGlobalEndpoints,
    setLoadbalanceStrategies,
    setAllModels,
    setPricingTemplates,
    setLoading,
    setLoadError,
    setNotFound,
    setDegradedParts,
  })

  // The global current-state read model filters on the public model id string,
  // not the numeric config id in the route. `model_configs` is unique on
  // (profile_id, model_id), so this is the same cohort the route addresses.
  const {
    currentStateByConnectionId,
    currentStateGapByConnectionId,
    currentStateFailure,
    currentStateCompleteness,
    currentStateGeneratedAt,
    currentStateLoading,
    resettingConnectionIds,
    refreshCurrentState,
    resetCooldown,
  } = useModelLoadbalanceCurrentState({
    modelId: model?.model_id,
    revision,
    enabled: Boolean(model),
    pollIntervalMs: autoRefreshIntervalMs,
  })

  const { applyTargets } = useModelDetailTargetReconciliation({
    modelConfigId: modelConfigId ?? NaN,
    revision,
    refreshCurrentState,
    refreshDiagnostics,
    refreshModels,
    setConnections,
    setModel,
  })

  const {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
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
    setRoutingScheduleError,
    setCustomRequestParametersError,
    setUpstreamModelIdError,
    editingConnection,
    pricingTemplates,
    endpointSourceDefaultName,
    refreshCurrentState,
    refreshDiagnostics,
    refreshModels,
    setIsConnectionDialogOpen,
    setConnections,
    setAllConnections,
    setGlobalEndpoints,
    applyTargets,
  })
  const {
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleDeleteAccessTarget,
  } = useModelDetailAccessTargetMutations({
    modelConfigId: modelConfigId ?? NaN,
    model,
    applyTargets,
  })

  const {
    formData,
    targetEditorError,
    setTargetEditorError,
    modelFormError,
    setModelFormError,
    setFormData,
    setIsEditModelDialogOpen,
    setLoadbalanceStrategyId,
    handleEditModelSubmit,
  } = useModelDetailModelForm({
    model,
    revision,
    setIsEditModelDialogOpenState,
    setModel,
    refreshDiagnostics,
    refreshModels,
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
    openConnectionDialog()
    setSelectedEndpointId(String(parsed))
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
    loadError,
    notFound,
    degradedParts,
    retryLoad: fetchModel,
    autoRefreshValue,
    setAutoRefreshValue,
    loadbalanceStrategies,
    isEditModelDialogOpen,
    setIsEditModelDialogOpen,
    formData,
    setFormData,
    setLoadbalanceStrategyId,
    targetConnectionsForApiFamily,
    allModels,
    targetModelsForApiFamily,
    targetEditorError,
    setTargetEditorError,
    modelFormError,
    setModelFormError,
    connections,
    refreshCurrentState,
    refreshModels,
    refreshCatalogPricingReads,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    connectionSearch,
    setConnectionSearch,
    currentStateByConnectionId,
    currentStateGapByConnectionId,
    currentStateFailure,
    currentStateCompleteness,
    currentStateGeneratedAt,
    currentStateLoading,
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
    upstreamModelIdError,
    setUpstreamModelIdError,
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
