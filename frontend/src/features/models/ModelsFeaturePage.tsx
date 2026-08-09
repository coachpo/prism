import { useState } from "react"
import { Plus } from "lucide-react"
import { useLocale } from "@/i18n/useLocale"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { OperatorPageHeader, OperatorPageShell } from "@/shared/design-system"
import { CreateModelDialog } from "@/pages/models/CreateModelDialog"
import { DeleteModelDialog } from "@/pages/models/DeleteModelDialog"
import { ModelDialog } from "@/pages/models/ModelDialog"
import { ModelsTable } from "./ModelsTable"
import { ModelsToolbar } from "@/pages/models/ModelsToolbar"
import { useModelsPageData } from "@/pages/models/useModelsPageData"
import { DEFAULT_MODELS_LIST_FILTERS, modelsQueryKeys, normalizeModelsListFilters } from "./queryKeys"

export function ModelsFeaturePage() {
  const { formatNumber, messages } = useLocale()
  const data = useModelsPageData(0)
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const copy = messages.modelsPage
  const [apiFamilyFilter, setApiFamilyFilter] = useState(DEFAULT_MODELS_LIST_FILTERS.api_family)
  const [statusFilter, setStatusFilter] = useState(DEFAULT_MODELS_LIST_FILTERS.status)

  const filters = normalizeModelsListFilters({
    search: data.search,
    api_family: apiFamilyFilter,
    status: statusFilter,
  })
  const queryKey = modelsQueryKeys.list(1, filters)
  const filtered = data.filtered.filter((model) => {
    if (filters.api_family !== "all" && model.api_family !== filters.api_family) return false
    if (filters.status === "enabled" && !model.is_enabled) return false
    if (filters.status === "disabled" && model.is_enabled) return false
    return true
  })
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

  return (
    <OperatorPageShell data-testid="models-feature-page" data-query-key={JSON.stringify(queryKey)}>
      <OperatorPageHeader title={copy.title} description={copy.countDescription(formatNumber(filtered.length))}>
        <Button size="sm" onClick={() => setCreateDialogOpen(true)}>
          <Plus data-icon="inline-start" />
          {copy.newModel}
        </Button>
      </OperatorPageHeader>

      <Card className="operator-table-shell gap-0 overflow-hidden rounded-xl">
        <CardHeader className="border-b">
          <FieldGroup className="gap-4 md:flex-row md:items-end">
            <Field className="md:max-w-sm">
              <FieldLabel>{messages.modelsPage.searchModels}</FieldLabel>
              <ModelsToolbar search={data.search} setSearch={data.setSearch} />
            </Field>
            <Field className="md:max-w-48">
              <FieldLabel>{messages.common.apiFamily}</FieldLabel>
              <Select value={apiFamilyFilter} onValueChange={setApiFamilyFilter}>
                <SelectTrigger aria-label={messages.common.apiFamily}><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{messages.modelsPage.allFamilies}</SelectItem>
                  <SelectItem value="openai">OpenAI</SelectItem>
                  <SelectItem value="anthropic">Anthropic</SelectItem>
                  <SelectItem value="gemini">Gemini</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field className="md:max-w-48">
              <FieldLabel>{messages.common.status}</FieldLabel>
              <Select value={statusFilter} onValueChange={setStatusFilter}>
                <SelectTrigger aria-label={messages.common.status}><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All statuses</SelectItem>
                  <SelectItem value="enabled">{messages.modelDetail.enabled}</SelectItem>
                  <SelectItem value="disabled">{messages.modelDetail.disabled}</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </FieldGroup>
        </CardHeader>
        <CardContent className="p-0" data-table-row-count={filtered.length}>
          <ModelsTable
            filtered={filtered}
            handleOpenDialog={data.handleOpenDialog}
            metricsLoading={data.metricsLoading}
            modelMetrics24h={data.modelMetrics24h}
            modelSpend30dMicros={data.modelSpend30dMicros}
            search={filters.search}
            setDeleteTarget={data.setDeleteTarget}
          />
        </CardContent>
      </Card>

      <CreateModelDialog
        isOpen={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        revision={0}
        loadbalanceStrategies={data.loadbalanceStrategies}
        createLoadbalanceStrategyDefaultsPending={data.loadbalanceStrategyDefaultsCreating}
        onCreateLoadbalanceStrategyDefaults={data.handleCreateLoadbalanceStrategyDefaults}
        onSubmit={async (payload) => {
          const response = await data.handleCreateModelSubmit(payload)
          setCreateDialogOpen(false)
          return response
        }}
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
