import { useCallback, useMemo, useState } from "react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { getStaticMessages } from "@/i18n/staticMessages"
import type { Connection } from "@/lib/types"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/i18n/useLocale"
import { AccessTargetsEditor } from "@/pages/models/AccessTargetsEditor"
import { ModelDialog } from "@/pages/models/ModelDialog"
import { accessTargetToMutation } from "@/pages/models/modelFormState"
import { ConnectionDialog } from "@/pages/model-detail/ConnectionDialog"
import { CopyTerminalTargetDialog } from "@/pages/model-detail/CopyTerminalTargetDialog"
import { ModelDetailHeader } from "@/pages/model-detail/ModelDetailHeader"
import { OpenAICoverageSummary } from "@/pages/model-detail/OpenAICoverageSummary"
import { OverviewCards } from "@/pages/model-detail/OverviewCards"
import { isOwnedConnectionTarget } from "@/pages/model-detail/useModelDetailDataSupport"
import { useModelDetailFeatureData } from "./useModelDetailFeatureData"
import { MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET } from "./modelDetailSchemas"

type URLSearchParamsInit = ConstructorParameters<typeof URLSearchParams>[0]
type SetURLSearchParams = (
  nextInit: URLSearchParamsInit | ((current: URLSearchParams) => URLSearchParamsInit),
  options?: { replace?: boolean },
) => void

interface ModelDetailFeaturePageProps {
  modelId: string | undefined
  searchParams?: URLSearchParams
  onBack?: () => void
  onNavigateTo?: (to: string) => void
  onSearchParamsChange?: (searchParams: URLSearchParams, options?: { replace?: boolean }) => void
}

function resolveSearchParamsInit(
  nextInit: URLSearchParamsInit | ((current: URLSearchParams) => URLSearchParamsInit) | undefined,
  current: URLSearchParams,
): URLSearchParams {
  if (typeof nextInit === "function") {
    return new URLSearchParams(nextInit(current))
  }
  return new URLSearchParams(nextInit)
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
  const [copyTarget, setCopyTarget] = useState<Connection | null>(null)
  const resolvedSearchParams = useMemo(
    () => new URLSearchParams(searchParams ?? new URLSearchParams(window.location.search)),
    [searchParams],
  )
  // One-shot query-driven create action: open the Terminal Target dialog and
  // clear the action parameters (replace) so a refresh never reopens it.
  const oneShotAction = useMemo(() => {
    const action = resolvedSearchParams.get("action")
    if (action !== MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET) return null
    const endpointId = resolvedSearchParams.get("endpoint_id")
    return { endpointId: endpointId && /^\d+$/.test(endpointId) ? endpointId : null }
  }, [resolvedSearchParams])
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
  const consumeOneShotAction = useCallback(() => {
    const next = new URLSearchParams(resolvedSearchParams)
    next.delete("action")
    next.delete("endpoint_id")
    setSearchParams(next, { replace: true })
  }, [resolvedSearchParams, setSearchParams])
  const data = useModelDetailFeatureData({
    modelId,
    searchParams: resolvedSearchParams,
    setSearchParams,
    navigateTo,
    oneShotAction,
    onOneShotActionConsumed: consumeOneShotAction,
  })
  const modelConfigIDByModelID = useMemo(() => {
    const map = new Map<string, number>()
    for (const candidate of data.allModels) {
      map.set(candidate.model_id, candidate.id)
    }
    return map
  }, [data.allModels])
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
  const ownerOpenAIAcceptedFormat = model.openai_accepted_format ?? null

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

      <OpenAICoverageSummary
        diagnostics={data.diagnostics}
        loading={data.diagnosticsLoading}
        error={data.diagnosticsError}
        onRetry={() => void data.refreshDiagnostics()}
      />

      <AccessTargetsEditor
        apiFamilyLabel={model.api_family}
        accessTargets={model.access_targets
          .map(accessTargetToMutation)
          .filter((target): target is NonNullable<typeof target> => target !== null)}
        modelOptions={data.targetModelsForApiFamily}
        connectionOptions={data.targetConnectionsForApiFamily}
        error={data.targetEditorError}
        isConnectionTargetMutable={isConnectionTargetMutable}
        diagnostics={data.diagnostics}
        modelConfigIDByModelID={modelConfigIDByModelID}
        ownerOpenAIAcceptedFormat={ownerOpenAIAcceptedFormat}
        currentStateByConnectionId={data.currentStateByConnectionId}
        resettingConnectionIds={data.resettingConnectionIds}
        onResetCooldown={(connectionId) => void data.handleResetCooldown(connectionId)}
        onRefreshRuntimeState={() => void data.refreshCurrentState()}
        pricingTemplates={data.pricingTemplates}
        onAddTarget={data.handleAddAccessTarget}
        onCreateConnection={() => data.openConnectionDialog()}
        onDeleteTarget={data.handleDeleteAccessTarget}
        onEditConnection={data.openConnectionDialog}
        onCopyConnection={setCopyTarget}
        onQuickCapabilityChange={data.handleQuickCapabilityChange}
        onQuickPricingChange={data.handleQuickPricingChange}
        onMoveTarget={data.handleMoveAccessTarget}
        onToggleTarget={data.handleToggleAccessTarget}
      />

      <CopyTerminalTargetDialog
        isOpen={copyTarget != null}
        onOpenChange={(open) => { if (!open) setCopyTarget(null) }}
        sourceModelConfigId={parsedModelConfigId ?? 0}
        sourceCapability={copyTarget?.openai_text_capability ?? null}
        destinationModels={data.allModels}
        onCopy={async (destinationModelConfigIds, enableCopies) => {
          if (!copyTarget || parsedModelConfigId == null) return
          await api.models.connections.copies(parsedModelConfigId, copyTarget.id, {
            destination_model_config_ids: destinationModelConfigIds,
            enable_copies: enableCopies,
          })
          toast.success(getStaticMessages().modelDetailData.connectionCopied)
          void data.refreshDiagnostics()
          void data.refreshCurrentState()
        }}
      />

      <ConnectionDialog
        isOpen={data.isConnectionDialogOpen}
        onOpenChange={data.setIsConnectionDialogOpen}
        apiFamily={model.api_family}
        ownerOpenAIAcceptedFormat={ownerOpenAIAcceptedFormat}
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
        endpointSourceDefaultName={data.endpointSourceDefaultName}
        pricingTemplates={data.pricingTemplates}
        prefillConnections={data.targetConnectionsForApiFamily}
      />

      <ModelDialog
        editingModel={model}
        formData={data.formData}
        formError={data.targetEditorError}
        isDialogOpen={data.isEditModelDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        dialogTitle={messages.modelDetail.modelSettingsTitle}
        dialogDescription={messages.modelDetail.modelSettingsDescription}
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
