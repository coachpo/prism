import { DragOverlay, DndContext } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { EmptyState } from "@/components/EmptyState";
import { useLocale } from "@/i18n/useLocale";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Plug, Plus, Search } from "lucide-react";
import { EndpointDialog } from "./endpoints/EndpointDialog";
import { EndpointCardView, SortableEndpointCard } from "./endpoints/EndpointCard";
import { DeleteEndpointDialog } from "./endpoints/DeleteEndpointDialog";
import { useEndpointsPageData } from "./endpoints/useEndpointsPageData";

export function EndpointsPage() {
  const { messages } = useLocale();
  const data = useEndpointsPageData();
  const copy = messages.endpointsPage;
  const showReviewToolbar = data.endpoints.length > 3 || data.hasActiveReviewFilters;
  const reviewFilterOptions = [
    { value: "all", label: copy.filterAll },
    { value: "in-use", label: copy.filterInUse },
    { value: "unused", label: copy.filterUnused },
  ] as const;

  return (
    <div className="space-y-[var(--density-page-gap)]">
      <PageHeader
        title={copy.title}
        description={copy.description}
      >
        <Button onClick={() => data.setIsCreateOpen(true)}>
          <Plus className="mr-2 h-4 w-4" />
          {copy.addEndpoint}
        </Button>
      </PageHeader>

      {!data.isLoading && showReviewToolbar ? (
        <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative w-full xl:max-w-sm">
            <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              name="endpoints_search"
              type="search"
              autoComplete="off"
              placeholder={copy.searchEndpoints}
              value={data.searchQuery}
              onChange={(event) => data.setSearchQuery(event.target.value)}
              className="h-9 pl-9"
            />
          </div>

          <div className="flex flex-wrap gap-2">
            {reviewFilterOptions.map(({ value, label }) => (
              <Button
                key={value}
                type="button"
                size="sm"
                variant={data.reviewFilter === value ? "default" : "outline"}
                aria-pressed={data.reviewFilter === value}
                onClick={() => data.setReviewFilter(value)}
              >
                {label}
              </Button>
            ))}
          </div>
        </div>
      ) : null}

      {data.isLoading ? (
        <div className="flex flex-col gap-3">
          {[1, 2, 3, 4, 5, 6].map((index) => (
            <Skeleton key={index} className="h-[88px] rounded-xl" />
          ))}
        </div>
      ) : data.endpoints.length === 0 ? (
        <EmptyState
          icon={<Plug className="h-6 w-6" />}
          title={copy.noEndpointsConfigured}
          description={copy.noEndpointsConfiguredDescription}
          action={
            <Button onClick={() => data.setIsCreateOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              {copy.addEndpoint}
            </Button>
          }
        />
      ) : data.endpoints.length > 0 && data.filteredEndpoints.length === 0 ? (
        <EmptyState
          icon={<Plug className="h-6 w-6" />}
          title={copy.noEndpointsMatchFilters}
          description={copy.noEndpointsMatchFiltersDescription}
        />
      ) : (
        <DndContext
          sensors={data.sensors}
          collisionDetection={data.collisionDetection}
          onDragStart={data.handleDragStart}
          onDragCancel={data.handleDragCancel}
          onDragEnd={(event) => {
            void data.handleDragEnd(event);
          }}
        >
          <SortableContext items={data.visibleEndpointIds} strategy={verticalListSortingStrategy}>
            <div className="flex flex-col gap-3">
              {data.hasActiveReviewFilters ? (
                <p className="text-xs text-muted-foreground">
                  {copy.reorderDisabledWhileFilters}
                </p>
              ) : null}

              {data.filteredEndpoints.map((endpoint) => (
                <SortableEndpointCard
                  key={endpoint.id}
                  endpoint={endpoint}
                  formatTime={data.formatTime}
                  models={data.endpointModels[endpoint.id] ?? []}
                  isDuplicating={data.duplicatingEndpointId === endpoint.id}
                  reorderDisabled={!data.canReorder}
                  onDuplicate={data.handleDuplicateEndpoint}
                  onEdit={data.setEditingEndpoint}
                  onDelete={data.setDeleteTarget}
                />
              ))}
            </div>
          </SortableContext>

          <DragOverlay>
            {data.activeDragEndpoint ? (
              <div className="h-full">
                <EndpointCardView
                  endpoint={data.activeDragEndpoint}
                  formatTime={data.formatTime}
                  models={data.endpointModels[data.activeDragEndpoint.id] ?? []}
                  isOverlay
                />
              </div>
            ) : null}
          </DragOverlay>
        </DndContext>
      )}

      <EndpointDialog
        open={data.isCreateOpen}
        onOpenChange={data.setIsCreateOpen}
        onSubmit={data.handleCreate}
        title={copy.addEndpoint}
        submitLabel={copy.addEndpoint}
      />

      <EndpointDialog
        open={!!data.editingEndpoint}
        onOpenChange={(open) => !open && data.setEditingEndpoint(null)}
        onSubmit={data.handleUpdate}
        initialValues={data.editingEndpoint || undefined}
        title={copy.editEndpoint}
        submitLabel={copy.saveChanges}
      />

      <DeleteEndpointDialog
        deleteTarget={data.deleteTarget}
        isDeletingEndpoint={data.isDeletingEndpoint}
        onOpenChange={data.handleDeleteDialogOpenChange}
        onConfirm={data.handleDelete}
      />
    </div>
  );
}
