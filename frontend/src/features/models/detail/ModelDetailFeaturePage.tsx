import { useCallback, useEffect, useMemo, useState } from "react"
import { Plus } from "lucide-react"
import { usePublishBreadcrumbEntity } from "@/components/layout/app-layout/breadcrumbEntity"
import { CopyButton } from "@/components/CopyButton"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/i18n/useLocale"
import { useTimezone } from "@/hooks/useTimezone"
import { modelRoutingDiagnostics, type RoutingDiagnosticsResponse } from "@/lib/api/observability";
import { AccessTargetsEditor } from "@/pages/models/AccessTargetsEditor"
import { ModelDialog } from "@/pages/models/ModelDialog"
import { ConnectionDialog } from "@/pages/model-detail/ConnectionDialog"
import { CopyTerminalTargetDialog } from "@/pages/model-detail/CopyTerminalTargetDialog"
import type { Connection } from "@/lib/types"
import { ModelCostCards } from "@/pages/model-detail/ModelCostCards"
import { RouteReadinessCard } from "@/pages/model-detail/RouteReadinessCard"
import {
  OperatorFreshnessBar,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorStatusBadge,
} from "@/shared/design-system"
import { isOwnedConnectionTarget } from "@/pages/model-detail/useModelDetailDataSupport"
import { useModelDetailFeatureData } from "./useModelDetailFeatureData"

type URLSearchParamsInit = ConstructorParameters<typeof URLSearchParams>[0]
type SetURLSearchParams = (
  nextInit: URLSearchParamsInit | ((current: URLSearchParams) => URLSearchParamsInit),
  options?: { replace?: boolean },
) => void

interface ModelDetailFeaturePageProps {
  modelId: string | undefined
  searchParams?: URLSearchParams
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
  onNavigateTo,
  onSearchParamsChange,
}: ModelDetailFeaturePageProps) {
  const { messages } = useLocale()
  const { format: formatTime } = useTimezone()
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
  const [routingDiagnostics, setRoutingDiagnostics] = useState<RoutingDiagnosticsResponse | null>(null);
  const [copyTarget, setCopyTarget] = useState<Connection | null>(null);
  // The breadcrumb leaf must name the model, not say "配置". Until the model
  // loads this stays null and the shell falls back to the id.
  usePublishBreadcrumbEntity(data.model?.display_name || data.model?.model_id);
  const parsedModelConfigIdForDiag = modelId ? Number.parseInt(modelId, 10) : undefined;
  useEffect(() => {
    if (!parsedModelConfigIdForDiag || Number.isNaN(parsedModelConfigIdForDiag)) return;
    let cancelled = false;
    void modelRoutingDiagnostics
      .get(parsedModelConfigIdForDiag)
      .then((diagnostics) => {
        if (!cancelled) setRoutingDiagnostics(diagnostics);
      })
      .catch(() => {
        if (!cancelled) setRoutingDiagnostics(null);
      });
    return () => {
      cancelled = true;
    };
  }, [parsedModelConfigIdForDiag, data.loading]);

  if (data.loading) {
    return (
      <div className="flex flex-col gap-[var(--density-page-gap)]" data-testid="model-detail-feature-loading">
        <div className="flex items-center gap-3">
          <Skeleton className="size-[var(--density-control-h-sm)] rounded" />
          <Skeleton className="h-7 w-48" />
        </div>
        <Skeleton className="h-[120px] rounded-lg" />
        <Skeleton className="h-[400px] rounded-lg" />
      </div>
    )
  }

  if (!data.model) return null

  const model = data.model
  // The runtime snapshot is per-connection; the freshest observation on the
  // page is what the bar reports.
  const runtimeUpdatedAt = [...data.currentStateByConnectionId.values()]
    .map((item) => item.updated_at)
    .sort()
    .at(-1) ?? model.updated_at
  const parsedModelConfigId = modelId ? Number.parseInt(modelId, 10) : undefined
  const isConnectionTargetMutable = (connectionId: number) =>
    isOwnedConnectionTarget(model, parsedModelConfigId, connectionId)
  const ownerOpenAIAcceptedFormat = model.openai_accepted_format ?? null

  return (
    <OperatorPageShell className="pb-2" data-testid="model-detail-feature-page">
      {/* The breadcrumb already carries the way back, so the page header owns
          the title and the primary action instead of an unlabelled arrow. */}
      <OperatorPageHeader
        title={model.display_name || model.model_id}
        description={
          <span className="flex min-w-0 items-center gap-1">
            <span className="truncate font-mono">{model.model_id}</span>
            <CopyButton
              value={model.model_id}
              label=""
              targetLabel={messages.modelDetail.modelIdLabel}
              aria-label={messages.modelDetail.copyModelIdAria(model.model_id)}
              variant="ghost"
              size="icon-xs"
              className="size-6 shrink-0 rounded-md text-muted-foreground hover:text-foreground"
            />
          </span>
        }
      >
        <OperatorStatusBadge
          intent={model.is_enabled ? "healthy" : "idle"}
          preserveLabel
          label={model.is_enabled ? messages.modelDetail.enabled : messages.modelDetail.disabled}
        />
        <Button type="button" variant="outline" onClick={() => data.setIsEditModelDialogOpen(true)}>
          {messages.modelDetail.editModel}
        </Button>
        <Button type="button" onClick={() => data.openConnectionDialog()}>
          <Plus data-icon="inline-start" />
          {messages.modelDetail.addConnection}
        </Button>
      </OperatorPageHeader>

      <OperatorFreshnessBar
        updatedAt={messages.freshness.updatedAt(formatTime(runtimeUpdatedAt))}
        basis={messages.modelDetail.runtimeBasis}
        refresh={{
          label: messages.freshness.refresh,
          onRefresh: () => {
            data.refreshCurrentState()
            data.refetchSpending()
          },
        }}
      />

      <RouteReadinessCard
        accessTargetSummary={data.accessTargetSummary}
        diagnostics={routingDiagnostics}
        model={model}
      />

      <ModelCostCards
        currencyCode={data.spendingCurrencyCode}
        currencySymbol={data.spendingCurrencySymbol}
        failed={data.spendingFailed}
        loading={data.spendingLoading}
        onRetry={data.refetchSpending}
        onWindowChange={data.setSpendingWindow}
        spending={data.spending}
        window={data.spendingWindow}
      />

      <AccessTargetsEditor
        apiFamilyLabel={model.api_family}
        accessTargets={model.access_targets}
        modelOptions={data.targetModelsForApiFamily}
        connectionOptions={data.targetConnectionsForApiFamily}
        error={data.targetEditorError}
        isConnectionTargetMutable={isConnectionTargetMutable}
		strategyType={model.loadbalance_strategy?.legacy_strategy_type}
		currentStateByConnectionId={data.currentStateByConnectionId}
		resettingConnectionIds={data.resettingConnectionIds}
		onResetCooldown={data.handleResetCooldown}
		onRefreshRuntimeState={data.refreshCurrentState}
        onAddTarget={data.handleAddAccessTarget}
        onCreateConnection={() => data.openConnectionDialog()}
        onDeleteTarget={data.handleDeleteAccessTarget}
        onEditConnection={data.openConnectionDialog}
        onMoveTarget={data.handleMoveAccessTarget}
        onToggleTarget={data.handleToggleAccessTarget}
        onCopyTarget={setCopyTarget}
      />

      <CopyTerminalTargetDialog
        isOpen={copyTarget !== null}
        modelConfigId={parsedModelConfigId ?? 0}
        connectionId={copyTarget?.id ?? 0}
        connectionName={copyTarget?.name ?? copyTarget?.endpoint?.name ?? ""}
        ownerMode={model.openai_accepted_format ?? null}
        models={data.targetModelsForApiFamily}
        onClose={() => setCopyTarget(null)}
        onCopied={() => {
          setCopyTarget(null);
          navigateTo(`/route/models/${model.id}`);
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
        customRequestParametersDraft={data.customRequestParametersDraft}
        setCustomRequestParametersDraft={data.setCustomRequestParametersDraft}
        customRequestParametersError={data.customRequestParametersError}
        setCustomRequestParametersError={data.setCustomRequestParametersError}
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
    </OperatorPageShell>
  )
}

export default ModelDetailFeaturePage
