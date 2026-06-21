import { useState } from "react"
import { Plus } from "lucide-react"
import { useProfileContext } from "@/context/ProfileContext"
import { useLocale } from "@/i18n/useLocale"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { OperatorPageHeader, OperatorPageShell } from "@/shared/design-system"
import { DeleteModelDialog } from "@/pages/models/DeleteModelDialog"
import { ModelDialog } from "@/pages/models/ModelDialog"
import { ModelsTable } from "./ModelsTable"
import { ModelsToolbar } from "@/pages/models/ModelsToolbar"
import { useModelsPageData } from "@/pages/models/useModelsPageData"
import { DEFAULT_MODELS_LIST_FILTERS, modelsQueryKeys, normalizeModelsListFilters } from "./queryKeys"

export function ModelsFeaturePage() {
  const { revision, selectedProfile } = useProfileContext()
  const { formatNumber, messages } = useLocale()
  const data = useModelsPageData(revision)
  const copy = messages.modelsPage
  const [apiFamilyFilter, setApiFamilyFilter] = useState(DEFAULT_MODELS_LIST_FILTERS.api_family)
  const [statusFilter, setStatusFilter] = useState(DEFAULT_MODELS_LIST_FILTERS.status)

  const filters = normalizeModelsListFilters({
    search: data.search,
    api_family: apiFamilyFilter,
    status: statusFilter,
  })
  const queryKey = modelsQueryKeys.list(selectedProfile?.id ?? null, filters)
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
        <Button size="sm" onClick={() => data.handleOpenDialog()}>
          <Plus data-icon="inline-start" />
          {copy.newModel}
        </Button>
      </OperatorPageHeader>

      <Card className="operator-table-shell gap-0 overflow-hidden rounded-xl">
        <CardHeader className="border-b">
          <FieldGroup className="gap-4 md:flex-row md:items-end">
            <Field className="md:max-w-sm">
              <FieldLabel>Search models</FieldLabel>
              <ModelsToolbar search={data.search} setSearch={data.setSearch} />
            </Field>
            <Field className="md:max-w-48">
              <FieldLabel>API family</FieldLabel>
              <Select value={apiFamilyFilter} onValueChange={setApiFamilyFilter}>
                <SelectTrigger aria-label="Filter API family"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All families</SelectItem>
                  <SelectItem value="openai">OpenAI</SelectItem>
                  <SelectItem value="anthropic">Anthropic</SelectItem>
                  <SelectItem value="gemini">Gemini</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field className="md:max-w-48">
              <FieldLabel>Status</FieldLabel>
              <Select value={statusFilter} onValueChange={setStatusFilter}>
                <SelectTrigger aria-label="Filter status"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All statuses</SelectItem>
                  <SelectItem value="enabled">Enabled</SelectItem>
                  <SelectItem value="disabled">Disabled</SelectItem>
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

      <ModelDialog
        editingModel={data.editingModel}
        formData={data.formData}
        formError={data.formError}
        isDialogOpen={data.isDialogOpen}
        loadbalanceStrategies={data.loadbalanceStrategies}
        targetModelsForApiFamily={data.targetModelsForApiFamily}
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
