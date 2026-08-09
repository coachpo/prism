import { useCallback, useEffect, useMemo, useRef, useState } from "react"
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
import { parseAttachTerminalTargetSearch } from "@/features/models/detail/modelDetailSchemas"
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
  oneShotAction?: { endpointId: string | null } | null
  onOneShotActionConsumed?: () => void
}

export function useModelDetailFeatureData({
  modelId,
  searchParams,
  setSearchParams,
  navigateTo,
  oneShotAction = null,
  onOneShotActionConsumed,
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
  const [spendingCurrencySymbol, setSpendingCurrencySymbol] = useState("$")
  const [spendingCurrencyCode, setSpendingCurrencyCode] = useState("USD")

  const [connections, setConnections] = useState<Connection[]>([])
  const [allConnections, setAllConnections] = useState<Connection[]>([])
  const [connectionSearch, setConnectionSearch] = useState("")
  const [focusedConnectionId, setFocusedConnectionId] = useState<number | null>(null)
  const [connectionCardRefs] = useState<Map<number, HTMLDivElement>>(new Map())
  const [globalEndpoints, setGlobalEndpoints] = useState<Endpoint[]>([])

  const attachTarget = useMemo(() => parseAttachTerminalTargetSearch({
    action: searchParams.get("action") ?? undefined,
    endpoint_id: searchParams.get("endpoint_id") ?? undefined,
  }), [searchParams])

  const {
    isEditModelDialogOpen,
    setIsEditModelDialogOpen: setIsEditModelDialogOpenState,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    lockedEndpointId,
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
    setCustomRequestParametersDraft,
    customRequestParametersError,
    setCustomRequestParametersError,
    endpointSourceDefaultName,
    openConnectionDialog,
  } = useModelDetailDialogState({
    apiFamily: model?.api_family ?? null,
    openAIMode: model?.openai_accepted_format ?? null,
    globalEndpoints,
    initialLockedEndpointId: attachTarget?.endpoint_id ?? null,
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
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleDeleteAccessTarget,
    handleQuickCapabilityChange,
    handleQuickPricingChange,
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
    setCustomRequestParametersError,
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
    model,
    revision,
    setIsEditModelDialogOpenState,
    setAllModels,
    setModel,
  })

  // One-shot query-driven create: open the Terminal Target dialog with an
  // optional preselected endpoint. The action parameters are cleared after the
  // dialog has opened (next tick) so a refresh never reopens it and the URL
  // replace does not race the dialog mount. The ref guard makes the whole
  // consumption fire exactly once per navigation.
  const oneShotConsumedRef = useRef(false)
  useEffect(() => {
    if (!oneShotAction || !model || oneShotConsumedRef.current) return
    oneShotConsumedRef.current = true
    openConnectionDialog()
    if (oneShotAction.endpointId) {
      setSelectedEndpointId(oneShotAction.endpointId)
    }
    onOneShotActionConsumed?.()
  }, [model, oneShotAction, onOneShotActionConsumed, openConnectionDialog, setSelectedEndpointId])

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

  // One-shot attach-to-model: after the model and global Endpoints are
  // available, open the Terminal Target create dialog with the Endpoint
  // preselected/locked, then clear the URL so refresh does not reopen it.
  useEffect(() => {
    if (!attachTarget || loading || !model) return
    const lockedEndpoint = globalEndpoints.find((endpoint) => endpoint.id === attachTarget.endpoint_id)
    if (!lockedEndpoint) return
    setIsConnectionDialogOpen(true)
    setCreateMode("select")
    setSelectedEndpointId(String(lockedEndpoint.id))
    setSearchParams(new URLSearchParams(), { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [attachTarget, loading, model, globalEndpoints])

  return {
    model,
    loading,
    allModels,
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
    spendingCurrencySymbol,
    spendingCurrencyCode,
    connections,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    lockedEndpointId,
    connectionSearch,
    setConnectionSearch,
    currentStateByConnectionId,
    resettingConnectionIds,
    refreshCurrentState,
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
    handleQuickCapabilityChange,
    handleQuickPricingChange,
    handleEditModelSubmit,
    pricingTemplates,
    handleResetCooldown: resetCooldown,
  }
}
