import { useCallback, useMemo, useState } from "react";
import { Plus, SlidersHorizontal } from "lucide-react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { cn } from "@/lib/utils";
import { readSpendingRetentionClip } from "./metricsCoverage";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardHeader } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import {
  OperatorCallout,
  OperatorClippedBadge,
  OperatorErrorState,
  OperatorFreshnessBar,
  OperatorKpiCard,
  OperatorMissingValue,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorRetryButton,
  OperatorSearchInput,
  OperatorStalenessBadge,
  OperatorStatusBadge,
} from "@/shared/design-system";
import { CreateModelDialog } from "@/pages/models/CreateModelDialog";
import { DeleteModelDialog } from "@/pages/models/DeleteModelDialog";
import { ModelDialog } from "@/pages/models/ModelDialog";
import { ModelsTable } from "./ModelsTable";
import { ModelsMetricsScopeSwitch } from "./ModelsMetricsScopeSwitch";
import { ModelInventoryViewSwitch } from "./ModelInventoryViewSwitch";
import {
  hasModelTarget,
  isSingleTruncated,
  isUpstreamDecoupled,
} from "./modelRoutingFlags";
import { filterModelsByInventoryView, type ModelInventoryView } from "./modelView";
import { useModelsPageData } from "@/pages/models/useModelsPageData";
import {
  DEFAULT_MODELS_LIST_FILTERS,
  modelsQueryKeys,
  normalizeModelsListFilters,
} from "./queryKeys";

export function ModelsFeaturePage() {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const search = useSearch({ from: "/route/models" });
  const scope = search.scope ?? "ingress";
  const data = useModelsPageData(
    0,
    scope as "ingress" | "final_execution" | "route_attempt",
  );
  const copy = messages.modelsPage;
  const navigate = useNavigate();
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());

  const searchText = search.search ?? "";
  const view: ModelInventoryView = search.view === "model_targets" || search.view === "all" ? search.view : "entries";
  const apiFamilyFilter =
    search.api_family ?? DEFAULT_MODELS_LIST_FILTERS.api_family;
  const statusFilter = search.status ?? DEFAULT_MODELS_LIST_FILTERS.status;
  const flagFilter = search.flag ?? "all";

  // Filters, sort and page all live in the URL, so a filtered view is a link
  // an operator can hand to someone else.
  const patchSearch = useCallback(
    (patch: Record<string, string | number | undefined>, resetPage = true) => {
      void navigate({
        to: "/route/models",
        search: (current: Record<string, unknown>) => {
          const next = { ...current, ...patch };
          if (resetPage) delete next.page;
          for (const key of Object.keys(next)) {
            const value = next[key];
            if (value === undefined || value === "" || (value === "all" && key !== "view"))
              delete next[key];
          }
          return next;
        },
        replace: true,
        // Filters, sort and paging are in-page state changes, not navigations.
        // The search box writes on every keystroke, and the pager sits at the
        // bottom of the table, so the router's default scroll reset would throw
        // the operator back to the top on each one.
        resetScroll: false,
      });
    },
    [navigate],
  );

  const visibleModels = useMemo(() => filterModelsByInventoryView(data.models, view), [data.models, view]);

  const filtered = useMemo(() => {
    const query = searchText.trim().toLowerCase();
    return visibleModels.filter(
      (model: import("@/lib/api/models").ManagedModelConfigListItem) => {
        if (query) {
          const haystack =
            `${model.model_id} ${model.display_name ?? ""}`.toLowerCase();
          if (!haystack.includes(query)) return false;
        }
        if (apiFamilyFilter !== "all" && model.api_family !== apiFamilyFilter)
          return false;
        if (statusFilter === "enabled" && !model.is_enabled) return false;
        if (statusFilter === "disabled" && model.is_enabled) return false;
        if (
          flagFilter === "needs_target" &&
          model.routing_summary?.total_access_target_count !== 0
        )
          return false;
        if (flagFilter === "single_truncated" && !isSingleTruncated(model))
          return false;
        if (flagFilter === "upstream_decoupled" && !isUpstreamDecoupled(model))
          return false;
        if (flagFilter === "has_model_target" && !hasModelTarget(model))
          return false;
        return true;
      },
    );
  }, [apiFamilyFilter, flagFilter, searchText, statusFilter, visibleModels]);

  // 窄屏下三个下拉默认折起；折起时按钮上必须显示还有几个筛选在生效。
  const [filtersOpen, setFiltersOpen] = useState(false);
  const activeDropdownFilters = [
    apiFamilyFilter !== "all",
    statusFilter !== "all",
    flagFilter !== "all",
  ].filter(Boolean).length;

  const hasActiveFilters =
    searchText.trim() !== "" ||
    apiFamilyFilter !== "all" ||
    statusFilter !== "all" ||
    flagFilter !== "all";

  const stats = useMemo(() => {
    const enabled = visibleModels.filter(
      (model: import("@/lib/api/models").ManagedModelConfigListItem) =>
        model.is_enabled,
    ).length;
    return {
      total: visibleModels.length,
      enabled,
      disabled: visibleModels.length - enabled,
      needsTarget: visibleModels.filter(
        (model: import("@/lib/api/models").ManagedModelConfigListItem) =>
          model.routing_summary?.total_access_target_count === 0,
      ).length,
      singleTruncated: visibleModels.filter(isSingleTruncated).length,
    };
  }, [visibleModels]);
  // 后端说清了成本只覆盖到哪一天；列头与新鲜度条都要把这件事说出来。
  const spendRetentionClip = readSpendingRetentionClip(data.metricsCoverage);
  const spendRetentionFrom = spendRetentionClip?.retentionFrom
    ? formatTime(spendRetentionClip.retentionFrom, {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      })
    : null;

  // 同一个计数只渲染一次：页头副标题与分页各自的数字曾经互相矛盾。
  const listCountSummary = hasActiveFilters
    ? copy.listStatusSummaryFiltered(
        formatNumber(filtered.length),
        formatNumber(visibleModels.length),
      )
    : copy.listStatusSummary(formatNumber(visibleModels.length));
  const listAnomalySummary =
    stats.needsTarget > 0 || stats.disabled > 0
      ? copy.listStatusAnomalies(
          formatNumber(stats.needsTarget),
          formatNumber(stats.disabled),
        )
      : null;
  const filters = normalizeModelsListFilters({
    search: searchText,
    api_family: apiFamilyFilter,
    status: statusFilter,
  });
  const queryKey = modelsQueryKeys.list(1, filters);

  if (data.loading) {
    // 骨架渲染真实的壳：页头、五张 KPI、筛选区与表格行位。
    // 一块 500px 的实心矩形不预示任何布局，落定时整页会跳三次。
    return (
      <OperatorPageShell
        role="status"
        aria-busy
        aria-label={messages.common.loadingApplication}
        data-testid="models-feature-loading"
      >
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-8 w-full rounded-md" />
        <div className="grid gap-[var(--density-card-gap)] grid-cols-2 md:grid-cols-3 lg:grid-cols-5">
          {[0, 1, 2, 3, 4].map((tile) => (
            <Skeleton key={tile} className="h-[72px] rounded-lg" />
          ))}
        </div>
        <Card className="gap-0 overflow-hidden rounded-lg">
          <CardHeader className="gap-2 border-b py-2">
            <Skeleton className="h-4 w-32" />
          </CardHeader>
          <CardContent className="p-0">
            <div className="flex flex-col gap-2 p-[var(--density-card-pad-x)]">
              <Skeleton className="h-9 w-full md:max-w-sm" />
            </div>
            <Skeleton className="h-[280px] rounded-none border-0" />
          </CardContent>
        </Card>
      </OperatorPageShell>
    );
  }

  if (data.loadError) {
    // 一次读取失败不该把操作者所在的位置也抹掉：页头留在原处，标题说明
    // 这里仍是模型配置页，「新建模型配置」这条出路也仍然可用。
    return (
      <OperatorPageShell data-testid="models-feature-error">
        <OperatorPageHeader title={copy.title}>
          <Button
            variant="outline"
            onClick={() => void navigate({ to: "/route/models/export" })}
          >
            {messages.modelExportPage.entryButton}
          </Button>
          <Button onClick={() => data.setCreateDialogOpen(true)}>
            <Plus data-icon="inline-start" />
            {copy.newModel}
          </Button>
        </OperatorPageHeader>

        <OperatorErrorState
          title={messages.modelsData.fetchFailed}
          description={messages.honesty.readFailedDescription}
          details={data.loadError}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <OperatorRetryButton onClick={data.retryLoad}>
              {messages.common.retry}
            </OperatorRetryButton>
          }
        />

        <CreateModelDialog
          isOpen={data.createDialogOpen}
          loadbalanceStrategies={data.loadbalanceStrategies}
          createLoadbalanceStrategyDefaultsPending={
            data.loadbalanceStrategyDefaultsCreating
          }
          onCreateLoadbalanceStrategyDefaults={
            data.handleCreateLoadbalanceStrategyDefaults
          }
          onClose={() => data.setCreateDialogOpen(false)}
          onCreated={data.handleModelCreated}
        />
      </OperatorPageShell>
    );
  }

  return (
    <OperatorPageShell
      data-testid="models-feature-page"
      data-query-key={JSON.stringify(queryKey)}
    >
      <OperatorPageHeader title={copy.title}>
        {/* Export entry: the standalone client-config export page. */}
        <Button
          variant="outline"
          onClick={() => void navigate({ to: "/route/models/export" })}
        >
          {messages.modelExportPage.entryButton}
        </Button>
        <Button onClick={() => data.setCreateDialogOpen(true)}>
          <Plus data-icon="inline-start" />
          {copy.newModel}
        </Button>
      </OperatorPageHeader>

      <OperatorFreshnessBar
        updatedAt={
          data.metricsLastSuccessAt ? (
            messages.freshness.updatedAt(formatTime(data.metricsLastSuccessAt))
          ) : (
            <OperatorMissingValue reason={messages.freshness.neverLoaded} />
          )
        }
        basis={copy.listBasis}
        badges={
          <>
            {data.metricsFailed ? (
              <OperatorStalenessBadge
                label={copy.metricsUnavailable}
                reason={copy.metricsUnavailableReason}
              />
            ) : null}
            {spendRetentionFrom ? (
              <OperatorClippedBadge
                label={messages.honesty.outsideRetention}
                reason={copy.spendWindowClippedReason(spendRetentionFrom)}
              />
            ) : null}
          </>
        }
        refresh={{
          label: messages.freshness.refresh,
          pending: data.metricsLoading,
          onRefresh: () => {
            data.refreshMetrics();
            data.refreshModels();
          },
        }}
      />

      {/* 统计后端抖动会让整表变红；页面级通知给出唯一的恢复动作，
          而不是让操作者在 44 个红字单元格里找出路。 */}
      {data.metricsFailed ? (
        <OperatorCallout
          intent="warning"
          role="alert"
          description={copy.metricsReadFailedNotice}
          action={
            <OperatorRetryButton onClick={() => data.refreshMetrics()}>
              {messages.common.retry}
            </OperatorRetryButton>
          }
        />
      ) : null}

      <div className="grid gap-[var(--density-card-gap)] grid-cols-2 md:grid-cols-3 lg:grid-cols-5">
        <OperatorKpiCard
          compact
          label={view === "model_targets" ? copy.viewModelTargets : view === "all" ? copy.viewAll : copy.kpiTotal}
          value={formatNumber(stats.total)}
          detail={copy.kpiTotalDetail}
          pressed={!hasActiveFilters}
          onClick={() =>
            patchSearch({
              search: undefined,
              status: undefined,
              flag: undefined,
              api_family: undefined,
            })
          }
        />
        <OperatorKpiCard
          compact
          label={copy.kpiEnabled}
          value={formatNumber(stats.enabled)}
          detail={copy.kpiEnabledDetail}
          pressed={statusFilter === "enabled"}
          onClick={() => patchSearch({ status: "enabled", flag: undefined })}
        />
        <OperatorKpiCard
          compact
          label={copy.kpiDisabled}
          value={formatNumber(stats.disabled)}
          detail={copy.kpiDisabledDetail}
          pressed={statusFilter === "disabled"}
          onClick={() => patchSearch({ status: "disabled", flag: undefined })}
        />
        <OperatorKpiCard
          compact
          label={copy.kpiNeedsTarget}
          value={formatNumber(stats.needsTarget)}
          detail={copy.kpiNeedsTargetDetail}
          intent={stats.needsTarget > 0 ? "failing" : "default"}
          pressed={flagFilter === "needs_target"}
          badges={
            stats.needsTarget > 0 ? (
              <OperatorStatusBadge
                intent="failing"
                preserveLabel
                label={copy.kpiNeedsTargetBadge}
              />
            ) : null
          }
          onClick={() =>
            patchSearch({ flag: "needs_target", status: undefined })
          }
        />
        <OperatorKpiCard
          compact
          label={copy.kpiSingleTruncated}
          value={formatNumber(stats.singleTruncated)}
          detail={copy.kpiSingleTruncatedDetail}
          intent={stats.singleTruncated > 0 ? "degraded" : "default"}
          pressed={flagFilter === "single_truncated"}
          badges={
            stats.singleTruncated > 0 ? (
              <OperatorStatusBadge
                intent="degraded"
                preserveLabel
                label={copy.kpiSingleTruncatedBadge}
              />
            ) : null
          }
          onClick={() =>
            patchSearch({ flag: "single_truncated", status: undefined })
          }
        />
      </div>

      <Card className="operator-table-shell gap-0 overflow-hidden rounded-lg">
        <CardHeader className="gap-2 border-b py-2">
          <div
            aria-live="polite"
            className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground"
            data-testid="models-list-summary"
          >
            <span>{listCountSummary}</span>
            {listAnomalySummary ? <span>·</span> : null}
            {listAnomalySummary ? <span>{listAnomalySummary}</span> : null}
          </div>
          <CardAction>
            <ModelInventoryViewSwitch
              onViewChange={(value) => patchSearch({ view: value === "entries" ? undefined : value })}
              view={view}
            />
          </CardAction>
          {/* 窄屏上四个字段竖排会把表头顶到第二屏：搜索常驻，三个下拉折起来。 */}
          <FieldGroup className="gap-2 md:flex-row md:items-end [&_[data-slot=field-label]]:sr-only">
            <Field className="md:max-w-sm">
              <FieldLabel htmlFor="models-search">
                {copy.searchLabel}
              </FieldLabel>
              <div className="flex min-w-0 items-center gap-2">
                <OperatorSearchInput
                  id="models-search"
                  name="models_search"
                  autoComplete="off"
                  className="min-w-0 flex-1"
                  placeholder={copy.searchModels}
                  value={searchText}
                  onChange={(event) =>
                    patchSearch({ search: event.target.value })
                  }
                />
                <Button
                  type="button"
                  variant="outline"
                  aria-expanded={filtersOpen}
                  className="shrink-0 md:hidden"
                  onClick={() => setFiltersOpen((current) => !current)}
                >
                  <SlidersHorizontal data-icon="inline-start" />
                  {activeDropdownFilters > 0
                    ? copy.filtersToggle(formatNumber(activeDropdownFilters))
                    : copy.filtersToggleEmpty}
                </Button>
              </div>
            </Field>
            <Field className={cn("md:max-w-48", !filtersOpen && "max-md:hidden")}>
              <FieldLabel htmlFor="models-api-family">
                {copy.apiFamilyLabel}
              </FieldLabel>
              <Select
                value={apiFamilyFilter}
                onValueChange={(value) => patchSearch({ api_family: value })}
              >
                <SelectTrigger
                  id="models-api-family"
                  aria-label={copy.apiFamilyLabel}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">{copy.apiFamilyAll}</SelectItem>
                    <SelectItem value="openai">OpenAI</SelectItem>
                    <SelectItem value="anthropic">Anthropic</SelectItem>
                    <SelectItem value="gemini">Gemini</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field className={cn("md:max-w-48", !filtersOpen && "max-md:hidden")}>
              <FieldLabel htmlFor="models-status">
                {copy.statusLabel}
              </FieldLabel>
              <Select
                value={statusFilter}
                onValueChange={(value) => patchSearch({ status: value })}
              >
                <SelectTrigger id="models-status" aria-label={copy.statusLabel}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">{copy.statusAll}</SelectItem>
                    <SelectItem value="enabled">
                      {copy.statusEnabled}
                    </SelectItem>
                    <SelectItem value="disabled">
                      {copy.statusDisabled}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field className={cn("md:max-w-48", !filtersOpen && "max-md:hidden")}>
              <FieldLabel htmlFor="models-flag">{copy.flagLabel}</FieldLabel>
              <Select
                value={flagFilter}
                onValueChange={(value) => patchSearch({ flag: value })}
              >
                <SelectTrigger id="models-flag" aria-label={copy.flagLabel}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">{copy.flagAll}</SelectItem>
                    <SelectItem value="needs_target">
                      {copy.flagNeedsTarget}
                    </SelectItem>
                    <SelectItem value="single_truncated">
                      {copy.flagSingleTruncated}
                    </SelectItem>
                    <SelectItem value="upstream_decoupled">
                      {copy.flagUpstreamDecoupled}
                    </SelectItem>
                    <SelectItem value="has_model_target">
                      {copy.flagHasModelTarget}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          </FieldGroup>
        </CardHeader>
        <CardContent className="p-0" data-table-row-count={filtered.length}>
          <ModelsTable
            scope={scope}
            spendRetentionFrom={spendRetentionFrom}
            metricsScopeControl={
              <ModelsMetricsScopeSwitch
                onScopeChange={(value) =>
                  patchSearch({
                    scope: value === "ingress" ? undefined : value,
                  })
                }
                scope={scope}
              />
            }
            filtered={filtered}
            hasActiveFilters={hasActiveFilters}
            metricsFailed={data.metricsFailed}
            metricsLoading={data.metricsLoading}
            modelMetrics24h={data.modelMetrics24h}
            modelSpend30dMicros={data.modelSpend30dMicros}
            onClearFilters={() =>
              patchSearch({
                search: undefined,
                api_family: undefined,
                status: undefined,
                flag: undefined,
              })
            }
            onCreate={() => data.setCreateDialogOpen(true)}
            onEdit={data.handleOpenDialog}
            onPageChange={(page) => patchSearch({ page }, false)}
            onPageSizeChange={(pageSize) =>
              patchSearch({ page_size: pageSize })
            }
            onSelectionChange={setSelectedIds}
            onSetEnabled={data.setModelEnabled}
            onSetManyEnabled={data.setModelsEnabled}
            onSort={(column, direction) =>
              patchSearch({ sort_by: column, sort_order: direction })
            }
            page={search.page ?? 1}
            pageSize={search.page_size}
            selectedIds={selectedIds}
            setDeleteTarget={data.setDeleteTarget}
            sortBy={search.sort_by ?? "name"}
            sortOrder={search.sort_order ?? "asc"}
            togglingModelIds={data.togglingModelIds}
            view={view}
          />
        </CardContent>
      </Card>

      <CreateModelDialog
        isOpen={data.createDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        createLoadbalanceStrategyDefaultsPending={
          data.loadbalanceStrategyDefaultsCreating
        }
        onCreateLoadbalanceStrategyDefaults={
          data.handleCreateLoadbalanceStrategyDefaults
        }
        onClose={() => data.setCreateDialogOpen(false)}
        onCreated={data.handleModelCreated}
      />
      <ModelDialog
        editingModel={data.editingModel}
        formData={data.formData}
        formError={data.formError}
        isDialogOpen={data.isDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        createLoadbalanceStrategyDefaultsPending={
          data.loadbalanceStrategyDefaultsCreating
        }
        setFormData={data.setFormData}
        setIsDialogOpen={data.setIsDialogOpen}
        setLoadbalanceStrategyId={data.setLoadbalanceStrategyId}
        onCreateLoadbalanceStrategyDefaults={
          data.handleCreateLoadbalanceStrategyDefaults
        }
        onSubmit={data.handleSubmit}
      />
      <DeleteModelDialog
        deleteTarget={data.deleteTarget}
        error={data.deleteError}
        referrers={
          data.deleteTarget
            ? data.models.filter((candidate) =>
                candidate.access_targets?.some(
                  (target) =>
                    target.target_model_id?.trim() ===
                    data.deleteTarget?.model_id,
                ),
              )
            : []
        }
        onDelete={data.handleDelete}
        setDeleteTarget={(model) =>
          data.setDeleteTarget(model as typeof data.deleteTarget)
        }
      />
    </OperatorPageShell>
  );
}

export default ModelsFeaturePage;
