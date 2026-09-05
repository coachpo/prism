import { Plug, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import {
  OperatorEmptyState,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorSearchInput,
  OperatorStalenessBadge,
  OperatorToolbar,
  OperatorRetryButton,
} from "@/shared/design-system";
import { AttachToModelDialog } from "@/pages/endpoints/AttachToModelDialog";
import { DeleteEndpointDialog } from "@/pages/endpoints/DeleteEndpointDialog";
import { EndpointDialog } from "./EndpointDialog";
import { EndpointTable } from "./EndpointTable";
import { OrphanCleanupDialog } from "@/pages/endpoints/OrphanCleanupDialog";
import { useEndpointsFeatureData } from "./useEndpointsFeatureData";
import type { ReviewFilter } from "./useEndpointList";

export function EndpointsFeaturePage() {
  const { messages } = useLocale();
  const copy = messages.endpointsPage;
  const data = useEndpointsFeatureData();

  const filterOptions: Array<{ value: ReviewFilter; label: string }> = [
    { value: "all", label: copy.filterAll },
    { value: "referenced", label: copy.filterReferenced },
    { value: "unreferenced", label: copy.filterUnreferenced },
    { value: "inactive_only", label: copy.filterInactiveOnly },
  ];

  const showToolbar = data.endpoints.length > 0;
  const unknownCount = data.unknownReferenceIds.size;
  // 空态渲染在表体里：连表壳一起卸载会丢掉列头这个定位锚点，
  // 筛到零条时操作者最需要的恰恰是「还有哪些列可以放宽」。
  const filteredEmptyState =
    data.filteredEndpoints.length === 0 ? (
      <OperatorEmptyState
        icon={<Plug className="h-6 w-6" />}
        title={copy.noEndpointsMatchFilters}
        description={copy.noEndpointsMatchFiltersDescription}
        action={
          <Button
            variant="outline"
            onClick={() => {
              data.setSearchQuery("");
              data.setReviewFilter("all");
            }}
          >
            {copy.clearFilters}
          </Button>
        }
      />
    ) : null;
  // One line of facts, not a row of KPI cards: this page's numbers are small
  // and the table right below already carries the detail.
  const referencedCount = data.endpoints.filter((endpoint) => {
    const summary = data.references.summaries[endpoint.id];
    return (
      summary?.status === "ready" && summary.value.direct_reference_count > 0
    );
  }).length;
  const inactiveCount = data.endpoints.filter((endpoint) => {
    const summary = data.references.summaries[endpoint.id];
    return (
      summary?.status === "ready" &&
      summary.value.direct_reference_count > 0 &&
      summary.value.enabled_reference_count === 0
    );
  }).length;

  return (
    <OperatorPageShell data-testid="endpoints-feature-page">
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Button onClick={() => data.setIsCreateOpen(true)}>
          <Plus data-icon="inline-start" />
          {copy.addEndpoint}
        </Button>
      </OperatorPageHeader>

      {/* 读失败但手里还有上次成功的数据时，不丢弃它：整块换成错误卡，
          「后端挂了」就被渲染成「这台网关没有端点」。 */}
      {data.endpointLoadError && data.endpoints.length > 0 ? (
        <OperatorStalenessBadge
          className="self-start"
          label={
            data.endpointsLoadedAt
              ? messages.honesty.lastSuccessful(
                  data.formatTime(data.endpointsLoadedAt),
                )
              : messages.honesty.readFailed
          }
          reason={messages.endpointsData.loadFailed}
        />
      ) : null}

      {data.isLoading ? (
        <OperatorLoadingState title={copy.title} />
      ) : data.endpointLoadError && data.endpoints.length === 0 ? (
        <OperatorErrorState
          title={messages.endpointsData.loadFailed}
          description={messages.common.requestFailed}
          action={
            <OperatorRetryButton onClick={data.retryEndpointLoad}>
              {messages.endpointsUi.deleteRetry}
            </OperatorRetryButton>
          }
        />
      ) : data.endpoints.length === 0 ? (
        <OperatorEmptyState
          icon={<Plug className="h-6 w-6" />}
          title={copy.noEndpointsConfigured}
          description={copy.noEndpointsConfiguredDescription}
          action={
            <Button onClick={() => data.setIsCreateOpen(true)}>
              <Plus data-icon="inline-start" />
              {copy.addEndpoint}
            </Button>
          }
        />
      ) : (
        <>
          {showToolbar ? (
            <OperatorToolbar>
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <OperatorSearchInput
                  name="endpoints_search"
                  autoComplete="off"
                  placeholder={copy.searchEndpoints}
                  value={data.searchQuery}
                  onChange={(event) => data.setSearchQuery(event.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  {copy.overviewSummary(
                    String(data.endpoints.length),
                    String(referencedCount),
                    String(inactiveCount),
                  )}
                </p>
              </div>
              <div className="flex min-w-0 flex-col items-stretch gap-1 sm:items-end">
                <div className="flex min-w-0 items-center gap-2">
                  {/* Unknown references degrade per row, so the controls stay
                      usable; the badge names how many rows are affected. */}
                  {unknownCount > 0 ? (
                    <>
                      <OperatorStalenessBadge
                        label={copy.referenceStaleBadge}
                        reason={copy.referenceStaleReason(String(unknownCount))}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={data.references.retry}
                      >
                        {copy.referenceRefetchAll}
                      </Button>
                    </>
                  ) : null}
                  <Select
                    value={data.reviewFilter}
                    onValueChange={(value) =>
                      data.setReviewFilter(value as ReviewFilter)
                    }
                  >
                    <SelectTrigger className="w-52" aria-label={copy.filterAll}>
                      <SelectValue placeholder={copy.filterAll} />
                    </SelectTrigger>
                    <SelectContent>
                      {filterOptions.map(({ value, label }) => (
                        <SelectItem key={value} value={value}>
                          {value !== "all" && unknownCount > 0
                            ? `${label} · ${copy.referenceUnknownOption(String(unknownCount))}`
                            : label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                {/* 引用信息未就绪会把筛选/排序复位。复位本身是无声的，
                    这行常驻的播报区负责说出它为什么弹回默认值。 */}
                <p
                  aria-live="polite"
                  className="text-[11px] text-degraded empty:hidden"
                  data-testid="endpoint-reference-rollback-notice"
                >
                  {data.referenceRollbackNotice}
                </p>
              </div>
            </OperatorToolbar>
          ) : null}

          <div className="operator-section-surface overflow-hidden rounded-lg border">
            <EndpointTable
              emptyState={filteredEmptyState}
              endpoints={data.filteredEndpoints}
              details={data.references.details}
              formatTime={data.formatTime}
              hasIntegrityError={data.references.hasIntegrityError}
              onAttach={data.handleAttachNavigate}
              onDelete={data.handleDeleteRequest}
              onDuplicate={data.handleDuplicateEndpoint}
              onEdit={data.setEditingEndpoint}
              onLoadMore={data.handleLoadMoreBlockers}
              onOpenReferences={data.references.loadDetail}
              onOrphanCleanup={(endpoint, item) =>
                data.setOrphanCleanupTarget({ endpoint, item })
              }
              onRetryDetail={data.references.loadDetail}
              onSort={data.toggleSort}
              sort={{
                column: data.sortKey,
                direction: data.sortDescending ? "desc" : "asc",
              }}
              summaries={data.references.summaries}
            />
          </div>
        </>
      )}

      <EndpointDialog
        open={data.isCreateOpen}
        onOpenChange={data.setIsCreateOpen}
        onSubmit={data.handleCreate}
        mode="create"
        serverError={data.isCreateOpen ? data.endpointDialogError : null}
        fieldErrors={data.isCreateOpen ? data.endpointFieldErrors : null}
      />
      <EndpointDialog
        open={Boolean(data.editingEndpoint)}
        onOpenChange={(open) => !open && data.setEditingEndpoint(null)}
        onSubmit={data.handleUpdate}
        mode="edit"
        initialValues={data.editingEndpoint || undefined}
        serverError={data.editingEndpoint ? data.endpointDialogError : null}
        fieldErrors={data.editingEndpoint ? data.endpointFieldErrors : null}
      />
      <DeleteEndpointDialog
        state={data.deleteDialog}
        onConfirm={data.handleDeleteConfirm}
        onOpenChange={data.handleDeleteDialogOpenChange}
        onOrphanCleanup={(endpoint, item) =>
          data.setOrphanCleanupTarget({ endpoint, item })
        }
        onRetry={data.handleDeleteRetry}
        onLoadMore={data.handleLoadMoreBlockers}
      />
      <OrphanCleanupDialog
        target={data.orphanCleanupTarget}
        onConfirm={data.handleOrphanCleanup}
        onOpenChange={(open) => !open && data.setOrphanCleanupTarget(null)}
      />
      <AttachToModelDialog
        key={data.attachModelTarget?.id ?? "closed"}
        endpoint={data.attachModelTarget}
        onNavigate={data.handleAttachModelSelected}
        onOpenChange={(open) => !open && data.setAttachModelTarget(null)}
      />
    </OperatorPageShell>
  );
}

export default EndpointsFeaturePage;
