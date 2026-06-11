import { useCallback, useMemo } from "react"
import { createSearchParams, type SetURLSearchParams, type URLSearchParamsInit } from "react-router-dom"
import { Skeleton } from "@/components/ui/skeleton"
import { AccessTargetsEditor } from "@/pages/models/AccessTargetsEditor"
import { accessTargetToMutation } from "@/pages/models/modelFormState"
import { ConnectionDialog } from "@/pages/model-detail/ConnectionDialog"
import { ModelDetailHeader } from "@/pages/model-detail/ModelDetailHeader"
import { ModelDetailTabs } from "@/pages/model-detail/ModelDetailTabs"
import { ModelSettingsDialog } from "@/pages/model-detail/ModelSettingsDialog"
import { OverviewCards } from "@/pages/model-detail/OverviewCards"
import { isOwnedConnectionTarget } from "@/pages/model-detail/useModelDetailDataSupport"
import { useModelDetailFeatureData } from "./useModelDetailFeatureData"
import { type ModelDetailTab, normalizeModelDetailTab } from "./modelDetailSchemas"

interface ModelDetailFeaturePageProps {
  modelId: string | undefined
  tab?: ModelDetailTab
  searchParams?: URLSearchParams
  onBack?: () => void
  onNavigateTo?: (to: string) => void
  onNavigateToRequestLogs?: (modelId: string) => void
  onSearchParamsChange?: (searchParams: URLSearchParams, options?: { replace?: boolean }) => void
  onTabChange?: (tab: ModelDetailTab) => void
}

function resolveSearchParamsInit(
  nextInit: URLSearchParamsInit | ((current: URLSearchParams) => URLSearchParamsInit) | undefined,
  current: URLSearchParams,
): URLSearchParams {
  if (typeof nextInit === "function") {
    return createSearchParams(nextInit(current))
  }
  return createSearchParams(nextInit)
}

function updateBrowserSearch(searchParams: URLSearchParams, replace?: boolean) {
  const query = searchParams.toString()
  const nextUrl = `${window.location.pathname}${query ? `?${query}` : ""}${window.location.hash}`
  if (replace) {
    window.history.replaceState(null, "", nextUrl)
    return
  }
  window.history.pushState(null, "", nextUrl)
}

export function ModelDetailFeaturePage({
  modelId,
  tab = "connections",
  searchParams,
  onBack,
  onNavigateTo,
  onNavigateToRequestLogs,
  onSearchParamsChange,
  onTabChange,
}: ModelDetailFeaturePageProps) {
  const activeTab = normalizeModelDetailTab(tab)
  const resolvedSearchParams = useMemo(
    () => new URLSearchParams(searchParams ?? new URLSearchParams(window.location.search)),
    [searchParams],
  )
  const setSearchParams = useCallback<SetURLSearchParams>(
    (nextInit, options) => {
      const nextSearchParams = resolveSearchParamsInit(nextInit, new URLSearchParams(resolvedSearchParams))
      onSearchParamsChange?.(nextSearchParams, options)
      if (!onSearchParamsChange) {
        updateBrowserSearch(nextSearchParams, options?.replace)
      }
    },
    [onSearchParamsChange, resolvedSearchParams],
  )
  const navigateTo = useCallback((to: string) => {
    if (onNavigateTo) {
      onNavigateTo(to)
      return
    }
    window.location.assign(to)
  }, [onNavigateTo])
  const data = useModelDetailFeatureData({
    modelId,
    searchParams: resolvedSearchParams,
    setSearchParams,
    navigateTo,
  })
  const handleTabChange = useCallback((nextTab: ModelDetailTab) => {
    onTabChange?.(nextTab)
    if (!onTabChange) {
      const nextSearchParams = new URLSearchParams(resolvedSearchParams)
      if (nextTab === "connections") {
        nextSearchParams.delete("tab")
      } else {
        nextSearchParams.set("tab", nextTab)
      }
      setSearchParams(nextSearchParams, { replace: true })
    }
  }, [onTabChange, resolvedSearchParams, setSearchParams])

  if (data.loading) {
    return (
      <div className="flex flex-col gap-[var(--density-page-gap)]" data-testid="model-detail-feature-loading">
        <div className="flex items-center gap-3">
          <Skeleton className="h-8 w-8 rounded" />
          <Skeleton className="h-7 w-48" />
        </div>
        <Skeleton className="h-[120px] rounded-xl" />
        <Skeleton className="h-[400px] rounded-xl" />
      </div>
    )
  }

  if (!data.model) return null

  const model = data.model
  const parsedModelConfigId = modelId ? Number.parseInt(modelId, 10) : undefined
  const isConnectionTargetMutable = (connectionId: number) =>
    isOwnedConnectionTarget(model, parsedModelConfigId, connectionId)

  return (
    <main
      className="operator-page-transition flex flex-col gap-[var(--density-page-gap)] pb-2"
      data-testid="model-detail-feature-page"
    >
      <ModelDetailHeader
        model={model}
        onBack={onBack ?? (() => navigateTo("/models"))}
        onEditModel={() => data.setIsEditModelDialogOpen(true)}
      />

      <OverviewCards
        model={model}
        spending={data.spending}
        spendingLoading={data.spendingLoading}
        spendingCurrencySymbol={data.spendingCurrencySymbol}
        spendingCurrencyCode={data.spendingCurrencyCode}
        accessTargetSummary={data.accessTargetSummary}
        onViewRequestLogs={() => {
          if (onNavigateToRequestLogs) {
            onNavigateToRequestLogs(model.model_id)
            return
          }
          navigateTo(`/observe/requests?model=${encodeURIComponent(model.model_id)}`)
        }}
      />

      <AccessTargetsEditor
        apiFamilyLabel={model.api_family}
        accessTargets={model.access_targets
          .map(accessTargetToMutation)
          .filter((target): target is NonNullable<typeof target> => target !== null)}
        modelOptions={data.targetModelsForApiFamily}
        connectionOptions={data.targetConnectionsForApiFamily}
        error={data.targetEditorError}
        healthCheckingIds={data.healthCheckingIds}
        isConnectionTargetMutable={isConnectionTargetMutable}
        onAddTarget={data.handleAddAccessTarget}
        onCreateConnection={() => data.openConnectionDialog()}
        onDeleteTarget={data.handleDeleteAccessTarget}
        onEditConnection={data.openConnectionDialog}
        onHealthCheck={data.handleHealthCheck}
        onMoveTarget={data.handleMoveAccessTarget}
        onToggleTarget={data.handleToggleAccessTarget}
        onUpdateModelTarget={data.handleUpdateModelTarget}
        onChange={() => undefined}
      />

      <ModelDetailTabs
        activeTab={activeTab}
        setActiveTab={handleTabChange}
        model={model}
        connections={data.connections}
        connectionSearch={data.connectionSearch}
        setConnectionSearch={data.setConnectionSearch}
        openConnectionDialog={data.openConnectionDialog}
        handleDeleteConnection={data.handleDeleteConnection}
        handleHealthCheck={data.handleHealthCheck}
        handleToggleActive={data.handleToggleActive}
        handleReorderConnections={data.handleReorderConnections}
        currentStateByConnectionId={data.currentStateByConnectionId}
        resettingConnectionIds={data.resettingConnectionIds}
        healthCheckingIds={data.healthCheckingIds}
        focusedConnectionId={data.focusedConnectionId}
        connectionCardRefs={data.connectionCardRefs}
        reorderInFlight={data.reorderInFlight}
        handleResetCooldown={data.handleResetCooldown}
      />

      <ConnectionDialog
        isOpen={data.isConnectionDialogOpen}
        onOpenChange={data.setIsConnectionDialogOpen}
        apiFamily={model.api_family}
        editingConnection={data.editingConnection}
        connectionForm={data.connectionForm}
        setConnectionForm={data.setConnectionForm}
        newEndpointForm={data.newEndpointForm}
        setNewEndpointForm={data.setNewEndpointForm}
        createMode={data.createMode}
        setCreateMode={data.setCreateMode}
        selectedEndpointId={data.selectedEndpointId}
        setSelectedEndpointId={data.setSelectedEndpointId}
        globalEndpoints={data.globalEndpoints}
        headerRows={data.headerRows}
        setHeaderRows={data.setHeaderRows}
        handleConnectionSubmit={data.handleConnectionSubmit}
        dialogTestingConnection={data.dialogTestingConnection}
        dialogTestResult={data.dialogTestResult}
        clearDialogTestResult={data.clearDialogTestResult}
        handleDialogTestConnection={data.handleDialogTestConnection}
        endpointSourceDefaultName={data.endpointSourceDefaultName}
        ownerCapabilityDefaults={{
          context_window_tokens: model.context_window_tokens,
          default_output_token_reserve: model.default_output_token_reserve,
          max_context_utilization: model.max_context_utilization,
          preferred_context_utilization_threshold: model.preferred_context_utilization_threshold,
        }}
        pricingTemplates={data.pricingTemplates}
      />

      <ModelSettingsDialog
        formData={data.formData}
        handleEditModelSubmit={data.handleEditModelSubmit}
        isOpen={data.isEditModelDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        model={model}
        targetEditorError={data.targetEditorError}
        targetModelsForApiFamily={data.targetModelsForApiFamily}
        onOpenChange={data.setIsEditModelDialogOpen}
        setFormData={data.setFormData}
        setLoadbalanceStrategyId={data.setLoadbalanceStrategyId}
        vendors={data.vendors}
      />
    </main>
  )
}

export default ModelDetailFeaturePage
