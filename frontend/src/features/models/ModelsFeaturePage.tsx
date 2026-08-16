import { useCallback, useMemo, useState } from "react"
import { Plus } from "lucide-react"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { useLocale } from "@/i18n/useLocale"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import {
  OperatorErrorState,
  OperatorKpiCard,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorRetryButton,
  OperatorSearchInput,
} from "@/shared/design-system"
import { CreateModelDialog } from "@/pages/models/CreateModelDialog"
import { DeleteModelDialog } from "@/pages/models/DeleteModelDialog"
import { ModelDialog } from "@/pages/models/ModelDialog"
import { ModelsTable } from "./ModelsTable"
import { isSingleTruncated } from "./modelRoutingFlags"
import { useModelsPageData } from "@/pages/models/useModelsPageData"
import { DEFAULT_MODELS_LIST_FILTERS, modelsQueryKeys, normalizeModelsListFilters } from "./queryKeys"

export function ModelsFeaturePage() {
  const { formatNumber, messages } = useLocale()
  const data = useModelsPageData(0)
  const copy = messages.modelsPage
  const search = useSearch({ from: "/route/models" })
  const navigate = useNavigate()
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())

  const searchText = search.search ?? ""
  const apiFamilyFilter = search.api_family ?? DEFAULT_MODELS_LIST_FILTERS.api_family
  const statusFilter = search.status ?? DEFAULT_MODELS_LIST_FILTERS.status
  const flagFilter = search.flag ?? "all"

  // Filters, sort and page all live in the URL, so a filtered view is a link
  // an operator can hand to someone else.
  const patchSearch = useCallback(
    (patch: Record<string, string | number | undefined>, resetPage = true) => {
      void navigate({
        to: "/route/models",
        search: (current: Record<string, unknown>) => {
          const next = { ...current, ...patch }
          if (resetPage) delete next.page
          for (const key of Object.keys(next)) {
            const value = next[key]
            if (value === undefined || value === "" || value === "all") delete next[key]
          }
          return next
        },
        replace: true,
      })
    },
    [navigate],
  )

  const filtered = useMemo(() => {
    const query = searchText.trim().toLowerCase()
    return data.models.filter((model) => {
      if (query) {
        const haystack = `${model.model_id} ${model.display_name ?? ""}`.toLowerCase()
        if (!haystack.includes(query)) return false
      }
      if (apiFamilyFilter !== "all" && model.api_family !== apiFamilyFilter) return false
      if (statusFilter === "enabled" && !model.is_enabled) return false
      if (statusFilter === "disabled" && model.is_enabled) return false
      if (flagFilter === "needs_target" && model.access_targets.length > 0) return false
      if (flagFilter === "single_truncated" && !isSingleTruncated(model)) return false
      return true
    })
  }, [apiFamilyFilter, data.models, flagFilter, searchText, statusFilter])

  const stats = useMemo(() => {
    const enabled = data.models.filter((model) => model.is_enabled).length
    return {
      total: data.models.length,
      enabled,
      disabled: data.models.length - enabled,
      needsTarget: data.models.filter((model) => model.access_targets.length === 0).length,
      singleTruncated: data.models.filter(isSingleTruncated).length,
    }
  }, [data.models])

  const filters = normalizeModelsListFilters({
    search: searchText,
    api_family: apiFamilyFilter,
    status: statusFilter,
  })
  const queryKey = modelsQueryKeys.list(1, filters)

  if (data.loading) {
    return (
      <div className="flex flex-col gap-6" data-testid="models-feature-loading">
        <Skeleton className="h-8 w-40" />
        <Card className="gap-0">
          <CardHeader className="border-b">
            <Skeleton className="h-9 w-full xl:max-w-sm" />
          </CardHeader>
          <CardContent className="p-0">
            <Skeleton className="h-[500px] rounded-none border-0" />
          </CardContent>
        </Card>
      </div>
    )
  }

  if (data.loadError) {
    return (
      <OperatorPageShell data-testid="models-feature-error">
        <OperatorErrorState
          title={messages.modelsData.fetchFailed}
          description={data.loadError}
          action={<OperatorRetryButton onClick={data.retryLoad}>{messages.common.retry}</OperatorRetryButton>}
        />
      </OperatorPageShell>
    )
  }

  return (
    <OperatorPageShell data-testid="models-feature-page" data-query-key={JSON.stringify(queryKey)}>
      <OperatorPageHeader title={copy.title} description={copy.countDescription(formatNumber(filtered.length))}>
        <Button onClick={() => data.setCreateDialogOpen(true)}>
          <Plus data-icon="inline-start" />
          {copy.newModel}
        </Button>
      </OperatorPageHeader>

      <div className="grid gap-[var(--density-card-gap)] sm:grid-cols-2 xl:grid-cols-5">
        <OperatorKpiCard
          label={copy.kpiTotal}
          value={formatNumber(stats.total)}
          detail={copy.kpiTotalDetail}
          onClick={() => patchSearch({ status: undefined, flag: undefined, api_family: undefined })}
        />
        <OperatorKpiCard
          label={copy.kpiEnabled}
          value={formatNumber(stats.enabled)}
          detail={copy.kpiEnabledDetail}
          onClick={() => patchSearch({ status: "enabled", flag: undefined })}
        />
        <OperatorKpiCard
          label={copy.kpiDisabled}
          value={formatNumber(stats.disabled)}
          detail={copy.kpiDisabledDetail}
          onClick={() => patchSearch({ status: "disabled", flag: undefined })}
        />
        <OperatorKpiCard
          label={copy.kpiNeedsTarget}
          value={formatNumber(stats.needsTarget)}
          detail={copy.kpiNeedsTargetDetail}
          onClick={() => patchSearch({ flag: "needs_target", status: undefined })}
        />
        <OperatorKpiCard
          label={copy.kpiSingleTruncated}
          value={formatNumber(stats.singleTruncated)}
          detail={copy.kpiSingleTruncatedDetail}
          onClick={() => patchSearch({ flag: "single_truncated", status: undefined })}
        />
      </div>

      <Card className="operator-table-shell gap-0 overflow-hidden rounded-lg">
        <CardHeader className="border-b">
          <FieldGroup className="gap-4 md:flex-row md:items-end">
            <Field className="md:max-w-sm">
              <FieldLabel htmlFor="models-search">{copy.searchLabel}</FieldLabel>
              <OperatorSearchInput
                id="models-search"
                name="models_search"
                autoComplete="off"
                placeholder={copy.searchModels}
                value={searchText}
                onChange={(event) => patchSearch({ search: event.target.value })}
              />
            </Field>
            <Field className="md:max-w-48">
              <FieldLabel htmlFor="models-api-family">{copy.apiFamilyLabel}</FieldLabel>
              <Select value={apiFamilyFilter} onValueChange={(value) => patchSearch({ api_family: value })}>
                <SelectTrigger id="models-api-family" aria-label={copy.apiFamilyLabel}><SelectValue /></SelectTrigger>
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
            <Field className="md:max-w-48">
              <FieldLabel htmlFor="models-status">{copy.statusLabel}</FieldLabel>
              <Select value={statusFilter} onValueChange={(value) => patchSearch({ status: value })}>
                <SelectTrigger id="models-status" aria-label={copy.statusLabel}><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">{copy.statusAll}</SelectItem>
                    <SelectItem value="enabled">{copy.statusEnabled}</SelectItem>
                    <SelectItem value="disabled">{copy.statusDisabled}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field className="md:max-w-48">
              <FieldLabel htmlFor="models-flag">{copy.flagLabel}</FieldLabel>
              <Select value={flagFilter} onValueChange={(value) => patchSearch({ flag: value })}>
                <SelectTrigger id="models-flag" aria-label={copy.flagLabel}><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="all">{copy.flagAll}</SelectItem>
                    <SelectItem value="needs_target">{copy.flagNeedsTarget}</SelectItem>
                    <SelectItem value="single_truncated">{copy.flagSingleTruncated}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          </FieldGroup>
        </CardHeader>
        <CardContent className="p-0" data-table-row-count={filtered.length}>
          <ModelsTable
            filtered={filtered}
            metricsFailed={data.metricsFailed}
            metricsLoading={data.metricsLoading}
            modelMetrics24h={data.modelMetrics24h}
            modelSpend30dMicros={data.modelSpend30dMicros}
            onClearFilters={() =>
              patchSearch({ search: undefined, api_family: undefined, status: undefined, flag: undefined })
            }
            onCreate={() => data.setCreateDialogOpen(true)}
            onEdit={data.handleOpenDialog}
            onPageChange={(page) => patchSearch({ page }, false)}
            onPageSizeChange={(pageSize) => patchSearch({ page_size: pageSize })}
            onSelectionChange={setSelectedIds}
            onSetEnabled={data.setModelEnabled}
            onSetManyEnabled={data.setModelsEnabled}
            onSort={(column, direction) => patchSearch({ sort_by: column, sort_order: direction })}
            page={search.page ?? 1}
            pageSize={search.page_size}
            search={filters.search}
            selectedIds={selectedIds}
            setDeleteTarget={data.setDeleteTarget}
            sortBy={search.sort_by ?? "name"}
            sortOrder={search.sort_order ?? "asc"}
            togglingModelIds={data.togglingModelIds}
          />
        </CardContent>
      </Card>

      <CreateModelDialog
        isOpen={data.createDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        createLoadbalanceStrategyDefaultsPending={data.loadbalanceStrategyDefaultsCreating}
        onCreateLoadbalanceStrategyDefaults={data.handleCreateLoadbalanceStrategyDefaults}
        onClose={() => data.setCreateDialogOpen(false)}
        onCreated={data.handleModelCreated}
      />
      <ModelDialog
        editingModel={data.editingModel}
        formData={data.formData}
        formError={data.formError}
        isDialogOpen={data.isDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        createLoadbalanceStrategyDefaultsPending={data.loadbalanceStrategyDefaultsCreating}
        setFormData={data.setFormData}
        setIsDialogOpen={data.setIsDialogOpen}
        setLoadbalanceStrategyId={data.setLoadbalanceStrategyId}
        onCreateLoadbalanceStrategyDefaults={data.handleCreateLoadbalanceStrategyDefaults}
        onSubmit={data.handleSubmit}
      />
      <DeleteModelDialog
        deleteTarget={data.deleteTarget}
        onDelete={data.handleDelete}
        setDeleteTarget={(model) => data.setDeleteTarget(model as typeof data.deleteTarget)}
      />
    </OperatorPageShell>
  )
}

export default ModelsFeaturePage
