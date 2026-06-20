import { useCallback, useMemo } from "react"
import { createSearchParams, type SetURLSearchParams, type URLSearchParamsInit } from "react-router-dom"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/i18n/useLocale"
import { AccessTargetsEditor } from "@/pages/models/AccessTargetsEditor"
import { ModelDialog } from "@/pages/models/ModelDialog"
import { accessTargetToMutation } from "@/pages/models/modelFormState"
import { ConnectionDialog } from "@/pages/model-detail/ConnectionDialog"
import { ModelDetailHeader } from "@/pages/model-detail/ModelDetailHeader"
import { OverviewCards } from "@/pages/model-detail/OverviewCards"
import { isOwnedConnectionTarget } from "@/pages/model-detail/useModelDetailDataSupport"
import { useModelDetailFeatureData } from "./useModelDetailFeatureData"
import { type ModelDetailTab } from "./modelDetailSchemas"

interface ModelDetailFeaturePageProps {
  modelId: string | undefined
  tab?: ModelDetailTab
  searchParams?: URLSearchParams
  onBack?: () => void
  onNavigateTo?: (to: string) => void
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
  searchParams,
  onBack,
  onNavigateTo,
  onSearchParamsChange,
}: ModelDetailFeaturePageProps) {
  const { messages } = useLocale()
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
  if (data.loading) {
    return (
      <div className="flex flex-col gap-[var(--density-page-gap)]" data-testid="model-detail-feature-loading">
        <div className="flex items-center gap-3">
          <Skeleton className="size-[var(--density-control-h-sm)] rounded" />
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
        onChange={() => undefined}
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
        pricingTemplates={data.pricingTemplates}
      />

      <ModelDialog
        editingModel={model}
        formData={data.formData}
        formError={data.targetEditorError}
        isDialogOpen={data.isEditModelDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        targetModelsForApiFamily={data.targetModelsForApiFamily}
        dialogTitle={messages.modelDetail.modelSettingsTitle}
        dialogDescription={messages.modelDetail.modelSettingsAccessTargetsDescription}
        includeTerminalTargetConnectionOptions={false}
        showModelIdInEditMode={true}
        submitLabel={messages.modelDetail.saveChanges}
        setFormData={data.setFormData}
        setIsDialogOpen={data.setIsEditModelDialogOpen}
        setLoadbalanceStrategyId={data.setLoadbalanceStrategyId}
        onSubmit={data.handleEditModelSubmit}
      />
    </main>
  )
}

export default ModelDetailFeaturePage
