import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus } from "lucide-react";
import { usePublishBreadcrumbEntity } from "@/components/layout/app-layout/breadcrumbEntity";
import { CopyButton } from "@/components/CopyButton";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { buildModelDetailPath } from "@/app/router/rewriteRoutes";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import {
  modelRoutingDiagnostics,
  type RoutingDiagnosticsResponse,
} from "@/lib/api/observability";
import { AccessTargetsEditor } from "@/pages/models/AccessTargetsEditor";
import { ModelDialog } from "@/pages/models/ModelDialog";
import { ConnectionDialog } from "@/pages/model-detail/ConnectionDialog";
import { CopyTerminalTargetDialog } from "@/pages/model-detail/CopyTerminalTargetDialog";
import { CatalogMetadataCard } from "@/pages/model-detail/CatalogMetadataCard";
import { CatalogPricingDialog } from "@/pages/model-detail/CatalogPricingDialog";
import { useModelCatalog } from "@/pages/model-detail/useModelCatalog";
import type { Connection } from "@/lib/types";
import { toast } from "sonner";
import { ModelCostCards } from "@/pages/model-detail/ModelCostCards";
import { RouteReadinessCard } from "@/pages/model-detail/RouteReadinessCard";
import {
  OperatorFreshnessBar,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorStatusBadge,
} from "@/shared/design-system";
import { isOwnedConnectionTarget } from "@/pages/model-detail/modelAccessTargetProjection";
import { useModelDetailFeatureData } from "./useModelDetailFeatureData";

type URLSearchParamsInit = ConstructorParameters<typeof URLSearchParams>[0];
type SetURLSearchParams = (
  nextInit:
    | URLSearchParamsInit
    | ((current: URLSearchParams) => URLSearchParamsInit),
  options?: { replace?: boolean },
) => void;

interface ModelDetailFeaturePageProps {
  modelId: string | undefined;
  searchParams?: URLSearchParams;
  onNavigateTo?: (to: string) => void;
  onSearchParamsChange?: (
    searchParams: URLSearchParams,
    options?: { replace?: boolean },
  ) => void;
}

function resolveSearchParamsInit(
  nextInit:
    | URLSearchParamsInit
    | ((current: URLSearchParams) => URLSearchParamsInit)
    | undefined,
  current: URLSearchParams,
): URLSearchParams {
  if (typeof nextInit === "function") {
    return new URLSearchParams(nextInit(current));
  }
  return new URLSearchParams(nextInit);
}

function updateBrowserSearch(searchParams: URLSearchParams, replace?: boolean) {
  const query = searchParams.toString();
  const nextUrl = `${window.location.pathname}${query ? `?${query}` : ""}${window.location.hash}`;
  if (replace) {
    window.history.replaceState(null, "", nextUrl);
    return;
  }
  window.history.pushState(null, "", nextUrl);
}

export type DiagnosticsView =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "loaded"; value: RoutingDiagnosticsResponse };

/**
 * Reads static routing diagnostics for one model, keeping the four states a
 * consumer needs to tell apart: never read, reading, read failed, and read
 * successfully. Collapsing them into one nullable value is what let a failed
 * fetch render as an absent panel.
 *
 * The fetch lives in a hook rather than inline in the component so the pending
 * transition can be set before the request without tripping the
 * set-state-in-effect rule, matching usePricingListFacts.
 */
function useRoutingDiagnosticsView(modelConfigId: number | undefined) {
  const [settled, setSettled] = useState<{
    token: number;
    modelConfigId: number;
    result: DiagnosticsView;
  } | null>(null);
  const [reloadToken, setReloadToken] = useState(0);
  const refreshDiagnostics = useCallback(() => {
    setReloadToken((token) => token + 1);
  }, []);

  useEffect(() => {
    if (!modelConfigId || Number.isNaN(modelConfigId)) return;
    let cancelled = false;
    void (async () => {
      try {
        const diagnostics = await modelRoutingDiagnostics.get(modelConfigId);
        if (!cancelled)
          setSettled({
            token: reloadToken,
            modelConfigId,
            result: { kind: "loaded", value: diagnostics },
          });
      } catch (error: unknown) {
        if (!cancelled) {
          setSettled({
            token: reloadToken,
            modelConfigId,
            result: {
              kind: "error",
              message: error instanceof Error ? error.message : String(error),
            },
          });
        }
      }
    })();
    return () => {
      cancelled = true;
    };
    // reloadToken is the only retry dimension: without it the retry button
    // would update state that no effect reads and issue no request.
  }, [modelConfigId, reloadToken]);

  // Pending is derived from "has the current request settled" rather than
  // written by the effect. Besides keeping the effect free of a synchronous
  // setState, this makes a stale result impossible to present as the current
  // one: a settled record only counts when both its token and its model match.
  const diagnosticsView: DiagnosticsView =
    !modelConfigId || Number.isNaN(modelConfigId)
      ? { kind: "idle" }
      : settled &&
          settled.token === reloadToken &&
          settled.modelConfigId === modelConfigId
        ? settled.result
        : { kind: "loading" };

  return { diagnosticsView, refreshDiagnostics };
}

export function ModelDetailFeaturePage({
  modelId,
  searchParams,
  onNavigateTo,
  onSearchParamsChange,
}: ModelDetailFeaturePageProps) {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const resolvedSearchParams = useMemo(
    () =>
      new URLSearchParams(
        searchParams ?? new URLSearchParams(window.location.search),
      ),
    [searchParams],
  );
  const setSearchParams = useCallback<SetURLSearchParams>(
    (nextInit, options) => {
      const nextSearchParams = resolveSearchParamsInit(
        nextInit,
        new URLSearchParams(resolvedSearchParams),
      );
      onSearchParamsChange?.(nextSearchParams, options);
      if (!onSearchParamsChange) {
        updateBrowserSearch(nextSearchParams, options?.replace);
      }
    },
    [onSearchParamsChange, resolvedSearchParams],
  );
  const navigateTo = useCallback(
    (to: string) => {
      if (onNavigateTo) {
        onNavigateTo(to);
        return;
      }
      window.location.assign(to);
    },
    [onNavigateTo],
  );
  const parsedModelConfigId = modelId
    ? Number.parseInt(modelId, 10)
    : undefined;
  const { diagnosticsView, refreshDiagnostics } =
    useRoutingDiagnosticsView(parsedModelConfigId);
  const data = useModelDetailFeatureData({
    modelId,
    searchParams: resolvedSearchParams,
    setSearchParams,
    navigateTo,
    refreshDiagnostics,
  });
  const [copyTarget, setCopyTarget] = useState<Connection | null>(null);
  const [pricingTarget, setPricingTarget] = useState<Connection | null>(null);
  // revision=0 matches the page's bootstrap cadence; refresh() drives catalog
  // re-reads after bind/refresh/override mutations.
  const { catalog: modelCatalog, refresh: refreshModelCatalog } =
    useModelCatalog(parsedModelConfigId, 0);
  // The breadcrumb leaf must name the model, not say "配置". Until the model
  // loads this stays null and the shell falls back to the id.
  usePublishBreadcrumbEntity(data.model?.display_name || data.model?.model_id);

  if (data.loading) {
    return (
      <div
        className="flex flex-col gap-[var(--density-page-gap)]"
        data-testid="model-detail-feature-loading"
      >
        <div className="flex items-center gap-3">
          <Skeleton className="size-[var(--density-control-h-sm)] rounded" />
          <Skeleton className="h-7 w-48" />
        </div>
        <Skeleton className="h-[120px] rounded-lg" />
        <Skeleton className="h-[400px] rounded-lg" />
      </div>
    );
  }

  if (!data.model) return null;

  const model = data.model;
  // The runtime snapshot is per-connection; the freshest observation on the
  // page is what the bar reports.
  const runtimeUpdatedAt =
    [...data.currentStateByConnectionId.values()]
      .map((item) => item.updated_at)
      .sort()
      .at(-1) ?? model.updated_at;
  const isConnectionTargetMutable = (connectionId: number) =>
    isOwnedConnectionTarget(model, parsedModelConfigId, connectionId);
  const ownerOpenAIAcceptedFormat = model.openai_accepted_format ?? null;

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
          label={
            model.is_enabled
              ? messages.modelDetail.enabled
              : messages.modelDetail.disabled
          }
        />
        <Button
          type="button"
          variant="outline"
          onClick={() => data.setIsEditModelDialogOpen(true)}
        >
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
            data.refreshCurrentState();
            data.refetchSpending();
          },
        }}
      />

      <RouteReadinessCard
        accessTargetSummary={data.accessTargetSummary}
        diagnosticsView={diagnosticsView}
        onRetryDiagnostics={refreshDiagnostics}
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

      <CatalogMetadataCard
        modelConfigId={parsedModelConfigId ?? 0}
        catalog={modelCatalog}
        onChanged={refreshModelCatalog}
      />

      <AccessTargetsEditor
        onGeneratePricing={setPricingTarget}
        apiFamilyLabel={model.api_family}
        accessTargets={model.access_targets}
        modelOptions={data.targetModelsForApiFamily}
        connectionOptions={data.targetConnectionsForApiFamily}
        error={data.targetEditorError}
        isConnectionTargetMutable={isConnectionTargetMutable}
        strategyType={model.loadbalance_strategy?.legacy_strategy_type}
        currentStateByConnectionId={data.currentStateByConnectionId}
        currentStateGapByConnectionId={data.currentStateGapByConnectionId}
        currentStateFailure={data.currentStateFailure}
        currentStateCompleteness={data.currentStateCompleteness}
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
        onViewModelTargetDetail={(targetModelConfigId) =>
          navigateTo(buildModelDetailPath(targetModelConfigId))
        }
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
      {/* Mounting only while a target is open lets the dialog initialize its
          selection from that target; keeping it mounted would freeze the
          initial empty selection. */}
      {pricingTarget !== null && (
        <CatalogPricingDialog
          isOpen
          modelConfigId={parsedModelConfigId ?? 0}
          connectionId={pricingTarget.id}
          connectionName={
            pricingTarget?.name ?? pricingTarget?.endpoint?.name ?? ""
          }
          connections={data.targetConnectionsForApiFamily}
          onClose={() => setPricingTarget(null)}
          onCommitted={(templateName, assignedCount) => {
            setPricingTarget(null);
            toast.success(
              messages.modelCatalog.pricingSuccessToast(
                templateName,
                assignedCount,
              ),
            );
          }}
        />
      )}
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
        routingScheduleDraft={data.routingScheduleDraft}
        setRoutingScheduleDraft={data.setRoutingScheduleDraft}
        routingScheduleError={data.routingScheduleError}
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
  );
}

export default ModelDetailFeaturePage;
