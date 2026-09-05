import { useCallback, useEffect, useMemo, useState } from "react";
import { Plus } from "lucide-react";
import { usePublishBreadcrumbEntity } from "@/components/layout/app-layout/breadcrumbEntity";
import { CopyButton } from "@/components/CopyButton";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { buildModelDetailPath } from "@/app/router/rewriteRoutes";
import { useLocale } from "@/i18n/useLocale";
import { MetricsScopeSwitch } from "@/features/models/MetricsScopeSwitch";
import { useTimezone } from "@/hooks/useTimezone";
import {
  modelRoutingDiagnostics,
  type RoutingDiagnosticsResponse,
} from "@/lib/api/observability";
import { MoreHorizontal, Trash2 } from "lucide-react";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { AccessTargetsEditor } from "@/pages/models/AccessTargetsEditor";
import { DeleteModelDialog } from "@/pages/models/DeleteModelDialog";
import { useModelDeletion } from "@/pages/models/useModelDeletion";
import { ModelDialog } from "@/pages/models/ModelDialog";
import { ConnectionDialog } from "@/pages/model-detail/ConnectionDialog";
import { CopyTerminalTargetDialog } from "@/pages/model-detail/CopyTerminalTargetDialog";
import { ExternalCatalogSourcesSection } from "./ExternalCatalogSourcesSection";
import { useModelDetailPiSource } from "./useModelDetailPiSource";
import { CatalogPricingDialog } from "@/features/pricing/catalog";
import { clearSharedReferenceData } from "@/lib/referenceData";
import { useModelCatalog } from "@/pages/model-detail/useModelCatalog";
import { usePiBindingController } from "@/features/models/catalog/pi/usePiBindingController";
import type { Connection } from "@/lib/types";
import type { ModelConfigListItem } from "@/lib/types";
import { toast } from "sonner";
import { RouteReadinessCard } from "@/pages/model-detail/RouteReadinessCard";
import {
  OperatorClippedBadge,
  OperatorCallout,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorFreshnessBar,
  OperatorKpiCard,
  OperatorMissingValue,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorRetryButton,
  OperatorSectionCard,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
} from "@/shared/design-system";
import { summarizeRoutingDisposition } from "./routingDisposition";
import { formatLatencyForDisplay } from "@/pages/model-detail/modelDetailMetricsAndPaths";
import { isOwnedConnectionTarget } from "@/pages/model-detail/modelAccessTargetProjection";
import { useModelDetailFeatureData } from "./useModelDetailFeatureData";
import { readSpendingRetentionClip } from "@/features/models/metricsCoverage";
import { useModelMetrics24h } from "@/pages/models/useModelMetrics24h";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { formatMoneyMicros } from "@/lib/costing";
import type { ModelDerivedMetric } from "@/pages/models/modelTableContracts";

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

export type DetailMetricsScope = "ingress" | "final_execution";

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
    const controller = new AbortController();
    void (async () => {
      try {
        const diagnostics = await modelRoutingDiagnostics.get(
          modelConfigId,
          controller.signal,
        );
        if (!controller.signal.aborted)
          setSettled({
            token: reloadToken,
            modelConfigId,
            result: { kind: "loaded", value: diagnostics },
          });
      } catch (error: unknown) {
        if (!controller.signal.aborted) {
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
      controller.abort();
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
  // "abc" 解析成 NaN 而不是 undefined，会被下游的 `?? null` 当成一个真实
  // 标识符带进 /api/models/NaN/… 。一处收口，三个读取都不会再发出去。
  const parsedModelConfigIdCandidate = Number.parseInt(modelId ?? "", 10);
  const parsedModelConfigId = Number.isFinite(parsedModelConfigIdCandidate)
    ? parsedModelConfigIdCandidate
    : undefined;
  // 路由带了参数却解析不出标识符，是「链接抄错了」，不是读取失败或不存在。
  const invalidModelRoute =
    Boolean(modelId?.trim()) && parsedModelConfigId === undefined;
  const { diagnosticsView, refreshDiagnostics } =
    useRoutingDiagnosticsView(parsedModelConfigId);
  const data = useModelDetailFeatureData({
    modelId,
    searchParams: resolvedSearchParams,
    setSearchParams,
    refreshDiagnostics,
  });
  const [copyTarget, setCopyTarget] = useState<Connection | null>(null);
  const [pricingTarget, setPricingTarget] = useState<Connection | null>(null);
  // revision=0 matches the page's bootstrap cadence; refresh() drives catalog
  // re-reads after bind/refresh/override mutations. The full view (loading,
  // failed, stale, last-good) flows into the models.dev panel so a failed
  // read never masquerades as "unbound".
  const modelCatalogView = useModelCatalog(parsedModelConfigId, 0);
  const piSource = useModelDetailPiSource(parsedModelConfigId ?? null);
  const piController = usePiBindingController({
    reconcile: piSource.reconcile,
    actionsBlocked: piSource.actionsBlocked,
  });
  // The breadcrumb leaf must name the model, not say "配置". Until the model
  // loads this stays null and the shell falls back to the id.
  usePublishBreadcrumbEntity(data.model?.display_name || data.model?.model_id);
  const metricsScope: DetailMetricsScope =
    resolvedSearchParams.get("metrics_scope") === "final_execution"
      ? "final_execution"
      : "ingress";
  const modelMetricRows = useMemo(
    () =>
      // SAFETY: the detail response is a superset of the list-item shape the
      // 24h metrics hook reads (id/model_id/display_name and the metric
      // counters); no list-only fields are accessed below.
      data.model ? ([data.model] as unknown as ModelConfigListItem[]) : [],
    [data.model],
  );
  const roleMetrics = useModelMetrics24h(modelMetricRows);
  // 后端说清了成本只覆盖到哪一天；成本卡与列表页必须说同一件事。
  const spendRetentionClip = readSpendingRetentionClip(roleMetrics.coverage);
  const spendRetentionFrom = spendRetentionClip?.retentionFrom
    ? formatTime(spendRetentionClip.retentionFrom, {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      })
    : null;
  const {
    deleteError,
    deleteTarget,
    handleDelete,
    setDeleteTarget,
  } = useModelDeletion({
    commitModels: () => {},
    // 在详情页删掉的就是当前这一页，删完必须离开。
    onDeleted: () => navigateTo("/route/models"),
  });

  // 页头的标题：读不到实体时退化成路由指向的那个 id，页面绝不失去身份。
  const pageTitle =
    data.model?.display_name ||
    data.model?.model_id ||
    messages.modelDetail.modelFallbackTitle(modelId ?? "");
  const backToListAction = (
    <Button
      type="button"
      variant="outline"
      onClick={() => navigateTo("/route/models")}
    >
      {messages.modelDetail.backToModels}
    </Button>
  );

  // 链接抄错了和网关读取失败是两件事，都不能靠一次静默跳转把人送走。
  if (invalidModelRoute) {
    return (
      <OperatorPageShell className="pb-2">
        <OperatorPageHeader title={pageTitle} />
        <OperatorEmptyState
          title={messages.modelDetailData.invalidModelRouteTitle}
          description={messages.modelDetailData.invalidModelRouteDescription(
            modelId ?? "",
          )}
          action={backToListAction}
          testId="model-detail-invalid-route"
        />
      </OperatorPageShell>
    );
  }

  // 「不存在」不是「读取失败」：用空态而不是 destructive 错误卡，
  // 并保留 URL，操作者才分得清是这条配置被删了还是网关抖了一下。
  if (data.notFound) {
    return (
      <OperatorPageShell className="pb-2">
        <OperatorPageHeader title={pageTitle} />
        <OperatorEmptyState
          title={messages.modelDetailData.modelConfigNotFoundTitle}
          description={messages.modelDetailData.modelConfigNotFoundDescription(
            modelId ?? "",
          )}
          action={backToListAction}
          testId="model-detail-not-found"
        />
      </OperatorPageShell>
    );
  }

  if (data.loading) {
    // 骨架按真实区块分块，形状要预示后面会出现什么，否则页面落定时会整体跳动。
    return (
      <OperatorPageShell
        role="status"
        aria-busy
        aria-label={messages.common.loadingApplication}
        data-testid="model-detail-feature-loading"
      >
        <div className="flex items-center gap-3">
          <Skeleton className="size-[var(--density-control-h-sm)] rounded" />
          <Skeleton className="h-7 w-48" />
        </div>
        <Skeleton className="h-8 w-full rounded-md" />
        <OperatorSectionCard title={messages.modelDetail.routeReadinessTitle}>
          <div className="grid gap-3 grid-cols-2 xl:grid-cols-4">
            {[0, 1, 2, 3].map((tile) => (
              <Skeleton key={tile} className="h-16 rounded-md" />
            ))}
          </div>
        </OperatorSectionCard>
        <OperatorSectionCard title={messages.modelsUi.accessTargets}>
          <Skeleton className="h-48 rounded-md" />
        </OperatorSectionCard>
      </OperatorPageShell>
    );
  }

  // 读取失败不是「没有这个模型」：保留 URL 与页面上下文，就地重试。
  if (data.loadError) {
    return (
      <OperatorPageShell className="pb-2">
        {/* 页头留在原处：读取失败不该连「你还在哪个模型配置上」一起抹掉。 */}
        <OperatorPageHeader title={pageTitle} />
        <OperatorErrorState
          title={messages.modelDetailData.fetchModelDetailsFailed}
          description={messages.modelDetailData.modelDetailLoadFailedDescription}
          details={data.loadError}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <OperatorRetryButton onClick={() => void data.retryLoad()}>
              {messages.common.retry}
            </OperatorRetryButton>
          }
          testId="model-detail-load-error"
        />
      </OperatorPageShell>
    );
  }

  if (!data.model) return null;

  const model = data.model;
  // 次要数据源读失败会让某些操作静默失效（端点选不出来、模板退化成未定价），
  // 必须在页面上点名说出来，而不是让下拉看起来「本来就是空的」。
  const degradedNotices = [
    data.degradedParts.endpoints
      ? messages.modelDetailData.degradedEndpoints
      : null,
    data.degradedParts.pricingTemplates
      ? messages.modelDetailData.degradedPricingTemplates
      : null,
    data.degradedParts.loadbalanceStrategies
      ? messages.modelDetailData.degradedLoadbalanceStrategies
      : null,
    data.degradedParts.models ? messages.modelDetailData.degradedModels : null,
  ].filter((notice): notice is string => notice !== null);
  const routingConclusion =
    diagnosticsView.kind === "loaded"
      ? summarizeRoutingDisposition(diagnosticsView.value, messages.observe)
      : null;
  const selectedRoleMetric =
    roleMetrics.modelMetricsByScope[model.id]?.[metricsScope] ?? null;
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
              aria-label={messages.modelDetail.copyModelIdAria()}
              variant="ghost"
              size="icon-xs"
              className="size-6 shrink-0 rounded-md text-muted-foreground hover:text-foreground"
            />
          </span>
        }
      >
        {/* 「路由是否就绪」的结论放在身份徽章之前：从列表点着「无法路由」
            进来，第一眼不该是一个绿色的「已启用」。 */}
        {routingConclusion ? (
          <OperatorStatusBadge
            intent={routingConclusion.intent}
            preserveLabel
            label={routingConclusion.label}
            
          />
        ) : null}
        {/* is_enabled 是配置布尔，不是运行观测：用类型徽章，不占运行态色阶。 */}
        <OperatorTypeBadge
          intent={model.is_enabled ? "accent" : "muted"}
          preserveLabel
          label={
            model.is_enabled
              ? messages.modelDetail.enabled
              : messages.modelDetail.disabled
          }
        />
        <OperatorTypeBadge
          intent={model.direct_request_enabled === true ? "accent" : "neutral"}
          preserveLabel
          label={model.direct_request_enabled === true ? messages.modelsPage.viewEntries : messages.modelsPage.viewModelTargets}
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
        {/* 删除的唯一入口原本藏在列表行的 hover 菜单里：站在详情页确认了
            要删的就是这一个，却必须回列表凭记忆再找一遍。 */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label={messages.modelsPage.modelMoreActions(
                model.display_name || model.model_id,
              )}
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              variant="destructive"
              onSelect={() => setDeleteTarget(model as never)}
            >
              <Trash2 />
              {messages.modelsUi.deleteModel}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </OperatorPageHeader>

      <OperatorFreshnessBar
        updatedAt={
          data.currentStateGeneratedAt ? (
            messages.freshness.updatedAt(
              formatTime(data.currentStateGeneratedAt),
            )
          ) : (
            <OperatorMissingValue reason={messages.freshness.neverLoaded} />
          )
        }
        basis={messages.modelDetail.runtimeBasis}
        autoRefresh={{
          ariaLabel: messages.freshness.autoRefreshAria,
          value: data.autoRefreshValue,
          onChange: (value) =>
            data.setAutoRefreshValue(value as "off" | "30s" | "60s"),
          options: [
            { value: "30s", label: messages.freshness.autoRefreshSeconds("30") },
            { value: "60s", label: messages.freshness.autoRefreshSeconds("60") },
            { value: "off", label: messages.freshness.autoRefreshOff },
          ],
        }}
        badges={
          data.currentStateFailure ? (
            <OperatorStalenessBadge
              label={
                data.currentStateFailure.staleData
                  ? messages.routing.stateStale
                  : messages.routing.runtimeReadFailed
              }
              reason={messages.routing.runtimeReadFailedReason(
                data.currentStateFailure.message,
              )}
            />
          ) : null
        }
        refresh={{
          label: messages.freshness.refresh,
          pending:
            data.currentStateLoading ||
            roleMetrics.metricsLoading ||
            diagnosticsView.kind === "loading",
          // 「刷新」必须真的刷新这一页读到的东西，而不是其中三分之一。
          onRefresh: () => {
            data.refreshCurrentState();
            roleMetrics.refresh();
            refreshDiagnostics();
            data.refreshCatalogPricingReads();
            void data.retryLoad();
          },
        }}
      />

      {degradedNotices.length > 0 ? (
        <OperatorCallout intent="warning" title={messages.honesty.readFailed}>
          <ul className="flex list-disc flex-col gap-0.5 pl-4">
            {degradedNotices.map((notice) => (
              <li key={notice}>{notice}</li>
            ))}
          </ul>
        </OperatorCallout>
      ) : null}

      <RouteReadinessCard
        accessTargetSummary={data.accessTargetSummary}
        diagnosticsView={diagnosticsView}
        onRetryDiagnostics={refreshDiagnostics}
        model={model}
      />

      <AccessTargetsEditor
        onGeneratePricing={setPricingTarget}
        apiFamilyLabel={model.api_family}
        modelEnabled={model.is_enabled}
        ownerModelId={model.model_id}
        focusedConnectionId={data.focusedConnectionId}
        connectionRowRefs={data.connectionCardRefs}
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
      <ModelRoleMetrics
        failed={roleMetrics.metricsFailed}
        loading={roleMetrics.metricsLoading}
        metric={selectedRoleMetric}
        spendRetentionFrom={spendRetentionFrom}
        onRetry={roleMetrics.refresh}
        onScopeChange={(nextScope) => {
          const next = new URLSearchParams(resolvedSearchParams);
          if (nextScope === "ingress") next.delete("metrics_scope");
          else next.set("metrics_scope", nextScope);
          setSearchParams(next, { replace: true });
        }}
        scope={metricsScope}
      />

      <ExternalCatalogSourcesSection
        modelConfigId={parsedModelConfigId ?? 0}
        prismModelId={model.model_id}
        apiFamily={model.api_family}
        catalogView={modelCatalogView}
        piController={piController}
        piRead={piSource.read}
        piReadFailed={piSource.readFailed}
        piReadStale={piSource.readStale}
        piReadError={piSource.readError}
        piLastSuccessfulAt={piSource.lastSuccessfulAt}
        piActionsBlocked={piSource.actionsBlocked}
        piView={piSource.piView}
        onPiRetry={() => {
          void piSource.reconcile().catch(() => {
            // The panel renders the authoritative error surface.
          });
        }}
        piReadPending={piSource.readPending}
        piReadRefreshing={piSource.readRefreshing}
        onCatalogChanged={modelCatalogView.refresh}
      />


      <CopyTerminalTargetDialog
        isOpen={copyTarget !== null}
        modelConfigId={parsedModelConfigId ?? 0}
        connectionId={copyTarget?.id ?? 0}
        connectionName={copyTarget?.name ?? copyTarget?.endpoint?.name ?? ""}
        ownerMode={copyTarget?.openai_text_capability ?? null}
        ownerImageCapability={copyTarget?.openai_image_capability ?? null}
        models={data.targetModelsForApiFamily}
        onClose={() => setCopyTarget(null)}
        onCopied={() => {
          setCopyTarget(null);
          void data.refreshModels().catch((error) => {
            console.error("Failed to refresh authoritative model list", error);
          });
          navigateTo(`/route/models/${model.id}`);
        }}
      />
      {/* Mounting only while a target is open lets the dialog initialize its
          selection from that target; keeping it mounted would freeze the
          initial empty selection. */}
      {pricingTarget !== null && (
        <CatalogPricingDialog
          isOpen
          source={{
            kind: "bound_model",
            modelConfigId: parsedModelConfigId ?? 0,
          }}
          title={`${messages.modelCatalog.pricingDialogTitlePrefix}${
            (pricingTarget?.name ?? pricingTarget?.endpoint?.name)
              ? ` · ${pricingTarget.name ?? pricingTarget.endpoint?.name}`
              : ""
          }`}
          targets={data.targetConnectionsForApiFamily.map((connection) => ({
            id: connection.id,
            name: connection.name ?? connection.endpoint?.name ?? null,
          }))}
          initialConnectionIds={[pricingTarget.id]}
          lockedConnectionIds={[pricingTarget.id]}
          onClose={() => setPricingTarget(null)}
          onCommitted={(templateName, assignedCount) => {
            setPricingTarget(null);
            toast.success(
              messages.modelCatalog.pricingSuccessToast(
                templateName,
                assignedCount,
              ),
            );
            // The imported template is now referenced by live targets, so the
            // pricing collection, the target option cache, and this model's
            // target rows all have to re-read authoritatively.
            clearSharedReferenceData("pricingTemplates", 0);
            clearSharedReferenceData("connections", 0);
            void data.refreshCatalogPricingReads().catch((error) => {
              console.error(
                "Failed to refresh model detail after catalog pricing commit",
                error,
              );
              toast.error(messages.modelCatalog.pricingPostCommitRefreshFailed);
            });
          }}
        />
      )}
      <ConnectionDialog
        endpointsError={data.degradedParts.endpoints}
        pricingTemplatesError={data.degradedParts.pricingTemplates}
        onRetryReferenceData={() => void data.retryLoad()}
        isOpen={data.isConnectionDialogOpen}
        onOpenChange={data.setIsConnectionDialogOpen}
        apiFamily={model.api_family}
        ownerOpenAIAcceptedFormat={ownerOpenAIAcceptedFormat}
        ownerModelId={model.model_id}
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
        upstreamModelIdError={data.upstreamModelIdError}
        setUpstreamModelIdError={data.setUpstreamModelIdError}
        handleConnectionSubmit={data.handleConnectionSubmit}
        endpointSourceDefaultName={data.endpointSourceDefaultName}
        pricingTemplates={data.pricingTemplates}
        prefillConnections={data.targetConnectionsForApiFamily}
      />

      <DeleteModelDialog
        deleteTarget={deleteTarget}
        error={deleteError}
        referrers={data.allModels.filter((candidate) =>
          candidate.access_targets?.some(
            (target) => target.target_model_id?.trim() === model.model_id,
          ),
        )}
        onDelete={handleDelete}
        setDeleteTarget={(next) => setDeleteTarget(next as never)}
      />

      <ModelDialog
        editingModel={model}
        formData={data.formData}
        formError={data.modelFormError}
        isDialogOpen={data.isEditModelDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        dialogDescription={messages.modelDetail.modelSettingsDescription}
        showModelIdInEditMode={true}
        setFormData={data.setFormData}
        setIsDialogOpen={data.setIsEditModelDialogOpen}
        setLoadbalanceStrategyId={data.setLoadbalanceStrategyId}
        onSubmit={data.handleEditModelSubmit}
      />
    </OperatorPageShell>
  );
}

export default ModelDetailFeaturePage;

function ModelRoleMetrics({
  failed,
  loading,
  metric,
  onRetry,
  onScopeChange,
  scope,
  spendRetentionFrom,
}: {
  failed: boolean;
  loading: boolean;
  metric: ModelDerivedMetric | null;
  onRetry: () => void;
  onScopeChange: (scope: DetailMetricsScope) => void;
  scope: DetailMetricsScope;
  /** 成本窗口被保留期裁剪时的实际起点（已格式化）。 */
  spendRetentionFrom: string | null;
}) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.modelDetail;
  // 加载中不是「没有值」：四张卡必须同时进入等待态，
  // 否则慢后端下会读到「窗口内没有延迟样本」这种假结论。
  const metricsPending = loading && metric === null;
  const latencyPartial = (metric?.samples?.latency_missing_count ?? 0) > 0;
  const costPartial = (metric?.samples?.cost_missing_count ?? 0) > 0;

  return (
    <OperatorSectionCard
      title={copy.roleMetricsTitle}
      description={
        scope === "ingress"
          ? copy.roleMetricsIngressDescription
          : copy.roleMetricsFinalDescription
      }
      actions={
        <MetricsScopeSwitch<DetailMetricsScope>
          label={copy.roleMetricsScopeLabel}
          value={scope}
          onChange={onScopeChange}
          options={[
            {
              value: "ingress" as const,
              label: copy.roleMetricsIngress,
              basis: messages.modelsPage.metricsScopeBasis("ingress"),
            },
            {
              value: "final_execution" as const,
              label: copy.roleMetricsFinal,
              basis: messages.modelsPage.metricsScopeBasis("final_execution"),
            },
            {
              value: "route_attempt",
              label: messages.modelsPage.scopeRouteAttempt,
              basis: messages.modelsPage.metricsScopeBasis("route_attempt"),
              disabledReason: copy.roleMetricsRouteAttemptDisabled,
            },
          ]}
        />
      }
    >
      {failed && metric === null ? (
        <OperatorErrorState
          title={copy.roleMetricsUnavailable}
          description={copy.roleMetricsUnavailableReason}
          action={
            <OperatorRetryButton onClick={onRetry}>
              {messages.common.retry}
            </OperatorRetryButton>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          {failed && metric ? (
            <OperatorStalenessBadge
              className="self-start"
              label={copy.roleMetricsStale}
              reason={copy.roleMetricsUnavailableReason}
            />
          ) : null}
          <div
            role="status"
            aria-live="polite"
            aria-busy={metricsPending}
            className="grid gap-[var(--density-card-gap)] sm:grid-cols-2 xl:grid-cols-4 [&>[data-slot=kpi-card]]:bg-inset"
          >
            <OperatorKpiCard
              label={
                scope === "ingress"
                  ? copy.roleMetricsIngressRequests
                  : copy.roleMetricsFinalRequests
              }
              value={
                metricsPending ? (
                  <Skeleton className="h-7 w-24" />
                ) : metric?.request_count_24h == null ? (
                  <OperatorMissingValue reason={messages.honesty.noValue} />
                ) : (
                  formatNumber(metric.request_count_24h)
                )
              }
              detail={copy.roleMetricsWindow24h}
            />
            <OperatorKpiCard
              label={copy.roleMetricsCompletionRate}
              value={
                metricsPending ? (
                  <Skeleton className="h-7 w-20" />
                ) : metric?.success_rate == null ? (
                  <OperatorMissingValue
                    reason={copy.roleMetricsNoDenominator}
                  />
                ) : (
                  `${formatNumber(metric.success_rate, { maximumFractionDigits: 1 })}%`
                )
              }
              detail={copy.roleMetricsWindow24h}
            />
            <OperatorKpiCard
              label={
                scope === "ingress"
                  ? copy.roleMetricsEndToEndP95
                  : copy.roleMetricsFinalAttemptP95
              }
              value={
                metricsPending ? (
                  <Skeleton className="h-7 w-24" />
                ) : metric?.p95_latency_ms == null ? (
                  <OperatorMissingValue
                    reason={copy.roleMetricsNoLatencySample}
                  />
                ) : (
                  formatLatencyForDisplay(metric.p95_latency_ms)
                )
              }
              detail={copy.roleMetricsWindow24h}
              badges={
                latencyPartial ? (
                  <OperatorClippedBadge
                    label={copy.roleMetricsPartial}
                    reason={copy.roleMetricsLatencyPartial(
                      metric?.samples?.latency_sample_count ?? 0,
                      metric?.samples?.latency_missing_count ?? 0,
                    )}
                  />
                ) : null
              }
            />
            <OperatorKpiCard
              label={copy.roleMetricsKnownCost}
              value={
                metricsPending ? (
                  <Skeleton className="h-7 w-28" />
                ) : metric?.known_cost_micros == null ? (
                  <OperatorMissingValue
                    reason={copy.roleMetricsNoTrustedCost}
                  />
                ) : (
                  // 币种写在卡组口径里说一次；可变小数位会让同列的数字小数点错位。
                  <span
                    title={formatMoneyMicros(
                      metric.known_cost_micros,
                      currencyState.currency.symbol,
                      currencyState.currency.code,
                      2,
                      6,
                      locale,
                    )}
                  >
                    {formatMoneyMicros(
                      metric.known_cost_micros,
                      currencyState.currency.symbol,
                      undefined,
                      2,
                      2,
                      locale,
                    )}
                  </span>
                )
              }
              detail={
                spendRetentionFrom
                  ? copy.roleMetricsWindow30dClipped(spendRetentionFrom)
                  : copy.roleMetricsWindow30d
              }
              badges={
                <>
                  {spendRetentionFrom ? (
                    <OperatorClippedBadge
                      label={messages.honesty.outsideRetention}
                      reason={copy.roleMetricsCostClippedReason(
                        spendRetentionFrom,
                      )}
                    />
                  ) : null}
                  {costPartial ? (
                    <OperatorClippedBadge
                      label={copy.roleMetricsPartial}
                      reason={copy.roleMetricsCostPartial(
                        metric?.samples?.cost_sample_count ?? 0,
                        metric?.samples?.cost_missing_count ?? 0,
                      )}
                    />
                  ) : null}
                </>
              }
            />
          </div>
        </div>
      )}
    </OperatorSectionCard>
  );
}
