import { DragOverlay, DndContext } from "@dnd-kit/core"
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable"
import { Plug, Plus } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/i18n/useLocale"
import { OperatorEmptyState, OperatorPageHeader, OperatorPageShell, OperatorSearchInput, OperatorToolbar } from "@/shared/design-system"
import { DeleteEndpointDialog } from "./DeleteEndpointDialog"
import { EndpointCardView, SortableEndpointCard } from "./EndpointCard"
import { EndpointDialog } from "./EndpointDialog"
import { useEndpointsFeatureData } from "./useEndpointsFeatureData"

export function EndpointsFeaturePage() {
  const { messages } = useLocale()
  const data = useEndpointsFeatureData()
  const copy = messages.endpointsPage
  const showReviewToolbar = data.endpoints.length > 3 || data.hasActiveReviewFilters
  const reviewFilterOptions = [
    { value: "all", label: copy.filterAll },
    { value: "in-use", label: copy.filterInUse },
    { value: "unused", label: copy.filterUnused },
  ] as const

  return (
    <OperatorPageShell data-testid="endpoints-feature-page">
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Button onClick={() => data.setIsCreateOpen(true)}><Plus data-icon="inline-start" />{copy.addEndpoint}</Button>
      </OperatorPageHeader>
      {!data.isLoading && showReviewToolbar ? (
        <OperatorToolbar>
          <OperatorSearchInput name="endpoints_search" autoComplete="off" placeholder={copy.searchEndpoints} value={data.searchQuery} onChange={(event) => data.setSearchQuery(event.target.value)} />
          <div className="flex flex-wrap gap-2">
            {reviewFilterOptions.map(({ value, label }) => (
              <Button key={value} type="button" size="sm" variant={data.reviewFilter === value ? "default" : "outline"} aria-pressed={data.reviewFilter === value} onClick={() => data.setReviewFilter(value)}>{label}</Button>
            ))}
          </div>
        </OperatorToolbar>
      ) : null}
      {data.isLoading ? (
        <div className="flex flex-col gap-3">{[1, 2, 3, 4, 5, 6].map((index) => <Skeleton key={index} className="h-[88px] rounded-xl" />)}</div>
      ) : data.endpoints.length === 0 ? (
        <OperatorEmptyState icon={<Plug className="h-6 w-6" />} title={copy.noEndpointsConfigured} description={copy.noEndpointsConfiguredDescription} action={<Button onClick={() => data.setIsCreateOpen(true)}><Plus data-icon="inline-start" />{copy.addEndpoint}</Button>} />
      ) : data.filteredEndpoints.length === 0 ? (
        <OperatorEmptyState icon={<Plug className="h-6 w-6" />} title={copy.noEndpointsMatchFilters} description={copy.noEndpointsMatchFiltersDescription} />
      ) : (
        <DndContext sensors={data.sensors} collisionDetection={data.collisionDetection} onDragStart={data.handleDragStart} onDragCancel={data.handleDragCancel} onDragEnd={(event) => { void data.handleDragEnd(event) }}>
          <SortableContext items={data.visibleEndpointIds} strategy={verticalListSortingStrategy}>
            <div className="flex flex-col gap-3">
              {data.hasActiveReviewFilters ? <p className="text-xs text-muted-foreground">{copy.reorderDisabledWhileFilters}</p> : null}
              {data.filteredEndpoints.map((endpoint) => (
                <SortableEndpointCard key={endpoint.id} endpoint={endpoint} formatTime={data.formatTime} models={data.endpointModels[endpoint.id] ?? []} isDuplicating={data.duplicatingEndpointId === endpoint.id} reorderDisabled={!data.canReorder} onDuplicate={data.handleDuplicateEndpoint} onEdit={data.setEditingEndpoint} onDelete={data.setDeleteTarget} />
              ))}
            </div>
          </SortableContext>
          <DragOverlay>{data.activeDragEndpoint ? <EndpointCardView endpoint={data.activeDragEndpoint} formatTime={data.formatTime} models={data.endpointModels[data.activeDragEndpoint.id] ?? []} isOverlay /> : null}</DragOverlay>
        </DndContext>
      )}
      <EndpointDialog open={data.isCreateOpen} onOpenChange={data.setIsCreateOpen} onSubmit={data.handleCreate} description={copy.description} serverError={data.isCreateOpen ? data.endpointDialogError : null} title={copy.addEndpoint} submitLabel={copy.addEndpoint} />
      <EndpointDialog open={!!data.editingEndpoint} onOpenChange={(open) => !open && data.setEditingEndpoint(null)} onSubmit={data.handleUpdate} description={copy.description} serverError={data.editingEndpoint ? data.endpointDialogError : null} initialValues={data.editingEndpoint || undefined} title={copy.editEndpoint} submitLabel={copy.saveChanges} />
      <DeleteEndpointDialog deleteTarget={data.deleteTarget} displayTarget={data.deleteDialogTarget ?? data.deleteTarget} isDeletingEndpoint={data.isDeletingEndpoint} onOpenChange={data.handleDeleteDialogOpenChange} onConfirm={data.handleDelete} />
    </OperatorPageShell>
  )
}

export default EndpointsFeaturePage
