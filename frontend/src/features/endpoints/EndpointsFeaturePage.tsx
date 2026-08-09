import { Plug, Plus } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useLocale } from "@/i18n/useLocale"
import { OperatorEmptyState, OperatorErrorState, OperatorLoadingState, OperatorPageHeader, OperatorPageShell, OperatorSearchInput, OperatorToolbar, OperatorRetryButton } from "@/shared/design-system"
import { AttachToModelDialog } from "@/pages/endpoints/AttachToModelDialog"
import { DeleteEndpointDialog } from "@/pages/endpoints/DeleteEndpointDialog"
import { EndpointDialog } from "./EndpointDialog"
import { EndpointTable } from "./EndpointTable"
import { OrphanCleanupDialog } from "@/pages/endpoints/OrphanCleanupDialog"
import { useEndpointsFeatureData, type ReviewFilter } from "./useEndpointsFeatureData"

export function EndpointsFeaturePage() {
  const { messages } = useLocale()
  const copy = messages.endpointsPage
  const data = useEndpointsFeatureData()

  const filterOptions: Array<{ value: ReviewFilter; label: string }> = [
    { value: "all", label: copy.filterAll },
    { value: "referenced", label: copy.filterReferenced },
    { value: "unreferenced", label: copy.filterUnreferenced },
    { value: "inactive_only", label: copy.filterInactiveOnly },
  ]

  const showToolbar = data.endpoints.length > 0

  return (
    <OperatorPageShell data-testid="endpoints-feature-page">
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Button onClick={() => data.setIsCreateOpen(true)}><Plus data-icon="inline-start" />{copy.addEndpoint}</Button>
      </OperatorPageHeader>

      {data.isLoading ? (
        <OperatorLoadingState title={copy.title} />
      ) : data.endpointLoadError ? (
        <OperatorErrorState
          title={messages.endpointsData.loadFailed}
          description={messages.common.requestFailed}
          action={<OperatorRetryButton onClick={data.retryEndpointLoad}>{messages.endpointsUi.deleteRetry}</OperatorRetryButton>}
        />
      ) : data.endpoints.length === 0 ? (
        <OperatorEmptyState icon={<Plug className="h-6 w-6" />} title={copy.noEndpointsConfigured} description={copy.noEndpointsConfiguredDescription} action={<Button onClick={() => data.setIsCreateOpen(true)}><Plus data-icon="inline-start" />{copy.addEndpoint}</Button>} />
      ) : (
        <>
          {showToolbar ? (
            <OperatorToolbar>
              <OperatorSearchInput name="endpoints_search" autoComplete="off" placeholder={copy.searchEndpoints} value={data.searchQuery} onChange={(event) => data.setSearchQuery(event.target.value)} />
              <div className="flex min-w-0 items-center gap-2">
                <Select
                  value={data.effectiveFilter}
                  disabled={data.filterDisabled}
                  onValueChange={(value) => data.setReviewFilter(value as ReviewFilter)}
                >
                  <SelectTrigger className="w-44" aria-label={copy.filterAll}>
                    <SelectValue placeholder={copy.filterAll} />
                  </SelectTrigger>
                  <SelectContent>
                    {filterOptions.map(({ value, label }) => (
                      <SelectItem key={value} value={value}>{label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {data.filterDisabled ? (
                  <p className="max-w-52 text-xs text-muted-foreground" id="endpoint-reference-filter-disabled-reason">
                    {copy.referenceFilterDisabled}
                  </p>
                ) : null}
              </div>
            </OperatorToolbar>
          ) : null}

          {data.endpoints.length > 0 && data.filteredEndpoints.length === 0 ? (
            <OperatorEmptyState icon={<Plug className="h-6 w-6" />} title={copy.noEndpointsMatchFilters} description={copy.noEndpointsMatchFiltersDescription} action={<Button variant="outline" onClick={() => { data.setSearchQuery(""); data.setReviewFilter("all") }}>{copy.filterAll}</Button>} />
          ) : null}

          {data.filteredEndpoints.length > 0 ? (
            <div className="operator-section-surface overflow-hidden rounded-xl border">
              <EndpointTable
                endpoints={data.filteredEndpoints}
                details={data.references.details}
                filterDisabled={data.filterDisabled}
                formatTime={data.formatTime}
                hasIntegrityError={data.references.hasIntegrityError}
                hasReferenceError={data.references.hasReferenceError}
                onAttach={data.handleAttachNavigate}
                onDelete={data.handleDeleteRequest}
                onDuplicate={data.handleDuplicateEndpoint}
                onEdit={data.setEditingEndpoint}
                onLoadMore={data.handleLoadMoreBlockers}
                onOpenReferences={data.references.loadDetail}
                onOrphanCleanup={(endpoint, item) => data.setOrphanCleanupTarget({ endpoint, item })}
                onRetryReferences={data.references.retry}
                onSort={data.toggleSort}
                sort={{ column: data.sortKey, direction: data.sortDescending ? "desc" : "asc" }}
                summaries={data.references.summaries}
              />
            </div>
          ) : null}

          {data.filterDisabled ? (
            <p className="sr-only" aria-live="polite">{copy.referenceFilterDisabled}</p>
          ) : null}
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
        onOrphanCleanup={(endpoint, item) => data.setOrphanCleanupTarget({ endpoint, item })}
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
  )
}

export default EndpointsFeaturePage
