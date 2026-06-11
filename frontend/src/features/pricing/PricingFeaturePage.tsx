import { Badge } from "@/components/ui/badge"
import { PageHeader } from "@/components/PageHeader"
import { useProfileContext } from "@/context/ProfileContext"
import { useLocale } from "@/i18n/useLocale"
import { DeletePricingTemplateDialog } from "./DeletePricingTemplateDialog"
import { PricingTemplateDialog } from "./PricingTemplateDialog"
import { PricingTemplatesTable } from "./PricingTemplatesTable"
import { PricingTemplateUsageDialog } from "./PricingTemplateUsageDialog"
import { usePricingFeatureData } from "./usePricingFeatureData"

export function PricingFeaturePage() {
  const { messages } = useLocale()
  const { selectedProfile, revision } = useProfileContext()
  const copy = messages.pricingTemplatesUi
  const data = usePricingFeatureData(revision)
  const selectedProfileLabel = selectedProfile ? `${selectedProfile.name} (#${selectedProfile.id})` : copy.selectedProfileFallback

  return (
    <main className="operator-page-transition flex flex-col gap-6" data-testid="pricing-feature-page">
      <PageHeader title={copy.title} description={copy.description} />
      <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <Badge variant="outline" className="w-fit border-amber-500/30 bg-amber-500/15 text-amber-700 dark:text-amber-300">{copy.profileScopedSettings}</Badge>
          <p className="text-sm text-amber-800 dark:text-amber-300">{copy.scopeCallout(selectedProfileLabel)}</p>
        </div>
      </div>
      <PricingTemplatesTable onCreate={data.openCreatePricingTemplateDialog} onDelete={data.handleDeletePricingTemplateClick} onEdit={data.handleEditPricingTemplate} onViewUsage={data.handleViewPricingTemplateUsage} pricingTemplatePreparingEditId={data.pricingTemplatePreparingEditId} pricingTemplates={data.pricingTemplates} pricingTemplatesLoading={data.pricingTemplatesLoading} />
      <PricingTemplateDialog editingPricingTemplate={data.editingPricingTemplate} onClose={data.closePricingTemplateDialog} onOpenChange={data.setPricingTemplateDialogOpen} onSave={data.handleSavePricingTemplate} open={data.pricingTemplateDialogOpen} pricingTemplateSaving={data.pricingTemplateSaving} serverError={data.pricingTemplateServerError} />
      <PricingTemplateUsageDialog onOpenChange={data.setPricingTemplateUsageDialogOpen} open={data.pricingTemplateUsageDialogOpen} pricingTemplateUsageLoading={data.pricingTemplateUsageLoading} pricingTemplateUsageRows={data.pricingTemplateUsageRows} pricingTemplateUsageTemplate={data.pricingTemplateUsageTemplate} />
      <DeletePricingTemplateDialog deletePricingTemplateConfirm={data.deletePricingTemplateConfirm} displayTemplate={data.deletePricingTemplateDisplay ?? data.deletePricingTemplateConfirm} deletePricingTemplateConflict={data.deletePricingTemplateConflict} pricingTemplateUsageError={data.pricingTemplateUsageError} onClose={() => { data.setDeletePricingTemplateConfirm(null); data.setDeletePricingTemplateConflict(null) }} onDelete={data.handleDeletePricingTemplate} pricingTemplateDeleting={data.pricingTemplateDeleting} pricingTemplateUsageLoading={data.pricingTemplateUsageLoading} pricingTemplateUsageRows={data.pricingTemplateUsageRows} />
    </main>
  )
}

export default PricingFeaturePage
