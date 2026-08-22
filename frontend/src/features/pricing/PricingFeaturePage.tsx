import { useMemo, useState } from "react"
import { Plus, Upload } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useLocale } from "@/i18n/useLocale"
import { OperatorKpiCard, OperatorPageHeader, OperatorPageShell } from "@/shared/design-system"
import { DeletePricingTemplateDialog } from "@/pages/pricing-templates/DeletePricingTemplateDialog"
import { PricingTemplateDialog } from "./PricingTemplateDialog"
import { PricingTemplateImportDialog } from "./PricingTemplateImportDialog"
import { PricingImportPreview } from "./PricingImportPreview"
import { PricingTemplatesTable, type PricingFilter } from "./PricingTemplatesTable"
import { usePricingFeatureData } from "./usePricingFeatureData"
import { isRecentlyChanged, totalReferences, usePricingListFacts } from "./usePricingListFacts"

export function PricingFeaturePage() {
  const { formatNumber, messages } = useLocale()
  const copy = messages.pricingTemplatesUi
  const importCopy = messages.pricing
  const data = usePricingFeatureData(0)
  const facts = usePricingListFacts(0)
  const [filter, setFilter] = useState<PricingFilter>("all")

  const stats = useMemo(() => {
    let incomplete = 0
    let unreferenced = 0
    let recentlyChanged = 0
    for (const template of data.pricingTemplates) {
      const item = facts.byId.get(template.id)
      if (item?.configuration_status === "incomplete") incomplete += 1
      if (totalReferences(item) === 0) unreferenced += 1
      if (isRecentlyChanged(template.updated_at)) recentlyChanged += 1
    }
    return { total: data.pricingTemplates.length, incomplete, unreferenced, recentlyChanged }
  }, [data.pricingTemplates, facts.byId])

  return (
    <OperatorPageShell data-testid="pricing-feature-page">
      <OperatorPageHeader
        title={copy.title}
        description={copy.description}
        actions={
          <>
            <Button type="button" variant="outline" onClick={() => data.setPricingTemplateImportDialogOpen(true)}>
              <Upload data-icon="inline-start" />
              {importCopy.importButton}
            </Button>
            <Button type="button" onClick={data.openCreatePricingTemplateDialog}>
              <Plus data-icon="inline-start" />
              {copy.addTemplate}
            </Button>
          </>
        }
      />

      <div className="grid gap-[var(--density-card-gap)] sm:grid-cols-2 xl:grid-cols-4">
        <OperatorKpiCard
          label={copy.kpiTotal}
          value={data.pricingTemplatesError ? "—" : formatNumber(stats.total)}
          detail={copy.kpiTotalDetail}
          onClick={() => setFilter("all")}
        />
        <OperatorKpiCard
          label={copy.kpiIncomplete}
          value={facts.failed ? "—" : formatNumber(stats.incomplete)}
          detail={copy.kpiIncompleteDetail}
          onClick={() => setFilter("incomplete")}
        />
        <OperatorKpiCard
          label={copy.kpiUnreferenced}
          value={facts.failed ? "—" : formatNumber(stats.unreferenced)}
          detail={copy.kpiUnreferencedDetail}
          onClick={() => setFilter("unreferenced")}
        />
        <OperatorKpiCard
          label={copy.kpiRecentlyChanged}
          value={data.pricingTemplatesError ? "—" : formatNumber(stats.recentlyChanged)}
          detail={copy.kpiRecentlyChangedDetail}
          onClick={() => setFilter("recently_changed")}
        />
      </div>

      {/* A preview is a fact the operator has to see before anything is
          written, so it lands on the page rather than inside the dialog. */}
      {data.importPreview ? (
        <PricingImportPreview
          committing={data.pricingTemplateImporting}
          onCancel={data.cancelImportPreview}
          onCommit={() => void data.commitImportPreview()}
          preview={data.importPreview}
        />
      ) : null}

      <PricingTemplatesTable
        detailHistory={data.pricingTemplateHistoryRevisions}
        detailHistoryError={data.pricingTemplateHistoryError}
        detailHistoryLoading={data.pricingTemplateHistoryLoading}
        detailUsage={data.pricingTemplateUsageRows}
        detailUsageError={data.pricingTemplateUsageLoadError}
        detailUsageLoading={data.pricingTemplateUsageLoading}
        facts={facts}
        filter={filter}
        onDelete={data.handleDeletePricingTemplateClick}
        onEdit={data.handleEditPricingTemplate}
        onFilterChange={setFilter}
        onLoadHistory={data.handleViewPricingTemplateHistory}
        onLoadUsage={data.handleViewPricingTemplateUsage}
        onRetry={() => {
          void data.fetchPricingTemplates(true)
          facts.refresh()
        }}
        pricingTemplateError={data.pricingTemplatesError}
        pricingTemplatePreparingEditId={data.pricingTemplatePreparingEditId}
        pricingTemplates={data.pricingTemplates}
        pricingTemplatesLoading={data.pricingTemplatesLoading}
      />

      <PricingTemplateDialog editingPricingTemplate={data.editingPricingTemplate} impact={data.pricingTemplateImpact} impactError={data.pricingTemplateImpactError} impactLoading={data.pricingTemplateImpactLoading} onClose={data.closePricingTemplateDialog} onOpenChange={data.setPricingTemplateDialogOpen} onRetryImpact={() => void data.retryPricingTemplateImpact()} onSave={data.handleSavePricingTemplate} open={data.pricingTemplateDialogOpen} pricingTemplateSaving={data.pricingTemplateSaving} serverValidation={data.pricingTemplateServerError} />
      <PricingTemplateImportDialog importing={data.pricingTemplateImporting} onClose={() => data.setPricingTemplateImportDialogOpen(false)} onImport={data.handleImportPricingTemplates} onOpenChange={data.setPricingTemplateImportDialogOpen} open={data.pricingTemplateImportDialogOpen} />
      <DeletePricingTemplateDialog deletePricingTemplateConfirm={data.deletePricingTemplateConfirm} displayTemplate={data.deletePricingTemplateDisplay ?? data.deletePricingTemplateConfirm} deletePricingTemplateConflict={data.deletePricingTemplateConflict} pricingTemplateUsageError={data.pricingTemplateUsageError} onClose={() => { data.setDeletePricingTemplateConfirm(null); data.setDeletePricingTemplateConflict(null) }} onDelete={data.handleDeletePricingTemplate} pricingTemplateDeleting={data.pricingTemplateDeleting} pricingTemplateUsageLoading={data.pricingTemplateUsageLoading} pricingTemplateUsageRows={data.pricingTemplateUsageRows} />
    </OperatorPageShell>
  )
}

export default PricingFeaturePage
